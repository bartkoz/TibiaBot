package core

import "testing"

func TestInitialState(t *testing.T) {
	sm := NewBotStateMachine()
	if sm.State() != Idle {
		t.Errorf("state = %v, want Idle", sm.State())
	}
}

func TestStartTransitionsToWalking(t *testing.T) {
	sm := NewBotStateMachine()
	sm.Start()
	if sm.State() != Walking {
		t.Errorf("state = %v, want Walking", sm.State())
	}
}

func TestStopTransitionsToIdle(t *testing.T) {
	sm := NewBotStateMachine()
	sm.Start()
	sm.Stop()
	if sm.State() != Idle {
		t.Errorf("state = %v, want Idle", sm.State())
	}
}

func TestTransitionToCombat(t *testing.T) {
	sm := NewBotStateMachine()
	sm.Start()
	sm.Transition(Combat)
	if sm.State() != Combat {
		t.Errorf("state = %v, want Combat", sm.State())
	}
}

func TestTransitionCombatToLooting(t *testing.T) {
	sm := NewBotStateMachine()
	sm.Start()
	sm.Transition(Combat)
	sm.Transition(Looting)
	if sm.State() != Looting {
		t.Errorf("state = %v, want Looting", sm.State())
	}
}

func TestTransitionLootingToWalking(t *testing.T) {
	sm := NewBotStateMachine()
	sm.Start()
	sm.Transition(Combat)
	sm.Transition(Looting)
	sm.Transition(Walking)
	if sm.State() != Walking {
		t.Errorf("state = %v, want Walking", sm.State())
	}
}

func TestStatusMap(t *testing.T) {
	sm := NewBotStateMachine()
	sm.Start()
	s := sm.Status()
	if s["state"] != "WALKING" {
		t.Errorf("state = %v, want WALKING", s["state"])
	}
	if _, ok := s["position"]; !ok {
		t.Error("missing position")
	}
	if _, ok := s["health_pct"]; !ok {
		t.Error("missing health_pct")
	}
	if _, ok := s["mana_pct"]; !ok {
		t.Error("missing mana_pct")
	}
}
