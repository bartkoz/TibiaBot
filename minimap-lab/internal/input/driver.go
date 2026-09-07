package input

import (
	"fmt"
	"sync"
	"time"
)

const (
	holdMS = 35
	// DefaultMaxObservationAgeMS is the -stale-ms default. A machine whose
	// capture-to-reply round trip (the panel's "Cały odczyt" telemetry)
	// exceeds this would otherwise have every single step refused.
	DefaultMaxObservationAgeMS = 400
	// MinStaleMS/MaxStaleMS bound -stale-ms to a range the system can
	// actually use, so an unvalidated flag cannot silently switch off the
	// freshness gate or disable every step:
	//   - MinStaleMS is the fastest tracking interval (10 Hz = 100ms).
	//     Tighter than that leaves no round-trip budget at all - every
	//     single observation would already be older than the threshold.
	//   - MaxStaleMS stays well under heartbeatTimeoutMS (750ms): at or
	//     above it, a dead heartbeat would already have disarmed the
	//     session before an observation could ever get that stale, so the
	//     freshness gate would stop meaning anything.
	MinStaleMS = 100
	// MaxStaleMS stays well under the brain loop's frame watchdog: at or above
	// it, a silent camera would already have disarmed the session before an
	// observation could ever get that stale, so the freshness gate would stop
	// meaning anything.
	MaxStaleMS = 600
	// Sliding one-second budgets. The ceiling is hard; the per-purpose rows
	// exist so one purpose cannot eat the whole thing. Their sum exceeds the
	// ceiling on purpose - they are a fence, not an allocation.
	//
	// maxNonHealTapsPerSecond is what actually fences the healing reserve off:
	// without it, walking and floor actions could take three each, fill the
	// ceiling between them, and leave the reserve existing only on paper.
	maxTapsPerSecond        = 8
	maxWalkTapsPerSecond    = 3
	maxActionTapsPerSecond  = 3
	maxHealTapsPerSecond    = 2
	maxNonHealTapsPerSecond = 6
	actionClickDelayMS      = 120
)

// ValidateStaleMS refuses a -stale-ms value outside MinStaleMS-MaxStaleMS.
// An unvalidated flag is worse than a fixed constant: an extra digit (8000
// instead of 800) would silently widen the freshness gate with no warning,
// and a negative value would make the driver refuse every single step while
// logging the confusing "pozycja starsza niż -N ms".
func ValidateStaleMS(ms int) error {
	if ms < MinStaleMS || ms > MaxStaleMS {
		return fmt.Errorf("-stale-ms musi mieścić się w zakresie %d–%d ms, podano %d", MinStaleMS, MaxStaleMS, ms)
	}
	return nil
}

// Result is what the driver did with one request. The locate package has a
// Result of its own for minimap matches; the package qualifier keeps the two
// apart at every call site.
type Result struct {
	Status string `json:"status"` // emitted, in_progress, refused, disarmed
	Reason string `json:"reason,omitempty"`
	Key    string `json:"key,omitempty"`
}

type ArmState struct {
	Armed  bool   `json:"armed"`
	Target Window `json:"target"`
	Reason string `json:"reason,omitempty"`
}

type action struct {
	kind string
}

// purpose is what a tap was for. Every emitted tap is recorded with one, and
// the budgets are counted per purpose.
type purpose int

const (
	purposeWalk purpose = iota
	purposeAction
	purposeHeal
)

type tap struct {
	at time.Time
	p  purpose
}

func purposeLimit(p purpose) int {
	switch p {
	case purposeWalk:
		return maxWalkTapsPerSecond
	case purposeAction:
		return maxActionTapsPerSecond
	default:
		return maxHealTapsPerSecond
	}
}

