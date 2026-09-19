package brain

// This file owns the loop's combat step: activity, targeting and spell
// rules. It runs once per fresh frame, right after healing and before the
// minimap search gate - the client's camera is centred on the character, so
// combat needs neither a world position nor the minimap region, and the map
// sieve's own position dependency is handled the same way finishVision
// handles it: fall back to whatever position the previous frame left.

import (
	"time"

	"minimap-lab/internal/fight"
	"minimap-lab/internal/vision"
)

// fightOptions converts FightConfig's millisecond fields to Durations once
// per call, the way healStep's caller already does for MinGap-adjacent
// values - fight.Activity/Targeter/Engine take Options rather than seven
// separate int arguments.
func (l *Loop) fightOptions() fight.Options {
	c := l.cfg.Fight
	return fight.Options{
		Confirm:        time.Duration(c.ConfirmMS) * time.Millisecond,
		LeaveFight:     time.Duration(c.LeaveFightMS) * time.Millisecond,
		TargetRetry:    time.Duration(c.TargetRetryMS) * time.Millisecond,
		TargetBackoff:  time.Duration(c.TargetBackoffMS) * time.Millisecond,
		TargetStall:    time.Duration(c.TargetStallMS) * time.Millisecond,
		TargetAttempts: c.TargetAttempts,
	}
}

// fightCrowd recomputes creature distances from the raw detected bars,
// applying the same map sieve as finishVision. It does not reuse
// CombatState/VisionView's own counts because those are filled by
// finishVision, which runs on handleFrame's deferred tail - after fightStep,
// not before it - and VisionView's bar list is capped at maxVisionBars for
// its own diagnostic-payload reasons, a cap Crowd must not inherit.
func (l *Loop) fightCrowd() fight.Crowd {
	var distances []float64
	for _, b := range l.bars {
		dx, dy := l.visionGrid.Offset(b)
		if l.blockedTile(dx, dy) {
			continue
		}
		distances = append(distances, vision.Distance(dx, dy))
	}
	mixed := !l.combat.BattleTruncated && l.combat.BattleRows > 0 && len(distances) > l.combat.BattleRows
	return fight.Crowd{Distances: distances, Mixed: mixed}
}

// targetHP reads the health of the battle-list row currently carrying the
// attack frame, from the same per-row data the vision preview uses.
func (l *Loop) targetHP() float64 {
	if l.combat.TargetRow == nil {
		return 0
	}
	i := *l.combat.TargetRow
	if i < 0 || i >= len(l.view.Battle) {
		return 0
	}
	return l.view.Battle[i].HP
}

func (l *Loop) blockMixedCrowd() bool {
	if l.cfg.Fight.BlockMixedCrowd == nil {
		return true
	}
	return *l.cfg.Fight.BlockMixedCrowd
}

// unreadBattleLimit bounds how long Fighting may survive without a single
// readable battle list. An unread list is "unknown, not empty", so it must
// never drive an ordinary exit - but unknown cannot mean forever: the region
// can stop arriving on its own (a cut that fails, a rectangle that slid off
// the captured area) while the rest of the calibration still looks healthy,
// and without a bound the machine would sit in Fighting with the route frozen
// at "Walka." until somebody noticed. Deliberately several times
// LeaveFightMS's own default, so this stays a backstop rather than becoming a
// second exit rule.
const unreadBattleLimit = 3 * time.Second

// abandonFight drives the machine out of Fighting when the loop has lost the
// ability to watch a fight at all, rather than because the fight ended. It
// orders the Escape for the same reason the "Atakuj" switch going off does:
// we can no longer see what is happening, but the client may well still be
// attacking something.
func (l *Loop) abandonFight(capturedAt time.Time, reason string) {
	if l.activity.State() == fight.Fighting {
		l.escapeDue = true
	}
	l.activity.Force(fight.Travelling)
	l.targeter.Reset()
	l.unreadBattle = fight.Presence{}
	l.fightState.Activity = fight.Travelling.String()
	l.fightState.EscapeDue = l.escapeDue
	l.fightState.Reason = reason
	l.sendPendingEscape(capturedAt)
}

