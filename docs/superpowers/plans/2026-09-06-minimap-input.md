# Sterowanie klawiaturą w minimap-lab — plan implementacji

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Panel minimap-lab sam wysyła klawisze ruchu do klienta Tibii i wykonuje akcje zmiany piętra, zamiast tylko pokazywać kierunek człowiekowi.

**Architecture:** `RouteFollower` zostaje w JS i nadal decyduje, dokąd iść. Go dostaje rolę sterownika: `Driver` przyjmuje pojedynczy zamiar przez `POST /api/input`, sprawdza uzbrojenie, focus okna gry, limit i świeżość obserwacji, po czym emituje jedno zdarzenie przez interfejs `Emitter`. Implementacje `Emitter` są per platforma (build tagi), a `Driver` nie zna żadnego API systemu i testuje się na atrapie.

**Tech Stack:** Go 1.24.2 (bez CGO), `syscall.NewLazyDLL` na Windows, `github.com/ebitengine/purego` na macOS, czysty JS w panelu, `go test` i `node --test`.

**Spec:** `docs/superpowers/specs/2026-09-06-minimap-input-design.md`

## Global Constraints

- **Bez CGO.** `CGO_ENABLED=0` musi budować się na wszystkich platformach.
- **Zależności:** `github.com/ebitengine/purego` wyłącznie w plikach z tagiem `darwin`, przypięty do konkretnej wersji. Windows i Linux zostają bez żadnego `require`.
- **Go 1.24.2** — `go.mod` deklaruje `go 1.22` z `toolchain go1.24.2`; domyślny Go 1.22.2 na tym Macu produkuje binarki odrzucane przez loader (`missing LC_UUID`).
- **Testy uruchamiane lokalnie**, nie w Dockerze: `cd minimap-lab && go test ./...` oraz `node --test <plik>.cjs`. minimap-lab jest samodzielnym modułem bez docker-compose.
- **Komunikaty dla użytkownika po polsku**, komentarze w kodzie po angielsku — tak jak w całym istniejącym module.
- **Kontrola `Origin` i loopback już istnieje** w `main.go:83-97` i obejmuje każdą trasę. Nie duplikować jej w nowych handlerach; dochodzi wyłącznie token sesji.
- **Stałe bezpieczeństwa** (dokładne wartości ze specu): `holdMS = 35`, `maxObservationAgeMS = 400`, `heartbeatTimeoutMS = 750`, `maxTapsPerSecond = 5`, `stepTimeoutMS = 1200` (startowo), `actionTimeoutMS = 5000`, `actionClickDelayMS = 120`.

## Świadome odstępstwo od specu

Jedna rzecz wypada z zakresu tego planu i musi trafić do README jako znane
ograniczenie, a nie zostać po cichu pominięta:

1. **Wykrycie ruchu myszy przez człowieka** nie jest implementowane. Wymaga
   odczytu pozycji kursora na obu platformach i odróżnienia własnych zdarzeń od
   cudzych. Zamiast tego sekwencja z kliknięciem sprawdza focus **drugi raz**,
   tuż przed kliknięciem — to pokrywa przypadek, dla którego ta ochrona
   powstała, czyli alt-tab w trakcie 120 ms pauzy.

---

### Task 1: Interfejs Emitter, mapowanie kierunków i emitter dry

**Files:**
- Create: `minimap-lab/input.go`
- Test: `minimap-lab/input_test.go`

**Interfaces:**
- Consumes: nic
- Produces: `type Emitter interface`, `type Window struct{PID int; Path, Title string}`, `func keyForDirection(dir string) (string, bool)`, `type DryEmitter struct`, `func (*DryEmitter) Events() []string`, `func (*DryEmitter) Released() int`, `var ErrUnsupported`

- [ ] **Step 1: Write the failing test**

```go
package main

import "testing"

func TestKeyForDirectionCoversEightDirections(t *testing.T) {
	want := map[string]string{
		"NW": "numpad7", "N": "numpad8", "NE": "numpad9",
		"W": "numpad4", "E": "numpad6",
		"SW": "numpad1", "S": "numpad2", "SE": "numpad3",
	}
	for dir, key := range want {
		got, ok := keyForDirection(dir)
		if !ok || got != key {
			t.Errorf("%s: got %q %v, want %q", dir, got, ok, key)
		}
	}
	if _, ok := keyForDirection("UP"); ok {
		t.Error("unknown direction must be refused, not guessed")
	}
	if _, ok := keyForDirection(""); ok {
		t.Error("empty direction must be refused")
	}
}

func TestDryEmitterRecordsEventsAndReleasesKeys(t *testing.T) {
	e := &DryEmitter{Window: Window{PID: 42, Path: "/Applications/Tibia.app"}}
	if err := e.TapKey("numpad8", 35); err != nil {
		t.Fatal(err)
	}
	if err := e.Click(0.5, 0.5); err != nil {
		t.Fatal(err)
	}
	got := e.Events()
	if len(got) != 2 || got[0] != "tap numpad8 35ms" || got[1] != "click 0.500 0.500" {
		t.Fatalf("got %v", got)
	}
	if err := e.ReleaseAll(); err != nil || e.Released() != 1 {
		t.Fatalf("released %d, err %v", e.Released(), err)
	}
}

func TestDryEmitterRefusesClickOutsideScreen(t *testing.T) {
	e := &DryEmitter{}
	if err := e.Click(1.5, 0.5); err == nil {
		t.Error("normalised coordinates outside 0-1 must be refused")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minimap-lab && go test -run 'TestKeyForDirection|TestDryEmitter' ./...`
Expected: FAIL — `undefined: keyForDirection`, `undefined: DryEmitter`

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"errors"
	"fmt"
	"sync"
)

// ErrUnsupported is returned by platforms with no emitter implementation.
var ErrUnsupported = errors.New("sterowanie nie jest dostępne na tej platformie")

// Window identifies the application that currently owns keyboard focus.
// Path is the executable path on Windows and the bundle identifier on macOS;
// the title is diagnostic only, because any browser tab can be called "Tibia".
type Window struct {
	PID   int    `json:"pid"`
	Path  string `json:"path"`
	Title string `json:"title"`
}

// Emitter is the whole surface the driver needs from the operating system.
// Keeping it this small is what lets the driver be tested without a graphical
// session: every safety rule lives above this line.
type Emitter interface {
	TapKey(key string, holdMS int) error
	Click(nx, ny float64) error // normalised 0-1 within the shared screen
	Focused() (Window, error)
	ReleaseAll() error
	Preflight() error
}

// Diagonals are single keys. Composing them from two arrow presses depends on
// event ordering inside the client and is unreliable.
var directionKeys = map[string]string{
	"NW": "numpad7", "N": "numpad8", "NE": "numpad9",
	"W": "numpad4", "E": "numpad6",
	"SW": "numpad1", "S": "numpad2", "SE": "numpad3",
}

func keyForDirection(dir string) (string, bool) {
	key, ok := directionKeys[dir]
	return key, ok
}

// DryEmitter performs no system calls. It backs the -input dry mode and every
// driver test, so the whole flow can be exercised without the game.
type DryEmitter struct {
	Window Window
	OnTap  func()

	mu       sync.Mutex
	events   []string
	released int
}

func (e *DryEmitter) TapKey(key string, holdMS int) error {
	e.mu.Lock()
	e.events = append(e.events, fmt.Sprintf("tap %s %dms", key, holdMS))
	hook := e.OnTap
	e.mu.Unlock()
	// OnTap lets a test change the focused window mid-sequence, the way a real
	// alt-tab would between the hotkey and the click.
	if hook != nil {
		hook()
	}
	return nil
}

func (e *DryEmitter) Click(nx, ny float64) error {
	if nx < 0 || nx > 1 || ny < 0 || ny > 1 {
		return fmt.Errorf("współrzędne kliknięcia poza ekranem: %.3f %.3f", nx, ny)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, fmt.Sprintf("click %.3f %.3f", nx, ny))
	return nil
}

func (e *DryEmitter) Focused() (Window, error) { return e.Window, nil }

func (e *DryEmitter) ReleaseAll() error {
	e.mu.Lock()
	e.released++
	e.mu.Unlock()
	return nil
}

func (e *DryEmitter) Preflight() error { return nil }

func (e *DryEmitter) Events() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.events...)
}

