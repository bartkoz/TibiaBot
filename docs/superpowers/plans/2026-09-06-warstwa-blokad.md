# Warstwa nauczonych blokad — plan implementacji

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bot pamięta kratki, po których nie da się chodzić mimo tego, co mówią dane mapy, omija je przy liczeniu trasy i pokazuje tę wiedzę na żywo w panelu.

**Architecture:** Magazyn blokad żyje w serwerze Go jako rzadka nakładka obok niezmiennej siatki kosztów. Panel zgłasza obserwacje z nieudanych kroków, serwer decyduje, czy uzasadniają blokadę. A* czyta koszt bazowy plus nakładkę przez `PathGrid`; blokada świeża podnosi koszt o 500, trwała czyni kratkę nieprzechodnią. Osobny endpoint zwraca okno przechodności wokół postaci dla podglądu w canvasie.

**Tech Stack:** Go 1.24.2 (stdlib + `github.com/ebitengine/purego` wyłącznie w emiterze macOS), panel w czystym JS bez frameworka, testy `go test ./...` i `node --test`.

**Spec:** `docs/superpowers/specs/2026-09-06-warstwa-blokad-design.md`

## Global Constraints

- Wszystkie pliki mieszkają w module `minimap-lab` (katalog `/Users/Bartek/TibiaBot/minimap-lab`); polecenia uruchamiaj z tego katalogu.
- CGO pozostaje wyłączone. Żadnych nowych zależności zewnętrznych — tylko stdlib.
- Komentarze w kodzie po angielsku, teksty widoczne dla użytkownika i dokumentacja po polsku. Tak jest w całym module.
- Komentarz tłumaczy **dlaczego**, nie **co**. Nie komentuj oczywistości.
- Testy uruchamiane lokalnie: `go test ./...` oraz `node --test <plik>.cjs`. Moduł nie ma Docker Compose.
- Bazowa tablica `CostGrid.pix` jest współdzielona z cache'em piętra i **nigdy** nie wolno jej modyfikować.
- Stałe polityki (jedno miejsce, `blocks.go`): `tempTTL = 60s`, `edgeTTL = 20s`, `forgetAfter = 24h`, `tempPenalty = 500`, `minStillFrames = 3`, `maxFrameAgeMS = 300`.
- Każde zadanie kończy się commitem. Wiadomość commita po polsku, w trybie oznajmującym, z linią `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

---

### Task 1: Magazyn blokad — TTL, epizody, awans

**Files:**
- Create: `blocks.go`
- Create: `blocks_test.go`

**Interfaces:**
- Consumes: `Position` z `matcher.go` (pola `X`, `Y`, `Z int`).
- Produces:
  - `type Kind uint8` z `KindNone`, `KindTemp`, `KindPerm`
  - `type Observation struct { From, To Position; Outcome string; StillFrames int; LastFrameAgeMS int }`
  - `type Decision struct { Result, Reason string }` — `Result` ∈ `ignored|temp|promoted|cleared`
  - `type Overlay struct` z metodami `Tile(x, y int) Kind` i `Edge(fx, fy, tx, ty int) bool`
  - `func NewBlockStore(now func() time.Time) *BlockStore`
  - `func (s *BlockStore) Observe(obs Observation) Decision`
  - `func (s *BlockStore) Snapshot(area image.Rectangle, z int) *Overlay`
  - `func (s *BlockStore) Clear(p Position) bool`
  - `func (s *BlockStore) Revision() uint64`

- [ ] **Step 1: Write the failing tests**

Utwórz `blocks_test.go`:

```go
package main

import (
	"image"
	"testing"
	"time"
)

// clock is a hand-cranked time source: TTL, promotion and forgetting are all
// time-driven, and a test that actually waited 60 seconds would be useless.
type clock struct{ at time.Time }

func (c *clock) now() time.Time      { return c.at }
func (c *clock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newTestStore() (*BlockStore, *clock) {
	c := &clock{at: time.Date(2026, 9, 6, 20, 0, 0, 0, time.UTC)}
	return NewBlockStore(c.now), c
}

// bump is a qualified straight step that failed: the evidence the panel is
// required to supply before anything is learned.
func bump(from, to Position) Observation {
	return Observation{From: from, To: to, Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 140}
}

func TestFirstBumpMakesTemporaryBlock(t *testing.T) {
	s, _ := newTestStore()
	d := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if d.Result != "temp" {
		t.Fatalf("result %q (%s), want temp", d.Result, d.Reason)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindTemp {
		t.Fatalf("tile kind %v, want KindTemp", got)
	}
}

func TestTemporaryBlockExpires(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindNone {
		t.Fatalf("tile kind %v after TTL, want KindNone", got)
	}
}

func TestRetryWithinTTLIsOneEpisode(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(2 * time.Second)
	d := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if d.Result != "temp" {
		t.Fatalf("result %q, want temp - a repeat inside the TTL is the same episode", d.Result)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindTemp {
		t.Fatalf("tile kind %v, want KindTemp", got)
	}
}

func TestSecondEpisodeAfterExpiryPromotes(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	d := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if d.Result != "promoted" {
		t.Fatalf("result %q (%s), want promoted", d.Result, d.Reason)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindPerm {
		t.Fatalf("tile kind %v, want KindPerm", got)
	}
}

func TestPermanentBlockDoesNotExpire(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(48 * time.Hour)
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindPerm {
		t.Fatalf("tile kind %v after two days, want KindPerm", got)
	}
}

func TestForgottenRecordStartsOver(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(25 * time.Hour) // past forgetAfter, so the episode count is gone
	d := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if d.Result != "temp" {
		t.Fatalf("result %q, want temp - a forgotten record cannot promote on its first bump", d.Result)
	}
}

func TestDiagonalBumpBlocksEdgeNotTile(t *testing.T) {
	s, _ := newTestStore()
	d := s.Observe(bump(Position{100, 100, 7}, Position{101, 99, 7}))
	if d.Result != "temp" {
		t.Fatalf("result %q (%s), want temp", d.Result, d.Reason)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(101, 99); got != KindNone {
		t.Fatalf("diagonal bump marked the tile %v; it must only block the edge", got)
	}
	if !o.Edge(100, 100, 101, 99) {
		t.Fatal("diagonal bump did not block the edge it failed on")
	}
}

func TestDiagonalEdgeNeverPromotes(t *testing.T) {
	s, c := newTestStore()
	for i := 0; i < 5; i++ {
		s.Observe(bump(Position{100, 100, 7}, Position{101, 99, 7}))
		c.advance(21 * time.Second)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(101, 99); got != KindNone {
		t.Fatalf("tile kind %v; a diagonal must never produce a tile block", got)
	}
}

func TestEnteredClearsPermanentBlock(t *testing.T) {
	s, c := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	d := s.Observe(Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
		Outcome: "entered", StillFrames: 1, LastFrameAgeMS: 50})
	if d.Result != "cleared" {
		t.Fatalf("result %q (%s), want cleared", d.Result, d.Reason)
	}
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if got := o.Tile(100, 99); got != KindNone {
		t.Fatalf("tile kind %v after entering it, want KindNone", got)
	}
	// The episode count must go too: proof of passage invalidates the whole
	// hypothesis, not just the current block. Otherwise the next single bump
	// would promote straight to permanent.
	c.advance(time.Second)
	if again := s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7})); again.Result != "temp" {
		t.Fatalf("result %q after clearing, want temp", again.Result)
	}
}

func TestUnqualifiedObservationsAreIgnored(t *testing.T) {
	cases := []struct {
		name string
		obs  Observation
	}{
		{"nie sąsiednie kratki", Observation{From: Position{100, 100, 7}, To: Position{100, 97, 7},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}},
		{"ta sama kratka", Observation{From: Position{100, 100, 7}, To: Position{100, 100, 7},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}},
		{"różne piętra", Observation{From: Position{100, 100, 7}, To: Position{100, 99, 6},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}},
		{"za mało klatek", Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
			Outcome: "no_motion", StillFrames: 2, LastFrameAgeMS: 100}},
		{"za stara klatka", Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
			Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 400}},
		{"nieznany wynik", Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
			Outcome: "transport_error", StillFrames: 3, LastFrameAgeMS: 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestStore()
			d := s.Observe(tc.obs)
			if d.Result != "ignored" {
				t.Fatalf("result %q, want ignored", d.Result)
			}
			if d.Reason == "" {
				t.Fatal("an ignored observation must say why - otherwise the panel cannot tell it from a lost request")
			}
			if got := s.Snapshot(image.Rect(90, 90, 110, 110), 7).Tile(100, 99); got != KindNone {
				t.Fatalf("tile kind %v; nothing may be learned from an unqualified observation", got)
			}
		})
	}
}

func TestSnapshotIsLimitedToAreaAndFloor(t *testing.T) {
	s, _ := newTestStore()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	s.Observe(bump(Position{500, 500, 7}, Position{500, 499, 7}))
	s.Observe(bump(Position{100, 100, 8}, Position{100, 99, 8}))
	o := s.Snapshot(image.Rect(90, 90, 110, 110), 7)
	if o.Tile(100, 99) != KindTemp {
		t.Fatal("tile inside the area is missing from the snapshot")
	}
	if o.Tile(500, 499) != KindNone {
		t.Fatal("tile outside the area leaked into the snapshot")
	}
}

