package brain

import (
	"encoding/json"
	"image"
	"image/color"
	"testing"
	"time"

	"minimap-lab/internal/fight"
	"minimap-lab/internal/frame"
	"minimap-lab/internal/heal"
	"minimap-lab/internal/nav"
	"minimap-lab/internal/route"
	"minimap-lab/internal/vision"
)

// fightConfig is the setup every test here starts from: vision calibration
// plus attacking switched on with a plain attack key.
func fightConfig(c *Config) {
	c.Combat = visionCalibration()
	c.Fight = FightConfig{Enabled: true, AttackKey: "space"}
}

// framedBattle paints one row of the battle list, optionally with the attack
// frame around its icon, matching internal/battle/list_test.go's own
// iconFrame helper and visionCalibration()'s icon geometry
// (IconOffsetX=-14, IconOffsetY=-6, IconSize=12).
func framedBattle(c CombatConfig, fill int, targeted bool) *image.NRGBA {
	g := vision.Geometry{Width: c.BattleBarWidth, Height: c.BattleBarHeight, Border: c.BattleBarBorder}
	im := filled(c.Battle.W, c.Battle.H, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
	at := image.Pt(30, 10)
	paintBar(im, g, at, fill, vision.DefaultColors()[0])
	if targeted {
		x0, y0, size := at.X-14, at.Y-6, 12
		for i := 0; i < size; i++ {
			set := func(x, y int) { im.SetNRGBA(x, y, color.NRGBA{R: 0xFF, G: 0x50, B: 0x50, A: 255}) }
			set(x0+i, y0)
			set(x0+i, y0+size-1)
			set(x0, y0+i)
			set(x0+size-1, y0+i)
		}
	}
	return im
}

// emptyBattle is the same region with no rows at all, which is what tells the
// activity machine the fight is over.
func emptyBattle(c CombatConfig) *image.NRGBA {
	return filled(c.Battle.W, c.Battle.H, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
}

// enterFight submits two viewport frames with one nearby creature and an
// untargeted battle row, enough to confirm Activity's entry debounce
// (150ms default) and land in Fighting.
func enterFight(t *testing.T, h *harness) *State {
	t.Helper()
	c := visionCalibration()
	battle := framedBattle(c, 5, false)
	h.submit(t, h.visionFrame(t,
		region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, battle}))
	h.clock.advance(200 * time.Millisecond)
	return h.submit(t, h.visionFrame(t,
		region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, battle}))
}

// leaveFight submits three frames with an empty battle list, which is the
// shortest way past Activity's leave debounce: Presence wants two distinct
// observations AND LeaveFightMS (600 ms by default) between the first of them
// and the confirming one, while a gap over its own 500 ms reset threshold
// would restart the streak - so two frames 600 ms apart can never confirm,
// and three frames 300 ms apart are the cheapest shape that can.
func leaveFight(t *testing.T, h *harness) *State {
	t.Helper()
	empty := emptyBattle(visionCalibration())
	var s *State
	for i := 0; i < 3; i++ {
		h.clock.advance(300 * time.Millisecond)
		s = h.submit(t, h.visionFrame(t, region{frame.RegionBattle, empty}))
	}
	return s
}

func TestFightEntersAndTapsAttackKeyWhenUntargeted(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	s := enterFight(t, h)
	if s.Fight.Activity != "fighting" {
		t.Fatalf("stan walki = %+v", s.Fight)
	}
	if got := h.ctrl.castKeys(); len(got) != 1 || got[0] != "space" {
		t.Fatalf("klawisze ataku = %v, oczekiwano jednego space", got)
	}
}

// The route is the stairs-then-walk shape TestPausingFloorActionsStillWalksOntoStairs
// uses, because it is proven to make the executor press a key on the very
// first frame - without a step actually in flight this test would pass
// whether or not entering combat drops one.
func TestFightEntryDropsPendingStepWithoutBlockLearning(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Follow, c.Walk = true, true
	})
	h.loop.SetRoute(h.ctx, route.Route{Waypoints: []route.Waypoint{
		{X: 1000, Y: 1000, Z: 7, Type: "stairs"}, {X: 1001, Y: 1000, Z: 6, Type: "walk"}}})
	h.at(1000, 1000)
	started := h.tick(t) // establish the position and start a step along the route
	if !started.Executor.Waiting {
		t.Fatalf("test wymaga kroku w locie przed wejściem w walkę: %+v", started.Executor)
	}
	s := enterFight(t, h)
	if s.Executor.Waiting {
		t.Error("wejście w walkę musi porzucić krok w locie, nie zostawiać go w oczekiwaniu")
	}
	if s.Executor.Retries != 0 {
		t.Errorf("porzucony krok obciążył licznik porażek (%d) — to jest nauka blokady, której walka nie może robić",
			s.Executor.Retries)
	}
}

