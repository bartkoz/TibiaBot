package brain

import (
	"strings"
	"testing"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/nav"
	"minimap-lab/internal/route"
)

func tol(v int) *int { return &v }

func wp(x, y int, rest ...any) route.Waypoint {
	z, kind := 7, "walk"
	if len(rest) > 0 {
		z = rest[0].(int)
	}
	if len(rest) > 1 {
		kind = rest[1].(string)
	}
	return route.Waypoint{X: x, Y: y, Z: z, Type: kind}
}

// straight is the eastward path the planner would return for from -> to.
func straight(from, to mapdata.Position) *nav.PathResult {
	steps := [][2]int{}
	for x := from.X; x <= to.X; x++ {
		steps = append(steps, [2]int{x, from.Y})
	}
	return &nav.PathResult{Found: true, Status: "ok", Steps: steps, Tiles: len(steps) - 1}
}

func TestAnEmptyRouteIsFinishedBeforeItStarts(t *testing.T) {
	f := NewFollower(nil, FollowerOptions{})
	if got := f.Step(pos(10, 10), at(0)).Action; got != ActionDone {
		t.Errorf("akcja = %s, oczekiwano done", got)
	}
}

func TestReachingTheLastWaypointFinishesTheRoute(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10)}, FollowerOptions{})
	if got := f.Step(pos(10, 10), at(0)).Action; got != ActionDone {
		t.Errorf("akcja = %s, oczekiwano done", got)
	}
}

func TestStandingOnAWaypointAdvancesToTheNextOne(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10), wp(20, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	if f.Index() != 1 {
		t.Errorf("indeks = %d, oczekiwano 1", f.Index())
	}
}

// Chebyshev distance 1 is within the default tolerance.
func TestADiagonalNeighbourCountsAsReachingTheWaypoint(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10), wp(20, 10)}, FollowerOptions{})
	f.Step(pos(11, 11), at(0))
	if f.Index() != 1 {
		t.Errorf("indeks = %d, oczekiwano 1", f.Index())
	}
}

func TestAWaypointTwoTilesAwayIsNotReachedYet(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10), wp(20, 10)}, FollowerOptions{})
	f.Step(pos(12, 10), at(0))
	if f.Index() != 0 {
		t.Errorf("indeks = %d, oczekiwano 0", f.Index())
	}
}

func TestWithoutAPathTheFollowerAsksForOne(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10)}, FollowerOptions{})
	out := f.Step(pos(10, 10), at(0))
	if out.Action != ActionPath {
		t.Fatalf("akcja = %s, oczekiwano path", out.Action)
	}
	if out.From != pos(10, 10) || out.To != pos(20, 10) {
		t.Errorf("zapytanie = %+v -> %+v", out.From, out.To)
	}
}

func TestAPathTurnsIntoAWalkingDirection(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	target := wp(20, 10)
	f.SetPath(straight(pos(10, 10), pos(20, 10)), at(10), &target)
	out := f.Step(pos(10, 10), at(20))
	if out.Action != ActionWalk || out.Direction != "E" {
		t.Fatalf("wyjście = %+v", out)
	}
	if out.Next != [2]int{11, 10} {
		t.Errorf("następna kratka = %v", out.Next)
	}
	if out.Remaining != 10 {
		t.Errorf("pozostało = %d, oczekiwano 10", out.Remaining)
	}
}

func TestEveryCompassDirectionComesOutOfTheNextStep(t *testing.T) {
	for want, next := range map[string][2]int{
		"N": {10, 9}, "NE": {11, 9}, "E": {11, 10}, "SE": {11, 11},
		"S": {10, 11}, "SW": {9, 11}, "W": {9, 10}, "NW": {9, 9},
	} {
		// The waypoint sits far enough away not to be reached on the spot.
		far := wp(10+(next[0]-10)*9, 10+(next[1]-10)*9)
		f := NewFollower([]route.Waypoint{far}, FollowerOptions{})
		f.Step(pos(10, 10), at(0))
		f.SetPath(&nav.PathResult{Found: true, Status: "ok", Steps: [][2]int{{10, 10}, next}, Tiles: 1}, at(10), &far)
		if got := f.Step(pos(10, 10), at(20)).Direction; got != want {
			t.Errorf("kierunek na %v = %q, oczekiwano %q", next, got, want)
		}
	}
}

func TestWalkingAlongThePathConsumesItWithoutAskingAgain(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	target := wp(20, 10)
	f.SetPath(straight(pos(10, 10), pos(20, 10)), at(10), &target)
	out := f.Step(pos(13, 10), at(5000))
	if out.Action != ActionWalk || out.Next != [2]int{14, 10} || out.Remaining != 7 {
		t.Errorf("wyjście = %+v", out)
	}
}

