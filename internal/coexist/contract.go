package coexist

import (
	"errors"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

var ErrCoexistenceBlocked = errors.New("coexistence gate blocked auto mode")

func RequireAuto(decision GateDecision) error {
	if decision.Mode != model.ModeAuto || !decision.Allowed {
		return ErrCoexistenceBlocked
	}
	return nil
}
