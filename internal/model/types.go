package model

import "time"

const SchemaVersion = "1"

type Mode string

const (
	ModeAuto        Mode = "auto"
	ModeGuided      Mode = "guided"
	ModeUnsupported Mode = "unsupported"
)

type Result string

const (
	ResultReady       Result = "ready"
	ResultBlocked     Result = "blocked"
	ResultUnsupported Result = "unsupported"
)

type PlatformReport struct {
	OS      string `json:"os"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
}

type CapabilityReport struct {
	Shell           bool `json:"shell"`
	Elevation       bool `json:"elevation"`
	NetworkMutation bool `json:"network_mutation"`
	GUI             bool `json:"gui"`
}

type NetworkReport struct {
	DefaultInterface string   `json:"default_interface,omitempty"`
	NetworkService   string   `json:"network_service,omitempty"`
	Gateway          string   `json:"gateway,omitempty"`
	DNS              []string `json:"dns"`
	ServiceDNS       []string `json:"service_dns,omitempty"`
	ProxyEnabled     bool     `json:"proxy_enabled"`
	ExistingTunnels  []string `json:"existing_tunnels"`
}

type Finding struct {
	Code    string `json:"code"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type Action struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type DetectReport struct {
	SchemaVersion string           `json:"schema_version"`
	Mode          Mode             `json:"mode"`
	Platform      PlatformReport   `json:"platform"`
	Capabilities  CapabilityReport `json:"capabilities"`
	Network       NetworkReport    `json:"network"`
	Result        Result           `json:"result"`
}

type PlanReport struct {
	SchemaVersion string         `json:"schema_version"`
	Mode          Mode           `json:"mode"`
	Platform      PlatformReport `json:"platform"`
	Actions       []Action       `json:"actions"`
	Blocked       []Finding      `json:"blocked"`
	Permissions   []string       `json:"permissions"`
	Result        Result         `json:"result"`
}

type DoctorReport struct {
	SchemaVersion string           `json:"schema_version"`
	Mode          Mode             `json:"mode"`
	Platform      PlatformReport   `json:"platform"`
	Capabilities  CapabilityReport `json:"capabilities"`
	Network       NetworkReport    `json:"network"`
	Findings      []Finding        `json:"findings"`
}

type GuidedStep struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type GuidedReport struct {
	SchemaVersion string           `json:"schema_version"`
	Mode          Mode             `json:"mode"`
	Platform      PlatformReport   `json:"platform"`
	Capabilities  CapabilityReport `json:"capabilities"`
	Network       NetworkReport    `json:"network"`
	Steps         []GuidedStep     `json:"steps"`
	Result        Result           `json:"result"`
}

type Endpoint struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type ProbePlan struct {
	Candidates []Endpoint `json:"candidates"`
	TimeoutSec int        `json:"timeout_sec"`
}

type ProbeResult struct {
	Endpoint  Endpoint `json:"endpoint"`
	Handshake bool     `json:"handshake"`
	Trace     string   `json:"trace,omitempty"`
}

type ApplyPlan struct {
	Endpoint            Endpoint `json:"endpoint"`
	LocalDNSDomains     []string `json:"local_dns_domains"`
	ProtectionDomains   []string `json:"protection_domains"`
	DirectHosts         []string `json:"direct_hosts,omitempty"`
	AlwaysDirectCIDRs   []string `json:"always_direct_cidrs"`
	LocalPorts          []int    `json:"local_ports"`
	ConfigPath          string   `json:"-"`
	ConfigDigest        string   `json:"config_digest,omitempty"`
	HealthTimeoutSecond int      `json:"health_timeout_sec,omitempty"`
	OriginalDNS         []string `json:"original_dns,omitempty"`
	Gateway             string   `json:"gateway,omitempty"`
	DefaultInterface    string   `json:"default_interface,omitempty"`
	NetworkService      string   `json:"network_service,omitempty"`
}

type NetworkSnapshot struct {
	SchemaVersion    string    `json:"schema_version"`
	CapturedAt       time.Time `json:"captured_at"`
	DefaultInterface string    `json:"default_interface"`
	NetworkService   string    `json:"network_service,omitempty"`
	Gateway          string    `json:"gateway"`
	DNS              []string  `json:"dns"`
	ResolverDNS      []string  `json:"resolver_dns,omitempty"`
	ProxyEnabled     bool      `json:"proxy_enabled"`
	ExistingTunnels  []string  `json:"existing_tunnels"`
	ManagedFiles     []string  `json:"managed_files"`
}

type HealthCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

type HealthReport struct {
	OK      bool          `json:"ok"`
	Checks  []HealthCheck `json:"checks"`
	Failure string        `json:"failure,omitempty"`
}

type Ownership struct {
	Interface     string   `json:"interface"`
	PID           int      `json:"pid,omitempty"`
	Endpoint      Endpoint `json:"endpoint"`
	ManagedFiles  []string `json:"managed_files"`
	ManagedRoutes []string `json:"managed_routes,omitempty"`
	DirectHosts   []string `json:"direct_hosts,omitempty"`
}

type StatusReport struct {
	SchemaVersion string        `json:"schema_version"`
	Phase         string        `json:"phase"`
	Connected     bool          `json:"connected"`
	Healthy       bool          `json:"healthy"`
	Interface     string        `json:"interface,omitempty"`
	Endpoint      Endpoint      `json:"endpoint,omitempty"`
	Checks        []HealthCheck `json:"checks"`
	Failure       string        `json:"failure,omitempty"`
}
