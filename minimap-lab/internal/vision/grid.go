package vision

import "math"

// Grid turns a bar's pixels into a fractional offset in tiles from the
// character. It works entirely inside the game window and needs no world
// position: the client's camera is centred on the character, so the character
// always stands on the middle tile. That is what lets the count of creatures
// around the character survive a failed minimap match.
type Grid struct {
	// Cols and Rows are the tiles the whole game window shows - 15 by 11 in
	// the official client.
	Cols, Rows int
	// TileW and TileH are pixels per tile, derived from the game window
	// rectangle rather than calibrated separately.
	TileW, TileH float64
	// CropX and CropY place the cut-out the bars were found in inside the
	// whole game window.
	CropX, CropY float64
	// AnchorDX and AnchorDY say where the centre of a bar sits relative to the
	// centre of the tile its creature stands on. Do not guess them: derive
	// them with AnchorFrom, from the character's own bar.
	AnchorDX, AnchorDY float64
	// Geometry is the bar geometry the offsets are computed with.
	Geometry Geometry
}

func (g Grid) Valid() bool {
	return g.Cols > 0 && g.Rows > 0 && g.TileW > 0 && g.TileH > 0
}

// PlayerCentre is the middle tile's centre, in game-window pixels. The
// division is integer on purpose: 15 columns put the character on column 7,
// with seven columns either side.
func (g Grid) PlayerCentre() (x, y float64) {
	return (float64(g.Cols/2) + 0.5) * g.TileW, (float64(g.Rows/2) + 0.5) * g.TileH
}

// barCentre is one bar's centre in game-window pixels.
func (g Grid) barCentre(b Bar) (x, y float64) {
	return g.CropX + float64(b.X) + float64(g.Geometry.Width)/2,
		g.CropY + float64(b.Y) + float64(g.Geometry.Height)/2
}

// Offset is where the creature stands relative to the character, in tiles.
// It is deliberately fractional: creatures slide between tiles as they walk,
// and rounding on every frame would make a count on the edge of a radius
// flicker between two values.
func (g Grid) Offset(b Bar) (dx, dy float64) {
	if !g.Valid() {
		return 0, 0
	}
	cx, cy := g.barCentre(b)
	px, py := g.PlayerCentre()
	return (cx - g.AnchorDX - px) / g.TileW, (cy - g.AnchorDY - py) / g.TileH
}

// AnchorFrom derives the anchor from the character's own bar. That bar belongs
// to a creature standing on a tile we know exactly - the middle one - so its
// displacement from that tile's centre is the anchor itself. Measuring it any
// other way means eyeballing pixels.
func (g Grid) AnchorFrom(self Bar) (dx, dy float64) {
	cx, cy := g.barCentre(self)
	px, py := g.PlayerCentre()
	return cx - px, cy - py
}

// Distance is the Chebyshev distance in tiles, which is how this game's area
// spells reach: everything in the 3x3 around the character is at distance 1.
func Distance(dx, dy float64) float64 {
	return math.Max(math.Abs(dx), math.Abs(dy))
}
