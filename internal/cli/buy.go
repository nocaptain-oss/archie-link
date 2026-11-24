package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/deviceid"
	"github.com/nocaptain-oss/archie-link/internal/leaseclient"
	qrcode "github.com/skip2/go-qrcode"
)

const (
	// buyDefaultTimeout 覆盖整个等待支付阶段；超时后订单仍可在 pay_before 前用同
	// --request-id 续接（服务端按 request_id 幂等返回同一订单）。
	buyDefaultTimeout = 15 * time.Minute
	buyPollInterval   = 3 * time.Second
)

type buyOptions struct {
	plan       string
	server     string
	requestID  string
	timeout    time.Duration
	pollEvery  time.Duration
	jsonOutput bool
}

// runBuy 一条命令走通统一收款：下单 → 收银台 URL → 轮询支付 → 激活落盘租约。
func runBuy(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	options, ok := parseBuyOptions(args, stderr)
	if !ok {
		return 2
	}
	if deps.LifecycleError != nil {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 5, "preflight_failed", deps.LifecycleError)
	}
	if deps.Lifecycle == nil || deps.Lifecycle.Automatic == nil {
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 3, "unsupported_platform",
			errors.New("buy is not available on this platform"))
	}
	lifecycle := deps.Lifecycle
	if options.requestID == "" {
		options.requestID = newBuyRequestID()
	}
	device := deviceid.NewWithEnclave(deviceid.KeychainStore{}, lifecycle.InstallID, deviceid.SecureEnclaveStore{})
	client := leaseclient.Client{
		BaseURL: options.server, Device: device, InstallID: lifecycle.InstallID,
		APIToken: os.Getenv("ARCHIE_LINK_CP_API_TOKEN"),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runBuyFlow(ctx, client, options, leaseFilePath(lifecycle, ""), lifecycle, stdout, stderr)
}

// runBuyFlow 执行 buy 的实际流程。--json 模式按行输出事件（intent → paid → 租约），
// 每行都是完整 JSON 对象，供 agent 逐行消费。
func runBuyFlow(ctx context.Context, client leaseclient.Client, options buyOptions, path string, lifecycle *LifecycleDependencies, stdout, stderr io.Writer) int {
	if options.timeout <= 0 {
		options.timeout = buyDefaultTimeout
	}
	if options.pollEvery <= 0 {
		options.pollEvery = buyPollInterval
	}
	intent, err := client.BeginPageIntent(ctx, options.requestID, options.plan)
	if err != nil {
		if pagePayUnavailable(err) {
			return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "page_pay_unavailable",
				fmt.Errorf("该服务端未开通统一收款：%w", err))
		}
		return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "buy_failed", err)
	}
	if options.jsonOutput {
		if err := writeJSON(stdout, map[string]any{
			"schema_version": "1",
			"event":          "intent",
			"order_id":       intent.OrderID,
			"plan":           intent.Plan,
			"amount":         intent.Amount,
			"currency":       intent.Currency,
			"paid":           intent.Paid,
			"pay_url":        intent.PayURL,
			"pay_qr_code":    intent.PayQRCode,
			"pay_before":     intent.PayBefore,
		}); err != nil {
			fmt.Fprintf(stderr, "encode buy intent: %v\n", err)
			return 1
		}
	} else if intent.Paid {
		fmt.Fprintf(stdout, "order %s already paid (%s %s)\n", intent.OrderID, intent.Amount, intent.Currency)
	} else {
		fmt.Fprintf(stdout, "order %s created: %s %s\n", intent.OrderID, intent.Amount, intent.Currency)
		// 即刻收优先在终端内渲染二维码，避免离开 agent 对话框。内容优先
		// alipays:// 深链（扫一扫直达购买页，实测有效）；purchase_qr_code 是
		// mobilecodec 中转链接，扫码只显示中转页的另一个码，仅作后备。
		qrContent := intent.PayURL
		if qrContent == "" {
			qrContent = intent.PayQRCode
		}
		if qr, err := renderTerminalQR(qrContent); err == nil {
			if strings.HasPrefix(qrContent, "alipays://") {
				// 即刻收 purchase.create 返回支付宝内打开的深链：只能用支付宝扫一扫。
				fmt.Fprintf(stdout, "open Alipay and scan this QR code (扫一扫) to complete payment (before %s):\n",
					intent.PayBefore.Format(time.RFC3339))
				fmt.Fprint(stdout, qr)
				fmt.Fprintf(stdout, "no scanner? copy this link into Alipay (before %s):\n%s\n",
					intent.PayBefore.Format(time.RFC3339), intent.PayURL)
			} else {
				fmt.Fprintf(stdout, "scan this QR code with Alipay to complete payment (before %s):\n",
					intent.PayBefore.Format(time.RFC3339))
				fmt.Fprint(stdout, qr)
				fmt.Fprintf(stdout, "no scanner? open this URL in a browser (before %s):\n%s\n",
					intent.PayBefore.Format(time.RFC3339), intent.PayURL)
			}
		} else {
			fmt.Fprintf(stdout, "open this URL in a browser to complete payment (before %s):\n%s\n",
				intent.PayBefore.Format(time.RFC3339), intent.PayURL)
		}
		fmt.Fprintf(stdout, "waiting for payment (up to %s)...\n", options.timeout)
	}
	if !intent.Paid {
		payBefore := intent.PayBefore
		requestID := options.requestID
		intent, err = waitForPayment(ctx, client, intent.OrderID, options)
		if err != nil {
			if errors.Is(err, errBuyTimeout) {
				return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "payment_timeout",
					fmt.Errorf("%w; rerun `archielink buy %s --request-id %s` to resume before %s",
						err, options.plan, requestID, payBefore.Format(time.RFC3339)))
			}
			return writeLifecycleError(stdout, stderr, options.jsonOutput, 4, "buy_failed", err)
		}
		if options.jsonOutput {
			if err := writeJSON(stdout, map[string]any{
				"schema_version": "1",
				"event":          "paid",
				"order_id":       intent.OrderID,
			}); err != nil {
				fmt.Fprintf(stderr, "encode paid event: %v\n", err)
				return 1
			}
		} else {
			fmt.Fprintln(stdout, "payment confirmed")
		}
	}
	return storeActivatedLease(ctx, client, intent.OrderID, path, lifecycle, stdout, stderr, options.jsonOutput)
}