func TestRevisionMovesOnlyOnChange(t *testing.T) {
	s, _ := newTestStore()
	start := s.Revision()
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	afterBump := s.Revision()
	if afterBump == start {
		t.Fatal("revision did not move after a learned block")
	}
	s.Observe(Observation{From: Position{100, 100, 7}, To: Position{100, 97, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})
	if s.Revision() != afterBump {
		t.Fatal("revision moved on an ignored observation; the panel would redraw for nothing")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestFirstBump|TestTemporary|TestRetry|TestSecond|TestPermanent|TestForgotten|TestDiagonal|TestEntered|TestUnqualified|TestSnapshot|TestRevision' ./...`
Expected: FAIL — `undefined: BlockStore`, `undefined: Observation`, `undefined: KindTemp` itd.

- [ ] **Step 3: Write the implementation**

Utwórz `blocks.go`:

```go
package main

import (
	"fmt"
	"image"
	"sync"
	"time"
)

// Policy constants for learned blockages. They live together because they are
// one policy, not six unrelated numbers.
const (
	// tempTTL is how long a freshly learned block steers routes away. It is
	// deliberately short: the cause may well have been a player who has since
	// walked off.
	tempTTL = 60 * time.Second
	// edgeTTL is shorter still. An edge block only stops the bot retrying the
	// same corner in a loop; it says nothing about the terrain.
	edgeTTL = 20 * time.Second
	// forgetAfter is how long the record itself survives. It outlives the
	// block on purpose: without a memory of the first episode, a second one
	// could never be recognised as second and nothing would ever be promoted.
	forgetAfter = 24 * time.Hour
	// tempPenalty is added to the tile cost while a temporary block holds. A
	// penalty rather than a wall, so a player standing in a one-tile doorway
	// makes the bot wait and retry instead of declaring the route impossible.
	tempPenalty = 500
	// minStillFrames and maxFrameAgeMS are the evidence a bump must carry. One
	// stale frame is not proof that the character stayed put.
	minStillFrames = 3
	maxFrameAgeMS  = 300
)

type Kind uint8

const (
	KindNone Kind = iota
	KindTemp
	KindPerm
)

// Observation is what the panel saw after a single step attempt. Outcome is
// "no_motion" (the character stayed on From) or "entered" (it reached To after
// all, which revokes whatever was learned about that tile).
type Observation struct {
	From           Position `json:"from"`
	To             Position `json:"to"`
	Outcome        string   `json:"outcome"`
	StillFrames    int      `json:"still_frames"`
	LastFrameAgeMS int      `json:"last_frame_age_ms"`
}

// Decision is the server's answer. Result is ignored, temp, promoted or
// cleared; Reason always explains it, so a refused observation never looks
// like a dropped request in the panel.
type Decision struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
}

type Blockage struct {
	Kind     Kind
	Episodes int
	First    time.Time
	Expires  time.Time // zero for KindPerm: a permanent block does not expire
	Forget   time.Time
}

type tileKey [3]int // x, y, z
type edgeKey [5]int // fromX, fromY, toX, toY, z

type BlockStore struct {
	mu    sync.RWMutex
	now   func() time.Time
	tiles map[tileKey]*Blockage
	edges map[edgeKey]*Blockage
	rev   uint64
}

// Overlay is an immutable view of the blockages inside one area of one floor,
// taken before a search starts. A* assumes the cost of a closed vertex never
// changes, so the graph must not shift under it mid-search.
type Overlay struct {
	tiles map[[2]int]Kind
	edges map[[4]int]bool
}

func (o *Overlay) Tile(x, y int) Kind {
	if o == nil {
		return KindNone
	}
	return o.tiles[[2]int{x, y}]
}

func (o *Overlay) Edge(fx, fy, tx, ty int) bool {
	if o == nil {
		return false
	}
	return o.edges[[4]int{fx, fy, tx, ty}]
}

func NewBlockStore(now func() time.Time) *BlockStore {
	return &BlockStore{now: now, tiles: map[tileKey]*Blockage{}, edges: map[edgeKey]*Blockage{}}
}

func (s *BlockStore) Revision() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rev
}

func (s *BlockStore) Observe(obs Observation) Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)

	switch obs.Outcome {
	case "entered":
		if s.clearLocked(tileKey{obs.To.X, obs.To.Y, obs.To.Z}) {
			s.rev++
			return Decision{Result: "cleared", Reason: "Postać weszła na kratkę; blokada usunięta."}
		}
		return Decision{Result: "ignored", Reason: "Kratka nie była zablokowana."}
	case "no_motion":
	default:
		return Decision{Result: "ignored", Reason: fmt.Sprintf("Nieznany wynik próby: %s", obs.Outcome)}
	}

	if obs.From.Z != obs.To.Z {
		return Decision{Result: "ignored", Reason: "Krok między piętrami nie jest dowodem przeszkody."}
	}
	dx, dy := abs(obs.To.X-obs.From.X), abs(obs.To.Y-obs.From.Y)
	if dx > 1 || dy > 1 || (dx == 0 && dy == 0) {
		return Decision{Result: "ignored", Reason: "Kratki nie sąsiadują ze sobą."}
	}
	if obs.StillFrames < minStillFrames {
		return Decision{Result: "ignored",
			Reason: fmt.Sprintf("Za mało klatek bez ruchu: %d, wymagane %d.", obs.StillFrames, minStillFrames)}
	}
	if obs.LastFrameAgeMS > maxFrameAgeMS {
		return Decision{Result: "ignored",
			Reason: fmt.Sprintf("Ostatnia klatka starsza niż %d ms.", maxFrameAgeMS)}
	}

	// A diagonal step also fails when both orthogonal tiles at the corner are
	// blocked, and then the target itself is often perfectly walkable. Learning
	// tiles from diagonals is how a bot slowly carves working rooms out of its
	// own map, so a diagonal only ever blocks the edge it failed on.
	if dx == 1 && dy == 1 {
		k := edgeKey{obs.From.X, obs.From.Y, obs.To.X, obs.To.Y, obs.From.Z}
		s.edges[k] = &Blockage{Kind: KindTemp, Episodes: 1, First: now,
			Expires: now.Add(edgeTTL), Forget: now.Add(forgetAfter)}
		s.rev++
		return Decision{Result: "temp", Reason: "Skos nieprzejezdny; zablokowano samo przejście."}
	}

	k := tileKey{obs.To.X, obs.To.Y, obs.To.Z}
	b := s.tiles[k]
	if b == nil {
		s.tiles[k] = &Blockage{Kind: KindTemp, Episodes: 1, First: now,
			Expires: now.Add(tempTTL), Forget: now.Add(forgetAfter)}
		s.rev++
		return Decision{Result: "temp", Reason: "Pierwszy epizod; blokada tymczasowa."}
	}
	if b.Kind == KindPerm {
		b.Forget = now.Add(forgetAfter)
		return Decision{Result: "promoted", Reason: "Kratka była już blokadą trwałą."}
	}
	// Still inside the previous block's lifetime: this is the same episode
	// seen again (a retry, a recomputed route, a resent report), not a second
	// independent one.
	if now.Before(b.Expires) {
		b.Forget = now.Add(forgetAfter)
		return Decision{Result: "temp", Reason: "Ten sam epizod; blokada tymczasowa przedłużona."}
	}
	b.Episodes++
	b.Forget = now.Add(forgetAfter)
	if b.Episodes >= 2 {
		b.Kind, b.Expires = KindPerm, time.Time{}
		s.rev++
		return Decision{Result: "promoted", Reason: "Drugi niezależny epizod; blokada trwała."}
	}
	b.Expires = now.Add(tempTTL)
	s.rev++
	return Decision{Result: "temp", Reason: "Kolejny epizod; blokada tymczasowa."}
}

// Clear removes whatever is known about a tile. Reported presence beats any
// learned hypothesis, so this also drops the episode count - otherwise the
// next single bump would promote straight to permanent.
func (s *BlockStore) Clear(p Position) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clearLocked(tileKey{p.X, p.Y, p.Z}) {
		s.rev++
		return true
	}
	return false
}

func (s *BlockStore) clearLocked(k tileKey) bool {
	if _, ok := s.tiles[k]; !ok {
		return false
	}
	delete(s.tiles, k)
	return true
}

func (s *BlockStore) Snapshot(area image.Rectangle, z int) *Overlay {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	o := &Overlay{tiles: map[[2]int]Kind{}, edges: map[[4]int]bool{}}
	for k, b := range s.tiles {
		// KindNone is an expired block whose record is still remembered for
		// episode counting; it must not steer any route.
		if b.Kind == KindNone || k[2] != z || !image.Pt(k[0], k[1]).In(area) {
			continue
		}
		o.tiles[[2]int{k[0], k[1]}] = b.Kind
	}
	for k, b := range s.edges {
		if k[4] != z || !image.Pt(k[0], k[1]).In(area) {
			continue
		}
		if b.Kind != KindNone {
			o.edges[[4]int{k[0], k[1], k[2], k[3]}] = true
		}
	}
	return o
}

// sweepLocked drops expired blocks and forgotten records. Expiry and
// forgetting are separate: a block stops steering routes long before its
// record is gone, and the record is what makes a second episode countable.
func (s *BlockStore) sweepLocked(now time.Time) {
	for k, b := range s.tiles {
		if now.After(b.Forget) {
			delete(s.tiles, k)
			s.rev++
			continue
		}
		if b.Kind == KindTemp && !now.Before(b.Expires) {
			b.Kind = KindNone
			s.rev++
		}
	}
	for k, b := range s.edges {
		if now.After(b.Forget) || !now.Before(b.Expires) {
			delete(s.edges, k)
			s.rev++
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS — wszystkie, również dotychczasowe.

- [ ] **Step 5: Run the race detector**

Run: `go test -race ./...`
Expected: PASS, bez ostrzeżeń o wyścigu.

- [ ] **Step 6: Commit**

```bash
git add blocks.go blocks_test.go
git commit -m "Magazyn nauczonych blokad z TTL, epizodami i awansem

Kratka, na którą nie udało się wejść, dostaje blokadę tymczasową na 60 s.
Drugi epizod - liczony dopiero po wygaśnięciu pierwszego, nie z retry tego
samego kroku - awansuje ją na trwałą. Rekord żyje dłużej niż blokada, bo
bez pamięci pierwszego epizodu drugi nigdy nie byłby drugim.

Nieudany skos blokuje samo przejście, nigdy kratkę: skos zawodzi także na
zamkniętym rogu, gdzie kratka docelowa bywa pusta, a nauka z takich prób
stopniowo wycina z mapy przechodnie pokoje.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Trwałość — `blocks.json`

**Files:**
- Modify: `blocks.go`
- Modify: `blocks_test.go`

**Interfaces:**
- Consumes: `BlockStore`, `Blockage`, `KindPerm` z Task 1.
- Produces:
  - `func (s *BlockStore) SetPath(path string)`
  - `func (s *BlockStore) Load() error`
  - `func (s *BlockStore) Save() error`
  - `func (s *BlockStore) Flush()` — zapis, jeśli coś się zmieniło; wołany po `Observe` z endpointu

- [ ] **Step 1: Write the failing tests**

Dopisz do `blocks_test.go`:

```go
func TestPermanentBlocksSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocks.json")

	s, c := newTestStore()
	s.SetPath(path)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	fresh, _ := newTestStore()
	fresh.SetPath(path)
	if err := fresh.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := fresh.Snapshot(image.Rect(90, 90, 110, 110), 7).Tile(100, 99); got != KindPerm {
		t.Fatalf("tile kind %v after reload, want KindPerm", got)
	}
}

func TestTemporaryBlocksAreNotWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocks.json")
	s, _ := newTestStore()
	s.SetPath(path)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	fresh, _ := newTestStore()
	fresh.SetPath(path)
	if err := fresh.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := fresh.Snapshot(image.Rect(90, 90, 110, 110), 7).Tile(100, 99); got != KindNone {
		t.Fatalf("tile kind %v; a temporary block is a guess and must not reach the disk", got)
	}
}

func TestClearedTileLeavesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocks.json")
	s, c := newTestStore()
	s.SetPath(path)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	s.Save()
	s.Clear(Position{100, 99, 7})
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	fresh, _ := newTestStore()
	fresh.SetPath(path)
	fresh.Load()
	if got := fresh.Snapshot(image.Rect(90, 90, 110, 110), 7).Tile(100, 99); got != KindNone {
		t.Fatalf("tile kind %v; a revoked block must disappear from disk too", got)
	}
}

func TestUnknownFileVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocks.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"tiles":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := newTestStore()
	s.SetPath(path)
	err := s.Load()
	if err == nil {
		t.Fatal("a file from a future version was accepted; the next Save would overwrite it")
	}
	if !strings.Contains(err.Error(), "wersj") {
		t.Fatalf("error %q does not name the version problem", err)
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	s, _ := newTestStore()
	s.SetPath(filepath.Join(t.TempDir(), "blocks.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("load of a missing file: %v - a first run has no file yet", err)
	}
}

func TestSaveLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	s, c := newTestStore()
	s.SetPath(filepath.Join(dir, "blocks.json"))
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	c.advance(61 * time.Second)
	s.Observe(bump(Position{100, 100, 7}, Position{100, 99, 7}))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d files in the directory, want exactly blocks.json", len(entries))
	}
}
```

Dopisz do importów w `blocks_test.go`: `"os"`, `"path/filepath"`, `"strings"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestPermanentBlocksSurvive|TestTemporaryBlocksAreNot|TestCleared|TestUnknownFileVersion|TestMissingFile|TestSaveLeaves' ./...`
Expected: FAIL — `s.SetPath undefined`.

- [ ] **Step 3: Write the implementation**

Dopisz do `blocks.go`:

```go
// blocksFileVersion guards the on-disk format. A file from a newer version is
// refused rather than read partially - Save would then overwrite data written
// by a build that understood more than this one does.
const blocksFileVersion = 1

type blocksFile struct {
	Version int          `json:"version"`
	Tiles   []storedTile `json:"tiles"`
}

type storedTile struct {
	X         int       `json:"x"`
	Y         int       `json:"y"`
	Z         int       `json:"z"`
	Episodes  int       `json:"episodes"`
	FirstSeen time.Time `json:"first_seen"`
}

func (s *BlockStore) SetPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = path
}

func (s *BlockStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // a first run simply has nothing to load
	}
	if err != nil {
		return err
	}
	var f blocksFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("nie udało się odczytać %s: %w", s.path, err)
	}
	if f.Version != blocksFileVersion {
		return fmt.Errorf("plik blokad ma wersję %d, obsługiwana jest %d", f.Version, blocksFileVersion)
	}
	now := s.now()
	for _, t := range f.Tiles {
		s.tiles[tileKey{t.X, t.Y, t.Z}] = &Blockage{Kind: KindPerm, Episodes: t.Episodes,
			First: t.FirstSeen, Forget: now.Add(forgetAfter)}
	}
	s.rev++
	return nil
}

// Save rewrites the file through a temporary in the same directory followed by
// a rename, so a crash mid-write cannot leave a half-written file behind. Only
// permanent blocks are written: a temporary one is a guess with a minute to
// live and has no business outliving the process.
func (s *BlockStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *BlockStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	f := blocksFile{Version: blocksFileVersion, Tiles: []storedTile{}}
	for k, b := range s.tiles {
		if b.Kind != KindPerm {
			continue
		}
		f.Tiles = append(f.Tiles, storedTile{X: k[0], Y: k[1], Z: k[2], Episodes: b.Episodes, FirstSeen: b.First})
	}
	// Stable order keeps the file diffable and its rewrites boring.
	sort.Slice(f.Tiles, func(i, j int) bool {
		a, b := f.Tiles[i], f.Tiles[j]
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	data, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".blocks-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename succeeded
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	s.dirty = false
	return os.Rename(name, s.path)
}

// Flush writes the file if anything changed since the last write. The caller
// is the HTTP handler, so a burst of observations costs at most one rewrite
// per changed observation rather than one per request.
func (s *BlockStore) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	if err := s.saveLocked(); err != nil {
		log.Printf("Nie udało się zapisać blokad: %v", err)
	}
}
```

Zmiany w istniejącym kodzie z Task 1:
- dopisz pola `path string` i `dirty bool` do `BlockStore`,
- ustaw `s.dirty = true` w każdym miejscu, które zmienia **trwałą** blokadę:
  w `Observe` przy `promoted` oraz w `clearLocked`, gdy usuwany wpis miał `Kind == KindPerm`,
- rozszerz importy o `encoding/json`, `errors`, `io/fs`, `log`, `os`, `path/filepath`, `sort`.

`clearLocked` musi zwracać informację, czy usuwany wpis był trwały:

```go
func (s *BlockStore) clearLocked(k tileKey) bool {
	b, ok := s.tiles[k]
	if !ok {
		return false
	}
	if b.Kind == KindPerm {
		s.dirty = true
	}
	delete(s.tiles, k)
	return true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add blocks.go blocks_test.go
git commit -m "Zapisz trwałe blokady do blocks.json

Zapis idzie przez plik tymczasowy i rename, więc przerwany zapis nie zostawia
połowicznego pliku. Na dysk trafiają wyłącznie blokady trwałe - tymczasowa to
hipoteza z minutą życia i nie ma po co przeżywać procesu. Plik w nieznanej
wersji jest odrzucany, nie nadpisywany.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Odróżnij brak danych od ściany

**Files:**
- Modify: `cost.go`
- Modify: `cost_test.go`

**Interfaces:**
- Produces: `func (g *CostGrid) Covered(x, y int) bool` — czy kratka pochodzi z wczytanego kafla PNG.

Dziś brakujący kafel i teren nieprzechodni są w `CostGrid` jedną wartością 255.
Wyszukiwanie trasy ma prawo je mylić — jedno i drugie jest nieprzejezdne — ale
podgląd musi je rozróżnić, inaczej cała niezwiedzona okolica wygląda jak mur.

- [ ] **Step 1: Write the failing test**

Dopisz do `cost_test.go`:

```go
func TestCoveredSeparatesMissingChunksFromWalls(t *testing.T) {
	dir := t.TempDir()
	// One chunk at (32512,32256) with a single blocked tile in its corner.
	pix := make([]uint8, 256*256)
	for i := range pix {
		pix[i] = 100
	}
	pix[0] = blockedCost
	writeCostChunk(t, dir, 32512, 32256, 7, pix)

	// The area reaches into the neighbouring chunk, which has no file at all.
	grid, err := loadCostArea(dir, 7, image.Rect(32500, 32250, 32530, 32270))
	if err != nil {
		t.Fatal(err)
	}
	if !grid.Covered(32512, 32256) {
		t.Fatal("a tile from a decoded chunk reports as uncovered")
	}
	if grid.At(32512, 32256) != blockedCost {
		t.Fatal("the blocked tile lost its cost")
	}
	if grid.Covered(32500, 32250) {
		t.Fatal("a tile with no chunk on disk reports as covered")
	}
	if grid.At(32500, 32250) != blockedCost {
		t.Fatal("a missing tile must still be impassable for the search")
	}
}
```

Jeżeli `cost_test.go` nie ma jeszcze helpera zapisującego kafel, dopisz:

```go
// writeCostChunk writes a 256x256 paletted PNG the way the game does: the
// palette is identity up to 250, so the raw index is the cost.
func writeCostChunk(t *testing.T, dir string, x, y, z int, pix []uint8) {
	t.Helper()
	pal := make(color.Palette, 256)
	for i := range pal {
		pal[i] = color.RGBA{uint8(i), uint8(i), uint8(i), 255}
	}
	im := image.NewPaletted(image.Rect(0, 0, 256, 256), pal)
	copy(im.Pix, pix)
	f, err := os.Create(filepath.Join(dir, fmt.Sprintf("Minimap_WaypointCost_%d_%d_%d.png", x, y, z)))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, im); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestCoveredSeparates ./...`
Expected: FAIL — `grid.Covered undefined`.

- [ ] **Step 3: Write the implementation**

W `cost.go`:

```go
// covered lists the chunks actually decoded into pix. Missing chunks and real
// walls are both blockedCost, which is right for the search and wrong for the
// panel: an unvisited area would look like a solid wall.
type CostGrid struct {
	pix      []uint8
	bounds   image.Rectangle
	area     image.Rectangle
	covered  []image.Rectangle
	cheapest uint8
	walkable bool
}

func (g *CostGrid) Covered(x, y int) bool {
	p := image.Pt(x, y)
	for _, r := range g.covered {
		if p.In(r) {
			return true
		}
	}
	return false
}
```

W `loadCostArea`, w pętli po kaflach, po udanym `decodeCostTile`, dopisz:

```go
grid.covered = append(grid.covered, image.Rect(t.x, t.y, t.x+256, t.y+256))
```

`limitTo` kopiuje strukturę, więc `covered` jedzie z nią bez zmian.

- [ ] **Step 4: Run the tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cost.go cost_test.go
git commit -m "Odróżnij brak danych mapy od terenu nieprzechodniego

Siatka kosztów mapuje brakujący kafel PNG i ścianę na tę samą wartość 255.
Dla wyszukiwania trasy to bez różnicy, ale podgląd przechodności pokazywałby
całą niezwiedzoną okolicę jako lity mur. CostGrid pamięta teraz, które kafle
faktycznie wczytał.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `PathGrid` — A* czyta koszt bazowy plus nakładkę

**Files:**
- Create: `pathgrid.go`
- Modify: `path.go`
- Modify: `path_test.go`
- Modify: `realpath_test.go`, `pathapi.go` (dostosowanie wywołań `findPath`)

**Interfaces:**
- Consumes: `Overlay`, `KindTemp`, `KindPerm`, `tempPenalty` z Task 1; `CostGrid` z `cost.go`.
- Produces:
  - `func NewPathGrid(base *CostGrid, o *Overlay) *PathGrid`
  - `func (g *PathGrid) Blocked(x, y int) bool`
  - `func (g *PathGrid) Cost(x, y int) float64` — jednostki kosztu, 100 = jeden krok
  - `func (g *PathGrid) EdgeBlocked(fx, fy, tx, ty int) bool`
  - `func (g *PathGrid) Cheapest() float64` — dolne ograniczenie kosztu kratki dla heurystyki
  - `findPath(ctx context.Context, grid *PathGrid, from, to [2]int, maxIterations int) PathResult`

- [ ] **Step 1: Write the failing tests**

Dopisz do `path_test.go`:

```go
// overlayOf builds an Overlay for the ASCII grids used across these tests,
// where 'T' marks a temporary block and 'P' a permanent one.
func overlayOf(rows []string) *Overlay {
	o := &Overlay{tiles: map[[2]int]Kind{}, edges: map[[4]int]bool{}}
	for y, row := range rows {
		for x, c := range row {
			switch c {
			case 'T':
				o.tiles[[2]int{1000 + x, 1000 + y}] = KindTemp
			case 'P':
				o.tiles[[2]int{1000 + x, 1000 + y}] = KindPerm
			}
		}
	}
	return o
}

func TestPermanentBlockIsImpassable(t *testing.T) {
	rows := []string{
		"....",
		".P..",
		"....",
	}
	g := NewPathGrid(gridFrom([]string{"....", "....", "...."}), overlayOf(rows))
	r := findPath(context.Background(), g, at(1, 0), at(1, 2), 1000)
	assertWalk(t, r, at(1, 0), at(1, 2))
	for _, s := range r.Steps {
		if s == at(1, 1) {
			t.Fatal("route runs through a permanent block")
		}
	}
}

func TestTemporaryBlockIsAvoidedWhenThereIsAWayAround(t *testing.T) {
	g := NewPathGrid(gridFrom([]string{"....", "....", "...."}), overlayOf([]string{
		"....",
		".T..",
		"....",
	}))
	r := findPath(context.Background(), g, at(1, 0), at(1, 2), 1000)
	assertWalk(t, r, at(1, 0), at(1, 2))
	for _, s := range r.Steps {
		if s == at(1, 1) {
			t.Fatal("route runs through a temporary block even though a detour exists")
		}
	}
}

func TestTemporaryBlockStillPassableWhenThereIsNoWayAround(t *testing.T) {
	// A one-tile doorway in a wall. A player standing in it must not turn the
	// route into "no route" - the bot should wait and retry, not give up.
	g := NewPathGrid(gridFrom([]string{
		"#.#",
		"#.#",
		"#.#",
	}), overlayOf([]string{
		"...",
		".T.",
		"...",
	}))
	r := findPath(context.Background(), g, at(1, 0), at(1, 2), 1000)
	if !r.Found {
		t.Fatalf("status %s (%s); a temporary block must be a penalty, not a wall", r.Status, r.Reason)
	}
	assertWalk(t, r, at(1, 0), at(1, 2))
}

func TestPermanentBlockClosesADiagonalCorner(t *testing.T) {
	// Walking NE from (0,2) to (1,1) squeezes between (1,2) and (0,1). With
	// both blocked the game refuses the step, and so must the search - even
	// when one of the two is a learned block rather than map data.
	g := NewPathGrid(gridFrom([]string{
		"...",
		"#..",
		".#.",
	}), overlayOf([]string{
		"...",
		"...",
		".P.",
	}))
	r := findPath(context.Background(), g, at(0, 2), at(1, 1), 1000)
	for i := 1; i < len(r.Steps); i++ {
		a, b := r.Steps[i-1], r.Steps[i]
		if a == at(0, 2) && b == at(1, 1) {
			t.Fatal("route cut a corner closed by a learned block")
		}
	}
}

func TestBlockedEdgeIsNotWalked(t *testing.T) {
	o := &Overlay{tiles: map[[2]int]Kind{}, edges: map[[4]int]bool{
		{1000, 1002, 1001, 1001}: true,
	}}
	g := NewPathGrid(gridFrom([]string{"...", "...", "..."}), o)
	r := findPath(context.Background(), g, at(0, 2), at(1, 1), 1000)
	if !r.Found {
		t.Fatalf("status %s; blocking one edge must not block the destination", r.Status)
	}
	for i := 1; i < len(r.Steps); i++ {
		if r.Steps[i-1] == at(0, 2) && r.Steps[i] == at(1, 1) {
			t.Fatal("route used an edge known to fail")
		}
	}
}

func TestEmptyOverlayChangesNothing(t *testing.T) {
	rows := []string{"....", ".##.", "...."}
	plain := findPath(context.Background(), NewPathGrid(gridFrom(rows), nil), at(0, 0), at(3, 2), 10000)
	empty := findPath(context.Background(), NewPathGrid(gridFrom(rows), overlayOf([]string{"....", "....", "...."})), at(0, 0), at(3, 2), 10000)
	if plain.Cost != empty.Cost || len(plain.Steps) != len(empty.Steps) {
		t.Fatalf("an empty overlay changed the route: %v vs %v", plain, empty)
	}
}

func TestBaseGridIsNotMutatedByOverlay(t *testing.T) {
	base := gridFrom([]string{"....", "....", "...."})
	before := append([]uint8(nil), base.pix...)
	findPath(context.Background(), NewPathGrid(base, overlayOf([]string{"....", ".P..", "...."})), at(0, 0), at(3, 2), 10000)
	for i := range before {
		if base.pix[i] != before[i] {
			t.Fatalf("the shared cost grid was modified at index %d; the floor cache is now poisoned", i)
		}
	}
}
```

Pozostałe testy w `path_test.go` wołają `findPath` z `*CostGrid`. Zamień w nich
argument na `NewPathGrid(grid, nil)` — `nil` znaczy „brak nakładki".

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestPermanentBlock|TestTemporaryBlock|TestBlockedEdge|TestEmptyOverlay|TestBaseGrid' ./...`
Expected: FAIL — `undefined: NewPathGrid`.

- [ ] **Step 3: Write the implementation**

Utwórz `pathgrid.go`:

```go
package main

import "image"

// PathGrid is what the search walks on: the immutable terrain costs plus a
// snapshot of the learned blockages. The snapshot is taken before the search
// starts, because A* assumes the cost of a closed vertex never changes.
//
// The base grid is never written to. CostGrid.limitTo copies the struct but
// shares the pixel slice with the cached floor, so writing a block into it
// would poison every later query.
type PathGrid struct {
	base    *CostGrid
	overlay *Overlay
}

func NewPathGrid(base *CostGrid, o *Overlay) *PathGrid {
	return &PathGrid{base: base, overlay: o}
}

func (g *PathGrid) Blocked(x, y int) bool {
	if g.base.At(x, y) == blockedCost {
		return true
	}
	return g.overlay.Tile(x, y) == KindPerm
}

// Cost is only meaningful where Blocked is false.
func (g *PathGrid) Cost(x, y int) float64 {
	c := float64(g.base.At(x, y))
	if g.overlay.Tile(x, y) == KindTemp {
		c += tempPenalty
	}
	return c
}

func (g *PathGrid) EdgeBlocked(fx, fy, tx, ty int) bool {
	return g.overlay.Edge(fx, fy, tx, ty)
}

// Cheapest bounds the per-tile cost from below so the octile estimate stays
// admissible. The penalty only ever raises a cost, so the base minimum still
// bounds the overlay grid.
func (g *PathGrid) Cheapest() float64 {
	if !g.base.walkable || float64(g.base.cheapest)/100 > 1 {
		return 1
	}
	return float64(g.base.cheapest) / 100
}

func (g *PathGrid) area() image.Rectangle { return g.base.area }
```

W `path.go` zmień sygnaturę i trzy odczyty:

```go
func findPath(ctx context.Context, grid *PathGrid, from, to [2]int, maxIterations int) PathResult {
	if grid.Blocked(from[0], from[1]) {
		return PathResult{Status: "blocked_start", Reason: "Pozycja startowa jest nieprzechodnia lub poza wczytanym obszarem."}
	}
	if grid.Blocked(to[0], to[1]) {
		return PathResult{Status: "blocked_goal", Reason: "Waypoint leży na kratce nieprzechodniej lub poza wczytanym obszarem."}
	}
	...
	floor := grid.Cheapest()
	...
			if grid.Blocked(next[0], next[1]) {
				continue
			}
			if grid.EdgeBlocked(cur.at[0], cur.at[1], next[0], next[1]) {
				continue
			}
			if s.dx != 0 && s.dy != 0 &&
				grid.Blocked(cur.at[0]+s.dx, cur.at[1]) &&
				grid.Blocked(cur.at[0], cur.at[1]+s.dy) {
				continue
			}
			g := best[cur.at] + s.weight*grid.Cost(next[0], next[1])/100
```

Usuń z `findPath` lokalne wyliczanie `floor` z `grid.cheapest`/`grid.walkable` —
zastępuje je `grid.Cheapest()`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./...`
Expected: PASS — łącznie z porównaniem z Dijkstrą na losowych siatkach, które
sprawdza optymalność.

- [ ] **Step 5: Commit**

```bash
git add pathgrid.go path.go path_test.go realpath_test.go pathapi.go
git commit -m "Wyszukiwanie trasy czyta koszt bazowy razem z nakładką blokad

PathGrid łączy niezmienną siatkę terenu ze zdjęciem nauczonych blokad,
robionym raz przed startem wyszukiwania: A* zakłada, że koszt zamkniętego
wierzchołka się nie zmienia. Blokada trwała jest nieprzechodnia, tymczasowa
podnosi koszt o 500 - dzięki temu gracz w jednokratkowych drzwiach nie zamienia
trasy w brak trasy. Bazowa tablica pikseli nie jest dotykana; współdzieli ją
cache piętra.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `/api/path` używa nakładki i kasuje blokadę pod postacią

**Files:**
- Modify: `pathapi.go`
- Modify: `main.go` (pole `blocks` w `server`, flaga `-blocks`, wczytanie pliku)
- Modify: `pathapi_test.go`

**Interfaces:**
- Consumes: `BlockStore`, `NewPathGrid` z Tasks 1–4.
- Produces: pole `blocks *BlockStore` w `server`; odpowiedź `/api/path` z polem `overlay_revision uint64`.

- [ ] **Step 1: Write the failing tests**

Dopisz do `pathapi_test.go`:

```go
func TestPathAvoidsLearnedBlock(t *testing.T) {
	dir := t.TempDir()
	pix := make([]uint8, 256*256)
	for i := range pix {
		pix[i] = 100
	}
	writeCostChunk(t, dir, 32512, 32256, 7, pix)
	s := &server{dir: dir, gate: make(chan struct{}, 1), blocks: NewBlockStore(time.Now)}

	body := `{"from":{"x":32600,"y":32300,"z":7},"to":{"x":32600,"y":32304,"z":7},"margin":8}`
	var before PathResult
	postJSON(t, s, "/api/path", body, &before)
	if !before.Found {
		t.Fatalf("baseline route not found: %s", before.Reason)
	}

	// Learn that the tile straight ahead cannot be entered, twice, so it
	// becomes permanent and the route has to go around it.
	obs := Observation{From: Position{32600, 32301, 7}, To: Position{32600, 32302, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100}
	s.blocks.Observe(obs)
	s.blocks.tiles[tileKey{32600, 32302, 7}].Kind = KindPerm

	var after PathResult
	postJSON(t, s, "/api/path", body, &after)
	if !after.Found {
		t.Fatalf("route not found after learning a block: %s", after.Reason)
	}
	for _, st := range after.Steps {
		if st == [2]int{32600, 32302} {
			t.Fatal("route still runs through the learned block")
		}
	}
}

func TestStandingOnABlockClearsIt(t *testing.T) {
	dir := t.TempDir()
	pix := make([]uint8, 256*256)
	for i := range pix {
		pix[i] = 100
	}
	writeCostChunk(t, dir, 32512, 32256, 7, pix)
	s := &server{dir: dir, gate: make(chan struct{}, 1), blocks: NewBlockStore(time.Now)}
	s.blocks.Observe(Observation{From: Position{32600, 32301, 7}, To: Position{32600, 32300, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})

	var r PathResult
	postJSON(t, s, "/api/path", `{"from":{"x":32600,"y":32300,"z":7},"to":{"x":32600,"y":32304,"z":7}}`, &r)

	if k := s.blocks.Snapshot(image.Rect(32590, 32290, 32610, 32310), 7).Tile(32600, 32300); k != KindNone {
		t.Fatalf("tile kind %v; the character is standing there, which beats any learned guess", k)
	}
}
```

Jeżeli `pathapi_test.go` nie ma jeszcze `postJSON`, dopisz:

```go
// postJSON runs one request through the server's own routes, so the test
// covers the handler wiring and not just the function under it.
func postJSON(t *testing.T, s *server, path, body string, out any) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Host = "127.0.0.1:8095"
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d: %s", path, w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("%s: %v (%s)", path, err, w.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestPathAvoidsLearned|TestStandingOnABlock' ./...`
Expected: FAIL — `unknown field blocks in struct literal`.

- [ ] **Step 3: Write the implementation**

W `main.go`:

```go
type server struct {
	...
	blocks *BlockStore
}
```

W `main()`, po sparsowaniu flag:

```go
blocksPath := flag.String("blocks", "blocks.json",
	"plik nauczonych blokad; katalog map to pobrana paczka i nasze wpisy nie powinny się z nią mieszać")
...
s.blocks = NewBlockStore(time.Now)
s.blocks.SetPath(*blocksPath)
if err := s.blocks.Load(); err != nil {
	log.Fatal(err)
}
```

Wczytanie kończy się `log.Fatal`, bo start z pustą nakładką po cichu oznaczałby
utratę wiedzy, którą użytkownik uważa za zapisaną.

W `pathapi.go`, w `path()`, po wyliczeniu `area` i pobraniu `grid`:

```go
	// Presence beats any learned hypothesis: if the character is standing on a
	// tile we marked unreachable, the mark is simply wrong.
	if s.blocks != nil {
		s.blocks.Clear(from)
	}
	overlay := s.blocks.Snapshot(area, from.Z)
	result := findPath(ctx, NewPathGrid(grid.limitTo(area), overlay), [2]int{from.X, from.Y}, [2]int{to.X, to.Y}, area.Dx()*area.Dy())
	result.OverlayRevision = s.blocks.Revision()
```

`Snapshot` na `nil` odbiorcy musi działać — dopisz w `blocks.go`:

```go
func (s *BlockStore) Snapshot(area image.Rectangle, z int) *Overlay {
	if s == nil {
		return nil
	}
	...
}
```
oraz analogiczny warunek w `Revision` i `Clear`. Dzięki temu testy, które budują
`server` bez magazynu, nadal działają.

W `path.go` dopisz pole do `PathResult`:

```go
	// OverlayRevision lets the panel tell a route computed before its latest
	// observation from one computed after it.
	OverlayRevision uint64 `json:"overlay_revision"`
```

- [ ] **Step 4: Run the tests**

Run: `go test ./...` oraz `go test -race ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pathapi.go path.go main.go pathapi_test.go
git commit -m "Trasa liczy się z uwzględnieniem nauczonych blokad

/api/path robi zdjęcie nakładki przed wyszukiwaniem i zwraca jej rewizję, więc
panel odróżni trasę policzoną przed swoją ostatnią obserwacją od tej po niej.
Postać stojąca na kratce oznaczonej jako nieprzechodnia kasuje ten wpis:
obecność jest mocniejszym dowodem niż hipoteza, a kosztuje jedno sprawdzenie
mapy zamiast osobnego żądania.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `/api/blocks` — zgłaszanie, podgląd listy, usuwanie

**Files:**
- Create: `blocksapi.go`
- Create: `blocksapi_test.go`
- Modify: `main.go` (trzy trasy)

**Interfaces:**
- Consumes: `BlockStore.Observe`, `Clear`, `Flush`, `Snapshot` z Tasks 1–2.
- Produces:
  - `POST /api/blocks/observe` — ciało `Observation`, odpowiedź `Decision`
  - `GET /api/blocks?x=&y=&z=&r=` — lista wpisów w obszarze
  - `DELETE /api/blocks` — ciało `{"x":…,"y":…,"z":…}`, odpowiedź `{"cleared":bool}`
  - `func (s *BlockStore) List(area image.Rectangle, z int) []BlockInfo`
  - `type BlockInfo struct { X, Y, Z int; Kind string; Episodes int; ExpiresInMS int }`

- [ ] **Step 1: Write the failing tests**

Utwórz `blocksapi_test.go`:

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newBlocksServer() *server {
	return &server{gate: make(chan struct{}, 1), blocks: NewBlockStore(time.Now)}
}

func TestObserveEndpointLearnsABlock(t *testing.T) {
	s := newBlocksServer()
	var d Decision
	postJSON(t, s, "/api/blocks/observe",
		`{"from":{"x":100,"y":100,"z":7},"to":{"x":100,"y":99,"z":7},"outcome":"no_motion","still_frames":3,"last_frame_age_ms":120}`, &d)
	if d.Result != "temp" {
		t.Fatalf("result %q (%s), want temp", d.Result, d.Reason)
	}
}

func TestObserveEndpointExplainsARefusal(t *testing.T) {
	s := newBlocksServer()
	var d Decision
	postJSON(t, s, "/api/blocks/observe",
		`{"from":{"x":100,"y":100,"z":7},"to":{"x":100,"y":99,"z":7},"outcome":"no_motion","still_frames":1,"last_frame_age_ms":120}`, &d)
	if d.Result != "ignored" {
		t.Fatalf("result %q, want ignored", d.Result)
	}
	if d.Reason == "" {
		t.Fatal("a refusal with no reason is indistinguishable from a lost request")
	}
}

func TestObserveEndpointRefusesMalformedBody(t *testing.T) {
	s := newBlocksServer()
	req := httptest.NewRequest("POST", "/api/blocks/observe", strings.NewReader(`{"from":`))
	req.Host = "127.0.0.1:8095"
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

func TestListEndpointReportsLearnedBlocks(t *testing.T) {
	s := newBlocksServer()
	s.blocks.Observe(Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})

	req := httptest.NewRequest("GET", "/api/blocks?x=100&y=100&z=7&r=10", nil)
	req.Host = "127.0.0.1:8095"
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var list []BlockInfo
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].X != 100 || list[0].Y != 99 || list[0].Kind != "temp" {
		t.Fatalf("list %+v, want one temporary block at (100,99)", list)
	}
	if list[0].ExpiresInMS <= 0 {
		t.Fatalf("expires_in_ms %d; the panel needs it to show a countdown", list[0].ExpiresInMS)
	}
}

func TestDeleteEndpointRemovesABlock(t *testing.T) {
	s := newBlocksServer()
	s.blocks.Observe(Observation{From: Position{100, 100, 7}, To: Position{100, 99, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})

	req := httptest.NewRequest("DELETE", "/api/blocks", strings.NewReader(`{"x":100,"y":99,"z":7}`))
	req.Host = "127.0.0.1:8095"
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if k := s.blocks.Snapshot(rectAround(100, 99, 10), 7).Tile(100, 99); k != KindNone {
		t.Fatalf("tile kind %v after delete, want KindNone", k)
	}
}

func TestBlocksRoutesAnswer503WithoutAStore(t *testing.T) {
	s := &server{gate: make(chan struct{}, 1)}
	req := httptest.NewRequest("GET", "/api/blocks?x=1&y=1&z=7&r=4", nil)
	req.Host = "127.0.0.1:8095"
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 - a server without a store must say so, not pretend the map is empty", w.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestObserveEndpoint|TestListEndpoint|TestDeleteEndpoint|TestBlocksRoutes' ./...`
Expected: FAIL — `undefined: BlockInfo`, brak tras.

- [ ] **Step 3: Write the implementation**

Utwórz `blocksapi.go`:

```go
package main

import (
	"encoding/json"
	"image"
	"net/http"
	"strconv"
	"time"
)

// maxBlocksRadius keeps one listing bounded. The panel never asks for more
// than its own preview window.
const maxBlocksRadius = 64

type BlockInfo struct {
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Z           int    `json:"z"`
	Kind        string `json:"kind"`
	Episodes    int    `json:"episodes"`
	ExpiresInMS int    `json:"expires_in_ms"`
}

// rectAround is the square window shared by the blocks listing and the grid
// preview, so both agree on what "radius" means.
func rectAround(x, y, r int) image.Rectangle {
	return image.Rect(x-r, y-r, x+r+1, y+r+1)
}

func (s *server) observeBlock(w http.ResponseWriter, r *http.Request) {
	if s.blocks == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Magazyn blokad nie jest włączony.")
		return
	}
	var obs Observation
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&obs); err != nil {
		http.Error(w, "Nieprawidłowe żądanie JSON", http.StatusBadRequest)
		return
	}
	d := s.blocks.Observe(obs)
	// Only a change worth keeping touches the disk; an ignored observation
	// must not rewrite the file.
	if d.Result == "promoted" || d.Result == "cleared" {
		s.blocks.Flush()
	}
	writeJSON(w, d)
}

func (s *server) listBlocks(w http.ResponseWriter, r *http.Request) {
	if s.blocks == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Magazyn blokad nie jest włączony.")
		return
	}
	x, y, z, radius, ok := windowParams(r)
	if !ok {
		http.Error(w, "Wymagane x, y (0–65535), z (0–15) i r (1–64).", http.StatusBadRequest)
		return
	}
	writeJSON(w, s.blocks.List(rectAround(x, y, radius), z))
}

func (s *server) deleteBlock(w http.ResponseWriter, r *http.Request) {
	if s.blocks == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "Magazyn blokad nie jest włączony.")
		return
	}
	var p Position
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&p); err != nil {
		http.Error(w, "Nieprawidłowe żądanie JSON", http.StatusBadRequest)
		return
	}
	cleared := s.blocks.Clear(p)
	if cleared {
		s.blocks.Flush()
	}
	writeJSON(w, map[string]bool{"cleared": cleared})
}

// windowParams parses the x/y/z/r query shared by the listing and the preview.
func windowParams(r *http.Request) (x, y, z, radius int, ok bool) {
	q := r.URL.Query()
	x, err1 := strconv.Atoi(q.Get("x"))
	y, err2 := strconv.Atoi(q.Get("y"))
	z, err3 := strconv.Atoi(q.Get("z"))
	radius, err4 := strconv.Atoi(q.Get("r"))
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return 0, 0, 0, 0, false
	}
	if x < 0 || x > 65535 || y < 0 || y > 65535 || z < 0 || z > 15 || radius < 1 || radius > maxBlocksRadius {
		return 0, 0, 0, 0, false
	}
	return x, y, z, radius, true
}
```

Dopisz `List` do `blocks.go`:

```go
func (s *BlockStore) List(area image.Rectangle, z int) []BlockInfo {
	if s == nil {
		return []BlockInfo{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	out := []BlockInfo{}
	for k, b := range s.tiles {
		if b.Kind == KindNone || k[2] != z || !image.Pt(k[0], k[1]).In(area) {
			continue
		}
		info := BlockInfo{X: k[0], Y: k[1], Z: k[2], Episodes: b.Episodes, Kind: "perm"}
		if b.Kind == KindTemp {
			info.Kind = "temp"
			info.ExpiresInMS = int(b.Expires.Sub(now) / time.Millisecond)
		}
		out = append(out, info)
	}
	return out
}
```

W `main.go`, w `routes()`:

```go
	mux.HandleFunc("POST /api/blocks/observe", s.observeBlock)
	mux.HandleFunc("GET /api/blocks", s.listBlocks)
	mux.HandleFunc("DELETE /api/blocks", s.deleteBlock)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add blocksapi.go blocksapi_test.go blocks.go main.go
git commit -m "Endpointy do zgłaszania, oglądania i kasowania blokad

Panel zgłasza obserwację, decyzję podejmuje serwer i zwraca ją z powodem -
odrzucenie nigdy nie wygląda jak zgubione żądanie. Plik na dysku jest ruszany
tylko przy zmianie trwałej, nie przy każdym zgłoszeniu.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: `/api/grid` — okno przechodności dla podglądu

**Files:**
- Create: `gridapi.go`
- Create: `gridapi_test.go`
- Modify: `main.go` (jedna trasa, pola cache'u podglądu w `server`)

**Interfaces:**
- Consumes: `CostGrid.Covered` (Task 3), `BlockStore.Snapshot` (Task 1), `windowParams`, `rectAround` (Task 6).
- Produces: `GET /api/grid?x=&y=&z=&r=` → `application/octet-stream`, `(2r+1)²` bajtów, nagłówki `X-Grid-Origin: <x>,<y>` i `X-Grid-Revision`.

Bity w bajcie kratki: `1` nieprzechodni w danych, `2` brak danych, `4` blokada
tymczasowa, `8` blokada trwała.

- [ ] **Step 1: Write the failing tests**

Utwórz `gridapi_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func getGrid(t *testing.T, s *server, query string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/grid?"+query, nil)
	req.Host = "127.0.0.1:8095"
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)
	return w, w.Body.Bytes()
}

func TestGridWindowHasOneBytePerTile(t *testing.T) {
	dir := t.TempDir()
	pix := make([]uint8, 256*256)
	for i := range pix {
		pix[i] = 100
	}
	writeCostChunk(t, dir, 32512, 32256, 7, pix)
	s := &server{dir: dir, gate: make(chan struct{}, 1), blocks: NewBlockStore(time.Now)}

	w, body := getGrid(t, s, "x=32600&y=32300&z=7&r=32")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, string(body))
	}
	if got, want := len(body), 65*65; got != want {
		t.Fatalf("%d bytes, want %d", got, want)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("content type %q", ct)
	}
	if origin := w.Header().Get("X-Grid-Origin"); origin != "32568,32268" {
		t.Fatalf("origin %q, want the window's top-left corner", origin)
	}
}

func TestGridDistinguishesWallFromMissingData(t *testing.T) {
	dir := t.TempDir()
	pix := make([]uint8, 256*256)
	for i := range pix {
		pix[i] = 100
	}
	pix[0] = blockedCost // world (32512,32256)
	writeCostChunk(t, dir, 32512, 32256, 7, pix)
	s := &server{dir: dir, gate: make(chan struct{}, 1), blocks: NewBlockStore(time.Now)}

	// Window centred so it spans the chunk edge: tiles left of x=32512 have no file.
	_, body := getGrid(t, s, "x=32512&y=32300&z=7&r=4")
	side := 9
	idx := func(x, y int) int { return (y-(32300-4))*side + (x - (32512 - 4)) }

	if body[idx(32511, 32300)]&2 == 0 {
		t.Fatal("a tile with no chunk on disk is not flagged as missing data")
	}
	if body[idx(32512, 32300)]&2 != 0 {
		t.Fatal("a tile from a decoded chunk is flagged as missing data")
	}
}

func TestGridShowsLearnedBlocks(t *testing.T) {
	dir := t.TempDir()
	pix := make([]uint8, 256*256)
	for i := range pix {
		pix[i] = 100
	}
	writeCostChunk(t, dir, 32512, 32256, 7, pix)
	s := &server{dir: dir, gate: make(chan struct{}, 1), blocks: NewBlockStore(time.Now)}
	s.blocks.Observe(Observation{From: Position{32600, 32301, 7}, To: Position{32600, 32300, 7},
		Outcome: "no_motion", StillFrames: 3, LastFrameAgeMS: 100})

	_, body := getGrid(t, s, "x=32600&y=32300&z=7&r=4")
	side := 9
	centre := body[(4)*side+4]
	if centre&4 == 0 {
		t.Fatalf("centre byte %08b does not carry the temporary-block bit", centre)
	}
	if centre&1 != 0 {
		t.Fatal("a learned block must not be reported as map terrain")
	}
}

func TestGridRefusesAnOutOfRangeRadius(t *testing.T) {
	s := &server{dir: t.TempDir(), gate: make(chan struct{}, 1), blocks: NewBlockStore(time.Now)}
	if w, _ := getGrid(t, s, "x=32600&y=32300&z=7&r=999"); w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if w, _ := getGrid(t, s, "x=32600&y=32300&z=7&r=0"); w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run TestGrid ./...`
Expected: FAIL — brak trasy `/api/grid` (404).

- [ ] **Step 3: Write the implementation**

Utwórz `gridapi.go`:

```go
package main

import (
	"fmt"
	"net/http"
)

// Bits of one preview tile. Missing data and a wall are separate on purpose:
// merged, every unvisited area would look like solid rock.
const (
	gridBlocked = 1 << iota
	gridMissing
	gridTemp
	gridPerm
)

// grid answers the panel's live walkability window. It uses its own small
// cache rather than the route planner's: the two ask for very different
// rectangles, and sharing one cache would make them evict each other on every
// single reading.
func (s *server) grid(w http.ResponseWriter, r *http.Request) {
	x, y, z, radius, ok := windowParams(r)
	if !ok {
		http.Error(w, "Wymagane x, y (0–65535), z (0–15) i r (1–64).", http.StatusBadRequest)
		return
	}
	area := rectAround(x, y, radius)
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	if s.previewCache == nil || s.previewFloor != z || !area.In(s.previewCache.bounds) {
		g, err := loadCostArea(s.dir, z, area)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.previewCache, s.previewFloor = g, z
	}
	grid := s.previewCache
	overlay := s.blocks.Snapshot(area, z)

	side := 2*radius + 1
	out := make([]byte, side*side)
	for row := 0; row < side; row++ {
		for col := 0; col < side; col++ {
			wx, wy := area.Min.X+col, area.Min.Y+row
			var b byte
			if !grid.Covered(wx, wy) {
				b |= gridMissing
			} else if grid.At(wx, wy) == blockedCost {
				b |= gridBlocked
			}
			switch overlay.Tile(wx, wy) {
			case KindTemp:
				b |= gridTemp
			case KindPerm:
				b |= gridPerm
			}
			out[row*side+col] = b
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Grid-Origin", fmt.Sprintf("%d,%d", area.Min.X, area.Min.Y))
	w.Header().Set("X-Grid-Revision", fmt.Sprintf("%d", s.blocks.Revision()))
	w.Write(out)
}
```

W `main.go`: pola `previewMu sync.Mutex`, `previewCache *CostGrid`,
`previewFloor int` w `server`, oraz trasa `mux.HandleFunc("GET /api/grid", s.grid)`.

- [ ] **Step 4: Run the tests**

Run: `go test ./...` oraz `go test -race ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gridapi.go gridapi_test.go main.go
git commit -m "Okno przechodności dla podglądu na żywo

Jeden bajt na kratkę, cztery bity: teren nieprzechodni, brak danych, blokada
tymczasowa, blokada trwała. Endpoint ma własny mały cache - planer trasy i
podgląd pytają o zupełnie inne prostokąty i przy wspólnym cache'u wypierałyby
się nawzajem przy każdym odczycie.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Executor rozróżnia, co się właściwie stało

**Files:**
- Modify: `web/executor.js`
- Modify: `executor_test.cjs`

**Interfaces:**
- Produces:
  - `StepExecutor.takeObservation()` → `{from:{x,y,z}, to:{x,y,z}, outcome, still_frames, last_frame_age_ms}` albo `null`; zwrócenie czyści bufor
  - pole `stillFrames` w obiekcie `pending`

Dziś `failPending()` traktuje tak samo „postać nie ruszyła się" i „klawisz nigdy
nie wyszedł z drivera". Do nauki wolno użyć wyłącznie tego pierwszego.

- [ ] **Step 1: Write the failing tests**

Dopisz do `executor_test.cjs`:

```js
// Drives one step that never moves the character, feeding `frames` readings
// that all show it still standing on `from`.
const failedStep = (ex, {frames = 3, from = at(100, 100), target = [100, 99], timeout = 1800} = {}) => {
  ex.observe(from, 0, 0);
  ex.intentFor(walk('N', target), 10);
  ex.emitted(20, ex.state().stepId);
  for (let i = 0; i < frames; i++) ex.observe(from, 100 + i * 100, 110 + i * 100);
  ex.intentFor(walk('N', target), 20 + timeout + 50);
  return ex.takeObservation();
};

test('krok bez ruchu z trzema klatkami produkuje obserwację', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800});
  const obs = failedStep(ex);
  assert.equal(obs.outcome, 'no_motion');
  assert.deepEqual(obs.from, {x: 100, y: 100, z: 7});
  assert.deepEqual(obs.to, {x: 100, y: 99, z: 7});
  assert.ok(obs.still_frames >= 3, `still_frames=${obs.still_frames}`);
});

test('obserwacja jest oddawana tylko raz', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800});
  failedStep(ex);
  assert.equal(ex.takeObservation(), null);
});

test('krok bez potwierdzenia emisji niczego nie uczy', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800});
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 10);
  // emitted() never called: the key may never have left the driver.
  ex.observe(at(100, 100), 100, 110);
  ex.intentFor(walk('N', [100, 99]), 10 + 2 * 1800 + 50);
  assert.equal(ex.takeObservation(), null);
});

test('zmiana piętra niczego nie uczy', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800});
  ex.observe(at(100, 100, 7), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 10);
  ex.emitted(20, ex.state().stepId);
  // Walking onto stairs changes Z; that is not a wall.
  ex.observe(at(100, 100, 6), 100, 110);
  ex.intentFor(walk('N', [100, 99]), 20 + 1850);
  assert.equal(ex.takeObservation(), null);
});

test('przesunięcie gdzie indziej niczego nie uczy', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800});
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 10);
  ex.emitted(20, ex.state().stepId);
  ex.observe(at(101, 101), 100, 110); // pushed by a creature, or the player took over
  ex.intentFor(walk('N', [100, 99]), 20 + 1850);
  assert.equal(ex.takeObservation(), null);
});

test('za mało klatek to za słaby dowód', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800});
  assert.equal(failedStep(ex, {frames: 1}), null);
});

test('udany krok nie produkuje obserwacji', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800});
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 10);
  ex.emitted(20, ex.state().stepId);
  ex.observe(at(100, 99), 100, 110);
  assert.equal(ex.takeObservation(), null);
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `node --test executor_test.cjs`
Expected: FAIL — `ex.takeObservation is not a function`.

- [ ] **Step 3: Write the implementation**

W `web/executor.js`:

```js
  // minStillFrames matches the server's own gate. One stale reading is not
  // proof the character stayed put; three consecutive ones are the cheapest
  // evidence that rules out a single dropped frame.
  const MIN_STILL_FRAMES = 3;
```

W `reset()` dopisz `this.observation = null;`.

W `intentFor`, przy tworzeniu `pending` dla kroku `walk`, dodaj pola:

```js
      this.pending = {kind: 'walk', target: out.next, from: this.last,
        stillFrames: 0, lastFrameAt: null,
        emittedAt: null, sentAt: now, id: this.nextId++};
```

W `observe`, w gałęzi zwykłego kroku, przed dotychczasowymi sprawdzeniami:

```js
    // A step that changed the floor is not a failed step: walking onto stairs
    // does exactly this. Learning from it would teach the bot that stairs are
    // a wall.
    if (p.from && position.z !== p.from.z) { this.pending = null; return; }
```

i w gałęzi „stoi tam, gdzie stał":

```js
    const stillThere = !p.from || (position.x === p.from.x && position.y === p.from.y);
    if (!stillThere) { this.pending = null; return; }
    p.stillFrames++;
    p.lastFrameAt = capturedAt;
```

W `failPending(p, now)` — funkcja dostaje teraz `now` — na początku:

```js
  // Only a step that was confirmed emitted and then watched standing still is
  // evidence about the map. A key that never left the driver, a lost position
  // or a floor change say nothing about the tile.
  if (p.kind === 'walk' && p.emittedAt !== null && p.from && p.stillFrames >= MIN_STILL_FRAMES) {
    this.observation = {
      from: {x: p.from.x, y: p.from.y, z: p.from.z},
      to: {x: p.target[0], y: p.target[1], z: p.from.z},
      outcome: 'no_motion',
      still_frames: p.stillFrames,
      last_frame_age_ms: Math.round(now - p.lastFrameAt),
    };
  }
```

Zaktualizuj oba wywołania `this.failPending(p)` w `intentFor` na `this.failPending(p, now)`.

Dopisz metodę:

```js
  // takeObservation hands the pending observation to the caller and forgets
  // it, so one failed step is reported exactly once no matter how many times
  // the panel ticks before the report goes out.
  takeObservation() {
    const obs = this.observation;
    this.observation = null;
    return obs;
  }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `node --test executor_test.cjs`
Expected: PASS — również wszystkie dotychczasowe testy w tym pliku.

- [ ] **Step 5: Commit**

```bash
git add web/executor.js executor_test.cjs
git commit -m "Executor rozróżnia brak ruchu od nieudanego wysłania klawisza

Obserwację o mapie produkuje wyłącznie krok potwierdzony jako wysłany, po
którym trzy kolejne świeże klatki pokazały postać wciąż na kratce startowej.
Zmiana piętra nie uczy niczego - inaczej bot nauczyłby się, że schody to
ściana, bo wejście na nie wygląda jak nieudany krok.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Spóźnione przybycie, wygasanie blokady i dłuższy timeout

**Files:**
- Modify: `web/executor.js`
- Modify: `executor_test.cjs`

**Interfaces:**
- Consumes: `takeObservation()` z Task 8.
- Produces: `stepTimeoutMS` domyślnie `1800`, `lateArrivalMS` domyślnie `600`, `blockedTTL` domyślnie `60000`.

- [ ] **Step 1: Write the failing tests**

Dopisz do `executor_test.cjs`:

```js
test('domyślny timeout kroku wynosi 1800 ms', () => {
  // A step on mud or under paralysis takes well over a second; a shorter
  // timeout would turn every such move into a false blockage.
  assert.equal(new StepExecutor().stepTimeoutMS, 1800);
});

test('spóźnione wejście na kratkę odwołuje naukę', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800, lateArrivalMS: 600});
  failedStep(ex);
  ex.takeObservation(); // the panel already reported the failure

  // 300 ms after the timeout the character finally arrives: that was lag.
  ex.observe(at(100, 99), 2200, 2210);

  const obs = ex.takeObservation();
  assert.equal(obs.outcome, 'entered');
  assert.deepEqual(obs.to, {x: 100, y: 99, z: 7});
});

test('wejście długo po timeoucie nie jest już odwołaniem', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800, lateArrivalMS: 600});
  failedStep(ex);
  ex.takeObservation();

  ex.observe(at(100, 99), 5000, 5010);

  assert.equal(ex.takeObservation(), null);
});

