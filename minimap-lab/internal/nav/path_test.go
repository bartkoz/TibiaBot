package nav

import (
	"context"
	"image"
	"math/rand"
	"strings"
	"testing"

	"minimap-lab/internal/mapdata"
)

// gridFrom builds a mapdata.CostGrid from ASCII rows anchored at world (1000,1000).
// '.' is normal ground (100), '-' is cheap ground (90), '~' is costly ground
// (200), '#' is blocked.
func gridFrom(rows []string) *mapdata.CostGrid {
	bounds := image.Rect(1000, 1000, 1000+len(rows[0]), 1000+len(rows))
	pix := make([]uint8, bounds.Dx()*bounds.Dy())
	for y, row := range rows {
		for x, c := range row {
			var v uint8
			switch c {
			case '#':
				v = mapdata.BlockedCost
			case '-':
				v = 90
			case '~':
				v = 200
			default:
				v = 100
			}
			pix[y*bounds.Dx()+x] = v
		}
	}
	return mapdata.NewCostGrid(pix, bounds)
}

func at(x, y int) [2]int { return [2]int{1000 + x, 1000 + y} }

func assertWalk(t *testing.T, r PathResult, from, to [2]int) {
	t.Helper()
	if !r.Found {
		t.Fatalf("no path: %s", r.Reason)
	}
	if r.Steps[0] != from {
		t.Fatalf("path starts at %v, want %v", r.Steps[0], from)
	}
	if r.Steps[len(r.Steps)-1] != to {
		t.Fatalf("path ends at %v, want %v", r.Steps[len(r.Steps)-1], to)
	}
	for i := 1; i < len(r.Steps); i++ {
		dx, dy := abs(r.Steps[i][0]-r.Steps[i-1][0]), abs(r.Steps[i][1]-r.Steps[i-1][1])
		if dx > 1 || dy > 1 || (dx == 0 && dy == 0) {
			t.Fatalf("step %d jumps from %v to %v", i, r.Steps[i-1], r.Steps[i])
		}
	}
	if r.Tiles != len(r.Steps)-1 {
		t.Fatalf("tiles %d does not match %d steps", r.Tiles, len(r.Steps))
	}
}

func TestFindPathCrossesOpenGroundOnStraightSteps(t *testing.T) {
	g := gridFrom([]string{
		".....",
		".....",
		".....",
	})

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(0, 0), at(3, 2), 10000)

	assertWalk(t, r, at(0, 0), at(3, 2))
	// Five straight steps cost 5; the diagonal shortcut would reach the same
	// tile in three steps but cost 7, because a diagonal takes three times as
	// long in game. The bot spends time, not tiles.
	if r.Tiles != 5 {
		t.Errorf("tiles: got %d, want 5", r.Tiles)
	}
	if r.Cost != 5 {
		t.Errorf("cost: got %v, want 5", r.Cost)
	}
}

func TestFindPathRoundsAWall(t *testing.T) {
	g := gridFrom([]string{
		"..#..",
		"..#..",
		"..#..",
		".....",
	})

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(0, 0), at(4, 0), 10000)

	assertWalk(t, r, at(0, 0), at(4, 0))
	for _, s := range r.Steps {
		if g.At(s[0], s[1]) == mapdata.BlockedCost {
			t.Fatalf("path crosses blocked tile %v", s)
		}
	}
}

func TestFindPathReportsUnreachableGoal(t *testing.T) {
	g := gridFrom([]string{
		".....",
		".###.",
		".#..#",
		".###.",
		".....",
	})

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(0, 0), at(2, 2), 10000)

	if r.Found || r.Status != "no_route" {
		t.Fatalf("expected no_route, got %+v", r)
	}
	if r.Reason == "" {
		t.Error("unreachable goal needs a reason")
	}
}

func TestFindPathRefusesToCutAClosedCorner(t *testing.T) {
	// Reaching (0,1) from (1,0) would mean slipping between two walls.
	g := gridFrom([]string{
		".#",
		"#.",
	})

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(1, 0), at(0, 1), 10000)

	if r.Found {
		t.Fatalf("expected no path through a closed corner, got %v", r.Steps)
	}
}

func TestFindPathWalksDiagonallyAlongASingleWall(t *testing.T) {
	// Only one of the two adjacent tiles is a wall, so the diagonal is legal.
	g := gridFrom([]string{
		".#.",
		"...",
		"...",
	})

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(0, 0), at(2, 1), 10000)

	assertWalk(t, r, at(0, 0), at(2, 1))
}

func TestFindPathPrefersCheapGroundOverTheDirectLine(t *testing.T) {
	// Both routes take four steps; the lower one crosses cheaper tiles.
	g := gridFrom([]string{
		".~~~.",
		".....",
	})

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(0, 0), at(4, 0), 10000)

	assertWalk(t, r, at(0, 0), at(4, 0))
	for _, s := range r.Steps {
		if g.At(s[0], s[1]) == 200 {
			t.Fatalf("path crosses costly tile %v: %v", s, r.Steps)
		}
	}
}

