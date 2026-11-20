package coexist

import (
	"errors"
	"testing"

	"github.com/nocaptain-oss/archie-link/internal/model"
)

func TestAutoApplyRequiresAllowedGate(t *testing.T) {
	decision := GateDecision{Mode: model.ModeGuided, Allowed: false}
	if err := RequireAuto(decision); !errors.Is(err, ErrCoexistenceBlocked) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAllowedAutoGatePasses(t *testing.T) {
	if err := RequireAuto(GateDecision{Mode: model.ModeAuto, Allowed: true}); err != nil {
		t.Fatal(err)
	}
}
