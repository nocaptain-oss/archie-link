package linux

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

// Linux 健康检查集合：warp_trace（带重试）、google、deepseek、
// wireguard_interface、wireguard_handshake、tunnel_route、protected_route:*。
// 无 local_8688：Linux 无常驻本地服务承诺（见 docs/platform/LINUX.md）。
func (a *Automatic) Verify(ctx context.Context, ownership model.Ownership) (model.HealthReport, error) {
	report := checkHealthWithRetry(ctx, a)
	runner := commandRunner{a.runner}

	interfaceOK := ownership.Interface != ""
	if interfaceOK {
		if _, err := runner.Run(ctx, "ip", "link", "show", ownership.Interface); err != nil {
			interfaceOK = false
		}
	}
	report.Checks = append(report.Checks, healthCheck("wireguard_interface", interfaceOK, 0, nil))
	if !interfaceOK {
		report.OK = false
		if report.Failure == "" {
			report.Failure = "wireguard_interface_missing"
		}
	}

	handshakeOutput, handshakeErr := runner.Run(ctx, "wg", "show", ownership.Interface, "latest-handshakes")
	handshakeOK := ownership.Interface != "" && handshakeErr == nil && parseLatestHandshake(handshakeOutput)
	report.Checks = append(report.Checks, healthCheck("wireguard_handshake", handshakeOK, 0, handshakeErr))
	if !handshakeOK {
		report.OK = false
		if report.Failure == "" {
			report.Failure = "wireguard_handshake_missing"
		}
	}

	routeOutput, routeErr := runner.Run(ctx, "ip", "route", "get", "1.1.1.1")
	routeOK := routeErr == nil && ownership.Interface != "" &&
		strings.Contains(string(routeOutput), "dev "+ownership.Interface)
	report.Checks = append(report.Checks, healthCheck("tunnel_route", routeOK, 0, routeErr))
	if !routeOK {
		report.OK = false
		if report.Failure == "" {
			report.Failure = "route_not_via_tunnel"
		}
	}

	for _, host := range ownership.DirectHosts {
		hostRoute, hostErr := runner.Run(ctx, "ip", "route", "get", host)
		hostOK := hostErr == nil && ownership.Interface != "" &&
			!strings.Contains(string(hostRoute), "dev "+ownership.Interface)
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

func checkHealthWithRetry(ctx context.Context, a *Automatic) model.HealthReport {
	report := checkHealth(ctx)
	for attempt := 1; attempt < 3 && retryableHealthFailure(report.Failure); attempt++ {
		select {
		case <-ctx.Done():
			return report
		case <-time.After(750 * time.Millisecond):
		}
		report = checkHealth(ctx)
	}
	return report
}

func retryableHealthFailure(failure string) bool {
	return failure == "warp_trace_unreachable" ||
		failure == "google_unreachable" ||
		failure == "deepseek_unreachable"
}

func checkHealth(ctx context.Context) model.HealthReport {
	report := model.HealthReport{OK: true, Checks: []model.HealthCheck{}}

	status, body, err := httpGet(ctx, "https://1.1.1.1/cdn-cgi/trace")
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

	status, err = httpStatus(ctx, "https://www.google.com")
	googleOK := status == http.StatusOK || status == http.StatusMovedPermanently || status == http.StatusFound
	report.Checks = append(report.Checks, healthCheck("google", googleOK, status, err))
	if !googleOK {
		report.OK = false
		report.Failure = "google_unreachable"
		return report
	}

	status, err = httpStatus(ctx, "https://api.deepseek.com")
	deepseekOK := status > 0 && status != http.StatusForbidden
	report.Checks = append(report.Checks, healthCheck("deepseek", deepseekOK, status, err))
	if !deepseekOK {
		report.OK = false
		report.Failure = "deepseek_unreachable"
		if err == nil {
			report.Failure = "deepseek_forbidden"
		}
	}
	return report
}

func httpGet(ctx context.Context, target string) (int, string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
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

func httpStatus(ctx context.Context, target string) (int, error) {
	client := &http.Client{Timeout: 5 * time.Second}
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

func healthCheck(name string, ok bool, status int, err error) model.HealthCheck {
	check := model.HealthCheck{Name: name, OK: ok, Status: status}
	if err != nil {
		check.Error = err.Error()
	}
	return check
}
