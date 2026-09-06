package main

// PathGrid is what the search walks on: the immutable terrain costs plus a
// snapshot of the learned blockages. The snapshot is taken before the search
// starts, because A* assumes the cost of a closed vertex never changes.
//
// The base grid is never written to. CostGrid.limitTo copies the struct but
// shares the pixel slice with the cached floor, so writing a block into it
// would poison every later query.
type PathGrid struct {
	base    *CostGrid
	overlay *Overlay
}

func NewPathGrid(base *CostGrid, o *Overlay) *PathGrid {
	return &PathGrid{base: base, overlay: o}
}

func (g *PathGrid) Blocked(x, y int) bool {
	if g.base.At(x, y) == blockedCost {
		return true
	}
	return g.overlay.Tile(x, y) == KindPerm
}

// Cost is only meaningful where Blocked is false.
func (g *PathGrid) Cost(x, y int) float64 {
	c := float64(g.base.At(x, y))
	if g.overlay.Tile(x, y) == KindTemp {
		c += tempPenalty
	}
	return c
}

// ClosesCorner is stricter than Blocked: a creature or a piece of furniture at
// a corner makes the game refuse the diagonal squeeze exactly as a wall does,
// even though a fresh block is only a cost penalty when walked onto directly.
// Letting the search squeeze past one would have the bot emit a step the game
// rejects, then blame the edge for it.
func (g *PathGrid) ClosesCorner(x, y int) bool {
	return g.Blocked(x, y) || g.overlay.Tile(x, y) != KindNone
}

func (g *PathGrid) EdgeBlocked(fx, fy, tx, ty int) bool {
	return g.overlay.Edge(fx, fy, tx, ty)
}

// Cheapest bounds the per-tile cost from below so the octile estimate stays
// admissible. The penalty only ever raises a cost, so the base minimum still
// bounds the overlay grid.
func (g *PathGrid) Cheapest() float64 {
	if !g.base.walkable {
		return 1
	}
	if floor := float64(g.base.cheapest) / 100; floor <= 1 {
		return floor
	}
	return 1
}
