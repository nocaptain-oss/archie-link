package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/entitlement"
	wireguardexport "github.com/nocaptain-oss/archie-link/internal/export"
)

// exportPrivateFeature 是完整导出（--include-private-key）/二维码（--format
// qrcode）所需的租约权益：仅 perpetual / family-annual-12 计划签发。
// 脱敏导出（默认 wireguard 格式，无私钥）保持免费。
const exportPrivateFeature = "export_private"

// requireExportPrivate 是完整导出/二维码的摩擦门禁（exit 4）。
// 门禁只挡 CLI/agent 便利路径，不锁定用户磁盘上的配置文件本身。
func requireExportPrivate(deps Dependencies, stdout, stderr io.Writer, jsonOutput bool) int {
	if deps.Lifecycle == nil || deps.Lifecycle.Entitlement == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 4, "entitlement_failed",
			errors.New("full export and QR export are paid entitlements (export_private, perpetual/family plans); run `archielink buy perpetual` or `archielink lease status`"))
	}
	if _, err := deps.Lifecycle.Entitlement.Verify(context.Background(), entitlement.Request{
		SkillID:   entitlement.SkillID,
		InstallID: deps.Lifecycle.InstallID,
		Feature:   exportPrivateFeature,
	}); err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 4, "entitlement_failed", err)
	}
	return 0
}

func runExport(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	format := ""
	jsonOutput := false
	includePrivate := false
	outputPath := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			i++
			if i >= len(args) {
				fmt.Fprintln(stderr, "--format requires a value")
				return 2
			}
			format = args[i]
		case "--json":
			jsonOutput = true
		case "--include-private-key":
			includePrivate = true
		case "--output":
			i++
			if i >= len(args) {
				fmt.Fprintln(stderr, "--output requires a value")
				return 2
			}
			outputPath = args[i]
		default:
			fmt.Fprintf(stderr, "unknown export option: %s\n", args[i])
			return 2
		}
	}
	if format != "wireguard" && format != "qrcode" {
		fmt.Fprintln(stderr, "export requires --format wireguard or --format qrcode")
		return 2
	}
	if format == "qrcode" {
		return runExportQrcode(jsonOutput, includePrivate, outputPath, deps, stdout, stderr)
	}
	if includePrivate && jsonOutput {
		fmt.Fprintln(stderr, "--include-private-key cannot be combined with --json: full config must not enter logs or agent output")
		return 2
	}
	if includePrivate && outputPath == "" {
		fmt.Fprintln(stderr, "--include-private-key requires --output <path>: full config must be written to a file, not stdout")
		return 2
	}
	if includePrivate {
		if code := requireExportPrivate(deps, stdout, stderr, jsonOutput); code != 0 {
			return code
		}
	}
	if deps.Lifecycle == nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "config_not_configured",
			errors.New("WireGuard config path is not configured"))
	}
	path := strings.TrimSpace(deps.Lifecycle.ConfigPath)
	if path == "" {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "config_not_configured",
			errors.New("WireGuard config path is empty"))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "config_unreadable", err)
	}
	output, err := wireguardexport.WireGuard(raw, includePrivate)
	if err != nil {
		return writeLifecycleError(stdout, stderr, jsonOutput, 5, "config_invalid", err)
	}
	if outputPath != "" {
		if err := writeExportFile(outputPath, output, stderr); err != nil {
			return 2
		}
		if includePrivate {
			fmt.Fprintf(stderr, "full config written to %s (0600); transfer it only via a channel you choose, never paste it into chat or cloud storage\n", outputPath)
		}
		return 0
	}
	if jsonOutput {
		value := map[string]any{
			"schema_version":       "1",
			"format":               "wireguard",
			"private_key_included": includePrivate,
			"content":              string(output),
		}
		if err := writeJSON(stdout, value); err != nil {
			fmt.Fprintf(stderr, "encode export: %v\n", err)
			return 1
		}
		return 0
	}
	if _, err := stdout.Write(output); err != nil {
		fmt.Fprintf(stderr, "write export: %v\n", err)
		return 1
	}
	return 0
}

func runExportQrcode(jsonOutput, includePrivate bool, outputPath string, deps Dependencies, stdout, stderr io.Writer) int {
	if jsonOutput {
		fmt.Fprintln(stderr, "--format qrcode cannot be combined with --json: the QR code encodes the full config with private key and must not enter logs or agent output")
		return 2
	}
	if includePrivate {
		fmt.Fprintln(stderr, "--format qrcode cannot be combined with --include-private-key: the QR code is itself a full export")
		return 2
	}
	if outputPath == "" {
		fmt.Fprintln(stderr, "--format qrcode requires --output <path>: the QR PNG must be written to a file, not stdout")
		return 2
	}
	if code := requireExportPrivate(deps, stdout, stderr, false); code != 0 {
		return code
	}
	if deps.Lifecycle == nil {
		return writeLifecycleError(stdout, stderr, false, 5, "config_not_configured",
			errors.New("WireGuard config path is not configured"))
	}
	path := strings.TrimSpace(deps.Lifecycle.ConfigPath)
	if path == "" {
		return writeLifecycleError(stdout, stderr, false, 5, "config_not_configured",
			errors.New("WireGuard config path is empty"))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return writeLifecycleError(stdout, stderr, false, 5, "config_unreadable", err)
	}
	full, err := wireguardexport.WireGuard(raw, true)
	if err != nil {
		return writeLifecycleError(stdout, stderr, false, 5, "config_invalid", err)
	}
	png, err := wireguardexport.QRPNG(full)
	if err != nil {
		return writeLifecycleError(stdout, stderr, false, 5, "qrcode_encode_failed", err)
	}
	if err := writeExportFile(outputPath, png, stderr); err != nil {
		return 2
	}
	fmt.Fprintf(stderr, "QR code PNG written to %s (0600); open the image and scan it with the official WireGuard app; delete the file after import\n", outputPath)
	return 0
}

func writeExportFile(outputPath string, content []byte, stderr io.Writer) error {
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			fmt.Fprintf(stderr, "refusing to overwrite existing file: %s\n", outputPath)
			return err
		}
		fmt.Fprintf(stderr, "open export output: %v\n", err)
		return err
	}
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		fmt.Fprintf(stderr, "write export output: %v\n", err)
		return err
	}
	return nil
}
