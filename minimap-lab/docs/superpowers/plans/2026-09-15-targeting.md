# Celowanie i czary — plan wdrożenia

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dać botowi ręce w walce: wchodzi w `Fighting`, gdy stwór jest blisko, wskazuje cel hotkeyem, rzuca czary według reguł, wraca na trasę, gdy nie ma kogo bić.

**Architecture:** Nowy czysty pakiet `internal/fight` (bez zegara, bez pikseli, bez klawiatury) — debounce liczników, maszyna stanów `Travelling`/`Fighting`, celownik z próbami/backoffem/bezpiecznikiem zastoju, silnik reguł czarów. `internal/brain/fight.go` spina to z pętlą klatek, `internal/input` rośnie o `Cast`/`CancelTarget` w nowym przeznaczeniu budżetu, panel dostaje `web/fight.js` na wzór `heal.js`.

**Tech Stack:** Go 1.x (`internal/fight`, `internal/brain`, `internal/input`), panel ES-modules (`web/`), testy `node --test` (`webtests/`).

**Spec:** `docs/superpowers/specs/2026-09-15-targeting-design.md`

## Global Constraints

- Testy Go: `go test ./... -race` musi przechodzić po każdym zadaniu. Testy panelu: `node --test webtests/*.mjs`.
- Cały nowy kod i komentarze w `internal/` po angielsku, jak reszta pakietu. Teksty panelu, README i komunikaty walidacji po polsku z pełnymi znakami diakrytycznymi.
- `internal/fight` jest czystą decyzją: żaden plik w tym pakiecie nie importuje `time.Now` jako efektu ubocznego (czas przychodzi zawsze jako parametr), nie czyta obrazu, nie dotyka `internal/input`.
- Priorytet klawiszy w jednej klatce: leczenie → oczekujący Escape → celowanie → czar → krok chodzenia. Dokładnie jeden klawisz nieleczący na klatkę.
- Cooldown startuje dopiero po potwierdzonej emisji (`Status == "emitted"`), nigdy po samej decyzji — ten sam wzór co `internal/heal`.
- Domyślne wartości i zakresy pól `FightConfig` (tabela w specu, sekcja „Konfiguracja"): `confirm_ms` 50–1000/150, `leave_fight_ms` 200–5000/600, `target_retry_ms` 300–3000/600, `target_attempts` 1–10/3, `target_backoff_ms` 500–30000/2000, `target_stall_ms` 3000–120000/15000, `fight_pause_ms` 1000–60000/10000, `block_mixed_crowd` domyślnie `true` (ale `false` jest prawidłową wartością — panel wysyła je zawsze), reguł czarów ≤ 8, `min_monsters` 1–64/1, `radius` 0.5–`decision_radius`/1, `cooldown_ms` 100–60000/2000, `min_mana_pct` 0–99/0.
- Budżet klawiszy: sufit globalny 8/s (bez zmian), chodzenie 3/s (bez zmian), akcje pięter 3/s (bez zmian), **walka 3/s (nowe)**, leczenie 2/s (bez zmian), nieleczące razem 6/s (bez zmian — obejmuje walkę automatycznie, bo liczy każdy tap ≠ leczenie).
- Nowe klawisze w `hotkeyNames` i w obu tablicach kodów: `escape` (darwin 53, Windows `0x1B`), `space` (darwin 49, Windows `0x20`), `tab` (darwin 48, Windows `0x09`).
- Klawisz `escape` jest zastrzeżony: nie wolno go przypisać jako klawisz akcji piętra, kierunku, leczenia, ataku ani czaru.
- Klawisz `attack_key` i klawisze czarów są jedną kategorią w kontroli kolizji (`keyConflicts`); w obrębie tej kategorii duplikaty są dozwolone (współdzielony cooldown hotkeya), między kategoriami — nie.

---

### Task 1: `fight.Presence` — debounce jednego warunku

Zamiast trackera per stwór: mały typ odpowiadający „czy warunek logiczny trzyma się wystarczająco długo". Wejście w walkę i każda reguła czaru dostają własną instancję.

**Files:**
- Create: `internal/fight/presence.go`
- Test: `internal/fight/presence_test.go`

**Interfaces:**
- Produces: `fight.Presence` (wartość zerowa jest gotowa do użycia, bez konstruktora), `func (p *Presence) Observe(holds bool, capturedAt time.Time, confirm time.Duration) bool`.
- Consumes: nic.

- [ ] **Step 1: Napisz nieprzechodzące testy**

Utwórz `internal/fight/presence_test.go`:

```go
package fight_test

import (
	"testing"
	"time"

	"minimap-lab/internal/fight"
)

func TestPresence(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	type observation struct {
		holds bool
		at    time.Duration // offset from base
	}
	tests := []struct {
		name    string
		observe []observation
		confirm time.Duration
		want    bool // wynik ostatniej obserwacji
	}{
		{
			name:    "dwie klatki i wystarczający czas potwierdzają",
			observe: []observation{{true, 0}, {true, 160 * time.Millisecond}},
			confirm: 150 * time.Millisecond,
			want:    true,
		},
		{
			name:    "dwie klatki ale za mało czasu nie potwierdzają",
			observe: []observation{{true, 0}, {true, 100 * time.Millisecond}},
			confirm: 150 * time.Millisecond,
			want:    false,
		},
		{
			name:    "jedna klatka nigdy nie potwierdza, nawet przy zerowym ConfirmMS",
			observe: []observation{{true, 0}},
			confirm: 0,
			want:    false,
		},
		{
			name: "przerwa dłuższa niż 500 ms zeruje ciągłość",
			observe: []observation{
				{true, 0}, {true, 160 * time.Millisecond},
				{true, 700 * time.Millisecond}, {true, 750 * time.Millisecond},
			},
			confirm: 150 * time.Millisecond,
			// Trzecia obserwacja jest 540 ms po drugiej - dalej niż próg
			// 500 ms - więc zaczyna nowy ciąg. Czwarta jest tylko 50 ms po
			// tym restarcie, za mało samodzielnie.
			want: false,
		},
		{
			name: "fałsz w środku zeruje, kolejne prawdziwe klatki liczą od nowa",
			observe: []observation{
				{true, 0}, {true, 160 * time.Millisecond}, {false, 200 * time.Millisecond},
				{true, 250 * time.Millisecond}, {true, 410 * time.Millisecond},
			},
			confirm: 150 * time.Millisecond,
			want:    true,
		},
		{
			name: "duplikat tej samej klatki nie liczy się jako druga obserwacja",
			observe: []observation{{true, 0}, {true, 0}},
			confirm: 0,
			// Gdyby duplikat (identyczny capturedAt) liczył się jako nowa
			// klatka, ConfirmMS=0 dałoby true już tutaj - druga obserwacja
			// wciąż musi widzieć tylko jedną prawdziwą klatkę.
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p fight.Presence
			var got bool
			for _, o := range tt.observe {
				got = p.Observe(o.holds, base.Add(o.at), tt.confirm)
			}
			if got != tt.want {
				t.Errorf("wynik ostatniej obserwacji = %v, oczekiwano %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie kompilują się**

Run: `go test ./internal/fight/... -v`
Expected: błąd `no required module provides package minimap-lab/internal/fight` / `undefined: fight.Presence` — pakiet jeszcze nie istnieje.

- [ ] **Step 3: Napisz `Presence`**

Utwórz `internal/fight/presence.go`:

```go
// Package fight is a pure decision layer for combat: activity, targeting and
// spell rules. Nothing here owns a clock, reads a pixel or touches the
// keyboard - it takes numbers and a frame's capture time, and hands back a
// decision. That is what lets the whole package be tested as a table, the
// same principle internal/heal already uses.
package fight

import "time"

// gapReset is how long a break between two observations may be before the
// streak they were building restarts. A camera that stalled and came back
// cannot claim it "had" the confirm duration behind it when nobody was
// watching during the gap.
const gapReset = 500 * time.Millisecond

// Presence answers "has this logical condition held long enough" for one
// caller-defined condition. The zero value is ready to use.
type Presence struct {
	since   time.Time
	lastAt  time.Time
	frames  int
	hasLast bool
}

// Observe records whether the condition held on a fresh frame and reports
// whether it has held continuously for at least confirm duration and across
// at least two distinct frames. Three properties, each answering one named
// failure mode:
//   - two frames AND confirm duration: neither alone is enough - one long
//     frame would let a single artefact through on time alone, two frames a
//     heartbeat apart would let two artefacts through on count alone.
//   - a gap longer than gapReset restarts the streak: a camera that stalled
//     cannot claim a duration nobody actually observed.
//   - a duplicate observation at the exact same capturedAt does not count as
//     a second frame - callers already drop duplicate video frames before
//     this is ever called, but Observe stays correct even if one slips
//     through.
func (p *Presence) Observe(holds bool, capturedAt time.Time, confirm time.Duration) bool {
	if !holds {
		*p = Presence{}
		return false
	}
	switch {
	case !p.hasLast:
		p.since, p.frames = capturedAt, 1
	case capturedAt.Equal(p.lastAt):
		// Same instant seen twice - not a new observation.
	case capturedAt.Sub(p.lastAt) > gapReset:
		p.since, p.frames = capturedAt, 1
	default:
		p.frames++
	}
	p.lastAt, p.hasLast = capturedAt, true
	return p.frames >= 2 && capturedAt.Sub(p.since) >= confirm
}
```

- [ ] **Step 4: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/fight/... -v`
Expected: PASS (jeden test z sześcioma podtestami).

- [ ] **Step 5: Commit**

```bash
cd minimap-lab
git add internal/fight/presence.go internal/fight/presence_test.go
git commit -m "Dodaj debounce jednego warunku (fight.Presence)"
```

---

### Task 2: `fight.Activity` — maszyna stanów Travelling/Fighting

**Files:**
- Create: `internal/fight/activity.go`
- Test: `internal/fight/activity_test.go`

**Interfaces:**
- Consumes: `fight.Presence` (Task 1).
- Produces: `fight.State` (`Travelling`, `Fighting`), `fight.Observation`, `fight.Options`, `fight.Transition` (`None`, `Entered`, `Left`), `fight.Activity` z metodami `Observe(o Observation, opts Options) Transition`, `State() State`, `Force(to State)`.

- [ ] **Step 1: Napisz nieprzechodzące testy**

Utwórz `internal/fight/activity_test.go`:

```go
package fight_test

import (
	"testing"
	"time"

	"minimap-lab/internal/fight"
)

func opts() fight.Options {
	return fight.Options{
		Confirm: 150 * time.Millisecond, LeaveFight: 600 * time.Millisecond,
		TargetRetry: 600 * time.Millisecond, TargetBackoff: 2 * time.Second,
		TargetStall: 15 * time.Second, TargetAttempts: 3,
	}
}

// ready is a valid "fight now" observation; tests mutate copies of it.
func ready(at time.Time) fight.Observation {
	return fight.Observation{Enabled: true, InRange: 1, BattleRead: true, Rows: 1, CapturedAt: at}
}

func TestActivityEntersAfterConfirm(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	if tr := a.Observe(ready(base), opts()); tr != fight.None {
		t.Fatalf("pierwsza klatka nie może wejść od razu, dostałem %v", tr)
	}
	if a.State() != fight.Travelling {
		t.Fatal("stan po pierwszej klatce musi zostać Travelling")
	}
	tr := a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	if tr != fight.Entered {
		t.Fatalf("druga klatka po 160 ms powinna wejść, dostałem %v", tr)
	}
	if a.State() != fight.Fighting {
		t.Fatal("stan po wejściu musi być Fighting")
	}
}

func TestActivityRequiresNonEmptyBattleList(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	o := ready(base)
	o.Rows = 0
	a.Observe(o, opts())
	o2 := ready(base.Add(160 * time.Millisecond))
	o2.Rows = 0
	if tr := a.Observe(o2, opts()); tr != fight.None {
		t.Fatalf("paski bez wierszy battle listy nie mogą wejść w walkę, dostałem %v", tr)
	}
	if a.State() != fight.Travelling {
		t.Fatal("bez wierszy stan musi zostać Travelling")
	}
}

func TestActivityUnknownBattleListBlocksEntryWithoutDrivingExit(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	// Enter fighting first with a real read.
	a.Observe(ready(base), opts())
	a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	if a.State() != fight.Fighting {
		t.Fatal("setup: powinniśmy być w Fighting")
	}
	// A frame where the battle region simply did not arrive (BattleRead=false,
	// Rows=0) must not look like "no rows" and drive an exit.
	unknown := fight.Observation{Enabled: true, InRange: 1, BattleRead: false, Rows: 0,
		CapturedAt: base.Add(320 * time.Millisecond)}
	for i := 0; i < 10; i++ {
		unknown.CapturedAt = base.Add(320*time.Millisecond + time.Duration(i)*100*time.Millisecond)
		if tr := a.Observe(unknown, opts()); tr == fight.Left {
			t.Fatalf("nieodczytana battle lista nie może napędzać wyjścia (i=%d)", i)
		}
	}
	if a.State() != fight.Fighting {
		t.Fatal("nieodczytana battle lista nie może wypchnąć z Fighting")
	}
}

func TestActivityLeavesAfterConfirm(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	a.Observe(ready(base), opts())
	a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	empty := fight.Observation{Enabled: true, BattleRead: true, Rows: 0, InRange: 0}
	empty.CapturedAt = base.Add(320 * time.Millisecond)
	if tr := a.Observe(empty, opts()); tr != fight.None {
		t.Fatalf("pierwsza pusta klatka nie może wyjść od razu, dostałem %v", tr)
	}
	empty.CapturedAt = base.Add(920 * time.Millisecond) // +600ms od since
	tr := a.Observe(empty, opts())
	if tr != fight.Left {
		t.Fatalf("po 600 ms braku celu powinniśmy wyjść, dostałem %v", tr)
	}
	if a.State() != fight.Travelling {
		t.Fatal("stan po wyjściu musi być Travelling")
	}
}

func TestActivityVisibleTargetFrameOverridesEmptyExit(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	a.Observe(ready(base), opts())
	a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	// The creature fled out of decision radius, but the client is still
	// chasing it and the attack frame is still on the battle list row.
	fleeing := fight.Observation{Enabled: true, BattleRead: true, Rows: 1, InRange: 0, HasTarget: true}
	for i := 0; i < 10; i++ {
		fleeing.CapturedAt = base.Add(320*time.Millisecond + time.Duration(i)*200*time.Millisecond)
		if tr := a.Observe(fleeing, opts()); tr == fight.Left {
			t.Fatalf("widoczna ramka celu musi trzymać w walce (i=%d)", i)
		}
	}
	if a.State() != fight.Fighting {
		t.Fatal("z widoczną ramką celu nie wolno wyjść z Fighting")
	}
}

func TestActivityFlickerAtRadiusEdgeGivesAtMostOneEntryAndExitPerCycle(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	entries, exits := 0, 0
	for i := 0; i < 40; i++ {
		o := ready(base.Add(time.Duration(i) * 100 * time.Millisecond))
		if i%2 == 0 {
			o.InRange = 0
		}
		switch a.Observe(o, opts()) {
		case fight.Entered:
			entries++
		case fight.Left:
			exits++
		}
	}
	if entries > 1 || exits > 1 {
		t.Fatalf("migający stwór na granicy promienia dał %d wejść i %d wyjść w jednym cyklu, oczekiwano co najwyżej 1 i 1", entries, exits)
	}
}

func TestActivityBlockedPreventsEntry(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	o := ready(base)
	o.Blocked = true
	a.Observe(o, opts())
	o2 := ready(base.Add(160 * time.Millisecond))
	o2.Blocked = true
	if tr := a.Observe(o2, opts()); tr != fight.None {
		t.Fatalf("Blocked musi zablokować wejście, dostałem %v", tr)
	}
	if a.State() != fight.Travelling {
		t.Fatal("zablokowane wejście musi zostawić stan Travelling")
	}
}

func TestActivityForce(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var a fight.Activity
	a.Observe(ready(base), opts())
	a.Observe(ready(base.Add(160*time.Millisecond)), opts())
	if a.State() != fight.Fighting {
		t.Fatal("setup: powinniśmy być w Fighting")
	}
	a.Force(fight.Travelling)
	if a.State() != fight.Travelling {
		t.Fatal("Force musi ustawić stan natychmiast")
	}
	// Force musi też wyczyścić debounce wejścia: pojedyncza klatka zaraz po
	// Force nie może od razu wejść, jakby confirm już trwał od dawna.
	if tr := a.Observe(ready(base.Add(200*time.Millisecond)), opts()); tr != fight.None {
		t.Fatalf("po Force wejście musi znowu przejść pełny debounce, dostałem %v", tr)
	}
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie kompilują się**

Run: `go test ./internal/fight/... -run TestActivity -v`
Expected: błąd kompilacji — `fight.Activity`, `fight.Observation`, `fight.Options`, `fight.State`, `fight.Transition` jeszcze nie istnieją.

- [ ] **Step 3: Napisz `Activity`**

Utwórz `internal/fight/activity.go`:

```go
package fight

import "time"

// State is one of the two activities the loop can be in during this phase.
// Retreating (phase 5) and Looting (phase 4) are not part of this type yet -
// see the design spec's "Co zostaje na potem".
type State int

const (
	Travelling State = iota
	Fighting
)

func (s State) String() string {
	if s == Fighting {
		return "fighting"
	}
	return "travelling"
}

// Observation is what one frame tells Activity about the world. It carries
// no interpretation of its own: brain/fight.go computes every field from the
// loop's own state and hands over plain facts.
type Observation struct {
	// Enabled is the "Atakuj" switch and a calibrated battle list, combined -
	// Activity does not need to know which one is false.
	Enabled bool
	// InRange is the count of confirmed creatures within the decision
	// radius, after the map sieve - the same number the vision snapshot
	// publishes as MonstersInRange.
	InRange int
	// BattleRead is false when the battle-list region simply did not arrive
	// in this frame. Rows and HasTarget are meaningless when this is false.
	BattleRead bool
	Rows       int
	HasTarget  bool
	// Blocked is a stall pause or a pending Escape - either one refuses
	// entry into Fighting outright.
	Blocked    bool
	CapturedAt time.Time
}

// Options are the FightConfig durations, converted once per SetConfig rather
// than on every frame, and shared by Activity, Targeter and Engine so the
// loop does not juggle seven numbers at every call site.
type Options struct {
	Confirm        time.Duration
	LeaveFight     time.Duration
	TargetRetry    time.Duration
	TargetBackoff  time.Duration
	TargetStall    time.Duration
	TargetAttempts int
}

// Transition reports whether Observe just crossed a state boundary.
type Transition int

const (
	None Transition = iota
	Entered
	Left
)

// Activity is the Travelling/Fighting state machine. It holds no clock and
// touches nothing outside itself: Observe is a pure function of its inputs
// and its own debounce state.
type Activity struct {
	state   State
	inRange Presence
	leave   Presence
}

func (a *Activity) State() State { return a.state }

// Force sets the state directly, bypassing debounce, for the panel's
// "Atakuj" switch turning off and for a disarmed driver - both must take
// effect on the same frame, not after ConfirmMS. It also clears both
// debounce streaks, so a later entry always pays the full confirm duration
// again rather than resuming a streak that predates the forced change.
func (a *Activity) Force(to State) {
	a.state = to
	a.inRange = Presence{}
	a.leave = Presence{}
}

// Observe advances the machine by one frame and reports whether it just
// crossed into or out of Fighting.
func (a *Activity) Observe(o Observation, opts Options) Transition {
	switch a.state {
	case Travelling:
		ready := o.Enabled && o.BattleRead && o.Rows > 0 && o.InRange >= 1 && !o.Blocked
		confirmed := a.inRange.Observe(ready, o.CapturedAt, opts.Confirm)
		if !ready {
			return None
		}
		if confirmed {
			a.state = Fighting
			a.leave = Presence{}
			return Entered
		}
		return None
	case Fighting:
		// An unread battle list is unknown, not empty: Rows==0 from a frame
		// that never carried the region must never look like "no rows" and
		// drive an exit.
		wantLeave := o.BattleRead && !o.HasTarget && (o.InRange == 0 || o.Rows == 0)
		confirmed := a.leave.Observe(wantLeave, o.CapturedAt, opts.LeaveFight)
		if !wantLeave {
			return None
		}
		if confirmed {
			a.state = Travelling
			a.inRange = Presence{}
			return Left
		}
		return None
	}
	return None
}
```

- [ ] **Step 4: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/fight/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd minimap-lab
git add internal/fight/activity.go internal/fight/activity_test.go
git commit -m "Dodaj maszynę stanów Travelling/Fighting"
```

---

### Task 3: `fight.Targeter` — celowanie, backoff, bezpiecznik zastoju

**Files:**
- Create: `internal/fight/targeter.go`
- Test: `internal/fight/targeter_test.go`

**Interfaces:**
- Consumes: nic z poprzednich zadań (niezależny od `Activity`).
- Produces: `fight.TargetInput`, `fight.TargetDecision`, `fight.Targeter` z metodami `Decide(in TargetInput, opts Options) TargetDecision`, `Tapped(at time.Time)`, `Reset()`, `Attempts() int`, `BackoffUntil() (time.Time, bool)`. Zadanie 7 (`fightSnapshot`) woła `Attempts()` i `BackoffUntil()` do publikacji stanu.

- [ ] **Step 1: Napisz nieprzechodzące testy**

Utwórz `internal/fight/targeter_test.go`:

```go
package fight_test

import (
	"testing"
	"time"

	"minimap-lab/internal/fight"
)

func TestTargeterTapsImmediatelyWhenNoTargetEverSeen(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base}, opts())
	if !d.Tap {
		t.Fatalf("pierwsze stuknięcie musi polecieć od razu, dostałem %+v", d)
	}
}

func TestTargeterWaitsRetryAfterEmission(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base}, opts())
	tg.Tapped(base)
	d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base.Add(300 * time.Millisecond)}, opts())
	if d.Tap {
		t.Fatal("drugie stuknięcie przed upływem target_retry_ms nie może polecieć")
	}
	d = tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base.Add(600 * time.Millisecond)}, opts())
	if !d.Tap {
		t.Fatal("po dokładnie target_retry_ms od emisji drugie stuknięcie powinno polecieć")
	}
}

func TestTargeterOneFrameFlickerDoesNotRetap(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	// The frame is visible, then vanishes for exactly one frame.
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 1, Rows: 1, CapturedAt: base}, opts())
	d := tg.Decide(fight.TargetInput{HasTarget: false, Rows: 1, CapturedAt: base.Add(100 * time.Millisecond)}, opts())
	if d.Tap {
		t.Fatal("jednoklatkowy zanik ramki nie może stuknąć - anchor liczy się od ostatniego widzenia")
	}
}

func TestTargeterRefusalDoesNotCountAsAttempt(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	// Three decisions to tap, but Tapped is never called (as if the driver
	// refused every one) - attempts must stay at zero, so no backoff fires.
	for i := 0; i < 5; i++ {
		at := base.Add(time.Duration(i) * 700 * time.Millisecond)
		d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		if !d.Tap {
			t.Fatalf("próba %d: bez potwierdzonych emisji nigdy nie powinniśmy wpaść w backoff, dostałem %+v", i, d)
		}
	}
}

func TestTargeterBacksOffAfterThreeEmissionsWithoutFrame(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	at := base
	for i := 0; i < 3; i++ {
		d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		if !d.Tap {
			t.Fatalf("emisja %d powinna polecieć, dostałem %+v", i, d)
		}
		tg.Tapped(at)
		at = at.Add(700 * time.Millisecond)
	}
	d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
	if d.Tap {
		t.Fatal("po trzech emisjach bez ramki czwarta próba musi wejść w backoff, nie stuknąć")
	}
	// Still inside target_backoff_ms (2s default).
	d = tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at.Add(500 * time.Millisecond)}, opts())
	if d.Tap {
		t.Fatal("w trakcie backoffu nie wolno stukać")
	}
	// Past the backoff window: a fresh set of attempts.
	d = tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at.Add(2100 * time.Millisecond)}, opts())
	if !d.Tap {
		t.Fatal("po upływie backoffu celownik powinien znów spróbować")
	}
}

func TestTargeterFrameResetsAttempts(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	at := base
	for i := 0; i < 2; i++ {
		tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		tg.Tapped(at)
		at = at.Add(700 * time.Millisecond)
	}
	// The frame appears - attempts must reset to zero.
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 1, Rows: 1, CapturedAt: at}, opts())
	at = at.Add(100 * time.Millisecond)
	// It vanishes again; three MORE fruitless emissions must be needed before
	// backing off again, not just one.
	for i := 0; i < 2; i++ {
		d := tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		if !d.Tap {
			t.Fatalf("próba %d po zresetowaniu powinna polecieć, dostałem %+v", i, d)
		}
		tg.Tapped(at)
		at = at.Add(700 * time.Millisecond)
	}
}

func TestTargeterStallsAfterFifteenSecondsWithoutHPDrop(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.9, Rows: 1, CapturedAt: base}, opts())
	d := tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.9, Rows: 1,
		CapturedAt: base.Add(14 * time.Second)}, opts())
	if d.Stall {
		t.Fatal("14 s bez spadku HP to jeszcze nie zastój (próg to 15 s)")
	}
	d = tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.9, Rows: 1,
		CapturedAt: base.Add(15 * time.Second)}, opts())
	if !d.Stall {
		t.Fatal("15 s bez spadku minimalnego HP musi zgłosić zastój")
	}
}

func TestTargeterHPDropResetsStall(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.9, Rows: 1, CapturedAt: base}, opts())
	// HP drops at 10s - a genuine hit, must reset stallSince.
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.7, Rows: 1,
		CapturedAt: base.Add(10 * time.Second)}, opts())
	d := tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.7, Rows: 1,
		CapturedAt: base.Add(24 * time.Second)}, opts())
	if d.Stall {
		t.Fatal("spadek HP w 10 s powinien zresetować licznik zastoju - 14 s później to za mało")
	}
}

func TestTargeterNewTargetResetsStall(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 0.5, Rows: 1, CapturedAt: base}, opts())
	// The target vanishes (killed or lost) and a new one appears later.
	tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: base.Add(1 * time.Second)}, opts())
	tg.Tapped(base.Add(1 * time.Second))
	newTarget := base.Add(14 * time.Second)
	d := tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 1, Rows: 1, CapturedAt: newTarget}, opts())
	if d.Stall {
		t.Fatal("nowy cel nie może odziedziczyć zastoju poprzedniego")
	}
	d = tg.Decide(fight.TargetInput{HasTarget: true, TargetHP: 1, Rows: 1,
		CapturedAt: newTarget.Add(14 * time.Second)}, opts())
	if d.Stall {
		t.Fatal("14 s od nowego celu to wciąż za mało na zastój")
	}
}

func TestTargeterAttemptsAndBackoffUntilGetters(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tg fight.Targeter
	if n := tg.Attempts(); n != 0 {
		t.Fatalf("świeży Targeter powinien mieć zero prób, dostałem %d", n)
	}
	if _, ok := tg.BackoffUntil(); ok {
		t.Fatal("świeży Targeter nie powinien mieć aktywnego backoffu")
	}
	at := base
	for i := 0; i < 3; i++ {
		tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
		tg.Tapped(at)
		at = at.Add(700 * time.Millisecond)
	}
	if n := tg.Attempts(); n != 3 {
		t.Fatalf("po trzech potwierdzonych emisjach oczekiwałem 3 prób, dostałem %d", n)
	}
	tg.Decide(fight.TargetInput{Rows: 1, CapturedAt: at}, opts())
	until, ok := tg.BackoffUntil()
	if !ok {
		t.Fatal("po czwartej próbie bez ramki backoff powinien być aktywny")
	}
	if want := at.Add(2 * time.Second); !until.Equal(want) {
		t.Fatalf("backoff powinien kończyć się o %v, dostałem %v", want, until)
	}
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie kompilują się**

Run: `go test ./internal/fight/... -run TestTargeter -v`
Expected: błąd kompilacji — `fight.Targeter`, `fight.TargetInput`, `fight.TargetDecision` jeszcze nie istnieją.

- [ ] **Step 3: Napisz `Targeter`**

Utwórz `internal/fight/targeter.go`:

```go
package fight

import "time"

// TargetInput is what one frame tells Targeter about the battle list's
// target row.
type TargetInput struct {
	HasTarget bool
	// TargetHP is 0-1, the targeted row's health bar - meaningful only when
	// HasTarget is true.
	TargetHP   float64
	Rows       int
	CapturedAt time.Time
}

// TargetDecision is what Targeter wants done this frame.
type TargetDecision struct {
	// Tap presses the configured attack key.
	Tap bool
	// Stall reports that the target has not lost health in TargetStall - the
	// loop should back out of the fight and pause, not keep trying.
	Stall  bool
	Reason string
}

// Targeter decides when to press the "attack next target" key, backs off
// after repeated fruitless presses, and watches for a target that never
// loses health (out of reach, behind an obstacle the client cannot path
// around).
type Targeter struct {
	lastTap  time.Time
	hasTap   bool
	lastSeen time.Time
	hasSeen  bool
	attempts int

	backoffUntil time.Time
	hasBackoff   bool

	minHP    float64
	hasMinHP bool

	stallSince time.Time
	hasStall   bool
}

// Decide answers what to do this frame. It never mutates emission-related
// state itself (attempts, the retry anchor) except through Tapped, which the
// caller invokes only once the driver confirms the key actually went out -
// a refused tap must cost nothing.
func (t *Targeter) Decide(in TargetInput, opts Options) TargetDecision {
	if in.HasTarget {
		t.lastSeen, t.hasSeen = in.CapturedAt, true
		t.attempts = 0
		if !t.hasMinHP || in.TargetHP < t.minHP {
			t.minHP, t.hasMinHP = in.TargetHP, true
			t.stallSince, t.hasStall = in.CapturedAt, true
		}
		if t.hasStall && in.CapturedAt.Sub(t.stallSince) >= opts.TargetStall {
			return TargetDecision{Stall: true, Reason: "cel nie traci HP"}
		}
		return TargetDecision{}
	}

	// No target this frame: whatever minimum-HP streak we were tracking
	// belongs to a target that is no longer visible.
	t.hasMinHP, t.hasStall = false, false

	if in.Rows == 0 {
		return TargetDecision{Reason: "brak wierszy battle listy"}
	}
	now := in.CapturedAt
	if t.hasBackoff {
		if now.Before(t.backoffUntil) {
			return TargetDecision{Reason: "backoff celowania"}
		}
		t.hasBackoff, t.attempts = false, 0
	}
	if t.attempts >= opts.TargetAttempts {
		t.hasBackoff, t.backoffUntil = true, now.Add(opts.TargetBackoff)
		return TargetDecision{Reason: "zbyt wiele prób bez ramki"}
	}
	// The anchor is the later of the last confirmed emission and the last
	// frame that actually showed a target - whichever gives the freshest
	// evidence about the client's state.
	anchor, anchorSet := t.lastSeen, t.hasSeen
	if t.hasTap && (!anchorSet || t.lastTap.After(anchor)) {
		anchor, anchorSet = t.lastTap, true
	}
	if anchorSet && now.Sub(anchor) < opts.TargetRetry {
		return TargetDecision{Reason: "za wcześnie na kolejną próbę"}
	}
	return TargetDecision{Tap: true}
}

// Tapped records that the attack key actually left the driver. Only this
// counts toward the attempt budget - a decision the driver refused changed
// nothing on screen.
func (t *Targeter) Tapped(at time.Time) {
	t.lastTap, t.hasTap = at, true
	t.attempts++
}

// Attempts reports how many fruitless emissions have happened since the
// last confirmed sighting or the last backoff reset - fightSnapshot (Task 7)
// publishes it so the panel can show why targeting is struggling.
func (t *Targeter) Attempts() int { return t.attempts }

// BackoffUntil reports the deadline of an active backoff, if one is
// currently running. The bool is false once the backoff has been consumed
// or none has ever started.
func (t *Targeter) BackoffUntil() (time.Time, bool) { return t.backoffUntil, t.hasBackoff }

// Reset clears all state, for leaving Fighting: a target lost when we walk
// away is not evidence about the next one.
func (t *Targeter) Reset() { *t = Targeter{} }
```

- [ ] **Step 4: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/fight/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd minimap-lab
git add internal/fight/targeter.go internal/fight/targeter_test.go
git commit -m "Dodaj celownik z próbami, backoffem i bezpiecznikiem zastoju"
```

---

### Task 4: `fight.Engine` — silnik reguł czarów

**Files:**
- Create: `internal/fight/rules.go`
- Test: `internal/fight/rules_test.go`

**Interfaces:**
- Consumes: `fight.Presence` (Task 1), `vitals.Reading` (istniejący typ `internal/vitals`, pola `Percent float64`, `OK bool`, `Reason string`).
- Produces: `fight.Rule` (z tagami JSON), `fight.Crowd` z metodą `Within(radius float64) int`, `fight.Decision`, `fight.Engine` z metodami `SetRules([]Rule)`, `Decide(crowd Crowd, mana vitals.Reading, hasTarget, blockMixed bool, capturedAt time.Time, confirm time.Duration) Decision`, `Emitted(hotkey string, at time.Time)`.

- [ ] **Step 1: Napisz nieprzechodzące testy**

Utwórz `internal/fight/rules_test.go`:

```go
package fight_test

import (
	"testing"
	"time"

	"minimap-lab/internal/fight"
	"minimap-lab/internal/vitals"
)

func fullMana() vitals.Reading { return vitals.Reading{Percent: 1, OK: true} }

// confirmedCrowd advances a fresh Engine's per-rule debounce until every
// rule's own condition (count >= MinMonsters within Radius) reads confirmed,
// then returns the same Crowd for the caller's actual assertion.
func confirmedCrowd(e *fight.Engine, crowd fight.Crowd, mana vitals.Reading, hasTarget, blockMixed bool, at time.Time, confirm time.Duration) time.Time {
	// Two observations, gapReset apart in spirit but well inside it, are
	// enough to confirm at any tested ConfirmMS in this file (150ms cases).
	e.Decide(crowd, mana, hasTarget, blockMixed, at, confirm)
	return at.Add(confirm + 10*time.Millisecond)
}

func TestEngineFirstFeasibleNotFirstMatching(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{
		{Enabled: true, Hotkey: "f2", MinMonsters: 4, Radius: 2, CooldownMS: 2000},
		{Enabled: true, Hotkey: "f3", MinMonsters: 1, Radius: 2, CooldownMS: 2000},
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	// Only 2 monsters present: rule 1 (needs 4) matches nothing, rule 2
	// (needs 1) is feasible and must fire, even though it is second in line.
	crowd := fight.Crowd{Distances: []float64{1, 1.5}}
	at := confirmedCrowd(e, crowd, fullMana(), false, true, base, confirm)
	d := e.Decide(crowd, fullMana(), false, true, at, confirm)
	if !d.Fire || d.Hotkey != "f3" {
		t.Fatalf("oczekiwano f3 (pierwsza wykonalna), dostałem %+v", d)
	}
}

func TestEngineCooldownSharedByHotkeyAcrossRules(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{
		{Enabled: true, Hotkey: "f2", MinMonsters: 4, Radius: 1, CooldownMS: 2000},
		{Enabled: true, Hotkey: "f2", MinMonsters: 1, Radius: 2, CooldownMS: 2000},
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	crowd := fight.Crowd{Distances: []float64{0.5, 0.5, 0.5, 0.5}}
	at := confirmedCrowd(e, crowd, fullMana(), false, true, base, confirm)
	d := e.Decide(crowd, fullMana(), false, true, at, confirm)
	if !d.Fire || d.Hotkey != "f2" {
		t.Fatalf("pierwsza reguła f2 powinna polecieć, dostałem %+v", d)
	}
	e.Emitted("f2", at)
	// Immediately after, rule 1's own cooldown blocks it - but rule 2 shares
	// the SAME hotkey's cooldown, so it must not fire either, even though its
	// own crowd count (1 needed, 4 present) would otherwise be feasible.
	d = e.Decide(crowd, fullMana(), false, true, at.Add(10*time.Millisecond), confirm)
	if d.Fire {
		t.Fatalf("cooldown klawisza f2 musi zablokować obie reguły, dostałem %+v", d)
	}
}

func TestEngineRequiresTarget(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{{Enabled: true, Hotkey: "f4", MinMonsters: 1, Radius: 2, CooldownMS: 1000, RequiresTarget: true}})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	crowd := fight.Crowd{Distances: []float64{1}}
	at := confirmedCrowd(e, crowd, fullMana(), false, true, base, confirm)
	d := e.Decide(crowd, fullMana(), false, true, at, confirm)
	if d.Fire {
		t.Fatalf("reguła RequiresTarget bez celu nie może polecieć, dostałem %+v", d)
	}
	d = e.Decide(crowd, fullMana(), true, true, at, confirm)
	if !d.Fire {
		t.Fatalf("z celem ta sama reguła powinna polecieć, dostałem %+v", d)
	}
}

func TestEngineUnreadableManaBlocksOnlyRulesThatCostMana(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{
		{Enabled: true, Hotkey: "f5", MinMonsters: 1, Radius: 2, CooldownMS: 1000, MinManaPct: 50},
		{Enabled: true, Hotkey: "f6", MinMonsters: 1, Radius: 2, CooldownMS: 1000, MinManaPct: 0},
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	crowd := fight.Crowd{Distances: []float64{1}}
	badMana := vitals.Reading{OK: false, Reason: "kalibracja"}
	at := confirmedCrowd(e, crowd, badMana, false, true, base, confirm)
	d := e.Decide(crowd, badMana, false, true, at, confirm)
	if !d.Fire || d.Hotkey != "f6" {
		t.Fatalf("reguła bez wymogu many musi polecieć mimo nieczytelnego odczytu, dostałem %+v", d)
	}
}

func TestEngineMixedCrowdBlocksOnlyAoE(t *testing.T) {
	e := fight.NewEngine()
	e.SetRules([]fight.Rule{
		{Enabled: true, Hotkey: "f7", MinMonsters: 3, Radius: 2, CooldownMS: 1000},
		{Enabled: true, Hotkey: "f8", MinMonsters: 1, Radius: 2, CooldownMS: 1000},
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirm := 150 * time.Millisecond
	crowd := fight.Crowd{Distances: []float64{0.5, 0.5, 0.5}, Mixed: true}
	at := confirmedCrowd(e, crowd, fullMana(), false, true, base, confirm)
	d := e.Decide(crowd, fullMana(), false, true, at, confirm)
	if !d.Fire || d.Hotkey != "f8" {
		t.Fatalf("mieszany tłum musi zablokować tylko regułę AoE (MinMonsters>1), dostałem %+v", d)
	}
}

func TestEngineWithinRoundsToNearestWholeTile(t *testing.T) {
	crowd := fight.Crowd{Distances: []float64{1.4, 1.5, 1.6}}
	if n := crowd.Within(1); n != 2 {
		t.Errorf("Within(1) = %d, oczekiwano 2 (próg 1,5)", n)
	}
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie kompilują się**

Run: `go test ./internal/fight/... -run TestEngine -v`
Expected: błąd kompilacji — `fight.Engine`, `fight.Rule`, `fight.Crowd` jeszcze nie istnieją.

- [ ] **Step 3: Napisz `Engine`**

Utwórz `internal/fight/rules.go`:

```go
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
	Enabled bool `json:"enabled"`
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
```

- [ ] **Step 4: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/fight/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd minimap-lab
git add internal/fight/rules.go internal/fight/rules_test.go
git commit -m "Dodaj silnik reguł czarów"
```

---

### Task 5: Sterownik — `Cast`, `CancelTarget`, budżet walki, nowe klawisze

**Files:**
- Modify: `internal/input/driver.go` (stałe budżetu, `purpose`, `purposeLimit`, nowe metody)
- Modify: `internal/input/input.go` (`hotkeyNames`)
- Modify: `internal/input/input_darwin.go` (`darwinKeys`)
- Modify: `internal/input/input_windows.go` (`windowsKeys`)
- Test: `internal/input/driver_test.go`

**Interfaces:**
- Consumes: nic z `internal/fight` — ten pakiet nie wie nic o walce, tylko o klawiszach.
- Produces: `func (d *Driver) Cast(key string, observationAge time.Duration) Result`, `func (d *Driver) CancelTarget(observationAge time.Duration) Result`; `hotkeyNames["escape"|"space"|"tab"] == true`.

- [ ] **Step 1: Napisz nieprzechodzące testy**

Znajdź w `internal/input/driver_test.go` istniejący test budżetu leczenia (np. `TestHealRespectsItsOwnBudget` albo podobny — poszukaj testu, który uzbraja `Driver` przez `NewDriver(&DryEmitter{...}, ...)`, woła `Arm()`, po czym stuka `Heal` cztery razy w pętli i sprawdza, że czwarte jest odmówione) i dopisz obok niego:

```go
func TestCastAndCancelTargetCountInCombatBudget(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 1}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	for i := 0; i < 3; i++ {
		res := d.Cast("f4", 0)
		if res.Status != "emitted" {
			t.Fatalf("czar %d: status = %s, oczekiwano emitted", i, res.Status)
		}
	}
	// Fourth combat-purpose tap in the same second must be refused - Cast and
	// CancelTarget share one budget row.
	if res := d.CancelTarget(0); res.Status != "refused" {
		t.Fatalf("czwarty klawisz walki w tej samej sekundzie: status = %s, oczekiwano refused", res.Status)
	}
}

func TestCastRefusesUnknownKey(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 1}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if res := d.Cast("nieznany-klawisz", 0); res.Status != "refused" {
		t.Fatalf("nieznany klawisz czaru: status = %s, oczekiwano refused", res.Status)
	}
}

func TestCancelTargetTapsEscape(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 1}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	res := d.CancelTarget(0)
	if res.Status != "emitted" || res.Key != "escape" {
		t.Fatalf("CancelTarget = %+v, oczekiwano emitted/escape", res)
	}
}

func TestCombatBudgetDoesNotBorrowFromHealReserve(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 1}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	// Six non-heal taps (the shared non-heal fence) must not block a
	// subsequent heal - the heal reserve stays carved out regardless of how
	// combat taps are spent.
	for i := 0; i < 3; i++ {
		d.Cast("f4", 0)
	}
	d.CancelTarget(0)
	d.Walk("N", 0)
	d.Walk("N", 0)
	if res := d.Heal("f1", 0); res.Status != "emitted" {
		t.Fatalf("leczenie po sześciu nieleczących stuknięciach: status = %s, oczekiwano emitted", res.Status)
	}
}

func TestValidHotkeyAcceptsEscapeSpaceTab(t *testing.T) {
	for _, k := range []string{"escape", "space", "tab"} {
		if !ValidHotkey(k) {
			t.Errorf("ValidHotkey(%q) = false, oczekiwano true", k)
		}
	}
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie kompilują się**

Run: `go test ./internal/input/... -run 'TestCast|TestCancelTarget|TestCombatBudget|TestValidHotkeyAcceptsEscape' -v`
Expected: błąd kompilacji — `d.Cast`/`d.CancelTarget` jeszcze nie istnieją.

- [ ] **Step 3: Dodaj `escape`, `space`, `tab` do `hotkeyNames`**

W `internal/input/input.go`, w literale `hotkeyNames`, dołóż trzy wpisy (przed zamykającym `}`):

```go
	"escape": true, "space": true, "tab": true,
