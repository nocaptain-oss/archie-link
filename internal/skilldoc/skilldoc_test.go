package skilldoc

import (
	"os"
	"strings"
	"testing"
)

func TestDoctorSkillContract(t *testing.T) {
	raw, err := os.ReadFile("../../skills/archie-link-doctor/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"archielink detect --json",
		"archielink plan --json",
		"archielink doctor --json",
		"archielink coexist inspect --json",
		"archielink coexist gate --json",
		"auto",
		"guided",
		"unsupported",
		"must not modify DNS, routes, proxies, or VPN state",
		"unknown network ownership must not enter auto mode",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if len(raw) > 8*1024 {
		t.Fatalf("SKILL.md is %d bytes; keep it under 8KB", len(raw))
	}
}

func TestAutomaticSkillContract(t *testing.T) {
	raw, err := os.ReadFile("../../skills/archie-link/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"archielink detect --json",
		"archielink plan --json",
		"archielink up --yes --json",
		"archielink status --json",
		"archielink down --json",
		"archielink rollback --json",
		"guided",
		"unsupported",
		"must not route login, payment, or identity verification through WARP",
		"never kill wireguard-go directly",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if len(raw) > 8*1024 {
		t.Fatalf("SKILL.md is %d bytes; keep it under 8KB", len(raw))
	}
}
