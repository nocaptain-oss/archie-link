package adapter

import (
	"context"
	"errors"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

var ErrUnsupportedPlatform = errors.New("unsupported platform")

type Adapter interface {
	Name() string
	Detect(context.Context) (model.PlatformReport, model.CapabilityReport, error)
	Snapshot(context.Context) (model.NetworkReport, error)
}

func SelectMode(c model.CapabilityReport) model.Mode {
	if c.Shell && c.Elevation && c.NetworkMutation {
		return model.ModeAuto
	}
	if c.Shell {
		return model.ModeGuided
	}
	return model.ModeUnsupported
}