type Driver struct {
	mu  sync.Mutex
	em  Emitter
	now func() time.Time

	armed  bool
	target Window
	reason string

	taps     []tap
	inFlight *action

	// Hotkeys used for floor transitions, filled from the panel config.
	ActionKeys map[string]string
	// ClickAfterHotkey is false for clients whose hotkey is set to "use on
	// yourself"; then the sequence is the tap alone.
	ClickAfterHotkey bool
	// DirectionKeys maps each of the eight compass directions to the key
	// walkLocked taps for it. Defaults to the numpad layout so a user with
	// numpad movement in the client needs to configure nothing; a client
	// bound to letter keys (WASD and the like) reconfigures this from the
	// panel. Diagonals are single keys - composing them from two straight
	// presses depends on event ordering inside the client and is unreliable,
	// and if a client has no diagonal key at all the direction is refused
	// outright rather than silently decomposed.
	DirectionKeys map[string]string
	// Tile is the player tile in normalised screen coordinates.
	Tile    [2]float64
	HasTile bool

	// MaxObservationAgeMS is the freshness gate: an intent whose observation
	// is older than this is refused. Configurable via -stale-ms, since a slow
	// capture-to-reply round trip would otherwise refuse every single step.
	MaxObservationAgeMS int
}

// defaultDirectionKeys is the numpad layout every driver starts with.
var defaultDirectionKeys = map[string]string{
	"NW": "numpad7", "N": "numpad8", "NE": "numpad9",
	"W": "numpad4", "E": "numpad6",
	"SW": "numpad1", "S": "numpad2", "SE": "numpad3",
}

// validDirections are the eight compass names the follower ever sends. They
// come from defaultDirectionKeys' keys, not its values, so this stays fixed
// even though a client may reconfigure - or blank out - any of those values.
var validDirections = func() map[string]bool {
	v := make(map[string]bool, len(defaultDirectionKeys))
	for dir := range defaultDirectionKeys {
		v[dir] = true
	}
	return v
}()

func NewDriver(e Emitter, maxObservationAgeMS int) *Driver {
	directions := make(map[string]string, len(defaultDirectionKeys))
	for dir, key := range defaultDirectionKeys {
		directions[dir] = key
	}
	return &Driver{
		em: e, now: time.Now,
		ActionKeys: map[string]string{}, DirectionKeys: directions,
		MaxObservationAgeMS: maxObservationAgeMS,
	}
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
	d.armed, d.target, d.reason = true, win, ""
	// The tap history deliberately survives arming. Re-arming happens after
	// every lost focus, and clearing the window there would let a runaway
	// caller reset its own rate limit at will.
	d.inFlight = nil
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
	d.armed, d.reason, d.inFlight = false, reason, nil
}

func (d *Driver) Status() ArmState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state()
}

func (d *Driver) state() ArmState {
	return ArmState{Armed: d.armed, Target: d.target, Reason: d.reason}
}

// Armed reports whether the driver may emit anything at all.
func (d *Driver) Armed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.armed
}

// ActionDone clears the in-flight action once the panel confirms the floor
// changed. Without it a repeated 'transition' would stay blocked forever.
func (d *Driver) ActionDone() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inFlight = nil
}

// guardLocked runs every check that has to pass before any key is emitted,
// whatever the key is for. It survives the intent protocol unchanged: session
// tokens and sequence numbers existed only because the decision came from
// another process, but a stale observation, a lost window and a runaway key
// rate are all still real.
func (d *Driver) guardLocked(observationAge time.Duration, p purpose) (Result, bool) {
	if !d.armed {
		return Result{Status: "disarmed", Reason: "wykonawca jest rozbrojony"}, false
	}
	ageMS := int(observationAge.Milliseconds())
	if ageMS < 0 || ageMS > d.MaxObservationAgeMS {
		return Result{Status: "refused",
			Reason: fmt.Sprintf("pozycja starsza niż %d ms", d.MaxObservationAgeMS)}, false
	}
	// Focus is checked as late as possible. It still is not atomic with the
	// emission that follows; see the spec's "granica gwarancji".
	win, err := d.em.Focused()
	if err != nil || win.PID != d.target.PID {
		d.disarmLocked("okno gry straciło focus")
		return Result{Status: "disarmed", Reason: d.reason}, false
	}
	if !d.allowTapLocked(p) {
		return Result{Status: "refused", Reason: "limit klawiszy na sekundę"}, false
	}
	return Result{}, true
}

