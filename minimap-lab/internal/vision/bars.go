// Package vision reads the health bars the game client draws above every
// creature. It is deliberately the only package that touches game-window
// pixels, and it holds neither state nor a clock: pixels in, bars out.
// Everything that needs memory of what was seen on an earlier frame lives in
// internal/combat.
package vision

import "image"

// Geometry describes one health bar as the client draws it: a black border
// with a coloured prefix inside. The classic numbers are 27x4 with a 1px
// border, but they are a calibration hypothesis rather than a constant - the
// client scales the game window and the operating system scales the client,
// so both can change underneath us.
type Geometry struct {
	Width  int
	Height int
	Border int
}

func (g Geometry) InnerWidth() int  { return g.Width - 2*g.Border }
func (g Geometry) InnerHeight() int { return g.Height - 2*g.Border }

func (g Geometry) valid() bool {
	return g.Border >= 1 && g.InnerWidth() >= 1 && g.InnerHeight() >= 1
}

// Color is one fill colour the client quantises creature health to.
type Color struct{ R, G, B uint8 }

// DefaultColors are the six values the client is known to quantise creature
// health to, brightest first. They are a starting point for calibration, not
// gospel: verify them against a real capture and adjust Tolerance rather than
// assuming a client build matches.
func DefaultColors() []Color {
	return []Color{
		{0x00, 0xBC, 0x00}, {0x50, 0xA1, 0x50}, {0xA1, 0xA1, 0x00},
		{0xBF, 0x0A, 0x0A}, {0x91, 0x0F, 0x0F}, {0x85, 0x0C, 0x0C},
	}
}

type Options struct {
	Geometry Geometry
	// Colors are the fill colours to look for and Tolerance the largest
	// difference allowed on any single channel.
	Colors    []Color
	Tolerance int
	// BlackMax is the highest value any channel may have and still count as
	// the bar's border or its unfilled background. Both are drawn black, which
	// is what lets one rule work at every health level.
	BlackMax int
	// Exclude lists top-left corners of bars that must never be reported: the
	// character's own, which sits at fixed pixels because the client's camera
	// is centred on the character. Matching on the exact position rather than
	// on nearness to the middle is deliberate - nearness would also swallow a
	// creature standing one tile away.
	Exclude          []image.Point
	ExcludeTolerance int
	// EdgeTolerance is how many pixels narrower than the core the first and
	// last inner rows may be. The client antialiases the edge of a bar, and
	// the antialiased row blends toward the dark background, so it measures
	// shorter - never longer. Zero, the default, demands the exact match the
	// detector always demanded.
	EdgeTolerance int
}

// Bar is one detected health bar, in the coordinates of the image it was found
// in. Fill is the width of the coloured part in pixels.
type Bar struct {
	X, Y int
	Fill int
}

// HP is how full the bar is, 0-1.
//
// Limitation: when another creature's bar overlaps this one's fill span, the
// coloured run is measured by Find only as far as that other bar's own black
// border, not as far as this bar's true interior black - so Fill, and
// therefore HP, reads low for the overlapped bar. The count of bars stays
// correct; only the fill fraction is affected. A consumer that reads HP must
// tolerate this.
func (b Bar) HP(g Geometry) float64 {
	if g.InnerWidth() <= 0 {
		return 0
	}
	return float64(b.Fill) / float64(g.InnerWidth())
}

// Find returns every health bar in the image, in reading order.
//
// A bar clipped by the edge of the image is not returned: its border is not
// there to confirm, and a half-measured fill would be a lie about the
// creature's health. That is why the panel cuts one tile more than the
// decision radius - a creature at the very edge of the radius still has its
// whole bar inside the crop.
func Find(im *image.NRGBA, o Options) []Bar {
	g := o.Geometry
	if im == nil || !g.valid() || len(o.Colors) == 0 {
		return nil
	}
	b := im.Bounds()
	var out []Bar
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			run := o.fillRun(im, x, y)
			if run == 0 {
				continue
			}
			bar := Bar{X: x - g.Border, Y: y - g.Border}
			fill, ok := o.confirm(im, bar)
			if !ok {
				continue
			}
			bar.Fill = fill
			if o.excluded(bar) {
				continue
			}
			out = append(out, bar)
		}
	}
	return out
}

