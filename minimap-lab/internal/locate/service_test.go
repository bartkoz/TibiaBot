package locate

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

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/testenv"
)

// The brain calls this in-process. If it still needed a ResponseWriter the
// split out of the HTTP layer would not have happened.
func TestServiceLocatesOnTheDemoAtlasWithoutHTTP(t *testing.T) {
	s := NewService("")
	im := mapdata.DemoSnippet(mapdata.DemoAtlas())
	res, atlas, err := s.Locate(context.Background(), im, Request{
		Options: Options{Zoom: 2, MarkerX: 94, MarkerY: 94, MaskRadius: 5, MinScore: .94, MinGap: .015},
		Floor:   7, Demo: true,
	})
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if atlas == nil {
		t.Fatal("brak atlasu w odpowiedzi")
	}
	assertPosition(t, res, mapdata.Position{X: 32200, Y: 32180, Z: 7})
	if res.MatchMS <= 0 {
		t.Error("czas dopasowania nie został zmierzony")
	}
}

func TestServiceRefusesAnInvalidRequestBeforeTouchingTheDisk(t *testing.T) {
	s := NewService("")
	for _, c := range []struct {
		name string
		req  Request
	}{
		{"piętro poza zakresem", Request{Floor: 99}},
		{"skala poza zakresem", Request{Options: Options{Zoom: 99}, Floor: 7, Near: &mapdata.Position{Z: 7}, Radius: 5}},
		{"piętro dalej niż o jedno", Request{Options: Options{Zoom: 1}, Floor: 7, Near: &mapdata.Position{Z: 5}, Radius: 5}},
	} {
		if _, _, err := s.Locate(context.Background(), image.NewNRGBA(image.Rect(0, 0, 8, 8)), c.req); err == nil {
			t.Errorf("%s: przyjęto nieprawidłowe żądanie", c.name)
		}
	}
}

// floorFixture builds a map directory where the floor the tracker believes in
// carries a picture nothing can match, so only a genuine adjacent-floor search
// can find the character.
func floorFixture(t testing.TB, oldZ int, targetFloors ...int) (*Service, image.Image, Request) {
	t.Helper()
	dir := t.TempDir()
	ref := testenv.LoadFixture(t, "venore-reference.png")
	wrong := image.NewNRGBA(ref.Bounds())
	draw.Draw(wrong, wrong.Bounds(), image.NewUniform(color.NRGBA{12, 12, 12, 255}), image.Point{}, draw.Src)
	testenv.SavePNG(t, filepath.Join(dir, fmt.Sprintf("Minimap_Color_32768_32000_%d.png", oldZ)), wrong)
	for _, z := range targetFloors {
		testenv.SavePNG(t, filepath.Join(dir, fmt.Sprintf("Minimap_Color_32768_32000_%d.png", z)), ref)
	}
	req := Request{
		Options: Options{Zoom: 1, MarkerX: 52, MarkerY: 57, MaskRadius: 5, MinScore: .85, MinGap: .015},
		Floor:   oldZ, Near: &mapdata.Position{X: 32957, Y: 32076, Z: oldZ},
		Radius: 5, AdjacentFloors: true, FloorRadius: 8,
	}
	return NewService(dir), testenv.LoadFixture(t, "venore-capture.png"), req
}

func TestAdjacentFloorTransitions(t *testing.T) {
	for _, pair := range [][2]int{{7, 6}, {7, 8}, {0, 1}, {15, 14}} {
		t.Run(fmt.Sprintf("%d_to_%d", pair[0], pair[1]), func(t *testing.T) {
			s, im, req := floorFixture(t, pair[0], pair[1])
			r, a, err := s.Locate(context.Background(), im, req)
			if err != nil {
				t.Fatal(err)
			}
			assertPosition(t, r, mapdata.Position{X: 32958, Y: 32077, Z: pair[1]})
			if !r.FloorChanged || r.Mode != "local" || r.SearchPositions > 699 || a.Floor != pair[1] {
				t.Fatalf("not a bounded floor transition: %+v", r)
			}
			for _, z := range r.SearchedFloors {
				if z < 0 || z > 15 || abs(z-pair[0]) > 1 {
					t.Fatalf("searched non-adjacent floor: %d", z)
				}
			}
			// Normal movement on the new floor reuses the small atlas at once.
			req.Floor = pair[1]
			req.Near = r.Position
			if err = os.Remove(filepath.Join(s.dir, fmt.Sprintf("Minimap_Color_32768_32000_%d.png", pair[1]))); err != nil {
				t.Fatal(err)
			}
			r, warm, err := s.Locate(context.Background(), im, req)
			if err != nil {
				t.Fatal(err)
			}
			assertPosition(t, r, mapdata.Position{X: 32958, Y: 32077, Z: pair[1]})
			if r.FloorChanged || warm != a || !reflect.DeepEqual(r.SearchedFloors, []int{pair[1]}) {
				t.Fatalf("cache not reused: %+v", r)
			}
		})
	}
}