// Released counts emergency releases, so tests can prove that disarming
// actually reached the system rather than merely flipping a flag.
func (e *DryEmitter) Released() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.released
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd minimap-lab && go test -run 'TestKeyForDirection|TestDryEmitter' ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add minimap-lab/input.go minimap-lab/input_test.go
git commit -m "feat: add Emitter interface, direction mapping and dry emitter"
```

---

### Task 2: Driver — uzbrojenie, bramka focusu, limit, idempotencja

**Files:**
- Create: `minimap-lab/driver.go`
- Test: `minimap-lab/driver_test.go`

**Interfaces:**
- Consumes: `Emitter`, `Window`, `keyForDirection`, `DryEmitter` z Task 1
- Produces: `type Driver struct` z polem `now func() time.Time`, `func NewDriver(e Emitter) *Driver`, `func (*Driver) Arm() (ArmState, error)`, `func (*Driver) Disarm(reason string)`, `func (*Driver) Status() ArmState`, `func (*Driver) Beat(session string) ArmState`, `func (*Driver) ActionDone()`, `func (*Driver) Submit(in Intent) InputResult`, typy `Intent`, `InputResult`, `ArmState`, stałe `holdMS`, `maxObservationAgeMS`, `heartbeatTimeoutMS`, `maxTapsPerSecond`, `actionClickDelayMS`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"testing"
	"time"
)

// driverAt builds an armed driver with a controllable clock, so timeouts are
// tested without sleeping.
func driverAt(t *testing.T, start time.Time) (*Driver, *DryEmitter, *time.Time) {
	t.Helper()
	now := start
	em := &DryEmitter{Window: Window{PID: 42, Path: "/Applications/Tibia.app"}}
	d := NewDriver(em)
	d.now = func() time.Time { return now }
	if _, err := d.Arm(); err != nil {
		t.Fatal(err)
	}
	return d, em, &now
}

func walk(seq uint64, session string) Intent {
	return Intent{Session: session, Seq: seq, Action: "walk", Direction: "N", AgeMS: 100}
}

func TestDriverRefusesEverythingWhileDisarmed(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 42}}
	d := NewDriver(em)

	got := d.Submit(walk(1, "nieistniejąca"))

	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("a disarmed driver must not touch the system")
	}
}

func TestDriverEmitsOneTapPerAcceptedIntent(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))

	got := d.Submit(walk(1, d.Status().Session))

	if got.Status != "emitted" || got.Key != "numpad8" {
		t.Fatalf("got %+v", got)
	}
	if ev := em.Events(); len(ev) != 1 || ev[0] != "tap numpad8 35ms" {
		t.Fatalf("got %v", ev)
	}
}

func TestDriverRejectsForeignSessionToken(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))

	got := d.Submit(walk(1, "podrobiony"))

	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("a wrong token must not reach the system")
	}
}

func TestDriverRepeatedSeqReturnsFirstResultWithoutPressingAgain(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session
	first := d.Submit(walk(7, session))

	again := d.Submit(walk(7, session))

	if again != first {
		t.Fatalf("got %+v, want %+v", again, first)
	}
	if len(em.Events()) != 1 {
		t.Fatalf("a retried request pressed the key twice: %v", em.Events())
	}
}

func TestDriverRefusesStaleObservation(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	in := walk(1, d.Status().Session)
	in.AgeMS = maxObservationAgeMS + 1

	got := d.Submit(in)

	if got.Status != "refused" || got.Reason == "" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("walking on a stale position is the worst possible state")
	}
}

func TestDriverDisarmsWhenAnotherWindowTakesFocus(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	em.Window = Window{PID: 99, Path: "/Applications/Safari.app"}

	got := d.Submit(walk(1, d.Status().Session))

	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
	if d.Status().Armed {
		t.Error("losing focus must disarm, not merely skip one step")
	}
	if len(em.Events()) != 0 {
		t.Error("no key may be sent to a foreign window")
	}
}

func TestDriverEnforcesTapRateWithoutBanking(t *testing.T) {
	d, _, now := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session
	for i := uint64(1); i <= maxTapsPerSecond; i++ {
		if got := d.Submit(walk(i, session)); got.Status != "emitted" {
			t.Fatalf("tap %d: %+v", i, got)
		}
	}

	over := d.Submit(walk(99, session))

	if over.Status != "refused" {
		t.Fatalf("got %+v", over)
	}
	// A quiet stretch must not hand back a burst of unused budget. The clock
	// moves in heartbeat-sized steps, or the driver would disarm instead.
	for range 6 {
		*now = now.Add(200 * time.Millisecond)
		d.Beat(session)
	}
	for i := uint64(100); i < 100+maxTapsPerSecond; i++ {
		if got := d.Submit(walk(i, session)); got.Status != "emitted" {
			t.Fatalf("after idle, tap %d: %+v", i, got)
		}
	}
	if got := d.Submit(walk(200, session)); got.Status != "refused" {
		t.Fatalf("idle time banked extra taps: %+v", got)
	}
}

func TestDriverExpiresWhenHeartbeatStops(t *testing.T) {
	d, _, now := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session

	*now = now.Add(time.Duration(heartbeatTimeoutMS+1) * time.Millisecond)

	if got := d.Submit(walk(1, session)); got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
}

func TestDriverHeartbeatKeepsTheSessionAlive(t *testing.T) {
	d, _, now := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session

	for range 5 {
		*now = now.Add(200 * time.Millisecond)
		d.Beat(session)
	}

	if got := d.Submit(walk(1, session)); got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
}

func TestDriverRunsOneActionAtATime(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	session := d.Status().Session
	rope := Intent{Session: session, Seq: 1, Action: "transition", Type: "rope", Waypoint: 3, AgeMS: 50}
	if got := d.Submit(rope); got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
	before := len(em.Events())

	// The follower repeats 'transition' on every reading until the floor
	// changes; the second one must not press the hotkey again.
	rope.Seq = 2
	got := d.Submit(rope)

	if got.Status != "in_progress" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != before {
		t.Fatalf("repeated transition pressed the hotkey again: %v", em.Events())
	}
}

func TestDriverDisarmReleasesHeldKeys(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	em.Window = Window{PID: 99}

	d.Submit(walk(1, d.Status().Session))

	if em.Released() != 1 {
		t.Error("disarming must release keys even though emitting is otherwise forbidden")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minimap-lab && go test -run TestDriver ./...`
Expected: FAIL — `undefined: NewDriver`, `undefined: Intent`

- [ ] **Step 3: Write minimal implementation**

```go
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
	if in.Seq != 0 && in.Seq == d.lastSeq {
		return d.lastResult
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
		d.disarmLocked(err.Error())
		return InputResult{Status: "disarmed", Reason: err.Error()}
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
	key, ok := d.ActionKeys[in.Type]
	if !ok || key == "" {
		return InputResult{Status: "refused", Reason: "brak hotkeya dla akcji " + in.Type}
	}
	if err := d.em.TapKey(key, holdMS); err != nil {
		d.disarmLocked(err.Error())
		return InputResult{Status: "disarmed", Reason: err.Error()}
	}
	d.taps = append(d.taps, d.now())
	d.inFlight = &want
	return InputResult{Status: "emitted", Key: key}
}

func (d *Driver) record(seq uint64, r InputResult) InputResult {
	d.lastSeq, d.lastResult = seq, r
	return r
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd minimap-lab && go test -run TestDriver ./...`
Expected: PASS (11 testów)

- [ ] **Step 5: Commit**

```bash
git add minimap-lab/driver.go minimap-lab/driver_test.go
git commit -m "feat: add input driver with arming, focus gate and rate limit"
```

---

### Task 3: Sekwencja akcji zmiany piętra z kliknięciem

**Files:**
- Modify: `minimap-lab/driver.go` (`transitionLocked`)
- Test: `minimap-lab/driver_test.go`

**Interfaces:**
- Consumes: `Driver`, `DryEmitter` z Task 2
- Produces: `func (*Driver) Calibrate(nx, ny float64) error`

- [ ] **Step 1: Write the failing test**

Ten task dokłada test sprawdzający, że po utracie focusu nie padnie kliknięcie,
więc do importów `driver_test.go` dochodzi `"strings"`:

```go
func TestDriverTransitionTapsHotkeyThenClicksPlayerTile(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	d.ClickAfterHotkey = true
	if err := d.Calibrate(0.42, 0.31); err != nil {
		t.Fatal(err)
	}
	session := d.Status().Session

	got := d.Submit(Intent{Session: session, Seq: 1, Action: "transition", Type: "rope", Waypoint: 2, AgeMS: 50})

	if got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
	want := []string{"tap f7 35ms", "click 0.420 0.310"}
	if ev := em.Events(); len(ev) != 2 || ev[0] != want[0] || ev[1] != want[1] {
		t.Fatalf("got %v, want %v", ev, want)
	}
}

func TestDriverTransitionRefusesWithoutCalibration(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	d.ClickAfterHotkey = true

	got := d.Submit(Intent{Session: d.Status().Session, Seq: 1, Action: "transition", Type: "rope", AgeMS: 50})

	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("clicking an unknown screen point is worse than doing nothing")
	}
}

func TestDriverTransitionSkipsClickWhenHotkeyUsesItself(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	d.ClickAfterHotkey = false

	d.Submit(Intent{Session: d.Status().Session, Seq: 1, Action: "transition", Type: "rope", AgeMS: 50})

	if ev := em.Events(); len(ev) != 1 || ev[0] != "tap f7 35ms" {
		t.Fatalf("got %v", ev)
	}
}

func TestDriverActionDoneUnblocksTheNextAction(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7", "hole": "f8"}
	session := d.Status().Session
	d.Submit(Intent{Session: session, Seq: 1, Action: "transition", Type: "rope", Waypoint: 1, AgeMS: 50})

	d.ActionDone()

	got := d.Submit(Intent{Session: session, Seq: 2, Action: "transition", Type: "hole", Waypoint: 2, AgeMS: 50})
	if got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
}

func TestDriverRefusesStairsBecauseTheyAreWalkedNotUsed(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}

	got := d.Submit(Intent{Session: d.Status().Session, Seq: 1, Action: "transition", Type: "stairs", AgeMS: 50})

	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("stairs are climbed by walking; no item is used on them")
	}
}

func TestDriverChecksFocusAgainBeforeTheClick(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	d.ClickAfterHotkey = true
	d.Calibrate(0.5, 0.5)
	// The window changes during the 120 ms the client needs to arm the
	// crosshair, so the click would land in a foreign window.
	em.OnTap = func() { em.Window = Window{PID: 99} }

	got := d.Submit(Intent{Session: d.Status().Session, Seq: 1, Action: "transition", Type: "rope", AgeMS: 50})

	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
	for _, ev := range em.Events() {
		if strings.HasPrefix(ev, "click") {
			t.Fatalf("clicked after losing focus: %v", em.Events())
		}
	}
}

func TestDriverCalibrateRefusesCoordinatesOutsideScreen(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	if err := d.Calibrate(1.2, 0.5); err == nil {
		t.Error("normalised coordinates must stay within 0-1")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minimap-lab && go test -run 'TestDriverTransition|TestDriverActionDone|TestDriverCalibrate' ./...`
Expected: FAIL — `d.Calibrate undefined`

- [ ] **Step 3: Write minimal implementation**

Dodaj metodę:

```go
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
```

W `transitionLocked`, zaraz po sprawdzeniu `d.inFlight`, przed pobraniem hotkeya:

```go
	// Stairs are climbed by walking onto them; no item is used. Sending a
	// hotkey here would press whatever else is bound to it.
	if in.Type == "stairs" {
		return InputResult{Status: "refused", Reason: "schody pokonuje się krokiem, nie akcją"}
	}
	if d.ClickAfterHotkey && !d.HasTile {
		return InputResult{Status: "refused", Reason: "brak kalibracji kratki postaci"}
	}
```

