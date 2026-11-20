package doctor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

type Prober interface {
	Probe(context.Context) []model.Finding
}

type ProbeTargets struct {
	DNSHost   string
	External  []string
	LocalTCP  []string
	Timeout   time.Duration
	Transport http.RoundTripper
}

func DefaultProbeTargets() ProbeTargets {
	return ProbeTargets{
		DNSHost:  "www.google.com",
		External: []string{"https://www.google.com", "https://github.com"},
		Timeout:  5 * time.Second,
	}
}

func (t ProbeTargets) Probe(ctx context.Context) []model.Finding {
	if t.Timeout <= 0 {
		t.Timeout = 5 * time.Second
	}
	var findings []model.Finding

	if t.DNSHost != "" {
		dnsCtx, cancel := context.WithTimeout(ctx, t.Timeout)
		_, err := net.DefaultResolver.LookupHost(dnsCtx, t.DNSHost)
		cancel()
		if err != nil {
			findings = append(findings, model.Finding{
				Code:    "dns_timeout",
				Level:   "error",
				Message: fmt.Sprintf("DNS lookup failed for %s: %v", t.DNSHost, err),
			})
		}
	}

	client := &http.Client{Timeout: t.Timeout, Transport: t.Transport}
	for _, target := range t.External {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			findings = append(findings, model.Finding{
				Code:    "foreign_endpoint_unreachable",
				Level:   "error",
				Message: fmt.Sprintf("%s is unreachable: %v", target, err),
			})
			continue
		}
		_ = resp.Body.Close()
	}

	for _, target := range t.LocalTCP {
		dialer := net.Dialer{Timeout: t.Timeout}
		conn, err := dialer.DialContext(ctx, "tcp", target)
		if err != nil {
			findings = append(findings, model.Finding{
				Code:    "local_service_unreachable",
				Level:   "error",
				Message: fmt.Sprintf("local service %s is unreachable: %v", target, err),
			})
			continue
		}
		_ = conn.Close()
	}

	return dedupe(findings)
}
