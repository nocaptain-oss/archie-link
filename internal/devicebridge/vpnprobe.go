package devicebridge

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// HRM-001 host-side VPN detection for HarmonyOS devices.
//
// The probe is read-only: it runs fixed-parameter hdc shell commands
// (ifconfig / hidumper -s NetsysNative / ps -ef), never writes device state,
// and never passes caller-controlled text to the shell. HarmonyOS exposes a
// single system VPN slot, and the companion extension runs as
// "com.archielink.companion:vpn".
//
// Real-device findings (BRA-AL00, 2026-11-18):
//
//   - A VPN running inside the Anco Android-compat container (e.g. v2VPN)
//     occupies the system VPN slot, yet its tun interface never appears in
//     host `ifconfig`. It is only visible as a NetsysNative network instance
//     (`hidumper -s NetsysNative` shows e.g. "NetId:278  interfaces:
//     {vpn-tun, }"), so the NetsysNative dump is a mandatory probe source.
//   - The companion `:vpn` extension process lingers after its connection
//     attempt is rejected with error 2203002, so process presence alone
//     over-attributes. "self" therefore also requires the tun-class interface
//     to sit in the host network namespace (or the ifconfig probe to be
//     unavailable); a slot-occupying VPN whose tun is missing from host
//     ifconfig cannot belong to the companion.
//   - Retail BRA-AL00 ships without `netcfg` and the VPNManager service
//     exposes no hidumper output, so neither is used.

var ErrVPNProbeUnavailable = errors.New("HarmonyOS VPN probe is unavailable")

const harmonyCompanionVPNProcess = "com.archielink.companion:vpn"

type VPNProbeDetail struct {
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

type VPNProbeReport struct {
	SchemaVersion int              `json:"schema_version"`
	Serial        string           `json:"serial"`
	Tool          string           `json:"tool"`
	VPNActive     bool             `json:"vpn_active"`
	Interfaces    []string         `json:"interfaces,omitempty"`
	Attribution   string           `json:"attribution"`
	Probes        []VPNProbeDetail `json:"probes"`
}

type interfaceEvidence struct {
	hostNames   []string
	hostOK      bool
	netsysNames []string
	netsysOK    bool
}

func (h *Host) VPNProbe(ctx context.Context, serial string) (VPNProbeReport, error) {
	if h == nil || h.Runner == nil {
		return VPNProbeReport{}, ErrVPNProbeUnavailable
	}
	if !validSerial(serial) {
		return VPNProbeReport{}, fmt.Errorf("%w: invalid device serial", ErrVPNProbeUnavailable)
	}
	hdc := strings.TrimSpace(h.HDC)
	if hdc == "" {
		hdc = "hdc"
	}
	report := VPNProbeReport{
		SchemaVersion: SchemaVersion,
		Serial:        serial,
		Tool:          "hdc",
		Probes:        []VPNProbeDetail{},
	}
	evidence, err := h.probeInterfaces(ctx, hdc, serial, &report)
	if err != nil {
		return VPNProbeReport{}, err
	}
	if !evidence.hostOK && !evidence.netsysOK {
		return VPNProbeReport{}, fmt.Errorf(
			"%w: interface probes failed on %s (shell may be restricted or device offline)",
			ErrVPNProbeUnavailable, serial,
		)
	}
	report.Interfaces = filterVPNInterfaces(evidence.hostNames, evidence.netsysNames)
	report.VPNActive = len(report.Interfaces) > 0
	psOutput, psOK, err := h.probeCommand(ctx, hdc, serial, "ps", &report, "shell", "ps", "-ef")
	if err != nil {
		return VPNProbeReport{}, err
	}
	report.Attribution = attribute(evidence, psOutput, psOK, report.VPNActive)
	return report, nil
}

func attribute(evidence interfaceEvidence, psOutput string, psOK bool, vpnActive bool) string {
	switch {
	case !vpnActive:
		return "none"
	case !psOK:
		return "unknown"
	case !strings.Contains(psOutput, harmonyCompanionVPNProcess):
		return "other"
	case evidence.hostOK && len(filterVPNInterfaces(evidence.hostNames, nil)) == 0:
		// The slot-occupying VPN's tun is not in the host network namespace,
		// so it cannot be the companion's; the lingering companion
		// extension process was rejected with error 2203002.
		return "other"
	default:
		return "self"
	}
}

func (h *Host) probeInterfaces(ctx context.Context, hdc, serial string, report *VPNProbeReport) (interfaceEvidence, error) {
	evidence := interfaceEvidence{
		hostNames:   []string{},
		netsysNames: []string{},
	}
	hostOutput, hostOK, err := h.probeCommand(ctx, hdc, serial, "ifconfig", report, "shell", "ifconfig")
	if err != nil {
		return evidence, err
	}
	evidence.hostOK = hostOK
	if hostOK {
		evidence.hostNames = parseIfconfigInterfaces(hostOutput)
	}
	netsysOutput, netsysOK, err := h.probeCommand(ctx, hdc, serial, "netsys", report, "shell", "hidumper", "-s", "NetsysNative")
	if err != nil {
		return evidence, err
	}
	evidence.netsysOK = netsysOK
	if netsysOK {
		evidence.netsysNames = parseNetsysInterfaces(netsysOutput)
	}
	return evidence, nil
}

func (h *Host) probeCommand(ctx context.Context, hdc, serial, label string, report *VPNProbeReport, args ...string) (string, bool, error) {
	full := append([]string{hdc, "-t", serial}, args...)
	output, err := h.Runner.Run(ctx, full[0], full[1:]...)
	detail := VPNProbeDetail{Command: strings.Join(full[1:], " ")}
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			report.Probes = append(report.Probes, VPNProbeDetail{Command: detail.Command, Error: "hdc unavailable"})
			return "", false, fmt.Errorf("%w: %v", ErrHDCUnavailable, err)
		}
		detail.Error = strings.TrimSpace(string(output))
		if detail.Error == "" {
			detail.Error = err.Error()
		}
	} else if strings.Contains(strings.ToLower(string(output)), "connect server failed") {
		detail.Error = strings.TrimSpace(string(output))
		report.Probes = append(report.Probes, detail)
		return string(output), false, ErrHDCUnavailable
	}
	detail.OK = err == nil
	report.Probes = append(report.Probes, detail)
	return string(output), err == nil, nil
}

func parseIfconfigInterfaces(output string) []string {
	names := []string{}
	seen := map[string]struct{}{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ":")
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// parseNetsysInterfaces extracts interface names from NetsysNative dump
// lines such as "NetId:278  interfaces: {vpn-tun, }".
func parseNetsysInterfaces(output string) []string {
	names := []string{}
	seen := map[string]struct{}{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		idx := strings.Index(line, "interfaces:")
		if idx < 0 {
			continue
		}
		open := strings.Index(line[idx:], "{")
		close := strings.LastIndex(line, "}")
		if open < 0 || close < 0 {
			continue
		}
		openAbs := idx + open
		if openAbs+1 > close {
			continue
		}
		listing := line[openAbs+1 : close]
		for _, item := range strings.Split(listing, ",") {
			name := strings.TrimSpace(item)
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}

func filterVPNInterfaces(nameLists ...[]string) []string {
	filtered := []string{}
	seen := map[string]struct{}{}
	for _, names := range nameLists {
		for _, name := range names {
			lower := strings.ToLower(name)
			if !strings.Contains(lower, "tun") && !strings.Contains(lower, "vpn") {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			filtered = append(filtered, name)
		}
	}
	return filtered
}