test('spóźnione wejście zdejmuje blokadę celu', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800, lateArrivalMS: 600});
  failedStep(ex);            // first failure
  failedStep(ex);            // second failure on the same target: blocked
  assert.equal(ex.state().blocked, true);

  ex.observe(at(100, 99), 5000, 5010 + 1);
  assert.equal(ex.state().blocked, false, 'a target the character reached cannot stay blocked');
});

test('blokada celu wygasa po swoim czasie', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1800, blockedTTL: 60000});
  failedStep(ex);
  failedStep(ex);
  assert.equal(ex.state().blocked, true);

  // The follower keeps asking for the same tile - with a cost penalty rather
  // than a wall on the server, A* may well still route through it. Without a
  // TTL the executor would refuse that target for the rest of the session.
  const out = walk('N', [100, 99]);
  assert.equal(ex.intentFor(out, 100000), null);
  assert.notEqual(ex.intentFor(out, 200000), null);
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `node --test executor_test.cjs`
Expected: FAIL — domyślny timeout to 1200, brak `lateArrivalMS`.

- [ ] **Step 3: Write the implementation**

W konstruktorze `StepExecutor`:

```js
    // 1800 ms, not 1200: step duration in Tibia scales with the character's
    // speed and the ground cost, and a step on mud or under paralysis runs
    // well past a second. A timeout shorter than the step itself would turn
    // ordinary slow movement into learned blockages.
    this.stepTimeoutMS = options.stepTimeoutMS ?? 1800;
    // How long after a timeout an arrival still counts as "that was lag, not
    // a wall" and revokes what the failure taught.
    this.lateArrivalMS = options.lateArrivalMS ?? 600;
    // A blocked target is a suspicion with a shelf life, not a verdict. It
    // matches the server's temporary-block TTL.
    this.blockedTTL = options.blockedTTL ?? 60000;
```

