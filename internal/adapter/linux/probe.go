package linux

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

// augmentProbePlan 用配置端点与 engage.cloudflareclient.com 兜底扩充候选，
// 去重后截断到 12 个，超时钳制在 10 秒内。
func augmentProbePlan(ctx context.Context, plan model.ProbePlan, configPath string) model.ProbePlan {
	candidates := []model.Endpoint{}
	add := func(endpoint model.Endpoint) {
		if validateEndpoint(endpoint) != nil {
			return
		}
		for _, existing := range candidates {
			if existing == endpoint {
				return
			}
		}
		candidates = append(candidates, endpoint)
	}

	configCandidateFound := false
	if raw, err := os.ReadFile(configPath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(raw)))
		section := ""
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.Trim(line, "[]")
				continue
			}
			if section != "Peer" {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok || strings.TrimSpace(key) != "Endpoint" {
				continue
			}
			value = strings.TrimSpace(value)
			host, portText, err := net.SplitHostPort(value)
			if err != nil {
				continue
			}
			port, err := strconv.Atoi(portText)
			if err != nil {
				continue
			}
			if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
				add(model.Endpoint{Address: ip.To4().String(), Port: port})
				configCandidateFound = true
			} else if addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", host); err == nil {
				for _, address := range addresses {
					for _, candidatePort := range []int{port, 2408, 500} {
						add(model.Endpoint{Address: address.String(), Port: candidatePort})
						configCandidateFound = true
					}
				}
			}
			break
		}
	}
	if !configCandidateFound {
		if addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", "engage.cloudflareclient.com"); err == nil {
			for _, address := range addresses {
				add(model.Endpoint{Address: address.String(), Port: 2408})
				add(model.Endpoint{Address: address.String(), Port: 500})
			}
		}
	}
	for _, candidate := range plan.Candidates {
		add(candidate)
	}
	if len(candidates) > 12 {
		candidates = candidates[:12]
	}
	if plan.TimeoutSec <= 0 || plan.TimeoutSec > 10 {
		plan.TimeoutSec = 10
	}
	plan.Candidates = candidates
	return plan
}

func runProbePlan(ctx context.Context, plan model.ProbePlan, probe func(context.Context, model.Endpoint) (model.ProbeResult, error)) (model.ProbeResult, error) {
	if len(plan.Candidates) == 0 {
		return model.ProbeResult{}, errors.New("probe plan has no candidates")
	}
	timeout := time.Duration(plan.TimeoutSec) * time.Second
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	seen := map[string]struct{}{}
	failures := make([]string, 0, len(plan.Candidates))
	for _, endpoint := range plan.Candidates {
		if err := validateEndpoint(endpoint); err != nil {
			return model.ProbeResult{}, err
		}
		key := endpointKey(endpoint)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		result, err := probe(probeCtx, endpoint)
		cancel()
		if err == nil && result.Handshake {
			return result, nil
		}
		if err == nil {
			err = errors.New("handshake was not observed")
		}
		failures = append(failures, key+": "+err.Error())
	}
	return model.ProbeResult{}, fmt.Errorf("all WireGuard probes failed: %s", strings.Join(failures, "; "))
}

func waitForHandshake(ctx context.Context, runner adapter.Runner, interfaceName string, timeout, pollInterval time.Duration) bool {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := runner.Run(ctx, "wg", "show", interfaceName, "latest-handshakes")
		if err == nil && parseLatestHandshake(output) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollInterval):
		}
	}
	return false
}

// isolatedProbe 起独立探针接口做真实握手测试：不装路由、不装地址，
// 探测结束彻底拆除。
func (a *Automatic) isolatedProbe(ctx context.Context, endpoint model.Endpoint) (model.ProbeResult, error) {
	if strings.TrimSpace(a.options.ConfigPath) == "" {
		return model.ProbeResult{}, errors.New("WireGuard source config is not configured")
	}
	stateDir := a.options.StateDir
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return model.ProbeResult{}, err
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return model.ProbeResult{}, err
	}
	source, err := os.ReadFile(a.options.ConfigPath)
	if err != nil {
		return model.ProbeResult{}, fmt.Errorf("read WireGuard config: %w", err)
	}
	config, _, err := buildSetConf(source, endpoint, 5)
	if err != nil {
		return model.ProbeResult{}, err
	}
	probePath := filepath.Join(stateDir, probeInterface+".setconf")
	if err := writeFilePrivate(probePath, config); err != nil {
		return model.ProbeResult{}, err
	}
	userspace, err := a.createWireguardInterface(ctx, probeInterface)
	if err != nil {
		_ = os.Remove(probePath)
		return model.ProbeResult{}, err
	}
	runner := commandRunner{a.runner}
	if _, err := runner.Run(ctx, "wg", "setconf", probeInterface, probePath); err != nil {
		_ = a.teardownWireguardInterface(context.WithoutCancel(ctx), probeInterface, userspace)
		_ = os.Remove(probePath)
		return model.ProbeResult{}, fmt.Errorf("configure isolated WireGuard interface: %w", err)
	}
	// kernel WireGuard 接口只有在 UP 状态才持有 UDP socket，
	// 不拉起接口则 keepalive 与握手都无法发出。
	if _, err := runner.Run(ctx, "ip", "link", "set", "dev", probeInterface, "up"); err != nil {
		_ = a.teardownWireguardInterface(context.WithoutCancel(ctx), probeInterface, userspace)
		_ = os.Remove(probePath)
		return model.ProbeResult{}, fmt.Errorf("bring probe interface up: %w", err)
	}
	handshake := waitForHandshake(ctx, a.runner, probeInterface, 10*time.Second, 200*time.Millisecond)
	teardownErr := a.teardownWireguardInterface(context.WithoutCancel(ctx), probeInterface, userspace)
	_ = os.Remove(probePath)
	if teardownErr != nil {
		return model.ProbeResult{}, fmt.Errorf("stop isolated WireGuard interface: %w", teardownErr)
	}
	if !handshake {
		return model.ProbeResult{}, fmt.Errorf(
			"WireGuard handshake timed out at %s (interface=%s)", endpointKey(endpoint), probeInterface)
	}
	return model.ProbeResult{Endpoint: endpoint, Handshake: true}, nil
}

func (a *Automatic) Probe(ctx context.Context, plan model.ProbePlan) (model.ProbeResult, error) {
	plan = augmentProbePlan(ctx, plan, a.options.ConfigPath)
	return runProbePlan(ctx, plan, a.isolatedProbe)
}
