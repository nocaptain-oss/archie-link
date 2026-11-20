package macos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestProbeSelectsFirstHealthyEndpoint(t *testing.T) {
	probe := FakeEndpointProbe{
		Healthy: map[string]bool{
			"162.159.192.1:2408": false,
			"188.114.97.1:500":   true,
		},
	}
	result, err := RunProbePlan(context.Background(), model.ProbePlan{
		Candidates: []model.Endpoint{
			{Address: "162.159.192.1", Port: 2408},
			{Address: "188.114.97.1", Port: 500},
		},
		TimeoutSec: 10,
	}, probe)
	if err != nil {
		t.Fatal(err)
	}
	if result.Endpoint.Address != "188.114.97.1" || result.Endpoint.Port != 500 {
		t.Fatalf("endpoint=%#v", result.Endpoint)
	}
}

func TestRunProbePlanRejectsNonCloudflarePort(t *testing.T) {
	probe := FakeEndpointProbe{Healthy: map[string]bool{"188.114.97.1:443": true}}
	_, err := RunProbePlan(context.Background(), model.ProbePlan{
		Candidates: []model.Endpoint{{Address: "188.114.97.1", Port: 443}},
		TimeoutSec: 10,
	}, probe)
	if err == nil {
		t.Fatal("expected unsupported probe port")
	}
}

func TestProbeConfigRemovesDNSAndDefaultRouteOwnership(t *testing.T) {
	input := `[Interface]
PrivateKey = private
Address = 172.16.0.2/32
DNS = 1.1.1.1
MTU = 1280
Table = auto

[Peer]
PublicKey = public
Endpoint = engage.cloudflareclient.com:2408
AllowedIPs = 0.0.0.0/0, ::/0
`
	output, err := probeConfig([]byte(input), model.Endpoint{Address: "188.114.97.1", Port: 500})
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if strings.Contains(text, "\nDNS =") {
		t.Fatalf("DNS remains in probe config:\n%s", text)
	}
	if !strings.Contains(text, "Table = off") {
		t.Fatalf("Table=off missing:\n%s", text)
	}
	if !strings.Contains(text, "Endpoint = 188.114.97.1:500") {
		t.Fatalf("endpoint not replaced:\n%s", text)
	}
	if strings.Contains(text, "PrivateKey = <redacted>") {
		t.Fatalf("probe config must preserve the source secret in memory: %s", text)
	}
}

func TestProbeConfigRejectsRootHooks(t *testing.T) {
	input := []byte(`[Interface]
PrivateKey = private
PostUp = /usr/bin/touch /tmp/pwned

[Peer]
PublicKey = public
Endpoint = 188.114.97.1:500
AllowedIPs = 0.0.0.0/0
`)
	if _, err := probeConfig(input, model.Endpoint{Address: "188.114.97.1", Port: 500}); err == nil {
		t.Fatal("expected PostUp rejection")
	}
}

