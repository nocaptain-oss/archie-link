package macos

import (
	"context"
	"net"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
	"github.com/nocaptain-oss/archie-link/internal/coexist"
	"github.com/nocaptain-oss/archie-link/internal/model"
)

type AutomaticOptions struct {
	ConfigPath      string
	StateDir        string
	ResolverDir     string
	WireguardRunDir string
	ResolveIPv4     func(context.Context, string) ([]string, error)
}

type Automatic struct {
	runner    adapter.Runner
	options   AutomaticOptions
	ownership *model.Ownership
	checker   HTTPChecker
}

func NewAutomatic(runner adapter.Runner, options AutomaticOptions) *Automatic {
	if options.ResolverDir == "" {
		options.ResolverDir = "/etc/resolver"
	}
	if options.WireguardRunDir == "" {
		options.WireguardRunDir = "/var/run/wireguard"
	}
	if options.ResolveIPv4 == nil {
		options.ResolveIPv4 = lookupIPv4
	}
	return &Automatic{runner: runner, options: options, checker: DefaultHTTPChecker{}}
}

func lookupIPv4(ctx context.Context, host string) ([]string, error) {
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, address := range addresses {
		if ipv4 := address.To4(); ipv4 != nil {
			result = append(result, ipv4.String())
		}
	}
	return result, nil
}

func (*Automatic) Name() string {
	return "darwin"
}

func (a *Automatic) Detect(ctx context.Context) (model.PlatformReport, model.CapabilityReport, error) {
	return New(a.runner).Detect(ctx)
}

func (a *Automatic) Snapshot(ctx context.Context) (model.NetworkReport, error) {
	return New(a.runner).Snapshot(ctx)
}

func (a *Automatic) Coexistence(ctx context.Context) (coexist.Profile, error) {
	return New(a.runner).Coexistence(ctx)
}

func (a *Automatic) Capture(ctx context.Context) (model.NetworkSnapshot, error) {
	network, err := a.Snapshot(ctx)
	if err != nil {
		return model.NetworkSnapshot{}, err
	}
	return model.NetworkSnapshot{
		SchemaVersion:    model.SchemaVersion,
		DefaultInterface: network.DefaultInterface,
		NetworkService:   network.NetworkService,
		Gateway:          network.Gateway,
		DNS:              append([]string(nil), network.ServiceDNS...),
		ResolverDNS:      append([]string(nil), network.DNS...),
		ProxyEnabled:     network.ProxyEnabled,
		ExistingTunnels:  append([]string(nil), network.ExistingTunnels...),
		ManagedFiles:     []string{},
	}, nil
}

func (a *Automatic) Probe(ctx context.Context, plan model.ProbePlan) (model.ProbeResult, error) {
	// Augment first: fault injection must be able to replace the full
	// candidate list, including endpoints discovered from the config.
	plan = augmentProbePlan(ctx, plan, a.options.ConfigPath)
	var err error
	plan, err = injectProbeFault(plan)
	if err != nil {
		return model.ProbeResult{}, err
	}
	probe := IsolatedProbe{
		Runner:     a.runner,
		ConfigPath: a.options.ConfigPath,
		StateDir:   a.options.StateDir,
	}
	return RunProbePlan(ctx, plan, probe)
}

func (a *Automatic) Resume(ownership model.Ownership) {
	copy := ownership
	a.ownership = &copy
}
