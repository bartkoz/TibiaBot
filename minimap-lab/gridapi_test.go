package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func getGrid(t testing.TB, s *server, query string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	req := httptest.NewRequest("GET", "http://127.0.0.1:8095/api/grid?"+query, nil)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	return w, w.Body.Bytes()
}

func TestGridWindowHasOneBytePerTile(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	s.blocks = NewBlockStore(time.Now)

	w, body := getGrid(t, s, "x=32800&y=32050&z=7&r=32")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, string(body))
	}
	if got, want := len(body), 65*65; got != want {
		t.Fatalf("%d bytes, want %d", got, want)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("content type %q", ct)
	}
	if origin := w.Header().Get("X-Grid-Origin"); origin != "32768,32018" {
		t.Fatalf("origin %q, want the window's top-left corner", origin)
	}
}

func TestGridDistinguishesWallFromMissingData(t *testing.T) {
	// The single chunk starts at (32768,32000); tiles left of it have no file.
	s := pathServer(t, costTile(100, map[[2]int]uint8{{0, 50}: 255}))
	s.blocks = NewBlockStore(time.Now)

	_, body := getGrid(t, s, "x=32768&y=32050&z=7&r=4")
	side := 9
	idx := func(x, y int) int { return (y-(32050-4))*side + (x - (32768 - 4)) }

	if body[idx(32767, 32050)]&gridMissing == 0 {
		t.Fatal("a tile with no chunk on disk is not flagged as missing data")
	}
	if body[idx(32768, 32050)]&gridMissing != 0 {
		t.Fatal("a tile from a decoded chunk is flagged as missing data")
	}
	if body[idx(32768, 32050)]&gridBlocked == 0 {
		t.Fatal("a wall from map data is not flagged as blocked")
	}
}

func TestGridShowsLearnedBlocks(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	s.blocks = NewBlockStore(time.Now)
	s.blocks.Observe(Observation{From: Position{32800, 32051, 7}, To: Position{32800, 32050, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})

	_, body := getGrid(t, s, "x=32800&y=32050&z=7&r=4")
	centre := body[4*9+4]
	if centre&gridTemp == 0 {
		t.Fatalf("centre byte %08b does not carry the temporary-block bit", centre)
	}
	if centre&gridBlocked != 0 {
		t.Fatal("a learned block must not be reported as map terrain")
	}
}

func TestGridRefusesAnOutOfRangeRadius(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	s.blocks = NewBlockStore(time.Now)
	for _, q := range []string{"x=32800&y=32050&z=7&r=999", "x=32800&y=32050&z=7&r=0"} {
		if w, _ := getGrid(t, s, q); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", q, w.Code)
		}
	}
}

func TestGridDoesNotEvictThePlannerCache(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	s.blocks = NewBlockStore(time.Now)
	decodePath(t, postPath(t, s, `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32800,"y":32054,"z":7}}`))
	planner := s.costCache
	if planner == nil {
		t.Fatal("the route query left no planner cache to protect")
	}
	getGrid(t, s, "x=32800&y=32050&z=7&r=32")
	if s.costCache != planner {
		t.Fatal("the preview replaced the planner's cached floor; the two would evict each other every reading")
	}
}
