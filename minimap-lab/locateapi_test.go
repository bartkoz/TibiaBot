package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"path/filepath"
	"reflect"
	"testing"

	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/testenv"
)

// The floor-transition logic itself is tested in internal/locate, where it now
// lives. What is left here is the HTTP surface around it.

func floorServer(t testing.TB, oldZ int, targetFloors ...int) (*server, matchRequest) {
	t.Helper()
	dir := t.TempDir()
	ref := testenv.LoadFixture(t, "venore-reference.png")
	wrong := image.NewNRGBA(ref.Bounds())
	draw.Draw(wrong, wrong.Bounds(), image.NewUniform(color.NRGBA{12, 12, 12, 255}), image.Point{}, draw.Src)
	testenv.SavePNG(t, filepath.Join(dir, fmt.Sprintf("Minimap_Color_32768_32000_%d.png", oldZ)), wrong)
	for _, z := range targetFloors {
		testenv.SavePNG(t, filepath.Join(dir, fmt.Sprintf("Minimap_Color_32768_32000_%d.png", z)), ref)
	}
	s := newServer(dir)
	req := matchRequest{Request: locate.Request{
		Options: locate.Options{Zoom: 1, MarkerX: 52, MarkerY: 57, MaskRadius: 5, MinScore: .85, MinGap: .015},
		Floor:   oldZ, Near: &mapdata.Position{X: 32957, Y: 32076, Z: oldZ},
		Radius: 5, AdjacentFloors: true, FloorRadius: 8,
	}, NoPreview: true}
	return s, req
}

func TestManualAdjacentFloorKeepsXY(t *testing.T) {
	s, req := floorServer(t, 7, 8)
	req.Floor = 8
	req.AdjacentFloors = false
	body, ct := trackingBody(t, req)
	w := replayTracking(s.routes(), body, ct)
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	var r locate.Result
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	assertPosition(t, r, mapdata.Position{X: 32958, Y: 32077, Z: 8})
	if !reflect.DeepEqual(r.SearchedFloors, []int{8}) {
		t.Fatalf("manual floor overridden: %+v", r)
	}
}

// A request the service would refuse must come back as a 400 with the reason,
// not as a generic failure the panel cannot explain.
func TestMatchRefusesAnInvalidLocalArea(t *testing.T) {
	s, req := floorServer(t, 7, 8)
	req.Near.Z = 4 // more than one floor from Floor
	body, ct := trackingBody(t, req)
	w := replayTracking(s.routes(), body, ct)
	if w.Code != 400 {
		t.Fatalf("kod = %d, oczekiwano 400", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("odmowa bez powodu")
	}
}