func TestSteppingOffThePathTriggersANewRequestOnceTheThrottleAllows(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	target := wp(20, 10)
	f.SetPath(straight(pos(10, 10), pos(20, 10)), at(10), &target)
	if got := f.Step(pos(13, 17), at(100)).Action; got != ActionWait {
		t.Errorf("akcja = %s, oczekiwano wait — za wcześnie po ostatnim zapytaniu", got)
	}
	if got := f.Step(pos(13, 17), at(700)).Action; got != ActionPath {
		t.Errorf("akcja = %s, oczekiwano path", got)
	}
}

func TestAWaypointOnAnotherFloorWaitsForTheTransition(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10, 6, "rope")}, FollowerOptions{})
	out := f.Step(pos(10, 10, 7), at(0))
	if out.Action != ActionTransition {
		t.Fatalf("akcja = %s, oczekiwano transition", out.Action)
	}
	if !strings.Contains(strings.ToLower(out.Instruction), "lin") {
		t.Errorf("instrukcja = %q", out.Instruction)
	}
	if out.Waypoint == nil || out.Waypoint.Z != 6 {
		t.Errorf("waypoint = %+v", out.Waypoint)
	}
}

func TestEachTransitionTypeHasItsOwnInstruction(t *testing.T) {
	for _, c := range []struct{ kind, want string }{
		{"ladder", "drabin"}, {"stairs", "schod"}, {"hole", "dziur"}, {"shovel", "kop"},
	} {
		f := NewFollower([]route.Waypoint{wp(10, 10, 8, c.kind)}, FollowerOptions{})
		got := f.Step(pos(10, 10, 7), at(0)).Instruction
		if !strings.Contains(strings.ToLower(got), c.want) {
			t.Errorf("instrukcja dla %s = %q, oczekiwano fragmentu %q", c.kind, got, c.want)
		}
	}
}

func TestArrivingOnTheNewFloorResumesNormalFollowing(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10, 6), wp(20, 10, 6)}, FollowerOptions{})
	f.Step(pos(10, 10, 7), at(0))
	f.Step(pos(10, 10, 6), at(100))
	if f.Index() != 1 {
		t.Errorf("indeks = %d, oczekiwano 1", f.Index())
	}
}

func TestAStalePathForAPreviousWaypointIsDiscarded(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10), wp(30, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	target := wp(20, 10)
	f.SetPath(straight(pos(10, 10), pos(20, 10)), at(10), &target)
	f.Step(pos(20, 10), at(20))
	if f.Index() != 1 {
		t.Fatalf("indeks = %d, oczekiwano 1", f.Index())
	}
	if got := f.Step(pos(20, 10), at(1000)).Action; got != ActionPath {
		t.Errorf("akcja = %s, oczekiwano path — stara trasa prowadziła do waypointa 1", got)
	}
}

func TestABlockedWaypointIsReportedRatherThanRetriedInATightLoop(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	target := wp(20, 10)
	f.SetPath(&nav.PathResult{Found: false, Status: "blocked_goal", Reason: "nieprzechodnia"}, at(10), &target)
	out := f.Step(pos(10, 10), at(20))
	if out.Action != ActionBlocked || out.Status != "blocked_goal" {
		t.Fatalf("wyjście = %+v", out)
	}
	if !strings.Contains(out.Reason, "nieprzechodnia") {
		t.Errorf("powód = %q", out.Reason)
	}
}

func TestALoopingRouteRestartsAtTheFirstWaypoint(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10), wp(20, 10)}, FollowerOptions{Loop: true})
	f.Step(pos(10, 10), at(0))
	out := f.Step(pos(20, 10), at(100))
	if f.Index() != 0 {
		t.Errorf("indeks = %d, oczekiwano 0", f.Index())
	}
	if out.Action == ActionDone {
		t.Error("zapętlona trasa się zakończyła")
	}
}

func TestToleranceIsConfigurableForOpenGround(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10), wp(20, 10)}, FollowerOptions{Tolerance: tol(3)})
	f.Step(pos(13, 12), at(0))
	if f.Index() != 1 {
		t.Errorf("indeks = %d, oczekiwano 1", f.Index())
	}
}