// A spell is the last branch of the frame's priority order, so it only runs
// on a frame that already has a target and therefore no attack tap to make.
// It also pins the wiring SetConfig does: the rules reach the engine, and
// only a confirmed emission is published as the last spell. The third frame
// is needed because each rule carries its own Presence - the crowd has to
// hold for ConfirmMS across two frames of Fighting before it may fire. The
// fourth frame is what proves the confirmed emission started the hotkey's
// cooldown: nothing about the crowd changed, so only the cooldown can be
// stopping a second cast.
func TestFightCastsSpellWhenTargetIsAlreadyFramed(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Fight.Spells = []fight.Rule{{
			Enabled: true, Hotkey: "f3", MinMonsters: 1, Radius: 1, CooldownMS: 2000,
		}}
	})
	h.at(1000, 1000)
	targeted := framedBattle(visionCalibration(), 10, true)
	var s *State
	for i := 0; i < 4; i++ {
		if i > 0 {
			h.clock.advance(200 * time.Millisecond)
		}
		s = h.submit(t, h.visionFrame(t,
			region{frame.RegionViewport, crop(image.Pt(1, 0))},
			region{frame.RegionBattle, targeted}))
	}
	if got := h.ctrl.castKeys(); len(got) != 1 || got[0] != "f3" {
		t.Fatalf("klawisze walki = %v, oczekiwano jednego f3", got)
	}
	if s.Fight.LastSpell != "f3" {
		t.Fatalf("ostatni czar = %q, oczekiwano f3 (%+v)", s.Fight.LastSpell, s.Fight)
	}
	if s.Fight.LastSpellAgeMS == nil {
		t.Fatal("po rzuceniu czaru panel musi dostać jego wiek")
	}
}

// While Fighting the client does the chasing, so follow() keeps the executor's
// view of the world current but never asks it for a step, and the panel is
// told why the route stopped. The battle row is targeted here on purpose:
// with a target in frame neither the targeter nor a spell rule wants a key,
// so the one-key-per-frame gate cannot be what is keeping the bot still -
// only the freeze itself can.
func TestFightFreezesWalkingWhileNoKeyFlies(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Follow, c.Walk = true, true
	})
	h.loop.SetRoute(h.ctx, route.Route{Waypoints: []route.Waypoint{
		{X: 1000, Y: 1000, Z: 7, Type: "stairs"}, {X: 1001, Y: 1000, Z: 6, Type: "walk"}}})
	h.at(1000, 1000)
	enterFight(t, h)
	before, _ := h.ctrl.pressed()
	casts := len(h.ctrl.castKeys())
	h.clock.advance(200 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t,
		region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, framedBattle(visionCalibration(), 10, true)}))
	if s.Fight.Activity != "fighting" {
		t.Fatalf("test wymaga trwającej walki, dostałem %+v", s.Fight)
	}
	if len(h.ctrl.castKeys()) != casts {
		t.Fatalf("test wymaga klatki bez klawisza walki, dostałem %v", h.ctrl.castKeys())
	}
	if keys, _ := h.ctrl.pressed(); len(keys) != len(before) {
		t.Errorf("bot poszedł krokiem w trakcie walki: %v, wcześniej %v", keys, before)
	}
	if s.Route.Next != "Walka." {
		t.Errorf("trasa mówi %q, oczekiwano \"Walka.\"", s.Route.Next)
	}
}

