package brain

import (
	"time"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/route"
)

// executorCompass maps a sign pair to a compass direction. The middle entry is
// empty on purpose: no displacement is no direction, and sending the driver an
// empty one would be refused as "nieznany kierunek".
var executorCompass = [3][3]string{
	{"NW", "N", "NE"},
	{"W", "", "E"},
	{"SW", "S", "SE"},
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func stepDirection(from mapdata.Position, to route.Waypoint) string {
	return executorCompass[sign(to.Y-from.Y)+1][sign(to.X-from.X)+1]
}

// minStillFrames matches the server's own gate. One stale reading is not proof
// the character stayed put; three consecutive ones are the cheapest evidence
// that rules out a single dropped frame.
const minStillFrames = 3

type Action string

const (
	ActionDone       Action = "done"
	ActionWalk       Action = "walk"
	ActionTransition Action = "transition"
	ActionPath       Action = "path"
	ActionWait       Action = "wait"
	ActionBlocked    Action = "blocked"
)

// Output is what the follower wants done next.
type Output struct {
	Action    Action
	Direction string
	// Next is the tile a walk step aims at.
	Next      [2]int
	Remaining int
	Index     int
	Waypoint  *route.Waypoint
	// NextWaypoint is the waypoint after the current one, needed to work out
	// which way a flight of stairs is climbed.
	NextWaypoint *route.Waypoint
	Instruction  string
	From, To     mapdata.Position
	Status       string
	Reason       string
}

// Intent is one thing to press right now.
type Intent struct {
	Action    string // "walk" or "transition"
	Direction string
	Type      string
	Waypoint  int
}

type ExecState struct {
	Waiting    bool
	Retries    int
	Cycles     int
	Blocked    bool
	Halted     bool
	Stopped    bool
	ActionDone bool
	// AwaitingEmit is true while a step is pending but the key has not been
	// confirmed as having left the driver - distinct from waiting on the
	// character to move.
	AwaitingEmit bool
	StepID       uint64
}

// target identifies the tile a step aims at. FloorKnown is false for walk
// steps, whose floor is whatever the character is standing on, so that a walk
// and a transition at the same x,y are never mistaken for the same target.
type target struct {
	X, Y, Z    int
	FloorKnown bool
}

func (t target) same(other target) bool { return t == other }

func targetFromOut(out *Output) (target, bool) {
	if out == nil {
		return target{}, false
	}
	switch out.Action {
	case ActionWalk:
		return target{X: out.Next[0], Y: out.Next[1]}, true
	case ActionTransition:
		if out.Waypoint == nil {
			return target{}, false
		}
		return target{X: out.Waypoint.X, Y: out.Waypoint.Y, Z: out.Waypoint.Z, FloorKnown: true}, true
	}
	return target{}, false
}

type pendingStep struct {
	kind      string // "walk" or "transition"
	target    [2]int
	x, y, z   int
	viaHotkey bool
	from      *mapdata.Position
	// stillFrames counts readings after the emission that showed the character
	// on the same tile it started from.
	stillFrames  int
	lastFrameAt  time.Time
	hasLastFrame bool
	emittedAt    time.Time
	hasEmitted   bool
	sentAt       time.Time
	id           uint64
}

func (p *pendingStep) at() target {
	if p.kind == "walk" {
		return target{X: p.target[0], Y: p.target[1]}
	}
	return target{X: p.x, Y: p.y, Z: p.z, FloorKnown: true}
}

type ExecutorOptions struct {
	StepTimeout     time.Duration
	LateArrival     time.Duration
	BlockedTTL      time.Duration
	ActionTimeout   time.Duration
	MaxFailedCycles int
}

// Executor runs one step at a time and decides what a step that did not pan
// out means. Nothing here touches the network or the clock: the loop feeds it
// follower output and confirmed positions, and it answers with the one intent
// that may be sent right now, or nothing.
type Executor struct {
	// stepTimeout is 1800 ms, not 1200: step duration in the game scales with
	// the character's speed and the ground cost, and a step on mud or under
	// paralysis runs well past a second. A timeout shorter than the step
	// itself would turn ordinary slow movement into learned blockages.
	stepTimeout time.Duration
	// lateArrival is how long after a timeout an arrival still counts as "that
	// was lag, not a wall" and revokes what the failure taught. Generous on
	// purpose: revoking a lesson that turned out wrong costs nothing, while
	// keeping a false block costs a permanently avoided tile.
	lateArrival time.Duration
	// blockedTTL matches the server's temporary-block TTL. A blocked target is
	// a suspicion with a shelf life, not a verdict.
	blockedTTL      time.Duration
	actionTimeout   time.Duration
	maxFailedCycles int

	// nextID is never reset, including across Reset(), so a late confirmation
	// for a step from before a reset can never be mistaken for a new one.
	nextID uint64

	pending *pendingStep
	last    *mapdata.Position
	retries int
	cycles  int

	blocked       bool
	blockedTarget target
	blockedAt     time.Time
	hasBlockedAt  bool

	// currentTarget is what the retries count is charging failures against.
	currentTarget    target
	hasCurrentTarget bool

	halted     bool
	stopped    bool
	actionDone bool
}

func NewExecutor(o ExecutorOptions) *Executor {
	e := &Executor{
		stepTimeout:     o.StepTimeout,
		lateArrival:     o.LateArrival,
		blockedTTL:      o.BlockedTTL,
		actionTimeout:   o.ActionTimeout,
		maxFailedCycles: o.MaxFailedCycles,
		nextID:          1,
	}
	if e.stepTimeout == 0 {
		e.stepTimeout = 1800 * time.Millisecond
	}
	if e.lateArrival == 0 {
		e.lateArrival = 2000 * time.Millisecond
	}
	if e.blockedTTL == 0 {
		e.blockedTTL = 60 * time.Second
	}
	if e.actionTimeout == 0 {
		e.actionTimeout = 5 * time.Second
	}
	if e.maxFailedCycles == 0 {
		e.maxFailedCycles = 3
	}
	return e
}

func (e *Executor) StepTimeout() time.Duration { return e.stepTimeout }

// Reset clears everything except the step id counter.
func (e *Executor) Reset() {
	e.pending, e.last = nil, nil
	e.retries, e.cycles = 0, 0
	e.blocked, e.blockedTarget, e.hasBlockedAt = false, target{}, false
	e.currentTarget, e.hasCurrentTarget = target{}, false
	e.halted, e.stopped, e.actionDone = false, false, false
}

func (e *Executor) State() ExecState {
	s := ExecState{
		Waiting: e.pending != nil, Retries: e.retries, Cycles: e.cycles,
		Blocked: e.blocked, Halted: e.halted, Stopped: e.stopped, ActionDone: e.actionDone,
	}
	if e.pending != nil {
		s.AwaitingEmit = !e.pending.hasEmitted
		s.StepID = e.pending.id
	}
	return s
}

// startTarget marks which tile the current retries count applies to. A target
// that differs from whatever it was tracking - the route was recomputed - gets
// a fresh retry allowance instead of inheriting a failure count that was never
// charged against it.
func (e *Executor) startTarget(t target) {
	if !e.hasCurrentTarget || !t.same(e.currentTarget) {
		e.retries = 0
	}
	e.currentTarget, e.hasCurrentTarget = t, true
}

func (e *Executor) clearBlocked() {
	e.blocked, e.blockedTarget, e.hasBlockedAt = false, target{}, false
}

func (e *Executor) done() {
	e.pending = nil
	e.retries, e.cycles = 0, 0
	e.clearBlocked()
	e.afterSuccess()
}

// IntentFor returns what to send now, or reports that the executor must wait.
func (e *Executor) IntentFor(out *Output, now time.Time) (Intent, bool) {
	if e.stopped || e.halted {
		return Intent{}, false
	}
	if p := e.pending; p != nil {
		if !p.hasEmitted {
			// The key press was never confirmed. Waiting forever would freeze
			// the executor silently, looking exactly like a legitimate wait,
			// so give up on it too after a generous grace period.
			if now.Sub(p.sentAt) < 2*e.stepTimeout {
				return Intent{}, false
			}
			e.failPending(p, now)
		} else {
			limit := e.stepTimeout
			if p.kind == "transition" {
				limit = e.actionTimeout
			}
			if now.Sub(p.emittedAt) < limit {
				return Intent{}, false
			}
			e.failPending(p, now)
		}
		if e.stopped {
			return Intent{}, false
		}
	}
	if e.blocked {
		// blocked means "this particular target cannot be reached", not "stop
		// forever": once the follower asks for something else, the route was
		// recomputed and the new target deserves a fresh attempt.
		//
		// It also lapses on its own. The server penalises a temporary block
		// rather than walling it off, so A* may keep routing through the very
		// same tile - the target never changes, and without a deadline the
		// executor would refuse it for the rest of the session.
		if e.hasBlockedAt && now.Sub(e.blockedAt) >= e.blockedTTL {
			// A lapsed block starts a fresh attempt, not the continuation of
			// the failed series that produced it. Carrying the cycle count
			// over would stop the bot on its very next try - the one that
			// would have taught the permanent block.
			e.clearBlocked()
			e.cycles = 0
		} else {
			t, ok := targetFromOut(out)
			if !ok || t.same(e.blockedTarget) {
				return Intent{}, false
			}
			e.clearBlocked()
		}
	}
	if out == nil {
		return Intent{}, false
	}
	switch out.Action {
	case ActionWalk:
		t, _ := targetFromOut(out)
		e.startTarget(t)
		// from is where the character stood when the key was sent. Taking it
		// from the first observation after the press would make "did not move"
		// and "moved somewhere unexpected" indistinguishable.
		e.pending = &pendingStep{kind: "walk", target: out.Next, from: e.copyLast(), sentAt: now, id: e.takeID()}
		return Intent{Action: "walk", Direction: out.Direction}, true
	case ActionTransition:
		wp := out.Waypoint
		if wp == nil {
			return Intent{}, false
		}
		// Stairs carry no item: the tile is on the current floor and stepping
		// onto it moves the character. The next waypoint says which way.
		if wp.Type == "stairs" {
			if out.NextWaypoint == nil || e.last == nil {
				return Intent{}, false
			}
			direction := stepDirection(*e.last, *out.NextWaypoint)
			// A stairs waypoint and its landing point recorded at the same x,y
			// is normal for straight-up stairs. The direction is then empty,
			// which the driver refuses, so send nothing and leave it to the
			// human, exactly like an unknown position does.
			if direction == "" {
				return Intent{}, false
			}
			t, _ := targetFromOut(out)
			e.startTarget(t)
			e.pending = &pendingStep{kind: "transition", x: wp.X, y: wp.Y, z: wp.Z,
				from: e.copyLast(), sentAt: now, id: e.takeID()}
			return Intent{Action: "walk", Direction: direction}, true
		}
		e.actionDone = false
		t, _ := targetFromOut(out)
		e.startTarget(t)
		e.pending = &pendingStep{kind: "transition", x: wp.X, y: wp.Y, z: wp.Z, viaHotkey: true,
			from: e.copyLast(), sentAt: now, id: e.takeID()}
		return Intent{Action: "transition", Type: wp.Type, Waypoint: out.Index}, true
	}
	return Intent{}, false
}

func (e *Executor) takeID() uint64 {
	id := e.nextID
	e.nextID++
	return id
}

func (e *Executor) copyLast() *mapdata.Position {
	if e.last == nil {
		return nil
	}
	p := *e.last
	return &p
}

// failPending is the consequence of a pending step that did not pan out,
// whether it timed out waiting for movement or was never confirmed as emitted
// at all: charge a cycle, stop the executor if that was one too many,
// otherwise allow one retry or, on the second failure of the same target,
// block it until the route changes. blocked never means "stop forever" - only
// cycles does.
func (e *Executor) failPending(p *pendingStep, now time.Time) {
	e.noteFailure(p, now)
	e.pending = nil
	e.cycles++
	if e.cycles >= e.maxFailedCycles {
		e.stopped = true
		return
	}
	if e.retries >= 1 {
		e.blocked = true
		e.blockedTarget = p.at()
		e.blockedAt, e.hasBlockedAt = now, true
		e.retries = 0
		return
	}
	e.retries++
}

// ClearActionDone is called once the loop has acted on a completed floor
// action. Without it actionDone stays true until the next transition intent is
// created, which may never happen again once the route moves past its last
// floor action.
func (e *Executor) ClearActionDone() { e.actionDone = false }

// DropPending abandons the current attempt without charging a failure: a
// refusal (rate limit, unknown hotkey, lost focus) means the key was never
// sent, so nothing was learned about whether the target is reachable.
// Escalation counters are untouched; only the stale attempt is cleared so the
// next reading gets a fresh intent instead of waiting out the full emission
// grace period for no reason.
func (e *Executor) DropPending() { e.pending = nil }

// Emitted records when the key actually left the driver, correlated by id so a
// late confirmation for a step already dropped cannot be mistaken for proof
// about whatever pending replaced it.
func (e *Executor) Emitted(now time.Time, id uint64) {
	if e.pending != nil && e.pending.id == id {
		e.pending.emittedAt, e.pending.hasEmitted = now, true
	}
}

func (e *Executor) Observe(p *mapdata.Position, capturedAt, now time.Time) {
	if p == nil {
		e.halted, e.pending = true, nil
		return
	}
	e.halted = false
	// Kept for stairs, whose direction comes from the current tile rather than
	// from a path.
	current := *p
	e.last = &current
	e.noteLateArrival(current, capturedAt, now)
	pending := e.pending
	if pending == nil || !pending.hasEmitted || !capturedAt.After(pending.emittedAt) {
		return
	}
	if pending.kind == "transition" {
		// A floor change is the only proof, whether an item was used or the
		// character simply walked onto stairs.
		if p.Z != pending.z {
			viaHotkey := pending.viaHotkey
			e.done()
			if viaHotkey {
				e.actionDone = true
			}
			return
		}
		// Same floor: no proof yet. If the character is no longer where it
		// stood when the key was sent, the situation changed - pushed by a
		// creature, or the player took over - so drop the step without
		// charging retries or cycles.
		if !stillThere(pending, current) {
			e.pending = nil
		}
		return
	}
	// A step that changed the floor is not a failed step: walking onto stairs
	// does exactly this. Judging it as one would teach the bot that stairs
	// are a wall.
	if pending.from != nil && p.Z != pending.from.Z {
		e.pending = nil
		return
	}
	if p.X == pending.target[0] && p.Y == pending.target[1] {
		e.done()
		return
	}
	// Standing still is a failed step and belongs to the retry counter, which
	// IntentFor bumps after the timeout. Standing somewhere else entirely is a
	// changed situation: drop the step and let the follower replan. With no
	// reference tile the step cannot be judged, so it is left to the timeout
	// rather than guessed at.
	if !stillThere(pending, current) {
		e.pending = nil
		return
	}
	pending.stillFrames++
	pending.lastFrameAt, pending.hasLastFrame = capturedAt, true
}

func stillThere(p *pendingStep, at mapdata.Position) bool {
	return p.from == nil || (at.X == p.from.X && at.Y == p.from.Y)
}

// The three hooks below are where the executor turns step outcomes into
// evidence about the map. They are separated from the step lifecycle above
// because they answer a different question: the lifecycle decides what to
// press next, these decide what a failure is allowed to teach.

func (e *Executor) afterSuccess() {}

func (e *Executor) noteFailure(p *pendingStep, now time.Time) {}

func (e *Executor) noteLateArrival(at mapdata.Position, capturedAt, now time.Time) {}