func TestFindPathRejectsBlockedStart(t *testing.T) {
	g := gridFrom([]string{"#.."})

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(0, 0), at(2, 0), 10000)

	if r.Found || r.Status != "blocked_start" || r.Reason == "" {
		t.Fatalf("blocked start must be reported: %+v", r)
	}
}

func TestFindPathRejectsBlockedGoal(t *testing.T) {
	g := gridFrom([]string{"..#"})

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(0, 0), at(2, 0), 10000)

	if r.Found || r.Status != "blocked_goal" || r.Reason == "" {
		t.Fatalf("blocked goal must be reported: %+v", r)
	}
}

func TestFindPathStopsAtTheIterationLimit(t *testing.T) {
	rows := make([]string, 40)
	for i := range rows {
		rows[i] = "........................................"
	}
	g := gridFrom(rows)

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(0, 0), at(39, 39), 5)

	if r.Found || r.Status != "limit" {
		t.Fatalf("expected the search to stop at the limit: %+v", r)
	}
	if r.Reason == "" {
		t.Error("hitting the limit needs a reason")
	}
}

func TestFindPathHonoursACancelledContext(t *testing.T) {
	rows := make([]string, 40)
	for i := range rows {
		rows[i] = "........................................"
	}
	g := gridFrom(rows)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := FindPath(ctx, NewPathGrid(g, nil), at(0, 0), at(39, 39), 100000)

	if r.Found || r.Status != "cancelled" || r.Reason == "" {
		t.Fatalf("cancelled search must be reported: %+v", r)
	}
}

func TestFindPathReturnsTheStartingTileWhenAlreadyThere(t *testing.T) {
	g := gridFrom([]string{"..."})

	r := FindPath(context.Background(), NewPathGrid(g, nil), at(1, 0), at(1, 0), 10000)

	if !r.Found || r.Status != "ok" || len(r.Steps) != 1 || r.Steps[0] != at(1, 0) || r.Tiles != 0 {
		t.Fatalf("standing on the goal: %+v", r)
	}
}

// dijkstra is a deliberately dumb reference implementation: no heuristic, so
// no way for one to be inadmissible.
func dijkstra(g *mapdata.CostGrid, from, to [2]int) float64 {
	best := map[[2]int]float64{from: 0}
	queue := [][2]int{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, s := range steps {
			next := [2]int{cur[0] + s.dx, cur[1] + s.dy}
			cost := g.At(next[0], next[1])
			if cost == mapdata.BlockedCost {
				continue
			}
			if s.dx != 0 && s.dy != 0 && g.At(cur[0]+s.dx, cur[1]) == mapdata.BlockedCost && g.At(cur[0], cur[1]+s.dy) == mapdata.BlockedCost {
				continue
			}
			candidate := best[cur] + s.weight*float64(cost)/100
			if seen, ok := best[next]; !ok || candidate < seen-1e-9 {
				best[next] = candidate
				queue = append(queue, next)
			}
		}
	}
	return best[to]
}

func TestFindPathIsOptimalOnGroundCheaperThanOneStep(t *testing.T) {
	// Real tiles go down to 90, so a step can cost less than 1.0. A heuristic
	// assuming 1.0 per step overestimates and stops A* being optimal.
	rng := rand.New(rand.NewSource(7))
	terrain := []byte("--..~#")
	for attempt := 0; attempt < 200; attempt++ {
		rows := make([]string, 9)
		for y := range rows {
			row := make([]byte, 9)
			for x := range row {
				row[x] = terrain[rng.Intn(len(terrain))]
			}
			rows[y] = string(row)
		}
		g := gridFrom(rows)
		from, to := at(0, 0), at(8, 8)
		if g.At(from[0], from[1]) == mapdata.BlockedCost || g.At(to[0], to[1]) == mapdata.BlockedCost {
			continue
		}
		r := FindPath(context.Background(), NewPathGrid(g, nil), from, to, 100000)
		want := dijkstra(g, from, to)
		if !r.Found {
			if want != 0 {
				t.Fatalf("attempt %d: A* found nothing but a route of %.6f exists\n%s", attempt, want, strings.Join(rows, "\n"))
			}
			continue
		}
		if r.Cost > want+1e-9 {
			t.Fatalf("attempt %d: A* returned %.9f, optimal is %.9f\n%s", attempt, r.Cost, want, strings.Join(rows, "\n"))
		}
	}
}