// Walk taps the key bound to one compass direction.
func (d *Driver) Walk(direction string, observationAge time.Duration) Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	if res, ok := d.guardLocked(observationAge, purposeWalk); !ok {
		return res
	}
	return d.walkLocked(direction)
}

// UseHotkey performs one floor transition: rope, ladder, hole or shovel.
func (d *Driver) UseHotkey(kind string, observationAge time.Duration) Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	if res, ok := d.guardLocked(observationAge, purposeAction); !ok {
		return res
	}
	return d.transitionLocked(kind)
}

// Heal taps one healing hotkey. The key is literal - it comes straight from
// the panel's rule list - and nothing follows it: the client's hotkey is
// configured to use the potion or spell on the character herself.
//
// It deliberately does not go through UseHotkey. A floor action stays "in
// flight" until the panel confirms the floor changed, and healing has nothing
// to confirm, so routing it there would block walking forever. For the same
// reason healing ignores an action already in flight: a rope waiting for its
// confirmation must not stop the bot from drinking.
func (d *Driver) Heal(key string, observationAge time.Duration) Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !hotkeyNames[key] {
		return Result{Status: "refused", Reason: "nieznany klawisz leczenia: " + key}
	}
	if res, ok := d.guardLocked(observationAge, purposeHeal); !ok {
		return res
	}
	if err := d.em.TapKey(key, holdMS); err != nil {
		return d.emitterFailureLocked(err)
	}
	d.taps = append(d.taps, tap{at: d.now(), p: purposeHeal})
	return Result{Status: "emitted", Key: key}
}

// ValidHotkey answers whether a key name is one the platform emitters know.
// The healing rules are validated in internal/brain, which has no business
// knowing the per-platform key tables - but must refuse a name no emitter
// could ever press.
func ValidHotkey(key string) bool { return hotkeyNames[key] }

// keyForDirection resolves a walk direction against the driver's own
// DirectionKeys, distinguishing two different failures: a direction name
// outside the eight the follower ever sends (a bug upstream, not a
// configuration gap) versus a real compass direction the panel has no key
// configured for (e.g. a WASD layout with no diagonal key at all). Silently
// emitting nothing for the latter would look identical to a dropped request;
// naming the direction lets the panel show the user exactly what to fill in.
func (d *Driver) keyForDirection(dir string) (string, error) {
	if !validDirections[dir] {
		return "", fmt.Errorf("nieznany kierunek")
	}
	key := d.DirectionKeys[dir]
	if key == "" {
		return "", fmt.Errorf("brak skonfigurowanego klawisza dla kierunku %s", dir)
	}
	return key, nil
}

func (d *Driver) walkLocked(direction string) Result {
	if d.inFlight != nil {
		return Result{Status: "in_progress", Reason: "trwa akcja zmiany piętra"}
	}
	key, err := d.keyForDirection(direction)
	if err != nil {
		return Result{Status: "refused", Reason: err.Error()}
	}
	if err := d.em.TapKey(key, holdMS); err != nil {
		return d.emitterFailureLocked(err)
	}
	d.taps = append(d.taps, tap{at: d.now(), p: purposeWalk})
	return Result{Status: "emitted", Key: key}
}

