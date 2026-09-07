package vision_test

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"

	"minimap-lab/internal/testenv"
	"minimap-lab/internal/vision"
)

func grid() vision.Grid {
	return vision.Grid{
		Cols: 15, Rows: 11, TileW: 32, TileH: 32,
		CropX: 64, CropY: 0, Geometry: classic,
	}
}

func TestPlayerCentreIsMiddleTile(t *testing.T) {
	x, y := grid().PlayerCentre()
	if x != 7.5*32 || y != 5.5*32 {
		t.Errorf("środek kratki postaci %v,%v, oczekiwano %v,%v", x, y, 7.5*32.0, 5.5*32.0)
	}
}

// Moving a bar by exactly one tile must change the offset by exactly one.
// That property holds regardless of any measurement.
func TestOffsetMovesOneTilePerTile(t *testing.T) {
	g := grid()
	ax, ay := g.Offset(vision.Bar{X: 100, Y: 100})
	bx, by := g.Offset(vision.Bar{X: 132, Y: 164})
	if math.Abs((bx-ax)-1) > 1e-9 {
		t.Errorf("kratka w prawo zmieniła dx o %.9f, oczekiwano 1", bx-ax)
	}
	if math.Abs((by-ay)-2) > 1e-9 {
		t.Errorf("dwie kratki w dół zmieniły dy o %.9f, oczekiwano 2", by-ay)
	}
}

// A difference test cancels CropX and the bar's own width away, and pairing
// AnchorFrom with Offset cancels barCentre against itself, so neither would
// notice if the crop offset were applied with the wrong sign. This pins the
// absolute mapping: flipping CropX's sign moves dx from about -1.95 to -5.95.
func TestOffsetMapsAbsolutePosition(t *testing.T) {
	dx, dy := grid().Offset(vision.Bar{X: 100, Y: 100})
	if dx != -1.953125 || dy != -2.3125 {
		t.Errorf("offset %.6f,%.6f, oczekiwano -1.953125,-2.3125", dx, dy)
	}
}

func TestAnchorPutsOwnBarAtZero(t *testing.T) {
	g := grid()
	self := vision.Bar{X: 150, Y: 170}
	g.AnchorDX, g.AnchorDY = g.AnchorFrom(self)
	dx, dy := g.Offset(self)
	if dx != 0 || dy != 0 {
		t.Errorf("własny pasek dał offset %.9f,%.9f, oczekiwano 0,0", dx, dy)
	}
}

func TestDistanceIsChebyshev(t *testing.T) {
	tests := []struct {
		dx, dy, want float64
	}{
		{0, 0, 0},
		{1, 0, 1},
		{0, -1, 1},
		{3, -4, 4},
		{-2.5, 1.2, 2.5},
	}
	for _, tt := range tests {
		if got := vision.Distance(tt.dx, tt.dy); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("Distance(%v,%v) = %v, oczekiwano %v", tt.dx, tt.dy, got, tt.want)
		}
	}
}

func TestNieskalibrowanaSiatkaNieWybucha(t *testing.T) {
	var g vision.Grid
	if g.Valid() {
		t.Fatal("pusta siatka nie powinna być poprawna")
	}
	if dx, dy := g.Offset(vision.Bar{X: 5, Y: 5}); dx != 0 || dy != 0 {
		t.Errorf("pusta siatka dała offset %v,%v, oczekiwano 0,0", dx, dy)
	}
}

// TestRealCaptureOffsets draws a diagnostic overlay and checks that no
// creature's offset falls further out than the crop can physically show.
func TestRealCaptureOffsets(t *testing.T) {
	fx := testenv.CombatCalibration()
	if fx.SelfBar == (image.Point{}) {
		t.Skip("brak pomiaru własnego paska w CombatCalibration — bez niego nie ma z czego wyliczyć " +
			"zakotwiczenia; zmierz go albo wypełnij AnchorDX/AnchorDY ręcznie")
	}
	im := testenv.NRGBACrop(t, testenv.CombatCapture(t), fx.Crop)
	g := vision.Grid{
		Cols: fx.GridCols, Rows: fx.GridRows,
		TileW:    float64(fx.Viewport.Dx()) / float64(fx.GridCols),
		TileH:    float64(fx.Viewport.Dy()) / float64(fx.GridRows),
		CropX:    float64(fx.Crop.Min.X - fx.Viewport.Min.X),
		CropY:    float64(fx.Crop.Min.Y - fx.Viewport.Min.Y),
		Geometry: classic,
	}
	g.AnchorDX, g.AnchorDY = g.AnchorFrom(vision.Bar{X: fx.SelfBar.X, Y: fx.SelfBar.Y})
	t.Logf("zakotwiczenie wyliczone z własnego paska: dx=%.2f dy=%.2f", g.AnchorDX, g.AnchorDY)

	o := opts(classic)
	o.Exclude = []image.Point{fx.SelfBar}
	bars := vision.Find(im, o)
	limitX := float64(fx.Crop.Dx()) / g.TileW / 2
	limitY := float64(fx.Crop.Dy()) / g.TileH / 2
	for _, b := range bars {
		dx, dy := g.Offset(b)
		t.Logf("stwór na %.2f,%.2f (dystans %.2f, HP %.0f%%)",
			dx, dy, vision.Distance(dx, dy), 100*b.HP(classic))
		if math.Abs(dx) > limitX+1 || math.Abs(dy) > limitY+1 {
			t.Errorf("stwór na %.2f,%.2f wypada poza wycinek (%.2f x %.2f kratek) — "+
				"zakotwiczenie albo prostokąt wycinka są złe", dx, dy, 2*limitX, 2*limitY)
		}
	}
	// Eyeball diagnostics: bar outlines drawn onto the crop. The .debug
	// directory is gitignored, so a fresh clone won't have it.
	debugDir := filepath.Join(testenv.RepoRoot(t), ".debug")
	if err := os.MkdirAll(debugDir, 0o700); err != nil {
		t.Fatal(err)
	}
	out := image.NewNRGBA(im.Bounds())
	copy(out.Pix, im.Pix)
	for _, b := range bars {
		for x := b.X; x < b.X+classic.Width; x++ {
			out.SetNRGBA(x, b.Y, color.NRGBA{R: 255, B: 255, A: 255})
			out.SetNRGBA(x, b.Y+classic.Height-1, color.NRGBA{R: 255, B: 255, A: 255})
		}
	}
	testenv.SavePNG(t, filepath.Join(debugDir, "vision-fixture.png"), out)
}
