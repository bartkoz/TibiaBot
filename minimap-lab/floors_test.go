package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func floorFixture(t testing.TB, oldZ int, targetFloors ...int) (*server, image.Image, matchRequest) {
	t.Helper()
	dir := t.TempDir()
	ref := loadFixture(t, "venore-reference.png")
	wrong := image.NewNRGBA(ref.Bounds())
	draw.Draw(wrong, wrong.Bounds(), image.NewUniform(color.NRGBA{12, 12, 12, 255}), image.Point{}, draw.Src)
	s := &server{dir: dir, gate: make(chan struct{}, 1), cached: &Atlas{wrong, image.Pt(32768, 32000), oldZ}}
	for _, z := range targetFloors {
		savePNG(t, filepath.Join(dir, fmt.Sprintf("Minimap_Color_32768_32000_%d.png", z)), ref)
	}
	req := matchRequest{Options: Options{Zoom: 1, MarkerX: 52, MarkerY: 57, MaskRadius: 5, MinScore: .85, MinGap: .015},
		Floor: oldZ, Near: &Position{32957, 32076, oldZ}, Radius: 5, AdjacentFloors: true, FloorRadius: 8, NoPreview: true}
	return s, loadFixture(t, "venore-capture.png"), req
}

func TestAdjacentFloorTransitions(t *testing.T) {
	for _, pair := range [][2]int{{7, 6}, {7, 8}, {0, 1}, {15, 14}} {
		t.Run(fmt.Sprintf("%d_to_%d", pair[0], pair[1]), func(t *testing.T) {
			s, im, req := floorFixture(t, pair[0], pair[1])
			r, a, err := s.locateLocal(context.Background(), im, req)
			if err != nil {
				t.Fatal(err)
			}
			assertPosition(t, r, Position{32958, 32077, pair[1]})
			if !r.FloorChanged || r.Mode != "local" || r.SearchPositions > 699 || a.Floor != pair[1] {
				t.Fatalf("not a bounded floor transition: %+v", r)
			}
			for _, z := range r.SearchedFloors {
				if z < 0 || z > 15 || abs(z-pair[0]) > 1 {
					t.Fatalf("searched non-adjacent floor: %d", z)
				}
			}
			// Normal movement on the new floor reuses the small atlas immediately.
			req.Floor = pair[1]
			req.Near = r.Position
			if err = os.Remove(filepath.Join(s.dir, fmt.Sprintf("Minimap_Color_32768_32000_%d.png", pair[1]))); err != nil {
				t.Fatal(err)
			}
			r, warm, err := s.locateLocal(context.Background(), im, req)
			if err != nil {
				t.Fatal(err)
			}
			assertPosition(t, r, Position{32958, 32077, pair[1]})
			if r.FloorChanged || warm != a || !reflect.DeepEqual(r.SearchedFloors, []int{pair[1]}) {
				t.Fatalf("cache not reused: %+v", r)
			}
		})
	}
}

func TestAmbiguousFloorsDoNotGuessZ(t *testing.T) {
	s, im, req := floorFixture(t, 7, 6, 8)
	r, _, err := s.locateLocal(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Found || r.Position != nil || r.Competitor == nil || r.Best.Z == r.Competitor.Z {
		t.Fatalf("ambiguous Z accepted: %+v", r)
	}
}

func TestManualAdjacentFloorKeepsXY(t *testing.T) {
	s, _, req := floorFixture(t, 7, 8)
	req.Floor = 8
	req.AdjacentFloors = false
	body, ct := trackingBody(t, req)
	w := replayTracking(s.routes(), body, ct)
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	var r Result
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	assertPosition(t, r, Position{32958, 32077, 8})
	if !reflect.DeepEqual(r.SearchedFloors, []int{8}) {
		t.Fatalf("manual floor overridden: %+v", r)
	}
}

func TestFloorSearchBoundsAndMissingData(t *testing.T) {
	s, im, req := floorFixture(t, 7, 9)
	r, _, err := s.locateLocal(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Found || !reflect.DeepEqual(r.UnavailableFloors, []int{6, 8}) {
		t.Fatalf("searched outside ±1 or hid missing maps: %+v", r)
	}
	s, im, req = floorFixture(t, 7, 6)
	req.Near.X -= 30
	r, _, err = s.locateLocal(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Found || r.Position != nil {
		t.Fatalf("accepted transition outside XY radius: %+v", r)
	}
	req.Near.X += 30
	req.AdjacentFloors = false
	r, _, err = s.locateLocal(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Found || !reflect.DeepEqual(r.SearchedFloors, []int{7}) {
		t.Fatalf("auto floor switch disabled: %+v", r)
	}
}

func TestLocalFloorLoaderSkipsDistantChunks(t *testing.T) {
	s, im, req := floorFixture(t, 7, 6)
	if err := os.WriteFile(filepath.Join(s.dir, "Minimap_Color_60000_60000_6.png"), []byte("corrupt distant tile"), 0600); err != nil {
		t.Fatal(err)
	}
	r, a, err := s.locateLocal(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, r, Position{32958, 32077, 6})
	if a.Image.Bounds().Dx() != 256 || a.Image.Bounds().Dy() != 256 {
		t.Fatal("loaded more than the nearby tile")
	}
}

func TestFloorTransitionRealAtlas(t *testing.T) {
	if os.Getenv("MINIMAP_REAL_MAP_TEST") != "1" {
		t.Skip("requires local real atlas")
	}
	s, im, req := floorFixture(t, 8)
	s.dir = "../data/minimap"
	s.cached = nil
	start := time.Now()
	r, _, err := s.locateLocal(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, r, Position{32958, 32077, 7})
	t.Logf("cold adjacent-floor lookup: %s, %d positions, floors %v", time.Since(start), r.SearchPositions, r.SearchedFloors)
}

func BenchmarkAdjacentFloorCold(b *testing.B) {
	s, im, req := floorFixture(b, 8, 7)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.localAtlases = nil
		r, _, err := s.locateLocal(context.Background(), im, req)
		if err != nil || !r.Found {
			b.Fatalf("%+v %v", r, err)
		}
	}
}
