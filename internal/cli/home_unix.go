//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package cli

import (
	"os"
	"os/user"
	"strings"
)

func effectiveHome() (string, error) {
	if os.Geteuid() == 0 {
		if name := strings.TrimSpace(os.Getenv("SUDO_USER")); name != "" && name != "root" {
			if account, err := user.Lookup(name); err == nil && account.HomeDir != "" {
				return account.HomeDir, nil
			}
		}
	}
	return os.UserHomeDir()
}
