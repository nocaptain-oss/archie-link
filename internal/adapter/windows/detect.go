package windows

import (
	"context"
	"strconv"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

const (
	versionScript = "[System.Environment]::OSVersion.Version.ToString()"
	archScript    = "$env:PROCESSOR_ARCHITECTURE"
)

func (a *Adapter) Detect(ctx context.Context) (model.PlatformReport, model.CapabilityReport, error) {
	version, err := a.runPowerShell(ctx, versionScript)
	if err != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, err
	}
	arch, err := a.runPowerShell(ctx, archScript)
	if err != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, err
	}

	build := windowsBuild(version)
	return model.PlatformReport{
		OS:      "windows",
		Version: version,
		Arch:    strings.ToLower(arch),
	}, model.CapabilityReport{
		Shell:           true,
		Elevation:       true,
		NetworkMutation: build >= 22000,
		GUI:             true,
	}, nil
}

func windowsBuild(version string) int {
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return 0
	}
	build, _ := strconv.Atoi(parts[2])
	return build
}
