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

// frame draws the attack border as one horizontal line across the entry.
func frame(im *image.NRGBA, y int) {
	for x := 0; x < im.Bounds().Dx(); x++ {
		im.SetNRGBA(x, y, color.NRGBA{R: frameColor.R, G: frameColor.G, B: frameColor.B, A: 255})
	}
}

func opts() battle.Options {
	return battle.Options{
		Geometry: mini, Colors: vision.DefaultColors(), Tolerance: 12, BlackMax: 48,
		RowPitch: pitch, Frame: frameColor, FrameTolerance: 12, FrameCoverage: 0.8,
	}
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
}

func TestReadFindsTargetFrame(t *testing.T) {
	im := canvas(60, 100)
	paint(im, image.Pt(30, 10), 18)
	paint(im, image.Pt(30, 10+pitch), 9)
	frame(im, 10+pitch-6) // attack frame line above the second row
	list := battle.Read(im, opts())
	if len(list.Rows) != 2 {
		t.Fatalf("odczytano %d wierszy, oczekiwano 2", len(list.Rows))
	}
	if list.Rows[0].Targeted {
		t.Error("pierwszy wiersz nie powinien mieć ramki")
	}
	if !list.Rows[1].Targeted {
		t.Error("drugi wiersz powinien mieć ramkę celu")
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