oraz po `d.taps = append(d.taps, d.now())`, przed `d.inFlight = &want`:

```go
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
			d.disarmLocked(err.Error())
			return InputResult{Status: "disarmed", Reason: err.Error()}
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd minimap-lab && go test -run TestDriver ./...`
Expected: PASS (16 testów)

- [ ] **Step 5: Commit**

```bash
git add minimap-lab/driver.go minimap-lab/driver_test.go
git commit -m "feat: add hotkey-then-click sequence for floor transitions"
```

---

### Task 4: HTTP API wykonawcy

**Files:**
- Create: `minimap-lab/inputapi.go`
- Create: `minimap-lab/input_other.go`
- Modify: `minimap-lab/main.go` (pole `driver` w `server`, trasy w `routes()`, flaga `-input`)
- Test: `minimap-lab/inputapi_test.go`

**Interfaces:**
- Consumes: `Driver`, `Intent`, `InputResult`, `ArmState`, `DryEmitter`
- Produces: `func selectEmitter(mode string) (Emitter, error)`, pole `server.driver *Driver`, trasy `POST /api/arm`, `POST /api/disarm`, `POST /api/input`, `POST /api/input/calibrate`, `POST /api/input/done`, `GET /api/input/status`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func inputServer(t testing.TB) (*server, *DryEmitter) {
	t.Helper()
	em := &DryEmitter{Window: Window{PID: 42, Path: "/Applications/Tibia.app"}}
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1), driver: NewDriver(em)}
	return s, em
}

func postInput(t testing.TB, s *server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "http://127.0.0.1:8095"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}

func armSession(t testing.TB, s *server) string {
	t.Helper()
	w := postInput(t, s, "/api/arm", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("arm: %d %s", w.Code, w.Body.String())
	}
	var st ArmState
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st.Session
}

func TestInputAPIWalksAfterArming(t *testing.T) {
	s, em := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input", `{"session":"`+session+`","seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)

	var got InputResult
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != http.StatusOK || got.Status != "emitted" || got.Key != "numpad6" {
		t.Fatalf("%d %+v", w.Code, got)
	}
	if len(em.Events()) != 1 {
		t.Fatalf("got %v", em.Events())
	}
}

