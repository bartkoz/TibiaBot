package fight

import "time"

// TargetInput is what one frame tells Targeter about the battle list's
// target row.
type TargetInput struct {
	HasTarget bool
	// TargetHP is 0-1, the targeted row's health bar - meaningful only when
	// HasTarget is true.
	TargetHP   float64
	Rows       int
	CapturedAt time.Time
}

// TargetDecision is what Targeter wants done this frame.
type TargetDecision struct {
	// Tap presses the configured attack key.
	Tap bool
	// Stall reports that the target has not lost health in TargetStall - the
	// loop should back out of the fight and pause, not keep trying.
	Stall  bool
	Reason string
}

// Targeter decides when to press the "attack next target" key, backs off
// after repeated fruitless presses, and watches for a target that never
// loses health (out of reach, behind an obstacle the client cannot path
// around).
type Targeter struct {
	lastTap  time.Time
	hasTap   bool
	lastSeen time.Time
	hasSeen  bool
	attempts int

	backoffUntil time.Time
	hasBackoff   bool

	minHP    float64
	hasMinHP bool

	stallSince time.Time
	hasStall   bool
}

// Decide answers what to do this frame. It never mutates emission-related
// state itself (attempts, the retry anchor) except through Tapped, which the
// caller invokes only once the driver confirms the key actually went out -
// a refused tap must cost nothing.
func (t *Targeter) Decide(in TargetInput, opts Options) TargetDecision {
	if in.HasTarget {
		t.lastSeen, t.hasSeen = in.CapturedAt, true
		t.attempts = 0
		if !t.hasMinHP || in.TargetHP < t.minHP {
			t.minHP, t.hasMinHP = in.TargetHP, true
			t.stallSince, t.hasStall = in.CapturedAt, true
		}
		if t.hasStall && in.CapturedAt.Sub(t.stallSince) >= opts.TargetStall {
			return TargetDecision{Stall: true, Reason: "cel nie traci HP"}
		}
		return TargetDecision{}
	}

	// No target this frame: whatever minimum-HP streak we were tracking
	// belongs to a target that is no longer visible.
	t.hasMinHP, t.hasStall = false, false

	if in.Rows == 0 {
		return TargetDecision{Reason: "brak wierszy battle listy"}
	}
	now := in.CapturedAt
	if t.hasBackoff {
		if now.Before(t.backoffUntil) {
			return TargetDecision{Reason: "backoff celowania"}
		}
		t.hasBackoff, t.attempts = false, 0
	}
	if t.attempts >= opts.TargetAttempts {
		t.hasBackoff, t.backoffUntil = true, now.Add(opts.TargetBackoff)
		return TargetDecision{Reason: "zbyt wiele prób bez ramki"}
	}
	// The anchor is the later of the last confirmed emission and the last
	// frame that actually showed a target - whichever gives the freshest
	// evidence about the client's state.
	anchor, anchorSet := t.lastSeen, t.hasSeen
	if t.hasTap && (!anchorSet || t.lastTap.After(anchor)) {
		anchor, anchorSet = t.lastTap, true
	}
	if anchorSet && now.Sub(anchor) < opts.TargetRetry {
		return TargetDecision{Reason: "za wcześnie na kolejną próbę"}
	}
	return TargetDecision{Tap: true}
}

// Tapped records that the attack key actually left the driver. Only this
// counts toward the attempt budget - a decision the driver refused changed
// nothing on screen.
func (t *Targeter) Tapped(at time.Time) {
	t.lastTap, t.hasTap = at, true
	t.attempts++
}

// Attempts reports how many fruitless emissions have happened since the
// last confirmed sighting or the last backoff reset - fightSnapshot (Task 7)
// publishes it so the panel can show why targeting is struggling.
func (t *Targeter) Attempts() int { return t.attempts }

// BackoffUntil reports the deadline of an active backoff, if one is
// currently running. The bool is false once the backoff has been consumed
// or none has ever started.
func (t *Targeter) BackoffUntil() (time.Time, bool) { return t.backoffUntil, t.hasBackoff }

// Reset clears all state, for leaving Fighting: a target lost when we walk
// away is not evidence about the next one.
func (t *Targeter) Reset() { *t = Targeter{} }
