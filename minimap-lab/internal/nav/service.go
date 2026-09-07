package nav

import (
	"context"
	"fmt"
	"image"
	"sync"
	"time"

	"minimap-lab/internal/mapdata"
)

const (
	DefaultMargin = 64
	MaxMargin     = 256
	PlanTimeout   = 5 * time.Second
	// Coordinates alone are not a bound: two valid tiles at opposite map
	// corners would allocate gigabytes before any search starts.
	maxSearchTiles = 4 << 20
)

// Planner answers route queries against the map on disk. It owns the decoded
// cost grid it keeps between queries, so it is safe to call from more than one
// goroutine even though in practice the brain loop is the only caller.
//
// The learned blockages are passed in per query rather than held here: the
// server owns that store, and a planner keeping its own pointer to it would be
// a second owner able to fall out of step with the first.
type Planner struct {
	mu    sync.Mutex
	dir   string
	cache *mapdata.CostGrid
	floor int
}

func NewPlanner(dir string) *Planner { return &Planner{dir: dir} }

// Plan finds a route between two tiles on one floor. Ordinary situations -
// blocked endpoints, no route, a waypoint on another floor - come back as a
// PathResult carrying an explanation, not as an error. Errors are reserved for
// requests that cannot be attempted at all.
func (p *Planner) Plan(ctx context.Context, blocks *BlockStore, from, to mapdata.Position, margin int) (PathResult, error) {
	started := time.Now()
	if margin < 0 || margin > MaxMargin {
		return PathResult{}, fmt.Errorf("margines musi mieścić się w zakresie 0–%d", MaxMargin)
	}
	if from.Z != to.Z {
		return PathResult{Status: "different_floor", Steps: [][2]int{},
			Reason: "Waypoint leży na innym piętrze. Użyj przejścia i poczekaj na potwierdzenie nowego Z."}, nil
	}
	if margin == 0 {
		margin = DefaultMargin
	}
	area := image.Rect(min(from.X, to.X), min(from.Y, to.Y),
		max(from.X, to.X)+1, max(from.Y, to.Y)+1).Inset(-margin)
	if int64(area.Dx())*int64(area.Dy()) > maxSearchTiles {
		return PathResult{}, fmt.Errorf("obszar wyszukiwania jest za duży; zmniejsz odległość między waypointami lub margines")
	}
	ctx, cancel := context.WithTimeout(ctx, PlanTimeout)
	defer cancel()

	p.mu.Lock()
	defer p.mu.Unlock()
	// The lock itself cannot be cancelled, so a request abandoned while
	// waiting is dropped here instead of going on to scan and decode chunks.
	if err := ctx.Err(); err != nil {
		return PathResult{}, err
	}
	grid, err := p.costGridLocked(from.Z, area)
	if err != nil {
		return PathResult{}, err
	}
	// Presence beats any learned hypothesis: a character standing on a tile we
	// marked unreachable is proof the mark is simply wrong.
	if blocks.Clear(from) {
		// A revoked permanent block has to reach the file too, or a restart
		// brings it back and cuts this route again.
		blocks.Flush()
	}
	// One snapshot for the whole search: A* assumes the cost of a closed
	// vertex never changes, so the graph must not shift under it mid-search.
	// The revision comes from the same call, so it describes the overlay the
	// route was actually computed on rather than whatever arrived while it ran.
	overlay, revision := blocks.SnapshotAt(area, from.Z)
	pg := NewPathGrid(grid.LimitTo(area), overlay)
	// A waypoint recorded a tile or two off - the tracker drifted, or the
	// position was read wrong - would otherwise kill the whole route. Aim at
	// the nearest tile the bot can stand on instead, and say so.
	//
	// Checked only once the start is sound: a character standing on ground the
	// map calls impassable is the more useful thing to report, and FindPath
	// says so on its own.
	goalX, goalY := to.X, to.Y
	ok := true
	if !pg.Blocked(from.X, from.Y) {
		goalX, goalY, ok = pg.NearestWalkable(to.X, to.Y)
	}
	if !ok {
		return PathResult{Status: "blocked_goal", Steps: [][2]int{},
			Reason: pg.GoalRefusal(to.X, to.Y), OverlayRevision: revision,
			ElapsedMS: elapsedMS(started)}, nil
	}
	// Every reachable tile is closed at most once, so the area itself bounds
	// the work; no arbitrary iteration constant is needed.
	result := FindPath(ctx, pg, [2]int{from.X, from.Y}, [2]int{goalX, goalY}, area.Dx()*area.Dy())
	result.GoalMoved = goalX != to.X || goalY != to.Y
	result.OverlayRevision = revision
	if result.Steps == nil {
		result.Steps = [][2]int{}
	}
	result.ElapsedMS = elapsedMS(started)
	return result, nil
}

func elapsedMS(since time.Time) float64 {
	return float64(time.Since(since).Microseconds()) / 1000
}

// costGridLocked keeps one decoded floor of walking costs.
func (p *Planner) costGridLocked(floor int, area image.Rectangle) (*mapdata.CostGrid, error) {
	if p.cache != nil && p.floor == floor && area.In(p.cache.Bounds()) {
		return p.cache, nil
	}
	grid, err := mapdata.LoadCostArea(p.dir, floor, area)
	if err != nil {
		return nil, err
	}
	p.cache, p.floor = grid, floor
	return grid, nil
}

// CachedFloor reports which floor of cost data the planner is holding, if any.
// It exists so a test can prove the route cache and the walkability-preview
// cache do not evict each other; nothing in production reads it.
func (p *Planner) CachedFloor() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cache == nil {
		return 0, false
	}
	return p.floor, true
}
