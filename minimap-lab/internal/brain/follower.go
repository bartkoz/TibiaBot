package brain

import (
	"fmt"
	"time"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/nav"
	"minimap-lab/internal/route"
)

var transitionInstructions = map[string]string{
	"rope":   "Użyj liny",
	"ladder": "Wejdź po drabinie",
	"stairs": "Wejdź na schody",
	"hole":   "Zejdź dziurą",
	"shovel": "Kop łopatą",
	"walk":   "Przejdź na piętro",
}

func chebyshev(w route.Waypoint, p mapdata.Position) int {
	dx, dy := abs(w.X-p.X), abs(w.Y-p.Y)
	if dx > dy {
		return dx
	}
	return dy
}

func sameTile(a, b route.Waypoint) bool { return a.X == b.X && a.Y == b.Y && a.Z == b.Z }

type FollowerOptions struct {
	// Tolerance is a pointer for the same reason pathapi.go makes coordinates
	// pointers: an unset field has to be distinguishable from a deliberate
	// zero. Nil means the default of one tile; zero means the character must
	// land exactly on the waypoint, which is a legitimate strict setting.
	Tolerance *int
	// ActionTolerance needs no such treatment - its default is zero anyway. A
	// rope used one tile off the rope spot does nothing at all, while walking
	// tolerance may stay loose.
	ActionTolerance int
	Loop            bool
	Replan          time.Duration
	Retry           time.Duration
}

type blockedInfo struct {
	status, reason string
}

// Follower turns a route and a position into what the player should do next.
// It knows nothing about keyboards or timeouts - only about where the route
// goes and whether a usable path to the next waypoint exists.
type Follower struct {
	waypoints []route.Waypoint
	tolerance int
	// actionTolerance is read on its own rather than falling back to
	// tolerance: the two answer different questions.
	actionTolerance int
	loop            bool
	replan          time.Duration
	// retry exists because a failure is a moment, not a verdict: one dropped
	// request must not freeze guidance until the user toggles following off
	// and on again.
	retry time.Duration

	index    int
	finished bool

	path   [][2]int
	pathTo *route.Waypoint

	blocked      *blockedInfo
	blockedAt    time.Time
	blockedFrom  *mapdata.Position
	hasBlockedAt bool

	requestedAt    time.Time
	hasRequestedAt bool

	// actionAt remembers that the character was standing on an action waypoint
	// and on which floor, so the floor change that follows can be recognised as
	// that action having been carried out.
	actionAt      *actionArmed
	minOverlayRev uint64
}

type actionArmed struct {
	index int
	z     int
}

func NewFollower(waypoints []route.Waypoint, o FollowerOptions) *Follower {
	f := &Follower{
		waypoints:       waypoints,
		tolerance:       1,
		actionTolerance: o.ActionTolerance,
		loop:            o.Loop,
		replan:          o.Replan,
		retry:           o.Retry,
	}
	if o.Tolerance != nil {
		f.tolerance = *o.Tolerance
	}
	if f.replan == 0 {
		f.replan = 500 * time.Millisecond
	}
	if f.retry == 0 {
		f.retry = 4 * time.Second
	}
	return f
}

func (f *Follower) Index() int     { return f.index }
func (f *Follower) Finished() bool { return f.finished }
func (f *Follower) Path() [][2]int { return f.path }

func (f *Follower) SetMinOverlayRevision(rev uint64) { f.minOverlayRev = rev }

func (f *Follower) Waypoint() *route.Waypoint {
	if f.finished || f.index < 0 || f.index >= len(f.waypoints) {
		return nil
	}
	return &f.waypoints[f.index]
}

func (f *Follower) Reset() {
	f.index, f.finished = 0, false
	f.DropPath()
	f.actionAt = nil
}

// SkipTo jumps to a waypoint, dropping work done for the previous one.
func (f *Follower) SkipTo(i int) {
	if len(f.waypoints) == 0 {
		return
	}
	if i < 0 {
		i = 0
	}
	if i > len(f.waypoints)-1 {
		i = len(f.waypoints) - 1
	}
	f.index, f.finished = i, false
	// A skipped-away action must be armed again rather than remembered: the
	// floor change that follows belongs to whatever the route is doing now.
	f.actionAt = nil
	f.DropPath()
}

func (f *Follower) DropPath() {
	f.path, f.pathTo = nil, nil
	f.blocked, f.blockedFrom, f.hasBlockedAt = nil, nil, false
	f.hasRequestedAt = false
}

// SetPath installs a planner reply. askedFor is the waypoint the request was
// made for; a reply that took long enough for the target to move describes a
// route the follower no longer wants.
func (f *Follower) SetPath(res *nav.PathResult, now time.Time, askedFor *route.Waypoint) {
	target := f.Waypoint()
	// Leave state untouched: whatever replaced this request is more current.
	if target == nil || askedFor == nil || !sameTile(*askedFor, *target) {
		return
	}
	// Same reasoning, one dimension further: the waypoint may be unchanged
	// while the map underneath it is not. Revision zero means the reply
	// carried none - a locally produced failure - and is also the state of a
	// store that has learned nothing, so nothing can be stale against it.
	if res != nil && res.OverlayRevision > 0 && res.OverlayRevision < f.minOverlayRev {
		return
	}
	f.requestedAt, f.hasRequestedAt = now, true
	if res == nil || !res.Found {
		f.path, f.pathTo = nil, nil
		f.blocked = nil
		if res != nil {
			f.blocked = &blockedInfo{status: res.Status, reason: res.Reason}
		}
		f.blockedAt, f.hasBlockedAt, f.blockedFrom = now, true, nil
		return
	}
	f.blocked = nil
	f.path = make([][2]int, len(res.Steps))
	copy(f.path, res.Steps)
	kept := *target
	f.pathTo = &kept
}