var errBuyTimeout = errors.New("payment was not confirmed in time")

// renderTerminalQR 把支付入口渲染为终端二维码：半块字符 + 固定黑字白底
// ANSI 色，扫描对比度与终端主题无关。渲染失败时调用方回退打印 URL。
func renderTerminalQR(content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", errors.New("qr content is empty")
	}
	code, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bitmap := code.Bitmap()
	var out strings.Builder
	for y := 0; y < len(bitmap); y += 2 {
		out.WriteString("\033[30;47m")
		for x := 0; x < len(bitmap[y]); x++ {
			top := bitmap[y][x]
			bottom := y+1 < len(bitmap) && bitmap[y+1][x]
			switch {
			case top && bottom:
				out.WriteRune('█')
			case top:
				out.WriteRune('▀')
			case bottom:
				out.WriteRune('▄')
			default:
				out.WriteRune(' ')
			}
		}
		out.WriteString("\033[0m\n")
	}
	return out.String(), nil
}

// waitForPayment 轮询订单状态直到 paid 或超时；服务端 Status 自带 TradeQuery 兜底。
func waitForPayment(ctx context.Context, client leaseclient.Client, orderID string, options buyOptions) (leaseclient.PageIntent, error) {
	pollCtx, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	for {
		select {
		case <-pollCtx.Done():
			return leaseclient.PageIntent{}, fmt.Errorf("%w within %s", errBuyTimeout, options.timeout)
		case <-time.After(options.pollEvery):
		}
		intent, err := client.PageStatus(pollCtx, orderID)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || pollCtx.Err() != nil {
				return leaseclient.PageIntent{}, fmt.Errorf("%w within %s", errBuyTimeout, options.timeout)
			}
			return leaseclient.PageIntent{}, err
		}
		if intent.Paid {
			return intent, nil
		}
	}
}

func pagePayUnavailable(err error) bool {
	return errors.Is(err, leaseclient.ErrPagePayUnavailable) ||
		strings.Contains(err.Error(), "not configured")
}

func newBuyRequestID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("buy-%d", time.Now().UnixNano())
	}
	return "buy-" + hex.EncodeToString(buf)
}

func parseBuyOptions(args []string, stderr io.Writer) (buyOptions, bool) {
	var options buyOptions
	usage := "usage: archielink buy <plan> [--server url] [--request-id id] [--timeout 15m] [--json]"
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(stderr, usage)
		return options, false
	}
	options.plan = args[0]
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--json":
			options.jsonOutput = true
		case "--server", "--request-id", "--timeout":
			option := args[i]
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "%s requires a value\n", option)
				return options, false
			}
			i++
			switch option {
			case "--server":
				options.server = args[i]
			case "--request-id":
				options.requestID = args[i]
			case "--timeout":
				duration, err := time.ParseDuration(args[i])
				if err != nil || duration <= 0 {
					fmt.Fprintf(stderr, "invalid --timeout: %s\n", args[i])
					return options, false
				}
				options.timeout = duration
			}
		default:
			fmt.Fprintf(stderr, "unknown buy option: %s\n", args[i])
			return options, false
		}
	}
	return options, true
}
