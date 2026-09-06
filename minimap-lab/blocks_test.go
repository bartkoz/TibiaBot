package main

import (
	"image"
	"testing"
	"time"
)

// clock is a hand-cranked time source: TTL, promotion and forgetting are all
// time-driven, and a test that actually waited 60 seconds would be useless.
type clock struct{ at time.Time }

func (c *clock) now() time.Time          { return c.at }
func (c *clock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newTestStore() (*BlockStore, *clock) {
	c := &clock{at: time.Date(2026, 9, 6, 20, 0, 0, 0, time.UTC)}
	return NewBlockStore(c.now), c
}

// bump is a qualified straight step that failed: the evidence the panel is
// required to supply before anything is learned.
func bump(from, to Position) Observation {
	return Observation{From: from, To: to, Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 140}
}

func TestFirstBumpMakesTemporaryBlock(t *testing.T) {
	s, _ := newTestStore()
	d := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if d.Result != "temp" {
		t.Fatalf("result %q (%s), want temp", d.Result, d.Reason)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindTemp {
		t.Fatalf("tile kind %v, want KindTemp", got)
	}
}

func TestTemporaryBlockExpires(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindNone {
		t.Fatalf("tile kind %v after TTL, want KindNone", got)
	}
}

func TestRetryWithinTTLIsOneEpisode(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(2 * time.Second)
	d := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if d.Result != "temp" {
		t.Fatalf("result %q, want temp - a repeat inside the TTL is the same episode", d.Result)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindTemp {
		t.Fatalf("tile kind %v, want KindTemp", got)
	}
}

func TestSecondEpisodeAfterExpiryPromotes(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	d := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if d.Result != "promoted" {
		t.Fatalf("result %q (%s), want promoted", d.Result, d.Reason)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindPerm {
		t.Fatalf("tile kind %v, want KindPerm", got)
	}
}

func TestPermanentBlockDoesNotExpire(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(48 * time.Hour)
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindPerm {
		t.Fatalf("tile kind %v after two days, want KindPerm", got)
	}
}

func TestForgottenRecordStartsOver(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(25 * time.Hour) // past forgetAfter, so the episode count is gone
	d := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if d.Result != "temp" {
		t.Fatalf("result %q, want temp - a forgotten record cannot promote on its first bump", d.Result)
	}
}

func TestDiagonalBumpBlocksEdgeNotTile(t *testing.T) {
	s, _ := newTestStore()
	d := s.Observe(bump(Position{100, 100, 7}, Position{101, 99, 7}))
	if d.Result != "temp" {
		t.Fatalf("result %q (%s), want temp", d.Result, d.Reason)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(101, 99); got != KindNone {
		t.Fatalf("diagonal bump marked the tile %v; it must only block the edge", got)
	}
	if !o.Edge(100, 100, 101, 99) {
		t.Fatal("diagonal bump did not block the edge it failed on")
	}
}

func TestDiagonalEdgeNeverPromotes(t *testing.T) {
	s, c := newTestStore()
	for i := 0; i < 5; i++ {
		s.Observe(bump(Position{100, 100, 7}, Position{101, 99, 7}))
		c.advance(21 * time.Second)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(101, 99); got != KindNone {
		t.Fatalf("tile kind %v; a diagonal must never produce a tile block", got)
	}
}

func TestEnteredClearsPermanentBlock(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	d := s.Observe(Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
		Outcome: "entered", StillFrames: 1, LastFrameAgeMS: 50})
	if d.Result != "cleared" {
		t.Fatalf("result %q (%s), want cleared", d.Result, d.Reason)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindNone {
		t.Fatalf("tile kind %v after entering it, want KindNone", got)
	}
	// The episode count must go too: proof of passage invalidates the whole
	// hypothesis, not just the current block. Otherwise the next single bump
	// would promote straight to permanent.
	c.advance(time.Second)
	if again := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7})); again.Result != "temp" {
		t.Fatalf("result %q after clearing, want temp", again.Result)
	}
}

func TestUnqualifiedObservationsAreIgnored(t *testing.T) {
	cases := []struct {
		name string
		obs  Observation
	}{
		{"nie sąsiednie kratki", Observation{From: Position{100, 100, 7}, To: Position{100, 97, 7},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}},
		{"ta sama kratka", Observation{From: Position{100, 100, 7}, To: Position{100, 100, 7},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}},
		{"różne piętra", Observation{From: Position{100, 100, 7}, To: Position{100, 99, 6},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}},
		{"za mało klatek", Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
			Outcome: "no_motion", StillFrames: 2, LastFrameAgeMS: 100}},
		{"za stara klatka", Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 400}},
		{"nieznany wynik", Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
			Outcome: "transport_error", StillFrames: 3, LastFrameAgeMS: 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestStore()
			d := s.Observe(tc.obs)
			if d.Result != "ignored" {
				t.Fatalf("result %q, want ignored", d.Result)
			}
			if d.Reason == "" {
				t.Fatal("an ignored observation must say why - otherwise the panel cannot tell it from a lost request")
			}
			if got := s.Snapshot(image.Rect(90, 90, 110, 110), 7).Tile(100, 99); got != KindNone {
				t.Fatalf("tile kind %v; nothing may be learned from an unqualified observation", got)
			}
		})
	}
}

func TestSnapshotIsLimitedToAreaAndFloor(t *testing.T) {
	s, _ := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	s.Observe(bump(Position{500, 500, 7}, Position{500, 499, 7}))
	s.Observe(bump(Position{100, 100, 8}, Position{100, 99, 8}))
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if o.Tile(100, 99) != KindTemp {
		t.Fatal("tile inside the area is missing from the snapshot")
	}
	if o.Tile(500, 499) != KindNone {
		t.Fatal("tile outside the area leaked into the snapshot")
	}
}

func TestRevisionMovesOnlyOnChange(t *testing.T) {
	s, _ := newTestStore()
	start := s.Revision()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	afterBump := s.Revision()
	if afterBump == start {
		t.Fatal("revision did not move after a learned block")
	}
	s.Observe(Observation{From: Position{100, 100, 7}, To: Position{100, 97, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})
	if s.Revision() != afterBump {
		t.Fatal("revision moved on an ignored observation; the panel would redraw for nothing")
	}
}
