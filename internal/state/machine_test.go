package state

import "testing"

func TestCannotSkipProbe(t *testing.T) {
	m := NewMachine()
	if err := m.To(PhasePlanned); err != nil {
		t.Fatal(err)
	}
	if err := m.To(PhaseLicensed); err != nil {
		t.Fatal(err)
	}
	if err := m.To(PhaseApplying); err == nil {
		t.Fatal("expected invalid transition")
	}
}

func TestRollbackPath(t *testing.T) {
	m := NewMachine()
	for _, phase := range []Phase{
		PhasePlanned, PhaseLicensed, PhaseProbing,
		PhaseApplying, PhaseRollingBack, PhaseRestored,
	} {
		if err := m.To(phase); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHealthFailureUsesRollbackPath(t *testing.T) {
	m := NewMachine()
	for _, phase := range []Phase{
		PhasePlanned, PhaseLicensed, PhaseProbing, PhaseApplying,
		PhaseVerifying, PhaseRollingBack, PhaseRollbackFailed,
	} {
		if err := m.To(phase); err != nil {
			t.Fatal(err)
		}
	}
}
