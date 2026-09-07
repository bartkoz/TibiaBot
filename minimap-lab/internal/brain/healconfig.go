package brain

import (
	"fmt"

	"minimap-lab/internal/heal"
	"minimap-lab/internal/input"
)

// maxHealRules bounds the list. Eight lines is more than any real setup needs
// - two potions and two spells is the usual four - and it keeps a malformed
// request from turning into an unbounded walk over rules on every frame.
const maxHealRules = 8

// HealConfig is the whole healing surface the panel edits. It lives here
// rather than in internal/heal because the key names are the driver's policy,
// not the rule engine's: the engine must stay a pure decision.
type HealConfig struct {
	// Enabled is the master switch. Rules are kept while it is off, so the
	// user can park the whole list without losing it.
	Enabled bool        `json:"enabled"`
	Rules   []heal.Rule `json:"rules"`
}

// validate checks every rule, including the disabled ones. A broken line that
// passes validation only because it is switched off would fail on the day it
// matters most.
func (c HealConfig) validate() error {
	if len(c.Rules) > maxHealRules {
		return fmt.Errorf("reguł leczenia może być najwyżej %d, podano %d", maxHealRules, len(c.Rules))
	}
	for i, r := range c.Rules {
		n := i + 1
		if r.Resource != heal.ResourceHP && r.Resource != heal.ResourceMana {
			return fmt.Errorf("reguła %d: zasób musi być hp albo mana", n)
		}
		if r.BelowPct < 1 || r.BelowPct > 99 {
			return fmt.Errorf("reguła %d: próg musi mieścić się w zakresie 1–99%%", n)
		}
		if r.MinManaPct < 0 || r.MinManaPct > 99 {
			return fmt.Errorf("reguła %d: minimalna mana musi mieścić się w zakresie 0–99%%", n)
		}
		if r.CooldownMS < 100 || r.CooldownMS > 60000 {
			return fmt.Errorf("reguła %d: cooldown musi mieścić się w zakresie 100–60000 ms", n)
		}
		if !input.ValidHotkey(r.Hotkey) {
			return fmt.Errorf("reguła %d: nieznany klawisz %q", n, r.Hotkey)
		}
	}
	return nil
}
