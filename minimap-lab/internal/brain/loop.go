package brain

import (
	"context"
	"fmt"
	"image"
	"sync/atomic"
	"time"

	"minimap-lab/internal/frame"
	"minimap-lab/internal/input"
	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/nav"
	"minimap-lab/internal/route"
)

const (
	// FrameTimeout is how long the loop tolerates silence from the camera
	// before disarming. It replaces the panel's heartbeat, and keeps its
	// value: the stream of frames is now the sign of life, and a watchdog
	// must fire even when requests stop entirely.
	FrameTimeout = 750 * time.Millisecond
	watchdogTick = 100 * time.Millisecond
	// logDepth bounds the log carried in every snapshot. The panel shows the
	// tail; the whole history has no business riding along at frame rate.
	logDepth = 50
	// planMargin of zero lets the planner pick its own default.
	planMargin = 0
)

// Controls is the keyboard the loop drives. It is an interface so the loop can
// be exercised without an OS event tap; internal/input.Driver satisfies it.
type Controls interface {
	Armed() bool
	Walk(direction string, observationAge time.Duration) input.Result
	UseHotkey(kind string, observationAge time.Duration) input.Result
	ActionDone()
	Disarm(reason string)
}

// Config is the whole panel-adjustable surface, applied as one piece. It is
// validated and swapped wholesale, like the input config it grows out of: one
// bad field must never silently clear an unrelated one.
type Config struct {
	Zoom       int     `json:"zoom"`
	MarkerX    int     `json:"marker_x"`
	MarkerY    int     `json:"marker_y"`
	MaskRadius int     `json:"mask_radius"`
	MinScore   float64 `json:"min_score"`
	MinGap     float64 `json:"min_gap"`

	Floor          int  `json:"floor"`
	AdjacentFloors bool `json:"adjacent_floors"`
	FloorRadius    int  `json:"floor_radius"`
	// Speed is how many tiles a second the character covers, used to widen the
	// local search window in step with how stale the anchor is.
	Speed int `json:"speed"`

	// The three switches the panel puts in front of the user. Follow only
	// guides; Walk lets the guidance reach the keyboard; FloorActions lets it
	// press a hotkey for a rope or a shovel.
	Follow       bool `json:"follow"`
	Walk         bool `json:"walk"`
	FloorActions bool `json:"floor_actions"`

	RecordAuto  bool `json:"record_auto"`
	RecordEvery int  `json:"record_every"`

	Tolerance       int  `json:"tolerance"`
	ActionTolerance int  `json:"action_tolerance"`
	LoopRoute       bool `json:"loop_route"`
}

func (c Config) validate() error {
	if c.Zoom < 0 || c.Zoom > 8 {
		return fmt.Errorf("skala musi mieścić się w zakresie 0–8 (0 = Auto)")
	}
	if c.Floor < 0 || c.Floor > 15 {
		return fmt.Errorf("piętro musi mieścić się w zakresie 0–15")
	}
	if c.MaskRadius < 0 || c.MaskRadius > 64 {
		return fmt.Errorf("promień maski musi mieścić się w zakresie 0–64")
	}
	if c.MinScore < 0 || c.MinScore > 1 || c.MinGap < 0 || c.MinGap > 1 {
		return fmt.Errorf("progi dopasowania muszą mieścić się w zakresie 0–1")
	}
	if c.FloorRadius < 0 || c.FloorRadius > 32 {
		return fmt.Errorf("promień przejścia musi mieścić się w zakresie 0–32")
	}
	if c.Speed < 1 || c.Speed > 100 {
		return fmt.Errorf("prędkość musi mieścić się w zakresie 1–100 kratek na sekundę")
	}
	if c.RecordEvery < 1 || c.RecordEvery > 100 {
		return fmt.Errorf("odstęp nagrywania musi mieścić się w zakresie 1–100 kratek")
	}
	if c.Tolerance < 0 || c.Tolerance > 32 || c.ActionTolerance < 0 || c.ActionTolerance > 32 {
		return fmt.Errorf("tolerancje muszą mieścić się w zakresie 0–32 kratek")
	}
	return nil
}

