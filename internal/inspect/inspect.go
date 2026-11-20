// Package inspect 承载平台检测的报告构建，供 CLI 输出与远程只读
// RPC 共用，保证远程 JSON schema 与本地 doctor 完全一致。
package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/doctor"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

// Registry 是检测所需的最小平台注册表接口。
type Registry interface {
	For(goos string) (adapter.Adapter, error)
}

// Inspected 是一次成功检测的完整快照。
type Inspected struct {
	Platform     model.PlatformReport
	Capabilities model.CapabilityReport
	Network      model.NetworkReport
	Mode         model.Mode
}

// Inspect 执行平台检测与网络快照并判定支持模式。
func Inspect(ctx context.Context, registry Registry, goos string) (Inspected, error) {
	if registry == nil {
		return Inspected{}, errors.New("adapter registry is not configured")
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	platformAdapter, err := registry.For(goos)
	if err != nil {
		return Inspected{}, err
	}
	platform, capabilities, err := platformAdapter.Detect(ctx)
	if err != nil {
		return Inspected{}, err
	}
	network, err := platformAdapter.Snapshot(ctx)
	if err != nil {
		return Inspected{}, err
	}
	return Inspected{
		Platform:     platform,
		Capabilities: capabilities,
		Network:      network,
		Mode:         adapter.SelectMode(capabilities),
	}, nil
}

// DetectReport 构建与本地 detect 命令一致的报告。
func (s Inspected) DetectReport() model.DetectReport {
	return model.DetectReport{
		SchemaVersion: model.SchemaVersion,
		Mode:          s.Mode,
		Platform:      s.Platform,
		Capabilities:  s.Capabilities,
		Network:       s.Network,
		Result:        ResultForMode(s.Mode),
	}
}

// PlanReport 构建与本地 plan 命令一致的报告（含 doctor 分析阻塞项）。
func (s Inspected) PlanReport() model.PlanReport {
	doctorView := s.DoctorView()
	blocked := doctor.Analyze(doctorView)
	return model.PlanReport{
		SchemaVersion: model.SchemaVersion,
		Mode:          s.Mode,
		Platform:      s.Platform,
		Actions:       ActionsForMode(s.Mode),
		Blocked:       blocked,
		Permissions:   PermissionsForMode(s.Mode),
		Result:        ResultForMode(s.Mode),
	}
}

// DoctorReport 构建与本地 doctor 命令一致的报告；prober 非空且平台
// 非 unsupported 时合并探测 findings（本地探测与远程探测结果同构）。
func (s Inspected) DoctorReport(prober doctor.Prober) model.DoctorReport {
	report := s.DoctorView()
	findings := doctor.Analyze(report)
	if prober != nil && s.Mode != model.ModeUnsupported {
		findings = MergeFindings(findings, prober.Probe(context.Background()))
	}
	report.Findings = findings
	return report
}

// DoctorView 构建不含 findings 的 doctor 基础视图。
func (s Inspected) DoctorView() model.DoctorReport {
	return model.DoctorReport{
		SchemaVersion: model.SchemaVersion,
		Mode:          s.Mode,
		Platform:      s.Platform,
		Capabilities:  s.Capabilities,
		Network:       s.Network,
		Findings:      []model.Finding{},
	}
}

// ResultForMode 将支持模式映射为稳定结果值。
func ResultForMode(mode model.Mode) model.Result {
	if mode == model.ModeUnsupported {
		return model.ResultUnsupported
	}
	return model.ResultReady
}

// ActionsForMode 返回各支持模式的建议动作。
func ActionsForMode(mode model.Mode) []model.Action {
	switch mode {
	case model.ModeAuto:
		return []model.Action{
			{Code: "review_findings", Message: "Review the read-only findings before any network change."},
			{Code: "prepare_safe_setup", Message: "The host is eligible for the guarded setup planned in V0.3."},
		}
	case model.ModeGuided:
		return []model.Action{
			{Code: "guided_setup", Message: "Use guided instructions because privileged network mutation is unavailable."},
		}
	default:
		return []model.Action{
			{Code: "unsupported", Message: "Use another host or a manual diagnostic path."},
		}
	}
}

// PermissionsForMode 返回各支持模式所需权限说明。
func PermissionsForMode(mode model.Mode) []string {
	switch mode {
	case model.ModeAuto:
		return []string{"network_diagnostics", "future_elevation_required"}
	case model.ModeGuided:
		return []string{"network_diagnostics", "manual_user_action_required"}
	default:
		return []string{}
	}
}

// MergeFindings 按错误码去重合并 findings 组。
func MergeFindings(groups ...[]model.Finding) []model.Finding {
	seen := map[string]struct{}{}
	var result []model.Finding
	for _, group := range groups {
		for _, finding := range group {
			if _, ok := seen[finding.Code]; ok {
				continue
			}
			seen[finding.Code] = struct{}{}
			result = append(result, finding)
		}
	}
	return result
}

// RemoteProvider 将本地检测实现适配为 remote.Provider 白名单 RPC。
// serve 端注入后，远程调用与本地命令输出同一 schema。
type RemoteProvider struct {
	Registry Registry
	GOOS     string
	Prober   doctor.Prober
	Version  string
	Now      func() time.Time
}

func (p *RemoteProvider) inspected(ctx context.Context) (Inspected, error) {
	return Inspect(ctx, p.Registry, p.GOOS)
}

// Hello 返回目标端应用摘要（无检测副作用）。
func (p *RemoteProvider) Hello(ctx context.Context) (json.RawMessage, error) {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	state, err := p.inspected(ctx)
	if err != nil {
		return nil, err
	}
	payload := struct {
		SchemaVersion string `json:"schema_version"`
		App           string `json:"app"`
		Version       string `json:"version"`
		GOOS          string `json:"goos"`
		Platform      string `json:"platform"`
		Time          string `json:"time"`
	}{
		SchemaVersion: model.SchemaVersion,
		App:           "archielink",
		Version:       p.Version,
		GOOS:          state.Platform.OS,
		Platform:      state.Platform.OS + " " + state.Platform.Version,
		Time:          now().UTC().Format(time.RFC3339),
	}
	return json.Marshal(payload)
}

// Capabilities 返回平台能力报告。
func (p *RemoteProvider) Capabilities(ctx context.Context) (json.RawMessage, error) {
	state, err := p.inspected(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		SchemaVersion string                  `json:"schema_version"`
		Capabilities  model.CapabilityReport  `json:"capabilities"`
	}{model.SchemaVersion, state.Capabilities})
}

// Detect 返回与本地 detect 一致的报告。
func (p *RemoteProvider) Detect(ctx context.Context) (json.RawMessage, error) {
	state, err := p.inspected(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(state.DetectReport())
}

// Plan 返回与本地 plan 一致的报告。
func (p *RemoteProvider) Plan(ctx context.Context) (json.RawMessage, error) {
	state, err := p.inspected(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(state.PlanReport())
}

// Doctor 返回与本地 doctor 一致的报告（含探测 findings）。
func (p *RemoteProvider) Doctor(ctx context.Context) (json.RawMessage, error) {
	state, err := p.inspected(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(state.DoctorReport(p.Prober))
}