```

- [ ] **Step 4: Dodaj kody klawiszy w obu tablicach platformowych**

W `internal/input/input_darwin.go`, w `darwinKeys`, dołóż:

```go
	"escape": 53, "space": 49, "tab": 48,
```

W `internal/input/input_windows.go`, w `windowsKeys`, dołóż:

```go
	"escape": 0x1B, "space": 0x20, "tab": 0x09,
```

- [ ] **Step 5: Rozbij budżet o przeznaczenie walki**

W `internal/input/driver.go`, w bloku stałych, dołóż limit i zmień sufit nieleczący, żeby dalej liczyć wszystko poza leczeniem (bez zmian w jego wartości — 6 już obejmuje walkę, bo `allowTapLocked` liczy każdy tap `≠ purposeHeal`):

```go
	maxCombatTapsPerSecond  = 3
```

(wstaw obok `maxHealTapsPerSecond = 2`, przed `maxNonHealTapsPerSecond`).

W bloku `purpose`, dołóż nową wartość:

```go
type purpose int

const (
	purposeWalk purpose = iota
	purposeAction
	purposeHeal
	purposeCombat
)
```

W `purposeLimit`, dodaj jawny przypadek — `default` musi zostać jedynym, jednoznacznym miejscem dla `purposeHeal`, więc `purposeCombat` potrzebuje własnego `case`, inaczej trafiłby błędnie w `default` (limit leczenia, 2, zamiast 3):

```go
func purposeLimit(p purpose) int {
	switch p {
	case purposeWalk:
		return maxWalkTapsPerSecond
	case purposeAction:
		return maxActionTapsPerSecond
	case purposeCombat:
		return maxCombatTapsPerSecond
	default:
		return maxHealTapsPerSecond
	}
}
```

- [ ] **Step 6: Dodaj `Cast` i `CancelTarget`**

W `internal/input/driver.go`, zaraz po `Heal`, dołóż:

```go
// Cast taps one spell hotkey. Like Heal, the key is literal - it comes
// straight from the panel's rule list - and carries no "in flight" semantics:
// a floor action stays pending until the panel confirms the floor changed,
// and a cast spell has nothing to confirm.
func (d *Driver) Cast(key string, observationAge time.Duration) Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !hotkeyNames[key] {
		return Result{Status: "refused", Reason: "nieznany klawisz czaru: " + key}
	}
	if res, ok := d.guardLocked(observationAge, purposeCombat); !ok {
		return res
	}
	if err := d.em.TapKey(key, holdMS); err != nil {
		return d.emitterFailureLocked(err)
	}
	d.taps = append(d.taps, tap{at: d.now(), p: purposeCombat})
	return Result{Status: "emitted", Key: key}
}