// Locator and Planner are interfaces rather than the concrete services so the
// loop can be driven through scripted positions and routes. locate.Service and
// nav.Planner satisfy them as they stand.
type Locator interface {
	Locate(ctx context.Context, im image.Image, req locate.Request) (locate.Result, *mapdata.Atlas, error)
}

type Planner interface {
	Plan(ctx context.Context, blocks *nav.BlockStore, from, to mapdata.Position, margin int) (nav.PathResult, error)
}

type Deps struct {
	Locator Locator
	Planner Planner
	Blocks  *nav.BlockStore
	Driver  Controls
	// Tile answers what the map data says about one tile, for the recording
	// gate. Supplied by the server, which owns the walkability cache.
	Tile func(mapdata.Position) TileVerdict
	Now  func() time.Time
}

type frameEnvelope struct {
	f          frame.Frame
	receivedAt time.Time
}

// Loop is the single owner of every decision the bot makes. Nothing outside
// its goroutine touches the tracker, recorder, executor or follower: frames
// arrive on a one-slot channel, everything else as a command to run in turn,
// and the outside world reads a published snapshot.
type Loop struct {
	deps   Deps
	frames chan frameEnvelope
	cmds   chan func()
	snap   atomic.Pointer[State]

	cfg      Config
	tracker  *Tracker
	recorder *Recorder
	executor *Executor
	follower *Follower

	routeName string
	routeNext string

	lastFrameSeq   uint64
	lastVideoUS    uint64
	hasVideoUS     bool
	captureSession uint64
	searchStopped  bool
	lastFrameAt    time.Time

	position    *mapdata.Position
	positionAt  time.Time
	hasPosition bool
	match       MatchState

	previewRev  uint64
	recSkipped  int
	recWaiting  bool
	wasBlocked  bool
	planPending bool

	lastAction *ActionState
	log        []LogEntry
	logSeq     uint64
	version    uint64
}

func NewLoop(d Deps) *Loop {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Tile == nil {
		// With no walkability oracle every tile is unknown, which makes the
		// recorder wait rather than write points it cannot vouch for.
		d.Tile = func(mapdata.Position) TileVerdict { return TileUnknown }
	}
	l := &Loop{
		deps:     d,
		frames:   make(chan frameEnvelope, 1),
		cmds:     make(chan func(), 16),
		tracker:  NewTracker(),
		recorder: NewRecorder(),
		executor: NewExecutor(ExecutorOptions{}),
		follower: NewFollower(nil, FollowerOptions{}),
		cfg: Config{Zoom: 1, MinScore: .85, MinGap: .015, Speed: 20,
			FloorRadius: 8, RecordEvery: 10, Tolerance: 1},
	}
	l.publish()
	return l
}

// Submit hands one frame to the loop. A newer frame replaces an unread older
// one; frames are never queued, because a backlog would have the bot acting on
// pictures of where the character used to be.
func (l *Loop) Submit(f frame.Frame, receivedAt time.Time) {
	env := frameEnvelope{f: f, receivedAt: receivedAt}
	select {
	case l.frames <- env:
		return
	default:
	}
	select {
	case <-l.frames:
	default:
	}
	select {
	case l.frames <- env:
	default:
		// Another producer refilled the slot in between. Dropping this frame
		// is right: whatever is in there is at least as new.
	}
}

func (l *Loop) Snapshot() *State { return l.snap.Load() }

func (l *Loop) ResetCapture(ctx context.Context, session uint64) {
	l.do(ctx, func() {
		l.captureSession = session
		l.hasVideoUS, l.searchStopped = false, false
		l.tracker.Reset()
		l.position, l.hasPosition = nil, false
		l.match = MatchState{Reason: "Szukam pozycji. Podczas pierwszego odczytu pozostań w miejscu."}
		l.executor.Reset()
		l.publish()
	})
}

