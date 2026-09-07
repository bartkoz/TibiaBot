package testenv_test

import (
	"testing"

	"minimap-lab/internal/testenv"
)

func TestCombatFixtureGeometry(t *testing.T) {
	im := testenv.CombatCapture(t)
	fx := testenv.CombatCalibration()
	bounds := im.Bounds()

	// Każdy zmierzony prostokąt musi leżeć w obrazie, inaczej pomiar jest z
	// innego zrzutu niż zacommitowany plik.
	for name, r := range fx.Rects() {
		if !r.In(bounds) {
			t.Errorf("%s %v wychodzi poza obraz %v", name, r, bounds)
		}
	}
	// Okno gry musi mieć proporcje siatki, z tolerancją na skalowanie klienta.
	want := float64(fx.GridCols) / float64(fx.GridRows)
	got := float64(fx.Viewport.Dx()) / float64(fx.Viewport.Dy())
	if got < want*0.97 || got > want*1.03 {
		t.Errorf("okno gry ma proporcje %.3f, siatka %dx%d oczekuje %.3f",
			got, fx.GridCols, fx.GridRows, want)
	}
	if !fx.Crop.In(fx.Viewport) {
		t.Errorf("wycinek %v nie mieści się w oknie gry %v", fx.Crop, fx.Viewport)
	}
	if fx.Monsters < 3 {
		t.Errorf("fixture ma %d potworów, potrzebne co najmniej 3", fx.Monsters)
	}
}
