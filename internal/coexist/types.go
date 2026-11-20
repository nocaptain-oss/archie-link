package coexist

import (
	"context"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

type Owner string

const (
	OwnerSystem          Owner = "system"
	OwnerArchielink      Owner = "archielink"
	OwnerKnownThirdParty Owner = "known_third_party"
	OwnerUnknown         Owner = "unknown"
)

type Layer string

const (
	LayerRoute    Layer = "route"
	LayerDNS      Layer = "dns"
	LayerProxy    Layer = "proxy"
	LayerHosts    Layer = "hosts"
	LayerListener Layer = "listener"
	LayerVPN      Layer = "vpn"
	LayerFirewall Layer = "firewall"
)

type RouteRecord struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Interface   string `json:"interface"`
	Table       string `json:"table,omitempty"`
	Owner       Owner  `json:"owner"`
	Managed     bool   `json:"managed"`
}

type RouteRule struct {
	Priority string `json:"priority"`
	Table    string `json:"table"`
	Owner    Owner  `json:"owner"`
}

type DNSResolver struct {
	Address string `json:"address"`
	Scope   string `json:"scope,omitempty"`
	Source  string `json:"source"`
	Owner   Owner  `json:"owner"`
}

type ProxyRecord struct {
	Kind     string   `json:"kind"`
	Enabled  bool     `json:"enabled"`
	URL      string   `json:"url,omitempty"`
	Bypass   []string `json:"bypass"`
	Owner    Owner    `json:"owner"`
	Complete bool     `json:"complete"`
}

type ListenerRecord struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Process string `json:"process,omitempty"`
	Owner   Owner  `json:"owner"`
}

type VPNRecord struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Owner Owner  `json:"owner"`
}

type ProtectedEndpoint struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	DirectURL        string `json:"direct_url,omitempty"`
	UpstreamURL      string `json:"upstream_url,omitempty"`
	Expected         string `json:"expected"`
	RequiredRoute    string `json:"required_route,omitempty"`
	BaselineRequired bool   `json:"baseline_required"`
}

type Profile struct {
	SchemaVersion    string              `json:"schema_version"`
	OS               string              `json:"os"`
	Routes           []RouteRecord       `json:"routes"`
	RouteRules       []RouteRule         `json:"route_rules"`
	DNSResolvers     []DNSResolver       `json:"dns_resolvers"`
	Proxies          []ProxyRecord       `json:"proxies"`
	Listeners        []ListenerRecord    `json:"listeners"`
	VPNs             []VPNRecord         `json:"vpns"`
	Protected        []ProtectedEndpoint `json:"protected_endpoints"`
	HostsDigest      string              `json:"hosts_digest,omitempty"`
	IncompleteLayers []Layer             `json:"incomplete_layers"`
	BaselineComplete bool                `json:"baseline_complete"`
}

type GateDecision struct {
	SchemaVersion    string          `json:"schema_version"`
	Mode             model.Mode      `json:"mode"`
	Allowed          bool            `json:"allowed"`
	Findings         []model.Finding `json:"findings"`
	IncompleteLayers []Layer         `json:"incomplete_layers"`
}

type Provider interface {
	Coexistence(context.Context) (Profile, error)
}
