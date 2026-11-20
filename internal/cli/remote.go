package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	inspection "github.com/nocaptain-oss/archie-link/internal/inspect"
	"github.com/nocaptain-oss/archie-link/internal/model"
	"github.com/nocaptain-oss/archie-link/internal/remote"
)

// runRemote 分发 remote serve / remote connect。
func runRemote(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: archielink remote <serve|connect> [options]")
		return 2
	}
	switch args[0] {
	case "serve":
		return runRemoteServe(args[1:], stdout, stderr, deps)
	case "connect":
		return runRemoteConnect(args[1:], stdout, stderr, deps)
	default:
		fmt.Fprintf(stderr, "unknown remote command: %s\n", args[0])
		return 2
	}
}

// serveStartupJSON 是 remote serve 的启动信息（完整配对码仅供本机用户
// 展示与口头传递，不进入日志与聊天通道）。
type serveStartupJSON struct {
	SchemaVersion string `json:"schema_version"`
	Command       string `json:"command"`
	Endpoint      string `json:"endpoint"`
	Fingerprint   string `json:"fingerprint"`
	PairingCode   string `json:"pairing_code"`
	ExpiresAt     string `json:"expires_at"`
}


// remoteWriteError 输出远程会话失败：--json 走结构化错误（稳定码 +
// exit 5），文本模式只打印一行错误，避免码与消息重复。
func remoteWriteError(stdout, stderr io.Writer, jsonOutput bool, code string, err error) int {
	if jsonOutput {
		return writeLifecycleError(stdout, stderr, true, 5, code, err)
	}
	fmt.Fprintf(stderr, "%s\n", err)
	return 5
}

func runRemoteServe(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	var (
		port      int
		ttl       = 10 * time.Minute
		yes       bool
		jsonQuiet bool
	)
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			jsonQuiet = true
		case "--yes":
			yes = true
		case "--port":
			index++
			if index >= len(args) {
				fmt.Fprintln(stderr, "usage: archielink remote serve [--port N] [--ttl 10m] [--yes] [--json]")
				return 2
			}
			parsed, err := parsePort(args[index])
			if err != nil {
				fmt.Fprintf(stderr, "invalid --port: %v\n", err)
				return 2
			}
			port = parsed
		case "--ttl":
			index++
			if index >= len(args) {
				fmt.Fprintln(stderr, "usage: archielink remote serve [--port N] [--ttl 10m] [--yes] [--json]")
				return 2
			}
			parsed, err := time.ParseDuration(args[index])
			if err != nil || parsed <= 0 {
				fmt.Fprintf(stderr, "invalid --ttl: %v\n", err)
				return 2
			}
			ttl = parsed
		default:
			fmt.Fprintf(stderr, "unknown remote serve option: %s\n", args[index])
			return 2
		}
	}
	addr := "0.0.0.0:0"
	if port > 0 {
		addr = fmt.Sprintf("0.0.0.0:%d", port)
	}
	provider := &inspection.RemoteProvider{
		Registry: deps.Registry,
		GOOS:     deps.GOOS,
		Prober:   deps.Prober,
		Version:  Version,
	}
	server, err := remote.Start(provider, remote.ServerOptions{
		Addr:        addr,
		SessionTTL:  ttl,
		Confirm:     confirmFromStdin(stderr, yes),
		Logger:      remoteLogger(stderr),
	})
	if err != nil {
		return remoteWriteError(stdout, stderr, jsonQuiet, "remote_serve_failed", err)
	}
	info := server.Info()
	if jsonQuiet {
		if err := encodeResult(stdout, stderr, serveStartupJSON{
			SchemaVersion: model.SchemaVersion,
			Command:       "remote serve",
			Endpoint:      info.Endpoint,
			Fingerprint:   info.Fingerprint,
			PairingCode:   info.PairingCode,
			ExpiresAt:     info.ExpiresAt.UTC().Format(time.RFC3339),
		}); err != 0 {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "ArchieLink one-time read-only session\n")
		fmt.Fprintf(stdout, "  Endpoint:    %s\n", info.Endpoint)
		fmt.Fprintf(stdout, "  Pairing code: %s\n", info.PairingCode)
		fmt.Fprintf(stdout, "  Fingerprint: %s\n", info.Fingerprint)
		fmt.Fprintf(stdout, "  Expires:     %s\n", info.ExpiresAt.Local().Format(time.RFC3339))
		fmt.Fprintf(stdout, "Waiting for a controller connection...\n")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErr := server.Run(ctx)
	if serveErr == nil {
		fmt.Fprintln(stderr, "session finished; listener closed, temporary keys discarded")
		return 0
	}
	if errors.Is(serveErr, context.Canceled) {
		fmt.Fprintln(stderr, "serve cancelled; listener closed, temporary keys discarded")
		return 0
	}
	return remoteWriteError(stdout, stderr, jsonQuiet, remote.ErrorCodeFrom(serveErr), serveErr)
}