func (f *Follower) Step(p mapdata.Position, now time.Time) Output {
	target := f.advance(p)
	if target == nil {
		return Output{Action: ActionDone}
	}
	standingOnAction := target.Type != "walk" && target.Z == p.Z && chebyshev(*target, p) <= f.actionTolerance
	if standingOnAction || target.Z != p.Z {
		f.DropPath()
		if standingOnAction {
			f.actionAt = &actionArmed{index: f.index, z: p.Z}
		}
		verb, ok := transitionInstructions[target.Type]
		if !ok {
			verb = transitionInstructions["walk"]
		}
		floor := ""
		if !standingOnAction {
			floor = fmt.Sprintf(" → piętro %d", target.Z)
		}
		out := Output{Action: ActionTransition, Index: f.index, Waypoint: target,
			Instruction: verb + floor}
		if f.index+1 < len(f.waypoints) {
			out.NextWaypoint = &f.waypoints[f.index+1]
		}
		return out
	}
	if f.pathTo != nil && !sameTile(*f.pathTo, *target) {
		f.DropPath()
	}
	if ahead := remainingPath(f.path, p); len(ahead) > 1 {
		return Output{Action: ActionWalk, Direction: directionTo(p, ahead[1]),
			Next: ahead[1], Remaining: len(ahead) - 1, Waypoint: target}
	}
	if f.blocked != nil {
		moved := f.blockedFrom != nil && *f.blockedFrom != p
		waited := f.hasBlockedAt && now.Sub(f.blockedAt) >= f.retry
		if !moved && !waited {
			if f.blockedFrom == nil {
				here := p
				f.blockedFrom = &here
			}
			return Output{Action: ActionBlocked, Waypoint: target,
				Status: f.blocked.status, Reason: f.blocked.reason}
		}
		// The situation changed, so ask again now rather than waiting out the
		// replan throttle on top of the backoff already served.
		f.blocked, f.blockedFrom, f.hasBlockedAt = nil, nil, false
		f.hasRequestedAt = false
	}
	if f.hasRequestedAt && now.Sub(f.requestedAt) < f.replan {
		return Output{Action: ActionWait, Waypoint: target}
	}
	f.requestedAt, f.hasRequestedAt = now, true
	f.path = nil
	return Output{Action: ActionPath, From: p,
		To: mapdata.Position{X: target.X, Y: target.Y, Z: target.Z}, Waypoint: target}
}

// advance consumes every waypoint already reached and returns the next one.
func (f *Follower) advance(p mapdata.Position) *route.Waypoint {
	// One pass at most: a looped route whose points all sit within tolerance
	// would otherwise cycle forever and re-request a path on every reading.
	for visited := 0; !f.finished; visited++ {
		if visited > len(f.waypoints) {
			f.finished = true
			return nil
		}
		if f.index < 0 || f.index >= len(f.waypoints) {
			f.finished = true
			return nil
		}
		target := &f.waypoints[f.index]
		// An action waypoint is done once the action has been carried out,
		// which the tracker reports as a change of floor. The player can also
		// cross between two readings, never being seen standing on the
		// waypoint - then standing on the floor the route continues on is the
		// evidence.
		armed := f.actionAt != nil && f.actionAt.index == f.index && f.actionAt.z != p.Z
		crossed := false
		if target.Type != "walk" && target.Z != p.Z && f.index+1 < len(f.waypoints) {
			crossed = f.waypoints[f.index+1].Z == p.Z
		}
		acted := armed || crossed
		if acted {
			f.actionAt = nil
		}
		if !acted {
			if target.Z != p.Z || chebyshev(*target, p) > f.tolerance {
				return target
			}
			if target.Type != "walk" {
				return target
			}
		}
		f.DropPath()
		switch {
		case f.index+1 < len(f.waypoints):
			f.index++
		case f.loop && len(f.waypoints) > 0:
			f.index = 0
		default:
			f.finished = true
			return nil
		}
	}
	return nil
}

// remainingPath cuts the path back to the player's tile. A player standing off
// the path gets nothing, which sends the follower back to the planner.
func remainingPath(path [][2]int, p mapdata.Position) [][2]int {
	for i, s := range path {
		if s[0] == p.X && s[1] == p.Y {
			return path[i:]
		}
	}
	return nil
}

func directionTo(from mapdata.Position, next [2]int) string {
	return executorCompass[sign(next[1]-from.Y)+1][sign(next[0]-from.X)+1]
}

// SetOptions adjusts the follower in place. Rebuilding it would be simpler but
// would throw away which waypoint the route had reached, so a user nudging the
// tolerance mid-route would be sent back to the start.
func (f *Follower) SetOptions(o FollowerOptions) {
	if o.Tolerance != nil {
		f.tolerance = *o.Tolerance
	}
	f.actionTolerance = o.ActionTolerance
	f.loop = o.Loop
	if o.Replan > 0 {
		f.replan = o.Replan
	}
	if o.Retry > 0 {
		f.retry = o.Retry
	}
}