func TestInputAPIRefusesWithoutSessionToken(t *testing.T) {
	s, em := inputServer(t)
	armSession(t, s)

	w := postInput(t, s, "/api/input", `{"seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)

	var got InputResult
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Status != "refused" {
		t.Fatalf("%d %+v", w.Code, got)
	}
	if len(em.Events()) != 0 {
		t.Error("a tokenless request must never reach the system")
	}
}

func TestInputAPIRefusesCrossOriginRequest(t *testing.T) {
	s, em := inputServer(t)
	session := armSession(t, s)
	r := httptest.NewRequest("POST", "http://127.0.0.1:8095/api/input",
		strings.NewReader(`{"session":"`+session+`","seq":1,"action":"walk","direction":"E","observation_age_ms":80}`))
	r.Header.Set("Origin", "https://obca-strona.example")
	w := httptest.NewRecorder()

	s.routes().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
	if len(em.Events()) != 0 {
		t.Error("127.0.0.1 is not a security boundary; any page can POST to it")
	}
}

func TestInputAPIRejectsMalformedJSON(t *testing.T) {
	s, _ := inputServer(t)
	armSession(t, s)

	w := postInput(t, s, "/api/input", `{"session":`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d", w.Code)
	}
}

func TestInputAPIStatusActsAsHeartbeat(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	r := httptest.NewRequest("GET", "http://127.0.0.1:8095/api/input/status", nil)
	r.Header.Set("X-Input-Session", session)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)

	var st ArmState
	json.Unmarshal(w.Body.Bytes(), &st)
	if w.Code != http.StatusOK || !st.Armed {
		t.Fatalf("%d %+v", w.Code, st)
	}
	if st.Session != "" {
		t.Error("status must not echo the token back; it is a bearer secret")
	}
}

func TestInputAPIDisarmStops(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	postInput(t, s, "/api/disarm", `{"session":"`+session+`"}`)

	w := postInput(t, s, "/api/input", `{"session":"`+session+`","seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)
	var got InputResult
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
}

func TestInputAPICalibrateStoresPlayerTile(t *testing.T) {
	s, _ := inputServer(t)
	session := armSession(t, s)

	w := postInput(t, s, "/api/input/calibrate", `{"session":"`+session+`","x":0.5,"y":0.4}`)

	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if !s.driver.HasTile {
		t.Error("calibration did not reach the driver")
	}
}

func TestInputAPIUnavailableWithoutEmitter(t *testing.T) {
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1)}

	w := postInput(t, s, "/api/input", `{"seq":1,"action":"walk","direction":"E","observation_age_ms":80}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestSelectEmitterRejectsUnknownMode(t *testing.T) {
	if _, err := selectEmitter("wlaczone"); err == nil {
		t.Error("unknown -input value must be refused at start, not silently ignored")
	}
	if em, err := selectEmitter("off"); err != nil || em != nil {
		t.Errorf("off must leave the panel without a driver, got %v %v", em, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minimap-lab && go test -run 'TestInputAPI|TestSelectEmitter' ./...`
Expected: FAIL — `unknown field driver in struct literal`

- [ ] **Step 3: Write minimal implementation**

W `main.go` dodaj pole do struktury `server`:

```go
	// Nil until -input selects an emitter; every input route then answers 503.
	driver *Driver
```

flagę w `main()` obok istniejących:

```go
	mode := flag.String("input", "off", "sterowanie: off, dry albo system")
```

i po utworzeniu `s := &server{...}`:

```go
	em, err := selectEmitter(*mode)
	if err != nil {
		log.Fatal(err)
	}
	if em != nil {
		s.driver = NewDriver(em)
		log.Printf("Sterowanie: %s — wykonawca startuje rozbrojony.", *mode)
	}
```

> **Uwaga:** `err` jest już zadeklarowane wyżej przy `net.SplitHostPort`; użyj
> przypisania, nie `:=`, albo zmień nazwę zmiennej.

Trasy w `routes()`, obok istniejących:

```go
	mux.HandleFunc("POST /api/arm", s.arm)
	mux.HandleFunc("POST /api/disarm", s.disarm)
	mux.HandleFunc("POST /api/input", s.input)
	mux.HandleFunc("POST /api/input/calibrate", s.calibrate)
	mux.HandleFunc("POST /api/input/done", s.actionDone)
	mux.HandleFunc("GET /api/input/status", s.inputStatus)
```

Nowy plik `input_other.go` — `selectEmitter` woła `newSystemEmitter`, więc bez
tego stuba build nie przejdzie na żadnej platformie poza Windows. Tag jest
tymczasowo szeroki; Task 6 zawęża go po dodaniu emitera macOS:

```go
//go:build !windows

package main

func newSystemEmitter() (Emitter, error) { return nil, ErrUnsupported }
```

Nowy plik `inputapi.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
)

// selectEmitter turns the -input flag into an emitter. "off" leaves the panel
// in the dry-run behaviour it had before this feature existed.
func selectEmitter(mode string) (Emitter, error) {
	switch mode {
	case "off":
		return nil, nil
	case "dry":
		return &DryEmitter{Window: Window{PID: 1, Path: "dry", Title: "dry"}}, nil
	case "system":
		return newSystemEmitter()
	default:
		return nil, fmt.Errorf("-input przyjmuje off, dry albo system")
	}
}

// readInput refuses early when control is switched off, then decodes the body.
func (s *server) readInput(w http.ResponseWriter, r *http.Request, v any) bool {
	if s.driver == nil {
		http.Error(w, "Sterowanie wyłączone. Uruchom panel z -input dry albo -input system.", http.StatusServiceUnavailable)
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(v); err != nil {
		http.Error(w, "Nieprawidłowe żądanie JSON", http.StatusBadRequest)
		return false
	}
	return true
}

// sessionOK guards the routes that change state on behalf of an armed panel.
func (s *server) sessionOK(w http.ResponseWriter, session string) bool {
	if session == "" || session != s.driver.Status().Session {
		http.Error(w, "Nieprawidłowy token sesji.", http.StatusForbidden)
		return false
	}
	return true
}

func (s *server) arm(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if !s.readInput(w, r, &body) {
		return
	}
	state, err := s.driver.Arm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, state)
}

func (s *server) disarm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
	}
	if !s.readInput(w, r, &body) || !s.sessionOK(w, body.Session) {
		return
	}
	s.driver.Disarm("zatrzymane z panelu")
	writeJSON(w, s.driver.Status())
}

func (s *server) input(w http.ResponseWriter, r *http.Request) {
	var in Intent
	if !s.readInput(w, r, &in) {
		return
	}
	writeJSON(w, s.driver.Submit(in))
}

func (s *server) calibrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string  `json:"session"`
		X       float64 `json:"x"`
		Y       float64 `json:"y"`
	}
	if !s.readInput(w, r, &body) || !s.sessionOK(w, body.Session) {
		return
	}
	if err := s.driver.Calibrate(body.X, body.Y); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *server) actionDone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Session string `json:"session"`
	}
	if !s.readInput(w, r, &body) || !s.sessionOK(w, body.Session) {
		return
	}
	s.driver.ActionDone()
	writeJSON(w, map[string]bool{"ok": true})
}

// inputStatus doubles as the heartbeat: the panel polls it every 200 ms, and a
// gap longer than heartbeatTimeoutMS disarms the driver.
func (s *server) inputStatus(w http.ResponseWriter, r *http.Request) {
	if s.driver == nil {
		writeJSON(w, map[string]any{"available": false, "platform": runtime.GOOS})
		return
	}
	state := s.driver.Beat(r.Header.Get("X-Input-Session"))
	// The token is a bearer secret; it leaves the server once, in the arm reply.
	state.Session = ""
	writeJSON(w, state)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd minimap-lab && go test ./...`
Expected: PASS — nowe testy oraz wszystkie istniejące

- [ ] **Step 5: Commit**

```bash
git add minimap-lab/inputapi.go minimap-lab/input_other.go minimap-lab/inputapi_test.go minimap-lab/main.go
git commit -m "feat: expose input driver over local HTTP API"
```

---

### Task 5: Emiter Windows

**Files:**
- Create: `minimap-lab/input_windows.go`

**Interfaces:**
- Consumes: `Emitter`, `Window`, `selectEmitter` z Task 4
- Produces: `func newSystemEmitter() (Emitter, error)` dla Windows

Emitera nie da się przetestować automatycznie — wymaga sesji graficznej Windows.
Weryfikacją w tym tasku jest kompilacja krzyżowa, `go vet` i test ręczny z Taska 11.

- [ ] **Step 1: Napisz emiter Windows**

`minimap-lab/input_windows.go`:

```go
//go:build windows

package main

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procSendInput            = user32.NewProc("SendInput")
	procGetForegroundWindow  = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	procMapVirtualKeyW       = user32.NewProc("MapVirtualKeyW")
	procOpenProcess          = kernel32.NewProc("OpenProcess")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	procQueryFullProcessName = kernel32.NewProc("QueryFullProcessImageNameW")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	keyeventfExtendedKey = 0x0001
	keyeventfKeyUp       = 0x0002
	keyeventfScanCode    = 0x0008

	mouseeventfMove        = 0x0001
	mouseeventfLeftDown    = 0x0002
	mouseeventfLeftUp      = 0x0004
	mouseeventfVirtualDesk = 0x4000
	mouseeventfAbsolute    = 0x8000

	processQueryLimitedInformation = 0x1000
)

// keyboardInput and mouseInput both stand in for INPUT, whose union member is
// the larger MOUSEINPUT. Both structs must therefore be the same size: 40
// bytes on amd64. The trailing padding on keyboardInput is what makes that so.
type keyboardInput struct {
	kind    uint32
	_       uint32 // union alignment on 64-bit
	wVk     uint16
	wScan   uint16
	flags   uint32
	time    uint32
	extra   uintptr
	padding [8]byte
}

type mouseInput struct {
	kind  uint32
	_     uint32
	dx    int32
	dy    int32
	data  uint32
	flags uint32
	time  uint32
	extra uintptr
}

// Virtual key codes for the keys the panel can send. Scan codes are derived
// from these at send time, because game clients frequently ignore events that
// carry only a virtual key.
var windowsKeys = map[string]uint16{
	"numpad1": 0x61, "numpad2": 0x62, "numpad3": 0x63, "numpad4": 0x64,
	"numpad6": 0x66, "numpad7": 0x67, "numpad8": 0x68, "numpad9": 0x69,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74, "f6": 0x75,
	"f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
}

// Arrows need the extended-key flag or the client reads them as their numpad
// twins.
var windowsExtended = map[string]bool{"up": true, "down": true, "left": true, "right": true}

type windowsEmitter struct {
	mu   sync.Mutex
	held map[uint16]bool
}

func newSystemEmitter() (Emitter, error) {
	return &windowsEmitter{held: map[uint16]bool{}}, nil
}

func (e *windowsEmitter) Preflight() error { return nil }

func (e *windowsEmitter) TapKey(key string, ms int) error {
	vk, ok := windowsKeys[key]
	if !ok {
		return fmt.Errorf("nieznany klawisz: %s", key)
	}
	scan, _, _ := procMapVirtualKeyW.Call(uintptr(vk), 0) // MAPVK_VK_TO_VSC
	flags := uint32(keyeventfScanCode)
	if windowsExtended[key] {
		flags |= keyeventfExtendedKey
	}
	e.mu.Lock()
	e.held[vk] = true
	e.mu.Unlock()
	if err := e.sendKey(keyboardInput{kind: inputKeyboard, wScan: uint16(scan), flags: flags}); err != nil {
		return err
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	err := e.sendKey(keyboardInput{kind: inputKeyboard, wScan: uint16(scan), flags: flags | keyeventfKeyUp})
	e.mu.Lock()
	delete(e.held, vk)
	e.mu.Unlock()
	return err
}

func (e *windowsEmitter) Click(nx, ny float64) error {
	if nx < 0 || nx > 1 || ny < 0 || ny > 1 {
		return fmt.Errorf("współrzędne kliknięcia poza ekranem")
	}
	// Absolute mouse coordinates are always 0-65535 across the virtual desktop,
	// independent of resolution and DPI.
	x, y := int32(nx*65535), int32(ny*65535)
	base := uint32(mouseeventfAbsolute | mouseeventfVirtualDesk)
	for _, flag := range []uint32{mouseeventfMove, mouseeventfLeftDown, mouseeventfLeftUp} {
		if err := e.sendMouse(mouseInput{kind: inputMouse, dx: x, dy: y, flags: base | flag}); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func (e *windowsEmitter) ReleaseAll() error {
	e.mu.Lock()
	keys := make([]uint16, 0, len(e.held))
	for vk := range e.held {
		keys = append(keys, vk)
	}
	e.held = map[uint16]bool{}
	e.mu.Unlock()
	for _, vk := range keys {
		scan, _, _ := procMapVirtualKeyW.Call(uintptr(vk), 0)
		e.sendKey(keyboardInput{kind: inputKeyboard, wScan: uint16(scan),
			flags: keyeventfScanCode | keyeventfKeyUp})
	}
	return nil
}

func (e *windowsEmitter) Focused() (Window, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return Window{}, fmt.Errorf("brak aktywnego okna")
	}
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	buf := make([]uint16, 256)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return Window{PID: int(pid), Path: processPath(pid), Title: syscall.UTF16ToString(buf)}, nil
}

// processPath is the identity that matters. A browser tab can be titled
// "Tibia", so the title alone must never gate emission.
func processPath(pid uint32) string {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	ok, _, _ := procQueryFullProcessName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

func (e *windowsEmitter) sendKey(in keyboardInput) error {
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if n != 1 {
		// UIPI blocks input aimed at a process running at a higher integrity
		// level, and reports it exactly this way.
		return fmt.Errorf("SendInput odrzucone (uruchom panel z tymi samymi uprawnieniami co grę): %v", err)
	}
	return nil
}

func (e *windowsEmitter) sendMouse(in mouseInput) error {
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if n != 1 {
		return fmt.Errorf("SendInput odrzucone: %v", err)
	}
	return nil
}
```

- [ ] **Step 2: Zweryfikuj rozmiar struktur i kompilację krzyżową**

Dopisz do `input_windows.go` asercję rozmiaru wykonywaną w czasie kompilacji —
jeśli układ pól się rozjedzie, `SendInput` po cichu odrzucałby zdarzenia:

```go
// SendInput reads an array of INPUT records of one fixed size; the two structs
// standing in for it must agree, or the call silently fails.
var _ [0]struct{} = [unsafe.Sizeof(keyboardInput{}) - unsafe.Sizeof(mouseInput{})]struct{}{}
```

```bash
cd minimap-lab
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet .
```

Expected: wszystkie trzy bez wyjścia. Jeżeli asercja rozmiaru się nie kompiluje,
popraw układ pól — nie usuwaj asercji.

- [ ] **Step 3: Uruchom pełne testy na macOS**

Run: `cd minimap-lab && go test ./...`
Expected: PASS — pliki Windows nie wchodzą do buildu macOS, a `input_other.go`
z Taska 4 dostarcza `newSystemEmitter`

- [ ] **Step 4: Commit**

```bash
git add minimap-lab/input_windows.go
git commit -m "feat: add Windows emitter using SendInput with scan codes"
```

---

### Task 6: Emiter macOS

**Files:**
- Create: `minimap-lab/input_darwin.go`
- Modify: `minimap-lab/input_other.go` (zawężenie tagu)
- Modify: `minimap-lab/go.mod`, `minimap-lab/go.sum`

**Interfaces:**
- Consumes: `Emitter`, `Window`
- Produces: implementacja `newSystemEmitter` dla darwin

- [ ] **Step 1: Dodaj przypiętą zależność**

```bash
cd minimap-lab && go get github.com/ebitengine/purego@v0.9.0
```

Expected: `go.mod` zyskuje `require github.com/ebitengine/purego v0.9.0`. Wersja
jest przypięta świadomie — purego nie weryfikuje zadeklarowanych sygnatur, więc
jej podbicie jest zmianą wymagającą ponownego testu ręcznego.

- [ ] **Step 2: Napisz emiter macOS**

`minimap-lab/input_darwin.go`:

```go
//go:build darwin

package main

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ANSI physical key codes. These are positions on the keyboard, not ASCII and
// not Windows virtual keys.
var darwinKeys = map[string]uint16{
	"numpad1": 83, "numpad2": 84, "numpad3": 85, "numpad4": 86,
	"numpad6": 88, "numpad7": 89, "numpad8": 91, "numpad9": 92,
	"up": 126, "down": 125, "left": 123, "right": 124,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
	"f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
}

const (
	kCGHIDEventTap        = 0
	kCGEventLeftMouseDown = 1
	kCGEventLeftMouseUp   = 2
	kCGEventMouseMoved    = 5
	kCGMouseButtonLeft    = 0
)

// CGPoint is a pair of float64 passed by value.
type cgPoint struct{ X, Y float64 }

type darwinEmitter struct {
	mu   sync.Mutex
	held map[uint16]bool

	createKeyboard func(source uintptr, key uint16, down bool) uintptr
	createMouse    func(source uintptr, kind uint32, at cgPoint, button uint32) uintptr
	post           func(tap uint32, event uintptr)
	release        func(ref uintptr)
	preflight      func() bool
	displayWide    func(display uint32) uint64
	displayHigh    func(display uint32) uint64
	mainDisplay    func() uint32

	frontmostPID  func() int
	frontmostName func() string
}

func newSystemEmitter() (Emitter, error) {
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("nie udało się otworzyć CoreGraphics: %w", err)
	}
	e := &darwinEmitter{held: map[uint16]bool{}}
	// CGKeyCode is uint16, C bool is one byte, CGPoint is two float64 by value.
	// purego does not verify any of this; a wrong declaration fails at run
	// time, not at compile time.
	purego.RegisterLibFunc(&e.createKeyboard, cg, "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&e.createMouse, cg, "CGEventCreateMouseEvent")
	purego.RegisterLibFunc(&e.post, cg, "CGEventPost")
	purego.RegisterLibFunc(&e.preflight, cg, "CGPreflightPostEventAccess")
	purego.RegisterLibFunc(&e.displayWide, cg, "CGDisplayPixelsWide")
	purego.RegisterLibFunc(&e.displayHigh, cg, "CGDisplayPixelsHigh")
	// Display id 0 is not the main display; it has to be asked for by name.
	purego.RegisterLibFunc(&e.mainDisplay, cg, "CGMainDisplayID")

	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
		purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("nie udało się otworzyć CoreFoundation: %w", err)
	}
	purego.RegisterLibFunc(&e.release, cf, "CFRelease")

	if err := e.bindWorkspace(); err != nil {
		return nil, err
	}
	return e, nil
}

// Preflight refuses to arm without Accessibility. Without this check
// CGEventPost silently does nothing and the bot merely looks frozen.
func (e *darwinEmitter) Preflight() error {
	if !e.preflight() {
		return fmt.Errorf("brak zgody Accessibility. Dodaj program w Ustawieniach → Prywatność i ochrona → Dostępność. Zgoda na nagrywanie ekranu, którą ma przeglądarka, jej nie zastępuje")
	}
	return nil
}

func (e *darwinEmitter) TapKey(key string, ms int) error {
	code, ok := darwinKeys[key]
	if !ok {
		return fmt.Errorf("nieznany klawisz: %s", key)
	}
	e.mu.Lock()
	e.held[code] = true
	e.mu.Unlock()
	down := e.createKeyboard(0, code, true)
	if down == 0 {
		return fmt.Errorf("nie udało się utworzyć zdarzenia klawiatury")
	}
	e.post(kCGHIDEventTap, down)
	e.release(down)
	time.Sleep(time.Duration(ms) * time.Millisecond)
	if up := e.createKeyboard(0, code, false); up != 0 {
		e.post(kCGHIDEventTap, up)
		e.release(up)
	}
	e.mu.Lock()
	delete(e.held, code)
	e.mu.Unlock()
	return nil
}

func (e *darwinEmitter) Click(nx, ny float64) error {
	if nx < 0 || nx > 1 || ny < 0 || ny > 1 {
		return fmt.Errorf("współrzędne kliknięcia poza ekranem")
	}
	// CGEvent works in points while the shared image is in physical pixels;
	// normalised coordinates make the Retina factor cancel out.
	display := e.mainDisplay()
	at := cgPoint{X: nx * float64(e.displayWide(display)), Y: ny * float64(e.displayHigh(display))}
	for _, kind := range []uint32{kCGEventMouseMoved, kCGEventLeftMouseDown, kCGEventLeftMouseUp} {
		ev := e.createMouse(0, kind, at, kCGMouseButtonLeft)
		if ev == 0 {
			return fmt.Errorf("nie udało się utworzyć zdarzenia myszy")
		}
		e.post(kCGHIDEventTap, ev)
		e.release(ev)
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func (e *darwinEmitter) ReleaseAll() error {
	e.mu.Lock()
	codes := make([]uint16, 0, len(e.held))
	for c := range e.held {
		codes = append(codes, c)
	}
	e.held = map[uint16]bool{}
	e.mu.Unlock()
	for _, c := range codes {
		if up := e.createKeyboard(0, c, false); up != 0 {
			e.post(kCGHIDEventTap, up)
			e.release(up)
		}
	}
	return nil
}

func (e *darwinEmitter) Focused() (Window, error) {
	pid := e.frontmostPID()
	if pid == 0 {
		return Window{}, fmt.Errorf("nie udało się odczytać aktywnej aplikacji")
	}
	name := e.frontmostName()
	return Window{PID: pid, Path: name, Title: name}, nil
}
```

- [ ] **Step 3: Podłącz NSWorkspace przez objc**

Dopisz do tego samego pliku. `NSWorkspace.frontmostApplication` jest wybrane
zamiast `CGWindowListCopyWindowInfo`, bo to drugie nie jest jednoznacznym
odpowiednikiem focusu, a odczyt tytułów okien wymagałby zgody Screen Recording:

```go
func (e *darwinEmitter) bindWorkspace() error {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit",
		purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return fmt.Errorf("nie udało się otworzyć AppKit: %w", err)
	}
	getClass, err := purego.Dlsym(purego.RTLD_DEFAULT, "objc_getClass")
	if err != nil {
		return err
	}
	registerSel, err := purego.Dlsym(purego.RTLD_DEFAULT, "sel_registerName")
	if err != nil {
		return err
	}
	msgSend, err := purego.Dlsym(purego.RTLD_DEFAULT, "objc_msgSend")
	if err != nil {
		return err
	}
	cstr := func(s string) uintptr {
		b := append([]byte(s), 0)
		return uintptr(unsafe.Pointer(&b[0]))
	}
	class := func(name string) uintptr {
		r, _, _ := purego.SyscallN(getClass, cstr(name))
		return r
	}
	sel := func(name string) uintptr {
		r, _, _ := purego.SyscallN(registerSel, cstr(name))
		return r
	}
	send := func(obj, s uintptr) uintptr {
		r, _, _ := purego.SyscallN(msgSend, obj, s)
		return r
	}
	workspace := class("NSWorkspace")
	shared, frontmost := sel("sharedWorkspace"), sel("frontmostApplication")
	pidSel, idSel, utf8Sel := sel("processIdentifier"), sel("bundleIdentifier"), sel("UTF8String")

	app := func() uintptr { return send(send(workspace, shared), frontmost) }
	e.frontmostPID = func() int {
		a := app()
		if a == 0 {
			return 0
		}
		return int(int32(send(a, pidSel)))
	}
	e.frontmostName = func() string {
		a := app()
		if a == 0 {
			return ""
		}
		id := send(a, idSel)
		if id == 0 {
			return ""
		}
		ptr := send(id, utf8Sel)
		if ptr == 0 {
			return ""
		}
		var out []byte
		for i := 0; ; i++ {
			c := *(*byte)(unsafe.Pointer(ptr + uintptr(i)))
			if c == 0 {
				break
			}
			out = append(out, c)
		}
		return string(out)
	}
	return nil
}
```

- [ ] **Step 4: Zawęź tag w input_other.go i sprawdź buildy**

Zmień pierwszą linię `minimap-lab/input_other.go` na:

```go
//go:build !windows && !darwin
```

```bash
cd minimap-lab
CGO_ENABLED=0 go build -o /dev/null .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null .
go test ./...
```

Expected: trzy buildy bez wyjścia, testy PASS.

- [ ] **Step 5: Test ręczny emitera**

```bash
cd minimap-lab && go run . -input system
```

Otwórz edytor tekstu, ustaw na nim focus, w panelu kliknij **Uzbrój**, potem
wyślij jeden krok. Oczekiwane: znak w edytorze albo czytelny komunikat o braku
zgody Accessibility. To jedyny sposób weryfikacji tego pliku — sesji graficznej
nie da się zasymulować w `go test`.

- [ ] **Step 6: Commit**

```bash
git add minimap-lab/input_darwin.go minimap-lab/input_other.go minimap-lab/go.mod minimap-lab/go.sum
git commit -m "feat: add macOS emitter using CoreGraphics through purego"
```

---

### Task 7: actionTolerance w followerze

**Files:**
- Modify: `minimap-lab/web/follower.js` (konstruktor `RouteFollower`, `step()`)
- Test: `minimap-lab/follower_test.cjs`

**Interfaces:**
- Consumes: `RouteFollower`
- Produces: opcja `actionTolerance` (domyślnie 0) w konstruktorze `RouteFollower`; pole `next` w wyniku `step()` o akcji `transition`

- [ ] **Step 1: Write the failing test**

Dopisz do `follower_test.cjs`, w konwencji już tam obecnej:

```js
test('waypoint akcji nie jest osiągnięty z sąsiedniej kratki', () => {
  const f = new RouteFollower([
    {x: 100, y: 100, z: 7, type: 'rope'},
    {x: 100, y: 100, z: 6, type: 'walk'},
  ], {tolerance: 1});

  const out = f.step({x: 101, y: 100, z: 7}, 0);

  // Walking tolerance may be loose; a rope used one tile off the rope spot
  // does nothing at all.
  assert.notEqual(out.action, 'transition');
});

test('waypoint akcji jest osiągnięty z dokładnie tej kratki', () => {
  const f = new RouteFollower([
    {x: 100, y: 100, z: 7, type: 'rope'},
    {x: 100, y: 100, z: 6, type: 'walk'},
  ], {tolerance: 1});

  const out = f.step({x: 100, y: 100, z: 7}, 0);

  assert.equal(out.action, 'transition');
});

test('instrukcja przejścia niesie następny waypoint', () => {
  const f = new RouteFollower([
    {x: 100, y: 100, z: 7, type: 'stairs'},
    {x: 101, y: 100, z: 6, type: 'walk'},
  ]);

  const out = f.step({x: 100, y: 100, z: 7}, 0);

  // The stairs tile sits on the current floor; the next waypoint is what says
  // which way to step onto it.
  assert.equal(out.action, 'transition');
  assert.deepEqual(out.next, {x: 101, y: 100, z: 6, type: 'walk'});
});

test('ostatni waypoint przejścia nie ma następnika', () => {
  const f = new RouteFollower([{x: 100, y: 100, z: 7, type: 'rope'}]);

  const out = f.step({x: 100, y: 100, z: 7}, 0);

  assert.equal(out.next, null);
});

test('actionTolerance można poluzować świadomie', () => {
  const f = new RouteFollower([
    {x: 100, y: 100, z: 7, type: 'stairs'},
    {x: 101, y: 100, z: 6, type: 'walk'},
  ], {tolerance: 1, actionTolerance: 1});

  const out = f.step({x: 101, y: 100, z: 7}, 0);

  assert.equal(out.action, 'transition');
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minimap-lab && node --test follower_test.cjs`
Expected: FAIL — pierwszy test, bo `standingOnAction` używa `tolerance` równego 1

- [ ] **Step 3: Write minimal implementation**

W konstruktorze, zaraz po linii `this.tolerance = options.tolerance ?? 1;`:

```js
    // An action waypoint needs the exact tile: a rope used one tile off the
    // rope spot does nothing, while walking tolerance may stay loose.
    this.actionTolerance = options.actionTolerance ?? 0;
```

W `step()` zamień wyliczenie `standingOnAction`:

```js
    const standingOnAction = target.type !== 'walk' && target.z === position.z &&
      chebyshev(target, position) <= this.actionTolerance;
```

oraz dołóż następny waypoint do zwracanej instrukcji przejścia — bez niego
wykonawca nie ma z czego policzyć kierunku kroku na schody:

```js
      return {action: 'transition', waypoint: target, next: this.waypoints[this.index + 1] ?? null,
        instruction: `${verb}${floor}`};
```

W `advance()`, w bloku `if (!acted) { ... }`, zamień drugi warunek tak, żeby
waypoint akcji z sąsiedniej kratki pozostał celem zamiast zostać zaliczonym:

```js
      if (!acted) {
        if (target.z !== position.z || chebyshev(target, position) > this.tolerance) return target;
        if (target.type !== 'walk') return target;
      }
```

Ten warunek już zwraca cel dla każdego waypointa akcji, więc `advance()` nie
wymaga zmiany — sprawdź to testem, zanim cokolwiek tam ruszysz.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd minimap-lab && node --test follower_test.cjs route_test.cjs recorder_test.cjs tracker_test.cjs ui_test.cjs`
Expected: PASS — także wszystkie istniejące testy followera

- [ ] **Step 5: Commit**

```bash
git add minimap-lab/web/follower.js minimap-lab/follower_test.cjs
git commit -m "feat: require the exact tile for action waypoints"
```

---

### Task 8: Wykonawca kroków w panelu (lock-step)

**Files:**
- Create: `minimap-lab/web/executor.js`
- Create: `minimap-lab/executor_test.cjs`

**Interfaces:**
- Consumes: wyjście `RouteFollower.step()`
- Produces: `class StepExecutor` z metodami `intentFor(out, now)`, `emitted(now)`, `observe(position, capturedAt, now)`, `state()`, `reset()`

Klasa jest czystym stanem, bez DOM i bez `fetch` — dokładnie jak `RouteFollower`
i `MinimapTracker`, żeby dała się testować w `node --test`.

- [ ] **Step 1: Write the failing test**

`minimap-lab/executor_test.cjs`:

```js
const test = require('node:test');
const assert = require('node:assert');
const {StepExecutor} = require('./web/executor.js');

const walk = (direction = 'N', next = [100, 99]) => ({action: 'walk', direction, next});
const at = (x, y, z = 7) => ({x, y, z});

test('pierwszy krok jest wysyłany od razu', () => {
  const ex = new StepExecutor();
  assert.deepEqual(ex.intentFor(walk(), 0), {action: 'walk', direction: 'N'});
});

test('drugi krok nie idzie, dopóki pierwszy nie jest potwierdzony', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk(), 0);
  ex.emitted(10);

  assert.equal(ex.intentFor(walk(), 20), null);
});

test('klatka sprzed emisji nie jest dowodem wykonania kroku', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(100);

  // Captured before the key was sent, even though it arrived after.
  ex.observe(at(100, 99), 50, 120);

  assert.equal(ex.state().waiting, true);
});

