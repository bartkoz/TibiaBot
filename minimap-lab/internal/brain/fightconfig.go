package brain

import (
	"fmt"

	"minimap-lab/internal/fight"
	"minimap-lab/internal/input"
)

// maxSpellRules mirrors internal/brain/healconfig.go's maxHealRules: eight
// lines is more than any real setup needs, and it keeps a malformed request
// from turning into an unbounded walk over rules on every frame.
const maxSpellRules = 8

// FightConfig is the whole targeting surface the panel edits.
type FightConfig struct {
	// Enabled is the "Atakuj" switch. The panel does not remember it across
	// a reload - it is a switch that makes the bot act, not a setting.
	Enabled   bool         `json:"enabled"`
	AttackKey string       `json:"attack_key"`
	Spells    []fight.Rule `json:"spells"`
	// BlockMixedCrowd is a *bool, not a bool: false is a legal, meaningful
	// choice (a spawn with an NPC permanently in frame), so withDefaults
	// must be able to tell "the panel sent false" from "this key was never
	// sent at all" - the only case that gets promoted to true.
	BlockMixedCrowd *bool `json:"block_mixed_crowd"`

	ConfirmMS       int `json:"confirm_ms"`
	LeaveFightMS    int `json:"leave_fight_ms"`
	TargetRetryMS   int `json:"target_retry_ms"`
	TargetAttempts  int `json:"target_attempts"`
	TargetBackoffMS int `json:"target_backoff_ms"`
	TargetStallMS   int `json:"target_stall_ms"`
	FightPauseMS    int `json:"fight_pause_ms"`
}

// withDefaults fills every field the panel may leave out, the same "zero
// means unset" rule combatconfig.go and healconfig.go already use - safe
// here because zero is not a legal value for any of these durations either.
func (c FightConfig) withDefaults() FightConfig {
	if c.ConfirmMS == 0 {
		c.ConfirmMS = 150
	}
	if c.LeaveFightMS == 0 {
		c.LeaveFightMS = 600
	}
	if c.TargetRetryMS == 0 {
		c.TargetRetryMS = 600
	}
	if c.TargetAttempts == 0 {
		c.TargetAttempts = 3
	}
	if c.TargetBackoffMS == 0 {
		c.TargetBackoffMS = 2000
	}
	if c.TargetStallMS == 0 {
		c.TargetStallMS = 15000
	}
	if c.FightPauseMS == 0 {
		c.FightPauseMS = 10000
	}
	if c.BlockMixedCrowd == nil {
		t := true
		c.BlockMixedCrowd = &t
	}
	rules := make([]fight.Rule, len(c.Spells))
	for i, r := range c.Spells {
		if r.CooldownMS == 0 {
			r.CooldownMS = 2000
		}
		if r.MinMonsters == 0 {
			r.MinMonsters = 1
		}
		if r.Radius == 0 {
			r.Radius = 1
		}
		rules[i] = r
	}
	c.Spells = rules
	return c
}

// validate checks every field, including disabled rules - a broken line that
// passes validation only because it is switched off would fail on the day it
// matters most. decisionRadius bounds a rule's own radius: a rule asking
// about tiles the vision layer never sent would be asking about nothing.
func (c FightConfig) validate(decisionRadius float64) error {
	if c.ConfirmMS < 50 || c.ConfirmMS > 1000 {
		return fmt.Errorf("confirm_ms musi mieścić się w zakresie 50–1000 ms")
	}
	if c.LeaveFightMS < 200 || c.LeaveFightMS > 5000 {
		return fmt.Errorf("leave_fight_ms musi mieścić się w zakresie 200–5000 ms")
	}
	if c.TargetRetryMS < 300 || c.TargetRetryMS > 3000 {
		return fmt.Errorf("target_retry_ms musi mieścić się w zakresie 300–3000 ms")
	}
	if c.TargetAttempts < 1 || c.TargetAttempts > 10 {
		return fmt.Errorf("target_attempts musi mieścić się w zakresie 1–10")
	}
	if c.TargetBackoffMS < 500 || c.TargetBackoffMS > 30000 {
		return fmt.Errorf("target_backoff_ms musi mieścić się w zakresie 500–30000 ms")
	}
	if c.TargetStallMS < 3000 || c.TargetStallMS > 120000 {
		return fmt.Errorf("target_stall_ms musi mieścić się w zakresie 3000–120000 ms")
	}
	if c.FightPauseMS < 1000 || c.FightPauseMS > 60000 {
		return fmt.Errorf("fight_pause_ms musi mieścić się w zakresie 1000–60000 ms")
	}
	if c.Enabled && c.AttackKey == "" {
		return fmt.Errorf("włączenie ataku wymaga pola attack_key")
	}
	if c.AttackKey != "" && !input.ValidHotkey(c.AttackKey) {
		return fmt.Errorf("nieznany klawisz attack_key %q", c.AttackKey)
	}
	if len(c.Spells) > maxSpellRules {
		return fmt.Errorf("reguł czarów może być najwyżej %d, podano %d", maxSpellRules, len(c.Spells))
	}
	for i, r := range c.Spells {
		n := i + 1
		if !input.ValidHotkey(r.Hotkey) {
			return fmt.Errorf("reguła %d: nieznany klawisz %q", n, r.Hotkey)
		}
		if r.MinMonsters < 1 || r.MinMonsters > 64 {
			return fmt.Errorf("reguła %d: min_monsters musi mieścić się w zakresie 1–64", n)
		}
		if r.Radius < 0.5 || r.Radius > decisionRadius {
			return fmt.Errorf("reguła %d: promień musi mieścić się w zakresie 0,5–%.1f (promień decyzji)", n, decisionRadius)
		}
		if r.CooldownMS < 100 || r.CooldownMS > 60000 {
			return fmt.Errorf("reguła %d: cooldown musi mieścić się w zakresie 100–60000 ms", n)
		}
		if r.MinManaPct < 0 || r.MinManaPct > 99 {
			return fmt.Errorf("reguła %d: minimalna mana musi mieścić się w zakresie 0–99%%", n)
		}
	}
	return nil
}
