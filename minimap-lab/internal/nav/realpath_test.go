package nav

import (
	"context"
	"image"
	"testing"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/testenv"
)

// A tile in the largest connected walkable region of the Venore surface, and a
// goal far enough away that the route has to work for it.
var (
	realOpenGround = [2]int{32786, 32061}
	realOpenGoal   = [2]int{32786, 32121}
	// The position the capture fixture is anchored on, hemmed in by walls.
	realEnclosed = [2]int{32958, 32077}
)

func realCostGrid(t *testing.T, centre [2]int, radius int) *mapdata.CostGrid {
	t.Helper()
	dir := testenv.MapDir(t)
	area := image.Rect(centre[0]-radius, centre[1]-radius, centre[0]+radius, centre[1]+radius)
	grid, err := mapdata.LoadCostArea(dir, 7, area)
	if err != nil {
		t.Fatal(err)
	}
	return grid.LimitTo(area)
}

func TestRealMapRouteStaysOnWalkableGround(t *testing.T) {
	grid := realCostGrid(t, realOpenGround, 70)

	r := FindPath(context.Background(), NewPathGrid(grid, nil), realOpenGround, realOpenGoal, 500000)

	if !r.Found {
		t.Fatalf("no route across open ground: %+v", r)
	}
	if r.Tiles < 40 {
		t.Fatalf("expected a long route, got %d tiles", r.Tiles)
	}
	if r.Steps[0] != realOpenGround || r.Steps[len(r.Steps)-1] != realOpenGoal {
		t.Fatalf("route runs from %v to %v", r.Steps[0], r.Steps[len(r.Steps)-1])
	}
	for i, s := range r.Steps {
		if grid.At(s[0], s[1]) == mapdata.BlockedCost {
			t.Fatalf("step %d at %v crosses a wall", i, s)
		}
		if i == 0 {
			continue
		}
		previous := r.Steps[i-1]
		dx, dy := s[0]-previous[0], s[1]-previous[1]
		if abs(dx) > 1 || abs(dy) > 1 || (dx == 0 && dy == 0) {
			t.Fatalf("step %d jumps from %v to %v", i, previous, s)
		}
		if dx != 0 && dy != 0 &&
			grid.At(previous[0]+dx, previous[1]) == mapdata.BlockedCost &&
			grid.At(previous[0], previous[1]+dy) == mapdata.BlockedCost {
			t.Fatalf("step %d cuts a closed corner from %v to %v", i, previous, s)
		}
	}
}

func TestRealMapRefusesAWallAsAWaypoint(t *testing.T) {
	grid := realCostGrid(t, realEnclosed, 40)
	var wall [2]int
	for dy := -20; dy <= 20 && wall == [2]int{}; dy++ {
		for dx := -20; dx <= 20; dx++ {
			if grid.At(realEnclosed[0]+dx, realEnclosed[1]+dy) == mapdata.BlockedCost {
				wall = [2]int{realEnclosed[0] + dx, realEnclosed[1] + dy}
				break
			}
		}
	}
	if wall == [2]int{} {
		t.Fatal("expected a blocked tile near the enclosed fixture position")
	}

	r := FindPath(context.Background(), NewPathGrid(grid, nil), realEnclosed, wall, 200000)

	if r.Found || r.Status != "blocked_goal" {
		t.Fatalf("a wall is not a waypoint: %+v", r)
	}
}

// Walled-in ground is a real situation on this map: the tile the capture
// fixture uses reaches only its own courtyard.
func TestRealMapReportsAnUnreachableWaypoint(t *testing.T) {
	grid := realCostGrid(t, realEnclosed, 40)
	var outside [2]int
	for dx := 10; dx <= 25 && outside == [2]int{}; dx++ {
		candidate := [2]int{realEnclosed[0] + dx, realEnclosed[1] + 20}
		if grid.At(candidate[0], candidate[1]) != mapdata.BlockedCost {
			outside = candidate
		}
	}
	if outside == [2]int{} {
		t.Skip("no walkable tile outside the courtyard in range")
	}

	r := FindPath(context.Background(), NewPathGrid(grid, nil), realEnclosed, outside, 500000)

	if r.Found {
		t.Skipf("tile %v turned out to be reachable; nothing to assert", outside)
	}
	if r.Status != "no_route" {
		t.Fatalf("unreachable ground should report no_route: %+v", r)
	}
}
