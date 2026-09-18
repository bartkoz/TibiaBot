package fight

import "time"

// State is one of the two activities the loop can be in during this phase.
// Retreating (phase 5) and Looting (phase 4) are not part of this type yet -
// see the design spec's "Co zostaje na potem".
type State int

const (
	Travelling State = iota
	Fighting
)

func (s State) String() string {
	if s == Fighting {
		return "fighting"
	}
	return "travelling"
}

// Observation is what one frame tells Activity about the world. It carries
// no interpretation of its own: brain/fight.go computes every field from the
// loop's own state and hands over plain facts.
type Observation struct {
	// Enabled is the "Atakuj" switch and a calibrated battle list, combined -
	// Activity does not need to know which one is false.
	Enabled bool
	// InRange is the count of confirmed creatures within the decision
	// radius, after the map sieve - the same number the vision snapshot
	// publishes as MonstersInRange.
	InRange int
	// BattleRead is false when the battle-list region simply did not arrive
	// in this frame. Rows and HasTarget are meaningless when this is false.
	BattleRead bool
	Rows       int
	HasTarget  bool
	// Blocked is a stall pause or a pending Escape - either one refuses
	// entry into Fighting outright.
	Blocked    bool
	CapturedAt time.Time
}

// Options are the FightConfig durations, converted once per SetConfig rather
// than on every frame, and shared by Activity, Targeter and Engine so the
// loop does not juggle seven numbers at every call site.
type Options struct {
	Confirm        time.Duration
	LeaveFight     time.Duration
	TargetRetry    time.Duration
	TargetBackoff  time.Duration
	TargetStall    time.Duration
	TargetAttempts int
}

// Transition reports whether Observe just crossed a state boundary.
type Transition int

const (
	None Transition = iota
	Entered
	Left
)

// Activity is the Travelling/Fighting state machine. It holds no clock and
// touches nothing outside itself: Observe is a pure function of its inputs
// and its own debounce state.
type Activity struct {
	state   State
	inRange Presence
	leave   Presence
}

func (a *Activity) State() State { return a.state }

// Force sets the state directly, bypassing debounce, for the panel's
// "Atakuj" switch turning off and for a disarmed driver - both must take
// effect on the same frame, not after ConfirmMS. It also clears both
// debounce streaks, so a later entry always pays the full confirm duration
// again rather than resuming a streak that predates the forced change.
func (a *Activity) Force(to State) {
	a.state = to
	a.inRange = Presence{}
	a.leave = Presence{}
}

// Observe advances the machine by one frame and reports whether it just
// crossed into or out of Fighting.
func (a *Activity) Observe(o Observation, opts Options) Transition {
	switch a.state {
	case Travelling:
		ready := o.Enabled && o.BattleRead && o.Rows > 0 && o.InRange >= 1 && !o.Blocked
		confirmed := a.inRange.Observe(ready, o.CapturedAt, opts.Confirm)
		if !ready {
			return None
		}
		if confirmed {
			a.state = Fighting
			a.leave = Presence{}
			return Entered
		}
		return None
	case Fighting:
		// An unread battle list is unknown, not empty: Rows==0 from a frame
		// that never carried the region must never look like "no rows" and
		// drive an exit.
		wantLeave := o.BattleRead && !o.HasTarget && (o.InRange == 0 || o.Rows == 0)
		confirmed := a.leave.Observe(wantLeave, o.CapturedAt, opts.LeaveFight)
		if !wantLeave {
			return None
		}
		if confirmed {
			a.state = Travelling
			a.inRange = Presence{}
			return Left
		}
		return None
	}
	return None
}
