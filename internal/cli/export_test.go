package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/entitlement"
	appstate "github.com/nocaptain-oss/archie-link/internal/state"
)

// exportTestDeps 返回免费层依赖：无 Entitlement，完整导出/二维码应 exit 4。
func exportTestDeps(t *testing.T) (Dependencies, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "warp.conf")
	if err := os.WriteFile(path, []byte("[Interface]\nPrivateKey = secret\n\n[Peer]\nPublicKey = public\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Dependencies{Lifecycle: &LifecycleDependencies{
		Store:      appstate.Store{Dir: dir},
		ConfigPath: path,
	}}, dir
}

// paidExportTestDeps 返回带 export_private 权益的依赖（永久/家庭版语义）。
func paidExportTestDeps(t *testing.T) (Dependencies, string) {
	t.Helper()
	deps, dir := exportTestDeps(t)
	deps.Lifecycle.Entitlement = entitlement.NewTestProvider(nil)
	deps.Lifecycle.InstallID = "test-install"
	return deps, dir
}

// taskExportTestDeps 返回只有 network_apply 的按次租约语义：无 export_private。
func taskExportTestDeps(t *testing.T) (Dependencies, string) {
	t.Helper()
	deps, dir := exportTestDeps(t)
	deps.Lifecycle.Entitlement = entitlement.NewTestProvider(func(e *entitlement.Entitlement) {
		e.Features = []string{"network_apply"}
	})
	deps.Lifecycle.InstallID = "test-install"
	return deps, dir
}

func TestExportMasksPrivateKeyByDefault(t *testing.T) {
	deps, _ := exportTestDeps(t)
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "wireguard", "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	content, _ := got["content"].(string)
	if strings.Contains(content, "secret") || !strings.Contains(content, "<redacted>") {
		t.Fatalf("content=%q", content)
	}
}

func TestExportFullConfigRequiresOutputFile(t *testing.T) {
	deps, _ := exportTestDeps(t)
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "wireguard", "--include-private-key"}, &stdout, &stderr, deps)
	if code != 2 || !strings.Contains(stderr.String(), "--output") {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "secret") {
		t.Fatalf("private key leaked to stdout")
	}
}

func TestExportFullConfigRejectsJSON(t *testing.T) {
	deps, _ := exportTestDeps(t)
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "wireguard", "--include-private-key", "--output", "/dev/null", "--json"}, &stdout, &stderr, deps)
	if code != 2 || !strings.Contains(stderr.String(), "--json") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "secret") {
		t.Fatalf("private key leaked to stdout")
	}
}

func TestExportFullConfigWritesFileWith0600(t *testing.T) {
	deps, dir := paidExportTestDeps(t)
	target := filepath.Join(dir, "export.conf")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "wireguard", "--include-private-key", "--output", target}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v", info.Mode().Perm())
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "secret") {
		t.Fatalf("exported file missing private key: %q", raw)
	}
	if strings.Contains(stdout.String(), "secret") {
		t.Fatalf("private key leaked to stdout")
	}
}

func TestExportRefusesToOverwriteExistingFile(t *testing.T) {
	deps, dir := paidExportTestDeps(t)
	target := filepath.Join(dir, "existing.conf")
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "wireguard", "--include-private-key", "--output", target}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "existing" {
		t.Fatalf("existing file was overwritten: %q", raw)
	}
}

func TestExportRedactedToOutputFile(t *testing.T) {
	deps, dir := exportTestDeps(t)
	target := filepath.Join(dir, "redacted.conf")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "wireguard", "--output", target}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || !strings.Contains(string(raw), "<redacted>") {
		t.Fatalf("content=%q", raw)
	}
}

func TestExportQrcodeRequiresOutput(t *testing.T) {
	deps, _ := exportTestDeps(t)
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "qrcode"}, &stdout, &stderr, deps)
	if code != 2 || !strings.Contains(stderr.String(), "--output") {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
}

func TestExportQrcodeRejectsJSON(t *testing.T) {
	deps, dir := exportTestDeps(t)
	target := filepath.Join(dir, "qr.png")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "qrcode", "--output", target, "--json"}, &stdout, &stderr, deps)
	if code != 2 || !strings.Contains(stderr.String(), "--json") {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("QR PNG should not be written, err=%v", err)
	}
}

func TestExportQrcodeRejectsIncludePrivateKey(t *testing.T) {
	deps, dir := exportTestDeps(t)
	target := filepath.Join(dir, "qr.png")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "qrcode", "--include-private-key", "--output", target}, &stdout, &stderr, deps)
	if code != 2 || !strings.Contains(stderr.String(), "--include-private-key") {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("QR PNG should not be written, err=%v", err)
	}
}

