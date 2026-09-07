// Package heal turns two bar readings into at most one hotkey press.
//
// Everything here is a decision and nothing here is an effect: the engine
// never touches the keyboard, never reads a pixel and has no clock of its own.
// That is what lets the whole rule set be tested as a table, and it is why the
// cooldown starts only when the caller reports that a key really went out.
package heal

import (
	"fmt"
	"time"

	"minimap-lab/internal/vitals"
)

const (
	ResourceHP   = "hp"
	ResourceMana = "mana"

	// MinGap is the spacing between any two heals, whatever the rule and
	// whatever the key. The client's bars update a moment after the potion
	// applies, so without it the second frame after a sip still shows the old
	// value and the bot drinks again. It is measured against the frame's
	// capture time, which is what makes it a rule about evidence rather than
	// about clocks: the next attempt needs an observation taken after the gap
	// elapsed, not merely a decision made after it.
	//
	// The value matches the driver's healing reserve of two taps a second, so
	// anything looser would be refused there anyway.
	MinGap = 500 * time.Millisecond
)

// Rule is one line of the user's healing list. Percentages are 0-100, the way
// they are typed in the panel; vitals.Reading counts 0-1, and the conversion
// happens here and only here.
type Rule struct {
	Enabled bool `json:"enabled"`
	// Resource is ResourceHP or ResourceMana: which bar the threshold reads.
	Resource string `json:"resource"`
	// BelowPct fires the rule at or below this reading.
	BelowPct   float64 `json:"below_pct"`
	Hotkey     string  `json:"hotkey"`
	CooldownMS int     `json:"cooldown_ms"`
	// MinManaPct is what the rule costs to use. Zero means "costs nothing",
	// and such a rule deliberately does not care whether the mana bar is
	// readable at all.
	MinManaPct float64 `json:"min_mana_pct"`
}

// Decision is what one frame's evaluation came to. Reason is filled whenever
// nothing fired and there was something worth telling the user - a blocked
// rule, not merely a healthy character.
type Decision struct {
	Fire   bool
	Index  int
	Hotkey string
	Reason string
}

type Engine struct {
	rules   []Rule
	lastKey map[string]time.Time
	lastAny time.Time
	healed  bool
}

func NewEngine() *Engine { return &Engine{lastKey: map[string]time.Time{}} }

// SetRules replaces the whole list. The copy is deliberate: the caller's slice
// comes from a config swap and must not be aliased into the engine.
func (e *Engine) SetRules(rules []Rule) { e.rules = append([]Rule(nil), rules...) }

// Decide picks the first feasible rule from the top - not the first that
// matches its threshold. A rule that matches but cannot be used (no mana, key
// still on cooldown) is passed over, so a cheap potion below a spell still
// saves the character.
func (e *Engine) Decide(hp, mana vitals.Reading, capturedAt time.Time) Decision {
	if len(e.rules) == 0 {
		return Decision{Index: -1, Reason: "brak reguł leczenia"}
	}
	if e.healed && capturedAt.Sub(e.lastAny) < MinGap {
		return Decision{Index: -1, Reason: "odstęp między leczeniami"}
	}
	// A living character never shows exactly zero health. Such a reading is
	// either death - where healing is pointless - or a rectangle that slipped
	// onto the black background, which the bar reader cannot tell apart from
	// an empty bar: zero filled pixels is a perfectly contiguous prefix.
	if hp.OK && hp.Percent <= 0 {
		return Decision{Index: -1, Reason: "HP na zerze — postać martwa albo pasek źle zaznaczony"}
	}
	blocked := ""
	for i, r := range e.rules {
		if !r.Enabled {
			continue
		}
		read := hp
		if r.Resource == ResourceMana {
			read = mana
		}
		if !read.OK {
			blocked = first(blocked, reasonUnreadable(r.Resource, read.Reason))
			continue
		}
		if read.Percent*100 > r.BelowPct {
			continue
		}
		if r.MinManaPct > 0 {
			if !mana.OK {
				blocked = first(blocked, reasonUnreadable(ResourceMana, mana.Reason))
				continue
			}
			if mana.Percent*100 < r.MinManaPct {
				blocked = first(blocked, fmt.Sprintf("za mało many na regułę %d", i+1))
				continue
			}
		}
		if at, ok := e.lastKey[r.Hotkey]; ok &&
			capturedAt.Sub(at) < time.Duration(r.CooldownMS)*time.Millisecond {
			blocked = first(blocked, "cooldown klawisza "+r.Hotkey)
			continue
		}
		return Decision{Fire: true, Index: i, Hotkey: r.Hotkey}
	}
	if blocked == "" {
		blocked = "żadna reguła nie pasuje"
	}
	return Decision{Index: -1, Reason: blocked}
}

// Emitted records that a key really went out. Only this starts a cooldown: a
// decision the driver refused - out of budget, window out of focus - changed
// nothing on screen and must not silence the rule.
func (e *Engine) Emitted(hotkey string, at time.Time) {
	e.lastKey[hotkey] = at
	e.lastAny, e.healed = at, true
}

func reasonUnreadable(resource, why string) string {
	name := "paska HP"
	if resource == ResourceMana {
		name = "paska many"
	}
	if why == "" {
		return "brak odczytu " + name
	}
	return "odczyt " + name + " nieufny: " + why
}

// first keeps the earliest reason. The list is ordered by the user's priority,
// so the first thing that blocked a rule is the one worth showing.
func first(kept, next string) string {
	if kept != "" {
		return kept
	}
	return next
}
