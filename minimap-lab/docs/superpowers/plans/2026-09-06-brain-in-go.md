# Mózg bota w Go — plan wdrożenia

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Przenieść całą logikę decyzyjną bota z przeglądarki do Go, zostawiając panelowi wyłącznie przechwytywanie obrazu i wyświetlanie stanu.

**Architecture:** Go dostaje pętlę bota jako goroutine będącą jedynym właścicielem stanu decyzyjnego. Przeglądarka wysyła surowe piksele przez `POST /api/frame` z binarnym ciałem i dostaje w odpowiedzi snapshot stanu. Protokół intencji (token sesji przy każdej intencji, numery sekwencyjne, heartbeat) znika, bo decydent i emiter klawiszy trafiają do jednego procesu.

**Tech Stack:** Go 1.22 (stdlib + `github.com/ebitengine/purego` — jedyna zależność), waniliowy JavaScript w `web/` bez frameworków i bez bundlera, testy `go test ./...` oraz `node --test webtests/*.cjs`.

**Spec:** `docs/superpowers/specs/2026-09-06-brain-in-go-design.md`

## Global Constraints

- **Zero nowych zależności.** `go.mod` ma pozostać przy `github.com/ebitengine/purego v0.9.0`. Żadnego WebSocketu, żadnego routera, żadnej biblioteki asercji.
- **Testy uruchamiane lokalnie:** `go test ./...` i `node --test webtests/*.cjs`. To repozytorium nie ma Dockera ani docker-compose — globalna zasada o testach w kontenerze tu nie obowiązuje.
- **Komentarze w kodzie po angielsku, teksty widoczne dla użytkownika po polsku.** Tak jest w całym repozytorium; komunikaty błędów HTTP, statusy panelu i logi są po polsku.
- **Komentarz tłumaczy „dlaczego", nigdy „co".** Istniejący kod trzyma ten standard bardzo wysoko — patrz `internal/input/driver.go` i `web/executor.js`. Przy porcie **komentarze uzasadniające przenosimy razem z logiką**; to one niosą wiedzę o naprawionych błędach.
- **Kolizja nazw:** `locate.Result` to wynik dopasowania minimapy. Nowy typ wyniku sterowania nie może się nazywać `Result` bez kwalifikatora pakietu.
- **Zegar zawsze wstrzykiwany** jako `now func() time.Time`, wzorem `nav.NewBlockStore`. Żadnego `time.Sleep` w testach.
- **Gałąź:** cała praca na nowej gałęzi odbitej od `go`. Gałąź `go` zostaje nietknięta do końca migracji.
- **Commit po każdym zadaniu**, komunikat po polsku w trybie rozkazującym, wyjaśniający powód zmiany.

## Struktura plików

| plik | odpowiedzialność |
|---|---|
| `internal/route/route.go` | format pliku trasy: walidacja, parsowanie, serializacja |
| `internal/frame/frame.go` | parsowanie binarnego ciała `POST /api/frame` |
| `internal/brain/tracker.go` | kotwica pozycji, promień lokalnego szukania, statystyki kadencji |
| `internal/brain/recorder.go` | nagrywanie waypointów z potwierdzonych pozycji |
| `internal/brain/executor.go` | lock-step wykonawca kroków, retry, nauka blokad |
| `internal/brain/follower.go` | wybór kolejnego kroku trasy, replan, backoff |
| `internal/brain/loop.go` | orkiestracja: jedyny właściciel stanu, goroutine pętli |
| `internal/brain/state.go` | snapshot stanu oddawany panelowi |
| `internal/locate/service.go` | rdzeń lokalizacji wyjęty z `locateapi.go` |
| `internal/nav/service.go` | rdzeń planowania trasy wyjęty z `pathapi.go` |
| `frameapi.go` | handler `POST /api/frame`, `GET /api/state`, `PUT /api/config`, `PUT /api/route` |
| `web/camera.js` | wycinanie regionów i wysyłka klatek |
| `web/worker.js` | zegar pętli, odporny na dławienie karty w tle |
| `web/view.js` | rysowanie snapshotu |

---

### Task 1: Format trasy w Go

Port `web/route.js` (47 linii, 9 przypadków testowych). Najprostszy moduł, czysta funkcja — służy za rozgrzewkę i ustala konwencje portu dla wszystkich następnych zadań.

**Files:**
- Create: `internal/route/route.go`
- Test: `internal/route/route_test.go`
- Reference: `web/route.js`, `webtests/route_test.cjs`

**Interfaces:**
- Consumes: nic
- Produces:
  ```go
  package route

  const Version = 1
  const MaxWaypoints = 1000
  const MaxLabel = 64

  var Types = []string{"walk", "rope", "ladder", "stairs", "hole", "shovel"}

  type Waypoint struct {
      X     int    `json:"x"`
      Y     int    `json:"y"`
      Z     int    `json:"z"`
      Type  string `json:"type"`
      Label string `json:"label"`
  }

  type Route struct {
      Version   int        `json:"version"`
      Name      string     `json:"name"`
      Waypoints []Waypoint `json:"waypoints"`
  }

  func Parse(data []byte) (Route, error)
  func Serialize(r Route) ([]byte, error)
  ```

- [ ] **Step 1: Napisz testy odtwarzające wszystkie 9 przypadków z `webtests/route_test.cjs`**

Przenieś co do jednego, zachowując wejścia i oczekiwane skutki:

1. `a waypoint without a type walks` — brak pola `type` daje `"walk"`
2. `every documented action type is accepted` — wszystkie sześć typów przechodzi
3. `an unknown action type names the offending waypoint` — komunikat zawiera numer waypointa i listę dozwolonych
4. `coordinates outside the map are rejected` — X/Y poza 0–65535, Z poza 0–15
5. `a file from a future version is refused rather than guessed at`
6. `an empty route is valid, a thousand-and-one point route is not`
7. `labels are kept but capped` — etykieta ucinana do 64 znaków
8. `malformed input fails with a message, not a crash`
9. `a parsed route survives a round trip`

Wzór pierwszego testu — resztę pisz w tym samym stylu tabelkowym:

