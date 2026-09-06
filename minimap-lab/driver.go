package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const (
	holdMS              = 35
	maxObservationAgeMS = 400
	heartbeatTimeoutMS  = 750
	maxTapsPerSecond    = 5
	actionClickDelayMS  = 120
)

// Intent is a single thing the panel wants done. It carries the age of the
// observation rather than its timestamp: performance.now() counts from
// document start and shares no zero with the Go clock.
type Intent struct {
	Session   string `json:"session"`
	Seq       uint64 `json:"seq"`
	Action    string `json:"action"` // "walk" or "transition"
	Direction string `json:"direction,omitempty"`
	Type      string `json:"type,omitempty"` // rope, ladder, hole, shovel
	Waypoint  int    `json:"waypoint,omitempty"`
	AgeMS     int    `json:"observation_age_ms"`
}

// InputResult is named apart from matcher.go's Result, which is the
// minimap match result and predates this feature.
type InputResult struct {
	Status string `json:"status"` // emitted, in_progress, refused, disarmed
	Reason string `json:"reason,omitempty"`
	Key    string `json:"key,omitempty"`
}

type ArmState struct {
	Armed   bool   `json:"armed"`
	Session string `json:"session,omitempty"`
	Target  Window `json:"target"`
	Reason  string `json:"reason,omitempty"`
}

type action struct {
	waypoint int
	kind     string
}

type Driver struct {
	mu  sync.Mutex
	em  Emitter
	now func() time.Time

	armed    bool
	session  string
	target   Window
	reason   string
	lastBeat time.Time

	taps       []time.Time
	lastSeq    uint64
	lastResult InputResult
	inFlight   *action

	// Hotkeys used for floor transitions, filled from the panel config.
	ActionKeys map[string]string
	// ClickAfterHotkey is false for clients whose hotkey is set to "use on
	// yourself"; then the sequence is the tap alone.
	ClickAfterHotkey bool
	// Tile is the player tile in normalised screen coordinates.
	Tile    [2]float64
	HasTile bool
}

func NewDriver(e Emitter) *Driver {
	return &Driver{em: e, now: time.Now, ActionKeys: map[string]string{}}
}

func (d *Driver) Arm() (ArmState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.em.Preflight(); err != nil {
		return ArmState{}, err
	}
	win, err := d.em.Focused()
	if err != nil {
		return ArmState{}, err
	}
	if win.PID == 0 {
		return ArmState{}, fmt.Errorf("nie udało się rozpoznać aktywnego okna")
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ArmState{}, err
	}
	d.armed, d.session, d.target = true, hex.EncodeToString(buf), win
	d.reason, d.lastBeat = "", d.now()
	d.taps, d.lastSeq, d.lastResult, d.inFlight = nil, 0, InputResult{}, nil
	return d.state(), nil
}

func (d *Driver) Disarm(reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.disarmLocked(reason)
}

// disarmLocked releases held keys on the way out. This is the one deliberate
// exception to "never emit without focus": leaving a key down forever is worse
// than one stray key-up event.
func (d *Driver) disarmLocked(reason string) {
	if d.armed {
		d.em.ReleaseAll()
	}
	d.armed, d.session, d.reason, d.inFlight = false, "", reason, nil
}

func (d *Driver) Status() ArmState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state()
}

func (d *Driver) state() ArmState {
	return ArmState{Armed: d.armed, Session: d.session, Target: d.target, Reason: d.reason}
}

// Beat records a sign of life from the panel and reports the current state.
func (d *Driver) Beat(session string) ArmState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.armed && session == d.session {
		if d.expiredLocked() {
			d.disarmLocked("panel przestał odpowiadać")
		} else {
			d.lastBeat = d.now()
		}
	}
	return d.state()
}

// ActionDone clears the in-flight action once the panel confirms the floor
// changed. Without it a repeated 'transition' would stay blocked forever.
func (d *Driver) ActionDone() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inFlight = nil
}

func (d *Driver) Submit(in Intent) InputResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.armed {
		return InputResult{Status: "disarmed", Reason: "wykonawca jest rozbrojony"}
	}
	if in.Session != d.session {
		return InputResult{Status: "refused", Reason: "nieprawidłowy token sesji"}
	}
	if d.expiredLocked() {
		d.disarmLocked("panel przestał odpowiadać")
		return InputResult{Status: "disarmed", Reason: d.reason}
	}
	// Sequence numbers start at 1 - the panel's client increments before
	// sending, so nothing legitimate sends 0. A stuck-at-zero client would
	// otherwise bypass replay protection entirely.
	if in.Seq == 0 {
		return InputResult{Status: "refused", Reason: "brak numeru sekwencyjnego"}
	}
	if in.Seq == d.lastSeq {
		return d.lastResult
	}
	// A Seq lower than the last accepted one is a replay out of order (e.g.
	// 5, 3, 5): refuse it outright rather than let it slip past the equality
	// check above and emit again.
	if in.Seq < d.lastSeq {
		return InputResult{Status: "refused", Reason: "numer sekwencyjny cofnął się"}
	}
	if in.AgeMS < 0 || in.AgeMS > maxObservationAgeMS {
		return d.record(in.Seq, InputResult{Status: "refused",
			Reason: fmt.Sprintf("pozycja starsza niż %d ms", maxObservationAgeMS)})
	}
	// Focus is checked as late as possible. It still is not atomic with the
	// emission that follows; see the spec's "granica gwarancji".
	win, err := d.em.Focused()
	if err != nil || win.PID != d.target.PID {
		d.disarmLocked("okno gry straciło focus")
		return InputResult{Status: "disarmed", Reason: d.reason}
	}
	if !d.allowTapLocked() {
		return d.record(in.Seq, InputResult{Status: "refused", Reason: "limit klawiszy na sekundę"})
	}
	switch in.Action {
	case "walk":
		return d.record(in.Seq, d.walkLocked(in))
	case "transition":
		return d.record(in.Seq, d.transitionLocked(in))
	default:
		return d.record(in.Seq, InputResult{Status: "refused", Reason: "nieznana akcja"})
	}
}

