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
	// EdgeTolerance is forwarded to vision.Find: the client antialiases the
	// battle-list bar edges too.
	EdgeTolerance int
	// IconOffsetX/Y place the creature icon's top-left corner relative to the
	// bar's, and IconSize is the icon's side. The client draws the attack
	// frame round the icon, not round the row, so that square is where the
	// frame is looked for.
	IconOffsetX, IconOffsetY, IconSize int
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
		Tolerance: o.Tolerance, BlackMax: o.BlackMax, EdgeTolerance: o.EdgeTolerance,
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

// framed looks for the attack border in the square around this bar's creature
// icon. The client draws the frame round the icon, which sits at a fixed
// offset from the bar, so a run of the frame colour covering FrameCoverage of
// the icon's width on any one of its rows means this entry is the target.
// Icon squares of adjacent rows do not overlap, so at most one row is framed.
func (o Options) framed(im *image.NRGBA, b vision.Bar) bool {
	if o.IconSize < 1 {
		return false
	}
	want := int(o.FrameCoverage * float64(o.IconSize))
	if want < 1 {
		want = 1
	}
	x0 := b.X + o.IconOffsetX
	y0 := b.Y + o.IconOffsetY
	for y := y0; y < y0+o.IconSize; y++ {
		if o.frameRun(im, y, x0, x0+o.IconSize) >= want {
			return true
		}
	}
	return false
}

// frameRun is the longest unbroken run of the frame colour on one line, within
// the given x range.
func (o Options) frameRun(im *image.NRGBA, y, x0, x1 int) int {
	b := im.Bounds()
	if y < b.Min.Y || y >= b.Max.Y {
		return 0
	}
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	best, run := 0, 0
	for x := x0; x < x1; x++ {
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
