package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newBlocksServer() *server {
	return &server{gate: make(chan struct{}, 1), blocks: NewBlockStore(time.Now)}
}

func callBlocks(t testing.TB, s *server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "http://127.0.0.1:8095"+path, nil)
	} else {
		r = httptest.NewRequest(method, "http://127.0.0.1:8095"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}

func decodeInto(t testing.TB, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("%v (%s)", err, w.Body.String())
	}
}

func TestObserveEndpointLearnsABlock(t *testing.T) {
	s := newBlocksServer()
	var d Decision
	decodeInto(t, callBlocks(t, s, "POST", "/api/blocks/observe",
		`{"from":{"x":100,"y":100,"z":7},"to":{"x":100,"y":99,"z":7},"outcome":"no_motion","still_frames":3,"last_frame_age_ms":120}`), &d)
	if d.Result != "temp" {
		t.Fatalf("result %q (%s), want temp", d.Result, d.Reason)
	}
}

func TestObserveEndpointExplainsARefusal(t *testing.T) {
	s := newBlocksServer()
	var d Decision
	decodeInto(t, callBlocks(t, s, "POST", "/api/blocks/observe",
		`{"from":{"x":100,"y":100,"z":7},"to":{"x":100,"y":99,"z":7},"outcome":"no_motion","still_frames":1,"last_frame_age_ms":120}`), &d)
	if d.Result != "ignored" {
		t.Fatalf("result %q, want ignored", d.Result)
	}
	if d.Reason == "" {
		t.Fatal("a refusal with no reason is indistinguishable from a lost request")
	}
}

func TestObserveEndpointRefusesMalformedBody(t *testing.T) {
	s := newBlocksServer()
	if w := callBlocks(t, s, "POST", "/api/blocks/observe", `{"from":`); w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestListEndpointReportsLearnedBlocks(t *testing.T) {
	s := newBlocksServer()
	s.blocks.Observe(Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})

	var list []BlockInfo
	decodeInto(t, callBlocks(t, s, "GET", "/api/blocks?x=100&y=100&z=7&r=10", ""), &list)
	if len(list) != 1 || list[0].X != 100 || list[0].Y != 99 || list[0].Kind != "temp" {
		t.Fatalf("list %+v, want one temporary block at (100,99)", list)
	}
	if list[0].ExpiresInMS <= 0 {
		t.Fatalf("expires_in_ms %d; the panel needs it to show a countdown", list[0].ExpiresInMS)
	}
}

func TestListEndpointRefusesABadWindow(t *testing.T) {
	s := newBlocksServer()
	for _, q := range []string{"x=100&y=100&z=7&r=999", "x=100&y=100&z=99&r=4", "x=-1&y=100&z=7&r=4", "x=100&y=100&z=7"} {
		if w := callBlocks(t, s, "GET", "/api/blocks?"+q, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", q, w.Code)
		}
	}
}

func TestDeleteEndpointRemovesABlock(t *testing.T) {
	s := newBlocksServer()
	s.blocks.Observe(Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})

	var res map[string]bool
	decodeInto(t, callBlocks(t, s, "DELETE", "/api/blocks", `{"x":100,"y":99,"z":7}`), &res)
	if !res["cleared"] {
		t.Fatal("delete reported nothing was cleared")
	}
	if k := s.blocks.Snapshot(rectAround(100, 99, 10), 7).Tile(100, 99); k != KindNone {
		t.Fatalf("tile kind %v after delete, want KindNone", k)
	}
}

func TestBlocksRoutesAnswer503WithoutAStore(t *testing.T) {
	s := &server{gate: make(chan struct{}, 1)}
	if w := callBlocks(t, s, "GET", "/api/blocks?x=1&y=1&z=7&r=4", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 - a server without a store must say so, not pretend the map is empty", w.Code)
	}
}

