//go:build windows

package cli

import "os"

func effectiveHome() (string, error) {
	return os.UserHomeDir()
}