// The route is on so the other half of this test's name can be checked too:
// a pending Escape is a non-heal key, and the frame that spends its key on
// one must not also spend one on a direction.
func TestFightPendingEscapeBlocksStepAndFiresNextFrame(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Follow, c.Walk = true, true
	})
	h.loop.SetRoute(h.ctx, route.Route{Waypoints: []route.Waypoint{
		{X: 1000, Y: 1000, Z: 7, Type: "stairs"}, {X: 1001, Y: 1000, Z: 6, Type: "walk"}}})
	h.at(1000, 1000)
	enterFight(t, h)
	stepped, _ := h.ctrl.pressed()
	h.ctrl.setStatus("refused")
	leaveFight(t, h)
	if h.ctrl.cancelCount() == 0 {
		t.Fatal("wyjście z walki musi spróbować Escape nawet gdy sterownik go odmawia")
	}
	h.ctrl.setStatus("")
	h.clock.advance(100 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionBattle, emptyBattle(visionCalibration())}))
	if s.Fight.EscapeDue {
		t.Fatal("po potwierdzonej emisji Escape escape_due musi zgasnąć")
	}
	if h.ctrl.cancelCount() < 2 {
		t.Fatal("Escape musi być akcją oczekującą - kolejna klatka próbuje ponownie")
	}
	if keys, _ := h.ctrl.pressed(); len(keys) != len(stepped) {
		t.Errorf("klatka z oczekującym Escape poszła też krokiem: %v, wcześniej %v", keys, stepped)
	}
}

// Healing owns the frame it fires on. The health bar is dropped on the second
// frame on purpose: that is the frame the entry debounce confirms on, so it
// is the one where the targeter would otherwise tap the attack key.
func TestFightOneNonHealKeyPerFrame(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Heal = HealConfig{Enabled: true, Rules: []heal.Rule{
			{Enabled: true, Resource: heal.ResourceHP, BelowPct: 60, Hotkey: "f1", CooldownMS: 1000},
		}}
	})
	h.at(1000, 1000)
	c := visionCalibration()
	battle := framedBattle(c, 5, false)
	h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, battle}, barAt(frame.RegionHP, 1)))
	h.clock.advance(200 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, battle}, barAt(frame.RegionHP, 0.1)))
	if s.Fight.Activity != "fighting" {
		t.Fatalf("test wymaga klatki, która wchodzi w walkę: %+v", s.Fight)
	}
	if len(h.ctrl.healKeys()) != 1 {
		t.Fatalf("leczenie powinno polecieć raz, dostałem %v", h.ctrl.healKeys())
	}
	if len(h.ctrl.castKeys()) != 0 {
		t.Fatalf("w tej samej klatce co leczenie żaden klawisz walki nie może polecieć, dostałem %v", h.ctrl.castKeys())
	}
}

// Combat.Enabled() is Viewport AND Crop, not Battle, so the whole vision
// calibration can go while Battle stays populated - a combination
// combatconfig.go explicitly calls legal. observeVision then returns before it
// ever reads the battle region, and a gate that only asked Battle.Empty()
// would let the machine sit in Fighting with nothing able to end it.
func TestFightLosingViewportCalibrationLeavesFighting(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	if s := enterFight(t, h); s.Fight.Activity != "fighting" {
		t.Fatalf("test wymaga trwającej walki, dostałem %+v", s.Fight)
	}
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Combat.Viewport, c.Combat.Crop = Rect{}, Rect{}
	})
	h.clock.advance(200 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionBattle, framedBattle(visionCalibration(), 5, false)}))
	if s.Fight.Activity != "travelling" {
		t.Fatalf("utrata kalibracji widzenia zostawiła pętlę w walce: %+v", s.Fight)
	}
	if h.ctrl.cancelCount() == 0 {
		t.Error("utrata kalibracji w walce musi wysłać Escape")
	}
}

// The battle region can simply stop arriving while the calibration still
// looks healthy - a cut that fails, a rectangle that slid off the captured
// area. An unread list is "unknown, not empty", so it drives no ordinary
// exit; without unreadBattleLimit's backstop nothing else would either, and
// the route would stay frozen at "Walka." indefinitely.
func TestFightLeavesWhenBattleListStopsArriving(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	if s := enterFight(t, h); s.Fight.Activity != "fighting" {
		t.Fatalf("test wymaga trwającej walki, dostałem %+v", s.Fight)
	}
	// Creature still in frame, battle region gone: nothing here says the
	// fight ended, so only the backstop can end it.
	var s *State
	for i := 0; i < 14; i++ {
		h.clock.advance(300 * time.Millisecond)
		s = h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))}))
		if s.Fight.Activity == "travelling" {
			break
		}
	}
	if s.Fight.Activity != "travelling" {
		t.Fatalf("nieczytana battle lista zostawiła pętlę w walce na zawsze: %+v", s.Fight)
	}
	if h.ctrl.cancelCount() == 0 {
		t.Error("wyjście z walki bez battle listy też musi wysłać Escape")
	}
}

