package linux

import (
	"os/exec"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
)

type Adapter struct {
	runner  adapter.Runner
	lookPath func(string) (string, error)
}

func New(runner adapter.Runner) *Adapter {
	return &Adapter{runner: runner, lookPath: exec.LookPath}
}

func (*Adapter) Name() string {
	return "linux"
}
