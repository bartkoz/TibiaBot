package brain

import (
	"context"
	"encoding/binary"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"minimap-lab/internal/frame"
	"minimap-lab/internal/input"
	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/nav"
	"minimap-lab/internal/route"
)

// --- atrapy ---

// Every double here is mutex-guarded: the loop runs on its own goroutine, so a
// test reading what it recorded is reading across goroutines whether the code
// looks like it or not.
type fakeControls struct {
	mu           sync.Mutex
	armed        bool
	keys         []string
	hotkeys      []string
	actionsDone  int
	disarmReason string
	nextStatus   string
}

func (c *fakeControls) Armed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.armed
}

func (c *fakeControls) resultLocked(key string) input.Result {
	status := c.nextStatus
	if status == "" {
		status = "emitted"
	}
	return input.Result{Status: status, Key: key}
}

func (c *fakeControls) Walk(direction string, _ time.Duration) input.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = append(c.keys, direction)
	return c.resultLocked(direction)
}

func (c *fakeControls) UseHotkey(kind string, _ time.Duration) input.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hotkeys = append(c.hotkeys, kind)
	return c.resultLocked(kind)
}

func (c *fakeControls) ActionDone() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.actionsDone++
}

func (c *fakeControls) Disarm(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.armed, c.disarmReason = false, reason
}

func (c *fakeControls) pressed() ([]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.keys...), append([]string(nil), c.hotkeys...)
}

func (c *fakeControls) reason() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disarmReason
}

// scriptedLocator answers with whatever position the test last set.
type scriptedLocator struct {
	mu    sync.Mutex
	pos   *mapdata.Position
	calls int
}

func (s *scriptedLocator) set(p mapdata.Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pos = &p
}

func (s *scriptedLocator) matches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *scriptedLocator) Locate(context.Context, image.Image, locate.Request) (locate.Result, *mapdata.Atlas, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.pos == nil {
		return locate.Result{Found: false, Mode: "local", Reason: "brak"}, nil, nil
	}
	p := *s.pos
	return locate.Result{Found: true, Mode: "local", Position: &p, Zoom: 1,
		Best: &locate.Candidate{Score: .99}}, nil, nil
}

// scriptedPlanner returns a straight eastward path, or whatever was set.
type scriptedPlanner struct {
	mu     sync.Mutex
	result *nav.PathResult
	calls  int
}

func (p *scriptedPlanner) Plan(_ context.Context, _ *nav.BlockStore, from, to mapdata.Position, _ int) (nav.PathResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.result != nil {
		return *p.result, nil
	}
	steps := [][2]int{}
	for x := from.X; x <= to.X; x++ {
		steps = append(steps, [2]int{x, from.Y})
	}
	return nav.PathResult{Found: true, Status: "ok", Steps: steps, OverlayRevision: 1}, nil
}

type loopClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *loopClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *loopClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

type harness struct {
	loop    *Loop
	ctrl    *fakeControls
	locator *scriptedLocator
	planner *scriptedPlanner
	clock   *loopClock
	cancel  context.CancelFunc
	ctx     context.Context
	seq     uint64
	videoUS uint64

	tileMu sync.Mutex
	tile   TileVerdict
}

func (h *harness) setTile(v TileVerdict) {
	h.tileMu.Lock()
	defer h.tileMu.Unlock()
	h.tile = v
}

func (h *harness) tileFor(mapdata.Position) TileVerdict {
	h.tileMu.Lock()
	defer h.tileMu.Unlock()
	return h.tile
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		ctrl:    &fakeControls{armed: true},
		locator: &scriptedLocator{},
		planner: &scriptedPlanner{},
		clock:   &loopClock{at: base},
		tile:    TileWalkable,
	}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	h.loop = NewLoop(Deps{
		Locator: h.locator, Planner: h.planner,
		Blocks: nav.NewBlockStore(h.clock.now), Driver: h.ctrl,
		Tile: h.tileFor,
		Now:  h.clock.now,
	})
	go h.loop.Run(h.ctx)
	t.Cleanup(h.cancel)
	return h
}

// minimapFrame builds the smallest body carrying a minimap region.
func (h *harness) minimapFrame() frame.Frame {
	h.seq++
	h.videoUS += 100_000
	body := make([]byte, frame.HeaderSize+frame.RegionHeader)
	copy(body[0:4], frame.Magic)
	body[4], body[5] = frame.FormatVersion, 1
	binary.LittleEndian.PutUint64(body[16:], h.seq)
	binary.LittleEndian.PutUint64(body[24:], h.videoUS)
	hdr := body[frame.HeaderSize:]
	hdr[0] = byte(frame.RegionMinimap)
	binary.LittleEndian.PutUint16(hdr[4:], 2)
	binary.LittleEndian.PutUint16(hdr[6:], 2)
	binary.LittleEndian.PutUint32(hdr[8:], 16)
	body = append(body, make([]byte, 16)...)
	f, err := frame.Parse(body)
	if err != nil {
		panic(err)
	}
	return f
}