// Turning "Atakuj" off mid-fight has to cancel the target: an Escape nobody
// sent leaves the client chasing the creature into the next room. SetConfig
// has no frame to measure an observation age against, so it orders the
// Escape rather than pressing it, and that order is the one field that
// survives its own cleanup - the first frame after attacking goes back on is
// what actually sends the key.
func TestFightDisablingAttackWhileFightingSendsEscape(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	enterFight(t, h)
	h.config(t, func(c *Config) { fightConfig(c); c.Fight.Enabled = false })
	if !h.loop.Snapshot().Fight.EscapeDue {
		t.Fatal("wyłączenie ataku w trakcie walki musi zlecić Escape")
	}
	// Attacking deliberately stays off: every frame from here on returns at
	// the disabled gate, so an Escape left for "later" would wait until the
	// user switched attacking back on - with the client chasing all the while.
	h.clock.advance(100 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t))
	if h.ctrl.cancelCount() == 0 {
		t.Fatal("zlecony Escape musi polecieć bez czekania na ponowne włączenie ataku")
	}
	if s.Fight.EscapeDue {
		t.Fatal("po potwierdzonej emisji Escape escape_due musi zgasnąć")
	}
	if s.Fight.Enabled {
		t.Fatal("test wymaga, by atak przez cały czas pozostał wyłączony")
	}
}

// The empty frames go through leaveFight rather than one frame after a single
// 700 ms jump: one frame can never confirm the leave (Presence wants two
// observations), so the single-frame shape would pass identically whether or
// not the driver was disarmed. Timed properly, this fails the moment the
// disarmed gate is missing - the confirmed leave reaches CancelTarget.
func TestFightDisarmingDoesNotSendEscape(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	enterFight(t, h)
	h.ctrl.Disarm("test")
	s := leaveFight(t, h)
	if h.ctrl.cancelCount() != 0 {
		t.Error("rozbrojony sterownik nie ma jak wysłać Escape - nie wolno nawet próbować liczyć tego jako escape_due")
	}
	if s.Fight.EscapeDue {
		t.Errorf("rozbrojenie nie może zostawić zlecenia Escape, którego nikt nie odbierze: %+v", s.Fight)
	}
	if s.Fight.Activity != "travelling" {
		t.Errorf("rozbrojenie musi od razu wrócić do travelling, dostałem %+v", s.Fight)
	}
}

// main.go defaults -input to "off", which leaves Deps.Driver nil, and nothing
// in validate() stops the panel switching "Atakuj" on in that configuration.
// Every other key site in this package guards for it; without the same guard
// here the first frame panics on the loop goroutine and takes the process
// with it.
func TestFightWithoutADriverDoesNotPanic(t *testing.T) {
	h := newHarness(t)
	// The harness always wires a driver, so this one runs its own loop with
	// Deps.Driver left nil. The harness's own loop simply never gets a frame.
	h.loop = NewLoop(Deps{
		Locator: h.locator, Planner: h.planner,
		Blocks: nav.NewBlockStore(h.clock.now),
		Tile:   h.tileFor, Now: h.clock.now,
	})
	go h.loop.Run(h.ctx)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	s := enterFight(t, h)
	if s.Fight.Activity != "travelling" {
		t.Fatalf("bez sterownika walka nie ma czym stukać, dostałem %+v", s.Fight)
	}
	if s.Fight.Reason != "wykonawca jest rozbrojony" {
		t.Errorf("powód = %q", s.Fight.Reason)
	}
}