func TestObserveIgnoredWhenTheCharacterStandsOnImpassableGround(t *testing.T) {
	// The character cannot be standing on water or inside a wall. A report
	// claiming it is means the locator matched the wrong place, and the tile it
	// "failed to enter" is somewhere else entirely - learning from it writes a
	// permanent block into open ground, which is exactly how three of them
	// appeared in a real run.
	s := pathServer(t, costTile(100, map[[2]int]uint8{{50, 51}: blockedCost}))
	s.blocks = NewBlockStore(time.Now)

	var d Decision
	decodeInto(t, callBlocks(t, s, "POST", "/api/blocks/observe",
		`{"from":{"x":32818,"y":32051,"z":7},"to":{"x":32818,"y":32050,"z":7},"outcome":"no_motion","still_frames":3,"last_frame_age_ms":120,"moved_since":true}`), &d)

	if d.Result != "ignored" {
		t.Fatalf("result %q (%s), want ignored", d.Result, d.Reason)
	}
	if !strings.Contains(strings.ToLower(d.Reason), "pozycja postaci") {
		t.Fatalf("reason %q does not name the unreliable position", d.Reason)
	}
	if k := s.blocks.Snapshot(rectAround(32818, 32050, 4), 7).Tile(32818, 32050); k != KindNone {
		t.Fatalf("tile kind %v; nothing may be learned from an unreliable position", k)
	}
}

func TestObserveStillWorksFromSolidGround(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	s.blocks = NewBlockStore(time.Now)

	var d Decision
	decodeInto(t, callBlocks(t, s, "POST", "/api/blocks/observe",
		`{"from":{"x":32818,"y":32051,"z":7},"to":{"x":32818,"y":32050,"z":7},"outcome":"no_motion","still_frames":3,"last_frame_age_ms":120,"moved_since":true}`), &d)
	if d.Result != "temp" {
		t.Fatalf("result %q (%s), want temp", d.Result, d.Reason)
	}
}

func TestEnteredIsAcceptedEvenWithoutMapData(t *testing.T) {
	// Revocation must never be gated on the map: a report that the character
	// walked onto a tile can only ever remove a block, so the worst a bogus one
	// does is forget a lesson - while refusing them would let a wrong permanent
	// block survive precisely where the map data is poor.
	// The reporting position is impassable in the map data - the same thing
	// that makes a no_motion report untrustworthy - so this only passes if
	// revocations are genuinely exempt from that check.
	s := pathServer(t, costTile(100, map[[2]int]uint8{{50, 51}: blockedCost}))
	s.blocks = NewBlockStore(time.Now)
	s.blocks.Observe(Observation{From: Position{32818, 32051, 7}, To: Position{32818, 32050, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100, MovedSince: true})

	var d Decision
	decodeInto(t, callBlocks(t, s, "POST", "/api/blocks/observe",
		`{"from":{"x":32818,"y":32051,"z":7},"to":{"x":32818,"y":32050,"z":7},"outcome":"entered","still_frames":1,"last_frame_age_ms":50}`), &d)
	if d.Result != "cleared" {
		t.Fatalf("result %q (%s), want cleared", d.Result, d.Reason)
	}
}

func TestObserveWorksWhereTheMapHasNoData(t *testing.T) {
	// Missing map data must not gate learning. The pack is thin in places, and
	// refusing to learn there would leave the bot helpless exactly where it has
	// the least information to begin with.
	s := pathServer(t, costTile(100, nil))
	s.blocks = NewBlockStore(time.Now)

	var d Decision
	decodeInto(t, callBlocks(t, s, "POST", "/api/blocks/observe",
		`{"from":{"x":33500,"y":32051,"z":7},"to":{"x":33500,"y":32050,"z":7},"outcome":"no_motion","still_frames":3,"last_frame_age_ms":120,"moved_since":true}`), &d)
	if d.Result != "temp" {
		t.Fatalf("result %q (%s), want temp", d.Result, d.Reason)
	}
}
