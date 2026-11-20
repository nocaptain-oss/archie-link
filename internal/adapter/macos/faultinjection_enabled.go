//go:build faultinjection

package macos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

const faultModeEnv = "ARCHIE_LINK_FAULT"

func injectProbeFault(plan model.ProbePlan) (model.ProbePlan, error) {
	if faultMode() != "unavailable_endpoint" {
		return plan, nil
	}
	plan.Candidates = []model.Endpoint{{Address: "203.0.113.1", Port: 500}}
	plan.TimeoutSec = 1
	return plan, nil
}

func injectAfterApply(ctx context.Context, interfaceName string) error {
	switch faultMode() {
	case "dns_write_failure":
		return errors.New("injected DNS write failure")
	case "daemon_exit":
		output, err := exec.CommandContext(ctx, "pkill", "-TERM", "-f", "wireguard-go").CombinedOutput()
		if err != nil {
			return fmt.Errorf("inject daemon exit: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return errors.New("injected WireGuard daemon exit on " + interfaceName)
	default:
		return nil
	}
}

func injectStatusOverride(target string) (int, bool) {
	if faultMode() == "health_403" && strings.Contains(target, "api.deepseek.com") {
		return 403, true
	}
	return 0, false
}

func faultMode() string {
	return strings.TrimSpace(os.Getenv(faultModeEnv))
}