W `reset()` dopisz `this.recentFailure = null;` oraz `this.blockedAt = null;`.

W `failPending(p, now)`, po zbudowaniu obserwacji:

```js
  if (p.kind === 'walk' && p.from) {
    this.recentFailure = {to: {x: p.target[0], y: p.target[1], z: p.from.z}, at: now};
  }
```

oraz przy ustawianiu blokady: `this.blockedAt = now;`.

W `observe(position, capturedAt, now)`, zaraz po `this.last = {...position}`:

```js
    // The character arrived after all, just late. That was lag or paralysis,
    // not an obstacle - revoke whatever the failure taught, and let the
    // target be tried again.
    const late = this.recentFailure;
    if (late && now - late.at <= this.lateArrivalMS &&
        position.x === late.to.x && position.y === late.to.y && position.z === late.to.z) {
      this.observation = {from: {...late.to}, to: {...late.to}, outcome: 'entered',
        still_frames: 1, last_frame_age_ms: Math.round(now - capturedAt)};
      this.recentFailure = null;
      this.retries = 0;
      if (this.blocked && sameTarget(targetOf({kind: 'walk', target: [late.to.x, late.to.y]}), this.blockedTarget)) {
        this.blocked = false;
        this.blockedTarget = null;
        this.blockedAt = null;
      }
    }
```

