package core

import (
	"log"
	"sync"
	"time"

	"github.com/anthropics/tibiabot/internal/combat"
	"github.com/anthropics/tibiabot/internal/navigation"
	"github.com/anthropics/tibiabot/internal/survival"
	"github.com/anthropics/tibiabot/internal/vision"
	"gocv.io/x/gocv"
)

type BotState int

const (
	Idle BotState = iota
	Walking
	Combat
	Looting
	Healing
	Refill
)

func (s BotState) String() string {
	switch s {
	case Idle:
		return "IDLE"
	case Walking:
		return "WALKING"
	case Combat:
		return "COMBAT"
	case Looting:
		return "LOOTING"
	case Healing:
		return "HEALING"
	case Refill:
		return "REFILL"
	default:
		return "UNKNOWN"
	}
}

type BotStateMachine struct {
	mu            sync.RWMutex
	state         BotState
	running       bool
	position      [3]int
	healthPct     float64
	manaPct       float64
	kills         int
	waypointIndex int
	events        []map[string]interface{}
}

func NewBotStateMachine() *BotStateMachine {
	return &BotStateMachine{
		state:     Idle,
		position:  [3]int{0, 0, 7},
		healthPct: 100.0,
		manaPct:   100.0,
	}
}

func (sm *BotStateMachine) State() BotState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

func (sm *BotStateMachine) Running() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.running
}

func (sm *BotStateMachine) Start() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.running = true
	sm.state = Walking
	sm.logEvent("Bot started")
}

func (sm *BotStateMachine) Stop() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.running = false
	sm.state = Idle
	sm.logEvent("Bot stopped")
}

func (sm *BotStateMachine) Transition(newState BotState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	old := sm.state
	sm.state = newState
	sm.logEvent("State: " + old.String() + " -> " + newState.String())
}

func (sm *BotStateMachine) GetPosition() [3]int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.position
}

func (sm *BotStateMachine) SetPosition(pos [3]int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.position = pos
}

func (sm *BotStateMachine) GetHealthMana() (float64, float64) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.healthPct, sm.manaPct
}

func (sm *BotStateMachine) SetHealthMana(health, mana float64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.healthPct = health
	sm.manaPct = mana
}

func (sm *BotStateMachine) IncrementKills() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.kills++
}

func (sm *BotStateMachine) AdvanceWaypoint(numWaypoints int) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.waypointIndex = (sm.waypointIndex + 1) % numWaypoints
	return sm.waypointIndex
}

func (sm *BotStateMachine) Status() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return map[string]interface{}{
		"state":          sm.state.String(),
		"running":        sm.running,
		"position":       sm.position,
		"health_pct":     sm.healthPct,
		"mana_pct":       sm.manaPct,
		"kills":          sm.kills,
		"waypoint_index": sm.waypointIndex,
	}
}

func (sm *BotStateMachine) logEvent(message string) {
	event := map[string]interface{}{
		"time":    float64(time.Now().Unix()),
		"message": message,
	}
	sm.events = append(sm.events, event)
	if len(sm.events) > 500 {
		sm.events = sm.events[len(sm.events)-250:]
	}
	log.Println(message)
}

// CaveBot is the full orchestrator: captures frames and runs the state machine.
type CaveBot struct {
	Config        *BotConfig
	atlas         *navigation.Atlas
	SM            *BotStateMachine
	capture       *vision.FrameCapture
	locator       *vision.MinimapLocator
	targeting     *combat.TargetingSystem
	healer        *survival.Healer
	supplies      *survival.SupplyTracker
	spellRotation *combat.SpellRotation
	currentPath   [][2]int
	pathIndex     int
	stopOnce      sync.Once
	stopCh        chan struct{}
	onStatus      func(map[string]interface{})
}

func NewCaveBot(config *BotConfig, atlas *navigation.Atlas, onStatus func(map[string]interface{})) *CaveBot {
	return &CaveBot{
		Config:  config,
		atlas:   atlas,
		SM:      NewBotStateMachine(),
		capture: vision.NewFrameCapture(config.Capture.CameraIndex),
		locator: vision.NewMinimapLocator(atlas),
		targeting: &combat.TargetingSystem{},
		healer: survival.NewHealer(
			float64(config.Healing.HealthThreshold),
			float64(config.Healing.ManaThreshold),
			config.Healing.HealthCooldown,
			config.Healing.ManaCooldown,
		),
		supplies: survival.NewSupplyTracker(
			config.Supplies.MaxPotions,
			config.Supplies.RefillThreshold,
		),
		spellRotation: combat.NewSpellRotation(
			config.Combat.SpellKeys,
			config.Combat.SpellCooldowns,
		),
		stopCh:   make(chan struct{}),
		onStatus: onStatus,
	}
}

func (cb *CaveBot) Start() {
	if !cb.capture.Open() {
		log.Println("Failed to open camera")
		return
	}
	cb.SM.Start()
	go cb.runLoop()
}

func (cb *CaveBot) Stop() {
	cb.stopOnce.Do(func() {
		close(cb.stopCh)
		cb.SM.Stop()
		cb.capture.Close()
		if cb.locator != nil {
			cb.locator.Close()
		}
	})
}

