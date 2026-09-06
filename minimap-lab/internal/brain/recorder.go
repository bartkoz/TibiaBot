package brain

import (
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/route"
)

// tileDistance is Chebyshev distance: the game charges for tiles crossed, and
// a diagonal step crosses one tile, not one and a half.
func tileDistance(a, b mapdata.Position) int {
	dx, dy := abs(a.X-b.X), abs(a.Y-b.Y)
	if dx > dy {
		return dx
	}
	return dy
}

// GuessTransition names the transition that most likely produced a floor
// change. The minimap encodes no transition type - rope, ladder, stairs and
// hole look alike on it - so direction and displacement are all there is to go
// on; the panel lets the user correct it afterwards.
func GuessTransition(from, to mapdata.Position) string {
	if tileDistance(from, to) > 0 {
		return "stairs"
	}
	if to.Z < from.Z {
		return "rope"
	}
	return "hole"
}

// Recorder turns a stream of confirmed positions into waypoints. It is
// deliberately ignorant of whether a tile is walkable: that check needs the
// cost grid and the learned blockages, so the loop makes it before calling in.
type Recorder struct {
	// Auto records automatically as the character walks; with it off, only
	// AddManual writes anything.
	Auto bool
	// Every is how many tiles apart automatic waypoints are placed.
	Every int

	waypoints []route.Waypoint
	last      *mapdata.Position
	lastSaved *mapdata.Position
}

func NewRecorder() *Recorder { return &Recorder{Every: 10} }

func (r *Recorder) Waypoints() []route.Waypoint { return r.waypoints }

func (r *Recorder) SetWaypoints(w []route.Waypoint) {
	r.waypoints = w
	r.lastSaved = nil
}

func (r *Recorder) Full() bool { return len(r.waypoints) >= route.MaxWaypoints }

func (r *Recorder) push(p mapdata.Position, kind string) bool {
	if r.Full() {
		return false
	}
	r.waypoints = append(r.waypoints, route.Waypoint{X: p.X, Y: p.Y, Z: p.Z, Type: kind})
	saved := p
	r.lastSaved = &saved
	return true
}

func (r *Recorder) AddManual(p mapdata.Position) bool { return r.push(p, "walk") }

// Observe consumes one tracked position and reports how many waypoints it
// added. A floor change always records the tile before the transition, which
// the tracker only reveals once the player is already on the new floor.
func (r *Recorder) Observe(p mapdata.Position) int {
	previous := r.last
	current := p
	r.last = &current
	if !r.Auto {
		return 0
	}
	if previous != nil && previous.Z != p.Z {
		added := 0
		if r.push(*previous, GuessTransition(*previous, p)) {
			added++
		}
		if r.push(p, "walk") {
			added++
		}
		return added
	}
	if r.lastSaved == nil || tileDistance(*r.lastSaved, p) >= r.Every {
		if r.push(p, "walk") {
			return 1
		}
	}
	return 0
}