W `intentFor`, w gałęzi `if (this.blocked)`, przed porównaniem celu:

```js
      if (this.blockedAt !== null && now - this.blockedAt >= this.blockedTTL) {
        this.blocked = false;
        this.blockedTarget = null;
        this.blockedAt = null;
      } else {
        if (!out) return null;
        if (sameTarget(targetFromOut(out), this.blockedTarget)) return null;
        this.blocked = false;
        this.blockedTarget = null;
        this.blockedAt = null;
      }
```

W `done()` dopisz `this.blockedAt = null; this.recentFailure = null;`.

- [ ] **Step 4: Run the tests**

Run: `node --test executor_test.cjs`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/executor.js executor_test.cjs
git commit -m "Odwołaj naukę, gdy postać jednak doszła, i daj blokadzie czas życia

Wejście na kratkę do 600 ms po timeoucie oznacza lag albo paraliż, nie
przeszkodę - executor zgłasza wtedy wejście i zdejmuje blokadę celu. Sama
blokada celu przestaje trwać bez końca: przy karze kosztu zamiast ściany
serwer nadal może prowadzić trasę tą samą kratką, więc cel się nie zmienia i
bot stałby w miejscu mimo że przeszkoda zniknęła.

Timeout kroku rośnie z 1200 do 1800 ms, bo krok na błocie albo pod paraliżem
trwa dłużej niż sekundę.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: `web/blocks.js` — klient obserwacji i okna podglądu

