package macos

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

type HTTPChecker interface {
	Get(context.Context, string) (int, string, error)
}

type HTTPStatusChecker interface {
	Status(context.Context, string) (int, error)
}

type DefaultHTTPChecker struct {
	Client *http.Client
}

func (c DefaultHTTPChecker) Get(ctx context.Context, target string) (int, string, error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(body), nil
}

func (c DefaultHTTPChecker) Status(ctx context.Context, target string) (int, error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	method := http.MethodHead
	if strings.Contains(target, "api.deepseek.com") {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func CheckHealth(ctx context.Context, checker HTTPChecker) model.HealthReport {
	report := model.HealthReport{OK: true, Checks: []model.HealthCheck{}}
	if checker == nil {
		checker = DefaultHTTPChecker{}
	}

	status, body, err := checker.Get(ctx, "https://1.1.1.1/cdn-cgi/trace")
	traceOK := err == nil && status >= 200 && status < 300 && strings.Contains(body, "warp=on")
	report.Checks = append(report.Checks, healthCheck("warp_trace", traceOK, status, err))
	if !traceOK {
		report.OK = false
		if err != nil || status < 200 || status >= 300 {
			report.Failure = "warp_trace_unreachable"
		} else {
			report.Failure = "warp_trace_off"
		}
		return report
	}

	status, err = checkerStatus(ctx, checker, "https://www.google.com")
	googleOK := status == http.StatusOK || status == http.StatusMovedPermanently || status == http.StatusFound
	report.Checks = append(report.Checks, healthCheck("google", googleOK, status, err))
	if !googleOK {
		report.OK = false
		report.Failure = "google_unreachable"
		return report
	}

	status, err = checkerStatus(ctx, checker, "https://api.deepseek.com")
	deepseekOK := status > 0 && status != http.StatusForbidden
	report.Checks = append(report.Checks, healthCheck("deepseek", deepseekOK, status, err))
	if !deepseekOK {
		report.OK = false
		if err != nil {
			report.Failure = "deepseek_unreachable"
		} else {
			report.Failure = "deepseek_forbidden"
		}
		return report
	}

	// 仅当本地回环服务在场时才要求其保持可达（FR-008）：
	// 与受保护端点注册表的检测语义一致，未运行本地服务的机器跳过该项。
	if probeLocalService(ctx) {
		status, err = checkerStatus(ctx, checker, "http://127.0.0.1:8688/responses")
		localOK := status > 0
		report.Checks = append(report.Checks, healthCheck("local_8688", localOK, status, err))
		if !localOK {
			report.OK = false
			report.Failure = "local_service_unreachable"
		}
	} else {
		report.Checks = append(report.Checks, healthCheck("local_8688_absent", true, 0, nil))
	}
	return report
}

// probeLocalService 探测 127.0.0.1:8688 是否有本地服务在场；
// 包级变量便于测试注入，默认实现做短超时 TCP 拨号。
var probeLocalService = defaultProbeLocalService

func defaultProbeLocalService(ctx context.Context) bool {
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:8688")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func checkerStatus(ctx context.Context, checker HTTPChecker, target string) (int, error) {
	if status, ok := injectStatusOverride(target); ok {
		return status, nil
	}
	if statusChecker, ok := checker.(HTTPStatusChecker); ok {
		return statusChecker.Status(ctx, target)
	}
	status, _, err := checker.Get(ctx, target)
	// The fallback GET may return a status plus a body-read error. The
	// status is still authoritative for reachability checks.
	return status, err
}

func (a *Automatic) Verify(ctx context.Context, ownership model.Ownership) (model.HealthReport, error) {
	report := checkHealthWithRetry(ctx, a.checker)
	processOK := false
	output, err := a.runner.Run(ctx, "pgrep", "-fl", "wireguard-go")
	if err == nil && strings.Contains(string(output), "wireguard-go") {
		processOK = true
	}
	report.Checks = append(report.Checks, healthCheck("wireguard_process", processOK, 0, err))
	if !processOK {
		report.OK = false
		if report.Failure == "" {
			report.Failure = "wireguard_process_missing"
		}
	}
	handshakeOutput, handshakeErr := a.runner.Run(ctx, "wg", "show", ownership.Interface, "latest-handshakes")
	handshakeOK := ownership.Interface != "" && handshakeErr == nil && parseLatestHandshake(handshakeOutput)
	report.Checks = append(report.Checks, healthCheck("wireguard_handshake", handshakeOK, 0, handshakeErr))
	if !handshakeOK {
		report.OK = false
		if report.Failure == "" {
			report.Failure = "wireguard_handshake_missing"
		}
	}
	if ownership.Interface == "" && report.OK {
		report.OK = false
		report.Failure = "ownership_missing"
	}
	routeOutput, routeErr := a.runner.Run(ctx, "route", "-n", "get", "1.1.1.1")
	routeOK := routeErr == nil && ownership.Interface != "" &&
		strings.Contains(string(routeOutput), "interface: "+ownership.Interface)
	report.Checks = append(report.Checks, healthCheck("tunnel_route", routeOK, 0, routeErr))
	if !routeOK {
		report.OK = false
		if report.Failure == "" {
			report.Failure = "route_not_via_tunnel"
		}
	}
	for _, host := range ownership.DirectHosts {
		hostRoute, hostErr := a.runner.Run(ctx, "route", "-n", "get", host)
		hostOK := hostErr == nil &&
			!strings.Contains(string(hostRoute), "interface: "+ownership.Interface)
		report.Checks = append(report.Checks, healthCheck("protected_route:"+host, hostOK, 0, hostErr))
		if !hostOK {
			report.OK = false
			if report.Failure == "" {
				report.Failure = "protected_route_missing"
			}
		}
	}
	return report, nil
}

func checkHealthWithRetry(ctx context.Context, checker HTTPChecker) model.HealthReport {
	report := CheckHealth(ctx, checker)
	for attempt := 1; attempt < 3 && retryableHealthFailure(report.Failure); attempt++ {
		select {
		case <-ctx.Done():
			return report
		case <-time.After(750 * time.Millisecond):
		}
		report = CheckHealth(ctx, checker)
	}
	return report
}

func retryableHealthFailure(failure string) bool {
	return failure == "warp_trace_unreachable" ||
		failure == "google_unreachable" ||
		failure == "deepseek_unreachable"
}

func healthCheck(name string, ok bool, status int, err error) model.HealthCheck {
	check := model.HealthCheck{Name: name, OK: ok, Status: status}
	if err != nil {
		check.Error = err.Error()
	}
	return check
}