// tick submits one frame and waits until the loop has finished with it.
func (h *harness) tick(t *testing.T) *State {
	t.Helper()
	f := h.minimapFrame()
	h.loop.Submit(f, h.clock.now())
	deadline := time.Now().Add(2 * time.Second)
	for {
		s := h.loop.Snapshot()
		if s.LastFrameSeq == f.Seq {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("pętla nie przetworzyła klatki %d", f.Seq)
		}
		time.Sleep(time.Millisecond)
	}
}

func (h *harness) config(t *testing.T, edit func(*Config)) {
	t.Helper()
	c := Config{Zoom: 1, MinScore: .85, MinGap: .015, Speed: 20, FloorRadius: 8,
		RecordEvery: 10, Tolerance: 1, Floor: 7}
	edit(&c)
	if err := h.loop.SetConfig(h.ctx, c); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
}

func (h *harness) at(x, y int, z ...int) { h.locator.set(pos(x, y, z...)) }

// --- scenariusze ---

// Asking the executor before the gate creates a pending step the gate then
// discards without confirming or resetting it, which times out into a retry
// and then a permanent block - stalling the route at that waypoint even after
// the switch goes back on.
func TestFloorActionGateRefusesBeforeAPendingStepExists(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Follow, c.Walk, c.FloorActions = true, true, false })
	h.loop.SetRoute(h.ctx, route.Route{Waypoints: []route.Waypoint{
		{X: 100, Y: 100, Z: 7, Type: "rope"}, {X: 100, Y: 100, Z: 6, Type: "walk"}}})
	h.at(100, 100, 7)

	s := h.tick(t)

	if keys, hotkeys := h.ctrl.pressed(); len(keys) != 0 || len(hotkeys) != 0 {
		t.Errorf("wysłano klawisze mimo wyłączonych akcji pięter: %v %v", keys, hotkeys)
	}
	if s.Executor.Waiting {
		t.Error("został krok w toku, który wygaśnie w ponowienie i trwałą blokadę")
	}
}

// Stairs are reported by the follower as a transition, but the executor turns
// them into an ordinary walk, so pausing floor actions must not refuse them.
func TestPausingFloorActionsStillWalksOntoStairs(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Follow, c.Walk, c.FloorActions = true, true, false })
	h.loop.SetRoute(h.ctx, route.Route{Waypoints: []route.Waypoint{
		{X: 100, Y: 100, Z: 7, Type: "stairs"}, {X: 101, Y: 100, Z: 6, Type: "walk"}}})
	h.at(100, 100, 7)

	h.tick(t)

	keys, _ := h.ctrl.pressed()
	if len(keys) == 0 {
		t.Fatal("nie wysłano kroku na schody")
	}
	if keys[0] != "E" {
		t.Errorf("kierunek = %q, oczekiwano E", keys[0])
	}
}

// A position the map calls impassable is proof the match was wrong, not a
// place worth recording as a waypoint no route can ever reach.
func TestImpassableTileIsNotRecordedAsAWaypoint(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.RecordAuto = true })
	h.setTile(TileBlocked)
	h.at(100, 100, 7)

	s := h.tick(t)

	if s.Recorder.Count != 0 {
		t.Errorf("nagrano %d waypointów na kratce nieprzechodniej", s.Recorder.Count)
	}
	if s.Recorder.Skipped != 1 {
		t.Errorf("pominięto %d, oczekiwano 1", s.Recorder.Skipped)
	}
}

// With no walkability data yet the recorder holds off rather than writing
// points it cannot vouch for - and says so, instead of silently counting a
// skip that never happened.
func TestRecordingWaitsForWalkabilityData(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.RecordAuto = true })
	h.setTile(TileUnknown)
	h.at(100, 100, 7)

	s := h.tick(t)

	if s.Recorder.Count != 0 || s.Recorder.Skipped != 0 {
		t.Errorf("nagrano %d, pominięto %d — oczekiwano czekania", s.Recorder.Count, s.Recorder.Skipped)
	}
	if !s.Recorder.Waiting {
		t.Error("stan nie mówi, że recorder czeka na dane przechodniości")
	}
}

func TestWalkableTileIsRecorded(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.RecordAuto = true })
	h.at(100, 100, 7)

	s := h.tick(t)

	if s.Recorder.Count != 1 {
		t.Errorf("nagrano %d waypointów, oczekiwano 1", s.Recorder.Count)
	}
}

