package main

import (
	"context"
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pathServer(t testing.TB, tiles ...*image.Paletted) *server {
	t.Helper()
	dir := t.TempDir()
	for i, im := range tiles {
		writeCostTile(t, dir, 32768+256*i, 32000, 7, im)
	}
	return &server{dir: dir, gate: make(chan struct{}, 1)}
}

func postPath(t testing.TB, s *server, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "http://127.0.0.1:8095/api/path", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}

func decodePath(t testing.TB, w *httptest.ResponseRecorder) PathResult {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var r PathResult
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPathAPIReturnsARoute(t *testing.T) {
	s := pathServer(t, costTile(100, nil))

	w := postPath(t, s, `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32806,"y":32050,"z":7}}`)

	r := decodePath(t, w)
	if !r.Found || r.Tiles != 6 {
		t.Fatalf("got %+v", r)
	}
	if r.Steps[0] != [2]int{32800, 32050} {
		t.Errorf("route must start at the player tile, got %v", r.Steps[0])
	}
}

func TestPathAPIRefusesRoutesBetweenFloors(t *testing.T) {
	s := pathServer(t, costTile(100, nil))

	w := postPath(t, s, `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32806,"y":32050,"z":8}}`)

	r := decodePath(t, w)
	if r.Found || r.Status != "different_floor" || r.Reason == "" {
		t.Fatalf("crossing floors is an action, not a walk: %+v", r)
	}
}

func TestPathAPIRejectsMalformedInput(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	for name, body := range map[string]string{
		"not json":               `{`,
		"empty object":           `{}`,
		"json null":              `null`,
		"missing to":             `{"from":{"x":1,"y":1,"z":7}}`,
		"null from":              `{"from":null,"to":{"x":2,"y":2,"z":7}}`,
		"missing coordinate":     `{"from":{"x":1,"z":7},"to":{"x":2,"y":2,"z":7}}`,
		"trailing document":      `{"from":{"x":1,"y":1,"z":7},"to":{"x":2,"y":2,"z":7}}{"x":1}`,
		"trailing bracket":       `{"from":{"x":1,"y":1,"z":7},"to":{"x":2,"y":2,"z":7}}]`,
		"trailing brace":         `{"from":{"x":1,"y":1,"z":7},"to":{"x":2,"y":2,"z":7}}}`,
		"padded second document": `{"from":{"x":1,"y":1,"z":7},"to":{"x":2,"y":2,"z":7}}                    {"x":1}`,
		"floor above range":      `{"from":{"x":1,"y":1,"z":16},"to":{"x":2,"y":2,"z":16}}`,
		"negative coordinate":    `{"from":{"x":-1,"y":1,"z":7},"to":{"x":2,"y":2,"z":7}}`,
		"coordinate above range": `{"from":{"x":70000,"y":1,"z":7},"to":{"x":2,"y":2,"z":7}}`,
		"margin above range":     `{"from":{"x":1,"y":1,"z":7},"to":{"x":2,"y":2,"z":7},"margin":300}`,
		"negative margin":        `{"from":{"x":1,"y":1,"z":7},"to":{"x":2,"y":2,"z":7},"margin":-1}`,
	} {
		t.Run(name, func(t *testing.T) {
			if w := postPath(t, s, body); w.Code != http.StatusBadRequest {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestPathAPIReportsMissingMapData(t *testing.T) {
	s := pathServer(t)

	w := postPath(t, s, `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32806,"y":32050,"z":7}}`)

	r := decodePath(t, w)
	if r.Found || r.Status != "blocked_start" {
		t.Fatalf("without cost tiles nothing is walkable: %+v", r)
	}
}

func TestPathAPIDoesNotBlockTracking(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	// Holding the locate gate must not delay or fail a route query.
	s.gate <- struct{}{}
	defer func() { <-s.gate }()

	w := postPath(t, s, `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32806,"y":32050,"z":7}}`)

	if r := decodePath(t, w); !r.Found {
		t.Fatalf("route query must not wait on the locate gate: %+v", r)
	}
}

func TestPathAPIWidensTheSearchAreaWithMargin(t *testing.T) {
	// A wall spans the direct line; going around it leaves the tight
	// bounding box between the two points.
	edits := map[[2]int]uint8{}
	for y := 40; y <= 60; y++ {
		edits[[2]int{35, y}] = blockedCost
	}
	s := pathServer(t, costTile(100, edits))
	body := `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32806,"y":32050,"z":7},"margin":%d}`

	tight := decodePath(t, postPath(t, s, strings.Replace(body, "%d", "1", 1)))
	if tight.Found || tight.Status != "no_route" {
		t.Fatalf("a one-tile margin walls the search in: %+v", tight)
	}

	wide := decodePath(t, postPath(t, s, strings.Replace(body, "%d", "32", 1)))
	if !wide.Found {
		t.Fatalf("a wide margin should find the way around: %+v", wide)
	}
}

func TestPathAPIRefusesAnAreaTooLargeToLoad(t *testing.T) {
	s := pathServer(t, costTile(100, nil))

	// Valid coordinates that span the whole map would allocate gigabytes.
	w := postPath(t, s, `{"from":{"x":0,"y":0,"z":7},"to":{"x":65535,"y":65535,"z":7}}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestPathAPIStopsWorkForAnAbandonedRequest(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "http://127.0.0.1:8095/api/path",
		strings.NewReader(`{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32806,"y":32050,"z":7}}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.routes().ServeHTTP(w, r)

	if w.Code == http.StatusOK {
		var result PathResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err == nil && result.Found {
			t.Fatal("an abandoned request should not produce a route")
		}
	}
	if s.costCache != nil {
		t.Error("an abandoned request should not populate the cost cache")
	}
}

func TestPathAvoidsLearnedBlock(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	s.blocks = NewBlockStore(time.Now)

	body := `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32800,"y":32054,"z":7},"margin":8}`
	before := decodePath(t, postPath(t, s, body))
	if !before.Found {
		t.Fatalf("baseline route not found: %s", before.Reason)
	}

	// Learn the tile straight ahead twice, so it becomes permanent and the
	// route has to go around it.
	obs := Observation{From: Position{32800, 32051, 7}, To: Position{32800, 32052, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}
	s.blocks.Observe(obs)
	s.blocks.tiles[tileKey{32800, 32052, 7}].Kind = KindPerm

	after := decodePath(t, postPath(t, s, body))
	if !after.Found {
		t.Fatalf("route not found after learning a block: %s", after.Reason)
	}
	for _, st := range after.Steps {
		if st == [2]int{32800, 32052} {
			t.Fatal("route still runs through the learned block")
		}
	}
	if after.OverlayRevision == 0 {
		t.Fatal("the reply carries no overlay revision, so the panel cannot tell a stale route from a fresh one")
	}
}

func TestStandingOnABlockClearsIt(t *testing.T) {
	s := pathServer(t, costTile(100, nil))
	s.blocks = NewBlockStore(time.Now)
	s.blocks.Observe(Observation{From: Position{32800, 32051, 7}, To: Position{32800, 32050, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})

	decodePath(t, postPath(t, s, `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32800,"y":32054,"z":7}}`))

	if k := s.blocks.Snapshot(image.Rect(32790, 32040, 32810, 32060), 7).Tile(32800, 32050); k != KindNone {
		t.Fatalf("tile kind %v; the character is standing there, which beats any learned guess", k)
	}
}

func TestPathWorksWithoutABlockStore(t *testing.T) {
	// Every existing test builds a server with no store; that must keep working
	// rather than panicking on a nil map.
	s := pathServer(t, costTile(100, nil))
	r := decodePath(t, postPath(t, s, `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32800,"y":32054,"z":7}}`))
	if !r.Found {
		t.Fatalf("route not found: %s", r.Reason)
	}
}

func TestClearingAPermanentBlockReachesTheDisk(t *testing.T) {
	// Walking onto a permanently blocked tile revokes it. If that never reaches
	// blocks.json, a restart resurrects the block and cuts the route again.
	s := pathServer(t, costTile(100, nil))
	path := filepath.Join(t.TempDir(), "blocks.json")
	s.blocks = NewBlockStore(time.Now)
	s.blocks.SetPath(path)
	s.blocks.Observe(Observation{From: Position{32800, 32051, 7}, To: Position{32800, 32050, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100, MovedSince: true})
	s.blocks.tiles[tileKey{32800, 32050, 7}].Kind = KindPerm
	if err := s.blocks.Save(); err != nil {
		t.Fatal(err)
	}

	decodePath(t, postPath(t, s, `{"from":{"x":32800,"y":32050,"z":7},"to":{"x":32800,"y":32054,"z":7}}`))

	fresh := NewBlockStore(time.Now)
	fresh.SetPath(path)
	if err := fresh.Load(); err != nil {
		t.Fatal(err)
	}
	if k := fresh.Snapshot(image.Rect(32790, 32040, 32810, 32060), 7).Tile(32800, 32050); k != KindNone {
		t.Fatalf("tile kind %v after reload; the revocation never reached the disk", k)
	}
}