func (o Options) isFill(im *image.NRGBA, x, y int) bool {
	if !(image.Point{X: x, Y: y}).In(im.Bounds()) {
		return false
	}
	c := im.NRGBAAt(x, y)
	for _, want := range o.Colors {
		if diff(c.R, want.R) <= o.Tolerance &&
			diff(c.G, want.G) <= o.Tolerance &&
			diff(c.B, want.B) <= o.Tolerance {
			return true
		}
	}
	return false
}

func (o Options) isDark(im *image.NRGBA, x, y int) bool {
	if !(image.Point{X: x, Y: y}).In(im.Bounds()) {
		return false
	}
	c := im.NRGBAAt(x, y)
	return int(c.R) <= o.BlackMax && int(c.G) <= o.BlackMax && int(c.B) <= o.BlackMax
}

// fillRun measures the coloured run beginning at (x, y). It answers zero
// unless the run is bounded by dark pixels on both sides and is no wider than
// the bar's inside. The right-hand bound is what rejects a large patch of the
// same colour somewhere else on screen: in a wide patch the pixel after the
// widest allowed run is still coloured, never dark.
func (o Options) fillRun(im *image.NRGBA, x, y int) int {
	if !o.isDark(im, x-1, y) || !o.isFill(im, x, y) {
		return 0
	}
	limit := o.Geometry.InnerWidth()
	n := 0
	for n < limit && o.isFill(im, x+n, y) {
		n++
	}
	if n == 0 || !o.isDark(im, x+n, y) {
		return 0
	}
	return n
}

// confirm re-measures the whole rectangle and returns the core fill width.
// The core is the run shared by the middle rows; the first and last inner
// rows may be up to EdgeTolerance pixels narrower (never wider), which is how
// the client's antialiased edge looks. With fewer than three inner rows there
// is no middle to trust, so every row must match exactly, EdgeTolerance or
// not. The border stays dark all the way round, unchanged - it is what still
// stops the same bar being found a second time one row down.
func (o Options) confirm(im *image.NRGBA, bar Bar) (int, bool) {
	g := o.Geometry
	whole := image.Rect(bar.X, bar.Y, bar.X+g.Width, bar.Y+g.Height)
	if !whole.In(im.Bounds()) {
		return 0, false
	}
	inner := g.InnerHeight()
	runs := make([]int, inner)
	for dy := 0; dy < inner; dy++ {
		r := o.fillRun(im, bar.X+g.Border, bar.Y+g.Border+dy)
		if r == 0 {
			return 0, false
		}
		runs[dy] = r
	}
	coreStart, coreEnd := 0, inner
	if inner >= 3 {
		coreStart, coreEnd = 1, inner-1
	}
	core := runs[coreStart]
	for i := coreStart; i < coreEnd; i++ {
		if runs[i] != core {
			return 0, false
		}
	}
	if inner >= 3 {
		for _, edge := range []int{runs[0], runs[inner-1]} {
			if d := core - edge; d < 0 || d > o.EdgeTolerance {
				return 0, false
			}
		}
	}
	for dy := 0; dy < g.Border; dy++ {
		for dx := 0; dx < g.Width; dx++ {
			if !o.isDark(im, bar.X+dx, bar.Y+dy) {
				return 0, false
			}
			if !o.isDark(im, bar.X+dx, bar.Y+g.Height-1-dy) {
				return 0, false
			}
		}
	}
	return core, true
}

func (o Options) excluded(bar Bar) bool {
	for _, p := range o.Exclude {
		if abs(bar.X-p.X) <= o.ExcludeTolerance && abs(bar.Y-p.Y) <= o.ExcludeTolerance {
			return true
		}
	}
	return false
}

func diff(a, b uint8) int { return abs(int(a) - int(b)) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
