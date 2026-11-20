package macos

import "github.com/nocaptain-oss/archie-link/internal/adapter"

type Adapter struct {
	runner adapter.Runner
}

func New(runner adapter.Runner) *Adapter {
	return &Adapter{runner: runner}
}

func (*Adapter) Name() string {
	return "darwin"
}
