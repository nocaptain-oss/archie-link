package linux

import (
	"context"
	"fmt"
	"strings"

	"github.com/nocaptain-oss/archie-link/internal/adapter"
)

type commandRunner struct {
	runner adapter.Runner
}

func (r commandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := r.runner.Run(ctx, name, args...)
	if err != nil {
		command := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
		message := strings.TrimSpace(string(output))
		if message != "" {
			return output, fmt.Errorf("%s: %w: %s", command, err, message)
		}
		return output, fmt.Errorf("%s: %w", command, err)
	}
	return output, nil
}