// confirmFromStdin 构建 Target 端确认回调：--yes 自动接受，否则要求
// 用户在终端输入 y 确认（时限由 remote 包控制，超时视为拒绝）。
func confirmFromStdin(stderr io.Writer, yes bool) func(remote.ConfirmRequest) bool {
	if yes {
		return func(remote.ConfirmRequest) bool { return true }
	}
	return func(req remote.ConfirmRequest) bool {
		fmt.Fprintf(stderr, "Pairing request from %s at %s\n", req.RemoteAddr, req.At.Local().Format(time.RFC3339))
		fmt.Fprintf(stderr, "Accept this read-only session? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return false
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes"
	}
}

func remoteLogger(stderr io.Writer) *log.Logger {
	return log.New(stderr, "archielink-remote: ", log.LstdFlags|log.Lmsgprefix)
}

// remoteCheckup 是 remote connect 的聚合结果：一次配对执行全套
// 只读体检，远程 schema 与本地命令一致。
type remoteCheckup struct {
	SchemaVersion string          `json:"schema_version"`
	Command       string          `json:"command"`
	Endpoint      string          `json:"endpoint"`
	Paired        bool            `json:"paired"`
	Hello         json.RawMessage `json:"hello,omitempty"`
	Capabilities  json.RawMessage `json:"capabilities,omitempty"`
	Detect        json.RawMessage `json:"detect,omitempty"`
	Plan          json.RawMessage `json:"plan,omitempty"`
	Doctor        json.RawMessage `json:"doctor,omitempty"`
}

func runRemoteConnect(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	var (
		endpoint    string
		code        string
		fingerprint string
		method      string
		timeout     = 30 * time.Second
		jsonOutput  bool
	)
	rest := args
	if len(rest) >= 2 && !strings.HasPrefix(rest[0], "-") {
		endpoint, code = rest[0], rest[1]
		rest = rest[2:]
	}
	for index := 0; index < len(rest); index++ {
		switch rest[index] {
		case "--json":
			jsonOutput = true
		case "--fingerprint":
			index++
			if index >= len(rest) {
				fmt.Fprintln(stderr, "usage: archielink remote connect <ip:port> <code> [--fingerprint <sha256>] [--rpc <method>] [--timeout 30s] [--json]")
				return 2
			}
			fingerprint = rest[index]
		case "--rpc":
			index++
			if index >= len(rest) {
				fmt.Fprintln(stderr, "usage: archielink remote connect <ip:port> <code> [--fingerprint <sha256>] [--rpc <method>] [--timeout 30s] [--json]")
				return 2
			}
			method = rest[index]
		case "--timeout":
			index++
			if index >= len(rest) {
				fmt.Fprintln(stderr, "usage: archielink remote connect <ip:port> <code> [--fingerprint <sha256>] [--rpc <method>] [--timeout 30s] [--json]")
				return 2
			}
			parsed, err := time.ParseDuration(rest[index])
			if err != nil || parsed <= 0 {
				fmt.Fprintf(stderr, "invalid --timeout: %v\n", err)
				return 2
			}
			timeout = parsed
		default:
			fmt.Fprintf(stderr, "unknown remote connect option: %s\n", rest[index])
			return 2
		}
	}
	if endpoint == "" || code == "" {
		fmt.Fprintln(stderr, "usage: archielink remote connect <ip:port> <code> [--fingerprint <sha256>] [--rpc <method>] [--timeout 30s] [--json]")
		return 2
	}
	if method != "" && !isRemoteMethod(method) {
		fmt.Fprintf(stderr, "unknown --rpc method: %s (allowed: hello|capabilities|detect|plan|doctor)\n", method)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client, err := remote.Connect(ctx, endpoint, code, remote.ClientOptions{
		Fingerprint: fingerprint,
		Timeout:     timeout,
	})
	if err != nil {
		return remoteWriteError(stdout, stderr, jsonOutput, remote.ErrorCodeFrom(err), err)
	}
	defer client.Close()
	checkup := remoteCheckup{
		SchemaVersion: model.SchemaVersion,
		Command:       "remote connect",
		Endpoint:      endpoint,
		Paired:        true,
	}
	if method != "" {
		result, callErr := client.Call(method)
		if callErr != nil {
			return remoteWriteError(stdout, stderr, jsonOutput, remote.ErrorCodeFrom(callErr), callErr)
		}
		attachCheckupResult(&checkup, method, result)
	} else {
		for _, m := range []string{remote.MethodHello, remote.MethodCapabilities, remote.MethodDetect, remote.MethodPlan, remote.MethodDoctor} {
			result, callErr := client.Call(m)
			if callErr != nil {
				return remoteWriteError(stdout, stderr, jsonOutput, remote.ErrorCodeFrom(callErr), callErr)
			}
			attachCheckupResult(&checkup, m, result)
		}
	}
	client.Close()
	if jsonOutput {
		return encodeResult(stdout, stderr, checkup)
	}
	writeCheckupText(stdout, checkup)
	return 0
}

func isRemoteMethod(method string) bool {
	switch method {
	case remote.MethodHello, remote.MethodCapabilities, remote.MethodDetect, remote.MethodPlan, remote.MethodDoctor:
		return true
	default:
		return false
	}
}

func attachCheckupResult(checkup *remoteCheckup, method string, result json.RawMessage) {
	switch method {
	case remote.MethodHello:
		checkup.Hello = result
	case remote.MethodCapabilities:
		checkup.Capabilities = result
	case remote.MethodDetect:
		checkup.Detect = result
	case remote.MethodPlan:
		checkup.Plan = result
	case remote.MethodDoctor:
		checkup.Doctor = result
	}
}

// writeCheckupText 输出人类可读摘要；结构化细节走 --json。
func writeCheckupText(stdout io.Writer, checkup remoteCheckup) {
	fmt.Fprintf(stdout, "paired=%s endpoint=%s\n", fmt.Sprint(checkup.Paired), checkup.Endpoint)
	if len(checkup.Hello) > 0 {
		var hello struct {
			Platform string `json:"platform"`
			Version  string `json:"version"`
		}
		if json.Unmarshal(checkup.Hello, &hello) == nil {
			fmt.Fprintf(stdout, "target: %s (archielink %s)\n", hello.Platform, hello.Version)
		}
	}
	if len(checkup.Detect) > 0 {
		var detect model.DetectReport
		if json.Unmarshal(checkup.Detect, &detect) == nil {
			fmt.Fprintf(stdout, "mode=%s os=%s\n", detect.Mode, detect.Platform.OS)
		}
	}
	if len(checkup.Doctor) > 0 {
		var report model.DoctorReport
		if json.Unmarshal(checkup.Doctor, &report) == nil {
			fmt.Fprintf(stdout, "findings=%d\n", len(report.Findings))
			for _, finding := range report.Findings {
				fmt.Fprintf(stdout, "- %s [%s]: %s\n", finding.Code, finding.Level, finding.Message)
			}
		}
	}
}

func parsePort(value string) (int, error) {
	var port int
	if _, err := fmt.Sscanf(value, "%d", &port); err != nil {
		return 0, err
	}
	if port < 0 || port > 65535 {
		return 0, fmt.Errorf("port %d out of range", port)
	}
	return port, nil
}
