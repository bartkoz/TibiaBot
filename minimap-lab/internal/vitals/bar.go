// Package vitals reads one of the client's own bars - health or mana - as a
// percentage.
//
// A pixel counts as filled by saturation and brightness, never by hue. The
// health bar changes colour as it empties, green through yellow to red, so
// matching a colour would stop working exactly when the reading matters most.
package vitals

import (
	"fmt"
	"image"
	"sort"
)

type Options struct {
	// Rows are the image rows to sample; empty means every row. The answer is
	// their median, so one row whose fill boundary differs from the rest -
	// antialiasing, a rounded end cap, a border row - cannot swing the
	// reading. A row with anything filled past its own prefix is a different
	// case: the prefix rule in Read discards it outright, before the median
	// ever sees it, rather than letting the median out-vote it.
	Rows []int
	// MinSaturation and MinValue are what a pixel must clear to count as
	// filled, both 0-1.
	MinSaturation float64
	MinValue      float64
	// MinValidRows is the fraction of sampled rows that must produce a clean
	// prefix before the reading is trusted at all.
	MinValidRows float64
}

func DefaultOptions() Options {
	return Options{MinSaturation: 0.35, MinValue: 0.25, MinValidRows: 0.5}
}

type Reading struct {
	Percent float64
	OK      bool
	Reason  string
}

// Read measures the filled prefix of the bar.
//
// The filled pixels must form a contiguous run from the left edge, and a row
// with anything filled after that run is thrown away. This is the guard
// against a calibration that has slipped off the bar: such a rectangle
// produces scattered matches, and reporting those as a low percentage would
// make a healing rule fire forever.
//
// That guard cannot catch every kind of slip, though: a rectangle that has
// slid entirely onto black reads as a clean, fully-empty bar - zero filled
// pixels is a contiguous prefix of length zero, same as a genuinely empty
// bar. Pixels alone cannot tell the two apart, and a Reading of {Percent: 0,
// OK: true} is reported for both. Zero mana is an ordinary game state, so
// this is not treated as an error: a caller that acts on a 0% reading (for
// example, gating a spell on a mana threshold) must tolerate that ambiguity.
//
// Percent is the filled median divided by the image width, so the rectangle
// passed in must be the bar and nothing but the bar. A calibrated rectangle
// wider than the real bar understates the reading proportionally - a 120px
// rectangle over a 100px bar that is 60% full reports 50%, not 60% - because
// the extra dark pixels count toward the width but never toward the fill.
func Read(im *image.NRGBA, o Options) Reading {
	if im == nil || im.Bounds().Empty() {
		return Reading{Reason: "brak obrazu paska"}
	}
	b := im.Bounds()
	rows := o.Rows
	if len(rows) == 0 {
		rows = make([]int, 0, b.Dy())
		for y := b.Min.Y; y < b.Max.Y; y++ {
			rows = append(rows, y)
		}
	}
	var prefixes []int
	sampled := 0
	for _, y := range rows {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		sampled++
		if n, ok := o.prefix(im, y); ok {
			prefixes = append(prefixes, n)
		}
	}
	if sampled == 0 {
		return Reading{Reason: "żaden z wybranych wierszy nie leży w obrazie paska"}
	}
	need := o.MinValidRows * float64(sampled)
	if float64(len(prefixes)) < need {
		return Reading{Reason: fmt.Sprintf(
			"tylko %d z %d wierszy ma ciągłe wypełnienie — kalibracja paska najpewniej się rozjechała",
			len(prefixes), sampled)}
	}
	sort.Ints(prefixes)
	median := prefixes[len(prefixes)/2]
	return Reading{Percent: float64(median) / float64(b.Dx()), OK: true}
}

// prefix counts the filled pixels at the start of one row, and rejects the row
// if anything further right is filled too.
func (o Options) prefix(im *image.NRGBA, y int) (int, bool) {
	b := im.Bounds()
	n := 0
	for x := b.Min.X; x < b.Max.X && o.filled(im, x, y); x++ {
		n++
	}
	for x := b.Min.X + n + 1; x < b.Max.X; x++ {
		if o.filled(im, x, y) {
			return 0, false
		}
	}
	return n, true
}

// filled reads only R, G and B and ignores alpha entirely. That is safe as
// long as the frame came from an opaque source - a browser canvas capturing
// the screen, the only producer these images have - because a non-
// premultiplied NRGBA pixel can otherwise carry an arbitrary colour behind
// A == 0.
func (o Options) filled(im *image.NRGBA, x, y int) bool {
	c := im.NRGBAAt(x, y)
	hi, lo := int(c.R), int(c.R)
	for _, v := range []int{int(c.G), int(c.B)} {
		if v > hi {
			hi = v
		}
		if v < lo {
			lo = v
		}
	}
	if hi == 0 {
		return false
	}
	value := float64(hi) / 255
	saturation := float64(hi-lo) / float64(hi)
	return value >= o.MinValue && saturation >= o.MinSaturation
}