func TestExportQrcodeWritesPNGWith0600(t *testing.T) {
	deps, dir := paidExportTestDeps(t)
	target := filepath.Join(dir, "qr.png")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "qrcode", "--output", target}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v", info.Mode().Perm())
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 8 || string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("not a PNG file: %q", raw[:8])
	}
	if strings.Contains(stdout.String(), "secret") {
		t.Fatalf("private key leaked to stdout")
	}
}

func TestExportQrcodeRefusesOverwrite(t *testing.T) {
	deps, dir := paidExportTestDeps(t)
	target := filepath.Join(dir, "existing.png")
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "qrcode", "--output", target}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "existing" {
		t.Fatalf("existing file was overwritten: %q", raw)
	}
}

func TestExportQrcodeDecodesToFullConfig(t *testing.T) {
	deps, dir := paidExportTestDeps(t)
	target := filepath.Join(dir, "qr.png")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "qrcode", "--output", target}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	png, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	img, err := pngToImage(png)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeQR(img)
	if err != nil {
		t.Fatalf("QR decode failed (PNG would be unscannable): %v", err)
	}
	if !strings.Contains(decoded, "PrivateKey = secret") {
		t.Fatalf("decoded QR missing private key: %q", decoded)
	}
	if !strings.Contains(decoded, "[Interface]") || !strings.Contains(decoded, "[Peer]") {
		t.Fatalf("decoded QR missing sections: %q", decoded)
	}
}

// ---- export_private 门禁行为矩阵 ----

// 免费层（无 Entitlement）：完整导出/二维码 exit 4，脱敏导出免费可用。
func TestExportFullConfigGateWithoutEntitlement(t *testing.T) {
	deps, dir := exportTestDeps(t)
	target := filepath.Join(dir, "full.conf")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "wireguard", "--include-private-key", "--output", target}, &stdout, &stderr, deps)
	if code != 4 || !strings.Contains(stderr.String(), "entitlement_failed") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("full config must not be written, err=%v", err)
	}
	// 脱敏导出保持免费。
	code = RunWithDeps([]string{"export", "--format", "wireguard"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("redacted export must stay free, exit=%d stderr=%s", code, stderr.String())
	}
}

func TestExportQrcodeGateWithoutEntitlement(t *testing.T) {
	deps, dir := exportTestDeps(t)
	target := filepath.Join(dir, "qr.png")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "qrcode", "--output", target}, &stdout, &stderr, deps)
	if code != 4 || !strings.Contains(stderr.String(), "entitlement_failed") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("QR PNG must not be written, err=%v", err)
	}
}

// 按次租约（只有 network_apply）：不签发 export_private，exit 4。
func TestExportFullConfigGateWithActivationTaskLease(t *testing.T) {
	deps, dir := taskExportTestDeps(t)
	target := filepath.Join(dir, "full.conf")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "wireguard", "--include-private-key", "--output", target}, &stdout, &stderr, deps)
	if code != 4 {
		t.Fatalf("activation-task lease must not grant full export, exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("full config must not be written, err=%v", err)
	}
}

func TestExportQrcodeGateWithActivationTaskLease(t *testing.T) {
	deps, dir := taskExportTestDeps(t)
	target := filepath.Join(dir, "qr.png")
	var stdout, stderr bytes.Buffer
	code := RunWithDeps([]string{"export", "--format", "qrcode", "--output", target}, &stdout, &stderr, deps)
	if code != 4 {
		t.Fatalf("activation-task lease must not grant QR export, exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("QR PNG must not be written, err=%v", err)
	}
}

// 门禁在参数校验（exit 2）之后、配置检查（exit 5）之前：
// 参数错误优先于权益检查。
func TestExportGateOrderAfterParamValidation(t *testing.T) {
	deps, _ := exportTestDeps(t)
	var stdout, stderr bytes.Buffer
	// --include-private-key 无 --output：exit 2（参数错误优先于门禁）。
	code := RunWithDeps([]string{"export", "--format", "wireguard", "--include-private-key"}, &stdout, &stderr, deps)
	if code != 2 || !strings.Contains(stderr.String(), "--output") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	// 二维码缺 --output：exit 2。
	code = RunWithDeps([]string{"export", "--format", "qrcode"}, &stdout, &stderr, deps)
	if code != 2 || !strings.Contains(stderr.String(), "--output") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}
