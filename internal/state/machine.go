package state

import (
	"fmt"
	"sync"
)

type Phase string

const (
	PhaseDetected       Phase = "detected"
	PhasePlanned        Phase = "planned"
	PhaseLicensed       Phase = "licensed"
	PhaseProbing        Phase = "probing"
	PhaseApplying       Phase = "applying"
	PhaseVerifying      Phase = "verifying"
	PhaseActive         Phase = "active"
	PhaseRollingBack    Phase = "rolling_back"
	PhaseRestored       Phase = "restored"
	PhaseRollbackFailed Phase = "rollback_failed"
)

var allowed = map[Phase][]Phase{
	PhaseDetected:    {PhasePlanned},
	PhasePlanned:     {PhaseLicensed},
	PhaseLicensed:    {PhaseProbing},
	PhaseProbing:     {PhaseApplying},
	PhaseApplying:    {PhaseVerifying, PhaseRollingBack},
	PhaseVerifying:   {PhaseActive, PhaseRollingBack},
	PhaseActive:      {PhaseRollingBack},
	PhaseRollingBack: {PhaseRestored, PhaseRollbackFailed},
}

type Machine struct {
	mu    sync.Mutex
	Phase Phase
}

func NewMachine() *Machine {
	return &Machine{Phase: PhaseDetected}
}

func (m *Machine) Current() Phase {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Phase
}

func (m *Machine) To(next Phase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, candidate := range allowed[m.Phase] {
		if candidate == next {
			m.Phase = next
			return nil
		}
	}
	return fmt.Errorf("invalid lifecycle transition: %s -> %s", m.Phase, next)
}
