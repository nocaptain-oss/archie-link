package model

import (
	"encoding/json"
	"testing"
)

func TestDoctorReportSchema(t *testing.T) {
	report := DoctorReport{
		SchemaVersion: "1",
		Mode:          ModeGuided,
		Platform:      PlatformReport{OS: "darwin", Version: "12.7.6", Arch: "amd64"},
		Capabilities:  CapabilityReport{Shell: true},
		Findings: []Finding{{
			Code:  "dns_timeout",
			Level: "warning",
		}},
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["schema_version"] != "1" || got["mode"] != "guided" {
		t.Fatalf("unexpected JSON: %s", raw)
	}
}