func TestSkippingToAWaypointDropsThePathBuiltForTheOldOne(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10), wp(30, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	target := wp(20, 10)
	f.SetPath(straight(pos(10, 10), pos(20, 10)), at(10), &target)
	f.SkipTo(1)
	if got := f.Step(pos(10, 10), at(1000)).Action; got != ActionPath {
		t.Errorf("akcja = %s, oczekiwano path", got)
	}
}

// The panel lets the user move a waypoint mid-route; the cached path still led
// to the old tile.
func TestEditingTheCurrentWaypointInvalidatesThePath(t *testing.T) {
	waypoints := []route.Waypoint{wp(20, 10)}
	f := NewFollower(waypoints, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	target := wp(20, 10)
	f.SetPath(straight(pos(10, 10), pos(20, 10)), at(10), &target)
	if got := f.Step(pos(10, 10), at(20)).Action; got != ActionWalk {
		t.Fatalf("akcja = %s, oczekiwano walk", got)
	}
	waypoints[0].X = 25
	if got := f.Step(pos(10, 10), at(1000)).Action; got != ActionPath {
		t.Errorf("akcja = %s, oczekiwano path", got)
	}
}

// Exactly what the recorder writes for a rope: the tile before the transition
// carries the action, the tile after it is plain walking.
func TestARecordedTransitionPairKeepsTheActionInstruction(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10, 7, "rope"), wp(10, 10, 6, "walk")}, FollowerOptions{})
	out := f.Step(pos(10, 10, 7), at(0))
	if out.Action != ActionTransition {
		t.Fatalf("akcja = %s, oczekiwano transition", out.Action)
	}
	if !strings.Contains(strings.ToLower(out.Instruction), "lin") {
		t.Errorf("instrukcja = %q", out.Instruction)
	}
	if f.Index() != 0 {
		t.Errorf("indeks = %d — waypoint akcji nie może zostać zjedzony samym wejściem na niego", f.Index())
	}
}

func TestAnActionWaypointIsConsumedOnceTheFloorActuallyChanges(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10, 7, "rope"), wp(10, 10, 6, "walk"), wp(20, 10, 6)}, FollowerOptions{})
	f.Step(pos(10, 10, 7), at(0))
	f.Step(pos(10, 10, 6), at(100))
	if f.Index() != 2 {
		t.Errorf("indeks = %d, oczekiwano 2", f.Index())
	}
}

func TestAnActionWaypointStillHasToBeWalkedToFirst(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10, 7, "rope")}, FollowerOptions{})
	if got := f.Step(pos(10, 10, 7), at(0)).Action; got != ActionPath {
		t.Errorf("akcja = %s, oczekiwano path — dziesięć kratek dalej", got)
	}
}

// The next waypoint lies west, so an eastward path for the old one would steer
// the player the wrong way.
func TestAPathThatArrivesAfterTheWaypointChangedIsDiscarded(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10), wp(0, 10)}, FollowerOptions{})
	asked := f.Step(pos(10, 10), at(0))
	if asked.To != pos(20, 10) {
		t.Fatalf("zapytano o %+v", asked.To)
	}
	f.Step(pos(19, 10), at(100))
	if f.Index() != 1 {
		t.Fatalf("indeks = %d, oczekiwano 1", f.Index())
	}
	askedFor := wp(20, 10)
	f.SetPath(straight(pos(10, 10), pos(20, 10)), at(200), &askedFor)
	out := f.Step(pos(19, 10), at(1000))
	if out.Direction == "E" {
		t.Error("nieaktualna trasa wskazała wschód, a nowy waypoint jest na zachodzie")
	}
	if out.Action != ActionPath || out.To != pos(0, 10) {
		t.Errorf("wyjście = %+v", out)
	}
}

// The tracker samples at 10 Hz; the player can use the rope and be reported on
// the new floor without any reading showing them on the action waypoint.
func TestATransitionCompletedBetweenTwoReadingsStillCounts(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10, 7, "rope"), wp(10, 10, 6), wp(20, 10, 6)}, FollowerOptions{})
	out := f.Step(pos(10, 10, 6), at(0))
	if out.Action == ActionTransition && strings.Contains(out.Instruction, "piętro 7") {
		t.Errorf("follower odsyła gracza z powrotem w górę: %+v", out)
	}
	if f.Index() != 2 {
		t.Errorf("indeks = %d, oczekiwano 2", f.Index())
	}
}