func (cb *CaveBot) Status() map[string]interface{} {
	return cb.SM.Status()
}

func (cb *CaveBot) runLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	frame := gocv.NewMat()
	defer frame.Close()

	for {
		select {
		case <-cb.stopCh:
			return
		case <-ticker.C:
			cb.tick(&frame)
			if cb.onStatus != nil {
				cb.onStatus(cb.SM.Status())
			}
		}
	}
}

func (cb *CaveBot) tick(frame *gocv.Mat) {
	if !cb.capture.Read(frame) {
		return
	}

	cfg := cb.Config

	healthBar := vision.CropRegion(*frame, cfg.Capture.HealthBarRegion)
	manaBar := vision.CropRegion(*frame, cfg.Capture.ManaBarRegion)
	hp := vision.ReadBarPercentage(healthBar, 2, 100)
	mp := vision.ReadBarPercentage(manaBar, 0, 100)
	cb.SM.SetHealthMana(hp, mp)

	healAction := cb.healer.Check(hp, mp)
	if healAction == "health" {
		PressKey(cfg.Healing.HealthPotKey)
		cb.healer.MarkUsed("health")
		cb.supplies.UsePotion()
	} else if healAction == "mana" {
		PressKey(cfg.Healing.ManaPotKey)
		cb.healer.MarkUsed("mana")
		cb.supplies.UsePotion()
	}

	if cb.supplies.NeedsRefill() && cb.SM.State() != Refill {
		cb.SM.Transition(Refill)
		cb.planPathToWaypoint(cfg.Supplies.DepotWaypointIndex)
		return
	}

	minimapImg := vision.CropRegion(*frame, cfg.Capture.MinimapRegion)
	minimapRGB := gocv.NewMat()
	gocv.CvtColor(minimapImg, &minimapRGB, gocv.ColorBGRToRGB)

	pos := cb.SM.GetPosition()
	x, y, _, found := cb.locator.Locate(minimapRGB, pos[2])
	minimapRGB.Close()
	if found {
		cb.SM.SetPosition([3]int{x, y, pos[2]})
	}

	state := cb.SM.State()
	switch state {
	case Walking, Refill:
		cb.tickWalking(frame)
	case Combat:
		cb.tickCombat(frame)
	case Looting:
		cb.SM.Transition(Walking)
	}
}

func (cb *CaveBot) tickWalking(frame *gocv.Mat) {
	cfg := cb.Config

	if cb.SM.State() != Refill {
		battleRegion := vision.CropRegion(*frame, cfg.Capture.BattleListRegion)
		if cb.targeting.HasTarget(battleRegion) {
			cb.SM.Transition(Combat)
			combat.Attack(cfg.Combat.AttackKey, PressKey)
			return
		}
	}

	if cb.pathIndex < len(cb.currentPath) {
		pos := cb.currentPath[cb.pathIndex]
		curPos := cb.SM.GetPosition()
		cx, cy := curPos[0], curPos[1]
		dx := clamp(pos[0]-cx, -1, 1)
		dy := clamp(pos[1]-cy, -1, 1)
		if dx == 0 && dy == 0 {
			cb.pathIndex++
		} else {
			var keys []string
			if dy < 0 {
				keys = append(keys, "up")
			} else if dy > 0 {
				keys = append(keys, "down")
			}
			if dx < 0 {
				keys = append(keys, "left")
			} else if dx > 0 {
				keys = append(keys, "right")
			}
			if len(keys) > 0 {
				PressKeysSimultaneous(keys)
			}
		}
	} else {
		cb.advanceWaypoint()
	}
}

func (cb *CaveBot) tickCombat(frame *gocv.Mat) {
	cfg := cb.Config
	battleRegion := vision.CropRegion(*frame, cfg.Capture.BattleListRegion)
	if !cb.targeting.HasTarget(battleRegion) {
		cb.SM.IncrementKills()
		cb.SM.Transition(Looting)
		return
	}
	cb.spellRotation.CastNext(PressKey)
}

func (cb *CaveBot) advanceWaypoint() {
	waypoints := cb.Config.Waypoints.List
	if len(waypoints) == 0 {
		return
	}
	idx := cb.SM.AdvanceWaypoint(len(waypoints))
	if !cb.Config.Waypoints.Loop && idx == 0 {
		cb.SM.Stop()
		return
	}
	cb.planPathToWaypoint(idx)
}

func (cb *CaveBot) planPathToWaypoint(wpIndex int) {
	waypoints := cb.Config.Waypoints.List
	if wpIndex >= len(waypoints) {
		return
	}
	wp := waypoints[wpIndex]
	start := cb.SM.GetPosition()
	goal := [3]int{wp.X, wp.Y, wp.Z}
	path := navigation.FindPath(cb.atlas, start, goal, 100000)
	if path != nil {
		cb.currentPath = path
		cb.pathIndex = 0
	} else {
		log.Printf("No path found to waypoint %d", wpIndex)
		cb.currentPath = nil
		cb.pathIndex = 0
	}
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