func TestAugmentProbePlanPrioritizesConfigEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warp.conf")
	if err := os.WriteFile(path, []byte("[Interface]\nPrivateKey = private\n\n[Peer]\nPublicKey = public\nEndpoint = 188.114.96.2:500\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := augmentProbePlan(context.Background(), model.ProbePlan{
		Candidates: []model.Endpoint{{Address: "162.159.192.1", Port: 2408}},
		TimeoutSec: 10,
	}, path)
	if len(plan.Candidates) == 0 ||
		plan.Candidates[0].Address != "188.114.96.2" ||
		plan.Candidates[0].Port != 500 {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestIsolatedProbeFailureNeverChangesHostRoutesOrDNS(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "source.conf")
	if err := os.WriteFile(configPath, []byte(`[Interface]
PrivateKey = private
Address = 172.16.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = public
Endpoint = engage.cloudflareclient.com:2408
AllowedIPs = 0.0.0.0/0
`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &RecordingRunner{fail: map[string]error{
		"wg show archieprobe latest-handshakes": errors.New("no handshake"),
	}}
	probe := IsolatedProbe{
		Runner:       runner,
		ConfigPath:   configPath,
		StateDir:     filepath.Join(dir, "state"),
		Timeout:      20 * time.Millisecond,
		PollInterval: time.Millisecond,
	}
	_, err := probe.Probe(context.Background(), model.Endpoint{Address: "188.114.97.1", Port: 500})
	if err == nil {
		t.Fatal("expected handshake failure")
	}
	for _, command := range runner.commands {
		switch {
		case strings.HasPrefix(command, "route add"),
			strings.HasPrefix(command, "networksetup -setdnsservers"),
			strings.Contains(command, "0.0.0.0/1"),
			strings.Contains(command, "128.0.0.0/1"):
			t.Fatalf("failure path mutated host routing: %s", command)
		}
	}
	if !runner.saw("wg-quick down " + filepath.Join(dir, "state", "archieprobe.conf")) {
		t.Fatalf("probe cleanup missing: %#v", runner.commands)
	}
}

func TestIsolatedProbeUsesNewUTUNInterface(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "source.conf")
	if err := os.WriteFile(configPath, []byte(`[Interface]
PrivateKey = private
Address = 172.16.0.2/32

[Peer]
PublicKey = public
Endpoint = 188.114.97.1:500
AllowedIPs = 0.0.0.0/0
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var runner *ScriptedRunner
	runner = &ScriptedRunner{
		responses: map[string][]byte{
			"wg show interfaces": nil,
		},
		fail: map[string]error{
			"wg show utun9 latest-handshakes": errors.New("no handshake"),
		},
		onRun: func(command string) error {
			if command == "wg-quick up "+filepath.Join(dir, "state", "archieprobe.conf") {
				runner.responses["wg show interfaces"] = []byte("utun9\n")
			}
			return nil
		},
	}
	probe := IsolatedProbe{
		Runner:       runner,
		ConfigPath:   configPath,
		StateDir:     filepath.Join(dir, "state"),
		Timeout:      10 * time.Millisecond,
		PollInterval: time.Millisecond,
	}
	if _, err := probe.Probe(context.Background(), model.Endpoint{Address: "188.114.97.1", Port: 500}); err == nil {
		t.Fatal("expected handshake failure")
	}
	if !containsCommand(runner.commands, "wg show utun9 latest-handshakes") {
		t.Fatalf("new utun interface was not used: %#v", runner.commands)
	}
}

func TestIsolatedProbeAcceptsHandshakeWithoutRouteTrace(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "source.conf")
	if err := os.WriteFile(configPath, []byte(`[Interface]
PrivateKey = private
Address = 172.16.0.2/32

[Peer]
PublicKey = public
Endpoint = 188.114.97.1:500
AllowedIPs = 0.0.0.0/0
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var runner *ScriptedRunner
	runner = &ScriptedRunner{
		responses: map[string][]byte{
			"wg show interfaces":              nil,
			"wg show utun9 latest-handshakes": []byte("123 456\n"),
			"wg show utun9":                   []byte("peer: handshake ok\n"),
		},
		onRun: func(command string) error {
			if command == "wg-quick up "+filepath.Join(dir, "state", "archieprobe.conf") {
				runner.responses["wg show interfaces"] = []byte("utun9\n")
			}
			return nil
		},
	}
	probe := IsolatedProbe{
		Runner:       runner,
		ConfigPath:   configPath,
		StateDir:     filepath.Join(dir, "state"),
		Timeout:      50 * time.Millisecond,
		PollInterval: time.Millisecond,
	}
	result, err := probe.Probe(context.Background(), model.Endpoint{Address: "188.114.97.1", Port: 500})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Handshake {
		t.Fatalf("result=%#v", result)
	}
}

type FakeEndpointProbe struct {
	Healthy map[string]bool
}

func (f FakeEndpointProbe) Probe(_ context.Context, endpoint model.Endpoint) (model.ProbeResult, error) {
	key := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
	if !f.Healthy[key] {
		return model.ProbeResult{Endpoint: endpoint}, errors.New("unhealthy")
	}
	return model.ProbeResult{Endpoint: endpoint, Handshake: true, Trace: "warp=on"}, nil
}

type RecordingRunner struct {
	commands []string
	fail     map[string]error
}

func (r *RecordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	command := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
	r.commands = append(r.commands, command)
	if err := r.fail[command]; err != nil {
		return nil, err
	}
	return nil, nil
}

func (r *RecordingRunner) saw(command string) bool {
	for _, got := range r.commands {
		if got == command {
			return true
		}
	}
	return false
}
