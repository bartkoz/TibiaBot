package battle_test

import (
	"image"
	"image/color"
	"testing"

	"minimap-lab/internal/battle"
	"minimap-lab/internal/testenv"
	"minimap-lab/internal/vision"
)

var mini = vision.Geometry{Width: 20, Height: 3, Border: 1}

const pitch = 22

// frameColor is deliberately far from every health colour in DefaultColors:
// #C00000 would sit inside the tolerance of the almost-dead bar (#BF0A0A) and
// the test would then pass for the wrong reason. The real client's frame
// colour is measured in step 4 and is allowed to be anything.
var frameColor = vision.Color{R: 0xFF, G: 0x50, B: 0x50}

func canvas(w, h int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
		}
	}
	return im
}

func paint(im *image.NRGBA, at image.Point, fill int) {
	for y := 0; y < mini.Height; y++ {
		for x := 0; x < mini.Width; x++ {
			im.SetNRGBA(at.X+x, at.Y+y, color.NRGBA{A: 255})
		}
	}
	green := vision.DefaultColors()[0]
	for y := 0; y < mini.InnerHeight(); y++ {
		for x := 0; x < fill; x++ {
			im.SetNRGBA(at.X+mini.Border+x, at.Y+mini.Border+y,
				color.NRGBA{R: green.R, G: green.G, B: green.B, A: 255})
		}
	}
}

// iconFrame draws the attack border as a square around the creature icon,
// offset from the bar the way the client draws it: to the left of the bar,
// centred on the same rows.
func iconFrame(im *image.NRGBA, barAt image.Point, offX, offY, size int) {
	x0, y0 := barAt.X+offX, barAt.Y+offY
	for i := 0; i < size; i++ {
		set := func(x, y int) {
			im.SetNRGBA(x, y, color.NRGBA{R: frameColor.R, G: frameColor.G, B: frameColor.B, A: 255})
		}
		set(x0+i, y0)        // top edge
		set(x0+i, y0+size-1) // bottom edge
		set(x0, y0+i)        // left edge
		set(x0+size-1, y0+i) // right edge
	}
}

func opts() battle.Options {
	return battle.Options{
		Geometry: mini, Colors: vision.DefaultColors(), Tolerance: 12, BlackMax: 48,
		RowPitch: pitch, Frame: frameColor, FrameTolerance: 12, FrameCoverage: 0.8,
	}
}

func iconOpts() battle.Options {
	o := opts()
	o.IconOffsetX, o.IconOffsetY, o.IconSize = -14, -6, 12
	o.FrameCoverage = 0.8
	return o
}

func TestReadCountsRowsTopDown(t *testing.T) {
	im := canvas(60, 100)
	paint(im, image.Pt(30, 10), 18)
	paint(im, image.Pt(30, 10+pitch), 9)
	paint(im, image.Pt(30, 10+2*pitch), 2)
	list := battle.Read(im, opts())
	if len(list.Rows) != 3 {
		t.Fatalf("odczytano %d wierszy, oczekiwano 3: %+v", len(list.Rows), list.Rows)
	}
	if list.Rows[0].Bar.Y >= list.Rows[1].Bar.Y || list.Rows[1].Bar.Y >= list.Rows[2].Bar.Y {
		t.Errorf("wiersze nie są od góry: %+v", list.Rows)
	}
	if hp := list.Rows[0].HP; hp < 0.99 {
		t.Errorf("pierwszy wiersz ma HP %.2f, oczekiwano pełnego", hp)
	}
	for i, r := range list.Rows {
		if r.Targeted {
			t.Errorf("wiersz %d ma ramkę celu, choć nikt nie jest atakowany", i)
		}
	}
	if list.Truncated {
		t.Error("lista mieszcząca się w całości nie powinna być oznaczona jako przewinięta")
	}
}

func TestReadFindsTargetFrameAroundIcon(t *testing.T) {
	im := canvas(60, 100)
	paint(im, image.Pt(30, 10), 18)
	paint(im, image.Pt(30, 10+pitch), 9)
	// Frame around the icon of the SECOND row's bar (top-left corner at
	// (30, 10+pitch) minus the icon offset).
	iconFrame(im, image.Pt(30, 10+pitch), -14, -6, 12)
	list := battle.Read(im, iconOpts())
	if len(list.Rows) != 2 {
		t.Fatalf("odczytano %d wierszy, oczekiwano 2", len(list.Rows))
	}
	if list.Rows[0].Targeted {
		t.Error("pierwszy wiersz nie powinien mieć ramki")
	}
	if !list.Rows[1].Targeted {
		t.Error("drugi wiersz powinien mieć ramkę wokół ikonki")
	}
}

func TestReadFrameDoesNotLeakToNeighbour(t *testing.T) {
	im := canvas(60, 100)
	paint(im, image.Pt(30, 10), 18)
	paint(im, image.Pt(30, 10+pitch), 9)
	// Frame around the FIRST row's icon must not mark the second row: icon
	// squares of adjacent rows do not overlap (size 12 < pitch 22).
	iconFrame(im, image.Pt(30, 10), -14, -6, 12)
	list := battle.Read(im, iconOpts())
	if !list.Rows[0].Targeted {
		t.Error("pierwszy wiersz powinien mieć ramkę")
	}
	if list.Rows[1].Targeted {
		t.Error("drugi wiersz nie powinien złapać ramki sąsiada")
	}
}

func TestReadGuardsInvalidInput(t *testing.T) {
	if list := battle.Read(nil, opts()); len(list.Rows) != 0 || list.Truncated {
		t.Errorf("Read(nil, ...) dało %+v, oczekiwano pustej listy", list)
	}
	o := opts()
	o.RowPitch = 0
	if list := battle.Read(canvas(60, 100), o); len(list.Rows) != 0 || list.Truncated {
		t.Errorf("Read z RowPitch=0 dało %+v, oczekiwano pustej listy", list)
	}
}

func TestReadEmptyList(t *testing.T) {
	list := battle.Read(canvas(60, 100), opts())
	if len(list.Rows) != 0 || list.Truncated {
		t.Errorf("pusta lista dała %+v", list)
	}
}

func TestReadMarksTruncatedList(t *testing.T) {
	im := canvas(60, 40)
	paint(im, image.Pt(30, 5), 18)
	paint(im, image.Pt(30, 5+pitch), 18)
	list := battle.Read(im, opts())
	if !list.Truncated {
		t.Error("lista dochodząca do dolnej krawędzi musi być oznaczona jako przewinięta")
	}
}

func TestReadOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.NRGBACrop(t, testenv.CombatCapture(t), fx.Battle)
	list := battle.Read(im, opts())
	t.Logf("wiersze na prawdziwej klatce: %+v (przewinięta: %v)", list.Rows, list.Truncated)
	if len(list.Rows) == 0 {
		t.Fatal("na prawdziwej klatce nie znaleziono żadnego wiersza; sprawdź prostokąt " +
			"battle listy i geometrię mini-paska")
	}
	if fx.TargetRow >= 0 {
		if fx.TargetRow >= len(list.Rows) {
			t.Fatalf("człowiek wskazał wiersz %d, odczytano tylko %d", fx.TargetRow, len(list.Rows))
		}
		if !list.Rows[fx.TargetRow].Targeted {
			t.Errorf("wiersz %d nie został rozpoznany jako cel; popraw Frame, FrameTolerance "+
				"albo FrameCoverage", fx.TargetRow)
		}
		for i, r := range list.Rows {
			if i != fx.TargetRow && r.Targeted {
				t.Errorf("wiersz %d fałszywie rozpoznany jako cel", i)
			}
		}
	}
}
