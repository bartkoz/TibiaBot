package navigation

import (
	"container/heap"
	"math"
)

const (
	blockedCost    = 255
	unexploredCost = 0
)

type movDir struct {
	dx, dy int
	cost   float64
}

var moveDirs = []movDir{
	{0, -1, 1.0},
	{0, 1, 1.0},
	{-1, 0, 1.0},
	{1, 0, 1.0},
	{-1, -1, 1.414},
	{1, -1, 1.414},
	{-1, 1, 1.414},
	{1, 1, 1.414},
}

func octileDistance(x1, y1, x2, y2 int) float64 {
	dx := math.Abs(float64(x2 - x1))
	dy := math.Abs(float64(y2 - y1))
	return math.Max(dx, dy) + (math.Sqrt2-1)*math.Min(dx, dy)
}

// FindPath runs A* from start to goal on the atlas cost grid.
// start and goal are [x, y, z]. Returns a slice of [x, y] points or nil.
func FindPath(atlas *Atlas, start, goal [3]int, maxIterations int) [][2]int {
	sx, sy, sz := start[0], start[1], start[2]
	gx, gy, gz := goal[0], goal[1], goal[2]

	if sz != gz {
		return nil
	}
	z := sz

	startCost := atlas.GetCostAt(sx, sy, z)
	goalCost := atlas.GetCostAt(gx, gy, z)
	if startCost >= blockedCost || startCost == unexploredCost {
		return nil
	}
	if goalCost >= blockedCost || goalCost == unexploredCost {
		return nil
	}

	if sx == gx && sy == gy {
		return [][2]int{{sx, sy}}
	}

	type point = [2]int
	counter := 0
	pq := &priorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &pqItem{
		x: sx, y: sy,
		f:   octileDistance(sx, sy, gx, gy),
		idx: counter,
	})
	counter++

	cameFrom := make(map[point]point)
	gScore := make(map[point]float64)
	gScore[point{sx, sy}] = 0

	iterations := 0
	for pq.Len() > 0 && iterations < maxIterations {
		iterations++
		cur := heap.Pop(pq).(*pqItem)
		cx, cy := cur.x, cur.y

		if cx == gx && cy == gy {
			path := []point{{cx, cy}}
			for {
				prev, ok := cameFrom[point{cx, cy}]
				if !ok {
					break
				}
				cx, cy = prev[0], prev[1]
				path = append(path, point{cx, cy})
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}

		for _, d := range moveDirs {
			nx, ny := cx+d.dx, cy+d.dy
			tileCost := atlas.GetCostAt(nx, ny, z)
			if tileCost >= blockedCost || tileCost == unexploredCost {
				continue
			}
			moveCost := d.cost * (float64(tileCost) / 150.0)
			tentativeG := gScore[point{cx, cy}] + moveCost

			prev, exists := gScore[point{nx, ny}]
			if !exists || tentativeG < prev {
				cameFrom[point{nx, ny}] = point{cx, cy}
				gScore[point{nx, ny}] = tentativeG
				f := tentativeG + octileDistance(nx, ny, gx, gy)
				heap.Push(pq, &pqItem{x: nx, y: ny, f: f, idx: counter})
				counter++
			}
		}
	}
	return nil
}

// Priority queue for A*
type pqItem struct {
	x, y    int
	f       float64
	idx     int
	heapIdx int
}

type priorityQueue []*pqItem

func (pq priorityQueue) Len() int            { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool  { return pq[i].f < pq[j].f }
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].heapIdx = i
	pq[j].heapIdx = j
}
func (pq *priorityQueue) Push(x interface{}) {
	item := x.(*pqItem)
	item.heapIdx = len(*pq)
	*pq = append(*pq, item)
}
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.heapIdx = -1
	*pq = old[:n-1]
	return item
}
