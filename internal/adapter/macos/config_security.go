package macos

import (
	"fmt"
	"strings"
)

func rejectDangerousWireGuardKey(key string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "preup", "postup", "predown", "postdown", "saveconfig":
		return fmt.Errorf("WireGuard directive %s is not allowed", strings.TrimSpace(key))
	default:
		return nil
	}
}
