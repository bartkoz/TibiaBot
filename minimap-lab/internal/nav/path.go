package nav

import (
	"container/heap"
	"context"
	"math"
)

// PathResult is the answer to one route query. Found is false for ordinary
// situations - blocked endpoints, no route, search limits - which carry an
// explanation rather than an error.
type PathResult struct {
	Found bool `json:"found"`
	// Status is the machine-readable outcome: ok, blocked_start, blocked_goal,
	// no_route, limit or cancelled. Reason carries the same thing for a human.
	Status    string   `json:"status"`
	Steps     [][2]int `json:"steps"`
	Tiles     int      `json:"tiles"`
	Cost      float64  `json:"cost"`
	Reason    string   `json:"reason"`
	ElapsedMS float64  `json:"elapsed_ms"`
	// OverlayRevision lets the panel tell a route computed before its latest
	// observation from one computed after it.
	OverlayRevision uint64 `json:"overlay_revision"`
	// GoalMoved says the route aims at a tile next to the waypoint, because the
	// waypoint itself cannot be stood on. The panel says so rather than letting
	// the route quietly end somewhere else.
	GoalMoved bool `json:"goal_moved,omitempty"`
}

type step struct {
	dx, dy int
	weight float64
}

// diagonalWeight is what one diagonal step costs in units of a straight one.
// Geometry would say sqrt(2), but the bot does not spend distance - it spends
// time, and the game takes three times as long over a diagonal as over a
// straight step. At sqrt(2) the search crossed open ground on diagonals, which
// is the shortest route in tiles and the slowest one in practice; at 3 a
// diagonal only wins where it genuinely saves more than the two straight steps
// it replaces.
const diagonalWeight = 3

var steps = []step{
	{0, -1, 1}, {0, 1, 1}, {-1, 0, 1}, {1, 0, 1},
	{-1, -1, diagonalWeight}, {1, -1, diagonalWeight}, {-1, 1, diagonalWeight}, {1, 1, diagonalWeight},
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// octile estimates the remaining cost the same way the steps above charge it:
// every tile of overlap between the axes can be taken as one diagonal instead
// of two straight steps, saving 2 - diagonalWeight each time. With
// diagonalWeight at 3 that saving is negative, so the estimate reduces to the
// Manhattan distance - which is exactly right when diagonals never pay off on
// open ground, and stays admissible either way.
func octile(ax, ay, bx, by int) float64 {
	dx, dy := float64(abs(bx-ax)), float64(abs(by-ay))
	// Each tile of overlap between the axes could be taken as one diagonal
	// instead of two straight steps, saving 2-diagonalWeight. Above a weight of
	// two there is no saving, and the estimate is simply the Manhattan
	// distance. Written without a branch so the formula stays correct - and
	// admissible - if the weight is ever changed.
	return dx + dy - math.Max(0, 2-diagonalWeight)*math.Min(dx, dy)
}

// FindPath runs A* over the cost grid. The returned steps include the starting
// tile, so Tiles is one less than their count.
func FindPath(ctx context.Context, grid *PathGrid, from, to [2]int, maxIterations int) PathResult {
	if grid.Blocked(from[0], from[1]) {
		return PathResult{Status: "blocked_start", Reason: "Pozycja startowa jest nieprzechodnia lub poza wczytanym obszarem."}
	}
	if grid.Blocked(to[0], to[1]) {
		return PathResult{Status: "blocked_goal", Reason: "Waypoint leży na kratce nieprzechodniej lub poza wczytanym obszarem."}
	}
	if from == to {
		return PathResult{Found: true, Status: "ok", Steps: [][2]int{from}}
	}
	// Ground can cost less than one unit per step, so the straight-line estimate
	// is scaled by the cheapest tile around; otherwise it overestimates and A*
	// stops returning the cheapest route.
	floor := grid.Cheapest()
	estimate := func(x, y int) float64 { return floor * octile(x, y, to[0], to[1]) }
	open := &pathQueue{}
	heap.Init(open)
	heap.Push(open, &pathNode{at: from, f: estimate(from[0], from[1])})
	best := map[[2]int]float64{from: 0}
	cameFrom := map[[2]int][2]int{}
	closed := map[[2]int]bool{}
	// The budget counts expanded tiles, not heap pops: a tile can sit in the
	// queue several times, and those duplicates are not search progress.
	for expanded := 0; open.Len() > 0; {
		if err := ctx.Err(); err != nil {
			return PathResult{Status: "cancelled", Reason: "Wyszukiwanie trasy przerwane: " + err.Error()}
		}
		cur := heap.Pop(open).(*pathNode)
		if cur.at == to {
			return buildPath(cameFrom, from, to, best[to])
		}
		if closed[cur.at] {
			continue
		}
		if expanded >= maxIterations {
			return PathResult{Status: "limit", Reason: "Przekroczono limit kroków wyszukiwania; zwiększ margines lub dodaj waypoint pośredni."}
		}
		expanded++
		closed[cur.at] = true
		for _, s := range steps {
			next := [2]int{cur.at[0] + s.dx, cur.at[1] + s.dy}
			if closed[next] {
				continue
			}
			if grid.Blocked(next[0], next[1]) {
				continue
			}
			// An edge learned to fail is refused on its own: the target tile may
			// be perfectly walkable from another side.
			if grid.EdgeBlocked(cur.at[0], cur.at[1], next[0], next[1]) {
				continue
			}
			// A diagonal step through a closed corner is impossible in game:
			// walking alongside one wall is fine, squeezing between two is not.
			if s.dx != 0 && s.dy != 0 &&
				grid.ClosesCorner(cur.at[0]+s.dx, cur.at[1]) &&
				grid.ClosesCorner(cur.at[0], cur.at[1]+s.dy) {
				continue
			}
			g := best[cur.at] + s.weight*grid.Cost(next[0], next[1])/100
			if seen, ok := best[next]; ok && seen <= g {
				continue
			}
			best[next] = g
			cameFrom[next] = cur.at
			heap.Push(open, &pathNode{at: next, f: g + estimate(next[0], next[1])})
		}
	}
	return PathResult{Status: "no_route", Reason: "Brak trasy w zadanym obszarze."}
}

func buildPath(cameFrom map[[2]int][2]int, from, to [2]int, cost float64) PathResult {
	path := [][2]int{to}
	for at := to; at != from; {
		at = cameFrom[at]
		path = append(path, at)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return PathResult{Found: true, Status: "ok", Steps: path, Tiles: len(path) - 1, Cost: cost}
}

type pathNode struct {
	at    [2]int
	f     float64
	index int
}

type pathQueue []*pathNode

func (q pathQueue) Len() int           { return len(q) }
func (q pathQueue) Less(i, j int) bool { return q[i].f < q[j].f }
func (q pathQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i]; q[i].index = i; q[j].index = j }
func (q *pathQueue) Push(x any)        { n := x.(*pathNode); n.index = len(*q); *q = append(*q, n) }
func (q *pathQueue) Pop() any          { old := *q; n := old[len(old)-1]; *q = old[:len(old)-1]; return n }