// CancelTarget taps Escape, which the client reads as "stop attacking and
// stop chasing". Unlike Cast, the key is fixed: Escape has exactly one
// meaning in this project, so there is nothing for a caller to get wrong.
func (d *Driver) CancelTarget(observationAge time.Duration) Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	if res, ok := d.guardLocked(observationAge, purposeCombat); !ok {
		return res
	}
	if err := d.em.TapKey("escape", holdMS); err != nil {
		return d.emitterFailureLocked(err)
	}
	d.taps = append(d.taps, tap{at: d.now(), p: purposeCombat})
	return Result{Status: "emitted", Key: "escape"}
}
```

- [ ] **Step 7: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/input/... -race -v`
Expected: PASS, łącznie z nowymi testami; wszystkie istniejące budżetowe testy dalej przechodzą (sufit globalny 8 i nieleczący 6 są niezmienione).

- [ ] **Step 8: Commit**

```bash
cd minimap-lab
git add internal/input/driver.go internal/input/input.go internal/input/input_darwin.go internal/input/input_windows.go internal/input/driver_test.go
git commit -m "Dodaj Cast i CancelTarget w przeznaczeniu walki"
```

---

### Task 6: `FightConfig` — konfiguracja z walidacją

**Files:**
- Create: `internal/brain/fightconfig.go`
- Test: `internal/brain/fightconfig_test.go`