func TestAmbiguousFloorsDoNotGuessZ(t *testing.T) {
	s, im, req := floorFixture(t, 7, 6, 8)
	r, _, err := s.Locate(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Found || r.Position != nil || r.Competitor == nil || r.Best.Z == r.Competitor.Z {
		t.Fatalf("ambiguous Z accepted: %+v", r)
	}
}

func TestFloorSearchBoundsAndMissingData(t *testing.T) {
	s, im, req := floorFixture(t, 7, 9)
	r, _, err := s.Locate(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Found || !reflect.DeepEqual(r.UnavailableFloors, []int{6, 8}) {
		t.Fatalf("searched outside ±1 or hid missing maps: %+v", r)
	}
	s, im, req = floorFixture(t, 7, 6)
	req.Near.X -= 30
	r, _, err = s.Locate(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Found || r.Position != nil {
		t.Fatalf("accepted transition outside XY radius: %+v", r)
	}
	req.Near.X += 30
	req.AdjacentFloors = false
	r, _, err = s.Locate(context.Background(), im, req)
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
	r, a, err := s.Locate(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, r, mapdata.Position{X: 32958, Y: 32077, Z: 6})
	if a.Image.Bounds().Dx() != 256 || a.Image.Bounds().Dy() != 256 {
		t.Fatal("loaded more than the nearby tile")
	}
}

func TestFloorTransitionRealAtlas(t *testing.T) {
	_, im, req := floorFixture(t, 8)
	s := NewService(testenv.MapDir(t))
	start := time.Now()
	r, _, err := s.Locate(context.Background(), im, req)
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, r, mapdata.Position{X: 32958, Y: 32077, Z: 7})
	t.Logf("cold adjacent-floor lookup: %s, %d positions, floors %v", time.Since(start), r.SearchPositions, r.SearchedFloors)
}

func BenchmarkAdjacentFloorCold(b *testing.B) {
	s, im, req := floorFixture(b, 8, 7)
	dir := s.dir
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A fresh service is the honest cold start: no cache to invalidate.
		cold := NewService(dir)
		r, _, err := cold.Locate(context.Background(), im, req)
		if err != nil || !r.Found {
			b.Fatalf("%+v %v", r, err)
		}
	}
}

// The panel sends snake_case. Marshalling a Request and unmarshalling it again
// would pass whether or not the tags exist, so this pins the names against a
// literal copied from what the browser actually posts.
func TestRequestReadsTheWireNamesThePanelSends(t *testing.T) {
	const fromPanel = `{"floor":7,"demo":false,"zoom":1,"marker_x":52,"marker_y":57,
		"mask_radius":5,"min_score":0.85,"min_gap":0.015,"near":{"x":32958,"y":32077,"z":7},
		"radius":5,"adjacent_floors":true,"floor_radius":8}`
	var req Request
	if err := json.Unmarshal([]byte(fromPanel), &req); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !req.AdjacentFloors {
		t.Error("adjacent_floors nie dotarło — automatyczna zmiana piętra byłaby wyłączona bez śladu")
	}
	if req.FloorRadius != 8 {
		t.Errorf("floor_radius = %d, oczekiwano 8", req.FloorRadius)
	}
	if req.Floor != 7 || req.Radius != 5 || req.Near == nil || req.Near.X != 32958 {
		t.Errorf("żądanie = %+v", req)
	}
	if req.Zoom != 1 || req.MarkerX != 52 || req.MaskRadius != 5 || req.MinScore != 0.85 {
		t.Errorf("kalibracja nie dotarła: %+v", req.Options)
	}
}
