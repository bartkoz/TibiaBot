package survival

import "time"

type Healer struct {
	healthThreshold float64
	manaThreshold   float64
	cooldowns       map[string]float64
	lastUsed        map[string]time.Time
}

func NewHealer(healthThreshold, manaThreshold, healthCooldown, manaCooldown float64) *Healer {
	return &Healer{
		healthThreshold: healthThreshold,
		manaThreshold:   manaThreshold,
		cooldowns: map[string]float64{
			"health": healthCooldown,
			"mana":   manaCooldown,
		},
		lastUsed: make(map[string]time.Time),
	}
}

func (h *Healer) Check(healthPct, manaPct float64) string {
	now := time.Now()
	if healthPct < h.healthThreshold {
		last := h.lastUsed["health"]
		if now.Sub(last).Seconds() >= h.cooldowns["health"] {
			return "health"
		}
	}
	if manaPct < h.manaThreshold {
		last := h.lastUsed["mana"]
		if now.Sub(last).Seconds() >= h.cooldowns["mana"] {
			return "mana"
		}
	}
	return ""
}

func (h *Healer) MarkUsed(action string) {
	h.lastUsed[action] = time.Now()
}
