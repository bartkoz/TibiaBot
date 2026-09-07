package vitals_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"minimap-lab/internal/testenv"
	"minimap-lab/internal/vitals"
)

// bar builds a client bar: a filled prefix in the given colour, the rest dark.
func bar(w, h, fill int, c color.NRGBA) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < fill {
				im.SetNRGBA(x, y, c)
			} else {
				im.SetNRGBA(x, y, color.NRGBA{R: 20, G: 20, B: 20, A: 255})
			}
		}
	}
	return im
}

var (
	green = color.NRGBA{R: 0, G: 200, B: 0, A: 255}
	red   = color.NRGBA{R: 200, G: 0, B: 0, A: 255}
	blue  = color.NRGBA{R: 0, G: 60, B: 220, A: 255}
)

func TestReadPercent(t *testing.T) {
	tests := []struct {
		name string
		im   *image.NRGBA
		want float64
	}{
		{"pełny zielony", bar(100, 8, 100, green), 1},
		{"połowa zielonego", bar(100, 8, 50, green), 0.5},
		{"pusty", bar(100, 8, 0, green), 0},
		{"czerwony liczy się tak samo jak zielony", bar(100, 8, 25, red), 0.25},
		{"mana na niebiesko", bar(100, 8, 80, blue), 0.8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vitals.Read(tt.im, vitals.DefaultOptions())
			if !got.OK {
				t.Fatalf("odczyt odrzucony: %s", got.Reason)
			}
			if math.Abs(got.Percent-tt.want) > 0.02 {
				t.Errorf("odczyt %.3f, oczekiwano %.3f", got.Percent, tt.want)
			}
		})
	}
}

// A slipped calibration produces scattered hits, not a contiguous prefix.
// Reporting that as a low percentage would make the healing rule fire forever.
func TestReadRejectsScatteredFill(t *testing.T) {
	im := bar(100, 8, 0, green)
	for y := 0; y < 8; y++ {
		for _, x := range []int{5, 30, 70, 95} {
			im.SetNRGBA(x, y, green)
		}
	}
	got := vitals.Read(im, vitals.DefaultOptions())
	if got.OK {
		t.Errorf("rozsypane trafienia zostały przyjęte jako %.3f", got.Percent)
	}
	if got.Reason == "" {
		t.Error("odrzucony odczyt musi podać przyczynę")
	}
}

// One row cut by a digit the client draws over the bar must not move the
// result - hence the median across many rows.
func TestReadSurvivesOneCorruptedRow(t *testing.T) {
	im := bar(100, 8, 60, green)
	for _, x := range []int{80, 81, 82} {
		im.SetNRGBA(x, 4, green)
	}
	got := vitals.Read(im, vitals.DefaultOptions())
	if !got.OK {
		t.Fatalf("odczyt odrzucony: %s", got.Reason)
	}
	if math.Abs(got.Percent-0.6) > 0.02 {
		t.Errorf("odczyt %.3f, oczekiwano 0,6", got.Percent)
	}
}

func TestReadOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.CombatCapture(t)
	for name, r := range map[string]image.Rectangle{"HP": fx.HP, "mana": fx.Mana} {
		got := vitals.Read(testenv.NRGBACrop(t, im, r), vitals.DefaultOptions())
		t.Logf("%s: %.1f%% (ok: %v, %s)", name, 100*got.Percent, got.OK, got.Reason)
		if !got.OK {
			t.Errorf("%s odrzucony na prawdziwej klatce: %s — sprawdź prostokąt, "+
				"powinien obejmować sam pasek, bez obwódki i bez cyfr", name, got.Reason)
		}
	}
}