test('klatka po emisji z docelową kratką kończy krok', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(100);

  ex.observe(at(100, 99), 150, 160);

  assert.equal(ex.state().waiting, false);
  assert.deepEqual(ex.intentFor(walk('N', [100, 98]), 170), {action: 'walk', direction: 'N'});
});

test('brak ruchu przed timeoutem nie powtarza kroku', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0);
  ex.observe(at(100, 100), 500, 510);

  assert.equal(ex.intentFor(walk('N', [100, 99]), 900), null);
});

test('brak ruchu po timeoucie powtarza krok raz', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0);
  ex.observe(at(100, 100), 500, 510);

  const retry = ex.intentFor(walk('N', [100, 99]), 1100);

  assert.deepEqual(retry, {action: 'walk', direction: 'N'});
  assert.equal(ex.state().retries, 1);
});

test('druga porażka tego samego kroku zgłasza blokadę', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0);
  ex.intentFor(walk('N', [100, 99]), 1100); // retry
  ex.emitted(1100);

  const third = ex.intentFor(walk('N', [100, 99]), 2200);

  assert.equal(third, null);
  assert.equal(ex.state().blocked, true);
});

test('trzy nieudane cykle zatrzymują wykonawcę', () => {
  const ex = new StepExecutor({stepTimeoutMS: 100, maxFailedCycles: 3});
  for (let i = 0; i < 8; i++) {
    const now = i * 200;
    const intent = ex.intentFor(walk('N', [100, 99]), now);
    if (intent) ex.emitted(now);
  }

  assert.equal(ex.state().stopped, true);
  assert.equal(ex.intentFor(walk(), 5000), null);
});

