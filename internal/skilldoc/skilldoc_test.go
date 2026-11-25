package skilldoc

import (
	"os"
	"strings"
	"testing"
)

// 12KB 上限是内部防膨胀跳线（SKILL.md 每次会话注入 agent 上下文，体积 = token 成本），
// 不是平台硬限制：Codex quick_validate 只查 description ≤1024 字符，SkillHub 上架无大小门槛。
// 历史注记：V0.2 曾自设 8KB 按字节强制，2026-11-25 经用户裁决放宽——
// 硬压缩伤害内容质量（Prerequisites/remote/kill-switch 经验均为承重内容），无平台依据。
const maxSkillBytes = 12 * 1024

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
	if len(raw) > maxSkillBytes {
		t.Fatalf("SKILL.md is %d bytes; keep it under 12KB", len(raw))
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
	if len(raw) > maxSkillBytes {
		t.Fatalf("SKILL.md is %d bytes; keep it under 12KB", len(raw))
	}
}
