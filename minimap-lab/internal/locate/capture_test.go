package locate

import (
	"context"
	"image"
	"image/draw"
	"testing"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/testenv"
)

// Actual browser capture received during diagnosis. Its palette differs from
// the atlas, and the previous demo left the caller incorrectly using zoom 2.
func TestActualCaptureAutomaticScale(t *testing.T) {
	ref := testenv.LoadFixture(t, "venore-reference.png")
	rgba := image.NewNRGBA(ref.Bounds())
	draw.Draw(rgba, rgba.Bounds(), ref, ref.Bounds().Min, draw.Src)
	a := &mapdata.Atlas{Image: rgba, Origin: image.Pt(32768, 32000), Floor: 7}
	capture := testenv.LoadFixture(t, "venore-capture.png")
	for _, scale := range []int{1, 2} {
		im := image.NewNRGBA(image.Rect(0, 0, capture.Bounds().Dx()*scale, capture.Bounds().Dy()*scale))
		for y := 0; y < im.Bounds().Dy(); y++ {
			for x := 0; x < im.Bounds().Dx(); x++ {
				im.Set(x, y, capture.At(x/scale, y/scale))
			}
		}
		o := Options{Zoom: 0, MarkerX: 52 * scale, MarkerY: 57 * scale, MaskRadius: 5 * scale, MinScore: .85, MinGap: .015}
		result, err := WithScale(context.Background(), a, im, o)
		if err != nil {
			t.Fatal(err)
		}
		assertPosition(t, result, mapdata.Position{X: 32958, Y: 32077, Z: 7})
		if result.Zoom != scale {
			t.Fatalf("scale: got %d want %d", result.Zoom, scale)
		}
		t.Logf("capture scale=%d score=%.4f", result.Zoom, result.Best.Score)
	}
}

func TestActualCaptureAgainstWholeFloor(t *testing.T) {
	a, err := mapdata.LoadAtlas(testenv.MapDir(t), 7)
	if err != nil {
		t.Fatal(err)
	}
	result, err := WithScale(context.Background(), a, testenv.LoadFixture(t, "venore-capture.png"), Options{Zoom: 0, MarkerX: 52, MarkerY: 57, MaskRadius: 5, MinScore: .85, MinGap: .015})
	if err != nil {
		t.Fatal(err)
	}
	assertPosition(t, result, mapdata.Position{X: 32958, Y: 32077, Z: 7})
	t.Logf("whole floor: score=%.4f zoom=%d", result.Best.Score, result.Zoom)
}
