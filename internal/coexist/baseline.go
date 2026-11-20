package coexist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

type HTTPChecker interface {
	Status(context.Context, string) (int, error)
}

type DefaultHTTPChecker struct {
	Client *http.Client
}

func (c DefaultHTTPChecker) Status(ctx context.Context, target string) (int, error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
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

type EndpointResult struct {
	ID       string          `json:"id"`
	OK       bool            `json:"ok"`
	Direct   int             `json:"direct_status,omitempty"`
	Upstream int             `json:"upstream_status,omitempty"`
	Findings []model.Finding `json:"findings"`
}

func RunBaseline(ctx context.Context, endpoints []ProtectedEndpoint, checker HTTPChecker) []EndpointResult {
	results := make([]EndpointResult, 0, len(endpoints))
	for _, endpoint := range endpoints {
		result := EndpointResult{ID: endpoint.ID, OK: true, Findings: []model.Finding{}}
		if endpoint.DirectURL != "" {
			status, err := checker.Status(ctx, endpoint.DirectURL)
			if err != nil {
				result.OK = false
				result.Findings = append(result.Findings, model.Finding{
					Code: "local_service_unreachable", Level: "error",
					Message: fmt.Sprintf("%s is unreachable: %v", endpoint.DirectURL, err),
				})
			} else {
				result.Direct = status
				if !matchesExpected(status, endpoint.Expected) {
					result.OK = false
					result.Findings = append(result.Findings, model.Finding{
						Code: "local_service_unreachable", Level: "error",
						Message: fmt.Sprintf("%s returned status %d", endpoint.DirectURL, status),
					})
				}
			}
		}
		if endpoint.UpstreamURL != "" {
			status, err := checker.Status(ctx, endpoint.UpstreamURL)
			if err != nil {
				result.OK = false
				result.Findings = append(result.Findings, model.Finding{
					Code: "local_proxy_upstream_breakage", Level: "error",
					Message: fmt.Sprintf("%s is unreachable: %v", endpoint.UpstreamURL, err),
				})
			} else {
				result.Upstream = status
				if !matchesExpected(status, endpoint.Expected) {
					result.OK = false
					result.Findings = append(result.Findings, model.Finding{
						Code: "local_proxy_upstream_breakage", Level: "error",
						Message: fmt.Sprintf("%s returned status %d", endpoint.UpstreamURL, status),
					})
				}
			}
		}
		if result.Findings == nil {
			result.Findings = []model.Finding{}
		}
		results = append(results, result)
	}
	return results
}

func matchesExpected(status int, expected string) bool {
	switch expected {
	case "not_403":
		return status >= 100 && status != http.StatusForbidden
	case "2xx":
		return status >= 200 && status < 300
	case "3xx":
		return status >= 300 && status < 400
	case "401":
		return status == http.StatusUnauthorized
	case "no_error":
		return status >= 100
	default:
		return false
	}
}

type endpointConfig struct {
	Endpoints []ProtectedEndpoint `json:"endpoints"`
}

func LoadEndpoints(path string) ([]ProtectedEndpoint, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var endpoints []ProtectedEndpoint
	if len(strings.TrimSpace(string(raw))) > 0 && strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		if err := json.Unmarshal(raw, &endpoints); err != nil {
			return nil, err
		}
	} else {
		var config endpointConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
		endpoints = config.Endpoints
	}
	if err := validateEndpoints(endpoints); err != nil {
		return nil, err
	}
	return endpoints, nil
}

func validateEndpoints(endpoints []ProtectedEndpoint) error {
	seen := map[string]struct{}{}
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint.ID) == "" {
			return fmt.Errorf("protected endpoint id is required")
		}
		if _, exists := seen[endpoint.ID]; exists {
			return fmt.Errorf("duplicate protected endpoint id: %s", endpoint.ID)
		}
		seen[endpoint.ID] = struct{}{}
		if !supportedEndpointKind(endpoint.Kind) {
			return fmt.Errorf("unsupported endpoint kind: %s", endpoint.Kind)
		}
		if endpoint.DirectURL == "" && endpoint.UpstreamURL == "" {
			return fmt.Errorf("endpoint %s requires direct_url or upstream_url", endpoint.ID)
		}
		switch endpoint.Expected {
		case "not_403", "2xx", "3xx", "401", "no_error":
		default:
			return fmt.Errorf("unsupported expected value for %s: %s", endpoint.ID, endpoint.Expected)
		}
	}
	return nil
}

func supportedEndpointKind(kind string) bool {
	switch kind {
	case "loopback_service", "local_proxy_upstream", "private_api", "public_api",
		"developer_callback", "payment", "identity", "streaming", "dlna_mdns", "user_defined":
		return true
	default:
		return false
	}
}