```go
package route

import (
	"strings"
	"testing"
)

func TestWaypointWithoutTypeWalks(t *testing.T) {
	r, err := Parse([]byte(`{"version":1,"waypoints":[{"x":100,"y":200,"z":7}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := r.Waypoints[0].Type; got != "walk" {
		t.Errorf("typ = %q, oczekiwano walk", got)
	}
}

func TestUnknownTypeNamesTheOffendingWaypoint(t *testing.T) {
	_, err := Parse([]byte(`{"version":1,"waypoints":[{"x":1,"y":1,"z":7},{"x":2,"y":2,"z":7,"type":"fly"}]}`))
	if err == nil {
		t.Fatal("oczekiwano błędu")
	}
	// The message must point at the waypoint the user has to fix; "unknown
	// type" alone leaves them hunting through a thousand-point file.
	if !strings.Contains(err.Error(), "Waypoint 2") || !strings.Contains(err.Error(), "fly") {
		t.Errorf("komunikat nie wskazuje winowajcy: %v", err)
	}
}

func TestRouteSurvivesRoundTrip(t *testing.T) {
	in := []byte(`{"version":1,"name":"test","waypoints":[{"x":1,"y":2,"z":7,"type":"rope","label":"tu"}]}`)
	first, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	data, err := Serialize(first)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	second, err := Parse(data)
	if err != nil {
		t.Fatalf("ponowne Parse: %v", err)
	}
	if len(second.Waypoints) != 1 || second.Waypoints[0] != first.Waypoints[0] {
		t.Errorf("round trip zmienił trasę: %+v vs %+v", second.Waypoints, first.Waypoints)
	}
}
```

- [ ] **Step 2: Uruchom testy i potwierdź, że nie kompilują się z braku pakietu**

Run: `go test ./internal/route/`
Expected: FAIL — `undefined: Parse`

- [ ] **Step 3: Napisz `internal/route/route.go`**

Komunikaty błędów przenieś **dosłownie** z `web/route.js` — użytkownik zna te teksty:

```go
// Package route is the route file format, shared by the panel and the brain. A
// route is a list of waypoints; the type says what to do there, and every
// unknown field is dropped so a hand-edited file cannot smuggle anything in.
package route

import (
	"encoding/json"
	"fmt"
)

func isTile(v int) bool { return v >= 0 && v <= 65535 }

func validType(t string) bool {
	for _, known := range Types {
		if known == t {
			return true
		}
	}
	return false
}

// rawWaypoint keeps every field a pointer so a missing one is distinguishable
// from a zero, exactly as the panel's parser did: an absent type defaults to
// walk, while an absent x is an error rather than tile zero.
type rawWaypoint struct {
	X     *int    `json:"x"`
	Y     *int    `json:"y"`
	Z     *int    `json:"z"`
	Type  *string `json:"type"`
	Label *string `json:"label"`
}

type rawRoute struct {
	Version   *int          `json:"version"`
	Name      *string       `json:"name"`
	Waypoints []rawWaypoint `json:"waypoints"`
}

func parseWaypoint(raw rawWaypoint, index int) (Waypoint, error) {
	at := fmt.Sprintf("Waypoint %d", index+1)
	if raw.X == nil || raw.Y == nil || !isTile(*raw.X) || !isTile(*raw.Y) {
		return Waypoint{}, fmt.Errorf("%s: X i Y muszą być liczbami całkowitymi 0–65535.", at)
	}
	if raw.Z == nil || *raw.Z < 0 || *raw.Z > 15 {
		return Waypoint{}, fmt.Errorf("%s: Z musi być liczbą całkowitą 0–15.", at)
	}
	kind := "walk"
	if raw.Type != nil {
		kind = *raw.Type
	}
	if !validType(kind) {
		return Waypoint{}, fmt.Errorf("%s: nieznany typ „%s”. Dozwolone: %s.", at, kind, joinTypes())
	}
	label := ""
	if raw.Label != nil {
		label = truncate(*raw.Label, MaxLabel)
	}
	return Waypoint{X: *raw.X, Y: *raw.Y, Z: *raw.Z, Type: kind, Label: label}, nil
}
```

Uzupełnij `Parse`, `Serialize`, `joinTypes` i `truncate`. `truncate` musi ciąć po **runach**, nie po bajtach — polska etykieta z ogonkami ucięta w połowie znaku dałaby niepoprawny UTF-8, czego JS-owy `slice` nie robił.

- [ ] **Step 4: Uruchom testy**

Run: `go test ./internal/route/ -v`
Expected: PASS, 9 testów

- [ ] **Step 5: Commit**

```bash
git add internal/route/
git commit -m "Przenieś format trasy do Go, bo mózg musi go czytać sam"
```

---

### Task 2: Parser klatki

Nowy kod bez odpowiednika w JS. Format opisany w specu, sekcja „Format ciała".

**Files:**
- Create: `internal/frame/frame.go`
- Test: `internal/frame/frame_test.go`

**Interfaces:**
- Consumes: nic
- Produces:
  ```go
  package frame

  type RegionID uint8

  const (
      RegionMinimap RegionID = 1
      RegionHP      RegionID = 2
      RegionMana    RegionID = 3
  )

  const (
      MaxBody       = 4 << 20
      HeaderSize    = 36
      RegionHeader  = 12
      MaxRegions    = 8
      MaxRegionSide = 1024
  )

  type Region struct {
      ID   RegionID
      W, H int
      Pix  []byte // RGBA8888, non-premultiplied; len == W*H*4
  }

  type Frame struct {
      Session     uint64
      Seq         uint64
      VideoTimeUS uint64
      AgeMS       uint32
      Regions     map[RegionID]Region
  }

  func Parse(data []byte) (Frame, error)
  func (f Frame) Image(id RegionID) (*image.NRGBA, bool)
  ```

- [ ] **Step 1: Napisz testy**

```go
package frame

import (
	"encoding/binary"
	"testing"
)

// build assembles a body the way the panel does, so tests exercise the real
// layout rather than a parser-shaped fiction.
func build(session, seq, videoUS uint64, ageMS uint32, regions []Region) []byte {
	body := make([]byte, HeaderSize+RegionHeader*len(regions))
	copy(body[0:4], "MLF1")
	body[4] = 1
	body[5] = byte(len(regions))
	binary.LittleEndian.PutUint64(body[8:], session)
	binary.LittleEndian.PutUint64(body[16:], seq)
	binary.LittleEndian.PutUint64(body[24:], videoUS)
	binary.LittleEndian.PutUint32(body[32:], ageMS)
	for i, r := range regions {
		h := body[HeaderSize+RegionHeader*i:]
		h[0] = byte(r.ID)
		h[1] = 0
		binary.LittleEndian.PutUint16(h[4:], uint16(r.W))
		binary.LittleEndian.PutUint16(h[6:], uint16(r.H))
		binary.LittleEndian.PutUint32(h[8:], uint32(r.W*r.H*4))
	}
	for _, r := range regions {
		body = append(body, r.Pix...)
	}
	return body
}

func pixels(w, h int) []byte { return make([]byte, w*h*4) }

func TestParseReadsHeaderAndRegions(t *testing.T) {
	body := build(7, 42, 1_500_000, 30, []Region{{ID: RegionMinimap, W: 2, H: 2, Pix: pixels(2, 2)}})
	f, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Session != 7 || f.Seq != 42 || f.VideoTimeUS != 1_500_000 || f.AgeMS != 30 {
		t.Errorf("nagłówek: %+v", f)
	}
	if r, ok := f.Regions[RegionMinimap]; !ok || r.W != 2 || len(r.Pix) != 16 {
		t.Errorf("region minimapy: %+v ok=%v", r, ok)
	}
}

func TestParseRefusesWrongMagic(t *testing.T) {
	body := build(1, 1, 0, 0, nil)
	body[0] = 'X'
	if _, err := Parse(body); err == nil {
		t.Fatal("oczekiwano odmowy dla obcego magicu")
	}
}

// A length that does not match w*h*4 is the shape a truncated or mismatched
// upload takes. Trusting it would hand the matcher a buffer it reads past.
func TestParseRefusesRegionLengthThatDoesNotMatchDimensions(t *testing.T) {
	body := build(1, 1, 0, 0, []Region{{ID: RegionMinimap, W: 4, H: 4, Pix: pixels(4, 4)}})
	binary.LittleEndian.PutUint32(body[HeaderSize+8:], 8)
	if _, err := Parse(body); err == nil {
		t.Fatal("oczekiwano odmowy dla niezgodnej długości regionu")
	}
}

func TestParseRefusesTruncatedPayload(t *testing.T) {
	body := build(1, 1, 0, 0, []Region{{ID: RegionMinimap, W: 4, H: 4, Pix: pixels(4, 4)}})
	if _, err := Parse(body[:len(body)-4]); err == nil {
		t.Fatal("oczekiwano odmowy dla uciętego ciała")
	}
}

func TestParseRefusesDuplicateRegion(t *testing.T) {
	body := build(1, 1, 0, 0, []Region{
		{ID: RegionMinimap, W: 1, H: 1, Pix: pixels(1, 1)},
		{ID: RegionMinimap, W: 1, H: 1, Pix: pixels(1, 1)},
	})
	if _, err := Parse(body); err == nil {
		t.Fatal("oczekiwano odmowy dla powtórzonego regionu")
	}
}

func TestParseRefusesUnknownRegionID(t *testing.T) {
	body := build(1, 1, 0, 0, []Region{{ID: RegionID(99), W: 1, H: 1, Pix: pixels(1, 1)}})
	if _, err := Parse(body); err == nil {
		t.Fatal("oczekiwano odmowy dla nieznanego regionu")
	}
}

func TestParseRefusesNonZeroFlags(t *testing.T) {
	body := build(1, 1, 0, 0, nil)
	body[6] = 1
	if _, err := Parse(body); err == nil {
		t.Fatal("oczekiwano odmowy dla niezerowych flag")
	}
}

func TestImageWrapsPixelsWithoutCopying(t *testing.T) {
	pix := pixels(2, 2)
	pix[0] = 200
	f, err := Parse(build(1, 1, 0, 0, []Region{{ID: RegionMinimap, W: 2, H: 2, Pix: pix}}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	im, ok := f.Image(RegionMinimap)
	if !ok || im.Bounds().Dx() != 2 || im.Pix[0] != 200 {
		t.Errorf("obraz: ok=%v bounds=%v pix0=%d", ok, im.Bounds(), im.Pix[0])
	}
}
```

- [ ] **Step 2: Uruchom testy**

Run: `go test ./internal/frame/`
Expected: FAIL — `undefined: Parse`

- [ ] **Step 3: Zaimplementuj `internal/frame/frame.go`**

Wymagania, których testy pilnują, plus te, których nie da się sprawdzić testem jednostkowym:

- magic `MLF1`, wersja `1`, flagi muszą być zerowe (pole zarezerwowane — przyjmowanie niezerowych flag dziś oznacza, że jutro nie da się ich użyć),
- odmowa dla nieznanego `RegionID`: panel i serwer jadą w jednej binarce, więc nieznany region to niezgodność wersji, nie rozszerzenie,
- odmowa dla powtórzonego regionu, boku poza 1–`MaxRegionSide`, `len` niezgodnego z `w*h*4`, sumy ładunków przekraczającej ciało,
- `Image` zwraca `*image.NRGBA` **owijający** bufor bez kopiowania — dane z `getImageData` są nieprzemnożone przez alfę, czyli dokładnie NRGBA. Kopia 46 kB przy każdej klatce to darmowa presja na GC.

- [ ] **Step 4: Uruchom testy**

Run: `go test ./internal/frame/ -v`
Expected: PASS, 8 testów

- [ ] **Step 5: Commit**

```bash
git add internal/frame/
git commit -m "Dodaj parser binarnej klatki, bo panel przestaje wysyłać PNG-i"
```

---

### Task 3: Tracker

Port `web/tracker.js`. **Uwaga: przenoszą się 2 z 3 przypadków testowych.** Trzeci, `cadence subtracts the request duration from each sampling period`, dotyczy `minimapNextDelay` — a to jest tempo kamery, nie decyzja bota. Zostaje w panelu i w `webtests/`.

**Files:**
- Create: `internal/brain/tracker.go`
- Test: `internal/brain/tracker_test.go`
- Reference: `web/tracker.js`, `webtests/tracker_test.cjs`

**Interfaces:**
- Consumes: `locate.Result`, `mapdata.Position`
- Produces:
  ```go
  package brain

  // Hint is the local search window the next match should use.
  type Hint struct {
      Near   mapdata.Position
      Radius int
  }

  type Stats struct {
      Hz          float64
      Success     float64
      AgeMS       int
      HasAge      bool
      RoundTripMS float64
      MatchMS     float64
      HasReadings bool
  }

  type Tracker struct{ /* unexported */ }

  func NewTracker() *Tracker
  func (t *Tracker) Reset()
  func (t *Tracker) Hint(now time.Time, floor, zoom, speed int) (Hint, bool)
  func (t *Tracker) Observe(r locate.Result, capturedAt, completedAt time.Time, roundTrip time.Duration)
  func (t *Tracker) Stats(now time.Time) Stats
  func (t *Tracker) Anchor() (mapdata.Position, time.Time, bool)
  ```

- [ ] **Step 1: Napisz testy**

Czas w JS to milisekundy od startu dokumentu. W Go użyj bazy i przesunięć — `at(ms)` niżej odtwarza tamte liczby jeden do jednego.

```go
package brain

import (
	"testing"
	"time"

	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
)

var base = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return base.Add(time.Duration(ms) * time.Millisecond) }

func success() locate.Result {
	return locate.Result{Found: true, Position: &mapdata.Position{X: 32958, Y: 32077, Z: 7},
		Zoom: 1, Mode: "local", MatchMS: 3}
}

// The radius must grow with how long ago the anchor was captured: the
// character kept walking while the match ran, so the window has to cover
// wherever it could have got to. Calibration that no longer matches the
// anchor invalidates it outright - a different zoom means a different picture.
func TestRadiusGrowsWithAgeAndRejectsMismatchedCalibration(t *testing.T) {
	tr := NewTracker()
	tr.Observe(success(), at(100), at(105), 5*time.Millisecond)

	for _, c := range []struct {
		now, floor, zoom, want int
	}{
		{200, 7, 1, 5},
		{1100, 7, 1, 22},
		{5000, 7, 1, 64},
	} {
		h, ok := tr.Hint(at(c.now), c.floor, c.zoom, 20)
		if !ok || h.Radius != c.want {
			t.Errorf("Hint(%d, z=%d, zoom=%d) = %d, ok=%v; oczekiwano %d", c.now, c.floor, c.zoom, h.Radius, ok, c.want)
		}
	}
	if h, ok := tr.Hint(at(200), 8, 1, 20); !ok || h.Near.Z != 7 {
		t.Errorf("sąsiednie piętro powinno zachować kotwicę na Z=7, dostano %+v ok=%v", h, ok)
	}
	for _, c := range []struct {
		name              string
		now, floor, zoom  int
	}{
		{"piętro dalej niż o jedno", 200, 9, 1},
		{"inna skala", 200, 7, 2},
		{"kotwica starsza niż 30 s bez zasiewu", 40000, 7, 1},
	} {
		if _, ok := tr.Hint(at(c.now), c.floor, c.zoom, 20); ok {
			t.Errorf("%s: oczekiwano braku podpowiedzi", c.name)
		}
	}
}

// A global acquisition may be minutes old by the time it lands, and its age
// must survive: it is what tells the next match how wide to look. The local
// confirmation that follows then narrows the window back down.
func TestGlobalAcquisitionKeepsItsAgeThenNarrows(t *testing.T) {
	tr := NewTracker()
	global := success()
	global.Mode = "global"
	tr.Observe(global, at(0), at(13000), 13*time.Second)

	if h, ok := tr.Hint(at(13000), 7, 1, 20); !ok || h.Radius != 64 {
		t.Errorf("promień po zasiewie = %d ok=%v, oczekiwano 64", h.Radius, ok)
	}
	if s := tr.Stats(at(13000)); !s.HasAge || s.AgeMS != 13000 {
		t.Errorf("wiek kotwicy = %d has=%v, oczekiwano 13000", s.AgeMS, s.HasAge)
	}
	tr.Observe(success(), at(13100), at(13105), 5*time.Millisecond)
	if h, ok := tr.Hint(at(13200), 7, 1, 20); !ok || h.Radius != 5 {
		t.Errorf("promień po potwierdzeniu = %d ok=%v, oczekiwano 5", h.Radius, ok)
	}
}
```

- [ ] **Step 2: Uruchom testy**

Run: `go test ./internal/brain/ -run Tracker`
Expected: FAIL — `undefined: NewTracker`

- [ ] **Step 3: Zaimplementuj `internal/brain/tracker.go`**

Reguły przenoszone dosłownie z `web/tracker.js`:

- podpowiedź odpada, gdy: brak kotwicy, 3 lub więcej chybień, `|kotwica.Z − piętro| > 1`, inny zoom, albo wiek > 30 s **i** kotwica nie pochodzi z wyszukiwania globalnego (`seeded`),
- `radius = min(64, max(5, ceil(wiek_s × speed) + 2 + chybienia × 5))`,
- wynik globalny czyści listę odczytów; wynik lokalny ją zasila,
- odczyty starsze niż 3 s odpadają, lista trzyma najwyżej 40 pozycji,
- trafienie ustawia kotwicę na **czas wycięcia klatki**, nie na czas odpowiedzi; chybienie zwiększa licznik, a chybienie globalne kasuje kotwicę,
- `Hz` liczone tylko wtedy, gdy ostatni odczyt jest świeższy niż 600 ms.

- [ ] **Step 4: Uruchom testy**

Run: `go test ./internal/brain/ -run Tracker -v`
Expected: PASS, 2 testy

- [ ] **Step 5: Commit**

```bash
git add internal/brain/tracker.go internal/brain/tracker_test.go
git commit -m "Przenieś śledzenie kotwicy pozycji do Go"
```

---

### Task 4: Recorder

Port `web/recorder.js` (12 przypadków). **Bramka przechodniości z `app.js` NIE trafia tutaj** — recorder zostaje czystą funkcją, a sprawdzenie, czy kratka nadaje się na waypoint, robi `loop.go` w Zadaniu 9, bo tylko ono ma dostęp do siatki kosztów i nakładki blokad.

**Files:**
- Create: `internal/brain/recorder.go`
- Test: `internal/brain/recorder_test.go`
- Reference: `web/recorder.js`, `webtests/recorder_test.cjs`

**Interfaces:**
- Consumes: `route.Waypoint`, `mapdata.Position`
- Produces:
  ```go
  const RecorderLimit = 1000

  type Recorder struct {
      Auto  bool
      Every int // tile distance between automatic waypoints
      /* unexported state */
  }

  func NewRecorder() *Recorder
  func (r *Recorder) Waypoints() []route.Waypoint
  func (r *Recorder) SetWaypoints(w []route.Waypoint)
  func (r *Recorder) Full() bool
  func (r *Recorder) AddManual(p mapdata.Position) bool
  func (r *Recorder) Observe(p mapdata.Position) int // how many waypoints were added
  func GuessTransition(from, to mapdata.Position) string
  ```

- [ ] **Step 1: Napisz testy odtwarzające wszystkie 12 przypadków**

1. `a manual waypoint records the current tile as walkable ground` → typ `walk`
2. `automatic recording starts with the tile the player stands on`
3. `automatic recording waits for the configured distance`
4. `distance counts diagonals as single tiles` → odległość Czebyszewa
5. `recording nothing while switched off, not even floor changes`
6. `a floor change records the tile before it and the tile after` → **dwa** waypointy
7. `going up without moving is guessed as a rope`
8. `going down without moving is guessed as a hole`
9. `a floor change that shifts the tile is guessed as stairs`
10. `a floor change is recorded even right after another waypoint`
11. `the recorder stops at the file format limit` → 1000
12. `manual and automatic points share one list`

Wzór dla najważniejszego z nich:

```go
// The minimap only reveals a transition once the player is already on the new
// floor, so the tile the transition was used from has to be recorded
// retroactively - otherwise the route has no idea where to stand.
func TestFloorChangeRecordsTheTileBeforeAndAfter(t *testing.T) {
	r := NewRecorder()
	r.Auto, r.Every = true, 10
	r.Observe(mapdata.Position{X: 100, Y: 100, Z: 7})
	added := r.Observe(mapdata.Position{X: 100, Y: 100, Z: 6})
	if added != 2 {
		t.Fatalf("dodano %d waypointów, oczekiwano 2", added)
	}
	got := r.Waypoints()
	last := got[len(got)-2:]
	if last[0].Z != 7 || last[0].Type != "rope" {
		t.Errorf("kratka sprzed przejścia = %+v, oczekiwano Z=7 typu rope", last[0])
	}
	if last[1].Z != 6 || last[1].Type != "walk" {
		t.Errorf("kratka po przejściu = %+v, oczekiwano Z=6 typu walk", last[1])
	}
}
```

- [ ] **Step 2: Uruchom testy**

Run: `go test ./internal/brain/ -run Recorder`
Expected: FAIL — `undefined: NewRecorder`

- [ ] **Step 3: Zaimplementuj `internal/brain/recorder.go`**

`GuessTransition`: odległość > 0 → `stairs`; w górę (`to.Z < from.Z`) → `rope`; w dół → `hole`.

- [ ] **Step 4: Uruchom testy**

Run: `go test ./internal/brain/ -run Recorder -v`
Expected: PASS, 12 testów

- [ ] **Step 5: Commit**

```bash
git add internal/brain/recorder.go internal/brain/recorder_test.go
git commit -m "Przenieś nagrywanie waypointów do Go"
```

---

### Task 5: Wykonawca — cykl życia kroku

Port pierwszej połowy `web/executor.js`: wysyłanie kroku, potwierdzanie, timeouty, ponowienia, blokada celu. Nauka blokad idzie osobno w Zadaniu 6, bo to niezależny obszar — recenzent może przyjąć jedno i odrzucić drugie.

Przenoszone przypadki: **1–22 oraz 30, 34, 35** z `webtests/executor_test.cjs`.

**Files:**
- Create: `internal/brain/executor.go`
- Test: `internal/brain/executor_test.go`
- Reference: `web/executor.js`, `webtests/executor_test.cjs`

**Interfaces:**
- Consumes: `mapdata.Position`, `route.Waypoint`, typ `Output` z Zadania 7 — **na czas tego zadania zadeklaruj `Output` w `executor.go` wg poniższej definicji i przenieś do `follower.go` w Zadaniu 7**
- Produces:
  ```go
  type Action string

  const (
      ActionDone       Action = "done"
      ActionWalk       Action = "walk"
      ActionTransition Action = "transition"
      ActionPath       Action = "path"
      ActionWait       Action = "wait"
      ActionBlocked    Action = "blocked"
  )

  // Output is what the follower wants done next.
  type Output struct {
      Action       Action
      Direction    string
      Next         [2]int
      Remaining    int
      Index        int
      Waypoint     *route.Waypoint
      NextWaypoint *route.Waypoint
      Instruction  string
      From, To     mapdata.Position
      Status       string
      Reason       string
  }

  // Intent is one thing to press right now.
  type Intent struct {
      Action    string // "walk" or "transition"
      Direction string
      Type      string
      Waypoint  int
  }

  type ExecState struct {
      Waiting      bool
      Retries      int
      Cycles       int
      Blocked      bool
      Halted       bool
      Stopped      bool
      ActionDone   bool
      AwaitingEmit bool
      StepID       uint64
  }

  type ExecutorOptions struct {
      StepTimeout     time.Duration // default 1800ms
      LateArrival     time.Duration // default 2000ms
      BlockedTTL      time.Duration // default 60s
      ActionTimeout   time.Duration // default 5s
      MaxFailedCycles int           // default 3
  }

  func NewExecutor(o ExecutorOptions) *Executor
  func (e *Executor) Reset()
  func (e *Executor) State() ExecState
  func (e *Executor) IntentFor(out *Output, now time.Time) (Intent, bool)
  func (e *Executor) Observe(p *mapdata.Position, capturedAt, now time.Time)
  func (e *Executor) Emitted(now time.Time, id uint64)
  func (e *Executor) DropPending()
  func (e *Executor) ClearActionDone()
  ```

- [ ] **Step 1: Napisz testy dla przypadków 1–22, 30, 34, 35**

Pełna lista do przeniesienia, nazwy zachowaj:

1. pierwszy krok jest wysyłany od razu
2. drugi krok nie idzie, dopóki pierwszy nie jest potwierdzony
3. klatka sprzed emisji nie jest dowodem wykonania kroku
4. klatka po emisji z docelową kratką kończy krok
5. brak ruchu przed timeoutem nie powtarza kroku
6. brak ruchu po timeoucie powtarza krok raz
7. druga porażka tego samego kroku zgłasza blokadę
8. trzy różne cele nieudane pod rząd zatrzymują wykonawcę
9. maxFailedCycles inny niż domyślny zatrzymuje wcześniej
10. blokada trzyma cel, ale inny cel ją czyści
11. inny cel po jednej porażce dostaje własną szansę, nie od razu blokadę
12. nieznana pozycja natychmiast wstrzymuje ruch
13. powrót poprawnego odczytu odblokowuje wykonawcę
14. nieoczekiwana kratka porzuca krok zamiast liczyć go jako nieudany
15. schody są pokonywane krokiem w stronę następnego waypointa
16. schody bez następnego waypointa nie dają kierunku
17. schody bez wcześniejszej obserwacji nie dają kierunku
18. schody z lądowaniem na tej samej kratce nie wysyłają pustego kierunku
19. akcja piętra czeka na zmianę Z, nie na kratkę
20. nieoczekiwana kratka podczas akcji piętra porzuca krok zamiast liczyć porażkę
21. krok bez potwierdzenia emisji jest porzucany po czasie i liczy się jako porażka
22. spóźnione potwierdzenie porzuconego kroku nie przesuwa terminu następnego
30. domyślny timeout kroku wynosi 1800 ms
34. blokada celu wygasa po swoim czasie
35. wygaśnięcie blokady daje botowi nową szansę, nie zatrzymanie

Wzór, po którym widać obsługę identyfikatorów kroków i zegara:

```go
func walkTo(x, y int) *Output {
	return &Output{Action: ActionWalk, Direction: "E", Next: [2]int{x, y},
		Waypoint: &route.Waypoint{X: x, Y: y, Z: 7, Type: "walk"}}
}

func TestSecondStepWaitsForTheFirstToBeConfirmed(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	e.Observe(&mapdata.Position{X: 100, Y: 100, Z: 7}, at(0), at(0))
	if _, ok := e.IntentFor(walkTo(101, 100), at(10)); !ok {
		t.Fatal("pierwszy krok powinien pójść od razu")
	}
	e.Emitted(at(20), e.State().StepID)
	if _, ok := e.IntentFor(walkTo(101, 100), at(30)); ok {
		t.Error("drugi krok poszedł, choć pierwszy nie został potwierdzony ruchem")
	}
}

// A step whose key never left the driver is not evidence about anything, but
// waiting for it forever would freeze the executor in a state that looks
// exactly like a legitimate wait.
func TestStepNeverConfirmedAsEmittedIsDroppedAfterAGracePeriod(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	e.Observe(&mapdata.Position{X: 100, Y: 100, Z: 7}, at(0), at(0))
	e.IntentFor(walkTo(101, 100), at(0))
	if _, ok := e.IntentFor(walkTo(101, 100), at(3599)); ok {
		t.Error("krok porzucony przed upływem podwójnego timeoutu")
	}
	if _, ok := e.IntentFor(walkTo(101, 100), at(3601)); !ok {
		t.Error("krok nie został porzucony po podwójnym timeoucie")
	}
	if e.State().Retries != 1 {
		t.Errorf("retries = %d, oczekiwano 1 — brak emisji to też porażka", e.State().Retries)
	}
}
```

- [ ] **Step 2: Uruchom testy**

Run: `go test ./internal/brain/ -run Executor`
Expected: FAIL — `undefined: NewExecutor`

- [ ] **Step 3: Zaimplementuj cykl życia kroku**

Zachowaj **wszystkie** komentarze uzasadniające z `web/executor.js`; to one tłumaczą, dlaczego liczby są takie, a nie inne:

- `StepTimeout` 1800 ms, nie 1200: krok w grze skaluje się z prędkością postaci i kosztem gruntu, a krok po błocie albo pod paraliżem trwa grubo ponad sekundę. Krótszy timeout zamieniałby zwykły wolny ruch w nauczone blokady.
- krok bez potwierdzenia emisji jest porzucany po **podwójnym** timeoucie i liczy się jako porażka,
- druga porażka **tego samego** celu ustawia blokadę; inny cel dostaje własny licznik ponowień,
- `Cycles` osiągając `MaxFailedCycles` zatrzymuje wykonawcę na dobre; `Blocked` dotyczy jednego celu i wygasa po `BlockedTTL`,
- **wygaśnięcie blokady zeruje `Cycles`** — wygaśnięcie zaczyna świeżą próbę, a nie ciąg dalszy serii porażek; bez tego bot zatrzymałby się przy pierwszej próbie po wygaśnięciu,
- identyfikatory kroków nigdy się nie powtarzają, także po `Reset()`, żeby spóźnione potwierdzenie nie ostemplowało następcy,
- schody: kierunek z bieżącej kratki do następnego waypointa; brak następnika, brak obserwacji albo pusty kierunek → nie wysyłamy nic.

- [ ] **Step 4: Uruchom testy**

Run: `go test ./internal/brain/ -run Executor -v`
Expected: PASS, 25 testów

- [ ] **Step 5: Commit**

```bash
git add internal/brain/executor.go internal/brain/executor_test.go
git commit -m "Przenieś cykl życia kroku do Go"
```

---

### Task 6: Wykonawca — nauka blokad

Druga połowa `web/executor.js`: kiedy nieudany krok jest dowodem o mapie, a kiedy nie, i jak spóźnione dojście ten dowód odwołuje. **Najgęstsza logika w całym projekcie** i miejsce, w którym regresja objawia się najpóźniej — jako kratka trwale omijana bez powodu.

Przenoszone przypadki: **23–29, 31–33, 36–39**.

**Files:**
- Modify: `internal/brain/executor.go`
- Modify: `internal/brain/executor_test.go`

**Interfaces:**
- Consumes: `nav.Observation` z `internal/nav/blocks.go`
- Produces:
  ```go
  func (e *Executor) TakeObservation() (nav.Observation, bool)
  ```

- [ ] **Step 1: Dopisz testy dla przypadków 23–29, 31–33, 36–39**

23. krok bez ruchu z trzema klatkami produkuje obserwację
24. obserwacja jest oddawana tylko raz
25. krok bez potwierdzenia emisji niczego nie uczy
26. zmiana piętra niczego nie uczy
27. przesunięcie gdzie indziej niczego nie uczy
28. za mało klatek to za słaby dowód
29. udany krok nie produkuje obserwacji
31. spóźnione wejście na kratkę odwołuje naukę
32. wejście długo po timeoucie nie jest już odwołaniem
33. spóźnione wejście zdejmuje blokadę celu
36. spóźnione wejście kasuje licznik nieudanych cykli
37. obserwacja niesie informację, czy postać w międzyczasie chodziła
38. spóźnione wejście niesie kratkę startową, nie tylko docelową
39. wejście oceniane jest po czasie klatki, nie po czasie jej przetworzenia

Wzór dla dwóch najtrudniejszych:

```go
// Only a step confirmed as emitted and then watched standing still for three
// consecutive frames is evidence about terrain. Fewer frames could be one
// dropped reading; an unconfirmed key says nothing at all.
func TestStandingStillForThreeFramesProducesAnObservation(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	from := mapdata.Position{X: 100, Y: 100, Z: 7}
	e.Observe(&from, at(0), at(0))
	e.IntentFor(walkTo(101, 100), at(0))
	e.Emitted(at(10), e.State().StepID)
	for i, ms := range []int{100, 200, 300} {
		e.Observe(&from, at(ms), at(ms))
		if _, ok := e.TakeObservation(); ok {
			t.Fatalf("obserwacja po %d klatkach — za wcześnie", i+1)
		}
	}
	e.IntentFor(walkTo(101, 100), at(2000)) // timeout wypycha porażkę
	obs, ok := e.TakeObservation()
	if !ok {
		t.Fatal("brak obserwacji po trzech nieruchomych klatkach")
	}
	if obs.Outcome != "no_motion" || obs.From != from || obs.To.X != 101 {
		t.Errorf("obserwacja = %+v", obs)
	}
	if obs.StillFrames < 3 {
		t.Errorf("still_frames = %d, oczekiwano co najmniej 3", obs.StillFrames)
	}
}

// Arriving shortly after the step was written off means it was lag or
// paralysis, not a wall. Whatever the failure taught has to be revoked, and
// the whole escalation it caused rolled back - otherwise three unrelated lag
// spikes in a session add up to a permanent stop.
func TestLateArrivalRevokesWhatTheFailureTaught(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	from := mapdata.Position{X: 100, Y: 100, Z: 7}
	e.Observe(&from, at(0), at(0))
	e.IntentFor(walkTo(101, 100), at(0))
	e.Emitted(at(10), e.State().StepID)
	for _, ms := range []int{100, 200, 300} {
		e.Observe(&from, at(ms), at(ms))
	}
	e.IntentFor(walkTo(101, 100), at(2000))
	e.TakeObservation() // porażka odebrana i wysłana

	arrived := mapdata.Position{X: 101, Y: 100, Z: 7}
	e.Observe(&arrived, at(2500), at(2500))
	obs, ok := e.TakeObservation()
	if !ok || obs.Outcome != "entered" {
		t.Fatalf("oczekiwano odwołania nauki, dostano %+v ok=%v", obs, ok)
	}
	// from identifies the edge a failed diagonal blocked; to alone does not.
	if obs.From != from {
		t.Errorf("obserwacja bez kratki startowej: %+v", obs)
	}
	if s := e.State(); s.Retries != 0 || s.Cycles != 0 {
		t.Errorf("eskalacja nie została cofnięta: %+v", s)
	}
}
```

- [ ] **Step 2: Uruchom testy**

Run: `go test ./internal/brain/ -run Executor`
Expected: FAIL — `TakeObservation` nie istnieje

- [ ] **Step 3: Dopisz naukę blokad**

Cztery sytuacje, w których krok **nie** jest dowodem o mapie — każda musi mieć swój warunek w kodzie:

1. klawisz nigdy nie opuścił drivera (`emittedAt` nieustawione),
2. postać zmieniła piętro — wejście na schody wygląda identycznie jak nieudany krok, a uznanie go za porażkę uczyłoby, że schody są ścianą,
3. postać stoi gdzie indziej niż przy wysłaniu klawisza — zepchnięta przez potwora albo gracz przejął sterowanie,
4. mniej niż 3 nieruchome klatki.

`TakeObservation` oddaje obserwację **raz** i o niej zapomina, żeby jedna porażka nie została zgłoszona tyle razy, ile pętla zdąży tyknąć.

Spóźnione dojście: ocena po **czasie wycięcia klatki**, nie po czasie jej przetworzenia — wolne dopasowanie nie może zamienić prawdziwego dojścia w spóźnialstwo.

- [ ] **Step 4: Uruchom testy**

Run: `go test ./internal/brain/ -run Executor -v`
Expected: PASS, 39 testów

- [ ] **Step 5: Commit**

```bash
git add internal/brain/executor.go internal/brain/executor_test.go
git commit -m "Przenieś naukę blokad do Go razem z jej czterema wyjątkami"
```

---

### Task 7: Follower — waypointy i przejścia

Port pierwszej połowy `web/follower.js`: konsumowanie osiągniętych waypointów, tolerancje, przejścia między piętrami, zapętlenie trasy.

Przenoszone przypadki: **1–5, 11–13, 16–17, 20–22, 24, 26, 29–36**.

**Files:**
- Create: `internal/brain/follower.go`
- Test: `internal/brain/follower_test.go`
- Modify: `internal/brain/executor.go` — przenieś tu deklarację `Output` i `Action` z Zadania 5
- Reference: `web/follower.js`, `webtests/follower_test.cjs`

**Interfaces:**
- Consumes: `route.Waypoint`, `mapdata.Position`, `Output`, `Action`
- Produces:
  ```go
  type FollowerOptions struct {
      Tolerance       int           // default 1
      ActionTolerance int           // default 0 - a rope used one tile off does nothing
      Loop            bool
      Replan          time.Duration // default 500ms
      Retry           time.Duration // default 4s
  }

  func NewFollower(waypoints []route.Waypoint, o FollowerOptions) *Follower
  func (f *Follower) Reset()
  func (f *Follower) Waypoint() *route.Waypoint
  func (f *Follower) Index() int
  func (f *Follower) Finished() bool
  func (f *Follower) SkipTo(i int)
  func (f *Follower) DropPath()
  func (f *Follower) Path() [][2]int
  func (f *Follower) Step(p mapdata.Position, now time.Time) Output
  func (f *Follower) SetMinOverlayRevision(rev uint64)
  ```

- [ ] **Step 1: Napisz testy dla 23 przypadków**

1. `an empty route is finished before it starts`
2. `reaching the last waypoint finishes the route`
3. `standing on a waypoint advances to the next one`
4. `a diagonal neighbour counts as reaching the waypoint`
5. `a waypoint two tiles away is not reached yet`
11. `a waypoint on another floor waits for the transition instead of pathing`
12. `each transition type has its own instruction`
13. `arriving on the new floor resumes normal following`
16. `a looping route restarts at the first waypoint`
17. `tolerance is configurable for open ground`
20. `a recorded transition pair keeps the action instruction`
21. `an action waypoint is consumed once the floor actually changes`
22. `an action waypoint still has to be walked to first`
24. `a transition completed between two readings still counts`
26. `skipping away and back does not reuse an old floor change`
29. `a looped single-waypoint route settles instead of spinning`
30. `a looped route whose points are all within tolerance settles too`
31. `waypoint akcji nie jest osiągnięty z sąsiedniej kratki`
32. `waypoint akcji jest osiągnięty z dokładnie tej kratki`
33. `instrukcja przejścia niesie następny waypoint`
34. `instrukcja przejścia niesie bieżący indeks waypointa`
35. `ostatni waypoint przejścia nie ma następnika`
36. `actionTolerance można poluzować świadomie`

Wzór dla dwóch, na których najłatwiej się potknąć:

```go
// A looped route whose every point sits within tolerance would otherwise
// consume waypoints forever and re-request a path on every single reading.
// One pass at most: after that the route is simply finished.
func TestLoopedRouteWithinToleranceSettlesInsteadOfSpinning(t *testing.T) {
	wps := []route.Waypoint{
		{X: 100, Y: 100, Z: 7, Type: "walk"},
		{X: 100, Y: 100, Z: 7, Type: "walk"},
	}
	f := NewFollower(wps, FollowerOptions{Loop: true, Tolerance: 1})
	out := f.Step(mapdata.Position{X: 100, Y: 100, Z: 7}, at(0))
	if out.Action != ActionDone {
		t.Errorf("akcja = %s, oczekiwano done zamiast kręcenia się w kółko", out.Action)
	}
}

// The player can cross between two readings and never be seen standing on the
// action waypoint. Standing on the floor the route continues on is then the
// evidence that the action happened.
func TestTransitionCompletedBetweenTwoReadingsStillCounts(t *testing.T) {
	wps := []route.Waypoint{
		{X: 100, Y: 100, Z: 7, Type: "rope"},
		{X: 100, Y: 100, Z: 6, Type: "walk"},
		{X: 110, Y: 100, Z: 6, Type: "walk"},
	}
	f := NewFollower(wps, FollowerOptions{Tolerance: 1})
	out := f.Step(mapdata.Position{X: 100, Y: 100, Z: 6}, at(0))
	if out.Action == ActionTransition {
		t.Errorf("follower dalej czeka na przejście, choć postać jest już piętro niżej: %+v", out)
	}
}
```

- [ ] **Step 2: Uruchom testy**

Run: `go test ./internal/brain/ -run Follower`
Expected: FAIL — `undefined: NewFollower`

- [ ] **Step 3: Zaimplementuj `Step` i `advance`**

Instrukcje przejść przenieś dosłownie — użytkownik czyta je w panelu:

```go
var transitionInstructions = map[string]string{
	"rope":   "Użyj liny",
	"ladder": "Wejdź po drabinie",
	"stairs": "Wejdź na schody",
	"hole":   "Zejdź dziurą",
	"shovel": "Kop łopatą",
	"walk":   "Przejdź na piętro",
}
```

Reguły:
- waypoint akcji wymaga **dokładnej** kratki (`ActionTolerance`, domyślnie 0) — lina użyta kratkę obok nie robi nic; waypoint marszu może mieć luźniejszą tolerancję,
- waypoint akcji jest zaliczony przez zmianę piętra (`actionAt`) **albo** przez stanie na piętrze, na którym trasa się kontynuuje,
- `advance` wykonuje najwyżej jedno okrążenie listy,
- odległość liczona metryką Czebyszewa.

- [ ] **Step 4: Uruchom testy**

Run: `go test ./internal/brain/ -run Follower -v`
Expected: PASS, 23 testy

- [ ] **Step 5: Commit**

```bash
git add internal/brain/follower.go internal/brain/follower_test.go internal/brain/executor.go
git commit -m "Przenieś konsumowanie waypointów i przejścia pięter do Go"
```

---

### Task 8: Follower — ścieżki, replan i backoff

Druga połowa `web/follower.js`: pamięć podręczna trasy, przeplanowanie, backoff po nieudanym planowaniu i odrzucanie tras policzonych przed nauczoną blokadą.

Przenoszone przypadki: **6–10, 14–15, 18–19, 23, 25, 27–28, 37–38**.

**Files:**
- Modify: `internal/brain/follower.go`
- Modify: `internal/brain/follower_test.go`

**Interfaces:**
- Consumes: `nav.PathResult`
- Produces:
  ```go
  func (f *Follower) SetPath(res *nav.PathResult, now time.Time, askedFor *route.Waypoint)
  ```

- [ ] **Step 1: Dopisz testy dla 15 przypadków**

6. `without a path the follower asks for one`
7. `a path turns into a walking direction`
8. `every compass direction comes out of the next step`
9. `walking along the path consumes it without asking again`
10. `stepping off the path triggers a new request once the throttle allows it`
14. `a stale path for a previous waypoint is discarded`
15. `a blocked waypoint is reported rather than retried in a tight loop`
18. `skipping to a waypoint drops the path built for the old one`
19. `editing the current waypoint while following invalidates the path`
23. `a path that arrives after the waypoint changed is discarded`
25. `a stale reply leaves a newer path alone`
27. `a failed path is retried once the player moves`
28. `a failed path is retried after a backoff even standing still`
37. `trasa policzona przed nauczoną blokadą jest odrzucana`
38. `brak rewizji w odpowiedzi nie blokuje trasy`

Wzór dla przypadku, który powstał po prawdziwym błędzie:

```go
// A path request already in flight when a block is learned would reinstall the
// pre-block route on arrival, sending the bot straight back into the tile it
// just learned about.
func TestPathComputedBeforeALearnedBlockIsDiscarded(t *testing.T) {
	wps := []route.Waypoint{{X: 110, Y: 100, Z: 7, Type: "walk"}}
	f := NewFollower(wps, FollowerOptions{Tolerance: 1})
	f.SetMinOverlayRevision(5)
	f.SetPath(&nav.PathResult{Found: true, Steps: [][2]int{{100, 100}, {101, 100}}, OverlayRevision: 4},
		at(0), &wps[0])
	if f.Path() != nil {
		t.Error("trasa sprzed nauczonej blokady została przyjęta")
	}
}

// A reply carrying no revision at all - an older server, or a locally produced
// failure - must not deadlock guidance.
func TestReplyWithoutARevisionIsStillAccepted(t *testing.T) {
	wps := []route.Waypoint{{X: 110, Y: 100, Z: 7, Type: "walk"}}
	f := NewFollower(wps, FollowerOptions{Tolerance: 1})
	f.SetMinOverlayRevision(5)
	f.SetPath(&nav.PathResult{Found: true, Steps: [][2]int{{100, 100}, {101, 100}}}, at(0), &wps[0])
	if f.Path() == nil {
		t.Error("odpowiedź bez rewizji została odrzucona, a nie powinna")
	}
}
```

**Uwaga na pułapkę portu.** W JS `result.overlay_revision !== undefined` odróżnia „brak pola" od zera. W Go `nav.PathResult.OverlayRevision` to zwykły `uint64` i brak pola nie różni się od zera. Rozwiąż to, przekazując do `SetPath` wskaźnik `*nav.PathResult` oraz osobną flagę — albo dodając do `nav.PathResult` metodę `HasRevision()`. Wybierz jedno i użyj konsekwentnie; przypadek 38 to sprawdza.

- [ ] **Step 2: Uruchom testy**

Run: `go test ./internal/brain/ -run Follower`
Expected: FAIL — `SetPath` nie istnieje

- [ ] **Step 3: Zaimplementuj pamięć trasy i backoff**

- odpowiedź na trasę dla **innego** waypointa niż aktualny jest ignorowana (żądanie było w locie, gdy cel się zmienił),
- `remainingPath` docina trasę do kratki gracza; gracz poza trasą dostaje `nil`, co odsyła do nowego planowania,
- nieudane planowanie jest ponawiane, gdy gracz się ruszył **albo** minął `Retry` (4 s) — porażka jest chwilą, nie wyrokiem,
- `Replan` (500 ms) dławi częstotliwość pytania o trasę.

- [ ] **Step 4: Uruchom testy**

Run: `go test ./internal/brain/ -run Follower -v`
Expected: PASS, 38 testów

- [ ] **Step 5: Commit**

```bash
git add internal/brain/follower.go internal/brain/follower_test.go
git commit -m "Przenieś planowanie trasy i backoff do Go"
```

---

### Task 9: Rdzeń lokalizacji i planowania wyjęty z HTTP

Mózg nie może rozmawiać z własnym procesem przez HTTP. `locateapi.go` i `pathapi.go` dzielą się na warstwę transportową i rdzeń wołany bezpośrednio.

**Files:**
- Create: `internal/locate/service.go` — przenieś z `locateapi.go`: `localAtlas`, `localFootprint`, `locateLocal`, `localAtlasEntry`
- Create: `internal/nav/service.go` — przenieś z `pathapi.go`: budowa obszaru, `costGrid`, wybór najbliższej przechodniej kratki, wywołanie `FindPath`
- Modify: `locateapi.go` — zostaje handler HTTP wołający rdzeń
- Modify: `pathapi.go` — jak wyżej
- Modify: `server.go` — pola cache przenoszą się do nowych typów usług
- Test: `internal/locate/service_test.go`, `internal/nav/service_test.go`

**Interfaces:**
- Produces:
  ```go
  // package locate
  type Service struct{ /* atlas caches, previously server fields */ }
  func NewService(dir string) *Service
  type Request struct {
      Options
      Floor          int
      Demo           bool
      Near           *mapdata.Position
      Radius         int
      AdjacentFloors bool
      FloorRadius    int
  }
  func (s *Service) Locate(ctx context.Context, im image.Image, req Request) (Result, *mapdata.Atlas, error)

  // package nav
  type Planner struct{ /* cost grid cache */ }
  func NewPlanner(dir string, blocks *BlockStore) *Planner
  func (p *Planner) Plan(ctx context.Context, from, to mapdata.Position, margin int) (PathResult, error)
  ```

- [ ] **Step 1: Napisz test, że rdzeń działa bez HTTP**

```go
package locate

import (
	"context"
	"testing"

	"minimap-lab/internal/mapdata"
)

// The brain calls this in-process; if it still needs a ResponseWriter the
// split did not happen.
func TestServiceLocatesOnTheDemoAtlasWithoutHTTP(t *testing.T) {
	s := NewService("")
	im := mapdata.DemoSnippet(mapdata.DemoAtlas())
	res, atlas, err := s.Locate(context.Background(), im, Request{
		Options: Options{Zoom: 2, MarkerX: 94, MarkerY: 94, MaskRadius: 5, MinScore: .94, MinGap: .015},
		Floor:   7, Demo: true,
	})
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if !res.Found || atlas == nil {
		t.Fatalf("demo nie zostało zlokalizowane: %+v", res)
	}
	if res.Position.X != 32200 || res.Position.Y != 32180 || res.Position.Z != 7 {
		t.Errorf("pozycja = %+v, oczekiwano 32200, 32180, 7", res.Position)
	}
}
```

- [ ] **Step 2: Uruchom test**

Run: `go test ./internal/locate/ -run Service`
Expected: FAIL — `undefined: NewService`

- [ ] **Step 3: Przenieś rdzeń, zostawiając handlery jako cienką skorupę**

Przenoszenie ma być **przenoszeniem, nie przepisywaniem**: kod trafia do nowego pakietu w niezmienionej postaci, razem z komentarzami. Zmienia się wyłącznie to, skąd bierze `s.dir`, `s.cached`, `s.localAtlases`, `s.cacheClock`, `s.gate` (locate) oraz `s.costMu`, `s.costCache`, `s.costFloor`, `s.blocks` (nav).

Handler `POST /api/locate` po tej zmianie tylko dekoduje formularz, woła `Locate` i koduje odpowiedź. **Nie kasuj go w tym zadaniu** — zniknie w Zadaniu 11 razem z resztą starego toru.

- [ ] **Step 4: Uruchom cały zestaw testów**

Run: `go test ./... && node --test webtests/*.cjs`
Expected: PASS — wszystkie dotychczasowe testy Go i JS dalej przechodzą, bo zachowanie się nie zmieniło

- [ ] **Step 5: Commit**

```bash
git add internal/locate/ internal/nav/ locateapi.go pathapi.go server.go
git commit -m "Wyjmij rdzeń lokalizacji i planowania z HTTP, bo mózg woła je w procesie"
```

---

### Task 10: Pętla mózgu

Serce migracji: odpowiednik `updateRoute`, `followStep` i `pumpBlocks` z `web/app.js`, jako jedyny właściciel stanu decyzyjnego.

**Files:**
- Create: `internal/brain/loop.go`
- Create: `internal/brain/state.go`
- Test: `internal/brain/loop_test.go`

**Interfaces:**
- Consumes: `Tracker`, `Recorder`, `Executor`, `Follower`, `locate.Service`, `nav.Planner`, `nav.BlockStore`, `input.Driver`, `frame.Frame`
- Produces:
  ```go
  type Config struct {
      Zoom, MarkerX, MarkerY, MaskRadius int
      MinScore, MinGap                   float64
      Floor                              int
      AdjacentFloors                     bool
      FloorRadius                        int
      Speed                              int
      Walk, FloorActions                 bool // the two panel checkboxes
      RecordAuto                         bool
      RecordEvery                        int
      Tolerance, ActionTolerance         int
      LoopRoute                          bool
  }

  type Deps struct {
      Locator *locate.Service
      Planner *nav.Planner
      Blocks  *nav.BlockStore
      Driver  *input.Driver
      Now     func() time.Time
  }

  func NewLoop(d Deps) *Loop
  func (l *Loop) Run(ctx context.Context)
  func (l *Loop) Submit(f frame.Frame, receivedAt time.Time)
  func (l *Loop) Snapshot() *State
  func (l *Loop) SetConfig(c Config) error
  func (l *Loop) SetRoute(r route.Route)
  func (l *Loop) Route() route.Route
  ```

- [ ] **Step 1: Napisz testy scenariuszowe**

Testy pętli sprawdzają **kolejność**, bo to ona jest wynikiem naprawionych błędów:

Najpierw wspólny harness. Atrapa emitera już istnieje w `internal/input/driver_test.go` — skopiuj jej kształt, bo jest nieeksportowana:

```go
// fakeEmitter records what would have been pressed instead of pressing it.
type fakeEmitter struct {
	keys    []string
	clicks  int
	focused input.Window
}

func (e *fakeEmitter) Preflight() error              { return nil }
func (e *fakeEmitter) Focused() (input.Window, error) { return e.focused, nil }
func (e *fakeEmitter) TapKey(k string, ms int) error { e.keys = append(e.keys, k); return nil }
func (e *fakeEmitter) Click(x, y float64) error      { e.clicks++; return nil }
func (e *fakeEmitter) ReleaseAll() error             { return nil }

type testClock struct{ at time.Time }

func (c *testClock) now() time.Time            { return c.at }
func (c *testClock) advance(d time.Duration)   { c.at = c.at.Add(d) }

// newTestLoop wires a Loop with everything faked but the brain itself.
func newTestLoop(t *testing.T) (*Loop, *fakeEmitter, *testClock) {
	t.Helper()
	clock := &testClock{at: base}
	em := &fakeEmitter{focused: input.Window{PID: 1}}
	driver := input.NewDriver(em, 400)
	if _, err := driver.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	l := NewLoop(Deps{
		Locator: locate.NewService(""),
		Planner: nav.NewPlanner("", nav.NewBlockStore(clock.now)),
		Blocks:  nav.NewBlockStore(clock.now),
		Driver:  driver,
		Now:     clock.now,
	})
	return l, em, clock
}
```

Pięć scenariuszy do napisania. Każdy podany jako stan początkowy → czynność → oczekiwany skutek, bo to one są tu treścią, a nie konstrukcja testu:

**1. `TestFloorActionGateRefusesBeforeAPendingStepExists`**
Dane: trasa z waypointem typu `rope`, postać stojąca na nim, `Config.FloorActions = false`, `Config.Walk = true`.
Czynność: jedna klatka przez `Submit`.
Oczekiwane: `em.keys` puste **oraz** `l.Snapshot().Executor.Waiting == false`. Drugi warunek jest istotą testu — wykonawca nie może mieć kroku w toku, który potem wygaśnie w ponowienie i trwałą blokadę. Kolejność jest tu jedyną rzeczą, która o tym decyduje: follower pytany przed wykonawcą.

**2. `TestPausingFloorActionsStillWalksOntoStairs`**
Dane: trasa z waypointem typu `stairs` i następnikiem obok, `Config.FloorActions = false`, `Config.Walk = true`.
Czynność: jedna klatka.
Oczekiwane: `em.keys` zawiera klawisz kierunku. Schody follower zgłasza jako `transition`, ale wykonawca zamienia je w zwykły krok, więc pauza akcji pięter nie może ich blokować.

**3. `TestNewlyBlockedTargetDropsTheCachedPath`**
Dane: trasa z zapamiętaną ścieżką, wykonawca doprowadzony do stanu `Blocked` na bieżącym celu.
Czynność: kolejna klatka.
Oczekiwane: `l.follower.Path() == nil`. Bez tego follower produkuje ten sam cel w nieskończoność, wykonawca go odrzuca i całość zamarza, nigdy nie dochodząc do eskalacji, która zatrzymałaby trasę.

**4. `TestImpassableTileIsNotRecordedAsAWaypoint`**
Dane: `Config.RecordAuto = true`, pozycja na kratce, którą siatka kosztów uznaje za nieprzechodnią.
Czynność: jedna klatka.
Oczekiwane: `l.Route().Waypoints` puste, a `l.Snapshot().Recorder.Skipped == 1`. Osobno: przy **braku** danych okna przechodniości nagrywanie ma czekać, a nie nagrywać ani liczyć pominięcia.

**5. `TestWatchdogDisarmsWhenFramesStop`**
Dane: uzbrojony sterownik, pętla uruchomiona przez `Run`.
Czynność: przesuń zegar o 800 ms bez żadnej klatki.
Oczekiwane: `driver.Armed() == false`. Watchdog musi działać także wtedy, gdy żądania ustają zupełnie — na tym polega różnica wobec heartbeatu, który zastępuje.

- [ ] **Step 2: Uruchom testy**

Run: `go test ./internal/brain/ -run Loop`
Expected: FAIL — `undefined: NewLoop`

- [ ] **Step 3: Zaimplementuj pętlę**

Model współbieżności — trzymaj się go dokładnie:

- **jedna goroutine** jest jedynym właścicielem `Tracker`, `Recorder`, `Executor`, `Follower`,
- klatki wchodzą **jednoslotowym** kanałem o pojemności 1; nowsza klatka nadpisuje starszą, nigdy się nie kolejkują,
- konfiguracja i trasa wchodzą kanałem poleceń,
- snapshot publikowany przez `atomic.Pointer[State]`, handlery czytają bez blokowania,
- osobna, mała goroutine watchdoga rozbraja wykonawcę po 750 ms bez klatki.

Kolejność wewnątrz obsługi klatki, przeniesiona z `followStep`:

1. dopasowanie minimapy z podpowiedzią z `Tracker.Hint`,
2. `Tracker.Observe`,
3. nagrywanie — **po** sprawdzeniu przechodniości kratki; brak danych okna → czekaj, kratka nieprzechodnia → policz pominięcie i nie nagrywaj,
4. wysłanie obserwacji z `Executor.TakeObservation` do `BlockStore.Observe`, podniesienie `minOverlayRevision` followera,
5. `Follower.Step`,
6. bramka „chodzenie wyłączone" i bramka „akcje pięter wyłączone" (schody wyjęte spod tej drugiej),
7. `Executor.Observe`, potem `Executor.IntentFor`,
8. emisja przez `input.Driver`, `Executor.Emitted` po potwierdzeniu.

**Brama świeżości sprawdzana tuż przed emisją**, liczona od czasu wycięcia klatki, która dała aktualną pozycję.

- [ ] **Step 4: Uruchom testy**

Run: `go test ./internal/brain/ -v`
Expected: PASS — wszystkie testy pakietu

- [ ] **Step 5: Commit**

```bash
git add internal/brain/loop.go internal/brain/state.go internal/brain/loop_test.go
git commit -m "Dodaj pętlę mózgu jako jedynego właściciela stanu"
```

---

### Task 11: Endpointy klatki, stanu, konfiguracji i trasy

**Files:**
- Create: `frameapi.go`
- Test: `frameapi_test.go`
- Modify: `server.go` — trasy i pola serwera
- Modify: `main.go` — start goroutine pętli
- Delete: `POST /api/locate`, `POST /api/path`, `POST /api/input`, `POST /api/input/done`, `GET /api/input/status`, `POST /api/input/config`, `POST /api/input/calibrate` wraz z `inputapi.go`

**Interfaces:**
- Consumes: `brain.Loop`, `frame.Parse`
- Produces: `POST /api/frame`, `GET /api/state`, `PUT /api/config`, `PUT /api/route`, `GET /api/route`, `GET /api/preview`

- [ ] **Step 1: Napisz testy handlerów**

Wzór pełnego testu handlera, po którym widać, jak zbudować żądanie — `helpers_test.go` ma już `newTestServer`, użyj go:

```go
func TestFrameFromAnotherCaptureSessionIsRefused(t *testing.T) {
	s := newTestServer(t)
	armed := armForTest(t, s) // sesja przechwytywania zwrócona przez POST /api/arm

	body := buildFrame(armed.Session+1, 1, 0, 0, minimapRegion(t))
	req := httptest.NewRequest("POST", "/api/frame", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, req)

	// A reload leaves the previous stream's frames in flight; accepting them
	// would feed the brain pictures from a capture the user has ended.
	if w.Code != http.StatusForbidden {
		t.Errorf("kod = %d, oczekiwano 403 dla obcej sesji przechwytywania", w.Code)
	}
}
```

Pozostałe pięć scenariuszy:

**`TestFrameHandlerBoundsAndDrainsTheBody`**
Dane: ciało o rozmiarze `frame.MaxBody + 1`.
Oczekiwane: odpowiedź 413, **a nie** klatka sparsowana z uciętych danych. Osobno sprawdź, że po poprawnym żądaniu `r.Body` jest doczytane do końca — nieodczytane ciało każe Go zerwać połączenie TCP zamiast je odzyskać, a przy 20 żądaniach na sekundę to wyczerpuje porty efemeryczne w kilka minut.

**`TestFrameHandlerAnswersWithoutWaitingForTheMatch`**
Dane: pętla, w której dopasowanie zajmuje sztucznie długo (atrapa lokatora usypiająca na sygnał).
Czynność: dwa `POST /api/frame` pod rząd.
Oczekiwane: drugi wraca, zanim pierwsze dopasowanie się skończy, a `state_version` w obu odpowiedziach jest taki sam. Handler nie obiecuje wyniku właśnie przesłanej klatki.

**`TestConfigIsRejectedWholeOrAppliedWhole`**
Dane: poprawna konfiguracja zastosowana, potem druga z jednym złym polem (np. `zoom: 99`).
Oczekiwane: 400 **oraz** konfiguracja niezmieniona — jedno złe pole nie może po cichu wyczyścić niezwiązanego z nim.

**`TestRouteRoundTripsThroughTheAPI`**
Czynność: `PUT /api/route` z trasą, potem `GET /api/route`.
Oczekiwane: identyczne waypointy. To jest droga, którą panel zapisuje nagraną trasę do pliku.

**`TestSnapshotDoesNotCarryWaypoints`**
Dane: trasa z 1000 waypointów.
Oczekiwane: odpowiedź na klatkę nie zawiera pola z waypointami, a jej rozmiar mieści się poniżej kilku kilobajtów. Snapshot leci przy każdej klatce — tysiąc punktów w każdej z nich to kilkanaście megabajtów na minutę za nic.

- [ ] **Step 2: Uruchom testy**

Run: `go test . -run Frame`
Expected: FAIL — handler nie istnieje

- [ ] **Step 3: Zaimplementuj handlery**

`POST /api/frame`:

```go
func (s *server) frame(w http.ResponseWriter, r *http.Request) {
	if s.loop == nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			"Sterowanie wyłączone. Uruchom panel z -input dry albo -input system.")
		return
	}
	received := time.Now()
	body := framePool.Get().(*[]byte)
	defer framePool.Put(body)
	// MaxBytesReader first: a body larger than the cap must be refused, not
	// silently truncated into a frame that parses.
	n, err := io.ReadFull(http.MaxBytesReader(w, r.Body, frame.MaxBody), *body)
	...
}
```

Wymagania:
- bufory z `sync.Pool`, odczyt przez `io.ReadFull`, ciało **zawsze** doczytane do końca,
- `POST /api/arm` zwraca **token sesji przechwytywania**; panel znakuje nim każdą klatkę. To jedyna pozostałość po tokenie sesji intencji i jedyna rzecz, która odróżnia klatki z bieżącego strumienia od klatek strumienia zamkniętego przed przeładowaniem karty,
- klatka z obcą sesją przechwytywania → 403,
- handler nie liczy: `l.Submit(f, received)` i natychmiastowa odpowiedź `l.Snapshot()`,
- odpowiedź zawiera `state_version` i `last_frame_seq`, ale nie obiecuje, że opisuje właśnie przesłaną klatkę,
- `GET /api/preview` oddaje wycinek atlasu 129×129 wokół ostatniej pozycji jako PNG; snapshot niesie `preview_revision`,
- `GET /api/route` oddaje trasę wraz z nagranymi waypointami, żeby panel mógł ją zapisać do pliku.

- [ ] **Step 4: Uruchom cały zestaw**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frameapi.go frameapi_test.go server.go main.go
git rm inputapi.go inputapi_test.go
git commit -m "Zamień protokół intencji na jeden endpoint klatki"
```

---

### Task 12: Panel jako kamera i widok

**Files:**
- Create: `web/worker.js` — zegar pętli
- Create: `web/camera.js` — wycinanie regionów i wysyłka
- Create: `web/view.js` — rysowanie snapshotu
- Modify: `web/app.js` — zostaje DOM, zaznaczanie regionów i formularz konfiguracji
- Modify: `web/index.html`
- Delete: `web/tracker.js`, `web/follower.js`, `web/executor.js`, `web/recorder.js`, `web/route.js`, `web/input.js`, `web/blocks.js`
- Delete: `webtests/executor_test.cjs`, `webtests/follower_test.cjs`, `webtests/recorder_test.cjs`, `webtests/route_test.cjs`, `webtests/blocks_test.cjs`, `webtests/input_client_test.cjs`
- Modify: `webtests/tracker_test.cjs` — zostaje wyłącznie przypadek `cadence subtracts the request duration from each sampling period`
- Modify: `webtests/ui_test.cjs` — zostaje to, co panelowi zostało
- Create: `webtests/camera_test.cjs`

**Interfaces:**
- Produces:
  ```js
  // camera.js
  class Camera {
    constructor(options)          // {fetch, regions, onSnapshot}
    setRegion(id, rect)           // 1 minimapa, 2 pasek HP, 3 pasek many
    buildBody(video, seq)         // ArrayBuffer w formacie MLF1
    async sendFrame(video)        // jeden POST w locie, nigdy dwa
  }
  ```

- [ ] **Step 1: Napisz testy kamery**

```js
const {test} = require('node:test');
const assert = require('node:assert/strict');
const {Camera} = require('../web/camera.js');

// A fake video source: buildBody only ever reads these three properties.
const fakeVideo = (currentTime = 1.5) => ({currentTime, videoWidth: 800, videoHeight: 600});

test('body zaczyna się magikiem MLF1 i niesie zadeklarowane regiony', () => {
  const cam = new Camera({fetch: async () => ({ok: true, json: async () => ({})})});
  cam.setRegion(1, {x: 0, y: 0, w: 4, h: 4});
  const body = new Uint8Array(cam.buildBody(fakeVideo(), 1));
  assert.equal(String.fromCharCode(...body.slice(0, 4)), 'MLF1');
  assert.equal(body[4], 1, 'wersja formatu');
  assert.equal(body[5], 1, 'liczba regionów');
  const view = new DataView(body.buffer);
  assert.equal(view.getUint16(36 + 4, true), 4, 'szerokość regionu');
  assert.equal(view.getUint32(36 + 8, true), 4 * 4 * 4, 'długość ładunku');
});

// Two POSTs in flight would arrive out of order and pile up behind a slow
// match; a skipped frame is always cheaper than a backlog.
test('druga klatka nie jedzie, dopóki pierwsza nie wróciła', async () => {
  let inFlight = 0, peak = 0;
  let release;
  const gate = new Promise(r => { release = r; });
  const cam = new Camera({fetch: async () => {
    inFlight++; peak = Math.max(peak, inFlight);
    await gate;
    inFlight--;
    return {ok: true, json: async () => ({})};
  }});
  cam.setRegion(1, {x: 0, y: 0, w: 4, h: 4});
  const first = cam.sendFrame(fakeVideo(1));
  const second = cam.sendFrame(fakeVideo(2));
  release();
  await Promise.all([first, second]);
  assert.equal(peak, 1, 'dwa POST-y naraz');
});

// The same video frame sent twice is one observation, not two: network
// traffic is no proof that the picture moved.
test('niezmieniony currentTime nie tworzy nowej obserwacji', async () => {
  const sent = [];
  const cam = new Camera({fetch: async (url, opts) => {
    sent.push(opts.body); return {ok: true, json: async () => ({})};
  }});
  cam.setRegion(1, {x: 0, y: 0, w: 4, h: 4});
  await cam.sendFrame(fakeVideo(1.5));
  await cam.sendFrame(fakeVideo(1.5));
  assert.equal(sent.length, 1, 'zamrożona klatka poszła dwa razy');
});
```

**Uwaga:** `buildBody` musi dać się zawołać bez DOM-u. Wycinanie pikseli trzymaj w osobnej metodzie, którą test podmienia — inaczej `camera.js` nie da się przetestować w node bez canvasa.

- [ ] **Step 2: Uruchom testy**

Run: `node --test webtests/camera_test.cjs`
Expected: FAIL — `Camera` nie istnieje

- [ ] **Step 3: Zbuduj kamerę, workera i widok**

`worker.js` to kilkanaście linii — tyka i wysyła `postMessage`. Cała jego racja bytu jest w komentarzu:

```js
// The clock lives in a worker because the panel tab is in the background
// whenever the user is actually playing, and browsers throttle setTimeout in
// hidden tabs to about 1 Hz while stopping requestAnimationFrame entirely.
// Worker timers are exempt.
let timer = null;
onmessage = e => {
  if (e.data.stop) { clearInterval(timer); timer = null; return; }
  clearInterval(timer);
  timer = setInterval(() => postMessage('tick'), e.data.intervalMS);
};
```

`camera.js`: buduje `ArrayBuffer` z nagłówkiem i regionami, `fetch` **bez** `keepalive` (ta flaga w Chrome ucina ciało do 64 kB), jeden POST w locie.

**Źródła jednoklatkowe zostają.** Wczytanie screenshotu z pliku i obraz demo to narzędzia diagnostyczne: wysyłają jedną klatkę i pokazują snapshot. Bot i tak nie ruszy — watchdog rozbroi wykonawcę, a brama świeżości odrzuci każdą emisję — więc służą wyłącznie do sprawdzenia dopasowania. Nie kasuj ich razem z logiką.

`app.js` traci `updateRoute`, `followStep`, `pumpBlocks`, `ensureFollower`, obsługę `executor` i `inputClient`. Zostaje: zaznaczanie regionów, formularz konfiguracji wysyłany przez `PUT /api/config`, wczytywanie i zapisywanie pliku trasy przez `PUT /api/route` i `GET /api/route`, oraz rysowanie tego, co przyszło w snapshocie.

- [ ] **Step 4: Uruchom oba zestawy testów**

Run: `go test ./... && node --test webtests/*.cjs`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/ webtests/
git commit -m "Zredukuj panel do kamery i widoku"
```

---

### Task 13: Usunięcie protokołu intencji ze sterownika

**Files:**
- Modify: `internal/input/driver.go` — usuń `Intent`, `Result`, `Submit`, `Beat`, `ActionDone`, token sesji, numery sekwencyjne, `heartbeatTimeoutMS`
- Modify: `internal/input/input_test.go`, `internal/input/driver_test.go`
- Modify: `internal/brain/loop.go` — wołanie nowego, wewnętrznego API sterownika

**Interfaces:**
- Produces:
  ```go
  // Walk and UseHotkey replace Submit, keeping the existing input.Result
  // type. The gates that mattered stay; the
  // transport-shaped ones (session token, sequence numbers, heartbeat) go,
  // because there is no longer a remote caller to protect against.
  func (d *Driver) Walk(direction string, observationAge time.Duration) Result
  func (d *Driver) UseHotkey(kind string, observationAge time.Duration) Result
  func (d *Driver) Armed() bool
  ```

- [ ] **Step 1: Przepisz testy sterownika**

Zachowaj przypadki dotyczące **bram**, usuń dotyczące **transportu**:

Zostają: odmowa gdy rozbrojony, odmowa dla nieświeżej obserwacji, odmowa po utracie focusu, limit klawiszy na sekundę, odmowa dla nieskonfigurowanego kierunku, odmowa dla nieznanego hotkeya, schody odrzucane jako akcja, zwolnienie klawiszy przy rozbrajaniu, kalibracja kratki wymagana przy `ClickAfterHotkey`.

Znikają: nieprawidłowy token sesji, brak numeru sekwencyjnego, powtórzony numer, numer cofnięty, wygaśnięcie heartbeatu.

- [ ] **Step 2: Uruchom testy**

Run: `go test ./internal/input/`
Expected: FAIL — testy wołają usunięte API

- [ ] **Step 3: Usuń transport, zostaw bramy**

Rozbrojenie z powodu milczącego panelu przenosi się do watchdoga pętli (Zadanie 10), więc `heartbeatTimeoutMS` znika ze sterownika. `MaxObservationAgeMS` i `-stale-ms` **zostają** bez zmian.

- [ ] **Step 4: Uruchom cały zestaw**

Run: `go test ./... && node --test webtests/*.cjs`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/input/ internal/brain/loop.go
git commit -m "Usuń protokół intencji, bo decydent i emiter są w jednym procesie"
```

---

### Task 14: Dokumentacja i weryfikacja końcowa

**Files:**
- Modify: `README.md` — sekcje „Sterowanie", „HTTP API", „Układ katalogów", „Jak działa", „Testy"

- [ ] **Step 1: Zaktualizuj README**

Do przepisania: opis architektury (mózg w Go, panel jako kamera), lista endpointów (znikają cztery, dochodzi sześć), układ katalogów (nowe pakiety), sekcja o dwóch checkboxach i klawiszach — teraz konfigurowane przez `PUT /api/config`.

Dopisz sekcję o tym, **dlaczego zegar panelu siedzi w Web Workerze** — bez tego pierwsza osoba, która zobaczy `worker.js`, uzna go za nadmiarowy i przeniesie z powrotem do wątku głównego.

- [ ] **Step 2: Uruchom pełny zestaw testów**

Run: `go test ./... && node --test webtests/*.cjs`
Expected: PASS, zero pominiętych

- [ ] **Step 3: Sprawdź, że nic nie zostało po starym torze**

```bash
grep -rn "observation_age_ms\|X-Input-Session\|api/input\|StepExecutor\|RouteFollower\|MinimapTracker" --include="*.go" --include="*.js" --include="*.cjs" . | grep -v "^./docs/"
```
Expected: brak wyników

- [ ] **Step 4: Potwierdź w grze**

Ręcznie, z `-input system`: uzbrojenie, przejście zapisanej trasy, obejście przeszkody, zmiana piętra liną, rozbrojenie przez utratę focusu, rozbrojenie przez zamknięcie karty panelu.

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "Opisz nową architekturę w README"
```