test('nieznana pozycja natychmiast wstrzymuje ruch', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk(), 0);
  ex.emitted(10);

  ex.observe(null, 100, 110);

  assert.equal(ex.state().halted, true);
  assert.equal(ex.intentFor(walk(), 120), null);
});

test('powrót poprawnego odczytu odblokowuje wykonawcę', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk(), 0);
  ex.emitted(10);
  ex.observe(null, 100, 110);

  ex.observe(at(100, 100), 200, 210);

  assert.equal(ex.state().halted, false);
  assert.ok(ex.intentFor(walk(), 220));
});

test('nieoczekiwana kratka porzuca krok zamiast liczyć go jako nieudany', () => {
  const ex = new StepExecutor();
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(10);

  // Pushed by a creature, or the player took over: not a failed step.
  ex.observe(at(105, 120), 100, 110);

  assert.equal(ex.state().waiting, false);
  assert.equal(ex.state().retries, 0, 'przesunięcie postaci to nie jest nieudany krok');
});

test('schody są pokonywane krokiem w stronę następnego waypointa', () => {
  const ex = new StepExecutor();
  const stairs = {action: 'transition', index: 1,
    waypoint: {x: 100, y: 100, z: 7, type: 'stairs'},
    next: {x: 101, y: 100, z: 6}};
  ex.observe(at(100, 100, 7), 0, 0);

  // The stairs tile is on the current floor, so this is a walk. What makes it
  // a transition is that the proof is a changed floor, not a reached tile.
  assert.deepEqual(ex.intentFor(stairs, 10), {action: 'walk', direction: 'E'});
  ex.emitted(20);

  ex.observe(at(101, 100, 6), 100, 110);

  assert.equal(ex.state().waiting, false);
  assert.equal(ex.state().actionDone, false, 'krok na schody nie zajmuje slotu akcji w driverze');
});

test('schody bez następnego waypointa nie dają kierunku', () => {
  const ex = new StepExecutor();
  ex.observe(at(100, 100, 7), 0, 0);

  const intent = ex.intentFor({action: 'transition', index: 0,
    waypoint: {x: 100, y: 100, z: 7, type: 'stairs'}, next: null}, 10);

  assert.equal(intent, null);
});

