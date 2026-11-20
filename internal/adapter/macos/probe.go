package macos

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

const probeInterface = "archieprobe"

type EndpointProbe interface {
	Probe(context.Context, model.Endpoint) (model.ProbeResult, error)
}

func DefaultProbePlan() model.ProbePlan {
	return model.ProbePlan{
		Candidates: []model.Endpoint{
			{Address: "162.159.192.1", Port: 2408},
			{Address: "162.159.193.1", Port: 2408},
			{Address: "188.114.96.1", Port: 500},
			{Address: "188.114.97.1", Port: 500},
			{Address: "188.114.98.1", Port: 500},
		},
		TimeoutSec: 10,
	}
}

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
		addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", "engage.cloudflareclient.com")
		if err == nil {
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

func RunProbePlan(ctx context.Context, plan model.ProbePlan, probe EndpointProbe) (model.ProbeResult, error) {
	if probe == nil {
		return model.ProbeResult{}, errors.New("probe is not configured")
	}
	if len(plan.Candidates) == 0 {
		return model.ProbeResult{}, errors.New("probe plan has no candidates")
	}
	timeout := time.Duration(plan.TimeoutSec) * time.Second
	if plan.TimeoutSec <= 0 {
		timeout = 10 * time.Second
	}
	if timeout > 10*time.Second {
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
		result, err := probe.Probe(probeCtx, endpoint)
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

type IsolatedProbe struct {
	Runner       adapter.Runner
	ConfigPath   string
	StateDir     string
	Timeout      time.Duration
	PollInterval time.Duration
}

func (p IsolatedProbe) Probe(ctx context.Context, endpoint model.Endpoint) (model.ProbeResult, error) {
	if p.Runner == nil {
		return model.ProbeResult{}, errors.New("probe runner is not configured")
	}
	if strings.TrimSpace(p.ConfigPath) == "" {
		return model.ProbeResult{}, errors.New("WireGuard source config is not configured")
	}
	if err := validateEndpoint(endpoint); err != nil {
		return model.ProbeResult{}, err
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	pollInterval := p.PollInterval
	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}
	stateDir := p.StateDir
	if stateDir == "" {
		stateDir = filepath.Dir(p.ConfigPath)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return model.ProbeResult{}, err
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return model.ProbeResult{}, err
	}

	source, err := os.ReadFile(p.ConfigPath)
	if err != nil {
		return model.ProbeResult{}, fmt.Errorf("read WireGuard config: %w", err)
	}
	config, err := probeConfig(source, endpoint)
	if err != nil {
		return model.ProbeResult{}, err
	}
	probePath := filepath.Join(stateDir, probeInterface+".conf")
	if err := os.WriteFile(probePath, config, 0o600); err != nil {
		return model.ProbeResult{}, err
	}
	if err := os.Chmod(probePath, 0o600); err != nil {
		_ = os.Remove(probePath)
		return model.ProbeResult{}, err
	}

	runner := commandRunner{runner: p.Runner}
	beforeInterfaces := interfaceSet(mustRun(p.Runner, ctx, "wg", "show", "interfaces"))
	if _, err := runner.Run(ctx, "wg-quick", "up", probePath); err != nil {
		_, _ = runner.Run(context.WithoutCancel(ctx), "wg-quick", "down", probePath)
		_ = os.Remove(probePath)
		return model.ProbeResult{}, fmt.Errorf("start isolated WireGuard interface: %w", err)
	}

	interfaceName := probeRealInterface(ctx, p.Runner, beforeInterfaces)
	handshake := waitForHandshake(ctx, p.Runner, interfaceName, timeout, pollInterval)
	_, downErr := runner.Run(context.WithoutCancel(ctx), "wg-quick", "down", probePath)
	_ = os.Remove(probePath)
	if downErr != nil {
		return model.ProbeResult{}, fmt.Errorf("stop isolated WireGuard interface: %w", downErr)
	}
	if !handshake {
		diagnostic, _ := runner.Run(ctx, "wg", "show", interfaceName)
		return model.ProbeResult{}, fmt.Errorf(
			"WireGuard handshake timed out at %s (interface=%s): %s",
			endpointKey(endpoint), interfaceName, strings.TrimSpace(string(diagnostic)),
		)
	}
	return model.ProbeResult{Endpoint: endpoint, Handshake: true}, nil
}

func waitForHandshake(
	ctx context.Context,
	runner adapter.Runner,
	interfaceName string,
	timeout time.Duration,
	pollInterval time.Duration,
) bool {
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

func probeRealInterface(ctx context.Context, runner adapter.Runner, before map[string]struct{}) string {
	if output, err := runner.Run(ctx, "cat", "/var/run/wireguard/"+probeInterface+".name"); err == nil {
		if name := strings.TrimSpace(string(output)); name != "" {
			return name
		}
	}
	if name := newInterface(before, mustRun(runner, ctx, "wg", "show", "interfaces")); name != "" {
		return name
	}
	return probeInterface
}

func mustRun(runner adapter.Runner, ctx context.Context, name string, args ...string) []byte {
	if runner == nil {
		return nil
	}
	output, _ := runner.Run(ctx, name, args...)
	return output
}

func interfaceSet(output []byte) map[string]struct{} {
	result := map[string]struct{}{}
	for _, field := range strings.Fields(string(output)) {
		result[field] = struct{}{}
	}
	return result
}

func newInterface(before map[string]struct{}, output []byte) string {
	for _, field := range strings.Fields(string(output)) {
		if _, exists := before[field]; !exists {
			return field
		}
	}
	return ""
}

func probeConfig(input []byte, endpoint model.Endpoint) ([]byte, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(input)))
	section := ""
	tableSeen := false
	endpointSeen := false
	lines := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if section == "Interface" && !tableSeen {
				lines = append(lines, "Table = off")
				tableSeen = true
			}
			if section == "Peer" && !endpointSeen {
				return nil, errors.New("WireGuard peer has no Endpoint")
			}
			section = strings.Trim(trimmed, "[]")
			lines = append(lines, line)
			continue
		}
		key, _, _ := strings.Cut(trimmed, "=")
		key = strings.TrimSpace(key)
		if err := rejectDangerousWireGuardKey(key); err != nil {
			return nil, err
		}
		switch section {
		case "Interface":
			if key == "DNS" {
				continue
			}
			if key == "Table" {
				lines = append(lines, "Table = off")
				tableSeen = true
				continue
			}
		case "Peer":
			if key == "Endpoint" {
				lines = append(lines, "Endpoint = "+endpointKey(endpoint))
				endpointSeen = true
				continue
			}
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if section == "Interface" && !tableSeen {
		lines = append(lines, "Table = off")
	}
	if section == "Peer" && !endpointSeen {
		return nil, errors.New("WireGuard peer has no Endpoint")
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func validateEndpoint(endpoint model.Endpoint) error {
	ip := net.ParseIP(endpoint.Address)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("probe endpoint must be an IPv4 address: %q", endpoint.Address)
	}
	if endpoint.Port != 2408 && endpoint.Port != 500 {
		return fmt.Errorf("unsupported WireGuard probe port: %d", endpoint.Port)
	}
	return nil
}

func endpointKey(endpoint model.Endpoint) string {
	return net.JoinHostPort(endpoint.Address, strconv.Itoa(endpoint.Port))
}

func parseLatestHandshake(output []byte) bool {
	for _, field := range strings.Fields(string(output)) {
		value, err := strconv.ParseInt(field, 10, 64)
		if err == nil && value > 0 {
			return true
		}
	}
	return false
}