**Files:**
- Create: `web/blocks.js`
- Create: `blocks_test.cjs`

**Interfaces:**
- Produces:
  - `class BlocksClient { constructor({fetch, minIntervalMS}) }`
  - `async report(observation)` → `Decision` albo `null`, gdy nic nie wysłano
  - `async window(x, y, z, r)` → `{origin:[x,y], revision, cells: Uint8Array}` albo `null`
  - `async remove(x, y, z)` → `bool`
  - `shouldRefresh(position, now)` → `bool`

- [ ] **Step 1: Write the failing tests**

Utwórz `blocks_test.cjs`:

```js
const test = require('node:test');
const assert = require('node:assert');
const {BlocksClient} = require('./web/blocks.js');

const okJSON = (body) => ({ok: true, status: 200, json: async () => body});
const okBytes = (bytes, headers = {}) => ({
  ok: true, status: 200,
  headers: {get: (k) => headers[k.toLowerCase()] ?? null},
  arrayBuffer: async () => new Uint8Array(bytes).buffer,
});

test('obserwacja idzie na endpoint jako JSON', async () => {
  const calls = [];
  const c = new BlocksClient({fetch: async (url, init) => { calls.push([url, JSON.parse(init.body)]); return okJSON({result: 'temp', reason: 'ok'}); }});
  const d = await c.report({from: {x: 1, y: 2, z: 7}, to: {x: 1, y: 1, z: 7}, outcome: 'no_motion', still_frames: 3, last_frame_age_ms: 100});
  assert.equal(d.result, 'temp');
  assert.equal(calls[0][0], '/api/blocks/observe');
  assert.equal(calls[0][1].outcome, 'no_motion');
});

test('odrzucone żądanie nie wybucha', async () => {
  const c = new BlocksClient({fetch: async () => { throw new Error('sieć'); }});
  assert.equal(await c.report({outcome: 'no_motion'}), null);
});

test('okno podglądu wraca jako bajty z pochodzeniem i rewizją', async () => {
  const c = new BlocksClient({fetch: async () => okBytes([0, 1, 2, 8], {'x-grid-origin': '100,200', 'x-grid-revision': '5'})});
  const w = await c.window(101, 201, 7, 1);
  assert.deepEqual(w.origin, [100, 200]);
  assert.equal(w.revision, 5);
  assert.equal(w.cells.length, 4);
  assert.equal(w.cells[3], 8);
});

test('nie ma dwóch żądań okna naraz', async () => {
  let inFlight = 0, peak = 0;
  const c = new BlocksClient({fetch: async () => {
    peak = Math.max(peak, ++inFlight);
    await new Promise(r => setTimeout(r, 5));
    inFlight--;
    return okBytes([0], {'x-grid-origin': '0,0', 'x-grid-revision': '1'});
  }});
  await Promise.all([c.window(0, 0, 7, 0), c.window(0, 0, 7, 0), c.window(0, 0, 7, 0)]);
  assert.equal(peak, 1, 'a second window request went out while the first was still running');
});

test('podgląd odświeża się po zmianie kratki albo po czasie', () => {
  const c = new BlocksClient({minIntervalMS: 500});
  assert.equal(c.shouldRefresh({x: 10, y: 10, z: 7}, 0), true, 'pierwszy odczyt zawsze odświeża');
  assert.equal(c.shouldRefresh({x: 10, y: 10, z: 7}, 100), false, 'ta sama kratka zaraz po odczycie');
  assert.equal(c.shouldRefresh({x: 11, y: 10, z: 7}, 150), true, 'zmiana kratki');
  assert.equal(c.shouldRefresh({x: 11, y: 10, z: 7}, 700), true, 'upłynął pełny odstęp');
});

test('usunięcie blokady zwraca wynik serwera', async () => {
  const c = new BlocksClient({fetch: async () => okJSON({cleared: true})});
  assert.equal(await c.remove(1, 2, 7), true);
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `node --test blocks_test.cjs`
Expected: FAIL — `Cannot find module './web/blocks.js'`.

- [ ] **Step 3: Write the implementation**

Utwórz `web/blocks.js`:

```js
// Panel-side client of the learned-blockage store. Owns nothing but the
// transport: what to report and when is decided by the executor and the panel
// loop, exactly as with the input driver.
class BlocksClient {
  constructor(options = {}) {
    this.fetch = options.fetch ?? ((...a) => globalThis.fetch(...a));
    // The preview is for a human's eyes; ten times a second buys nothing and
    // would compete with the tracking loop for the connection.
    this.minIntervalMS = options.minIntervalMS ?? 500;
    this.windowInFlight = false;
    this.lastTile = null;
    this.lastAt = null;
  }
  // A failed report is not worth retrying: the same failure will be reported
  // again on the next attempt at the same tile, and a queue of stale
  // observations would teach the map about a situation long gone.
  async report(observation) {
    try {
      const r = await this.fetch('/api/blocks/observe', {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(observation),
      });
      if (!r.ok) return null;
      return await r.json();
    } catch { return null; }
  }
  async remove(x, y, z) {
    try {
      const r = await this.fetch('/api/blocks', {
        method: 'DELETE', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({x, y, z}),
      });
      if (!r.ok) return false;
      return !!(await r.json()).cleared;
    } catch { return false; }
  }
  // shouldRefresh answers whether the preview is worth re-fetching now: the
  // character moved to another tile, or the interval elapsed anyway (blocks
  // can appear without the character moving at all).
  shouldRefresh(position, now) {
    if (!position) return false;
    const tile = `${position.x},${position.y},${position.z}`;
    if (this.lastTile !== tile) return true;
    return this.lastAt === null || now - this.lastAt >= this.minIntervalMS;
  }
  async window(x, y, z, r) {
    if (this.windowInFlight) return null;
    this.windowInFlight = true;
    try {
      const res = await this.fetch(`/api/grid?x=${x}&y=${y}&z=${z}&r=${r}`);
      if (!res.ok) return null;
      const origin = (res.headers.get('X-Grid-Origin') ?? '0,0').split(',').map(Number);
      const revision = Number(res.headers.get('X-Grid-Revision') ?? 0);
      const cells = new Uint8Array(await res.arrayBuffer());
      this.lastTile = `${x},${y},${z}`;
      this.lastAt = performance.now();
      return {origin, revision, cells};
    } catch {
      return null;
    } finally {
      this.windowInFlight = false;
    }
  }
}