func TestAStaleReplyLeavesANewerPathAlone(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(20, 10), wp(40, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	first := wp(20, 10)
	second := f.Step(pos(19, 10), at(100))
	if second.To != pos(40, 10) {
		t.Fatalf("drugie zapytanie o %+v", second.To)
	}
	secondTarget := wp(40, 10)
	f.SetPath(straight(pos(19, 10), pos(40, 10)), at(150), &secondTarget)
	if got := f.Step(pos(19, 10), at(160)).Action; got != ActionWalk {
		t.Fatalf("akcja = %s, oczekiwano walk", got)
	}
	f.SetPath(straight(pos(10, 10), pos(20, 10)), at(200), &first) // spóźniona odpowiedź
	if got := f.Step(pos(19, 10), at(210)).Action; got != ActionWalk {
		t.Errorf("akcja = %s — dobra trasa musi przetrwać spóźnioną odpowiedź", got)
	}
}

func TestSkippingAwayAndBackDoesNotReuseAnOldFloorChange(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10, 7, "rope"), wp(50, 50, 7)}, FollowerOptions{})
	f.Step(pos(10, 10, 7), at(0))
	f.SkipTo(1)
	f.SkipTo(0)
	out := f.Step(pos(10, 10, 6), at(100))
	if f.Index() != 0 {
		t.Errorf("indeks = %d — porzucona akcja musi zostać uzbrojona na nowo, nie zapamiętana", f.Index())
	}
	if out.Action != ActionTransition {
		t.Errorf("akcja = %s, oczekiwano transition", out.Action)
	}
}

func TestAFailedPathIsRetriedOnceThePlayerMoves(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(30, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	target := wp(30, 10)
	f.SetPath(&nav.PathResult{Found: false, Status: "error", Reason: "sieć padła"}, at(10), &target)
	if got := f.Step(pos(10, 10), at(20)).Action; got != ActionBlocked {
		t.Fatalf("akcja = %s, oczekiwano blocked", got)
	}
	if got := f.Step(pos(11, 10), at(30)).Action; got != ActionPath {
		t.Errorf("akcja = %s — nowa kratka to nowa sytuacja, nie ta sama porażka", got)
	}
}

func TestAFailedPathIsRetriedAfterABackoffEvenStandingStill(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(30, 10)}, FollowerOptions{})
	f.Step(pos(10, 10), at(0))
	target := wp(30, 10)
	f.SetPath(&nav.PathResult{Found: false, Status: "blocked_goal", Reason: "ściana"}, at(10), &target)
	if got := f.Step(pos(10, 10), at(500)).Action; got != ActionBlocked {
		t.Fatalf("akcja = %s — nie wolno młócić planera", got)
	}
	if got := f.Step(pos(10, 10), at(5000)).Action; got != ActionPath {
		t.Errorf("akcja = %s — ale nigdy nie poddajemy się na dobre", got)
	}
}

func TestALoopedSingleWaypointRouteSettlesInsteadOfSpinning(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10)}, FollowerOptions{Loop: true})
	if got := f.Step(pos(10, 10), at(0)).Action; got == ActionPath {
		t.Error("stanie na jedynym waypoincie nie może wywoływać planowania trasy")
	}
}

func TestALoopedRouteWhosePointsAreAllWithinToleranceSettlesToo(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10), wp(10, 11), wp(11, 10)}, FollowerOptions{Loop: true})
	if got := f.Step(pos(10, 10), at(0)).Action; got == ActionPath {
		t.Errorf("akcja = %s — każdy punkt jest osiągnięty od razu", got)
	}
}

// Walking tolerance may be loose; a rope used one tile off the rope spot does
// nothing at all.
func TestWaypointAkcjiNieJestOsiagnietyZSasiedniejKratki(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(100, 100, 7, "rope"), wp(100, 100, 6, "walk")},
		FollowerOptions{Tolerance: tol(1)})
	if got := f.Step(pos(101, 100, 7), at(0)).Action; got == ActionTransition {
		t.Error("lina użyta kratkę obok została uznana za osiągnięcie waypointa akcji")
	}
}

func TestWaypointAkcjiJestOsiagnietyZDokladnieTejKratki(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(100, 100, 7, "rope"), wp(100, 100, 6, "walk")},
		FollowerOptions{Tolerance: tol(1)})
	if got := f.Step(pos(100, 100, 7), at(0)).Action; got != ActionTransition {
		t.Errorf("akcja = %s, oczekiwano transition", got)
	}
}

// The stairs tile sits on the current floor; the next waypoint is what says
// which way to step onto it.
func TestInstrukcjaPrzejsciaNiesieNastepnyWaypoint(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(100, 100, 7, "stairs"), wp(101, 100, 6, "walk")}, FollowerOptions{})
	out := f.Step(pos(100, 100, 7), at(0))
	if out.Action != ActionTransition {
		t.Fatalf("akcja = %s", out.Action)
	}
	if out.NextWaypoint == nil || *out.NextWaypoint != wp(101, 100, 6, "walk") {
		t.Errorf("następny waypoint = %+v", out.NextWaypoint)
	}
}

