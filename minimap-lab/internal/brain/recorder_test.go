package brain

import (
	"testing"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/route"
)

func pos(x, y int, z ...int) mapdata.Position {
	floor := 7
	if len(z) > 0 {
		floor = z[0]
	}
	return mapdata.Position{X: x, Y: y, Z: floor}
}

func autoRecorder(every int) *Recorder {
	r := NewRecorder()
	r.Auto, r.Every = true, every
	return r
}

func TestManualWaypointRecordsTheCurrentTileAsWalkableGround(t *testing.T) {
	r := NewRecorder()
	r.AddManual(pos(100, 200))
	want := route.Waypoint{X: 100, Y: 200, Z: 7, Type: "walk", Label: ""}
	if got := r.Waypoints(); len(got) != 1 || got[0] != want {
		t.Errorf("waypointy = %+v, oczekiwano [%+v]", got, want)
	}
}

func TestAutomaticRecordingStartsWithTheTileThePlayerStandsOn(t *testing.T) {
	r := autoRecorder(10)
	r.Observe(pos(100, 200))
	if got := len(r.Waypoints()); got != 1 {
		t.Errorf("waypointów = %d, oczekiwano 1", got)
	}
}

func TestAutomaticRecordingWaitsForTheConfiguredDistance(t *testing.T) {
	r := autoRecorder(10)
	r.Observe(pos(100, 200))
	r.Observe(pos(105, 200))
	if got := len(r.Waypoints()); got != 1 {
		t.Errorf("po pięciu kratkach waypointów = %d, oczekiwano 1 — pięć to nie dziesięć", got)
	}
	r.Observe(pos(110, 200))
	if got := len(r.Waypoints()); got != 2 {
		t.Errorf("po dziesięciu kratkach waypointów = %d, oczekiwano 2", got)
	}
}

// Six diagonal steps are six tiles walked, not twelve: the game charges for
// tiles crossed, so Chebyshev is the metric that matches what the character
// actually did.
func TestDistanceCountsDiagonalsAsSingleTiles(t *testing.T) {
	r := autoRecorder(10)
	r.Observe(pos(100, 200))
	r.Observe(pos(106, 206))
	if got := len(r.Waypoints()); got != 1 {
		t.Errorf("po sześciu skosach waypointów = %d, oczekiwano 1", got)
	}
	r.Observe(pos(110, 210))
	if got := len(r.Waypoints()); got != 2 {
		t.Errorf("po dziesięciu skosach waypointów = %d, oczekiwano 2", got)
	}
}

func TestRecordsNothingWhileSwitchedOffNotEvenFloorChanges(t *testing.T) {
	r := NewRecorder()
	r.Every = 10
	r.Observe(pos(100, 200, 7))
	r.Observe(pos(100, 200, 6))
	if got := len(r.Waypoints()); got != 0 {
		t.Errorf("wyłączony recorder zapisał %d waypointów", got)
	}
}

// The minimap only reveals a transition once the player is already on the new
// floor, so the tile it was used from has to be recorded retroactively -
// otherwise the route has no idea where to stand.
func TestFloorChangeRecordsTheTileBeforeItAndTheTileAfter(t *testing.T) {
	r := autoRecorder(10)
	r.Observe(pos(100, 200, 7))
	r.Observe(pos(101, 201, 7))
	r.Observe(pos(101, 201, 6))

	got := r.Waypoints()
	if len(got) != 3 {
		t.Fatalf("waypointów = %d, oczekiwano 3", len(got))
	}
	before, after := got[1], got[2]
	if before.X != 101 || before.Y != 201 || before.Z != 7 {
		t.Errorf("kratka przejścia = %+v, akcja dzieje się na starym piętrze", before)
	}
	if after.X != 101 || after.Y != 201 || after.Z != 6 || after.Type != "walk" {
		t.Errorf("kratka po przejściu = %+v", after)
	}
}

func TestTransitionTypeIsGuessedFromDirectionAndDisplacement(t *testing.T) {
	for _, c := range []struct {
		name     string
		to       mapdata.Position
		wantType string
	}{
		{"w górę bez ruchu to lina", pos(100, 200, 6), "rope"},
		{"w dół bez ruchu to dziura", pos(100, 200, 8), "hole"},
		{"zmiana piętra z przesunięciem to schody", pos(101, 200, 6), "stairs"},
	} {
		r := autoRecorder(10)
		r.Observe(pos(100, 200, 7))
		r.Observe(c.to)
		got := r.Waypoints()
		if len(got) < 2 || got[1].Type != c.wantType {
			t.Errorf("%s: typ = %v, oczekiwano %s", c.name, got, c.wantType)
		}
	}
}

func TestFloorChangeIsRecordedEvenRightAfterAnotherWaypoint(t *testing.T) {
	r := autoRecorder(10)
	r.Observe(pos(100, 200, 7))
	r.Observe(pos(100, 200, 6))
	r.Observe(pos(100, 200, 5))
	if got := len(r.Waypoints()); got != 5 {
		t.Errorf("waypointów = %d, oczekiwano 5 — każde przejście dokłada własną parę", got)
	}
}

func TestRecorderStopsAtTheFileFormatLimit(t *testing.T) {
	r := autoRecorder(1)
	for i := 0; i < 1200; i++ {
		r.Observe(pos(100+i, 200))
	}
	if got := len(r.Waypoints()); got != route.MaxWaypoints {
		t.Errorf("waypointów = %d, oczekiwano %d", got, route.MaxWaypoints)
	}
}

func TestManualAndAutomaticPointsShareOneList(t *testing.T) {
	r := autoRecorder(10)
	r.Observe(pos(100, 200))
	r.AddManual(pos(103, 200))
	r.Observe(pos(105, 200))
	if got := len(r.Waypoints()); got != 2 {
		t.Errorf("waypointów = %d, oczekiwano 2 — ręczny punkt zeruje licznik odległości", got)
	}
	r.Observe(pos(113, 200))
	if got := len(r.Waypoints()); got != 3 {
		t.Errorf("waypointów = %d, oczekiwano 3", got)
	}
}

// Observe reports how many waypoints it added, so the loop can tell a frame
// that changed the route from one that did not without diffing the list.
func TestObserveReportsHowManyWaypointsItAdded(t *testing.T) {
	r := autoRecorder(10)
	if got := r.Observe(pos(100, 200, 7)); got != 1 {
		t.Errorf("pierwsza obserwacja dodała %d, oczekiwano 1", got)
	}
	if got := r.Observe(pos(101, 200, 7)); got != 0 {
		t.Errorf("krok w miejscu dodał %d, oczekiwano 0", got)
	}
	if got := r.Observe(pos(101, 200, 6)); got != 2 {
		t.Errorf("zmiana piętra dodała %d, oczekiwano 2", got)
	}
}
