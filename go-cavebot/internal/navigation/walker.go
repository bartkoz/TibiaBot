package navigation

import "time"

type Direction int

const (
	North Direction = iota
	South
	West
	East
	Northwest
	Northeast
	Southwest
	Southeast
)

type dirDelta struct {
	dx, dy int
}

var dirDeltas = map[Direction]dirDelta{
	North:     {0, -1},
	South:     {0, 1},
	West:      {-1, 0},
	East:      {1, 0},
	Northwest: {-1, -1},
	Northeast: {1, -1},
	Southwest: {-1, 1},
	Southeast: {1, 1},
}

var deltaToDir map[dirDelta]Direction

func init() {
	deltaToDir = make(map[dirDelta]Direction)
	for d, delta := range dirDeltas {
		deltaToDir[delta] = d
	}
}

func (d Direction) Keys() []string {
	delta := dirDeltas[d]
	var keys []string
	if delta.dy < 0 {
		keys = append(keys, "up")
	} else if delta.dy > 0 {
		keys = append(keys, "down")
	}
	if delta.dx < 0 {
		keys = append(keys, "left")
	} else if delta.dx > 0 {
		keys = append(keys, "right")
	}
	return keys
}

func PathToDirections(path [][2]int) []Direction {
	var dirs []Direction
	for i := 1; i < len(path); i++ {
		dx := path[i][0] - path[i-1][0]
		dy := path[i][1] - path[i-1][1]
		if dx < -1 {
			dx = -1
		} else if dx > 1 {
			dx = 1
		}
		if dy < -1 {
			dy = -1
		} else if dy > 1 {
			dy = 1
		}
		if d, ok := deltaToDir[dirDelta{dx, dy}]; ok {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func WalkPath(path [][2]int, stepDelay time.Duration, pressFunc func([]string)) {
	dirs := PathToDirections(path)
	for _, d := range dirs {
		pressFunc(d.Keys())
		time.Sleep(stepDelay)
	}
}