globalThis.BlocksClient = BlocksClient;
if (typeof module !== 'undefined') module.exports = {BlocksClient};
```

`shouldRefresh` w teście działa na czasie podanym z zewnątrz, a `window()`
stempluje `lastAt` z `performance.now()`. Żeby test był deterministyczny,
dopisz w konstruktorze `this.now = options.now ?? (() => performance.now());`
i użyj `this.now()` w `window()`; w teście `shouldRefresh` czas i tak jest
argumentem.

- [ ] **Step 4: Run the tests**

Run: `node --test blocks_test.cjs`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/blocks.js blocks_test.cjs
git commit -m "Klient panelu do zgłaszania blokad i pobierania okna podglądu

Nieudane zgłoszenie nie jest ponawiane: ta sama przeszkoda zgłosi się sama
przy kolejnej próbie, a kolejka zaległych obserwacji uczyłaby mapę o sytuacji
dawno nieaktualnej. Okno podglądu nigdy nie ma dwóch żądań naraz.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Spięcie w panelu i podgląd na żywo

**Files:**
- Modify: `web/app.js`
- Modify: `web/index.html`
- Modify: `web/tracking.css`
- Modify: `ui_test.cjs`

**Interfaces:**
- Consumes: `BlocksClient` (Task 10), `StepExecutor.takeObservation()` (Tasks 8–9), `/api/grid` (Task 7).

- [ ] **Step 1: Write the failing test**

Dopisz do `ui_test.cjs` (plik ładuje `web/app.js` w atrapie DOM — trzymaj się
wzorca istniejących testów w tym pliku):

```js
test('nieudany krok jest zgłaszany do magazynu blokad', async () => {
  const panel = loadPanel(); // istniejący helper z tego pliku
  const reported = [];
  panel.blocksClient.report = async (obs) => { reported.push(obs); return {result: 'temp', reason: ''}; };

  panel.executor.observation = {from: {x: 100, y: 100, z: 7}, to: {x: 100, y: 99, z: 7},
    outcome: 'no_motion', still_frames: 3, last_frame_age_ms: 120};
  await panel.pumpBlocks();

  assert.equal(reported.length, 1);
  assert.equal(reported[0].outcome, 'no_motion');
});

