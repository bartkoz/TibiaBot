package locate

import (
	"context"
	"image"
	"image/draw"
	"testing"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/testenv"
)

func trackingFrame(a *mapdata.Atlas, p image.Point) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, 65, 65))
	draw.Draw(im, im.Bounds(), a.Image, p.Sub(a.Origin).Sub(image.Pt(32, 32)), draw.Src)
	return im
}

func TestTrackMovingSequence(t *testing.T) {
	a := mapdata.DemoAtlas()
	near := mapdata.Position{X: 32190, Y: 32170, Z: 7}
	o := Options{Zoom: 1, MarkerX: 32, MarkerY: 32, MaskRadius: 5, MinScore: .85, MinGap: .015}
	for _, d := range []image.Point{{1, 0}, {1, 0}, {0, -1}, {-1, 0}, {0, 1}, {1, 1}, {-2, -2}, {0, 0}} {
		want := mapdata.Position{X: near.X + d.X, Y: near.Y + d.Y, Z: 7}
		result, err := Near(context.Background(), a, trackingFrame(a, image.Pt(want.X, want.Y)), o, near, 5)
		if err != nil {
			t.Fatal(err)
		}
		assertPosition(t, result, want)
		if result.Mode != "local" || result.SearchPositions != 121 {
			t.Fatalf("wrong search extent: %+v", result)
		}
		near = want
	}
}

func TestTrackDoesNotSilentlySearchWholeMap(t *testing.T) {
	a := mapdata.DemoAtlas()
	near := mapdata.Position{X: 32190, Y: 32170, Z: 7}
	o := Options{Zoom: 1, MarkerX: 32, MarkerY: 32, MaskRadius: 5, MinScore: .94, MinGap: .015}
	result, err := Near(context.Background(), a, trackingFrame(a, image.Pt(32240, 32220)), o, near, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Found || result.Position != nil || result.SearchPositions != 121 {
		t.Fatalf("teleport must lose local tracking: %+v", result)
	}
	result, err = Near(context.Background(), a, trackingFrame(a, image.Pt(32195, 32170)), o, near, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Found || result.Position != nil {
		t.Fatal("boundary hit must ask for wider search")
	}
	for _, radius := range []int{0, 65} {
		if _, err := Near(context.Background(), a, trackingFrame(a, image.Pt(32190, 32170)), o, near, radius); err == nil {
			t.Fatal("invalid radius accepted")
		}
	}
	near.Z = 8
	if _, err := Near(context.Background(), a, trackingFrame(a, image.Pt(32190, 32170)), o, near, 5); err == nil {
		t.Fatal("floor mismatch accepted")
	}
}

func actualTrackingFixture(t *testing.T) (*mapdata.Atlas, image.Image, Options) {
	ref := testenv.LoadFixture(t, "venore-reference.png")
	rgba := image.NewNRGBA(ref.Bounds())
	draw.Draw(rgba, rgba.Bounds(), ref, ref.Bounds().Min, draw.Src)
	c := testenv.VenoreCalibration()
	return &mapdata.Atlas{Image: rgba, Origin: image.Pt(32768, 32000), Floor: 7},
		testenv.LoadFixture(t, "venore-capture.png"),
		Options{Zoom: c.Zoom, MarkerX: c.MarkerX, MarkerY: c.MarkerY,
			MaskRadius: c.MaskRadius, MinScore: c.MinScore, MinGap: c.MinGap}
}

func TestTrackActualCapture(t *testing.T) {
	a, im, o := actualTrackingFixture(t)
	r, err := Near(context.Background(), a, im, o, mapdata.Position{X: 32957, Y: 32076, Z: 7}, 5)
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, r, mapdata.Position{X: 32958, Y: 32077, Z: 7})
	r, err = Near(context.Background(), a, im, o, mapdata.Position{X: 1000, Y: 1000, Z: 7}, 5)
	if err != nil || r.Found || r.SearchPositions != 0 {
		t.Fatalf("out-of-atlas window: %+v %v", r, err)
	}
}

func BenchmarkTrackActualCapture(b *testing.B) {
	// Fixture loading excluded from the timed repeated-frame path.
	ref := testenv.LoadFixture(b, "venore-reference.png")
	rgba := image.NewNRGBA(ref.Bounds())
	draw.Draw(rgba, rgba.Bounds(), ref, ref.Bounds().Min, draw.Src)
	a := &mapdata.Atlas{Image: rgba, Origin: image.Pt(32768, 32000), Floor: 7}
	im := testenv.LoadFixture(b, "venore-capture.png")
	o := Options{Zoom: 1, MarkerX: 52, MarkerY: 57, MaskRadius: 5, MinScore: .85, MinGap: .015}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := Near(context.Background(), a, im, o, mapdata.Position{X: 32958, Y: 32077, Z: 7}, 5)
		if err != nil || !r.Found {
			b.Fatalf("failed: %+v %v", r, err)
		}
	}
}
