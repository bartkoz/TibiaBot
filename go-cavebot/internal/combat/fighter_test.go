package combat

import "testing"

func TestSpellRotationReady(t *testing.T) {
	r := NewSpellRotation([]string{"F4", "F5"}, []float64{2.0, 6.0})
	key, ok := r.NextReadySpell()
	if !ok || key != "F4" {
		t.Errorf("NextReadySpell = %q, %v; want F4, true", key, ok)
	}
}

func TestSpellRotationCooldown(t *testing.T) {
	r := NewSpellRotation([]string{"F4", "F5"}, []float64{2.0, 6.0})
	r.MarkUsed("F4")
	key, ok := r.NextReadySpell()
	if !ok || key != "F5" {
		t.Errorf("NextReadySpell = %q, %v; want F5, true", key, ok)
	}
}

func TestSpellRotationAllOnCooldown(t *testing.T) {
	r := NewSpellRotation([]string{"F4"}, []float64{10.0})
	r.MarkUsed("F4")
	_, ok := r.NextReadySpell()
	if ok {
		t.Error("expected no ready spell")
	}
}
