package brain

// This file owns the loop's healing step. It runs once per fresh frame, right
// after the bars are read and before the match starts: the client's camera is
// centred on the character, so healing needs neither a world position nor the
// minimap region, and a full search that takes seconds must not blind it.

import "time"

// healStep evaluates the rules against this frame's bar readings and presses at
// most one key. It records the emission in the engine only when the driver
// actually sent it, so a refusal - out of budget, window out of focus - leaves
// the rule free to try again on the next frame.
func (l *Loop) healStep(capturedAt time.Time) {
	l.healedLastFrame = false
	l.healState.Enabled = l.cfg.Heal.Enabled
	l.healState.RuleCount = len(l.cfg.Heal.Rules)
	if !l.cfg.Heal.Enabled {
		l.healState.Reason = "leczenie wyłączone"
		return
	}
	if !l.combat.Calibrated {
		l.healState.Reason = "brak kalibracji pasków"
		return
	}
	if l.deps.Driver == nil || !l.deps.Driver.Armed() {
		l.healState.Reason = "wykonawca jest rozbrojony"
		return
	}
	d := l.healer.Decide(l.hpReading, l.manaReading, capturedAt)
	if !d.Fire {
		l.healState.Reason = d.Reason
		return
	}
	now := l.deps.Now()
	res := l.deps.Driver.Heal(d.Hotkey, now.Sub(capturedAt))
	l.healState.Reason = res.Reason
	if res.Status != "emitted" {
		l.logf("leczenie regułą %d (%s) -> %s: %s", d.Index+1, d.Hotkey, res.Status, res.Reason)
		return
	}
	l.healer.Emitted(d.Hotkey, now)
	l.healedLastFrame = true
	l.healState.LastIndex, l.healState.LastHotkey = d.Index, d.Hotkey
	l.healedAt, l.hasHealed = now, true
	l.healState.Reason = ""
	l.logf("leczenie regułą %d: %s", d.Index+1, d.Hotkey)
}

// healSnapshot fills in the age of the last emission at publish time; the rest
// of HealState is written as it happens.
func (l *Loop) healSnapshot() HealState {
	out := l.healState
	if l.hasHealed {
		age := int(l.deps.Now().Sub(l.healedAt).Milliseconds())
		if age < 0 {
			age = 0
		}
		out.LastAgeMS = &age
	}
	return out
}
