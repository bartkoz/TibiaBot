package survival

import "testing"

func TestSupplyTrackerInitial(t *testing.T) {
	st := NewSupplyTracker(200, 20)
	if st.NeedsRefill() {
		t.Error("should not need refill initially")
	}
	if st.Remaining() != 200 {
		t.Errorf("Remaining = %d, want 200", st.Remaining())
	}
}

func TestSupplyTrackerUse(t *testing.T) {
	st := NewSupplyTracker(200, 20)
	st.UsePotion()
	if st.Remaining() != 199 {
		t.Errorf("Remaining = %d, want 199", st.Remaining())
	}
}

func TestSupplyTrackerNeedsRefill(t *testing.T) {
	st := NewSupplyTracker(200, 20)
	for i := 0; i < 185; i++ {
		st.UsePotion()
	}
	if st.Remaining() != 15 {
		t.Errorf("Remaining = %d, want 15", st.Remaining())
	}
	if !st.NeedsRefill() {
		t.Error("should need refill")
	}
}

func TestSupplyTrackerRefill(t *testing.T) {
	st := NewSupplyTracker(200, 20)
	for i := 0; i < 190; i++ {
		st.UsePotion()
	}
	st.Refill()
	if st.Remaining() != 200 {
		t.Errorf("Remaining = %d, want 200", st.Remaining())
	}
	if st.NeedsRefill() {
		t.Error("should not need refill after refill")
	}
}
