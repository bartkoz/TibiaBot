package survival

type SupplyTracker struct {
	max       int
	threshold int
	remaining int
}

func NewSupplyTracker(maxPotions, refillThreshold int) *SupplyTracker {
	return &SupplyTracker{
		max:       maxPotions,
		threshold: refillThreshold,
		remaining: maxPotions,
	}
}

func (s *SupplyTracker) Remaining() int {
	return s.remaining
}

func (s *SupplyTracker) UsePotion() {
	if s.remaining > 0 {
		s.remaining--
	}
}

func (s *SupplyTracker) NeedsRefill() bool {
	return s.remaining <= s.threshold
}

func (s *SupplyTracker) Refill() {
	s.remaining = s.max
}