func (d *Driver) transitionLocked(kind string) Result {
	want := action{kind: kind}
	if d.inFlight != nil {
		if *d.inFlight == want {
			return Result{Status: "in_progress", Reason: "akcja już trwa"}
		}
		return Result{Status: "refused", Reason: "trwa inna akcja"}
	}
	// Stairs are climbed by walking onto them; no item is used. Sending a
	// hotkey here would press whatever else is bound to it.
	if kind == "stairs" {
		return Result{Status: "refused", Reason: "schody pokonuje się krokiem, nie akcją"}
	}
	if d.ClickAfterHotkey && !d.HasTile {
		return Result{Status: "refused", Reason: "brak kalibracji kratki postaci"}
	}
	key, ok := d.ActionKeys[kind]
	if !ok || key == "" {
		return Result{Status: "refused", Reason: "brak hotkeya dla akcji " + kind}
	}
	if err := d.em.TapKey(key, holdMS); err != nil {
		return d.emitterFailureLocked(err)
	}
	d.taps = append(d.taps, tap{at: d.now(), p: purposeAction})
	if d.ClickAfterHotkey {
		// The client needs a moment to arm the crosshair before the click.
		time.Sleep(actionClickDelayMS * time.Millisecond)
		// That pause is long enough for the player to alt-tab, and a click is
		// far more destructive in a foreign window than a key tap.
		if win, err := d.em.Focused(); err != nil || win.PID != d.target.PID {
			d.disarmLocked("okno gry straciło focus w trakcie akcji")
			return Result{Status: "disarmed", Reason: d.reason}
		}
		if err := d.em.Click(d.Tile[0], d.Tile[1]); err != nil {
			return d.emitterFailureLocked(err)
		}
	}
	d.inFlight = &want
	return Result{Status: "emitted", Key: key}
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

// SetInputConfig stores the whole panel-configurable input surface: the
// hotkeys for floor actions, whether the hotkey is used on the character's
// own tile (no follow-up click) or needs a click afterwards, and the
// direction->key mapping walkLocked consults. It is the only way ActionKeys,
// ClickAfterHotkey and DirectionKeys are ever populated outside tests:
// without it every floor action is refused for lack of a hotkey, and walking
// stays on the numpad default. Both keys and directions are full
// replacements, not merges - like the panel already does for hotkeys, the
// caller resends the whole picture on every change. Everything is validated
// before anything is written, so a bad request never partially applies: one
// bad key name must not silently clear an unrelated field.
func (d *Driver) SetInputConfig(keys map[string]string, clickAfterHotkey bool, directions map[string]string) error {
	cleanKeys := make(map[string]string, len(keys))
	for action, key := range keys {
		if !validActionTypes[action] {
			return fmt.Errorf("nieznany typ akcji: %s", action)
		}
		if key == "" {
			continue // an unset hotkey is fine; that action stays refused until configured
		}
		if !hotkeyNames[key] {
			return fmt.Errorf("nieznany klawisz dla akcji %s: %s", action, key)
		}
		cleanKeys[action] = key
	}
	cleanDirections := make(map[string]string, len(directions))
	for dir, key := range directions {
		if !validDirections[dir] {
			return fmt.Errorf("nieznany kierunek: %s", dir)
		}
		if key == "" {
			continue // an unset direction is fine; walking refuses it until configured
		}
		if !hotkeyNames[key] {
			return fmt.Errorf("nieznany klawisz dla kierunku %s: %s", dir, key)
		}
		cleanDirections[dir] = key
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ActionKeys = cleanKeys
	d.ClickAfterHotkey = clickAfterHotkey
	d.DirectionKeys = cleanDirections
	return nil
}

// emitterFailureLocked disarms the driver on a failed OS call and produces a
// user-facing result. The Reason always starts in Polish, regardless of what
// the underlying OS binding's error text says; Status() and the returned
// result therefore always agree.
func (d *Driver) emitterFailureLocked(err error) Result {
	msg := fmt.Sprintf("nie udało się wysłać zdarzenia: %v", err)
	d.disarmLocked(msg)
	return Result{Status: "disarmed", Reason: msg}
}

// allowTapLocked keeps a sliding one-second window and answers whether one
// more tap for this purpose fits in it. Idle time must not bank budget for a
// later burst, and an unused healing slot is never lent to walking: a reserve
// that can be borrowed is not a reserve.
func (d *Driver) allowTapLocked(p purpose) bool {
	cutoff := d.now().Add(-time.Second)
	kept := d.taps[:0]
	for _, t := range d.taps {
		if t.at.After(cutoff) {
			kept = append(kept, t)
		}
	}
	d.taps = kept
	samePurpose, nonHeal := 0, 0
	for _, t := range d.taps {
		if t.p == p {
			samePurpose++
		}
		if t.p != purposeHeal {
			nonHeal++
		}
	}
	if len(d.taps) >= maxTapsPerSecond || samePurpose >= purposeLimit(p) {
		return false
	}
	return p == purposeHeal || nonHeal < maxNonHealTapsPerSecond
}
