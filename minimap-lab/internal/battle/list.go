// Package battle reads the client's battle list: how many creatures it shows
// and which entry carries the attack frame.
//
// The frame matters more than it looks. Clicking the entry already being
// attacked cancels the attack, so a bot that could not see the frame would
// toggle its own target on and off once per frame and never kill anything.
package battle

import (
	"image"

	"minimap-lab/internal/vision"
)

type Options struct {
	// Geometry is the small health bar drawn inside one entry. It is a
	// different size from the bars above creatures but exactly the same shape,
	// so the same detector reads both. It is not called Bar because Row.Bar
	// already means a detected bar, and one package with two meanings of Bar
	// is one too many.
	Geometry  vision.Geometry
	Colors    []vision.Color
	Tolerance int
	BlackMax  int
	// RowPitch is the vertical distance between two entries. It bounds where
	// the attack frame is looked for, and how close to the bottom edge the
	// last entry has to be for the list to count as scrolled.
	RowPitch int
	// Frame is the colour of the border the client draws round the entry being
	// attacked, with its own tolerance because it is not one of the health
	// colours.
	Frame          vision.Color
	FrameTolerance int
	// FrameCoverage is the fraction of the crop's width the frame colour must
	// cover on a single line to count as the frame rather than as some
	// coloured pixel that happens to match.
	FrameCoverage float64
}

// Row is one entry. Bar's centre is where a click on this entry goes - the
// health bar is part of the entry, so clicking it selects the creature, and
// unlike a click in the game window a click here can never move the
// character.
type Row struct {
	Bar      vision.Bar
	HP       float64
	Targeted bool
}

type List struct {
	Rows []Row
	// Truncated is true when the last entry sits against the bottom edge, so
	// the list is scrolled and the count is a floor rather than a total.
	Truncated bool
}

// Read answers what the list shows. Rows come out top to bottom because
// vision.Find scans row by row, so its output is already sorted by Y.
func Read(im *image.NRGBA, o Options) List {
	if im == nil || o.RowPitch < 1 {
		return List{}
	}
	bars := vision.Find(im, vision.Options{
		Geometry: o.Geometry, Colors: o.Colors,
		Tolerance: o.Tolerance, BlackMax: o.BlackMax,
	})
	var out List
	for _, b := range bars {
		out.Rows = append(out.Rows, Row{
			Bar: b, HP: b.HP(o.Geometry), Targeted: o.framed(im, b),
		})
	}
	if n := len(bars); n > 0 && bars[n-1].Y+o.RowPitch >= im.Bounds().Max.Y {
		out.Truncated = true
	}
	return out
}

// framed looks for the attack border in the band one entry tall around the
// bar - the band, not the bar's own rows, because the client draws the frame
// round the whole entry and the entry is taller than its health bar. The band
// is centred on the bar and half-open at the top, so consecutive entries tile
// exactly instead of sharing a strip. An overlapping band would mark two
// entries as the target at once - and since clicking the entry already under
// attack cancels the attack, a target the bot only thinks it has is as costly
// as one it fails to see.
func (o Options) framed(im *image.NRGBA, b vision.Bar) bool {
	want := int(o.FrameCoverage * float64(im.Bounds().Dx()))
	if want < 1 {
		want = 1
	}
	top := b.Y + o.Geometry.Height/2 - o.RowPitch/2
	for y := top; y < top+o.RowPitch; y++ {
		if o.frameRun(im, y) >= want {
			return true
		}
	}
	return false
}

// frameRun is the longest unbroken run of the frame colour on one line.
func (o Options) frameRun(im *image.NRGBA, y int) int {
	b := im.Bounds()
	if y < b.Min.Y || y >= b.Max.Y {
		return 0
	}
	best, run := 0, 0
	for x := b.Min.X; x < b.Max.X; x++ {
		c := im.NRGBAAt(x, y)
		if near(c.R, o.Frame.R, o.FrameTolerance) &&
			near(c.G, o.Frame.G, o.FrameTolerance) &&
			near(c.B, o.Frame.B, o.FrameTolerance) {
			run++
			if run > best {
				best = run
			}
			continue
		}
		run = 0
	}
	return best
}

func near(a, b uint8, tol int) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	return d <= tol
}