**Interfaces:**
- Consumes: `fight.Rule` (Task 4), `input.ValidHotkey` (istniejące).
- Produces: `FightConfig` z metodami `withDefaults() FightConfig`, `validate(decisionRadius float64) error`.

- [ ] **Step 1: Napisz nieprzechodzące testy**

Utwórz `internal/brain/fightconfig_test.go`:

```go
package brain

import (
	"strings"
	"testing"

	"minimap-lab/internal/fight"
)

// calibratedFight is a whole, valid fight configuration, the way calibrated()
// is for CombatConfig: enough to pass validate() as-is.
func calibratedFight() FightConfig {
	return FightConfig{Enabled: true, AttackKey: "space"}
}

func TestFightConfigAcceptsCalibrated(t *testing.T) {
	if err := calibratedFight().withDefaults().validate(4); err != nil {
		t.Fatalf("poprawna konfiguracja odrzucona: %v", err)
	}
}

func TestFightConfigDefaults(t *testing.T) {
	c := calibratedFight().withDefaults()
	if c.ConfirmMS != 150 || c.LeaveFightMS != 600 || c.TargetRetryMS != 600 ||
		c.TargetAttempts != 3 || c.TargetBackoffMS != 2000 || c.TargetStallMS != 15000 ||
		c.FightPauseMS != 10000 {
		t.Errorf("czasy domyślne nie zostały wypełnione: %+v", c)
	}
	if c.BlockMixedCrowd == nil || !*c.BlockMixedCrowd {
		t.Errorf("block_mixed_crowd musi domyślnie być true dla brakującego klucza, dostałem %+v", c.BlockMixedCrowd)
	}
}

func TestFightConfigBlockMixedCrowdFalseIsRespected(t *testing.T) {
	c := calibratedFight()
	f := false
	c.BlockMixedCrowd = &f
	c = c.withDefaults()
	if c.BlockMixedCrowd == nil || *c.BlockMixedCrowd {
		t.Error("jawne false nie może zostać zamienione na domyślne true")
	}
}

func TestFightConfigDisabledIsLegalWithoutAttackKey(t *testing.T) {
	if err := (FightConfig{}).withDefaults().validate(4); err != nil {
		t.Fatalf("wyłączona, pusta konfiguracja musi być dozwolona: %v", err)
	}
}

func TestFightConfigEnabledRequiresAttackKey(t *testing.T) {
	c := FightConfig{Enabled: true}
	err := c.withDefaults().validate(4)
	if err == nil || !strings.Contains(err.Error(), "attack_key") {
		t.Fatalf("enabled bez attack_key musi być odrzucone, dostałem: %v", err)
	}
}

func TestFightConfigRejections(t *testing.T) {
	tests := []struct {
		name string
		edit func(*FightConfig)
		want string
	}{
		{"confirm_ms poza zakresem", func(c *FightConfig) { c.ConfirmMS = 10000 }, "confirm_ms"},
		{"leave_fight_ms poza zakresem", func(c *FightConfig) { c.LeaveFightMS = 100 }, "leave_fight_ms"},
		{"target_retry_ms poza zakresem", func(c *FightConfig) { c.TargetRetryMS = 10 }, "target_retry_ms"},
		{"target_attempts poza zakresem", func(c *FightConfig) { c.TargetAttempts = 0 }, "target_attempts"},
		{"target_backoff_ms poza zakresem", func(c *FightConfig) { c.TargetBackoffMS = 100 }, "target_backoff_ms"},
		{"target_stall_ms poza zakresem", func(c *FightConfig) { c.TargetStallMS = 100 }, "target_stall_ms"},
		{"fight_pause_ms poza zakresem", func(c *FightConfig) { c.FightPauseMS = 100 }, "fight_pause_ms"},
		{"nieznany klawisz ataku", func(c *FightConfig) { c.AttackKey = "??" }, "attack_key"},
		{"za dużo reguł", func(c *FightConfig) {
			for i := 0; i < 9; i++ {
				c.Spells = append(c.Spells, fight.Rule{Hotkey: "f1", CooldownMS: 1000})
			}
		}, "reguł"},
		{"reguła: nieznany klawisz", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "??", CooldownMS: 1000}}
		}, "klawisz"},
		{"reguła: min_monsters poza zakresem", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "f2", CooldownMS: 1000, MinMonsters: 0}}
		}, "min_monsters"},
		{"reguła: promień większy niż promień decyzji", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "f2", CooldownMS: 1000, MinMonsters: 1, Radius: 9}}
		}, "promień"},
		{"reguła: cooldown poza zakresem", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "f2", CooldownMS: 50, MinMonsters: 1, Radius: 1}}
		}, "cooldown"},
		{"reguła: min_mana_pct poza zakresem", func(c *FightConfig) {
			c.Spells = []fight.Rule{{Hotkey: "f2", CooldownMS: 1000, MinMonsters: 1, Radius: 1, MinManaPct: 150}}
		}, "mana"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := calibratedFight()
			tt.edit(&c)
			err := c.withDefaults().validate(4)
			if err == nil {
				t.Fatal("konfiguracja przeszła, choć nie powinna")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("komunikat %q nie zawiera %q", err.Error(), tt.want)
			}
		})
	}
}

func TestFightConfigDisabledRulesAreStillValidated(t *testing.T) {
	c := calibratedFight()
	c.Spells = []fight.Rule{{Enabled: false, Hotkey: "??", CooldownMS: 1000, MinMonsters: 1, Radius: 1}}
	err := c.withDefaults().validate(4)
	if err == nil {
		t.Fatal("wyłączona reguła z niepoprawnym klawiszem musi i tak zostać odrzucona")
	}
}

func TestFightConfigRuleDefaults(t *testing.T) {
	c := calibratedFight()
	c.Spells = []fight.Rule{{Hotkey: "f2", MinMonsters: 1, Radius: 1}}
	c = c.withDefaults()
	if c.Spells[0].CooldownMS != 2000 {
		t.Errorf("domyślny cooldown reguły = %d, oczekiwano 2000", c.Spells[0].CooldownMS)
	}
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie kompilują się**

Run: `go test ./internal/brain/... -run TestFightConfig -v`
Expected: błąd kompilacji — `FightConfig` jeszcze nie istnieje.

- [ ] **Step 3: Napisz `FightConfig`**

Utwórz `internal/brain/fightconfig.go`:

```go
package brain