// sendPendingEscape carries out an ordered cancel, if this frame still has a
// key to spend and a driver to spend it on. Every gate that returns early
// calls it rather than merely queueing: while that gate's own condition
// holds, every later frame returns at the same place, so an Escape left for
// "the next frame" would wait until the user fixed whatever tripped the gate
// - and meanwhile the client keeps chasing, which is the one thing escapeDue
// exists to stop. A refusal still costs the frame its key: the attempt was
// made, and the order stays pending for the next one.
func (l *Loop) sendPendingEscape(capturedAt time.Time) {
	if !l.escapeDue || l.healedLastFrame || l.fightKeyLastFrame {
		return
	}
	if l.deps.Driver == nil || !l.deps.Driver.Armed() {
		return
	}
	res := l.deps.Driver.CancelTarget(l.deps.Now().Sub(capturedAt))
	l.fightState.Reason = res.Reason
	l.fightKeyLastFrame = true
	if res.Status == "emitted" {
		l.escapeDue, l.fightState.EscapeDue = false, false
	}
}

// fightStep evaluates activity, targeting and spell rules for this frame and
// presses at most one non-heal key. Priority within the frame: healing (run
// before this step) beats a pending Escape, which beats targeting, which
// beats a spell - fightKeyLastFrame preempts the walking step exactly the
// way healedLastFrame does.
func (l *Loop) fightStep(capturedAt time.Time) {
	l.fightKeyLastFrame = false
	l.fightState.Reason = ""
	l.fightState.Enabled = l.cfg.Fight.Enabled

	if !l.cfg.Fight.Enabled {
		if l.activity.State() == fight.Fighting {
			l.escapeDue = true
		}
		l.activity.Force(fight.Travelling)
		l.targeter.Reset()
		l.unreadBattle = fight.Presence{}
		l.fightState = FightState{Enabled: false, Activity: fight.Travelling.String(), EscapeDue: l.escapeDue}
		l.sendPendingEscape(capturedAt)
		return
	}
	// Both halves of the calibration are checked, not just the battle
	// rectangle: Combat.Enabled() is Viewport AND Crop, so observeVision can
	// return before it ever looks at the battle region while Battle itself is
	// still non-empty - a combination combatconfig.go explicitly calls legal.
	// Gating on combat.Calibrated as well is also what makes this step agree
	// with healStep about what "calibrated" means.
	if !l.combat.Calibrated || l.cfg.Combat.Battle.Empty() {
		reason := "brak kalibracji battle listy"
		if !l.combat.Calibrated {
			reason = "brak kalibracji widzenia"
		}
		l.abandonFight(capturedAt, reason)
		return
	}
	if l.deps.Driver == nil || !l.deps.Driver.Armed() {
		// The same gate healStep and follow()'s walk step already carry, and
		// for a stronger reason here: -input=off leaves Deps.Driver nil, so
		// without the nil half every key this step presses would panic on the
		// loop goroutine and take the process with it. Unlike the two
		// branches above this one orders no Escape: a driver that cannot be
		// handed a key cannot be handed that one either, and re-arming pays
		// the full entry debounce again, which Force's own reset guarantees.
		l.activity.Force(fight.Travelling)
		l.targeter.Reset()
		l.fightState.Activity = fight.Travelling.String()
		l.fightState.Reason = "wykonawca jest rozbrojony"
		return
	}

	if l.unreadBattle.Observe(!l.battleRead, capturedAt, unreadBattleLimit) &&
		l.activity.State() == fight.Fighting {
		l.abandonFight(capturedAt, "battle lista nie dociera")
		return
	}

	cc := l.cfg.Combat
	crowd := l.fightCrowd()
	now := l.deps.Now()
	paused := l.hasPause && now.Before(l.pauseUntil)
	if l.hasPause && !paused {
		l.hasPause = false
	}
	opts := l.fightOptions()
	obs := fight.Observation{
		Enabled: true, InRange: crowd.Within(cc.DecisionRadius), BattleRead: l.battleRead,
		Rows: l.combat.BattleRows, HasTarget: l.combat.TargetRow != nil,
		Blocked: paused || l.escapeDue, CapturedAt: capturedAt,
	}
	switch l.activity.Observe(obs, opts) {
	case fight.Entered:
		// The in-flight step is abandoned without learning a blocked tile:
		// the key already left, but whether the character actually arrived
		// now depends on the client's chase and any creature in the way, not
		// on a wall - crediting that to the block-learning executor would be
		// exactly the "pending-step misattribution" the combat design spec
		// warned about.
		l.executor.DropPending()
	case fight.Left:
		l.escapeDue = true
		l.targeter.Reset()
	}
	l.fightState.Activity = l.activity.State().String()
	l.fightState.EscapeDue = l.escapeDue

	if l.healedLastFrame {
		l.fightState.Reason = "leczenie zajęło klatkę"
		return
	}

	if l.escapeDue {
		l.sendPendingEscape(capturedAt)
		return
	}

	if l.activity.State() != fight.Fighting {
		return
	}

	age := now.Sub(capturedAt)
	td := l.targeter.Decide(fight.TargetInput{
		HasTarget: obs.HasTarget, TargetHP: l.targetHP(), Rows: obs.Rows, CapturedAt: capturedAt,
	}, opts)
	l.fightState.TargetAttempts = l.targeter.Attempts()
	if td.Stall {
		l.activity.Force(fight.Travelling)
		l.escapeDue = true
		l.pauseUntil, l.hasPause = now.Add(time.Duration(l.cfg.Fight.FightPauseMS)*time.Millisecond), true
		l.targeter.Reset()
		l.fightState.Activity = fight.Travelling.String()
		l.fightState.Reason = td.Reason
		return
	}
	if td.Tap {
		res := l.deps.Driver.Cast(l.cfg.Fight.AttackKey, age)
		l.fightState.Reason = res.Reason
		if res.Status == "emitted" {
			l.targeter.Tapped(now)
			l.fightState.TargetAttempts = l.targeter.Attempts()
			l.fightKeyLastFrame = true
		}
		return
	}
	if td.Reason != "" {
		l.fightState.Reason = td.Reason
	}

	d := l.engine.Decide(crowd, l.manaReading, obs.HasTarget, l.blockMixedCrowd(), capturedAt, opts.Confirm)
	if !d.Fire {
		if l.fightState.Reason == "" {
			l.fightState.Reason = d.Reason
		}
		return
	}
	res := l.deps.Driver.Cast(d.Hotkey, age)
	l.fightState.Reason = res.Reason
	if res.Status == "emitted" {
		l.engine.Emitted(d.Hotkey, now)
		l.fightKeyLastFrame = true
		l.fightState.LastSpell = d.Hotkey
		l.lastSpellAt, l.hasLastSpell = now, true
	}
}

// fightSnapshot fills in the ages of the events currently in flight at
// publish time; the rest of FightState is written as it happens - the same
// split healSnapshot uses.
func (l *Loop) fightSnapshot() FightState {
	out := l.fightState
	now := l.deps.Now()
	if l.hasPause {
		left := int(l.pauseUntil.Sub(now).Milliseconds())
		if left < 0 {
			left = 0
		}
		out.PauseMSLeft = &left
	}
	if until, ok := l.targeter.BackoffUntil(); ok {
		left := int(until.Sub(now).Milliseconds())
		if left < 0 {
			left = 0
		}
		out.BackoffMSLeft = &left
	}
	if l.hasLastSpell {
		age := int(now.Sub(l.lastSpellAt).Milliseconds())
		if age < 0 {
			age = 0
		}
		out.LastSpellAgeMS = &age
	}
	return out
}
