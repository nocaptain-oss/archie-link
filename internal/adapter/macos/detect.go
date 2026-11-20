package macos

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func (a *Adapter) Detect(ctx context.Context) (model.PlatformReport, model.CapabilityReport, error) {
	version, err := a.runText(ctx, "sw_vers", "-productVersion")
	if err != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, err
	}
	arch, err := a.runText(ctx, "uname", "-m")
	if err != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, err
	}
	uid, err := a.runText(ctx, "id", "-u")
	if err != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, err
	}

	major, err := macMajor(version)
	if err != nil {
		return model.PlatformReport{}, model.CapabilityReport{}, err
	}
	uidValue, uidErr := strconv.Atoi(uid)

	return model.PlatformReport{
		OS:      "darwin",
		Version: version,
		Arch:    arch,
	}, model.CapabilityReport{
		Shell:           true,
		Elevation:       uidErr == nil && uidValue == 0,
		NetworkMutation: major >= 12,
		GUI:             true,
	}, nil
}

func macMajor(version string) (int, error) {
	part, _, _ := strings.Cut(version, ".")
	major, err := strconv.Atoi(part)
	if err != nil {
		return 0, fmt.Errorf("parse macOS version %q: %w", version, err)
	}
	return major, nil
}