func (d *Driver) walkLocked(in Intent) InputResult {
	if d.inFlight != nil {
		return InputResult{Status: "in_progress", Reason: "trwa akcja zmiany piętra"}
	}
	key, ok := keyForDirection(in.Direction)
	if !ok {
		return InputResult{Status: "refused", Reason: "nieznany kierunek"}
	}
	if err := d.em.TapKey(key, holdMS); err != nil {
		return d.emitterFailureLocked(err)
	}
	d.taps = append(d.taps, d.now())
	return InputResult{Status: "emitted", Key: key}
}

func (d *Driver) transitionLocked(in Intent) InputResult {
	want := action{waypoint: in.Waypoint, kind: in.Type}
	if d.inFlight != nil {
		if *d.inFlight == want {
			return InputResult{Status: "in_progress", Reason: "akcja już trwa"}
		}
		return InputResult{Status: "refused", Reason: "trwa inna akcja"}
	}
	// Stairs are climbed by walking onto them; no item is used. Sending a
	// hotkey here would press whatever else is bound to it.
	if in.Type == "stairs" {
		return InputResult{Status: "refused", Reason: "schody pokonuje się krokiem, nie akcją"}
	}
	if d.ClickAfterHotkey && !d.HasTile {
		return InputResult{Status: "refused", Reason: "brak kalibracji kratki postaci"}
	}
	key, ok := d.ActionKeys[in.Type]
	if !ok || key == "" {
		return InputResult{Status: "refused", Reason: "brak hotkeya dla akcji " + in.Type}
	}
	if err := d.em.TapKey(key, holdMS); err != nil {
		return d.emitterFailureLocked(err)
	}
	d.taps = append(d.taps, d.now())
	if d.ClickAfterHotkey {
		// The client needs a moment to arm the crosshair before the click.
		time.Sleep(actionClickDelayMS * time.Millisecond)
		// That pause is long enough for the player to alt-tab, and a click is
		// far more destructive in a foreign window than a key tap.
		if win, err := d.em.Focused(); err != nil || win.PID != d.target.PID {
			d.disarmLocked("okno gry straciło focus w trakcie akcji")
			return InputResult{Status: "disarmed", Reason: d.reason}
		}
		if err := d.em.Click(d.Tile[0], d.Tile[1]); err != nil {
			return d.emitterFailureLocked(err)
		}
	}
	d.inFlight = &want
	return InputResult{Status: "emitted", Key: key}
}

// Calibrate stores the player tile as a fraction of the shared screen. The
// panel sends normalised coordinates so Retina scaling never reaches the JS
// side; the emitter multiplies them by the screen size in points.
func (d *Driver) Calibrate(nx, ny float64) error {
	if nx < 0 || nx > 1 || ny < 0 || ny > 1 {
		return fmt.Errorf("współrzędne kratki muszą mieścić się w 0–1")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Tile, d.HasTile = [2]float64{nx, ny}, true
	return nil
}

// validActionTypes are the floor transitions a hotkey can be configured for.
// Stairs are excluded on purpose: transitionLocked refuses them outright,
// because stairs are climbed by walking onto them, not by using an item.
var validActionTypes = map[string]bool{"rope": true, "ladder": true, "hole": true, "shovel": true}

// SetActionConfig stores the hotkeys the panel configured for floor actions,
// plus whether the hotkey is used on the character's own tile (no follow-up
// click) or needs a click afterwards. It is the only way ActionKeys and
// ClickAfterHotkey are ever populated outside tests: without it every floor
// action is refused for lack of a hotkey. The whole config is validated
// before anything is written, so a bad request never partially applies.
func (d *Driver) SetActionConfig(keys map[string]string, clickAfterHotkey bool) error {
	clean := make(map[string]string, len(keys))
	for action, key := range keys {
		if !validActionTypes[action] {
			return fmt.Errorf("nieznany typ akcji: %s", action)
		}
		if key == "" {
			continue // an unset hotkey is fine; that action stays refused until configured
		}
		if !hotkeyNames[key] {
			return fmt.Errorf("nieznany klawisz: %s", key)
		}
		clean[action] = key
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ActionKeys = clean
	d.ClickAfterHotkey = clickAfterHotkey
	return nil
}

func (d *Driver) record(seq uint64, r InputResult) InputResult {
	d.lastSeq, d.lastResult = seq, r
	return r
}

// emitterFailureLocked disarms the driver on a failed OS call and produces a
// user-facing result. The Reason always starts in Polish, regardless of what
// the underlying OS binding's error text says; Status() and the returned
// result therefore always agree.
func (d *Driver) emitterFailureLocked(err error) InputResult {
	msg := fmt.Sprintf("nie udało się wysłać zdarzenia: %v", err)
	d.disarmLocked(msg)
	return InputResult{Status: "disarmed", Reason: msg}
}

func (d *Driver) expiredLocked() bool {
	return d.now().Sub(d.lastBeat) > time.Duration(heartbeatTimeoutMS)*time.Millisecond
}

// allowTapLocked keeps a sliding one-second window. Idle time must not bank
// budget for a later burst.
func (d *Driver) allowTapLocked() bool {
	cutoff := d.now().Add(-time.Second)
	kept := d.taps[:0]
	for _, at := range d.taps {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	d.taps = kept
	return len(d.taps) < maxTapsPerSecond
}
