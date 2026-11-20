package export

import (
	"bufio"
	"errors"
	"strings"
)

func WireGuard(input []byte, includePrivate bool) ([]byte, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(input)))
	lines := []string{}
	sections := map[string]bool{}
	section := ""
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.Trim(trimmed, "[]")
			sections[section] = true
			lines = append(lines, line)
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if ok && !includePrivate {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "privatekey", "presharedkey":
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				lines = append(lines, indent+strings.TrimSpace(key)+" = <redacted>")
				continue
			}
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !sections["Interface"] || !sections["Peer"] {
		return nil, errors.New("WireGuard config requires [Interface] and [Peer] sections")
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}
