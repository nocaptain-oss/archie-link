package devicebridge

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type probeRunner struct {
	outputs    map[string]string
	failures   map[string]error
	hdcMissing bool
	calls      [][]string
}

func (r *probeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.hdcMissing {
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	key := strings.Join(args[2:], " ")
	if err, failed := r.failures[key]; failed {
		return []byte(key + " failed"), err
	}
	if output, ok := r.outputs[key]; ok {
		return []byte(output), nil
	}
	return []byte("command failed"), errors.New("command failed: " + key)
}

func TestVPNProbeDetectsCompanionVPN(t *testing.T) {
	runner := &probeRunner{outputs: map[string]string{
		"shell ifconfig": "lo: flags=73<UP>\nvpn-tun: flags=4305<UP>\n",
		"shell hidumper -s NetsysNative": "  NetId:100  interfaces: {wlan0, }\n" +
			"  NetId:278  interfaces: {vpn-tun, }\n",
		"shell ps -ef": "root 1 0 ...\nappspawn 4242 1 com.archielink.companion:vpn\n",
	}}
	report, err := NewHost(runner).VPNProbe(context.Background(), "harmony-1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.VPNActive || len(report.Interfaces) != 1 || report.Interfaces[0] != "vpn-tun" {
		t.Fatalf("vpn_active=%t interfaces=%v", report.VPNActive, report.Interfaces)
	}
	if report.Attribution != "self" || report.Serial != "harmony-1" || report.Tool != "hdc" {
		t.Fatalf("attribution=%s serial=%s tool=%s", report.Attribution, report.Serial, report.Tool)
	}
	if len(report.Probes) != 3 || !report.Probes[0].OK || !report.Probes[1].OK || !report.Probes[2].OK {
		t.Fatalf("probes=%#v", report.Probes)
	}
	if report.Probes[0].Command != "-t harmony-1 shell ifconfig" {
		t.Fatalf("probe command=%s", report.Probes[0].Command)
	}
}

// Container VPNs (e.g. v2VPN inside the Anco compat layer) occupy the system
// VPN slot without exposing a tun interface to host ifconfig. Only the
// NetsysNative dump reveals them (real-device finding, BRA-AL00).
func TestVPNProbeDetectsContainerVPNOnlyVisibleInNetsys(t *testing.T) {
	runner := &probeRunner{outputs: map[string]string{
		"shell ifconfig": "lo Link encap:Local Loopback\nwlan0 Link encap:Ethernet\n",
		"shell hidumper -s NetsysNative": "  NetId:100  interfaces: {wlan0, }\n" +
			"  NetId:278  interfaces: {vpn-tun, }\n",
		"shell ps -ef": "root 1 0 ...\ncom.other.app 2000 1 something\n",
	}}
	report, err := NewHost(runner).VPNProbe(context.Background(), "harmony-1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.VPNActive || len(report.Interfaces) != 1 || report.Interfaces[0] != "vpn-tun" {
		t.Fatalf("vpn_active=%t interfaces=%v", report.VPNActive, report.Interfaces)
	}
	if report.Attribution != "other" {
		t.Fatalf("attribution=%s", report.Attribution)
	}
}

// After a rejected connect (system error 2203002), the companion :vpn
// extension process lingers even though the slot belongs to a container VPN.
// Process presence alone must not attribute the VPN to the companion
// (real-device finding, BRA-AL00).
func TestVPNProbeAttributesContainerVPNOtherDespiteLingeringCompanionProcess(t *testing.T) {
	runner := &probeRunner{outputs: map[string]string{
		"shell ifconfig": "lo Link encap:Local Loopback\nwlan0 Link encap:Ethernet\n",
		"shell hidumper -s NetsysNative": "  NetId:278  interfaces: {vpn-tun, }\n",
		"shell ps -ef": "root 1 0 ...\n20020315 58807 592 com.archielink.companion:vpn\n",
	}}
	report, err := NewHost(runner).VPNProbe(context.Background(), "harmony-1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.VPNActive || len(report.Interfaces) != 1 || report.Interfaces[0] != "vpn-tun" {
		t.Fatalf("vpn_active=%t interfaces=%v", report.VPNActive, report.Interfaces)
	}
	if report.Attribution != "other" {
		t.Fatalf("attribution=%s", report.Attribution)
	}
}

func TestVPNProbeAttributesOtherVPN(t *testing.T) {
	runner := &probeRunner{outputs: map[string]string{
		"shell ifconfig": "wlan0: flags=4163<UP>\ntun0: flags=4305<UP>\n",
		"shell ps -ef":   "root 1 0 ...\nshell 2000 1 com.other.vpn:core\n",
	}}
	report, err := NewHost(runner).VPNProbe(context.Background(), "harmony-1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.VPNActive || len(report.Interfaces) != 1 || report.Interfaces[0] != "tun0" {
		t.Fatalf("vpn_active=%t interfaces=%v", report.VPNActive, report.Interfaces)
	}
	if report.Attribution != "other" {
		t.Fatalf("attribution=%s", report.Attribution)
	}
}

func TestVPNProbeNoVPN(t *testing.T) {
	runner := &probeRunner{outputs: map[string]string{
		"shell ifconfig": "lo: flags=73<UP>\nwlan0: flags=4163<UP>\n",
		"shell hidumper -s NetsysNative": "  NetId:100  interfaces: {wlan0, }\n",
		"shell ps -ef":   "root 1 0 ...\n",
	}}
	report, err := NewHost(runner).VPNProbe(context.Background(), "harmony-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.VPNActive || len(report.Interfaces) != 0 || report.Attribution != "none" {
		t.Fatalf("vpn_active=%t interfaces=%v attribution=%s",
			report.VPNActive, report.Interfaces, report.Attribution)
	}
}

func TestVPNProbeUnknownAttributionWhenProcessListUnavailable(t *testing.T) {
	runner := &probeRunner{
		outputs: map[string]string{
			"shell ifconfig": "vpn-tun: flags=4305<UP>\n",
		},
		failures: map[string]error{
			"shell ps -ef": errors.New("permission denied"),
		},
	}
	report, err := NewHost(runner).VPNProbe(context.Background(), "harmony-1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.VPNActive || report.Attribution != "unknown" {
		t.Fatalf("vpn_active=%t attribution=%s", report.VPNActive, report.Attribution)
	}
}

func TestVPNProbeFallsBackToNetsysWhenIfconfigFails(t *testing.T) {
	runner := &probeRunner{
		outputs: map[string]string{
			"shell hidumper -s NetsysNative": "  NetId:278  interfaces: {vpn-tun, }\n",
		},
		failures: map[string]error{
			"shell ifconfig": errors.New("permission denied"),
		},
	}
	report, err := NewHost(runner).VPNProbe(context.Background(), "harmony-1")
	if err != nil {
		t.Fatal(err)
	}
	if !report.VPNActive || len(report.Interfaces) != 1 || report.Interfaces[0] != "vpn-tun" {
		t.Fatalf("vpn_active=%t interfaces=%v", report.VPNActive, report.Interfaces)
	}
	if report.Attribution != "unknown" {
		t.Fatalf("attribution=%s", report.Attribution)
	}
	if len(report.Probes) != 3 || report.Probes[0].OK || !report.Probes[1].OK {
		t.Fatalf("probes=%#v", report.Probes)
	}
}

func TestVPNProbeFailsWhenAllInterfaceProbesFail(t *testing.T) {
	runner := &probeRunner{
		failures: map[string]error{
			"shell ifconfig": errors.New("permission denied"),
			"shell hidumper -s NetsysNative": errors.New("permission denied"),
		},
	}
	_, err := NewHost(runner).VPNProbe(context.Background(), "harmony-1")
	if !errors.Is(err, ErrVPNProbeUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestVPNProbeReportsMissingHDC(t *testing.T) {
	runner := &probeRunner{hdcMissing: true}
	_, err := NewHost(runner).VPNProbe(context.Background(), "harmony-1")
	if !errors.Is(err, ErrHDCUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestVPNProbeRejectsInvalidSerial(t *testing.T) {
	runner := &probeRunner{outputs: map[string]string{}}
	_, err := NewHost(runner).VPNProbe(context.Background(), "-t")
	if !errors.Is(err, ErrVPNProbeUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unexpected calls=%v", runner.calls)
	}
}

func TestParseIfconfigInterfacesDeduplicatesAndSkipsIndented(t *testing.T) {
	names := parseIfconfigInterfaces(
		"lo: flags=73<UP>\n" +
			"	inet 127.0.0.1 netmask 0xff000000\n" +
			"vpn-tun: flags=4305<UP>\n" +
			"vpn-tun: flags=4305<UP>\n" +
			"wlan0 flags=4163<UP>\n",
	)
	expected := []string{"lo", "vpn-tun", "wlan0"}
	if len(names) != len(expected) {
		t.Fatalf("names=%v", names)
	}
	for index, name := range expected {
		if names[index] != name {
			t.Fatalf("names=%v", names)
		}
	}
}

func TestParseNetsysInterfaces(t *testing.T) {
	names := parseNetsysInterfaces(
		"  NetId:99  interfaces: {}\n" +
			"  NetId:100  interfaces: {wlan0, }\n" +
			"  NetId:278  interfaces: {vpn-tun, }\n" +
			"  NetId: 100\n" +
			"  TimeoutMsec: 4000\n",
	)
	expected := []string{"wlan0", "vpn-tun"}
	if len(names) != len(expected) {
		t.Fatalf("names=%v", names)
	}
	for index, name := range expected {
		if names[index] != name {
			t.Fatalf("names=%v", names)
		}
	}
}
