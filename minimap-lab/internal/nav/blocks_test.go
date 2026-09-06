package nav

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minimap-lab/internal/mapdata"
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
// required to supply before anything is learned. MovedSince is true, the
// ordinary case of a bot that walks between encounters; the tests about an
// obstacle that never moved set it to false explicitly.
func bump(from, to mapdata.Position) Observation {
	return Observation{From: from, To: to, Outcome: "no_motion",
		StillFrames: 3, LastFrameAgeMS: 140, MovedSince: true}
}

func TestFirstBumpMakesTemporaryBlock(t *testing.T) {
	s, _ := newTestStore()
	d := s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
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
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindNone {
		t.Fatalf("tile kind %v after TTL, want KindNone", got)
	}
}

func TestRetryWithinTTLIsOneEpisode(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(2 * time.Second)
	d := s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
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
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)
	d := s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
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
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(48 * time.Hour)
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindPerm {
		t.Fatalf("tile kind %v after two days, want KindPerm", got)
	}
}

func TestForgottenRecordStartsOver(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(25 * time.Hour) // past forgetAfter, so the episode count is gone
	d := s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	if d.Result != "temp" {
		t.Fatalf("result %q, want temp - a forgotten record cannot promote on its first bump", d.Result)
	}
}

func TestDiagonalBumpBlocksEdgeNotTile(t *testing.T) {
	s, _ := newTestStore()
	d := s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 101, Y: 99, Z: 7}))
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
		s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 101, Y: 99, Z: 7}))
		c.advance(21 * time.Second)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(101, 99); got != KindNone {
		t.Fatalf("tile kind %v; a diagonal must never produce a tile block", got)
	}
}

func TestEnteredClearsPermanentBlock(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	d := s.Observe(Observation{From: mapdata.Position{X: 100, Y: 100, Z: 7}, To: mapdata.Position{X: 100, Y: 99, Z: 7},
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
	if again := s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7})); again.Result != "temp" {
		t.Fatalf("result %q after clearing, want temp", again.Result)
	}
}

func TestUnqualifiedObservationsAreIgnored(t *testing.T) {
	cases := []struct {
		name string
		obs  Observation
	}{
		{"nie sąsiednie kratki", Observation{From: mapdata.Position{X: 100, Y: 100, Z: 7}, To: mapdata.Position{X: 100, Y: 97, Z: 7},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}},
		{"ta sama kratka", Observation{From: mapdata.Position{X: 100, Y: 100, Z: 7}, To: mapdata.Position{X: 100, Y: 100, Z: 7},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}},
		{"różne piętra", Observation{From: mapdata.Position{X: 100, Y: 100, Z: 7}, To: mapdata.Position{X: 100, Y: 99, Z: 6},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}},
		{"za mało klatek", Observation{From: mapdata.Position{X: 100, Y: 100, Z: 7}, To: mapdata.Position{X: 100, Y: 99, Z: 7},
			Outcome: "no_motion", StillFrames: 2, LastFrameAgeMS: 100}},
		{"za stara klatka", Observation{From: mapdata.Position{X: 100, Y: 100, Z: 7}, To: mapdata.Position{X: 100, Y: 99, Z: 7},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 400}},
		{"nieznany wynik", Observation{From: mapdata.Position{X: 100, Y: 100, Z: 7}, To: mapdata.Position{X: 100, Y: 99, Z: 7},
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
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	s.Observe(bump(mapdata.Position{X: 500, Y: 500, Z: 7}, mapdata.Position{X: 500, Y: 499, Z: 7}))
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 8}, mapdata.Position{X: 100, Y: 99, Z: 8}))
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
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	afterBump := s.Revision()
	if afterBump == start {
		t.Fatal("revision did not move after a learned block")
	}
	s.Observe(Observation{From: mapdata.Position{X: 100, Y: 100, Z: 7}, To: mapdata.Position{X: 100, Y: 97, Z: 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})
	if s.Revision() != afterBump {
		t.Fatal("revision moved on an ignored observation; the panel would redraw for nothing")
	}
}

func TestPermanentBlocksSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocks.json")

	s, c := newTestStore()
	s.SetPath(path)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	fresh, _ := newTestStore()
	fresh.SetPath(path)
	if err := fresh.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := fresh.Snapshot(image.Rect(90, 90, 110, 110), 7).Tile(100, 99); got != KindPerm {
		t.Fatalf("tile kind %v after reload, want KindPerm", got)
	}
}

func TestTemporaryBlocksAreNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocks.json")
	s, _ := newTestStore()
	s.SetPath(path)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	fresh, _ := newTestStore()
	fresh.SetPath(path)
	if err := fresh.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := fresh.Snapshot(image.Rect(90, 90, 110, 110), 7).Tile(100, 99); got != KindNone {
		t.Fatalf("tile kind %v; a temporary block is a guess and must not reach the disk", got)
	}
}

func TestClearedTileLeavesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocks.json")
	s, c := newTestStore()
	s.SetPath(path)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	s.Save()
	s.Clear(mapdata.Position{X: 100, Y: 99, Z: 7})
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	fresh, _ := newTestStore()
	fresh.SetPath(path)
	fresh.Load()
	if got := fresh.Snapshot(image.Rect(90, 90, 110, 110), 7).Tile(100, 99); got != KindNone {
		t.Fatalf("tile kind %v; a revoked block must disappear from disk too", got)
	}
}

func TestUnknownFileVersionIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocks.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"tiles":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := newTestStore()
	s.SetPath(path)
	err := s.Load()
	if err == nil {
		t.Fatal("a file from a future version was accepted; the next Save would overwrite it")
	}
	if !strings.Contains(err.Error(), "wersj") {
		t.Fatalf("error %q does not name the version problem", err)
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	s, _ := newTestStore()
	s.SetPath(filepath.Join(t.TempDir(), "blocks.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("load of a missing file: %v - a first run has no file yet", err)
	}
}

func TestSaveLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	s, c := newTestStore()
	s.SetPath(filepath.Join(dir, "blocks.json"))
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d files in the directory, want exactly blocks.json", len(entries))
	}
}

func TestFlushWritesOnlyAfterAPersistentChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocks.json")
	s, _ := newTestStore()
	s.SetPath(path)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7})) // temporary only
	s.Flush()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a temporary block rewrote the file; only permanent changes belong on disk")
	}
}

func TestStandingObstacleNeverPromotes(t *testing.T) {
	// A player idling in a doorway fails the bot, waits out the TTL and fails
	// it again - with no walking in between. Time alone must not turn that into
	// a permanent wall, or one AFK character permanently deletes a corridor.
	s, c := newTestStore()
	for i := 0; i < 5; i++ {
		obs := bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7})
		obs.MovedSince = false
		if d := s.Observe(obs); d.Result != "temp" {
			t.Fatalf("attempt %d: result %q (%s), want temp", i, d.Result, d.Reason)
		}
		c.advance(61 * time.Second)
	}
}

func TestEpisodeAfterWalkingPromotes(t *testing.T) {
	// The bot bumped, went around, came back and bumped again: two separate
	// encounters with the same tile, which is what terrain looks like.
	s, c := newTestStore()
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)
	if d := s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7})); d.Result != "promoted" {
		t.Fatalf("result %q (%s), want promoted", d.Result, d.Reason)
	}
}

func TestRepeatWithinTTLExtendsIt(t *testing.T) {
	// Bumping again at 58s must push the deadline out. Without that, a bump at
	// 61s counts as a second episode even though the obstacle never left.
	s, c := newTestStore()
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(58 * time.Second)
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(5 * time.Second) // 63s from the first bump, but only 5s from the second
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindTemp {
		t.Fatalf("tile kind %v; the repeat did not extend the block", got)
	}
}

func TestEnteredClearsADiagonalEdge(t *testing.T) {
	s, _ := newTestStore()
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 101, Y: 99, Z: 7}))
	if !s.Snapshot(image.Rect(90, 90, 110, 110), 7).Edge(100, 100, 101, 99) {
		t.Fatal("the diagonal bump did not block its edge")
	}
	d := s.Observe(Observation{From: mapdata.Position{X: 100, Y: 100, Z: 7}, To: mapdata.Position{X: 101, Y: 99, Z: 7},
		Outcome: "entered", StillFrames: 1, LastFrameAgeMS: 50})
	if d.Result != "cleared" {
		t.Fatalf("result %q (%s), want cleared", d.Result, d.Reason)
	}
	if s.Snapshot(image.Rect(90, 90, 110, 110), 7).Edge(100, 100, 101, 99) {
		t.Fatal("walking the diagonal did not lift the edge block")
	}
}

func TestRenewedBlockStaysActive(t *testing.T) {
	// The exact sequence the feature exists for: the executor's own block and
	// the server's TTL are both armed by the first failure, so the retry lands
	// just past the deadline with nothing having succeeded in between. The
	// renewed block must still steer routes - a renewal that leaves the tile
	// invisible neuters it for the rest of the run, because every later bump
	// then only pushes the deadline out again.
	s, c := newTestStore()
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)

	stale := bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7})
	stale.MovedSince = false
	if d := s.Observe(stale); d.Result != "temp" {
		t.Fatalf("result %q (%s), want temp", d.Result, d.Reason)
	}

	if got := s.Snapshot(image.Rect(90, 90, 110, 110), 7).Tile(100, 99); got != KindTemp {
		t.Fatalf("tile kind %v after renewal, want KindTemp - the renewed block steers nothing", got)
	}
	if list := s.List(image.Rect(90, 90, 110, 110), 7); len(list) != 1 {
		t.Fatalf("List returned %d entries after renewal, want 1", len(list))
	}
}

func TestRenewedBlockCanStillPromote(t *testing.T) {
	// After a renewal, an episode that does carry proof of walking must still
	// be able to promote the tile.
	s, c := newTestStore()
	s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7}))
	c.advance(61 * time.Second)
	stale := bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7})
	stale.MovedSince = false
	s.Observe(stale)
	c.advance(61 * time.Second)

	if d := s.Observe(bump(mapdata.Position{X: 100, Y: 100, Z: 7}, mapdata.Position{X: 100, Y: 99, Z: 7})); d.Result != "promoted" {
		t.Fatalf("result %q (%s), want promoted", d.Result, d.Reason)
	}
}