// The executor reads Index to tell the driver which waypoint a floor action
// belongs to; without it every transition looks like waypoint zero.
func TestInstrukcjaPrzejsciaNiesieBiezacyIndeks(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(10, 10, 7, "walk"), wp(100, 100, 7, "rope")},
		FollowerOptions{Tolerance: tol(1)})
	f.Step(pos(10, 10, 7), at(0))
	out := f.Step(pos(100, 100, 7), at(100))
	if out.Action != ActionTransition || out.Index != 1 {
		t.Errorf("wyjście = akcja %s, indeks %d", out.Action, out.Index)
	}
}

func TestOstatniWaypointPrzejsciaNieMaNastepnika(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(100, 100, 7, "rope")}, FollowerOptions{})
	if out := f.Step(pos(100, 100, 7), at(0)); out.NextWaypoint != nil {
		t.Errorf("następnik = %+v, oczekiwano braku", out.NextWaypoint)
	}
}

// A tight walking tolerance paired with a loose action tolerance: only reading
// ActionTolerance, rather than falling back to Tolerance, satisfies this.
func TestActionToleranceMoznaPoluzowacSwiadomie(t *testing.T) {
	f := NewFollower([]route.Waypoint{wp(100, 100, 7, "stairs"), wp(101, 100, 6, "walk")},
		FollowerOptions{Tolerance: tol(0), ActionTolerance: 1})
	if got := f.Step(pos(101, 100, 7), at(0)).Action; got != ActionTransition {
		t.Errorf("akcja = %s, oczekiwano transition", got)
	}
}

// The path request goes out on the tick that blocks a target; the block only
// reaches the store on the next one. The in-flight reply then describes the
// world before the block - installing it would send the bot straight back into
// the tile it just learned about.
func TestTrasaPoliczonaPrzedNauczonaBlokadaJestOdrzucana(t *testing.T) {
	target := wp(10, 10)
	f := NewFollower([]route.Waypoint{target}, FollowerOptions{})
	f.SetMinOverlayRevision(5)
	f.SetPath(&nav.PathResult{Found: true, Status: "ok", Steps: [][2]int{{1, 1}, {2, 2}}, OverlayRevision: 4},
		at(0), &target)
	if f.Path() != nil {
		t.Error("zainstalowano trasę sprzed nauczonej blokady")
	}
	f.SetPath(&nav.PathResult{Found: true, Status: "ok", Steps: [][2]int{{1, 1}, {2, 2}}, OverlayRevision: 5},
		at(0), &target)
	if f.Path() == nil {
		t.Error("trasa policzona na aktualnej nakładce została odrzucona")
	}
}

// A locally produced reply carries no revision. Revision zero is also the
// state of a store that has learned nothing, so nothing can be stale relative
// to it - treating it as "unknown" cannot deadlock the follower.
func TestBrakRewizjiWOdpowiedziNieBlokujeTrasy(t *testing.T) {
	target := wp(10, 10)
	f := NewFollower([]route.Waypoint{target}, FollowerOptions{})
	f.SetMinOverlayRevision(5)
	f.SetPath(&nav.PathResult{Found: true, Status: "ok", Steps: [][2]int{{1, 1}, {2, 2}}}, at(0), &target)
	if f.Path() == nil {
		t.Error("odpowiedź bez rewizji została odrzucona")
	}
}

// Nil tolerance means the default of one tile; an explicit zero means the
// character must land exactly on the waypoint. The two must not collapse into
// each other, which is why the option is a pointer.
func TestExplicitZeroToleranceIsNotTheDefault(t *testing.T) {
	strict := NewFollower([]route.Waypoint{wp(10, 10), wp(20, 10)}, FollowerOptions{Tolerance: tol(0)})
	strict.Step(pos(11, 10), at(0))
	if strict.Index() != 0 {
		t.Errorf("przy tolerancji 0 sąsiednia kratka zaliczyła waypoint (indeks %d)", strict.Index())
	}
	lenient := NewFollower([]route.Waypoint{wp(10, 10), wp(20, 10)}, FollowerOptions{})
	lenient.Step(pos(11, 10), at(0))
	if lenient.Index() != 1 {
		t.Errorf("domyślna tolerancja nie zaliczyła sąsiedniej kratki (indeks %d)", lenient.Index())
	}
}
