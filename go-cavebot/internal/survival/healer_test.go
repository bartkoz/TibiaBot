package survival

import "testing"

func TestHealerNeedsHealthPot(t *testing.T) {
	h := NewHealer(60, 40, 1.0, 1.0)
	action := h.Check(30.0, 80.0)
	if action != "health" {
		t.Errorf("action = %q, want health", action)
	}
}

func TestHealerNeedsManaPot(t *testing.T) {
	h := NewHealer(60, 40, 1.0, 1.0)
	action := h.Check(90.0, 20.0)
	if action != "mana" {
		t.Errorf("action = %q, want mana", action)
	}
}

func TestHealerNoActionNeeded(t *testing.T) {
	h := NewHealer(60, 40, 1.0, 1.0)
	action := h.Check(90.0, 80.0)
	if action != "" {
		t.Errorf("action = %q, want empty", action)
	}
}

func TestHealerRespectsCooldown(t *testing.T) {
	h := NewHealer(60, 40, 10.0, 10.0)
	action := h.Check(30.0, 80.0)
	if action != "health" {
		t.Fatalf("action = %q, want health", action)
	}
	h.MarkUsed("health")
	action = h.Check(30.0, 80.0)
	if action != "" {
		t.Errorf("action = %q, want empty (on cooldown)", action)
	}
}

func TestHealerHealthPriorityOverMana(t *testing.T) {
	h := NewHealer(60, 40, 1.0, 1.0)
	action := h.Check(30.0, 20.0)
	if action != "health" {
		t.Errorf("action = %q, want health (priority)", action)
	}
}