import (
	"fmt"

	"minimap-lab/internal/fight"
	"minimap-lab/internal/input"
)

// maxSpellRules mirrors internal/brain/healconfig.go's maxHealRules: eight
// lines is more than any real setup needs, and it keeps a malformed request
// from turning into an unbounded walk over rules on every frame.
const maxSpellRules = 8

// FightConfig is the whole targeting surface the panel edits.
type FightConfig struct {
	// Enabled is the "Atakuj" switch. The panel does not remember it across
	// a reload - it is a switch that makes the bot act, not a setting.
	Enabled   bool         `json:"enabled"`
	AttackKey string       `json:"attack_key"`
	Spells    []fight.Rule `json:"spells"`
	// BlockMixedCrowd is a *bool, not a bool: false is a legal, meaningful
	// choice (a spawn with an NPC permanently in frame), so withDefaults
	// must be able to tell "the panel sent false" from "this key was never
	// sent at all" - the only case that gets promoted to true.
	BlockMixedCrowd *bool `json:"block_mixed_crowd"`

	ConfirmMS       int `json:"confirm_ms"`
	LeaveFightMS    int `json:"leave_fight_ms"`
	TargetRetryMS   int `json:"target_retry_ms"`
	TargetAttempts  int `json:"target_attempts"`
	TargetBackoffMS int `json:"target_backoff_ms"`
	TargetStallMS   int `json:"target_stall_ms"`
	FightPauseMS    int `json:"fight_pause_ms"`
}

