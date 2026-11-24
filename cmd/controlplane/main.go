// controlplane 是 ArchieLink 商业控制面的最小 HTTP 壳。
//
// 启动密钥（fail closed，缺一不可）：
//
//	ARCHIE_LINK_CP_KEY_ID          与客户端编译期信任根匹配的 key ID
//	ARCHIE_LINK_CP_SIGNING_KEY     base64 Ed25519 私钥 seed
//	ARCHIE_LINK_CP_NOTIFY_SECRET   内部支付通知 HMAC secret
//	ARCHIE_LINK_CP_API_TOKEN       订单/epoch API bearer token
//	ARCHIE_LINK_CP_ADMIN_TOKEN     退款/拒付/设备管理 admin bearer token
//	ARCHIE_LINK_PAY_*              SkillHub Pay / 支付宝 AI 按量付费配置
//	ARCHIE_LINK_PAGE_PAY_*         统一收款（alipay.trade.page.pay）配置
//	ARCHIE_LINK_CP_ADDR            监听地址（默认 127.0.0.1:8790）
//	ARCHIE_LINK_CP_STATE           状态文件（默认 /var/lib/archielink-controlplane/state.json）
//
// 红线：云端只做控制面；不承载流量、不存用户秘密，
// down/rollback 在客户端永不依赖本服务。
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/aipay"
	"github.com/nocaptain-oss/archie-link/internal/controlplane"
)

