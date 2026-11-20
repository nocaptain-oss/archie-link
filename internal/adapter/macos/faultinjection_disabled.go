//go:build !faultinjection

package macos

import (
	"context"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func injectProbeFault(plan model.ProbePlan) (model.ProbePlan, error) {
	return plan, nil
}

func injectAfterApply(context.Context, string) error {
	return nil
}

func injectStatusOverride(string) (int, bool) {
	return 0, false
}
