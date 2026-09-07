// Package vision reads the health bars the game client draws above every
// creature. It is deliberately the only package that touches game-window
// pixels, and it holds neither state nor a clock: pixels in, bars out.
// Everything that needs memory of what was seen on an earlier frame lives in
// internal/combat.
package vision

import (
	"image"
	"image/color"
)

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
}

// Bar is one detected health bar, in the coordinates of the image it was found
// in. Fill is the width of the coloured part in pixels.
type Bar struct {
	X, Y int
	Fill int
}

// HP is how full the bar is, 0-1.
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
	// claimed marks pixels already accounted for by a bar, so one bar is not
	// reported once per row of its fill.
	claimed := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if claimed.AlphaAt(x, y).A != 0 {
				continue
			}
			run := o.fillRun(im, x, y)
			if run == 0 {
				continue
			}
			bar := Bar{X: x - g.Border, Y: y - g.Border, Fill: run}
			if !o.confirm(im, bar, run) {
				continue
			}
			whole := image.Rect(bar.X, bar.Y, bar.X+g.Width, bar.Y+g.Height)
			for cy := whole.Min.Y; cy < whole.Max.Y; cy++ {
				for cx := whole.Min.X; cx < whole.Max.X; cx++ {
					claimed.SetAlpha(cx, cy, color.Alpha{A: 255})
				}
			}
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
	max := o.Geometry.InnerWidth()
	n := 0
	for n < max && o.isFill(im, x+n, y) {
		n++
	}
	if n == 0 || !o.isDark(im, x+n, y) {
		return 0
	}
	return n
}

// confirm checks the whole rectangle: every inner row carries the same run,
// and the border is dark all the way round.
func (o Options) confirm(im *image.NRGBA, bar Bar, run int) bool {
	g := o.Geometry
	whole := image.Rect(bar.X, bar.Y, bar.X+g.Width, bar.Y+g.Height)
	if !whole.In(im.Bounds()) {
		return false
	}
	for dy := 0; dy < g.InnerHeight(); dy++ {
		if o.fillRun(im, bar.X+g.Border, bar.Y+g.Border+dy) != run {
			return false
		}
	}
	for dy := 0; dy < g.Border; dy++ {
		for dx := 0; dx < g.Width; dx++ {
			if !o.isDark(im, bar.X+dx, bar.Y+dy) {
				return false
			}
			if !o.isDark(im, bar.X+dx, bar.Y+g.Height-1-dy) {
				return false
			}
		}
	}
	return true
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
