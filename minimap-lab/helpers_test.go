package main

import (
	"image"
	"image/draw"
	"testing"

	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/nav"
	"minimap-lab/internal/testenv"
	"time"
)

// The helpers below have twins in the packages under internal/. They are
// deliberately copied rather than shared: each one names a type from the
// package it belongs to, so a shared home would have to import locate and
// mapdata - and internal/locate's own tests could then no longer use it
// without an import cycle. Only the parts that name no domain type at all
// (path resolution, fixture loading) live in internal/testenv.

func assertPosition(t *testing.T, result locate.Result, want mapdata.Position) {
	t.Helper()
	if !result.Found || result.Position == nil || *result.Position != want {
		t.Fatalf("want %+v, got %+v (best %+v, competitor %+v)", want, result, result.Best, result.Competitor)
	}
}

// actualTrackingFixture is the saved Venore capture and the atlas it was taken
// from, with the calibration that reads it correctly.
func actualTrackingFixture(t *testing.T) (*mapdata.Atlas, image.Image, locate.Options) {
	t.Helper()
	ref := testenv.LoadFixture(t, "venore-reference.png")
	rgba := image.NewNRGBA(ref.Bounds())
	draw.Draw(rgba, rgba.Bounds(), ref, ref.Bounds().Min, draw.Src)
	c := testenv.VenoreCalibration()
	return &mapdata.Atlas{Image: rgba, Origin: image.Pt(32768, 32000), Floor: 7},
		testenv.LoadFixture(t, "venore-capture.png"),
		locate.Options{Zoom: c.Zoom, MarkerX: c.MarkerX, MarkerY: c.MarkerY,
			MaskRadius: c.MaskRadius, MinScore: c.MinScore, MinGap: c.MinGap}
}

// testClock drives a BlockStore's sense of time. The store takes its clock as
// a parameter precisely so a test can put two observations far enough apart to
// count as separate episodes without waiting a real minute.
type testClock struct{ at time.Time }

func newTestClock() *testClock {
	return &testClock{at: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) now() time.Time          { return c.at }
func (c *testClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// learnPermanentBlock walks the store through the two independent episodes
// Observe demands before promoting a tile: one bump, then a second one long
// after the temporary block lapsed and only once the character has managed to
// move in between. Going through the real rule rather than writing the state
// directly is the point - these tests are meant to break if promotion ever
// stops working.
func learnPermanentBlock(t *testing.T, clock *testClock, store *nav.BlockStore, from, to mapdata.Position) {
	t.Helper()
	obs := nav.Observation{From: from, To: to, Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}
	if d := store.Observe(obs); d.Result != "temp" {
		t.Fatalf("pierwszy epizod nie dał blokady tymczasowej: %+v", d)
	}
	// An hour is far past the temporary block's lifetime and far short of the
	// point where the record itself is forgotten.
	clock.advance(time.Hour)
	obs.MovedSince = true
	if d := store.Observe(obs); d.Result != "promoted" {
		t.Fatalf("drugi epizod nie awansował blokady na trwałą: %+v", d)
	}
}
