// Package fight is a pure decision layer for combat: activity, targeting and
// spell rules. Nothing here owns a clock, reads a pixel or touches the
// keyboard - it takes numbers and a frame's capture time, and hands back a
// decision. That is what lets the whole package be tested as a table, the
// same principle internal/heal already uses.
package fight

import "time"

// gapReset is how long a break between two observations may be before the
// streak they were building restarts. A camera that stalled and came back
// cannot claim it "had" the confirm duration behind it when nobody was
// watching during the gap.
const gapReset = 500 * time.Millisecond

// Presence answers "has this logical condition held long enough" for one
// caller-defined condition. The zero value is ready to use.
type Presence struct {
	since   time.Time
	lastAt  time.Time
	frames  int
	hasLast bool
}

// Observe records whether the condition held on a fresh frame and reports
// whether it has held continuously for at least confirm duration and across
// at least two distinct frames. Three properties, each answering one named
// failure mode:
//   - two frames AND confirm duration: neither alone is enough - one long
//     frame would let a single artefact through on time alone, two frames a
//     heartbeat apart would let two artefacts through on count alone.
//   - a gap longer than gapReset restarts the streak: a camera that stalled
//     cannot claim a duration nobody actually observed.
//   - a duplicate observation at the exact same capturedAt does not count as
//     a second frame - callers already drop duplicate video frames before
//     this is ever called, but Observe stays correct even if one slips
//     through.
func (p *Presence) Observe(holds bool, capturedAt time.Time, confirm time.Duration) bool {
	if !holds {
		*p = Presence{}
		return false
	}
	switch {
	case !p.hasLast:
		p.since, p.frames = capturedAt, 1
	case capturedAt.Equal(p.lastAt):
		// Same instant seen twice - not a new observation.
	case capturedAt.Sub(p.lastAt) > gapReset:
		p.since, p.frames = capturedAt, 1
	default:
		p.frames++
	}
	p.lastAt, p.hasLast = capturedAt, true
	return p.frames >= 2 && capturedAt.Sub(p.since) >= confirm
}
