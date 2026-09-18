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

// darkBackground stands in for the real game view, which is mostly dark:
// unlike the neutral grey above, it is itself within BlackMax, so a bar
// found against it is not getting free help telling border from background.
var darkBackground = color.NRGBA{R: 20, G: 20, B: 20, A: 255}

func darkCanvas(w, h int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, darkBackground)
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

// paintRows draws a bar whose inner rows have the exact widths given, top to
// bottom, so a test can reproduce the client's antialiased edge (narrower
// first and last row) precisely.
func paintRows(im *image.NRGBA, g vision.Geometry, at image.Point, widths []int, c vision.Color) {
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			im.SetNRGBA(at.X+x, at.Y+y, color.NRGBA{A: 255})
		}
	}
	for dy, w := range widths {
		for x := 0; x < w; x++ {
			im.SetNRGBA(at.X+g.Border+x, at.Y+g.Border+dy,
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
			name: "wypełnienie jednopikselowe",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), 1, green)
				return im, opts(classic)
			},
			want: []vision.Bar{{X: 10, Y: 20, Fill: 1}},
		},
		{
			name: "pasek na ciemnym tle gry jest wykrywany",
			build: func() (*image.NRGBA, vision.Options) {
				im := darkCanvas(120, 60)
				paint(im, classic, image.Pt(10, 20), 12, green)
				return im, opts(classic)
			},
			want: []vision.Bar{{X: 10, Y: 20, Fill: 12}},
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
			name: "dwa nachodzące paski są liczone oba, lewy z obciętym wypełnieniem",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), 20, green) // A: painted with fill 20
				paint(im, classic, image.Pt(25, 20), 10, green) // B: starts inside A's fill span, painted after A
				return im, opts(classic)
			},
			// B is painted after A, so B's own black left border (at x=25)
			// overwrites part of A's colour and cuts A's run short there. A's
			// coloured run then measures from its first fill column
			// (A.X+Border=11) up to B's left border column (25):
			// 25 - (10 + 1) = 14, not the 20 it was painted with. B itself is
			// untouched, since nothing overlaps its right side.
			want: []vision.Bar{{X: 10, Y: 20, Fill: 14}, {X: 25, Y: 20, Fill: 10}},
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
			name: "pasek przycięty lewą krawędzią nie jest zgłaszany",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				// The left border falls left of the image.
				paint(im, classic, image.Pt(-1, 20), 12, green)
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "pasek przycięty górną krawędzią nie jest zgłaszany",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				// The top border falls above the image.
				paint(im, classic, image.Pt(10, -1), 12, green)
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "pasek przycięty dolną krawędzią nie jest zgłaszany",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				// The bottom border falls below the image.
				paint(im, classic, image.Pt(10, im.Bounds().Dy()-classic.Height+1), 12, green)
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
				paint(im, classic, image.Pt(46, 28), 25, green) // own
				paint(im, classic, image.Pt(46, 60), 18, green) // one tile below
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

func TestConfirmEdgeTolerance(t *testing.T) {
	green := vision.DefaultColors()[0]
	// Height 6, border 1 -> 4 inner rows. Core (middle two) is 20; the first
	// and last inner rows are one pixel narrower, exactly like the client's
	// antialiased edge.
	geo := vision.Geometry{Width: 24, Height: 6, Border: 1}
	build := func() (*image.NRGBA, vision.Options) {
		im := darkCanvas(60, 40)
		paintRows(im, geo, image.Pt(10, 10), []int{19, 20, 20, 19}, green)
		o := opts(geo)
		return im, o
	}

	im, o := build()
	o.EdgeTolerance = 0
	if bars := vision.Find(im, o); len(bars) != 0 {
		t.Errorf("przy EdgeTolerance=0 rozmyty brzeg musi odrzucić pasek, dostałem %v", bars)
	}

	im, o = build()
	o.EdgeTolerance = 1
	bars := vision.Find(im, o)
	if len(bars) != 1 {
		t.Fatalf("przy EdgeTolerance=1 pasek z brzegiem o 1 px węższym musi przejść, dostałem %v", bars)
	}
	if bars[0].Fill != 20 {
		t.Errorf("Fill musi być rdzeniem (20), nie brzegiem — dostałem %d", bars[0].Fill)
	}
}

func TestConfirmRejectsWiderEdge(t *testing.T) {
	green := vision.DefaultColors()[0]
	geo := vision.Geometry{Width: 24, Height: 6, Border: 1}
	im := darkCanvas(60, 40)
	// A row WIDER than the core is not antialiasing - it is a different shape.
	paintRows(im, geo, image.Pt(10, 10), []int{21, 20, 20, 19}, green)
	o := opts(geo)
	o.EdgeTolerance = 4
	if bars := vision.Find(im, o); len(bars) != 0 {
		t.Errorf("brzeg szerszy od rdzenia musi odrzucić pasek przy każdej tolerancji, dostałem %v", bars)
	}
}

func TestConfirmRejectsCrookedMiddle(t *testing.T) {
	green := vision.DefaultColors()[0]
	geo := vision.Geometry{Width: 24, Height: 7, Border: 1}
	im := darkCanvas(60, 40)
	// Height 7, border 1 -> 5 inner rows. A middle row differs: that is a
	// projectile or a digit cutting the bar, never antialiasing, so it must be
	// rejected even at the loosest tolerance.
	paintRows(im, geo, image.Pt(10, 10), []int{19, 20, 18, 20, 19}, green)
	o := opts(geo)
	o.EdgeTolerance = 4
	if bars := vision.Find(im, o); len(bars) != 0 {
		t.Errorf("różny wiersz środkowy musi odrzucić pasek, dostałem %v", bars)
	}
}

func TestConfirmRejectsEmptyRow(t *testing.T) {
	green := vision.DefaultColors()[0]
	geo := vision.Geometry{Width: 24, Height: 6, Border: 1}
	im := darkCanvas(60, 40)
	// A zero-width inner row is a failure, not a width eligible for tolerance.
	paintRows(im, geo, image.Pt(10, 10), []int{20, 0, 20, 20}, green)
	o := opts(geo)
	o.EdgeTolerance = 4
	if bars := vision.Find(im, o); len(bars) != 0 {
		t.Errorf("pusty wiersz wnętrza musi odrzucić pasek, dostałem %v", bars)
	}
}

// TestFindOnRealCapture checks the detector against the real capture: once
// the character's own bar can be excluded, it must find exactly as many
// creature bars as a human counted.
func TestFindOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.NRGBACrop(t, testenv.CombatCapture(t), fx.Crop)
	o := opts(classic)
	if fx.SelfBar != (image.Point{}) {
		o.Exclude = []image.Point{fx.SelfBar}
	}
	bars := vision.Find(im, o)
	if len(bars) != fx.Monsters {
		t.Errorf("na prawdziwej klatce znaleziono %d pasków stworów, człowiek policzył %d: %v; "+
			"sprawdź w tej kolejności prostokąt wycinka, BlackMax, tolerancję barw i geometrię",
			len(bars), fx.Monsters, bars)
	}
	t.Logf("paski na prawdziwej klatce: %v", bars)
}