func TestFindPathBudgetCountsExpansionsNotHeapPops(t *testing.T) {
	// The budget callers pass is the number of tiles in the search area, so it
	// must count tiles actually expanded. Stale duplicate heap entries would
	// burn it and reject reachable routes.
	rng := rand.New(rand.NewSource(11))
	terrain := []byte("--..~#")
	for attempt := 0; attempt < 2000; attempt++ {
		rows := make([]string, 9)
		for y := range rows {
			row := make([]byte, 9)
			for x := range row {
				row[x] = terrain[rng.Intn(len(terrain))]
			}
			rows[y] = string(row)
		}
		g := gridFrom(rows)
		from, to := at(1, 1), at(7, 7)
		if g.At(from[0], from[1]) == mapdata.BlockedCost || g.At(to[0], to[1]) == mapdata.BlockedCost {
			continue
		}
		r := FindPath(context.Background(), NewPathGrid(g, nil), from, to, g.Area().Dx()*g.Area().Dy())
		if r.Status == "limit" && dijkstra(g, from, to) > 0 {
			t.Fatalf("attempt %d: reachable route refused with an area-sized budget\n%s", attempt, strings.Join(rows, "\n"))
		}
	}
}

// overlayOf builds an Overlay for the ASCII grids used across these tests,
// where 'T' marks a temporary block and 'P' a permanent one.
func overlayOf(rows []string) *Overlay {
	o := &Overlay{tiles: map[[2]int]Kind{}, edges: map[[4]int]bool{}}
	for y, row := range rows {
		for x, c := range row {
			switch c {
			case 'T':
				o.tiles[[2]int{1000 + x, 1000 + y}] = KindTemp
			case 'P':
				o.tiles[[2]int{1000 + x, 1000 + y}] = KindPerm
			}
		}
	}
	return o
}

func TestPermanentBlockIsImpassable(t *testing.T) {
	g := NewPathGrid(gridFrom([]string{"....", "....", "...."}), overlayOf([]string{
		"....",
		".P..",
		"....",
	}))
	r := FindPath(context.Background(), g, at(1, 0), at(1, 2), 1000)
	assertWalk(t, r, at(1, 0), at(1, 2))
	for _, s := range r.Steps {
		if s == at(1, 1) {
			t.Fatal("route runs through a permanent block")
		}
	}
}

func TestTemporaryBlockIsAvoidedWhenThereIsAWayAround(t *testing.T) {
	g := NewPathGrid(gridFrom([]string{"....", "....", "...."}), overlayOf([]string{
		"....",
		".T..",
		"....",
	}))
	r := FindPath(context.Background(), g, at(1, 0), at(1, 2), 1000)
	assertWalk(t, r, at(1, 0), at(1, 2))
	for _, s := range r.Steps {
		if s == at(1, 1) {
			t.Fatal("route runs through a temporary block even though a detour exists")
		}
	}
}

func TestTemporaryBlockStillPassableWhenThereIsNoWayAround(t *testing.T) {
	// A one-tile doorway in a wall. A player standing in it must not turn the
	// route into "no route" - the bot should wait and retry, not give up.
	g := NewPathGrid(gridFrom([]string{
		"#.#",
		"#.#",
		"#.#",
	}), overlayOf([]string{
		"...",
		".T.",
		"...",
	}))
	r := FindPath(context.Background(), g, at(1, 0), at(1, 2), 1000)
	if !r.Found {
		t.Fatalf("status %s (%s); a temporary block must be a penalty, not a wall", r.Status, r.Reason)
	}
	assertWalk(t, r, at(1, 0), at(1, 2))
}

func TestPermanentBlockClosesADiagonalCorner(t *testing.T) {
	// Walking NE from (0,2) to (1,1) squeezes between (1,2) and (0,1). With
	// both blocked the game refuses the step, and so must the search - even
	// when one of the two is a learned block rather than map data.
	g := NewPathGrid(gridFrom([]string{
		"...",
		"#..",
		"...",
	}), overlayOf([]string{
		"...",
		"...",
		".P.",
	}))
	r := FindPath(context.Background(), g, at(0, 2), at(1, 1), 1000)
	for i := 1; i < len(r.Steps); i++ {
		if r.Steps[i-1] == at(0, 2) && r.Steps[i] == at(1, 1) {
			t.Fatal("route cut a corner closed by a learned block")
		}
	}
}

func TestBlockedEdgeIsNotWalked(t *testing.T) {
	o := &Overlay{tiles: map[[2]int]Kind{}, edges: map[[4]int]bool{
		{1000, 1002, 1001, 1001}: true,
	}}
	g := NewPathGrid(gridFrom([]string{"...", "...", "..."}), o)
	r := FindPath(context.Background(), g, at(0, 2), at(1, 1), 1000)
	if !r.Found {
		t.Fatalf("status %s; blocking one edge must not block the destination", r.Status)
	}
	for i := 1; i < len(r.Steps); i++ {
		if r.Steps[i-1] == at(0, 2) && r.Steps[i] == at(1, 1) {
			t.Fatal("route used an edge known to fail")
		}
	}
}

