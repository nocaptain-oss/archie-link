package linux

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

// configMeta 承载 wg-quick 专有键的语义：wg setconf 不接受它们，
// 由调用方用 ip(8) 单独应用。
type configMeta struct {
	Addresses   []string
	MTU         int
	AllowedIPs  []string
	HasIPv6Flow bool
}

func (m configMeta) hasDefaultRoute() bool {
	for _, cidr := range m.AllowedIPs {
		if cidr == "0.0.0.0/0" {
			return true
		}
	}
	return false
}

func (m configMeta) hasIPv6DefaultRoute() bool {
	for _, cidr := range m.AllowedIPs {
		if cidr == "::/0" {
			return true
		}
	}
	return false
}

func rejectDangerousWireGuardKey(key string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "preup", "postup", "predown", "postdown", "saveconfig":
		return fmt.Errorf("WireGuard directive %s is not allowed", strings.TrimSpace(key))
	default:
		return nil
	}
}

// buildSetConf 把 wg-quick 风格的源配置转换为 wg setconf 可接受的正文：
// Interface 只保留 PrivateKey/ListenPort，Peer 保留五键并把 Endpoint 换成探针结果；
// Address/MTU 提取进 meta，DNS/Table/FwMark 丢弃（路由由 ArchieLink 托管）。
// WireGuard 是懒握手的：没有流量或 PersistentKeepalive 时不会主动发起握手。
// wgcf 等工具生成的配置普遍缺 keepalive，探针接口又没有路由可承载流量，
// 因此 keepalive 缺省补 5 秒；forceKeepalive 非 0 时无条件覆盖（探针用）。
func buildSetConf(input []byte, endpoint model.Endpoint, forceKeepalive int) ([]byte, configMeta, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return nil, configMeta{}, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(input)))
	section := ""
	endpointSeen := false
	peerSeen := false
	keepaliveSeen := false
	meta := configMeta{Addresses: []string{}, AllowedIPs: []string{}, MTU: 0}
	lines := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if section == "Peer" {
				if !endpointSeen {
					return nil, configMeta{}, errors.New("WireGuard peer has no Endpoint")
				}
				lines = append(lines, keepaliveLine(forceKeepalive, keepaliveSeen)...)
			}
			section = strings.Trim(trimmed, "[]")
			lines = append(lines, line)
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			return nil, configMeta{}, fmt.Errorf("WireGuard config line is not a key=value pair: %q", trimmed)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if err := rejectDangerousWireGuardKey(key); err != nil {
			return nil, configMeta{}, err
		}
		switch section {
		case "Interface":
			switch key {
			case "PrivateKey", "ListenPort":
				lines = append(lines, line)
			case "Address":
				for _, address := range strings.Split(value, ",") {
					address = strings.TrimSpace(address)
					if address != "" {
						meta.Addresses = append(meta.Addresses, address)
					}
				}
			case "MTU":
				mtu, err := strconv.Atoi(value)
				if err != nil || mtu <= 0 {
					return nil, configMeta{}, fmt.Errorf("invalid MTU: %q", value)
				}
				meta.MTU = mtu
			case "DNS", "Table", "FwMark":
				// ArchieLink 托管 DNS 与路由，源配置的这几项不透传。
			default:
				return nil, configMeta{}, fmt.Errorf("unsupported WireGuard Interface key: %s", key)
			}
		case "Peer":
			peerSeen = true
			switch key {
			case "PublicKey", "PresharedKey", "AllowedIPs":
				lines = append(lines, line)
				if key == "AllowedIPs" {
					for _, cidr := range strings.Split(value, ",") {
						cidr = strings.TrimSpace(cidr)
						if cidr == "" {
							continue
						}
						meta.AllowedIPs = append(meta.AllowedIPs, cidr)
						if strings.Contains(cidr, ":") {
							meta.HasIPv6Flow = true
						}
					}
				}
			case "PersistentKeepalive":
				keepaliveSeen = true
				if forceKeepalive > 0 {
					lines = append(lines, "PersistentKeepalive = "+strconv.Itoa(forceKeepalive))
				} else {
					lines = append(lines, line)
				}
			case "Endpoint":
				lines = append(lines, "Endpoint = "+endpointKey(endpoint))
				endpointSeen = true
			default:
				return nil, configMeta{}, fmt.Errorf("unsupported WireGuard Peer key: %s", key)
			}
		default:
			return nil, configMeta{}, fmt.Errorf("WireGuard config key outside a section: %s", key)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, configMeta{}, err
	}
	if section == "Peer" {
		lines = append(lines, keepaliveLine(forceKeepalive, keepaliveSeen)...)
		if !endpointSeen {
			return nil, configMeta{}, errors.New("WireGuard peer has no Endpoint")
		}
	}
	if !peerSeen {
		return nil, configMeta{}, errors.New("WireGuard config has no Peer section")
	}
	return []byte(strings.Join(lines, "\n") + "\n"), meta, nil
}

// keepaliveLine 返回需要追加的 PersistentKeepalive 行；
// 源配置已带 keepalive 时已在主循环处理，此处不再追加。
func keepaliveLine(force int, seen bool) []string {
	if seen {
		return nil
	}
	if force > 0 {
		return []string{"PersistentKeepalive = " + strconv.Itoa(force)}
	}
	return []string{"PersistentKeepalive = 5"}
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
