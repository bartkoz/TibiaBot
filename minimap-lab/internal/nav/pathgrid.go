package nav

import (
	"minimap-lab/internal/mapdata"
)

// PathGrid is what the search walks on: the immutable terrain costs plus a
// snapshot of the learned blockages. The snapshot is taken before the search
// starts, because A* assumes the cost of a closed vertex never changes.
//
// The base grid is never written to. CostGrid.LimitTo copies the struct but
// shares the pixel slice with the cached floor, so writing a block into it
// would poison every later query.
type PathGrid struct {
	base    *mapdata.CostGrid
	overlay *Overlay
}

func NewPathGrid(base *mapdata.CostGrid, o *Overlay) *PathGrid {
	return &PathGrid{base: base, overlay: o}
}

func (g *PathGrid) Blocked(x, y int) bool {
	if g.base.At(x, y) == mapdata.BlockedCost {
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

// nearestWalkableRadius is how far a goal may be nudged. Two tiles covers the
// tracker drifting by a tile or two - the common case behind a waypoint that
// sits in a wall. Anything further means the waypoint was recorded from a
// position the locator got wrong outright, and guessing a destination for it
// would send the bot somewhere nobody asked for.
const nearestWalkableRadius = 2

// NearestWalkable finds a tile the bot can actually stand on, closest to the
// one asked for. A waypoint recorded a tile off a wall is the normal case; the
// user meant the doorway, not the door frame.
func (g *PathGrid) NearestWalkable(x, y int) (int, int, bool) {
	if !g.Blocked(x, y) {
		return x, y, true
	}
	for r := 1; r <= nearestWalkableRadius; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				// Only the ring at distance r; the inside was covered already.
				if abs(dx) != r && abs(dy) != r {
					continue
				}
				if !g.Blocked(x+dx, y+dy) {
					return x + dx, y + dy, true
				}
			}
		}
	}
	return x, y, false
}

// GoalRefusal explains why a tile cannot be a destination. The three causes
// need three different reactions from the user, so one message for all of them
// is no help at all.
func (g *PathGrid) GoalRefusal(x, y int) string {
	switch {
	case !g.base.Covered(x, y):
		return "Waypoint leży poza obszarem, dla którego są dane mapy (brak danych). Dograj kafle minimapy albo popraw punkt."
	case g.overlay.Tile(x, y) == KindPerm:
		return "Waypoint leży na kratce nauczonej jako nieprzechodnia. Skasuj tę blokadę w podglądzie przechodności, jeśli jest błędna."
	default:
		return "Waypoint leży na kratce nieprzechodniej według danych mapy - i nic w promieniu dwóch kratek nie jest przechodnie. Punkt prawdopodobnie zapisał się z błędnie odczytanej pozycji; nagraj go ponownie."
	}
}

func (g *PathGrid) EdgeBlocked(fx, fy, tx, ty int) bool {
	return g.overlay.Edge(fx, fy, tx, ty)
}

// Cheapest bounds the per-tile cost from below so the octile estimate stays
// admissible. The penalty only ever raises a cost, so the base minimum still
// bounds the overlay grid.
func (g *PathGrid) Cheapest() float64 {
	cheapest, walkable := g.base.CheapestWalkable()
	if !walkable {
		return 1
	}
	if floor := float64(cheapest) / 100; floor <= 1 {
		return floor
	}
	return 1
}