// The watchdog replaces the panel's heartbeat and must fire even when requests
// stop entirely - that is the case it exists for.
func TestWatchdogDisarmsWhenFramesStop(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Follow = false })
	h.at(100, 100, 7)
	h.tick(t)

	h.clock.advance(FrameTimeout + 100*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for h.ctrl.Armed() {
		if time.Now().After(deadline) {
			t.Fatal("watchdog nie rozbroił wykonawcy mimo ciszy kamery")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if reason := h.ctrl.reason(); !strings.Contains(reason, "klatk") {
		t.Errorf("powód rozbrojenia = %q", reason)
	}
}

// The same video frame posted twice is one observation. Without this a frozen
// capture would look like a stream of fresh readings.
func TestRepeatedVideoFrameIsNotASecondObservation(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {})
	h.at(100, 100, 7)
	h.tick(t)
	before := h.locator.matches()

	f := h.minimapFrame()
	// Same picture as the previous frame, only a new sequence number.
	h.videoUS -= 100_000
	f2 := h.minimapFrame()
	_ = f
	h.loop.Submit(f2, h.clock.now())
	deadline := time.Now().Add(2 * time.Second)
	for h.loop.Snapshot().LastFrameSeq != f2.Seq {
		if time.Now().After(deadline) {
			t.Fatal("pętla nie przetworzyła klatki")
		}
		time.Sleep(time.Millisecond)
	}
	if got := h.locator.matches(); got != before+1 {
		t.Errorf("dopasowań = %d, oczekiwano jednego więcej niż %d", got, before)
	}
}

// The snapshot rides on every single frame, so a thousand-point route must not
// travel with it.
func TestSnapshotDoesNotCarryWaypoints(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {})
	points := make([]route.Waypoint, 1000)
	for i := range points {
		points[i] = route.Waypoint{X: 100 + i, Y: 100, Z: 7, Type: "walk"}
	}
	h.loop.SetRoute(h.ctx, route.Route{Name: "duża", Waypoints: points})
	h.at(100, 100, 7)

	s := h.tick(t)

	if s.Route.Count != 1000 {
		t.Fatalf("liczba punktów = %d", s.Route.Count)
	}
	data, err := marshalState(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 4096 {
		t.Errorf("snapshot ma %d bajtów — trasa jedzie razem z nim", len(data))
	}
}

// The route the panel saves has to include what the recorder added.
func TestRouteHandsBackRecordedWaypoints(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.RecordAuto = true })
	h.at(100, 100, 7)
	h.tick(t)

	r := h.loop.Route(h.ctx)

	if len(r.Waypoints) != 1 || r.Waypoints[0].X != 100 {
		t.Errorf("trasa = %+v", r.Waypoints)
	}
}

func TestSetConfigIsRejectedWholeOrAppliedWhole(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Floor, c.MarkerX = 7, 52 })

	bad := Config{Zoom: 99, MinScore: .85, MinGap: .015, Speed: 20, FloorRadius: 8,
		RecordEvery: 10, Tolerance: 1, Floor: 3, MarkerX: 999}
	if err := h.loop.SetConfig(h.ctx, bad); err == nil {
		t.Fatal("przyjęto konfigurację ze złą skalą")
	}
	h.at(100, 100, 7)
	h.tick(t)
	// Nothing from the rejected config may have leaked through.
	if got := h.loop.Snapshot(); got.Match.Found != true {
		t.Errorf("stan po odrzuconej konfiguracji: %+v", got.Match)
	}
}

// A newly blocked target must force the follower to drop its cached path.
// Without it the follower keeps producing the very same target forever and the
// executor keeps refusing it - frozen, never reaching the escalation that
// would stop the route instead.
func TestNewlyBlockedTargetDropsTheCachedPath(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Follow, c.Walk = true, true })
	h.loop.SetRoute(h.ctx, route.Route{Waypoints: []route.Waypoint{{X: 110, Y: 100, Z: 7, Type: "walk"}}})
	h.at(100, 100, 7)

	// First tick asks for a path; the second walks it.
	h.tick(t)
	h.clock.advance(50 * time.Millisecond)
	s := h.tick(t)
	if s.Route.PathLen == 0 {
		t.Fatalf("brak trasy do porzucenia: %+v", s.Route)
	}

	// The character never moves, so the step times out twice and the target
	// gets blocked.
	for i := 0; i < 8 && !h.loop.Snapshot().Executor.Blocked; i++ {
		h.clock.advance(2 * time.Second)
		s = h.tick(t)
	}
	if !s.Executor.Blocked {
		t.Fatalf("cel nie został zablokowany po dwóch porażkach: %+v", s.Executor)
	}
	// The block is created inside IntentFor, which runs after the gate that
	// reads it, so the drop lands on the next reading rather than this one.
	// The original panel behaved the same way.
	h.clock.advance(50 * time.Millisecond)
	s = h.tick(t)
	if s.Route.PathLen != 0 {
		t.Errorf("trasa z pamięci przetrwała zablokowanie celu (%d kratek)", s.Route.PathLen)
	}
}

// The neighbourhood preview is a separate request now, so the panel needs to
// be told when it is worth making again - and only then.
func TestPreviewRevisionChangesOnlyWhenTheTileDoes(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {})
	h.at(100, 100, 7)
	first := h.tick(t).PreviewRevision
	if first == 0 {
		t.Fatal("pierwsza pozycja nie podniosła rewizji podglądu")
	}
	if same := h.tick(t).PreviewRevision; same != first {
		t.Errorf("rewizja = %d, oczekiwano bez zmiany na tej samej kratce", same)
	}
	h.at(101, 100, 7)
	if moved := h.tick(t).PreviewRevision; moved == first {
		t.Error("zmiana kratki nie podniosła rewizji podglądu")
	}
}