// withDefaults fills every field the panel may leave out, the same "zero
// means unset" rule combatconfig.go and healconfig.go already use - safe
// here because zero is not a legal value for any of these durations either.
func (c FightConfig) withDefaults() FightConfig {
	if c.ConfirmMS == 0 {
		c.ConfirmMS = 150
	}
	if c.LeaveFightMS == 0 {
		c.LeaveFightMS = 600
	}
	if c.TargetRetryMS == 0 {
		c.TargetRetryMS = 600
	}
	if c.TargetAttempts == 0 {
		c.TargetAttempts = 3
	}
	if c.TargetBackoffMS == 0 {
		c.TargetBackoffMS = 2000
	}
	if c.TargetStallMS == 0 {
		c.TargetStallMS = 15000
	}
	if c.FightPauseMS == 0 {
		c.FightPauseMS = 10000
	}
	if c.BlockMixedCrowd == nil {
		t := true
		c.BlockMixedCrowd = &t
	}
	rules := make([]fight.Rule, len(c.Spells))
	for i, r := range c.Spells {
		if r.CooldownMS == 0 {
			r.CooldownMS = 2000
		}
		if r.MinMonsters == 0 {
			r.MinMonsters = 1
		}
		if r.Radius == 0 {
			r.Radius = 1
		}
		rules[i] = r
	}
	c.Spells = rules
	return c
}

// validate checks every field, including disabled rules - a broken line that
// passes validation only because it is switched off would fail on the day it
// matters most. decisionRadius bounds a rule's own radius: a rule asking
// about tiles the vision layer never sent would be asking about nothing.
func (c FightConfig) validate(decisionRadius float64) error {
	if c.ConfirmMS < 50 || c.ConfirmMS > 1000 {
		return fmt.Errorf("confirm_ms musi mieścić się w zakresie 50–1000 ms")
	}
	if c.LeaveFightMS < 200 || c.LeaveFightMS > 5000 {
		return fmt.Errorf("leave_fight_ms musi mieścić się w zakresie 200–5000 ms")
	}
	if c.TargetRetryMS < 300 || c.TargetRetryMS > 3000 {
		return fmt.Errorf("target_retry_ms musi mieścić się w zakresie 300–3000 ms")
	}
	if c.TargetAttempts < 1 || c.TargetAttempts > 10 {
		return fmt.Errorf("target_attempts musi mieścić się w zakresie 1–10")
	}
	if c.TargetBackoffMS < 500 || c.TargetBackoffMS > 30000 {
		return fmt.Errorf("target_backoff_ms musi mieścić się w zakresie 500–30000 ms")
	}
	if c.TargetStallMS < 3000 || c.TargetStallMS > 120000 {
		return fmt.Errorf("target_stall_ms musi mieścić się w zakresie 3000–120000 ms")
	}
	if c.FightPauseMS < 1000 || c.FightPauseMS > 60000 {
		return fmt.Errorf("fight_pause_ms musi mieścić się w zakresie 1000–60000 ms")
	}
	if c.Enabled && c.AttackKey == "" {
		return fmt.Errorf("włączenie ataku wymaga pola attack_key")
	}
	if c.AttackKey != "" && !input.ValidHotkey(c.AttackKey) {
		return fmt.Errorf("nieznany klawisz attack_key %q", c.AttackKey)
	}
	if len(c.Spells) > maxSpellRules {
		return fmt.Errorf("reguł czarów może być najwyżej %d, podano %d", maxSpellRules, len(c.Spells))
	}
	for i, r := range c.Spells {
		n := i + 1
		if !input.ValidHotkey(r.Hotkey) {
			return fmt.Errorf("reguła %d: nieznany klawisz %q", n, r.Hotkey)
		}
		if r.MinMonsters < 1 || r.MinMonsters > 64 {
			return fmt.Errorf("reguła %d: min_monsters musi mieścić się w zakresie 1–64", n)
		}
		if r.Radius < 0.5 || r.Radius > decisionRadius {
			return fmt.Errorf("reguła %d: promień musi mieścić się w zakresie 0,5–%.1f (promień decyzji)", n, decisionRadius)
		}
		if r.CooldownMS < 100 || r.CooldownMS > 60000 {
			return fmt.Errorf("reguła %d: cooldown musi mieścić się w zakresie 100–60000 ms", n)
		}
		if r.MinManaPct < 0 || r.MinManaPct > 99 {
			return fmt.Errorf("reguła %d: minimalna mana musi mieścić się w zakresie 0–99%%", n)
		}
	}
	return nil
}
```

- [ ] **Step 4: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/brain/... -run TestFightConfig -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd minimap-lab
git add internal/brain/fightconfig.go internal/brain/fightconfig_test.go
git commit -m "Dodaj konfigurację walki z walidacją"
```