test('podgląd rysuje kratkę nieprzechodnią innym kolorem niż nauczoną blokadę', () => {
  const panel = loadPanel();
  const cells = new Uint8Array([1, 4, 8, 2]); // ściana, temp, perm, brak danych
  const pixels = panel.gridPixels({origin: [0, 0], revision: 1, cells}, 1);
  const colour = (i) => pixels.slice(i * 4, i * 4 + 4).join(',');
  assert.notEqual(colour(0), colour(1), 'ściana i blokada tymczasowa mają ten sam kolor');
  assert.notEqual(colour(1), colour(2), 'blokada tymczasowa i trwała mają ten sam kolor');
  assert.notEqual(colour(0), colour(3), 'ściana i brak danych mają ten sam kolor');
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test ui_test.cjs`
Expected: FAIL — `panel.blocksClient is undefined`.

- [ ] **Step 3: Write the implementation**

W `web/app.js`, obok `const executor = new StepExecutor();`:

```js
const blocksClient = new BlocksClient();
// Preview radius. 32 gives a 65x65 window: wide enough to show the room the
// character is in, small enough to stay a single 4 kB response.
const GRID_RADIUS = 32;
let gridWindow = null;
```

Dopisz funkcję pompującą obserwacje i podgląd, wołaną z `updateRoute` po
`followStep`:

```js
// pumpBlocks ships whatever the executor learned and refreshes the preview.
// It never blocks the tracking loop: both calls are fire-and-forget and at
// most one preview request is in flight at a time.
async function pumpBlocks(position, now) {
  const obs = executor.takeObservation();
  if (obs) {
    const decision = await blocksClient.report(obs);
    if (decision) {
      // The panel shows the server's verdict verbatim, so a refused
      // observation is visible rather than looking like a lost request.
      $('blocks-status').textContent = `${obs.to.x}, ${obs.to.y}: ${decision.reason}`;
      // A learned block changes the map, so the cached route is stale.
      if (decision.result !== 'ignored') follower?.dropPath();
    }
  }
  if (!$('grid-preview-on').checked || !position) return;
  if (!blocksClient.shouldRefresh(position, now)) return;
  const w = await blocksClient.window(position.x, position.y, position.z, GRID_RADIUS);
  if (w) { gridWindow = w; drawGrid(w); }
}
```

`gridPixels` buduje bufor kolorów — wydzielone z rysowania, żeby dało się je
przetestować bez canvasu:

```js
// Colours are deliberately far apart: this preview exists to answer "why did
// the bot go around there", and a subtle difference answers nothing.
const GRID_COLOURS = {
  free:    [40, 70, 40, 255],
  wall:    [150, 40, 40, 255],
  missing: [40, 40, 45, 255],
  temp:    [220, 170, 40, 255],
  perm:    [230, 80, 230, 255],
};
function gridPixels(w, side) {
  const out = new Uint8ClampedArray(side * side * 4);
  for (let i = 0; i < w.cells.length && i < side * side; i++) {
    const c = w.cells[i];
    let colour = GRID_COLOURS.free;
    if (c & 2) colour = GRID_COLOURS.missing;
    else if (c & 1) colour = GRID_COLOURS.wall;
    // A learned block wins over the terrain underneath: that is the whole
    // point of showing it.
    if (c & 4) colour = GRID_COLOURS.temp;
    if (c & 8) colour = GRID_COLOURS.perm;
    out.set(colour, i * 4);
  }
  return out;
}
function drawGrid(w) {
  const canvas = $('grid-canvas');
  const side = 2 * GRID_RADIUS + 1;
  canvas.width = canvas.height = side;
  const c = canvas.getContext('2d');
  c.putImageData(new ImageData(gridPixels(w, side), side, side), 0, 0);
}
```

Kliknięcie w podgląd usuwa blokadę:

```js
$('grid-canvas').addEventListener('click', async (e) => {
  if (!gridWindow) return;
  const rect = e.target.getBoundingClientRect();
  const side = 2 * GRID_RADIUS + 1;
  const col = Math.floor((e.clientX - rect.left) / rect.width * side);
  const row = Math.floor((e.clientY - rect.top) / rect.height * side);
  const x = gridWindow.origin[0] + col, y = gridWindow.origin[1] + row;
  const cell = gridWindow.cells[row * side + col] ?? 0;
  if (!(cell & 12)) { $('blocks-status').textContent = `${x}, ${y}: brak nauczonej blokady.`; return; }
  const ok = await blocksClient.remove(x, y, lastPosition?.z ?? 7);
  $('blocks-status').textContent = ok ? `${x}, ${y}: blokada usunięta.` : `${x}, ${y}: nie udało się usunąć.`;
  follower?.dropPath();
});
```

W `updateRoute`, po `const out = followStep(position, capturedAt, now);`:

```js
  pumpBlocks(position, now);
```

W `web/index.html` dopisz sekcję (numeracja po istniejącej „5. Sterowanie"):

```html
<section>
  <h2>6. Podgląd przechodności</h2>
  <label><input type="checkbox" id="grid-preview-on"> Pokazuj mapę przechodności</label>
  <canvas id="grid-canvas" class="grid-canvas" width="65" height="65"></canvas>
  <p class="legend">
    <span class="swatch swatch-free"></span> przejdziesz
    <span class="swatch swatch-wall"></span> teren nieprzechodni
    <span class="swatch swatch-missing"></span> brak danych mapy
    <span class="swatch swatch-temp"></span> blokada nauczona (wygasa)
    <span class="swatch swatch-perm"></span> blokada nauczona (trwała)
  </p>
  <p id="blocks-status">—</p>
  <p class="hint">Kliknij kratkę z nauczoną blokadą, żeby ją usunąć.</p>
</section>
```

W `web/tracking.css`:

```css
/* The window is 65 tiles wide; scaling it up must not blur the tiles, or a
   single learned block becomes impossible to point at. */
.grid-canvas { width: 320px; height: 320px; image-rendering: pixelated; border: 1px solid #333; }
.swatch { display: inline-block; width: .8em; height: .8em; margin: 0 .2em 0 .8em; vertical-align: middle; }
.swatch-free { background: rgb(40, 70, 40); }
.swatch-wall { background: rgb(150, 40, 40); }
.swatch-missing { background: rgb(40, 40, 45); }
.swatch-temp { background: rgb(220, 170, 40); }
.swatch-perm { background: rgb(230, 80, 230); }
```

- [ ] **Step 4: Run all the JS tests**

Run: `node --test ui_test.cjs tracker_test.cjs route_test.cjs recorder_test.cjs follower_test.cjs executor_test.cjs input_client_test.cjs blocks_test.cjs`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/app.js web/index.html web/tracking.css ui_test.cjs
git commit -m "Podgląd przechodności na żywo i zgłaszanie blokad z panelu

Panel wysyła to, czego executor się nauczył, i pokazuje werdykt serwera
dosłownie - odrzucona obserwacja jest widoczna, a nie wygląda jak zgubione
żądanie. Podgląd rysuje okno 65x65 wokół postaci: teren nieprzechodni, brak
danych mapy i obie nauczone blokady mają wyraźnie różne kolory, bo ten podgląd
istnieje po to, żeby odpowiedzieć na pytanie \"czemu bot tamtędy nie poszedł\".

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Napisz sekcję**

Dopisz po sekcji **Sterowanie** sekcję **Nauczone blokady**. Szkic do
rozwinięcia w stylu reszty README (akapity, nie wypunktowania):

> ## Nauczone blokady
>
> Trasy liczy A* na kaflach `Minimap_WaypointCost`, a te znają teren i ściany
> budynków — nie znają mebli. Lada w sklepie, stół i zamknięte drzwi stoją na
> kratkach opisanych jako w pełni przechodnie, więc trasa prowadzi wprost przez
> nie. Wykonawca uczy się takich miejsc sam, z nieudanych kroków.
>
> Krok prosty, po którym trzy kolejne świeże klatki pokazują postać wciąż na
> kratce startowej, tworzy **blokadę tymczasową** na 60 sekund. Drugi taki
> epizod — liczony dopiero po wygaśnięciu pierwszego, nie z ponowionej próby —
> awansuje kratkę na **blokadę trwałą**, zapisywaną w pliku wskazanym przez
> `-blocks` (domyślnie `blocks.json` w katalogu uruchomienia). Katalog map nie
> jest tu domyślną lokalizacją celowo: to pobrana paczka danych i nasze wpisy
> nie powinny się z nią mieszać.
>
> Blokada tymczasowa **nie jest ścianą** — podnosi koszt kratki o 500. Gracz
> stojący w jednokratkowych drzwiach nie odcina wtedy trasy: jeśli objazd
> istnieje, zostanie wybrany, a jeśli nie — bot poczeka i spróbuje ponownie,
> zamiast ogłosić brak drogi. Dopiero blokada trwała czyni kratkę
> nieprzechodnią.
>
> Wykonawca nie uczy się ze wszystkiego. Nieudany **skos** blokuje samo
> przejście na 20 sekund, nigdy kratkę: skos zawodzi także na zamkniętym rogu,
> gdzie kratka docelowa bywa pusta, a nauka z takich prób stopniowo wycinałaby
> z mapy przechodnie pokoje. Zmiana piętra nie uczy niczego — inaczej schody
> dostałyby blokadę, bo wejście na nie zmienia Z i wygląda jak nieudany krok.
> Odmowa drivera, błąd połączenia i utrata pozycji też nie uczą: to problemy
> sterowania, nie mapy. Wejście na kratkę do 600 ms po timeoucie odwołuje
> naukę — to był lag albo paraliż.
>
> Blokada jest odwoływalna. Postać stojąca na kratce kasuje jej wpis, także
> trwały: obecność jest mocniejszym dowodem niż jakakolwiek hipoteza.
>
> ### Podgląd przechodności
>
> Sekcja **6. Podgląd przechodności** rysuje okno 65×65 kratek wokół postaci.
> Ciemna zieleń to teren przejezdny, czerwień — nieprzechodni w danych mapy,
> grafit — brak danych (nie ma kafla PNG; to nie to samo co ściana), żółć —
> blokada nauczona tymczasowa, fiolet — trwała. Kliknięcie kratki z nauczoną
> blokadą usuwa ją. Okno odświeża się po zmianie kratki postaci albo co pół
> sekundy i nigdy nie ma dwóch żądań naraz, więc nie odbiera przepustowości
> pętli śledzenia.
>
> Podgląd jest też narzędziem diagnostycznym: lada, przez którą postać nie
> przejdzie, a która świeci na zielono, jest dowodem, że dane mapy jej nie
> znają.

Zaktualizuj też listę testów w sekcji **Testy** o `blocks_test.cjs`.

- [ ] **Step 2: Sprawdź, czy README nie kłamie**

Przejrzyj sekcje **Jak działa** i **HTTP API** — opis `/api/path` musi
wspomnieć nakładkę i pole `overlay_revision`, a lista endpointów `/api/blocks`,
`/api/blocks/observe` i `/api/grid`.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "Opisz warstwę nauczonych blokad w README

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Test na żywym kliencie

`go test` nie obejmie tego, czy bot faktycznie omija ladę. Po Tasku 12,
z `-input system`, w tej kolejności:

1. Stań przed ladą w sklepie, wyznacz waypoint za nią. Bot próbuje wejść,
   po dwóch próbach panel pokazuje „Pierwszy epizod; blokada tymczasowa",
   a trasa zaczyna prowadzić dookoła.
2. Podgląd przechodności pokazuje tę kratkę na żółto.
3. Odczekaj ponad minutę, powtórz. Panel pokazuje „Drugi niezależny epizod;
   blokada trwała", kratka robi się fioletowa, a `blocks.json` zawiera wpis.
4. Zrestartuj program. Kratka nadal jest fioletowa, trasa nadal ją omija.
5. Kliknij kratkę w podglądzie — blokada znika, trasa znów przez nią prowadzi.
6. Wejdź na kratkę ręcznie, gdy jest zablokowana — wpis znika sam.
7. Wejdź na schody z włączonym chodzeniem. Schody **nie** mogą dostać blokady.
8. Poproś kogoś, żeby stanął w wąskim przejściu na trasie. Bot ma czekać i
   ponowić, nie ogłosić braku trasy.

Zapisz wynik każdego punktu w opisie commita — tak jak przy sterowaniu.