// do runs fn on the loop goroutine and waits for it, so callers observe the
// effect rather than racing it.
func (l *Loop) do(ctx context.Context, fn func()) {
	done := make(chan struct{})
	select {
	case l.cmds <- func() { fn(); close(done) }:
	case <-ctx.Done():
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (l *Loop) SetConfig(ctx context.Context, c Config) error {
	if err := c.validate(); err != nil {
		return err
	}
	l.do(ctx, func() {
		if c.Zoom != l.cfg.Zoom || c.MarkerX != l.cfg.MarkerX || c.MarkerY != l.cfg.MarkerY || c.MaskRadius != l.cfg.MaskRadius || c.MinScore != l.cfg.MinScore || c.MinGap != l.cfg.MinGap || abs(c.Floor-l.cfg.Floor) > 1 {
			l.tracker.Reset()
			l.position, l.hasPosition = nil, false
		}
		l.searchStopped = false
		l.cfg = c
		l.recorder.Auto, l.recorder.Every = c.RecordAuto, c.RecordEvery
		// Options are updated in place rather than by rebuilding the follower:
		// a user nudging the tolerance mid-route must not lose their progress.
		l.follower.SetOptions(FollowerOptions{
			Tolerance: &c.Tolerance, ActionTolerance: c.ActionTolerance, Loop: c.LoopRoute,
		})
		l.publish()
	})
	return nil
}

func (l *Loop) SetRoute(ctx context.Context, r route.Route) {
	l.do(ctx, func() {
		l.routeName = r.Name
		l.recorder.SetWaypoints(r.Waypoints)
		l.rebuildFollower()
		l.executor.Reset()
		l.recSkipped = 0
		l.publish()
	})
}

// Route hands back the current route, including anything the recorder has
// added, so the panel can write it to a file.
func (l *Loop) Route(ctx context.Context) route.Route {
	var out route.Route
	l.do(ctx, func() {
		points := l.recorder.Waypoints()
		copied := make([]route.Waypoint, len(points))
		copy(copied, points)
		out = route.Route{Version: route.Version, Name: l.routeName, Waypoints: copied}
	})
	return out
}

// AddManualWaypoint records the tile the character is standing on right now.
func (l *Loop) AddManualWaypoint(ctx context.Context) bool {
	added := false
	l.do(ctx, func() {
		if l.position == nil {
			return
		}
		added = l.recorder.AddManual(*l.position)
		if added {
			l.rebuildFollower()
			l.publish()
		}
	})
	return added
}

func (l *Loop) rebuildFollower() {
	index, finished := l.follower.Index(), l.follower.Finished()
	l.follower = NewFollower(l.recorder.Waypoints(), FollowerOptions{
		Tolerance: &l.cfg.Tolerance, ActionTolerance: l.cfg.ActionTolerance, Loop: l.cfg.LoopRoute,
	})
	if index < len(l.recorder.Waypoints()) && !finished {
		l.follower.SkipTo(index)
	}
}

// Run owns every piece of decision state until ctx is cancelled.
func (l *Loop) Run(ctx context.Context) {
	ticker := time.NewTicker(watchdogTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case env := <-l.frames:
			l.handleFrame(ctx, env)
		case fn := <-l.cmds:
			fn()
		case <-ticker.C:
			l.watchdog()
		}
	}
}

// watchdog disarms when the camera goes quiet. Without it a panel that closed
// mid-route would leave an armed driver waiting for instructions that will
// never come.
func (l *Loop) watchdog() {
	if l.position != nil && l.deps.Now().Sub(l.positionAt) > time.Second {
		l.position, l.hasPosition = nil, false
		l.publish()
	}
	if l.deps.Driver == nil || !l.deps.Driver.Armed() {
		return
	}
	if l.lastFrameAt.IsZero() {
		return
	}
	if l.deps.Now().Sub(l.lastFrameAt) > FrameTimeout {
		l.deps.Driver.Disarm("kamera przestała wysyłać klatki")
		l.executor.Reset()
		l.logf("rozbrojono: kamera przestała wysyłać klatki")
		l.publish()
	}
}

func (l *Loop) handleFrame(ctx context.Context, env frameEnvelope) {
	if l.captureSession != 0 && env.f.Session != l.captureSession {
		return
	}
	l.lastFrameSeq = env.f.Seq
	l.lastFrameAt = env.receivedAt
	im, ok := env.f.Image(frame.RegionMinimap)
	if !ok {
		l.publish()
		return
	}
	// The same video frame sent twice is one observation, not two. Network
	// traffic is no proof that the picture moved.
	if l.hasVideoUS && env.f.VideoTimeUS == l.lastVideoUS {
		l.publish()
		return
	}
	l.lastVideoUS, l.hasVideoUS = env.f.VideoTimeUS, true
	if l.searchStopped {
		l.publish()
		return
	}

	capturedAt := env.receivedAt.Add(-time.Duration(env.f.AgeMS) * time.Millisecond)
	req := locate.Request{
		Options: locate.Options{Zoom: l.cfg.Zoom, MarkerX: l.cfg.MarkerX, MarkerY: l.cfg.MarkerY,
			MaskRadius: l.cfg.MaskRadius, MinScore: l.cfg.MinScore, MinGap: l.cfg.MinGap},
		Floor: l.cfg.Floor, AdjacentFloors: l.cfg.AdjacentFloors, FloorRadius: l.cfg.FloorRadius,
	}
	if hint, ok := l.tracker.Hint(l.deps.Now(), l.cfg.Floor, l.cfg.Zoom, l.cfg.Speed); ok {
		near := hint.Near
		req.Near, req.Radius = &near, hint.Radius
	}
	matchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	result, _, err := l.deps.Locator.Locate(matchCtx, im, req)
	cancel()
	completedAt := l.deps.Now()
	if err != nil {
		l.match = MatchState{Reason: err.Error()}
		l.logf("dopasowanie nie powiodło się: %v", err)
		l.noPosition(capturedAt, completedAt)
		l.searchStopped = true
		l.publish()
		return
	}
	l.tracker.Observe(result, capturedAt, completedAt, completedAt.Sub(capturedAt))
	l.recordMatch(result, completedAt)

	if !result.Found || result.Position == nil {
		// One failed full scan is enough. Keep accepting frames for the camera
		// watchdog, but wait for recalibration or an explicit restart.
		if req.Near == nil {
			l.searchStopped = true
		}
		l.noPosition(capturedAt, completedAt)
		l.publish()
		return
	}
	pos := *result.Position
	// The neighbourhood picture only changes when the tile does, so the panel
	// is told to refetch it then and not on every single frame.
	if l.position == nil || *l.position != pos {
		l.previewRev++
	}
	l.position, l.positionAt, l.hasPosition = &pos, capturedAt, true
	// The floor the tracker believes in follows what was actually found, so a
	// confirmed transition does not leave the next search looking one floor up.
	l.cfg.Floor = pos.Z
	l.cfg.Zoom = result.Zoom

	l.record(pos)
	l.pumpBlocks()
	l.follow(ctx, pos, capturedAt, completedAt)
	l.publish()
}

func (l *Loop) noPosition(capturedAt, now time.Time) {
	l.position, l.hasPosition = nil, false
	l.executor.Observe(nil, capturedAt, now)
}

func (l *Loop) recordMatch(r locate.Result, now time.Time) {
	stats := l.tracker.Stats(now)
	m := MatchState{
		Found: r.Found, Mode: r.Mode, MatchMS: r.MatchMS, Samples: r.Samples,
		SearchPositions: r.SearchPositions, Reason: r.Reason, SearchedFloors: r.SearchedFloors,
		Hz: stats.Hz, Success: stats.Success, RoundTripMS: stats.RoundTripMS,
	}
	if r.Best != nil {
		m.Score = r.Best.Score
	}
	l.match = m
}

// record feeds the recorder, but only for tiles the map agrees a character
// could have been standing on. A position the map calls impassable is proof
// the match was wrong, not a place worth turning into a waypoint no route can
// ever reach - and the user only finds that out much later.
func (l *Loop) record(pos mapdata.Position) {
	l.recorder.Auto, l.recorder.Every = l.cfg.RecordAuto, l.cfg.RecordEvery
	if !l.cfg.RecordAuto {
		l.recWaiting = false
		// Still fed, so the tile before the next floor change is known.
		l.recorder.Observe(pos)
		return
	}
	switch l.deps.Tile(pos) {
	case TileUnknown:
		// The window is one read away; waiting beats writing unverified points.
		l.recWaiting = true
	case TileWalkable:
		l.recWaiting = false
		if l.recorder.Observe(pos) > 0 {
			l.rebuildFollower()
		}
	default:
		l.recWaiting = false
		l.recSkipped++
	}
}

// pumpBlocks ships whatever the executor learned about the map.
func (l *Loop) pumpBlocks() {
	obs, ok := l.executor.TakeObservation()
	if !ok {
		return
	}
	decision := l.deps.Blocks.Observe(obs)
	if decision.Revision > 0 {
		// A path request already in flight predates this block; refusing it
		// stops the bot walking straight back into what it just learned.
		l.follower.SetMinOverlayRevision(decision.Revision)
	}
	l.logf("blokada %s→%s: %s (%s)", tileText(obs.From), tileText(obs.To), decision.Result, decision.Reason)
}

func tileText(p mapdata.Position) string { return fmt.Sprintf("%d,%d,%d", p.X, p.Y, p.Z) }

func (l *Loop) follow(ctx context.Context, pos mapdata.Position, capturedAt, now time.Time) {
	if !l.cfg.Follow || len(l.recorder.Waypoints()) == 0 {
		l.routeNext = ""
		return
	}
	out := l.follower.Step(pos, now)
	l.routeNext = describe(out)
	if out.Action == ActionPath && !l.planPending {
		l.requestPlan(ctx, out)
	}
	// Automatic control is opt-in and needs an armed driver. With the switch
	// off nothing below runs, so guidance-only behaviour is untouched.
	if !l.cfg.Walk || l.deps.Driver == nil || !l.deps.Driver.Armed() {
		return
	}
	l.executor.Observe(&pos, capturedAt, now)
	if l.executor.State().ActionDone {
		l.executor.ClearActionDone()
		l.deps.Driver.ActionDone()
	}
	// A newly blocked target means the executor gave up on it. Without forcing
	// the follower to drop its cached path it keeps producing the very same
	// target forever and the executor keeps refusing it - frozen, never
	// reaching the escalation that would stop the route instead.
	blockedNow := l.executor.State().Blocked
	if blockedNow && !l.wasBlocked {
		l.follower.DropPath()
	}
	l.wasBlocked = blockedNow
	// Decided from the follower's own output, before the executor is asked for
	// anything: asking first would create a pending step this gate then
	// discarded without confirming or resetting it, leaving it to time out
	// into a retry and then a permanent block - stalling the route at that
	// waypoint even after the switch goes back on.
	//
	// Stairs are excluded: the follower reports them as a transition too, but
	// the executor turns them into an ordinary walk (a floor change confirms
	// them, not a hotkey), so pausing floor actions must not refuse to walk
	// onto stairs.
	if out.Action == ActionTransition && out.Waypoint != nil &&
		out.Waypoint.Type != "stairs" && !l.cfg.FloorActions {
		return
	}
	intent, ok := l.executor.IntentFor(&out, now)
	if !ok {
		return
	}
	stepID := l.executor.State().StepID
	age := now.Sub(capturedAt)
	var res input.Result
	switch intent.Action {
	case "walk":
		res = l.deps.Driver.Walk(intent.Direction, age)
	case "transition":
		res = l.deps.Driver.UseHotkey(intent.Type, age)
	default:
		return
	}
	l.noteAction(intent, res, age)
	switch res.Status {
	case "emitted":
		l.executor.Emitted(l.deps.Now(), stepID)
	case "refused":
		// The key was never sent, so the situation did not change: drop only
		// the abandoned attempt, not the counters tracking how many times this
		// route has actually failed.
		l.executor.DropPending()
	}
}

func (l *Loop) requestPlan(ctx context.Context, out Output) {
	if out.Waypoint == nil {
		return
	}
	l.planPending = true
	from, to, asked := out.From, out.To, *out.Waypoint
	go func() {
		res, err := l.deps.Planner.Plan(ctx, l.deps.Blocks, from, to, planMargin)
		apply := func() {
			l.planPending = false
			if err != nil {
				// A local failure carries no overlay revision, which the
				// follower treats as "unknown" rather than stale.
				l.follower.SetPath(&nav.PathResult{Status: "error", Reason: err.Error()}, l.deps.Now(), &asked)
				return
			}
			l.follower.SetPath(&res, l.deps.Now(), &asked)
		}
		select {
		case l.cmds <- apply:
		case <-ctx.Done():
		}
	}()
}

func (l *Loop) noteAction(in Intent, res input.Result, age time.Duration) {
	a := &ActionState{Kind: in.Action, Direction: in.Direction, Type: in.Type,
		Status: res.Status, Key: res.Key, Reason: res.Reason, AgeMS: int(age.Milliseconds())}
	l.lastAction = a
	line := fmt.Sprintf("%s %s%s -> %s", in.Action, in.Direction, in.Type, res.Status)
	if res.Key != "" {
		line += " " + res.Key
	}
	if res.Reason != "" {
		line += ": " + res.Reason
	}
	l.logf("%s (wiek %d ms)", line, a.AgeMS)
}

func describe(out Output) string {
	switch out.Action {
	case ActionDone:
		return "Trasa ukończona."
	case ActionWalk:
		return fmt.Sprintf("Idź %s, pozostało %d kratek.", out.Direction, out.Remaining)
	case ActionTransition:
		return out.Instruction
	case ActionPath:
		return fmt.Sprintf("Szukam trasy do %d, %d, %d.", out.To.X, out.To.Y, out.To.Z)
	case ActionWait:
		return "Czekam na trasę."
	case ActionBlocked:
		return "Trasa zablokowana: " + out.Reason
	}
	return ""
}

func (l *Loop) logf(format string, args ...any) {
	l.logSeq++
	l.log = append(l.log, LogEntry{Seq: l.logSeq, Text: fmt.Sprintf(format, args...)})
	if len(l.log) > logDepth {
		l.log = l.log[len(l.log)-logDepth:]
	}
}

func (l *Loop) publish() {
	l.version++
	s := &State{
		Zoom:            l.cfg.Zoom,
		StateVersion:    l.version,
		LastFrameSeq:    l.lastFrameSeq,
		Match:           l.match,
		Executor:        l.executor.State(),
		PreviewRevision: l.previewRev,
		LastAction:      l.lastAction,
		Recorder: RecorderState{Auto: l.cfg.RecordAuto, Count: len(l.recorder.Waypoints()),
			Skipped: l.recSkipped, Waiting: l.recWaiting},
		Route: RouteState{
			Loaded: len(l.recorder.Waypoints()) > 0, Following: l.cfg.Follow,
			Name: l.routeName, Index: l.follower.Index(), Count: len(l.recorder.Waypoints()),
			Finished: l.follower.Finished(), Next: l.routeNext, PathLen: len(l.follower.Path()),
		},
	}
	if l.deps.Driver != nil {
		s.Armed = l.deps.Driver.Armed()
	}
	if l.position != nil {
		p := *l.position
		s.Position = &p
		age := int(l.deps.Now().Sub(l.positionAt).Milliseconds())
		if age < 0 {
			age = 0
		}
		s.PositionAgeMS = &age
	}
	if len(l.log) > 0 {
		s.Log = make([]LogEntry, len(l.log))
		copy(s.Log, l.log)
	}
	l.snap.Store(s)
}

// The real services must satisfy the interfaces above. Nothing else checks
// until the frame endpoint wires them together, and a signature drift would
// otherwise surface as a build failure in a package that did nothing wrong.
var (
	_ Locator  = (*locate.Service)(nil)
	_ Planner  = (*nav.Planner)(nil)
	_ Controls = (*input.Driver)(nil)
)