func TestEmptyOverlayChangesNothing(t *testing.T) {
	rows := []string{"....", ".##.", "...."}
	plain := FindPath(context.Background(), NewPathGrid(gridFrom(rows), nil), at(0, 0), at(3, 2), 10000)
	empty := FindPath(context.Background(), NewPathGrid(gridFrom(rows), overlayOf([]string{"....", "....", "...."})), at(0, 0), at(3, 2), 10000)
	if plain.Cost != empty.Cost || len(plain.Steps) != len(empty.Steps) {
		t.Fatalf("an empty overlay changed the route: %v vs %v", plain, empty)
	}
}

func TestBaseGridIsNotMutatedByOverlay(t *testing.T) {
	base := gridFrom([]string{"....", "....", "...."})
	area := base.Area()
	before := map[[2]int]uint8{}
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			before[[2]int{x, y}] = base.At(x, y)
		}
	}
	FindPath(context.Background(), NewPathGrid(base, overlayOf([]string{"....", ".P..", "...."})), at(0, 0), at(3, 2), 10000)
	for tile, want := range before {
		if got := base.At(tile[0], tile[1]); got != want {
			t.Fatalf("the shared cost grid was modified at %v (%d -> %d); the floor cache is now poisoned", tile, want, got)
		}
	}
}

func TestTemporaryBlockAlsoClosesADiagonalCorner(t *testing.T) {
	// A creature standing at a corner makes the game refuse the squeeze just as
	// a wall would. Treating a fresh block as passable there would have the bot
	// emit a step the game rejects, then blame the edge for it.
	g := NewPathGrid(gridFrom([]string{
		"...",
		"#..",
		"...",
	}), overlayOf([]string{
		"...",
		"...",
		".T.",
	}))
	r := FindPath(context.Background(), g, at(0, 2), at(1, 1), 1000)
	for i := 1; i < len(r.Steps); i++ {
		if r.Steps[i-1] == at(0, 2) && r.Steps[i] == at(1, 1) {
			t.Fatal("route cut a corner closed by a fresh learned block")
		}
	}
}

func TestTemporaryPenaltyOutweighsALongDetour(t *testing.T) {
	// A wall with two gaps: the near one holds a fresh block, the far one is
	// eleven tiles away. Going around costs about twenty extra steps, so a
	// penalty worth only a few steps would send the bot straight into the
	// obstacle and leave it stuck there.
	terrain := []string{
		"...............",
		"#####.####.####",
		"...............",
	}
	blocks := []string{
		"...............",
		".....T.........",
		"...............",
	}
	g := NewPathGrid(gridFrom(terrain), overlayOf(blocks))
	r := FindPath(context.Background(), g, at(5, 0), at(5, 2), 10000)
	if !r.Found {
		t.Fatalf("status %s (%s)", r.Status, r.Reason)
	}
	for _, s := range r.Steps {
		if s == at(5, 1) {
			t.Fatal("A* preferred a fresh block over a detour far cheaper than the penalty")
		}
	}
}

func TestStraightStepsBeatDiagonalsOverOpenGround(t *testing.T) {
	// Walking a diagonal in Tibia takes three times as long as a straight step,
	// so ten diagonals cost far more than ten right + ten down. The shortest
	// route in tiles is not the fastest route in time, and it is time the bot
	// spends.
	rows := make([]string, 12)
	for i := range rows {
		rows[i] = strings.Repeat(".", 12)
	}
	g := NewPathGrid(gridFrom(rows), nil)
	r := FindPath(context.Background(), g, at(0, 0), at(10, 10), 100000)
	assertWalk(t, r, at(0, 0), at(10, 10))

	diagonals := 0
	for i := 1; i < len(r.Steps); i++ {
		if r.Steps[i][0] != r.Steps[i-1][0] && r.Steps[i][1] != r.Steps[i-1][1] {
			diagonals++
		}
	}
	if diagonals > 0 {
		t.Fatalf("route uses %d diagonals over open ground; straight steps are faster", diagonals)
	}
}

func TestDiagonalIsStillUsedWhenItSavesEnough(t *testing.T) {
	// One diagonal replaces two straight steps at a cost of three, so it loses
	// on open ground - but where the straight way is walled off, it must still
	// be taken rather than refused outright.
	g := NewPathGrid(gridFrom([]string{
		"..#",
		"#..",
		"...",
	}), nil)
	r := FindPath(context.Background(), g, at(0, 0), at(2, 1), 1000)
	if !r.Found {
		t.Fatalf("status %s (%s); the only way through is diagonal", r.Status, r.Reason)
	}
}