---

### Task 7: Wpnij krok walki w pętlę mózgu

Najbardziej rozległe zadanie: `Controls` rośnie o dwie metody, `Config`/`Loop`/`State` rosną o pola walki, nowy plik `internal/brain/fight.go` liczy decyzję na klatkę, `handleFrame` i `follow()` dostają wywołania w odpowiedniej kolejności.

**Files:**
- Modify: `internal/brain/loop.go` (`Controls`, `Config`, `Loop`, `NewLoop`, `SetConfig`, `handleFrame`, `follow`)
- Modify: `internal/brain/state.go` (`State`, nowy `FightState`)
- Modify: `internal/brain/loop_test.go` (`fakeControls` o `casts`/`cancels`)
- Create: `internal/brain/fight.go`
- Create: `internal/brain/fight_test.go`

**Interfaces:**
- Consumes: `fight.Activity/Observation/Options/Transition` (Task 2), `fight.Targeter/TargetInput/TargetDecision` (Task 3), `fight.Engine/Crowd/Decision` (Task 4), `Driver.Cast/CancelTarget` (Task 5), `FightConfig` (Task 6).
- Produces: `Controls` rośnie o `Cast`, `CancelTarget`; `State.Fight FightState`; `Config.Fight FightConfig`.

- [ ] **Step 1: Rozszerz `fakeControls` o `casts` i `cancels`**

W `internal/brain/loop_test.go`, w strukturze `fakeControls`, dołóż pola:

```go
	casts   []string
	cancels int
```

Zaraz po metodzie `healKeys()`, dołóż:

```go
func (c *fakeControls) Cast(key string, _ time.Duration) input.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.casts = append(c.casts, key)
	return c.resultLocked(key)
}

func (c *fakeControls) castKeys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.casts...)
}

func (c *fakeControls) CancelTarget(_ time.Duration) input.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancels++
	return c.resultLocked("escape")
}

func (c *fakeControls) cancelCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cancels
}
```

- [ ] **Step 2: Napisz nieprzechodzące testy pętli**

Utwórz `internal/brain/fight_test.go`:

```go
package brain

import (
	"image"
	"image/color"
	"testing"
	"time"

	"minimap-lab/internal/fight"
	"minimap-lab/internal/frame"
	"minimap-lab/internal/heal"
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

func TestFightEntryDropsPendingStepWithoutBlockLearning(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) {
		fightConfig(c)
		c.Follow, c.Walk = true, true
	})
	h.at(1000, 1000)
	h.tick(t) // ustal pozycję i wystartuj krok w locie po trasie
	enterFight(t, h)
	if h.loop.executor.State().AwaitingEmit {
		t.Error("wejście w walkę musi porzucić krok w locie, nie zostawiać go w oczekiwaniu")
	}
}

func TestFightPendingEscapeBlocksStepAndFiresNextFrame(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	enterFight(t, h)
	h.ctrl.setStatus("refused")
	c := visionCalibration()
	empty := framedBattle(c, 0, false)
	empty = filled(c.Battle.W, c.Battle.H, color.NRGBA{R: 60, G: 60, B: 60, A: 255}) // brak wierszy
	h.clock.advance(700 * time.Millisecond)
	h.submit(t, h.visionFrame(t, region{frame.RegionBattle, empty}))
	if h.ctrl.cancelCount() == 0 {
		t.Fatal("wyjście z walki musi spróbować Escape nawet gdy sterownik go odmawia")
	}
	h.ctrl.setStatus("")
	h.clock.advance(100 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionBattle, empty}))
	if s.Fight.EscapeDue {
		t.Fatal("po potwierdzonej emisji Escape escape_due musi zgasnąć")
	}
	if h.ctrl.cancelCount() < 2 {
		t.Fatal("Escape musi być akcją oczekującą - kolejna klatka próbuje ponownie")
	}
}

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
	hpLow := barAt(frame.RegionHP, 0.1)
	h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, battle}, hpLow))
	h.clock.advance(200 * time.Millisecond)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))},
		region{frame.RegionBattle, battle}, hpLow))
	if len(h.ctrl.healKeys()) != 1 {
		t.Fatalf("leczenie powinno polecieć raz, dostałem %v", h.ctrl.healKeys())
	}
	if len(h.ctrl.castKeys()) != 0 {
		t.Fatalf("w tej samej klatce co leczenie żaden klawisz walki nie może polecieć, dostałem %v", h.ctrl.castKeys())
	}
	_ = s
}

func TestFightDisablingAttackWhileFightingSendsEscape(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	enterFight(t, h)
	h.config(t, func(c *Config) { fightConfig(c); c.Fight.Enabled = false })
	if h.ctrl.cancelCount() == 0 {
		t.Fatal("wyłączenie ataku w trakcie walki musi zlecić Escape")
	}
}

func TestFightDisarmingDoesNotSendEscape(t *testing.T) {
	h := newHarness(t)
	h.config(t, fightConfig)
	h.at(1000, 1000)
	enterFight(t, h)
	h.ctrl.Disarm("test")
	h.clock.advance(700 * time.Millisecond)
	c := visionCalibration()
	empty := filled(c.Battle.W, c.Battle.H, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
	h.submit(t, h.visionFrame(t, region{frame.RegionBattle, empty}))
	if h.ctrl.cancelCount() != 0 {
		t.Error("rozbrojony sterownik nie ma jak wysłać Escape - nie wolno nawet próbować liczyć tego jako escape_due")
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
```

- [ ] **Step 3: Uruchom testy i sprawdź, że nie kompilują się**

Run: `go test ./internal/brain/... -run TestFight -v`
Expected: błąd kompilacji — `Config.Fight`, `State.Fight`, `Controls.Cast/CancelTarget` jeszcze nie istnieją.

- [ ] **Step 4: Dodaj `Cast`/`CancelTarget` do `Controls` i `Fight` do `Config`**

W `internal/brain/loop.go`, w interfejsie `Controls`, dołóż po `Heal`:

```go
	Cast(key string, observationAge time.Duration) input.Result
	CancelTarget(observationAge time.Duration) input.Result
```

W strukturze `Config`, dołóż po `Heal HealConfig`:

```go
	Fight FightConfig `json:"fight"`
```

W `Config.validate()`, zaraz po `if err := c.Heal.validate(); err != nil { return err }`, dołóż:

```go
	if err := c.Fight.withDefaults().validate(c.Combat.withDefaults().DecisionRadius); err != nil {
		return err
	}
```

- [ ] **Step 5: Dodaj pola walki do `Loop` i zainicjuj je w `NewLoop`**

W strukturze `Loop`, dołóż po polach leczenia (`healer`, `healState`, ...):

```go
	activity   fight.Activity
	targeter   fight.Targeter
	engine     *fight.Engine
	battleRead bool

	fightState        FightState
	fightKeyLastFrame bool
	escapeDue         bool
	pauseUntil        time.Time
	hasPause          bool
	lastSpellAt       time.Time
	hasLastSpell      bool
```

Dodaj import `"minimap-lab/internal/fight"` do `internal/brain/loop.go`.

W `NewLoop`, w literale `&Loop{...}`, dołóż `engine: fight.NewEngine(),` obok `healer: heal.NewEngine(),`.

- [ ] **Step 6: Wepnij `FightConfig` w `SetConfig`**

W `SetConfig`, zaraz po bloku obsługującym `l.healer.SetRules(...)` / wyłączenie leczenia (przed `l.cfg.Combat = c.Combat.withDefaults()` albo zaraz po nim — kolejność między Combat a Fight nie ma znaczenia, oba operują na już zdekodowanym `c`), dołóż:

```go
		l.cfg.Fight = c.Fight.withDefaults()
		l.engine.SetRules(l.cfg.Fight.Spells)
		if !l.cfg.Fight.Enabled {
			// Same reasoning as heal's own cleanup a few lines up: fightStep
			// runs once per frame, so without this a disabled attack would
			// leave "fighting" published until the next frame arrives - or
			// forever, if the camera stopped. escapeDue survives on purpose:
			// turning attack off mid-fight still has to cancel the target.
			if l.activity.State() == fight.Fighting {
				l.escapeDue = true
			}
			l.activity.Force(fight.Travelling)
			l.targeter.Reset()
			l.fightState = FightState{Enabled: false, Activity: fight.Travelling.String(), EscapeDue: l.escapeDue}
		}
```

- [ ] **Step 7: Napisz `internal/brain/fight.go`**

Utwórz `internal/brain/fight.go`:

