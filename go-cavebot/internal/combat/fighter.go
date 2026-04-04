package combat

import "time"

type spellEntry struct {
	key      string
	cooldown float64
}

type SpellRotation struct {
	spells   []spellEntry
	lastUsed map[string]time.Time
}

func NewSpellRotation(keys []string, cooldowns []float64) *SpellRotation {
	spells := make([]spellEntry, len(keys))
	for i, k := range keys {
		cd := 0.0
		if i < len(cooldowns) {
			cd = cooldowns[i]
		}
		spells[i] = spellEntry{key: k, cooldown: cd}
	}
	return &SpellRotation{
		spells:   spells,
		lastUsed: make(map[string]time.Time),
	}
}

func (r *SpellRotation) NextReadySpell() (string, bool) {
	now := time.Now()
	for _, s := range r.spells {
		last := r.lastUsed[s.key]
		if now.Sub(last).Seconds() >= s.cooldown {
			return s.key, true
		}
	}
	return "", false
}

func (r *SpellRotation) MarkUsed(key string) {
	r.lastUsed[key] = time.Now()
}

// CastNext finds the next ready spell, calls pressFunc, and marks it used.
func (r *SpellRotation) CastNext(pressFunc func(string)) bool {
	key, ok := r.NextReadySpell()
	if !ok {
		return false
	}
	pressFunc(key)
	r.MarkUsed(key)
	return true
}

// Attack presses the attack key.
func Attack(attackKey string, pressFunc func(string)) {
	pressFunc(attackKey)
}
