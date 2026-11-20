package coexist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBaselineChecksLocalPortAndUpstream(t *testing.T) {
	checker := FakeChecker{
		Statuses: map[string]int{
			"http://127.0.0.1:8688/responses": 405,
			"https://api.deepseek.com":        403,
		},
	}
	endpoint := ProtectedEndpoint{
		ID:               "cache-api",
		Kind:             "local_proxy_upstream",
		DirectURL:        "http://127.0.0.1:8688/responses",
		UpstreamURL:      "https://api.deepseek.com",
		Expected:         "not_403",
		BaselineRequired: true,
	}

	results := RunBaseline(context.Background(), []ProtectedEndpoint{endpoint}, checker)
	if len(results) != 1 || results[0].OK {
		t.Fatalf("results=%#v", results)
	}
	if !hasBaselineCode(results[0], "local_proxy_upstream_breakage") {
		t.Fatalf("result=%#v", results[0])
	}
}

func TestBaselineReportsUnreachableLocalPort(t *testing.T) {
	checker := FakeChecker{
		Errors: map[string]error{"http://127.0.0.1:8688": errors.New("connection refused")},
	}
	results := RunBaseline(context.Background(), []ProtectedEndpoint{{
		ID: "local", Kind: "loopback_service", DirectURL: "http://127.0.0.1:8688", Expected: "no_error",
	}}, checker)
	if results[0].OK || !hasBaselineCode(results[0], "local_service_unreachable") {
		t.Fatalf("result=%#v", results[0])
	}
}

func TestLoadEndpointsValidatesJSON(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(valid, []byte(`{"endpoints":[{"id":"api","kind":"public_api","upstream_url":"https://example.com","expected":"not_403","baseline_required":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoints, err := LoadEndpoints(valid)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints=%#v err=%v", endpoints, err)
	}

	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{"endpoints":[{"id":"","kind":"unknown","expected":"x"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEndpoints(invalid); err == nil {
		t.Fatal("expected validation error")
	}
}

type FakeChecker struct {
	Statuses map[string]int
	Errors   map[string]error
}

func (f FakeChecker) Status(_ context.Context, url string) (int, error) {
	if err := f.Errors[url]; err != nil {
		return 0, err
	}
	status, ok := f.Statuses[url]
	if !ok {
		return 0, errors.New("unexpected URL: " + url)
	}
	return status, nil
}

func hasBaselineCode(result EndpointResult, code string) bool {
	for _, finding := range result.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