test('akcja piętra czeka na zmianę Z, nie na kratkę', () => {
  const ex = new StepExecutor({actionTimeoutMS: 5000});
  const rope = {action: 'transition', index: 3, waypoint: {x: 100, y: 100, z: 7, type: 'rope'}};

  assert.deepEqual(ex.intentFor(rope, 0), {action: 'transition', type: 'rope', waypoint: 3});
  ex.emitted(10);

  ex.observe(at(100, 100, 7), 200, 210);
  assert.equal(ex.intentFor(rope, 220), null, 'to samo piętro nie kończy akcji');

  ex.observe(at(100, 100, 6), 400, 410);
  assert.equal(ex.state().waiting, false);
  assert.equal(ex.state().actionDone, true);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minimap-lab && node --test executor_test.cjs`
Expected: FAIL — `Cannot find module './web/executor.js'`

- [ ] **Step 3: Write minimal implementation**

`minimap-lab/web/executor.js`:

```js
const COMPASS = [['NW', 'N', 'NE'], ['W', '', 'E'], ['SW', 'S', 'SE']];
const stepDirection = (from, to) =>
  COMPASS[Math.sign(to.y - from.y) + 1][Math.sign(to.x - from.x) + 1];

// Lock-step execution state, kept free of the DOM and of fetch so it can be
// tested directly. The panel feeds it follower output and confirmed positions;
// it answers with the one intent that may be sent right now, or null.
class StepExecutor {
  constructor(options = {}) {
    this.stepTimeoutMS = options.stepTimeoutMS ?? 1200;
    this.actionTimeoutMS = options.actionTimeoutMS ?? 5000;
    this.maxFailedCycles = options.maxFailedCycles ?? 3;
    this.reset();
  }
  reset() {
    this.pending = null; // {kind, target, z, from, viaHotkey, emittedAt}
    this.last = null;
    this.retries = 0;
    this.cycles = 0;
    this.blocked = false;
    this.halted = false;
    this.stopped = false;
    this.actionDone = false;
  }
  state() {
    return {
      waiting: !!this.pending, retries: this.retries, cycles: this.cycles,
      blocked: this.blocked, halted: this.halted, stopped: this.stopped,
      actionDone: this.actionDone,
    };
  }
  // intentFor returns what to send now, or null when the executor must wait.
  intentFor(out, now) {
    if (this.stopped || this.halted) return null;
    if (this.pending) {
      const limit = this.pending.kind === 'transition' ? this.actionTimeoutMS : this.stepTimeoutMS;
      // A step whose emission was never confirmed is still in flight.
      if (this.pending.emittedAt === null) return null;
      if (now - this.pending.emittedAt < limit) return null;
      this.pending = null;
      this.cycles++;
      if (this.cycles >= this.maxFailedCycles) { this.stopped = true; return null; }
      if (this.retries >= 1) { this.blocked = true; this.retries = 0; return null; }
      this.retries++;
    }
    if (!out) return null;
    if (out.action === 'walk') {
      // from is where the character stood when the key was sent. Taking it
      // from the first observation after the press would make "did not move"
      // and "moved somewhere unexpected" indistinguishable.
      this.pending = {kind: 'walk', target: out.next, from: this.last, emittedAt: null};
      return {action: 'walk', direction: out.direction};
    }
    if (out.action === 'transition') {
      // Stairs carry no item: the tile is on the current floor and stepping
      // onto it moves the character. The next waypoint says which way that is.
      if (out.waypoint.type === 'stairs') {
        if (!out.next || !this.last) return null;
        this.pending = {kind: 'transition', z: out.waypoint.z, viaHotkey: false, emittedAt: null};
        return {action: 'walk', direction: stepDirection(this.last, out.next)};
      }
      this.actionDone = false;
      this.pending = {kind: 'transition', z: out.waypoint.z, viaHotkey: true, emittedAt: null};
      return {action: 'transition', type: out.waypoint.type, waypoint: out.index ?? 0};
    }
    return null;
  }
  // emitted records when the key actually left the driver. It is taken after
  // the reply arrives, so no frame captured before the press can be mistaken
  // for proof that the step happened.
  emitted(now) {
    if (this.pending) {
      this.pending.emittedAt = now;
    }
  }
  observe(position, capturedAt, now) {
    if (!position) { this.halted = true; this.pending = null; return; }
    this.halted = false;
    // Kept for stairs, whose direction comes from the current tile rather than
    // from a path.
    this.last = {...position};
    const p = this.pending;
    if (!p || p.emittedAt === null || capturedAt <= p.emittedAt) return;
    if (p.kind === 'transition') {
      // A floor change is the only proof, whether an item was used or the
      // character simply walked onto stairs.
      if (position.z !== p.z) {
        this.done();
        if (p.viaHotkey) this.actionDone = true;
      }
      return;
    }
    if (position.x === p.target[0] && position.y === p.target[1]) { this.done(); return; }
    // Standing still is a failed step and belongs to the retry counter, which
    // intentFor bumps after the timeout. Standing somewhere else entirely is a
    // changed situation: drop the step and let the follower replan.
    // With no reference tile the step cannot be judged, so it is left to the
    // timeout rather than guessed at.
    const stillThere = !p.from || (position.x === p.from.x && position.y === p.from.y);
    if (!stillThere) { this.pending = null; }
  }
  done() {
    this.pending = null;
    this.retries = 0;
    this.cycles = 0;
    this.blocked = false;
  }
}

globalThis.StepExecutor = StepExecutor;
if (typeof module !== 'undefined') module.exports = {StepExecutor};
```

> **Uwaga dla wykonawcy:** testy są kontraktem. Jeżeli któryś nie przechodzi,
> popraw `executor.js`, nie test.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd minimap-lab && node --test executor_test.cjs`
Expected: PASS — 14 testów

- [ ] **Step 5: Commit**

```bash
git add minimap-lab/web/executor.js minimap-lab/executor_test.cjs
git commit -m "feat: add lock-step executor state for the panel"
```

---

### Task 9: Klient wykonawcy w panelu

**Files:**
- Create: `minimap-lab/web/input.js`
- Test: `minimap-lab/input_client_test.cjs`

**Interfaces:**
- Consumes: `/api/arm`, `/api/disarm`, `/api/input`, `/api/input/done`, `/api/input/calibrate`, `/api/input/status`
- Produces: `class InputClient` z `arm()`, `disarm()`, `send(intent, ageMS)`, `actionDone()`, `calibrate(nx, ny)`, `startHeartbeat()`, `stopHeartbeat()` oraz polami `armed`, `session`, `seq`

- [ ] **Step 1: Write the failing test**

`minimap-lab/input_client_test.cjs`:

```js
const test = require('node:test');
const assert = require('node:assert');
const {InputClient} = require('./web/input.js');

// A fetch stand-in that records calls and answers with a queued reply.
function fakeFetch(replies) {
  const calls = [];
  const fetch = async (url, opts = {}) => {
    calls.push({url, body: opts.body ? JSON.parse(opts.body) : null});
    const reply = replies.shift() ?? {};
    return {ok: reply.ok ?? true, json: async () => reply.json ?? {}};
  };
  return {fetch, calls};
}

test('uzbrojenie zapamiętuje token', async () => {
  const {fetch, calls} = fakeFetch([{json: {armed: true, session: 'abc', target: {pid: 42}}}]);
  const client = new InputClient({fetch, beatMS: 0});

  await client.arm();

  assert.equal(client.session, 'abc');
  assert.equal(client.armed, true);
  assert.ok(calls[0].url.includes('/api/arm'));
  client.stopHeartbeat();
});

test('nieudane uzbrojenie nie ustawia stanu uzbrojonego', async () => {
  const {fetch} = fakeFetch([{ok: false, json: {armed: false, reason: 'brak zgody'}}]);
  const client = new InputClient({fetch});

  const state = await client.arm();

  assert.equal(client.armed, false);
  assert.equal(state.reason, 'brak zgody');
});

test('wiek obserwacji jedzie w żądaniu, nie znacznik czasu', async () => {
  const {fetch, calls} = fakeFetch([{json: {status: 'emitted'}}]);
  const client = new InputClient({fetch});
  client.session = 'abc';
  client.armed = true;

  await client.send({action: 'walk', direction: 'N'}, 137.4);

  assert.equal(calls[0].body.observation_age_ms, 137);
  assert.equal(calls[0].body.session, 'abc');
  assert.ok(calls[0].body.seq > 0, 'każde żądanie musi mieć rosnący seq');
});

test('rozbrojenie po stronie serwera zatrzymuje wysyłanie', async () => {
  const {fetch} = fakeFetch([{json: {status: 'disarmed', reason: 'okno gry straciło focus'}}]);
  const client = new InputClient({fetch});
  client.session = 'abc';
  client.armed = true;

  const result = await client.send({action: 'walk', direction: 'N'}, 100);

  assert.equal(result.status, 'disarmed');
  assert.equal(client.armed, false, 'panel musi odzwierciedlić rozbrojenie po stronie Go');
});

test('rozbrojony klient nie wysyła żądań', async () => {
  const {fetch, calls} = fakeFetch([]);
  const client = new InputClient({fetch});

  const result = await client.send({action: 'walk', direction: 'N'}, 10);

  assert.equal(result.status, 'disarmed');
  assert.equal(calls.length, 0);
});

test('seq rośnie z każdym wysłanym zamiarem', async () => {
  const {fetch, calls} = fakeFetch([{json: {status: 'emitted'}}, {json: {status: 'emitted'}}]);
  const client = new InputClient({fetch});
  client.session = 'abc';
  client.armed = true;

  await client.send({action: 'walk', direction: 'N'}, 10);
  await client.send({action: 'walk', direction: 'E'}, 10);

  assert.ok(calls[1].body.seq > calls[0].body.seq);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minimap-lab && node --test input_client_test.cjs`
Expected: FAIL — `Cannot find module './web/input.js'`

- [ ] **Step 3: Write minimal implementation**

`minimap-lab/web/input.js`:

```js
// Panel-side client of the Go driver. Keeps the session token, the sequence
// number and the heartbeat; contains no route logic.
class InputClient {
  constructor(options = {}) {
    this.fetch = options.fetch ?? ((...a) => globalThis.fetch(...a));
    this.beatMS = options.beatMS ?? 200;
    this.onState = options.onState ?? (() => {});
    this.session = null;
    this.armed = false;
    this.seq = 0;
    this.timer = null;
  }
  async post(path, body) {
    return this.fetch(path, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(body),
    });
  }
  async arm() {
    const r = await this.post('/api/arm', {});
    const state = await r.json();
    if (!r.ok || !state.armed) { this.armed = false; this.onState(state); return state; }
    this.session = state.session;
    this.armed = true;
    this.startHeartbeat();
    this.onState(state);
    return state;
  }
  async disarm() {
    this.stopHeartbeat();
    if (!this.session) return;
    await this.post('/api/disarm', {session: this.session});
    this.armed = false;
  }
  async send(intent, ageMS) {
    if (!this.armed || !this.session) return {status: 'disarmed'};
    this.seq++;
    const r = await this.post('/api/input', {
      ...intent, session: this.session, seq: this.seq,
      observation_age_ms: Math.round(ageMS),
    });
    const result = await r.json();
    if (result.status === 'disarmed') { this.armed = false; this.stopHeartbeat(); }
    this.onState(result);
    return result;
  }
  async actionDone() {
    if (!this.session) return;
    await this.post('/api/input/done', {session: this.session});
  }
  async calibrate(nx, ny) {
    const r = await this.post('/api/input/calibrate', {session: this.session, x: nx, y: ny});
    return r.ok;
  }
  // The status poll is also the heartbeat: a gap longer than the driver's
  // timeout disarms it on the Go side.
  startHeartbeat() {
    this.stopHeartbeat();
    const beat = async () => {
      if (!this.armed || !this.session) return;
      // The token travels in a header, never in the URL: query strings land in
      // logs, history and Referer.
      const r = await this.fetch('/api/input/status', {headers: {'X-Input-Session': this.session}});
      const state = await r.json();
      if (!state.armed) { this.armed = false; this.stopHeartbeat(); }
      this.onState(state);
      if (this.armed) this.timer = setTimeout(beat, this.beatMS);
    };
    this.timer = setTimeout(beat, this.beatMS);
  }
  stopHeartbeat() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
  }
}

globalThis.InputClient = InputClient;
if (typeof module !== 'undefined') module.exports = {InputClient};
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd minimap-lab && node --test input_client_test.cjs`
Expected: PASS — 6 testów

- [ ] **Step 5: Commit**

```bash
git add minimap-lab/web/input.js minimap-lab/input_client_test.cjs
git commit -m "feat: add panel-side client of the input driver"
```

---

### Task 10: Sekcja sterowania w panelu i podłączenie do pętli

**Files:**
- Modify: `minimap-lab/web/index.html` (skrypty + sekcja 5)
- Modify: `minimap-lab/web/app.js` (`followStep`, kalibracja)
- Modify: `minimap-lab/web/tracking.css`
- Test: `minimap-lab/ui_test.cjs`

**Interfaces:**
- Consumes: `StepExecutor` (Task 8), `InputClient` (Task 9)
- Produces: `function normalisedPoint(event, element)` w `app.js`

- [ ] **Step 1: Write the failing test**

Dopisz do `ui_test.cjs`, korzystając z tamtejszego sposobu ładowania `app.js`:

```js
test('kalibracja przelicza piksel podglądu na ułamek ekranu', () => {
  // A Retina capture is twice the point size; a fraction cancels that out.
  const point = normalisedPoint({offsetX: 720, offsetY: 450}, {clientWidth: 1440, clientHeight: 900});

  assert.equal(point.x, 0.5);
  assert.equal(point.y, 0.5);
});

test('kalibracja odrzuca punkt poza podglądem', () => {
  assert.equal(normalisedPoint({offsetX: -5, offsetY: 10}, {clientWidth: 100, clientHeight: 100}), null);
});

test('kalibracja odrzuca podgląd o zerowym rozmiarze', () => {
  assert.equal(normalisedPoint({offsetX: 1, offsetY: 1}, {clientWidth: 0, clientHeight: 0}), null);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minimap-lab && node --test ui_test.cjs`
Expected: FAIL — `normalisedPoint is not defined`

- [ ] **Step 3: Write minimal implementation**

W `app.js` dodaj funkcję i wyeksportuj ją tak, jak plik eksportuje pozostałe:

```js
// The panel sends fractions of the shared image, never pixels. Go multiplies
// them by the screen size in points, so Retina and DPI never reach this side.
function normalisedPoint(event, element) {
  const w = element.clientWidth, h = element.clientHeight;
  if (!w || !h) return null;
  const x = event.offsetX / w, y = event.offsetY / h;
  if (x < 0 || x > 1 || y < 0 || y > 1) return null;
  return {x, y};
}
```

W `index.html` dopisz skrypty przed `app.js`:

```html
<script src="/executor.js"></script>
<script src="/input.js"></script>
```

oraz sekcję sterowania po sekcji „4. Trasa":

```html
  <section class="panel control">
    <h2>5. Sterowanie</h2>
    <p>Wykonawca startuje rozbrojony. Uzbrajaj dopiero wtedy, gdy klient gry jest oknem aktywnym — zapamiętany zostanie właśnie ten proces. Alt-tab rozbraja. Wymagane jest udostępnienie całego ekranu i praca na jednym monitorze.</p>
    <div class="toolbar">
      <button id="input-arm" disabled>Uzbrój</button>
      <button id="input-disarm" class="secondary" disabled>Rozbrój</button>
      <button id="input-calibrate" class="secondary" disabled>Wskaż kratkę postaci</button>
    </div>
    <div class="route-switches">
      <label class="check"><input id="input-walk" type="checkbox" disabled>Chodź automatycznie</label>
      <label class="check"><input id="input-actions" type="checkbox" disabled>Wykonuj akcje pięter</label>
    </div>
    <div id="input-status" role="status" aria-live="polite">Sterowanie wyłączone. Uruchom panel z <code>-input dry</code> albo <code>-input system</code>.</div>
  </section>
```

W `app.js`, w `followStep`, po wyliczeniu `out` i wypisaniu instrukcji. Sterowanie
działa tylko przy zaznaczonym checkboxie, więc dotychczasowy tryb podglądu
zostaje nietknięty:

```js
  if (!$('input-walk').checked || !inputClient.armed) return;
  executor.observe(position, lastPositionAt, now);
  if (executor.state().actionDone) inputClient.actionDone();
  const intent = executor.intentFor(out, now);
  if (!intent) return;
  if (intent.action === 'transition' && !$('input-actions').checked) return;
  inputClient.send(intent, now - lastPositionAt).then(result => {
    if (result.status === 'emitted') executor.emitted(performance.now());
    else if (result.status !== 'in_progress') executor.reset();
  });
```

Zadeklaruj stan sterowania obok istniejących zmiennych panelu (`follower`,
`lastPosition`), a kalibrację podłącz jako jednorazowy nasłuch:

```js
const executor = new StepExecutor();
const inputClient = new InputClient({onState: renderInputStatus});
let calibrating = false;

$('input-calibrate').addEventListener('click', () => {
  calibrating = true;
  $('input-status').textContent = 'Kliknij kratkę postaci na podglądzie ekranu.';
});

// The preview element is the same one the minimap selection already uses.
$('preview').addEventListener('click', async event => {
  if (!calibrating) return;
  calibrating = false;
  const point = normalisedPoint(event, event.currentTarget);
  if (!point) { $('input-status').textContent = 'Punkt poza podglądem.'; return; }
  const ok = await inputClient.calibrate(point.x, point.y);
  $('input-status').textContent = ok
    ? `Kratka postaci: ${point.x.toFixed(3)}, ${point.y.toFixed(3)}`
    : 'Nie udało się zapisać kalibracji.';
});

// Every way of stopping clears half-finished step state, so re-arming never
// resumes a step whose confirmation was never seen.
$('input-walk').addEventListener('change', () => { if (!$('input-walk').checked) executor.reset(); });
$('input-disarm').addEventListener('click', async () => { await inputClient.disarm(); executor.reset(); });
```

Zmiana rozdzielczości źródła obrazu zeruje `revision` i zatrzymuje śledzenie —
w tej samej gałęzi wywołaj `executor.reset()` i `inputClient.disarm()`, bo bez
świeżej pozycji dalszy ruch jest niedopuszczalny.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd minimap-lab && node --test ui_test.cjs executor_test.cjs input_client_test.cjs follower_test.cjs`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add minimap-lab/web/app.js minimap-lab/web/index.html minimap-lab/web/tracking.css minimap-lab/ui_test.cjs
git commit -m "feat: wire keyboard control into the panel"
```

---

### Task 11: README, pełne testy i test na żywym kliencie

**Files:**
- Modify: `minimap-lab/README.md`

- [ ] **Step 1: Popraw nieprawdziwe zdania w README**

Dzisiejsze README twierdzi „bez zewnętrznych bibliotek" oraz „**sterowanie
postacią nie jest zaimplementowane** — panel nie wysyła klawiszy ani kliknięć do
gry". Oba zdania przestają być prawdziwe. Zaktualizuj akapit wstępny (w tym
zdanie o testerze 5–10 Hz, które również kończy się „nie wysyła klawiszy")
i dopisz sekcję **Sterowanie** obejmującą:

- flagi `-input off|dry|system`, z `off` jako domyślną,
- uzbrajanie: klient gry musi być oknem aktywnym w chwili kliknięcia „Uzbrój",
- macOS: zgoda w Ustawieniach → Prywatność i ochrona → Dostępność, i że zgoda na
  nagrywanie ekranu przyznana przeglądarce jej nie zastępuje,
- zależność `purego` wyłącznie na macOS; Windows i Linux bez zależności,
- wymóg udostępnienia **całego ekranu** i pracy na jednym monitorze,
- że alt-tab rozbraja wykonawcę i jest podstawowym kill-switchem,
- znane ograniczenie: sprawdzenie focusu i emisja nie są atomowe,
- znane ograniczenie: przeglądarka throttluje kartę w tle, a brak świeżych
  klatek zatrzymuje ruch — zachowanie zamierzone,
- że przyjęcie zdarzeń syntetycznych przez klient gry nie jest zagwarantowane,
- że waypoint typu `stairs` jest pokonywany krokiem w stronę następnego
  waypointa, a potwierdzeniem jest zmiana piętra, nie osiągnięta kratka,
- że ruch myszy przez człowieka nie przerywa sekwencji; chroni ją ponowne
  sprawdzenie focusu tuż przed kliknięciem,
- komendy testów: `go test ./...` oraz `node --test` z pełną listą plików `.cjs`.

- [ ] **Step 2: Uruchom pełny zestaw testów**

```bash
cd minimap-lab
go test ./...
node --test ui_test.cjs tracker_test.cjs route_test.cjs recorder_test.cjs follower_test.cjs executor_test.cjs input_client_test.cjs
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null .
```

Expected: wszystko zielone, oba buildy krzyżowe bez wyjścia.

- [ ] **Step 3: Test w trybie dry, bez gry**

```bash
cd minimap-lab && go run . -input dry
```

W panelu: uzbrój, włącz „Chodź automatycznie" na nagranej trasie i sprawdź, że
sekwencja kierunków zgadza się z trasą pokazywaną w podglądzie. Żadne zdarzenie
nie trafia do systemu.

- [ ] **Step 4: Test na żywym kliencie, w tej kolejności**

```bash
cd minimap-lab && go run . -input system
```

1. Cztery kierunki po jednym kroku — postać rusza we właściwą stronę.
2. Krok po skosie.
3. Krok w ścianę — wykonawca zatrzymuje się po dwóch próbach, nie zapętla.
4. Alt-tab w trakcie chodzenia — panel pokazuje rozbrojenie, klawisze ustają.
5. Trasa 20 kratek bez przejść między piętrami.
6. Waypoint z liną.
7. Waypoint ze schodami — postać wchodzi krokiem, panel potwierdza nowe Z.

Zapisz wynik każdego punktu w opisie commita — to jedyna weryfikacja emiterów,
których nie da się objąć `go test`.

- [ ] **Step 5: Commit**

```bash
git add minimap-lab/README.md
git commit -m "docs: describe keyboard control, permissions and known limits"
```
