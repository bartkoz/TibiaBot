package main

import (
	"context"
	"image"
	"image/draw"
	"image/png"
	"os"
	"testing"
)

func loadFixture(t testing.TB, name string) image.Image {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

// Actual browser capture received during diagnosis. Its palette differs from
// the atlas, and the previous demo left the caller incorrectly using zoom 2.
func TestActualCaptureAutomaticScale(t *testing.T) {
	ref := loadFixture(t, "venore-reference.png")
	rgba := image.NewNRGBA(ref.Bounds())
	draw.Draw(rgba, rgba.Bounds(), ref, ref.Bounds().Min, draw.Src)
	a := &Atlas{rgba, image.Pt(32768, 32000), 7}
	capture := loadFixture(t, "venore-capture.png")
	for _, scale := range []int{1, 2} {
		im := image.NewNRGBA(image.Rect(0, 0, capture.Bounds().Dx()*scale, capture.Bounds().Dy()*scale))
		for y := 0; y < im.Bounds().Dy(); y++ {
			for x := 0; x < im.Bounds().Dx(); x++ {
				im.Set(x, y, capture.At(x/scale, y/scale))
			}
		}
		o := Options{Zoom: 0, MarkerX: 52 * scale, MarkerY: 57 * scale, MaskRadius: 5 * scale, MinScore: .85, MinGap: .015}
		result, err := locateWithScale(context.Background(), a, im, o)
		if err != nil {
			t.Fatal(err)
		}
		assertPosition(t, result, Position{32958, 32077, 7})
		if result.Zoom != scale {
			t.Fatalf("scale: got %d want %d", result.Zoom, scale)
		}
		t.Logf("capture scale=%d score=%.4f", result.Zoom, result.Best.Score)
	}
}

func TestActualCaptureAgainstWholeFloor(t *testing.T) {
	if os.Getenv("MINIMAP_REAL_MAP_TEST") != "1" {
		t.Skip("requires local map atlas")
	}
	a, err := loadAtlas("../data/minimap", 7)
	if err != nil {
		t.Fatal(err)
	}
	result, err := locateWithScale(context.Background(), a, loadFixture(t, "venore-capture.png"), Options{Zoom: 0, MarkerX: 52, MarkerY: 57, MaskRadius: 5, MinScore: .85, MinGap: .015})
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, result, Position{32958, 32077, 7})
	t.Logf("whole floor: score=%.4f zoom=%d", result.Best.Score, result.Zoom)
}
