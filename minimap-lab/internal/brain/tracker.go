package brain

import (
	"math"
	"time"

	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
)

const (
	// maxMisses is how many consecutive failed matches an anchor survives. One
	// dropped reading is not evidence that the character teleported; three in
	// a row mean the anchor is no longer describing where it is.
	maxMisses = 3
	// maxAnchorAge is how long a locally confirmed anchor stays usable. Past
	// it the character could be anywhere, and a local search would be looking
	// in the wrong place with great confidence.
	maxAnchorAge = 30 * time.Second
	// minRadius and maxRadius bound the local search window. The floor keeps
	// the window useful when the anchor is fresh; the ceiling stops a stale
	// anchor from turning a local search into a slower global one.
	minRadius = 5
	maxRadius = 64
	// readingWindow and maxReadings bound the telemetry ring used for the
	// cadence figures shown in the panel.
	readingWindow = 3 * time.Second
	maxReadings   = 40
	// staleReading is how long after the last reading the measured rate stops
	// meaning anything: a loop that has stopped must report zero, not the rate
	// it had while it was running.
	staleReading = 600 * time.Millisecond
)

// Hint is the local search window the next match should use.
type Hint struct {
	Near   mapdata.Position
	Radius int
}

type Stats struct {
	Hz          float64
	Success     float64
	AgeMS       int
	HasAge      bool
	RoundTripMS float64
	MatchMS     float64
	HasReadings bool
}

type reading struct {
	at        time.Time
	found     bool
	roundTrip time.Duration
	matchMS   float64
}

type anchor struct {
	position mapdata.Position
	zoom     int
	at       time.Time
}

// Tracker remembers where the character was last seen so the next match can
// look in a small window instead of scanning a whole floor.
type Tracker struct {
	anchor   *anchor
	misses   int
	readings []reading
	// seeded marks an anchor that came from a global search. Only such an
	// anchor may outlive maxAnchorAge: a global acquisition is often already
	// seconds old when it lands, and refusing it would leave the tracker with
	// nothing to confirm locally.
	seeded bool
}

func NewTracker() *Tracker { return &Tracker{} }

func (t *Tracker) Reset() {
	t.anchor, t.misses, t.readings, t.seeded = nil, 0, nil, false
}

// Anchor is the last confirmed position, for callers that need to know where
// the character was rather than where to search next.
func (t *Tracker) Anchor() (mapdata.Position, time.Time, bool) {
	if t.anchor == nil {
		return mapdata.Position{}, time.Time{}, false
	}
	return t.anchor.position, t.anchor.at, true
}

func (t *Tracker) Misses() int { return t.misses }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Hint answers where and how widely the next match should look, or reports
// that no local search is justified and the caller must fall back to a global
// one.
func (t *Tracker) Hint(now time.Time, floor, zoom, speed int) (Hint, bool) {
	if t.anchor == nil || t.misses >= maxMisses {
		return Hint{}, false
	}
	// One floor apart is still worth trying: a transition may have happened
	// between two readings. Two apart is a different place entirely.
	if abs(t.anchor.position.Z-floor) > 1 || t.anchor.zoom != zoom {
		return Hint{}, false
	}
	age := now.Sub(t.anchor.at)
	if age < 0 {
		age = 0
	}
	if age > maxAnchorAge && !t.seeded {
		return Hint{}, false
	}
	// The character kept walking while the match ran, so the window has to
	// cover everywhere it could have reached since the frame was captured.
	// Each miss widens it further, because a miss usually means it walked out
	// of the window we were looking in.
	reach := int(math.Ceil(age.Seconds() * float64(speed)))
	radius := reach + 2 + t.misses*5
	if radius < minRadius {
		radius = minRadius
	}
	if radius > maxRadius {
		radius = maxRadius
	}
	return Hint{Near: t.anchor.position, Radius: radius}, true
}

// Observe folds one match result into the tracker. capturedAt is when the
// frame was cut, completedAt when the answer came back: the anchor is stamped
// with the former, because that is when the character was where the match says
// it was.
func (t *Tracker) Observe(r locate.Result, capturedAt, completedAt time.Time, roundTrip time.Duration) {
	// A global result describes a search over the whole floor, so the local
	// cadence figures collected before it no longer describe the same thing.
	if r.Mode == "global" {
		t.readings = nil
	}
	if r.Mode == "local" {
		t.readings = append(t.readings, reading{at: completedAt, found: r.Found, roundTrip: roundTrip, matchMS: r.MatchMS})
	}
	kept := t.readings[:0]
	for _, rd := range t.readings {
		if completedAt.Sub(rd.at) <= readingWindow {
			kept = append(kept, rd)
		}
	}
	t.readings = kept
	if len(t.readings) > maxReadings {
		t.readings = t.readings[len(t.readings)-maxReadings:]
	}
	if r.Found && r.Position != nil {
		t.anchor = &anchor{position: *r.Position, zoom: r.Zoom, at: capturedAt}
		t.misses, t.seeded = 0, r.Mode == "global"
		return
	}
	t.misses++
	// A global miss says the character is nowhere on the floor we believed in,
	// which makes the anchor wrong rather than merely stale.
	if r.Mode == "global" {
		t.anchor = nil
	}
}

func (t *Tracker) Stats(now time.Time) Stats {
	rows := make([]reading, 0, len(t.readings))
	for _, rd := range t.readings {
		if now.Sub(rd.at) <= readingWindow {
			rows = append(rows, rd)
		}
	}
	s := Stats{HasReadings: len(rows) > 0}
	if t.anchor != nil {
		age := now.Sub(t.anchor.at)
		if age < 0 {
			age = 0
		}
		s.AgeMS, s.HasAge = int(age.Milliseconds()), true
	}
	if len(rows) == 0 {
		return s
	}
	latest := rows[len(rows)-1]
	s.RoundTripMS = float64(latest.roundTrip.Microseconds()) / 1000
	s.MatchMS = latest.matchMS
	found := 0
	for _, rd := range rows {
		if rd.found {
			found++
		}
	}
	s.Success = float64(found) / float64(len(rows))
	if len(rows) > 1 {
		duration := latest.at.Sub(rows[0].at)
		if duration > 0 && now.Sub(latest.at) < staleReading {
			s.Hz = float64(len(rows)-1) * 1000 / float64(duration.Milliseconds())
		}
	}
	return s
}
