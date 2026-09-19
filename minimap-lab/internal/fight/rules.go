package fight

import (
	"fmt"
	"time"

	"minimap-lab/internal/vitals"
)

// Rule is one line of the user's spell list. The engine that evaluates it
// mirrors internal/heal's: first feasible rule from the top, one hotkey per
// frame, cooldown per hotkey started only on confirmed emission - the same
// mechanism, because in this game everything is a hotkey.
type Rule struct {
	Enabled bool   `json:"enabled"`
	Hotkey  string `json:"hotkey"`
	// MinMonsters is how many confirmed creatures within Radius this rule
	// needs.
	MinMonsters int `json:"min_monsters"`
	// Radius must not exceed the vision layer's decision_radius - a rule
	// asking about tiles the panel never sent would be asking about nothing.
	Radius         float64 `json:"radius"`
	CooldownMS     int     `json:"cooldown_ms"`
	MinManaPct     float64 `json:"min_mana_pct"`
	RequiresTarget bool    `json:"requires_target"`
}

// Crowd is the creature distances brain/fight.go derives from the vision
// layer's bars each frame, after the map sieve - the same sieve that feeds
// MonstersInRange.
type Crowd struct {
	Distances []float64
	// Mixed mirrors CombatState.MixedCrowd: more bars than battle-list rows,
	// proof something in the crop is not a monster.
	Mixed bool
}

// Within counts distances at or inside radius, using the same rounding as
// MonstersInRange: nearest whole tile, so radius 1 sees the 3x3 ring around
// the character and radius 2 sees 5x5.
func (c Crowd) Within(radius float64) int {
	n := 0
	for _, d := range c.Distances {
		if d <= radius+0.5 {
			n++
		}
	}
	return n
}

// Decision is what one frame's evaluation came to.
type Decision struct {
	Fire   bool
	Index  int
	Hotkey string
	Reason string
}

// Engine holds the rule list and one Presence per rule, so a rule that
// changes its own radius or monster count mid-flight does not inherit a
// streak that was measuring something else.
type Engine struct {
	rules    []Rule
	presence []Presence
	lastTap  map[string]time.Time
}

func NewEngine() *Engine { return &Engine{lastTap: map[string]time.Time{}} }

// SetRules replaces the whole list and its debounce state.
func (e *Engine) SetRules(rules []Rule) {
	e.rules = append([]Rule(nil), rules...)
	e.presence = make([]Presence, len(e.rules))
}

// Decide picks the first feasible rule from the top - not the first that
// merely matches its crowd threshold. Emitted starts a hotkey's cooldown
// only once the driver confirms the key really went out.
func (e *Engine) Decide(crowd Crowd, mana vitals.Reading, hasTarget, blockMixed bool,
	capturedAt time.Time, confirm time.Duration) Decision {
	blocked := ""
	for i, r := range e.rules {
		if !r.Enabled {
			// Keep debounce reset while parked, so re-enabling a rule pays
			// the full confirm duration again rather than resuming a streak
			// that predates the switch.
			e.presence[i] = Presence{}
			continue
		}
		count := crowd.Within(r.Radius)
		confirmed := e.presence[i].Observe(count >= r.MinMonsters, capturedAt, confirm)
		if !confirmed {
			blocked = firstFightReason(blocked, fmt.Sprintf("reguła %d: za mało potworów w promieniu", i+1))
			continue
		}
		if r.MinManaPct > 0 {
			if !mana.OK {
				blocked = firstFightReason(blocked, "odczyt many nieufny")
				continue
			}
			if mana.Percent*100 < r.MinManaPct {
				blocked = firstFightReason(blocked, fmt.Sprintf("za mało many na regułę %d", i+1))
				continue
			}
		}
		if at, ok := e.lastTap[r.Hotkey]; ok && capturedAt.Sub(at) < time.Duration(r.CooldownMS)*time.Millisecond {
			blocked = firstFightReason(blocked, "cooldown klawisza "+r.Hotkey)
			continue
		}
		if r.RequiresTarget && !hasTarget {
			blocked = firstFightReason(blocked, fmt.Sprintf("reguła %d: brak celu", i+1))
			continue
		}
		if blockMixed && crowd.Mixed && r.MinMonsters > 1 {
			blocked = firstFightReason(blocked, fmt.Sprintf("reguła %d: mieszany tłum", i+1))
			continue
		}
		return Decision{Fire: true, Index: i, Hotkey: r.Hotkey}
	}
	if blocked == "" {
		blocked = "żadna reguła nie pasuje"
	}
	return Decision{Index: -1, Reason: blocked}
}

// Emitted records that a hotkey really went out. Only this starts its
// cooldown - a decision the driver refused changed nothing on screen.
func (e *Engine) Emitted(hotkey string, at time.Time) {
	e.lastTap[hotkey] = at
}

// firstFightReason keeps the earliest reason, matching internal/heal's
// "first" helper: rules are ordered by the user's priority, so the first
// thing that blocked evaluation is the one worth showing.
func firstFightReason(kept, next string) string {
	if kept != "" {
		return kept
	}
	return next
}
