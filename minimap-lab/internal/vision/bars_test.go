package vision_test

import (
	"image"
	"image/color"
	"testing"

	"minimap-lab/internal/testenv"
	"minimap-lab/internal/vision"
)

var classic = vision.Geometry{Width: 27, Height: 4, Border: 1}

// background is neither a fill colour nor dark, so nothing in an empty image
// can be mistaken for part of a bar.
var background = color.NRGBA{R: 100, G: 100, B: 100, A: 255}

func canvas(w, h int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, background)
		}
	}
	return im
}

// paint draws one bar the way the client does: a black rectangle with a
// coloured prefix inside it. The unfilled remainder stays black, which is why
// the detector's rule is the same at every health level.
func paint(im *image.NRGBA, g vision.Geometry, at image.Point, fill int, c vision.Color) {
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			im.SetNRGBA(at.X+x, at.Y+y, color.NRGBA{A: 255})
		}
	}
	for y := 0; y < g.InnerHeight(); y++ {
		for x := 0; x < fill; x++ {
			im.SetNRGBA(at.X+g.Border+x, at.Y+g.Border+y,
				color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
}

func opts(g vision.Geometry) vision.Options {
	return vision.Options{
		Geometry: g, Colors: vision.DefaultColors(),
		Tolerance: 12, BlackMax: 48, ExcludeTolerance: 2,
	}
}

func TestFind(t *testing.T) {
	green := vision.DefaultColors()[0]
	tests := []struct {
		name  string
		build func() (*image.NRGBA, vision.Options)
		want  []vision.Bar
	}{
		{
			name: "jeden pasek w pełni wypełniony",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), classic.InnerWidth(), green)
				return im, opts(classic)
			},
			want: []vision.Bar{{X: 10, Y: 20, Fill: 25}},
		},
		{
			name: "pasek w połowie",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), 12, green)
				return im, opts(classic)
			},
			want: []vision.Bar{{X: 10, Y: 20, Fill: 12}},
		},
		{
			name: "pasek pusty jest niewidoczny, bo cały jest czarny",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), 0, green)
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "dwa paski obok siebie",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(5, 10), 20, green)
				paint(im, classic, image.Pt(40, 30), 7, green)
				return im, opts(classic)
			},
			want: []vision.Bar{{X: 5, Y: 10, Fill: 20}, {X: 40, Y: 30, Fill: 7}},
		},
		{
			name: "pasek przycięty prawą krawędzią nie jest zgłaszany",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(30, 60)
				paint(im, classic, image.Pt(10, 20), 12, green)
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "pasek przycięty górną krawędzią nie jest zgłaszany",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				// Górna obwódka wypada nad obrazem.
				paint(im, classic, image.Pt(10, -1), 12, green)
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "własny pasek jest wykluczany po dokładnej pozycji",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(46, 28), 25, green)
				o := opts(classic)
				o.Exclude = []image.Point{{X: 46, Y: 28}}
				return im, o
			},
			want: nil,
		},
		{
			name: "potwór kratkę nad postacią nie jest wykluczany razem z własnym paskiem",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 120)
				paint(im, classic, image.Pt(46, 28), 25, green) // własny
				paint(im, classic, image.Pt(46, 60), 18, green) // kratkę niżej
				o := opts(classic)
				o.Exclude = []image.Point{{X: 46, Y: 28}}
				return im, o
			},
			want: []vision.Bar{{X: 46, Y: 60, Fill: 18}},
		},
		{
			name: "barwa poza tolerancją nie jest paskiem",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), 12, vision.Color{R: 10, G: 10, B: 200})
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "przeskalowana geometria działa tym samym kodem",
			build: func() (*image.NRGBA, vision.Options) {
				g := vision.Geometry{Width: 54, Height: 8, Border: 2}
				im := canvas(200, 90)
				paint(im, g, image.Pt(20, 30), 40, green)
				return im, opts(g)
			},
			want: []vision.Bar{{X: 20, Y: 30, Fill: 40}},
		},
		{
			name: "duża plama barwy paska nie jest paskiem",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				for y := 10; y < 50; y++ {
					for x := 10; x < 110; x++ {
						im.SetNRGBA(x, y, color.NRGBA{R: green.R, G: green.G, B: green.B, A: 255})
					}
				}
				return im, opts(classic)
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			im, o := tt.build()
			got := vision.Find(im, o)
			if len(got) != len(tt.want) {
				t.Fatalf("znaleziono %d pasków (%v), oczekiwano %d (%v)",
					len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("pasek %d: %v, oczekiwano %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBarHP(t *testing.T) {
	if hp := (vision.Bar{Fill: 25}).HP(classic); hp != 1 {
		t.Errorf("pełny pasek dał %.3f, oczekiwano 1", hp)
	}
	if hp := (vision.Bar{Fill: 5}).HP(classic); hp < 0.19 || hp > 0.21 {
		t.Errorf("pasek 5/25 dał %.3f, oczekiwano około 0,2", hp)
	}
}

// TestFindOnRealCapture jest progiem regresji: nie sprawdza dokładnej liczby,
// bo własny pasek nie jest jeszcze zmierzony (zadanie 3), tylko czy detektor
// widzi co najmniej tyle stworów, ile policzył człowiek.
func TestFindOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.NRGBACrop(t, testenv.CombatCapture(t), fx.Crop)
	bars := vision.Find(im, opts(classic))
	if len(bars) < fx.Monsters {
		t.Errorf("na prawdziwej klatce znaleziono %d pasków, człowiek policzył %d potworów; "+
			"sprawdź geometrię, barwy i prostokąt wycinka", len(bars), fx.Monsters)
	}
	t.Logf("paski na prawdziwej klatce: %v", bars)
}