```go
// This file owns the loop's combat step: activity, targeting and spell
// rules. It runs once per fresh frame, right after healing and before the
// minimap search gate - the client's camera is centred on the character, so
// combat needs neither a world position nor the minimap region, and the map
// sieve's own position dependency is handled the same way finishVision
// handles it: fall back to whatever position the previous frame left.
package brain

import (
	"time"

	"minimap-lab/internal/fight"
	"minimap-lab/internal/vision"
)

// fightOptions converts FightConfig's millisecond fields to Durations once
// per call, the way healStep's caller already does for MinGap-adjacent
// values - fight.Activity/Targeter/Engine take Options rather than seven
// separate int arguments.
func (l *Loop) fightOptions() fight.Options {
	c := l.cfg.Fight
	return fight.Options{
		Confirm:        time.Duration(c.ConfirmMS) * time.Millisecond,
		LeaveFight:     time.Duration(c.LeaveFightMS) * time.Millisecond,
		TargetRetry:    time.Duration(c.TargetRetryMS) * time.Millisecond,
		TargetBackoff:  time.Duration(c.TargetBackoffMS) * time.Millisecond,
		TargetStall:    time.Duration(c.TargetStallMS) * time.Millisecond,
		TargetAttempts: c.TargetAttempts,
	}
}

// fightCrowd recomputes creature distances from the raw detected bars,
// applying the same map sieve as finishVision. It does not reuse
// CombatState/VisionView's own counts because those are filled by
// finishVision, which runs on handleFrame's deferred tail - after fightStep,
// not before it - and VisionView's bar list is capped at maxVisionBars for
// its own diagnostic-payload reasons, a cap Crowd must not inherit.
func (l *Loop) fightCrowd() fight.Crowd {
	var distances []float64
	for _, b := range l.bars {
		dx, dy := l.visionGrid.Offset(b)
		if l.blockedTile(dx, dy) {
			continue
		}
		distances = append(distances, vision.Distance(dx, dy))
	}
	mixed := !l.combat.BattleTruncated && l.combat.BattleRows > 0 && len(distances) > l.combat.BattleRows
	return fight.Crowd{Distances: distances, Mixed: mixed}
}

// targetHP reads the health of the battle-list row currently carrying the
// attack frame, from the same per-row data the vision preview uses.
func (l *Loop) targetHP() float64 {
	if l.combat.TargetRow == nil {
		return 0
	}
	i := *l.combat.TargetRow
	if i < 0 || i >= len(l.view.Battle) {
		return 0
	}
	return l.view.Battle[i].HP
}

func (l *Loop) blockMixedCrowd() bool {
	if l.cfg.Fight.BlockMixedCrowd == nil {
		return true
	}
	return *l.cfg.Fight.BlockMixedCrowd
}

// fightStep evaluates activity, targeting and spell rules for this frame and
// presses at most one non-heal key. Priority within the frame: healing (run
// before this step) beats a pending Escape, which beats targeting, which
// beats a spell - fightKeyLastFrame preempts the walking step exactly the
// way healedLastFrame does.
func (l *Loop) fightStep(capturedAt time.Time) {
	l.fightKeyLastFrame = false
	l.fightState.Reason = ""
	l.fightState.Enabled = l.cfg.Fight.Enabled

	if !l.cfg.Fight.Enabled {
		if l.activity.State() == fight.Fighting {
			l.escapeDue = true
		}
		l.activity.Force(fight.Travelling)
		l.targeter.Reset()
		l.fightState = FightState{Enabled: false, Activity: fight.Travelling.String(), EscapeDue: l.escapeDue}
		return
	}
	if l.cfg.Combat.Battle.Empty() {
		l.fightState.Activity = l.activity.State().String()
		l.fightState.Reason = "brak kalibracji battle listy"
		return
	}

	cc := l.cfg.Combat
	crowd := l.fightCrowd()
	now := l.deps.Now()
	paused := l.hasPause && now.Before(l.pauseUntil)
	if l.hasPause && !paused {
		l.hasPause = false
	}
	opts := l.fightOptions()
	obs := fight.Observation{
		Enabled: true, InRange: crowd.Within(cc.DecisionRadius), BattleRead: l.battleRead,
		Rows: l.combat.BattleRows, HasTarget: l.combat.TargetRow != nil,
		Blocked: paused || l.escapeDue, CapturedAt: capturedAt,
	}
	switch l.activity.Observe(obs, opts) {
	case fight.Entered:
		// The in-flight step is abandoned without learning a blocked tile:
		// the key already left, but whether the character actually arrived
		// now depends on the client's chase and any creature in the way, not
		// on a wall - crediting that to the block-learning executor would be
		// exactly the "pending-step misattribution" the combat design spec
		// warned about.
		l.executor.DropPending()
	case fight.Left:
		l.escapeDue = true
		l.targeter.Reset()
	}
	l.fightState.Activity = l.activity.State().String()
	l.fightState.EscapeDue = l.escapeDue

	if l.healedLastFrame {
		l.fightState.Reason = "leczenie zajęło klatkę"
		return
	}

	if l.escapeDue {
		res := l.deps.Driver.CancelTarget(now.Sub(capturedAt))
		l.fightState.Reason = res.Reason
		l.fightKeyLastFrame = true
		if res.Status == "emitted" {
			l.escapeDue, l.fightState.EscapeDue = false, false
		}
		return
	}

	if l.activity.State() != fight.Fighting {
		return
	}

	age := now.Sub(capturedAt)
	td := l.targeter.Decide(fight.TargetInput{
		HasTarget: obs.HasTarget, TargetHP: l.targetHP(), Rows: obs.Rows, CapturedAt: capturedAt,
	}, opts)
	l.fightState.TargetAttempts = l.targeter.Attempts()
	if td.Stall {
		l.activity.Force(fight.Travelling)
		l.escapeDue = true
		l.pauseUntil, l.hasPause = now.Add(time.Duration(l.cfg.Fight.FightPauseMS)*time.Millisecond), true
		l.targeter.Reset()
		l.fightState.Activity = fight.Travelling.String()
		l.fightState.Reason = td.Reason
		return
	}
	if td.Tap {
		res := l.deps.Driver.Cast(l.cfg.Fight.AttackKey, age)
		l.fightState.Reason = res.Reason
		if res.Status == "emitted" {
			l.targeter.Tapped(now)
			l.fightState.TargetAttempts = l.targeter.Attempts()
			l.fightKeyLastFrame = true
		}
		return
	}
	if td.Reason != "" {
		l.fightState.Reason = td.Reason
	}

	d := l.engine.Decide(crowd, l.manaReading, obs.HasTarget, l.blockMixedCrowd(), capturedAt, opts.Confirm)
	if !d.Fire {
		if l.fightState.Reason == "" {
			l.fightState.Reason = d.Reason
		}
		return
	}
	res := l.deps.Driver.Cast(d.Hotkey, age)
	l.fightState.Reason = res.Reason
	if res.Status == "emitted" {
		l.engine.Emitted(d.Hotkey, now)
		l.fightKeyLastFrame = true
		l.fightState.LastSpell = d.Hotkey
		l.lastSpellAt, l.hasLastSpell = now, true
	}
}

// fightSnapshot fills in the ages of the events currently in flight at
// publish time; the rest of FightState is written as it happens - the same
// split healSnapshot uses.
func (l *Loop) fightSnapshot() FightState {
	out := l.fightState
	now := l.deps.Now()
	if l.hasPause {
		left := int(l.pauseUntil.Sub(now).Milliseconds())
		if left < 0 {
			left = 0
		}
		out.PauseMSLeft = &left
	}
	if until, ok := l.targeter.BackoffUntil(); ok {
		left := int(until.Sub(now).Milliseconds())
		if left < 0 {
			left = 0
		}
		out.BackoffMSLeft = &left
	}
	if l.hasLastSpell {
		age := int(now.Sub(l.lastSpellAt).Milliseconds())
		if age < 0 {
			age = 0
		}
		out.LastSpellAgeMS = &age
	}
	return out
}
```

- [ ] **Step 8: Wywołaj `fightStep` z `handleFrame`, zapisuj `battleRead`**

W `internal/brain/vision.go`, w `observeVision`, na samym początku (obok istniejącego resetu `l.combat, l.view, l.bars = ...`), dołóż `l.battleRead = false`. W bloku `if im, ok := f.Image(frame.RegionBattle); ok {`, jako pierwszą linię wewnątrz `{`, dołóż `l.battleRead = true`.

W `internal/brain/loop.go`, w `handleFrame`, zaraz po `l.healStep(capturedAt)` a przed `if l.searchStopped {`, dołóż:

```go
	l.fightStep(capturedAt)
```

- [ ] **Step 9: Zamroź `follow()` w `Fighting`, wywłaszcz krok fightKeyLastFrame**

W `follow()`, zaraz po `if !l.cfg.Follow || len(l.recorder.Waypoints()) == 0 { l.routeNext = ""; return }`, dołóż:

```go
	if l.activity.State() == fight.Fighting {
		l.routeNext = "Walka."
		if l.cfg.Walk && l.deps.Driver != nil && l.deps.Driver.Armed() {
			l.executor.Observe(&pos, capturedAt, now)
		}
		return
	}
```

Znajdź istniejący blok:

```go
	if l.healedLastFrame {
		return
	}
```

i zamień go na:

```go
	if l.healedLastFrame || l.fightKeyLastFrame {
		return
	}
```

(zaktualizuj też komentarz nad tym blokiem, wspominając oba przeznaczenia, nie tylko leczenie).

- [ ] **Step 10: Dodaj `FightState` i wepnij `fightSnapshot()` w `publish()`**

W `internal/brain/state.go`, dołóż:

```go
type FightState struct {
	Enabled        bool   `json:"enabled"`
	Activity       string `json:"activity"`
	EscapeDue      bool   `json:"escape_due"`
	TargetAttempts int    `json:"target_attempts"`
	BackoffMSLeft  *int   `json:"backoff_ms_left"`
	PauseMSLeft    *int   `json:"pause_ms_left"`
	LastSpell      string `json:"last_spell,omitempty"`
	LastSpellAgeMS *int   `json:"last_spell_age_ms"`
	Reason         string `json:"reason,omitempty"`
}
```

W strukturze `State`, dołóż po `Heal HealState`:

```go
	Fight FightState `json:"fight"`
```

W `publish()`, w literale `&State{...}`, dołóż po `Heal: l.healSnapshot(),`:

```go
		Fight: l.fightSnapshot(),
```

- [ ] **Step 11: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/brain/... -race -v`
Expected: PASS, łącznie z nowymi testami z `fight_test.go` i wszystkimi istniejącymi (leczenie, wykonawca, trasa).

- [ ] **Step 12: Uruchom całość**

Run: `go test ./... -race`
Expected: PASS wszystkie pakiety.

- [ ] **Step 13: Commit**

```bash
cd minimap-lab
git add internal/brain/loop.go internal/brain/loop_test.go internal/brain/state.go internal/brain/vision.go internal/brain/fight.go internal/brain/fight_test.go
git commit -m "Wepnij walkę w pętlę mózgu"
```

---