// Losing the battle-list calibration mid-fight must not wedge the machine in
// Fighting. The battle list is the only thing that can show the fight ending,
// so without this the route stays frozen at "Walka." for good and only
// switching "Atakuj" off recovers it.
func TestFightLosingBattleCalibrationLeavesFighting(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Follow, c.Walk = true, true
	})
	h.loop.SetRoute(h.ctx, route.Route{Waypoints: []route.Waypoint{
		{X: 1000, Y: 1000, Z: 7, Type: "stairs"}, {X: 1001, Y: 1000, Z: 6, Type: "walk"}}})
	h.at(1000, 1000)
	if s := enterFight(t, h); s.Fight.Activity != "fighting" {
		t.Fatalf("test wymaga trwającej walki, dostałem %+v", s.Fight)
	}
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Combat.Battle = Rect{}
		c.Follow, c.Walk = true, true
	})
	h.clock.advance(200 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))}))
	if s.Fight.Activity != "travelling" {
		t.Fatalf("utrata kalibracji battle listy zostawiła pętlę w walce: %+v", s.Fight)
	}
	if h.ctrl.cancelCount() == 0 {
		t.Error("utrata kalibracji w walce musi wysłać Escape - klient może dalej gonić")
	}
	if s.Route.Next == "Walka." {
		t.Errorf("trasa dalej stoi na walce mimo wyjścia z niej: %q", s.Route.Next)
	}
}

func TestFightMissingBattleCalibrationRefusesWithReason(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		c.Combat = visionCalibration()
		c.Combat.Battle = Rect{}
		c.Fight = FightConfig{Enabled: true, AttackKey: "space"}
	})
	h.at(1000, 1000)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))}))
	if s.Fight.Reason == "" {
		t.Fatal("brak kalibracji battle listy musi opisać powód")
	}
	if len(h.ctrl.castKeys()) != 0 || h.ctrl.cancelCount() != 0 {
		t.Fatal("bez kalibracji battle listy nic nie może polecieć")
	}
}

func TestFightWorksWithoutAPosition(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.locator.miss()
	c := visionCalibration()
	battle := framedBattle(c, 5, false)
	h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, battle}))
	h.clock.advance(200 * time.Millisecond)
	f := h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, battle})
	h.loop.Submit(f, h.clock.now())
	s := h.awaitFrame(t, f.Seq)
	if s.Fight.Activity != "fighting" {
		t.Fatalf("walka musi działać bez znanej pozycji, dostałem %+v", s.Fight)
	}
}

func TestFightStallPausesAndLetsRouteContinue(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Fight.TargetStallMS = 3000
		c.Fight.FightPauseMS = 2000
	})
	h.at(1000, 1000)
	c := visionCalibration()
	targeted := framedBattle(c, 10, true)
	h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, targeted}))
	h.clock.advance(200 * time.Millisecond)
	h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, targeted}))
	// Same HP throughout - the target never loses health.
	h.clock.advance(3100 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, targeted}))
	if s.Fight.Activity != "travelling" {
		t.Fatalf("po zastoju stan musi wrócić do travelling, dostałem %+v", s.Fight)
	}
	if s.Fight.PauseMSLeft == nil {
		t.Fatal("po zastoju panel musi pokazać odliczanie pauzy")
	}
}

// A config document that never mentioned the fight surface is not asking for
// it to be cleared. Until the panel grows a fight module, every click it
// makes sends exactly such a document, and applying it as a zero FightConfig
// would switch attacking off behind the user's back.
func TestFightSetConfigWithoutTheFightKeyKeepsIt(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	if s := enterFight(t, h); s.Fight.Activity != "fighting" {
		t.Fatalf("test wymaga trwającej walki, dostałem %+v", s.Fight)
	}
	// Built the way the panel builds it: the whole brain document, no "fight".
	raw := `{"zoom":1,"min_score":0.85,"min_gap":0.015,"speed":20,"floor_radius":8,
		"record_every":10,"tolerance":1,"floor":7}`
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	c.Combat = visionCalibration()
	if err := h.loop.SetConfig(h.ctx, c); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if s := h.loop.Snapshot(); !s.Fight.Enabled {
		t.Fatalf("brak klucza fight wyłączył atak: %+v", s.Fight)
	}
	h.clock.advance(200 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t,
		region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, framedBattle(visionCalibration(), 5, false)}))
	if s.Fight.Activity != "fighting" {
		t.Fatalf("walka nie przetrwała konfiguracji bez klucza fight: %+v", s.Fight)
	}
}

func TestFightSetConfigDisabledClearsSnapshot(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	enterFight(t, h)
	h.config(t, func(c *Config) { c.Combat = visionCalibration(); c.Fight = FightConfig{} })
	s := h.loop.Snapshot()
	if s.Fight.Enabled || s.Fight.Activity != "travelling" {
		t.Fatalf("SetConfig z enabled=false musi wyczyścić stan natychmiast, dostałem %+v", s.Fight)
	}
}