func main() {
	keyID := os.Getenv("ARCHIE_LINK_CP_KEY_ID")
	seedText := os.Getenv("ARCHIE_LINK_CP_SIGNING_KEY")
	secretText := os.Getenv("ARCHIE_LINK_CP_NOTIFY_SECRET")
	apiToken := os.Getenv("ARCHIE_LINK_CP_API_TOKEN")
	adminToken := os.Getenv("ARCHIE_LINK_CP_ADMIN_TOKEN")
	if keyID == "" || seedText == "" || secretText == "" || apiToken == "" || adminToken == "" {
		log.Fatal("controlplane signing, notify, API and admin secrets are required")
	}
	seed, err := base64.StdEncoding.DecodeString(seedText)
	if err != nil || len(seed) != ed25519.SeedSize {
		log.Fatal("ARCHIE_LINK_CP_SIGNING_KEY must be base64 ed25519 seed")
	}
	key := ed25519.NewKeyFromSeed(seed)
	addr := os.Getenv("ARCHIE_LINK_CP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8790"
	}
	statePath := os.Getenv("ARCHIE_LINK_CP_STATE")
	if statePath == "" {
		statePath = "/var/lib/archielink-controlplane/state.json"
	}
	service, err := controlplane.NewServiceWithStore(keyID, key, []byte(secretText), nil, controlplane.NewFileStore(statePath))
	if err != nil {
		log.Fatalf("load controlplane state from %s: %v", statePath, err)
	}
	payment, err := newPaymentService(service)
	if err != nil {
		log.Fatal(err)
	}
	pagePay, err := newPagePayService(service)
	if err != nil {
		log.Fatal(err)
	}
	nowPay, err := newNowPayService(service)
	if err != nil {
		log.Fatal(err)
	}
	handler, err := controlplane.NewHandler(service, controlplane.HandlerConfig{
		APIToken:   apiToken,
		AdminToken: adminToken,
		Limiter:    controlplane.NewRateLimiter(60, 20),
		TrustProxy: true,
		Payment:    payment,
		PagePay:    pagePay,
		NowPay:     nowPay,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("controlplane listening on %s (state=%s)", addr, statePath)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func newPaymentService(service *controlplane.Service) (*controlplane.PaymentService, error) {
	values := map[string]string{
		"ARCHIE_LINK_PAY_APP_ID":            os.Getenv("ARCHIE_LINK_PAY_APP_ID"),
		"ARCHIE_LINK_PAY_PRIVATE_KEY":       os.Getenv("ARCHIE_LINK_PAY_PRIVATE_KEY"),
		"ARCHIE_LINK_PAY_ALIPAY_PUBLIC_KEY": os.Getenv("ARCHIE_LINK_PAY_ALIPAY_PUBLIC_KEY"),
		"ARCHIE_LINK_PAY_SELLER_ID":         os.Getenv("ARCHIE_LINK_PAY_SELLER_ID"),
		"ARCHIE_LINK_PAY_SERVICE_ID":        os.Getenv("ARCHIE_LINK_PAY_SERVICE_ID"),
	}
	configured := false
	for name, value := range values {
		if strings.TrimSpace(value) != "" {
			configured = true
		} else if configured {
			return nil, errors.New(name + " is required when SkillHub Pay is configured")
		}
	}
	if !configured {
		log.Printf("SkillHub Pay disabled: ARCHIE_LINK_PAY_* is not configured")
		return nil, nil
	}
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, errors.New(name + " is required")
		}
	}
	productionValue := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_PRODUCTION"))
	production := false
	if productionValue != "" {
		parsed, err := strconv.ParseBool(productionValue)
		if err != nil {
			return nil, errors.New("ARCHIE_LINK_PAY_PRODUCTION must be true, false, 1 or 0")
		}
		production = parsed
	}
	gateway := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_GATEWAY"))
	if gateway == "" {
		if production {
			gateway = "https://openapi.alipay.com/gateway.do"
		} else {
			gateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
		}
	}
	serviceID := values["ARCHIE_LINK_PAY_SERVICE_ID"]
	allowMissing := !production &&
		gateway == "https://openapi-sandbox.dl.alipaydev.com/gateway.do" &&
		serviceID == "api_mock_service_id"
	if production && serviceID == "api_mock_service_id" {
		return nil, errors.New("production SkillHub Pay cannot use api_mock_service_id")
	}
	client, err := aipay.New(aipay.Config{
		AppID:           values["ARCHIE_LINK_PAY_APP_ID"],
		PrivateKey:      values["ARCHIE_LINK_PAY_PRIVATE_KEY"],
		AlipayPublicKey: values["ARCHIE_LINK_PAY_ALIPAY_PUBLIC_KEY"],
		Gateway:         gateway,
		Production:      production,
	})
	if err != nil {
		return nil, err
	}
	price := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_ACTIVATION_PRICE"))
	if price == "" {
		price = "19.90"
	}
	goodsName := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_GOODS_NAME"))
	if goodsName == "" {
		goodsName = "ArchieLink 安全激活"
	}
	window := 30 * time.Minute
	if value := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_WINDOW")); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 || parsed > 12*time.Hour {
			return nil, errors.New("ARCHIE_LINK_PAY_WINDOW must be a duration no longer than 12h")
		}
		window = parsed
	}
	payment, err := controlplane.NewPaymentService(service, client, controlplane.PaymentConfig{
		Price: price, GoodsName: goodsName, Currency: "CNY",
		SellerID: values["ARCHIE_LINK_PAY_SELLER_ID"], SellerName: "汪连 ArchieLink",
		SellerAppID: values["ARCHIE_LINK_PAY_APP_ID"], ServiceID: serviceID,
		ResourceID: controlplane.ActivationTaskResource, PayWindow: window,
		AllowSandboxMissingFields: allowMissing,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("SkillHub Pay enabled (production=%t, service_id=%s)", production, serviceID)
	return payment, nil
}

// newPagePayService 装配三产品统一收款（aipay「AI网页应用收款」，
// alipay.trade.page.pay）。凭据复用 ARCHIE_LINK_PAY_* 的同一支付宝应用；
// ARCHIE_LINK_PAGE_PAY_NOTIFY_URL 与 RETURN_URL 均未配置时整块禁用。
func newPagePayService(service *controlplane.Service) (*controlplane.PagePayService, error) {
	notifyURL := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAGE_PAY_NOTIFY_URL"))
	returnURL := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAGE_PAY_RETURN_URL"))
	appID := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_APP_ID"))
	privateKey := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_PRIVATE_KEY"))
	alipayPublicKey := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_ALIPAY_PUBLIC_KEY"))
	if notifyURL == "" && returnURL == "" && appID == "" {
		log.Printf("page pay disabled: ARCHIE_LINK_PAGE_PAY_* is not configured")
		return nil, nil
	}
	if notifyURL == "" || returnURL == "" {
		return nil, errors.New("ARCHIE_LINK_PAGE_PAY_NOTIFY_URL and ARCHIE_LINK_PAGE_PAY_RETURN_URL must be configured together")
	}
	if appID == "" || privateKey == "" || alipayPublicKey == "" {
		return nil, errors.New("page pay requires ARCHIE_LINK_PAY_APP_ID, ARCHIE_LINK_PAY_PRIVATE_KEY and ARCHIE_LINK_PAY_ALIPAY_PUBLIC_KEY")
	}
	production := false
	if value := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_PRODUCTION")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, errors.New("ARCHIE_LINK_PAY_PRODUCTION must be true, false, 1 or 0")
		}
		production = parsed
	}
	gateway := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_GATEWAY"))
	if gateway == "" {
		if production {
			gateway = "https://openapi.alipay.com/gateway.do"
		} else {
			gateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
		}
	}
	client, err := aipay.New(aipay.Config{
		AppID:           appID,
		PrivateKey:      privateKey,
		AlipayPublicKey: alipayPublicKey,
		Gateway:         gateway,
		Production:      production,
	})
	if err != nil {
		return nil, err
	}
	window := 30 * time.Minute
	if value := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAGE_PAY_WINDOW")); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 || parsed > 2*time.Hour {
			return nil, errors.New("ARCHIE_LINK_PAGE_PAY_WINDOW must be a duration no longer than 2h")
		}
		window = parsed
	}
	pagePay, err := controlplane.NewPagePayService(service, client, controlplane.PagePayConfig{
		AppID: appID, NotifyURL: notifyURL, ReturnURL: returnURL, PayWindow: window,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("page pay enabled (production=%t, window=%s)", production, window)
	return pagePay, nil
}

// newNowPayService 装配三产品统一收款（Vibe Pay「即刻收」直连模式，
// alipay.aipay.nowpaydirect.*）。凭据复用 ARCHIE_LINK_PAY_* 的同一支付宝
// 应用；三个 ARCHIE_LINK_NOWPAY_*_PRODUCT_ID 均未配置时整块禁用。
func newNowPayService(service *controlplane.Service) (*controlplane.NowPayService, error) {
	products := map[string]string{
		"activation-task":  strings.TrimSpace(os.Getenv("ARCHIE_LINK_NOWPAY_ACTIVATION_PRODUCT_ID")),
		"perpetual":        strings.TrimSpace(os.Getenv("ARCHIE_LINK_NOWPAY_PERPETUAL_PRODUCT_ID")),
		"family-annual-12": strings.TrimSpace(os.Getenv("ARCHIE_LINK_NOWPAY_FAMILY_PRODUCT_ID")),
	}
	configured := false
	for _, productID := range products {
		if productID != "" {
			configured = true
		}
	}
	appID := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_APP_ID"))
	privateKey := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_PRIVATE_KEY"))
	alipayPublicKey := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_ALIPAY_PUBLIC_KEY"))
	if !configured {
		log.Printf("now pay disabled: ARCHIE_LINK_NOWPAY_*_PRODUCT_ID is not configured")
		return nil, nil
	}
	if appID == "" || privateKey == "" || alipayPublicKey == "" {
		return nil, errors.New("now pay requires ARCHIE_LINK_PAY_APP_ID, ARCHIE_LINK_PAY_PRIVATE_KEY and ARCHIE_LINK_PAY_ALIPAY_PUBLIC_KEY")
	}
	production := false
	if value := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_PRODUCTION")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, errors.New("ARCHIE_LINK_PAY_PRODUCTION must be true, false, 1 or 0")
		}
		production = parsed
	}
	gateway := strings.TrimSpace(os.Getenv("ARCHIE_LINK_PAY_GATEWAY"))
	if gateway == "" {
		if production {
			gateway = "https://openapi.alipay.com/gateway.do"
		} else {
			gateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
		}
	}
	client, err := aipay.New(aipay.Config{
		AppID:           appID,
		PrivateKey:      privateKey,
		AlipayPublicKey: alipayPublicKey,
		Gateway:         gateway,
		Production:      production,
	})
	if err != nil {
		return nil, err
	}
	window := 30 * time.Minute
	if value := strings.TrimSpace(os.Getenv("ARCHIE_LINK_NOWPAY_WINDOW")); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 || parsed > 2*time.Hour {
			return nil, errors.New("ARCHIE_LINK_NOWPAY_WINDOW must be a duration no longer than 2h")
		}
		window = parsed
	}
	nowPay, err := controlplane.NewNowPayService(service, client, controlplane.NowPayConfig{
		Products: products, CallbackURL: strings.TrimSpace(os.Getenv("ARCHIE_LINK_NOWPAY_CALLBACK_URL")),
		PayWindow: window,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("now pay enabled (production=%t, products=%d, window=%s)", production, len(products), window)
	return nowPay, nil
}
