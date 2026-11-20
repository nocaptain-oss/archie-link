package macos

import (
	"context"
	"errors"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestHealthRejectsCloudFront403(t *testing.T) {
	checker := FakeHTTPChecker{
		Responses: map[string]FakeHTTPResponse{
			"https://1.1.1.1/cdn-cgi/trace":   {Status: 200, Body: "warp=on\n"},
			"https://www.google.com":          {Status: 200},
			"https://api.deepseek.com":        {Status: 403},
			"http://127.0.0.1:8688/responses": {Status: 405},
		},
	}
	report := CheckHealth(context.Background(), checker)
	if report.OK || report.Failure != "deepseek_forbidden" {
		t.Fatalf("report=%#v", report)
	}
}

func TestHealthRequiresWarpTrace(t *testing.T) {
	checker := FakeHTTPChecker{
		Responses: map[string]FakeHTTPResponse{
			"https://1.1.1.1/cdn-cgi/trace":   {Status: 200, Body: "warp=off\n"},
			"https://www.google.com":          {Status: 200},
			"https://api.deepseek.com":        {Status: 200},
			"http://127.0.0.1:8688/responses": {Status: 405},
		},
	}
	report := CheckHealth(context.Background(), checker)
	if report.OK || report.Failure != "warp_trace_off" {
		t.Fatalf("report=%#v", report)
	}
}

func TestVerifyRequiresOwnedWireGuardProcess(t *testing.T) {
	withLocalServicePresent(t, true)
	dir := t.TempDir()
	runner := &ScriptedRunner{
		responses: map[string][]byte{
			"pgrep -fl wireguard-go": nil,
		},
		fail: map[string]error{
			"pgrep -fl wireguard-go": errors.New("not found"),
		},
	}
	adapter := NewAutomatic(runner, AutomaticOptions{StateDir: dir})
	adapter.checker = FakeHTTPChecker{
		Responses: map[string]FakeHTTPResponse{
			"https://1.1.1.1/cdn-cgi/trace":   {Status: 200, Body: "warp=on\n"},
			"https://www.google.com":          {Status: 200},
			"https://api.deepseek.com":        {Status: 200},
			"http://127.0.0.1:8688/responses": {Status: 405},
		},
	}
	report, err := adapter.Verify(context.Background(), model.Ownership{Interface: "utun12"})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Failure != "wireguard_process_missing" {
		t.Fatalf("report=%#v", report)
	}
}

func TestVerifyReportsRouteNotViaTunnel(t *testing.T) {
	withLocalServicePresent(t, true)
	dir := t.TempDir()
	runner := &ScriptedRunner{responses: map[string][]byte{
		"pgrep -fl wireguard-go":           []byte("123 wireguard-go utun\n"),
		"wg show utun12 latest-handshakes": []byte("123 456\n"),
		"route -n get 1.1.1.1":             []byte("gateway: 192.168.1.1\ninterface: en0\n"),
	}}
	adapter := NewAutomatic(runner, AutomaticOptions{StateDir: dir})
	adapter.checker = FakeHTTPChecker{
		Responses: map[string]FakeHTTPResponse{
			"https://1.1.1.1/cdn-cgi/trace":   {Status: 200, Body: "warp=on\n"},
			"https://www.google.com":          {Status: 200},
			"https://api.deepseek.com":        {Status: 200},
			"http://127.0.0.1:8688/responses": {Status: 405},
		},
	}
	report, err := adapter.Verify(context.Background(), model.Ownership{Interface: "utun12"})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Failure != "route_not_via_tunnel" {
		t.Fatalf("report=%#v", report)
	}
}

func TestHealthUsesStatusWhenBodyReadTimesOut(t *testing.T) {
	withLocalServicePresent(t, true)
	checker := FakeHTTPChecker{
		Responses: map[string]FakeHTTPResponse{
			"https://1.1.1.1/cdn-cgi/trace": {Status: 200, Body: "warp=on\n"},
			"https://www.google.com":        {Status: 200, Err: context.DeadlineExceeded},
			"https://api.deepseek.com":      {Status: 401, Err: context.DeadlineExceeded},
			"http://127.0.0.1:8688/responses": {
				Status: 405,
				Err:    context.DeadlineExceeded,
			},
		},
	}
	report := CheckHealth(context.Background(), checker)
	if !report.OK {
		t.Fatalf("status code should be sufficient: %#v", report)
	}
}

func TestVerifyRejectsProtectedHostRouteThroughTunnel(t *testing.T) {
	withLocalServicePresent(t, true)
	runner := &ScriptedRunner{responses: map[string][]byte{
		"pgrep -fl wireguard-go":           []byte("123 wireguard-go utun\n"),
		"wg show utun12 latest-handshakes": []byte("123 456\n"),
		"route -n get 1.1.1.1":             []byte("interface: utun12\n"),
		"route -n get 198.51.100.10":       []byte("gateway: 192.168.1.1\ninterface: utun12\n"),
	}}
	adapter := NewAutomatic(runner, AutomaticOptions{StateDir: t.TempDir()})
	adapter.checker = FakeHTTPChecker{
		Responses: map[string]FakeHTTPResponse{
			"https://1.1.1.1/cdn-cgi/trace":   {Status: 200, Body: "warp=on\n"},
			"https://www.google.com":          {Status: 200},
			"https://api.deepseek.com":        {Status: 401},
			"http://127.0.0.1:8688/responses": {Status: 405},
		},
	}
	report, err := adapter.Verify(context.Background(), model.Ownership{
		Interface:   "utun12",
		DirectHosts: []string{"198.51.100.10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Failure != "protected_route_missing" {
		t.Fatalf("report=%#v", report)
	}
}

// TestHealthSkipsLocalServiceWhenAbsent 回归 V1.0 切片 2 缺陷：
// 本地 8688 服务不在场时健康检查必须整体通过（跳过 local 项），
// 否则未运行本地缓存服务的机器 up 永远失败回滚。
func TestHealthSkipsLocalServiceWhenAbsent(t *testing.T) {
	withLocalServicePresent(t, false)
	checker := FakeHTTPChecker{
		Responses: map[string]FakeHTTPResponse{
			"https://1.1.1.1/cdn-cgi/trace": {Status: 200, Body: "warp=on\n"},
			"https://www.google.com":        {Status: 200},
			"https://api.deepseek.com":      {Status: 200},
			// 故意不提供 8688 响应：若实现仍查询该项，FakeHTTPChecker 会报 unexpected target。
		},
	}
	report := CheckHealth(context.Background(), checker)
	if !report.OK {
		t.Fatalf("absent local service must not fail health: %#v", report)
	}
	found := false
	for _, check := range report.Checks {
		if check.Name == "local_8688_absent" {
			found = true
		}
		if check.Name == "local_8688" {
			t.Fatalf("absent local service must skip the local_8688 check: %#v", report)
		}
	}
	if !found {
		t.Fatalf("expected local_8688_absent marker: %#v", report)
	}
}

func TestHealthRequiresLocalServiceWhenPresent(t *testing.T) {
	withLocalServicePresent(t, true)
	checker := FakeHTTPChecker{
		Responses: map[string]FakeHTTPResponse{
			"https://1.1.1.1/cdn-cgi/trace": {Status: 200, Body: "warp=on\n"},
			"https://www.google.com":        {Status: 200},
			"https://api.deepseek.com":      {Status: 200},
			"http://127.0.0.1:8688/responses": {
				Status: 0,
				Err:    errors.New("connection refused"),
			},
		},
	}
	report := CheckHealth(context.Background(), checker)
	if report.OK || report.Failure != "local_service_unreachable" {
		t.Fatalf("report=%#v", report)
	}
}

// withLocalServicePresent 固定本地服务在场性探测结果，消除对宿主机 8688 端口环境的依赖。
func withLocalServicePresent(t *testing.T, present bool) {
	t.Helper()
	original := probeLocalService
	probeLocalService = func(context.Context) bool { return present }
	t.Cleanup(func() { probeLocalService = original })
}

type FakeHTTPResponse struct {
	Status int
	Body   string
	Err    error
}

type FakeHTTPChecker struct {
	Responses map[string]FakeHTTPResponse
}

func (f FakeHTTPChecker) Get(_ context.Context, target string) (int, string, error) {
	response, ok := f.Responses[target]
	if !ok {
		return 0, "", errors.New("unexpected target: " + target)
	}
	return response.Status, response.Body, response.Err
}
