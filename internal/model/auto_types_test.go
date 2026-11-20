package model

import (
	"encoding/json"
	"testing"
)

func TestApplyAndHealthJSON(t *testing.T) {
	plan := ApplyPlan{
		Endpoint:          Endpoint{Address: "188.114.97.1", Port: 500},
		LocalDNSDomains:   []string{"api.deepseek.com"},
		ProtectionDomains: []string{"api.skillhub.cn"},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["endpoint"] == nil || got["local_dns_domains"] == nil || got["protection_domains"] == nil {
		t.Fatalf("unexpected JSON: %s", raw)
	}
}

func TestLifecycleStatusJSON(t *testing.T) {
	status := StatusReport{
		SchemaVersion: SchemaVersion,
		Phase:         "active",
		Connected:     true,
		Healthy:       true,
		Interface:     "utun12",
		Endpoint:      Endpoint{Address: "188.114.97.1", Port: 500},
		Checks:        []HealthCheck{},
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["phase"] != "active" || got["connected"] != true || got["endpoint"] == nil {
		t.Fatalf("unexpected JSON: %s", raw)
	}
}
