# Widzenie na prawdziwym kliencie — plan wdrożenia

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Nauczyć warstwę widzenia czytać paski i ramkę celu prawdziwego klienta, którego pierwsza klatka obaliła trzy założenia fazy 1.

**Architecture:** Trzy niezależne poprawki plus fixture. `vision.confirm()` dostaje tolerancję rozmycia tylko na skrajnych wierszach wnętrza. `CombatConfig` rozdziela tolerancję barw i próg czerni na parę okna gry i parę battle listy. `battle.framed()` szuka ramki w prostokącie ikonki, nie w pasie wiersza. Na końcu testy realnej klatki dostają zmierzoną kalibrację z fixture i panel dostaje nowe pola.

**Tech Stack:** Go 1.x (`internal/vision`, `internal/battle`, `internal/brain`, `internal/testenv`), panel ES-modules (`web/`), testy `node --test` (`webtests/`).

**Spec:** `docs/superpowers/specs/2026-09-18-vision-antialiasing-design.md`

## Global Constraints

- Testy Go: `go test ./... -race` musi przechodzić po każdym zadaniu. Testy panelu: `npm test` (czyli `node --test webtests/*.mjs`).
- Cały nowy kod i komentarze po angielsku (jak reszta `internal/`); teksty panelu, README i komunikaty walidacji po polsku z pełnymi znakami diakrytycznymi.
- `EdgeTolerance` domyślnie **0** — to oznacza dzisiejsze zachowanie bit-do-bitu; każdy istniejący test syntetyczny musi przejść bez zmian.
- Zero pól konfiguracji dubluje się między oknem gry a battle listą poza tymi, które spec wymienia (`BarColors` zostaje jedną wspólną listą).
- Zmierzona kalibracja (z `docs/superpowers/plans/2026-09-07-vision-layer-measurements.md`), wpisywana do fixture: okno gry `Rect(637,245,3778,2548)`, wycinek `Rect(1056,245,3359,2548)`, battle `Rect(4770,900,5100,1150)`, HP `Rect(24,134,2202,136)`, mana `Rect(2217,134,4392,136)`, pasek `62×8` obwódka `3` tolerancja `20` czerń `125`, battle pasek `262×8` obwódka `1` tolerancja `80` czerń `48` brzeg `1` odstęp `44`, ramka `#c90a0a` ikonka offset `(-45,-31)` bok `40`, 3 stwory, `TargetRow=0`, `SelfBar=(1070,985)`.

---

### Task 1: Tolerancja rozmycia w `vision.confirm()`

Klient wygładza brzeg paska, więc pierwszy i ostatni wiersz wypełnienia bywa o 1–2 px węższy od rdzenia. Dzisiejszy `confirm()` żąda identycznej szerokości w każdym wierszu i odrzuca każdy ranny pasek battle listy. Zmiana: rdzeń liczony z wierszy środkowych, brzegi tolerowane tylko w dół.

**Files:**
- Modify: `internal/vision/bars.go` (typ `Options`, `Find`, `confirm`)
- Test: `internal/vision/bars_test.go`

**Interfaces:**
- Produces: `vision.Options.EdgeTolerance int` (nowe pole); `confirm(im *image.NRGBA, bar Bar) (fill int, ok bool)` (zmieniona sygnatura, prywatna — używa jej tylko `Find`).
- Consumes: nic nowego.

- [ ] **Step 1: Dodaj nieprzechodzące testy tolerancji brzegu**

W `internal/vision/bars_test.go` dopisz funkcję pomocniczą rysującą pasek z brzegami węższymi od rdzenia, i testy. `classic` to `vision.Geometry{Width:27, Height:4, Border:1}` (InnerHeight 2) — za mało wierszy na brzeg, więc do tych testów użyj wyższego paska.

```go
// paintRows draws a bar whose inner rows have the exact widths given, top to
// bottom, so a test can reproduce the client's antialiased edge (narrower
// first and last row) precisely.
func paintRows(im *image.NRGBA, g vision.Geometry, at image.Point, widths []int, c vision.Color) {
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			im.SetNRGBA(at.X+x, at.Y+y, color.NRGBA{A: 255})
		}
	}
	for dy, w := range widths {
		for x := 0; x < w; x++ {
			im.SetNRGBA(at.X+g.Border+x, at.Y+g.Border+dy,
				color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
}

func TestConfirmEdgeTolerance(t *testing.T) {
	green := vision.DefaultColors()[0]
	// Height 6, border 1 -> 4 inner rows. Core (middle two) is 20; the first
	// and last inner rows are one pixel narrower, exactly like the client's
	// antialiased edge.
	geo := vision.Geometry{Width: 24, Height: 6, Border: 1}
	build := func() (*image.NRGBA, vision.Options) {
		im := darkCanvas(60, 40)
		paintRows(im, geo, image.Pt(10, 10), []int{19, 20, 20, 19}, green)
		o := opts(geo)
		return im, o
	}

	im, o := build()
	o.EdgeTolerance = 0
	if bars := vision.Find(im, o); len(bars) != 0 {
		t.Errorf("przy EdgeTolerance=0 rozmyty brzeg musi odrzucić pasek, dostałem %v", bars)
	}

	im, o = build()
	o.EdgeTolerance = 1
	bars := vision.Find(im, o)
	if len(bars) != 1 {
		t.Fatalf("przy EdgeTolerance=1 pasek z brzegiem o 1 px węższym musi przejść, dostałem %v", bars)
	}
	if bars[0].Fill != 20 {
		t.Errorf("Fill musi być rdzeniem (20), nie brzegiem — dostałem %d", bars[0].Fill)
	}
}

func TestConfirmRejectsWiderEdge(t *testing.T) {
	green := vision.DefaultColors()[0]
	geo := vision.Geometry{Width: 24, Height: 6, Border: 1}
	im := darkCanvas(60, 40)
	// A row WIDER than the core is not antialiasing - it is a different shape.
	paintRows(im, geo, image.Pt(10, 10), []int{21, 20, 20, 19}, green)
	o := opts(geo)
	o.EdgeTolerance = 4
	if bars := vision.Find(im, o); len(bars) != 0 {
		t.Errorf("brzeg szerszy od rdzenia musi odrzucić pasek przy każdej tolerancji, dostałem %v", bars)
	}
}

func TestConfirmRejectsCrookedMiddle(t *testing.T) {
	green := vision.DefaultColors()[0]
	geo := vision.Geometry{Width: 24, Height: 7, Border: 1}
	im := darkCanvas(60, 40)
	// Height 7, border 1 -> 5 inner rows. A middle row differs: that is a
	// projectile or a digit cutting the bar, never antialiasing, so it must be
	// rejected even at the loosest tolerance.
	paintRows(im, geo, image.Pt(10, 10), []int{19, 20, 18, 20, 19}, green)
	o := opts(geo)
	o.EdgeTolerance = 4
	if bars := vision.Find(im, o); len(bars) != 0 {
		t.Errorf("różny wiersz środkowy musi odrzucić pasek, dostałem %v", bars)
	}
}

func TestConfirmRejectsEmptyRow(t *testing.T) {
	green := vision.DefaultColors()[0]
	geo := vision.Geometry{Width: 24, Height: 6, Border: 1}
	im := darkCanvas(60, 40)
	// A zero-width inner row is a failure, not a width eligible for tolerance.
	paintRows(im, geo, image.Pt(10, 10), []int{20, 0, 20, 20}, green)
	o := opts(geo)
	o.EdgeTolerance = 4
	if bars := vision.Find(im, o); len(bars) != 0 {
		t.Errorf("pusty wiersz wnętrza musi odrzucić pasek, dostałem %v", bars)
	}
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie przechodzą (i że nie kompilują się)**

Run: `go test ./internal/vision/ -run TestConfirm -v`
Expected: błąd kompilacji `unknown field EdgeTolerance` — pole i nowa sygnatura jeszcze nie istnieją.

- [ ] **Step 3: Dodaj pole `EdgeTolerance` do `Options`**

W `internal/vision/bars.go`, w strukturze `Options`, po `BlackMax`:

```go
	// EdgeTolerance is how many pixels narrower than the core the first and
	// last inner rows may be. The client antialiases the edge of a bar, and
	// the antialiased row blends toward the dark background, so it measures
	// shorter - never longer. Zero, the default, demands the exact match the
	// detector always demanded.
	EdgeTolerance int
```

- [ ] **Step 4: Przepisz `confirm` na rdzeń plus brzeg i zmień jego wywołanie w `Find`**

W `internal/vision/bars.go` zastąp całą funkcję `confirm`:

```go
// confirm re-measures the whole rectangle and returns the core fill width.
// The core is the run shared by the middle rows; the first and last inner
// rows may be up to EdgeTolerance pixels narrower (never wider), which is how
// the client's antialiased edge looks. With fewer than three inner rows there
// is no middle to trust, so every row must match exactly, EdgeTolerance or
// not. The border stays dark all the way round, unchanged - it is what still
// stops the same bar being found a second time one row down.
func (o Options) confirm(im *image.NRGBA, bar Bar) (int, bool) {
	g := o.Geometry
	whole := image.Rect(bar.X, bar.Y, bar.X+g.Width, bar.Y+g.Height)
	if !whole.In(im.Bounds()) {
		return 0, false
	}
	inner := g.InnerHeight()
	runs := make([]int, inner)
	for dy := 0; dy < inner; dy++ {
		r := o.fillRun(im, bar.X+g.Border, bar.Y+g.Border+dy)
		if r == 0 {
			return 0, false
		}
		runs[dy] = r
	}
	coreStart, coreEnd := 0, inner
	if inner >= 3 {
		coreStart, coreEnd = 1, inner-1
	}
	core := runs[coreStart]
	for i := coreStart; i < coreEnd; i++ {
		if runs[i] != core {
			return 0, false
		}
	}
	if inner >= 3 {
		for _, edge := range []int{runs[0], runs[inner-1]} {
			if d := core - edge; d < 0 || d > o.EdgeTolerance {
				return 0, false
			}
		}
	}
	for dy := 0; dy < g.Border; dy++ {
		for dx := 0; dx < g.Width; dx++ {
			if !o.isDark(im, bar.X+dx, bar.Y+dy) {
				return 0, false
			}
			if !o.isDark(im, bar.X+dx, bar.Y+g.Height-1-dy) {
				return 0, false
			}
		}
	}
	return core, true
}
```

W `Find` zmień blok wykrycia (usuń `run` z konstrukcji `Bar` i przypisz `Fill` z `confirm`):

```go
			run := o.fillRun(im, x, y)
			if run == 0 {
				continue
			}
			bar := Bar{X: x - g.Border, Y: y - g.Border}
			fill, ok := o.confirm(im, bar)
			if !ok {
				continue
			}
			bar.Fill = fill
			if o.excluded(bar) {
				continue
			}
			out = append(out, bar)
```

- [ ] **Step 5: Uruchom nowe testy i sprawdź, że przechodzą**

Run: `go test ./internal/vision/ -run TestConfirm -v`
Expected: PASS (cztery testy).

- [ ] **Step 6: Uruchom cały pakiet — istniejące testy muszą przejść bez zmian**

Run: `go test ./internal/vision/ -race`
Expected: PASS. `EdgeTolerance` zeruje się domyślnie, więc `TestFind` i reszta zachowują się dokładnie jak dotąd.

- [ ] **Step 7: Commit**

```bash
git add internal/vision/bars.go internal/vision/bars_test.go
git commit -m "Toleruj rozmyty brzeg paska w confirm()"
```

---

### Task 2: Osobna tolerancja barw dla battle listy w `CombatConfig`

Battle lista potrzebuje tolerancji 80, okno gry 20; dziś to jedno pole. Zadanie rozdziela tolerancję, próg czerni i tolerancję brzegu na dwie pary, podnosi sufit szerokości paska (battle ma 262 px) i przenosi nowe pola do opcji obu detektorów.

**Files:**
- Modify: `internal/brain/combatconfig.go` (typ `CombatConfig`, `withDefaults`, `validate`, `checkBar`, `barOptions`, `battleOptions`)
- Test: `internal/brain/combatconfig_test.go`

**Interfaces:**
- Consumes: `vision.Options.EdgeTolerance` z Task 1.
- Produces: pola JSON `bar_edge_tolerance`, `battle_bar_tolerance`, `battle_black_max`, `battle_edge_tolerance` w `CombatConfig`; `barOptions()` ustawia `EdgeTolerance` z `BarEdgeTolerance`, `battleOptions()` ustawia `Tolerance/BlackMax/EdgeTolerance` z pól battle.

- [ ] **Step 1: Dodaj nieprzechodzące testy nowych pól**

W `internal/brain/combatconfig_test.go` dopisz:

```go
func TestCombatConfigBattleToleranceDefaults(t *testing.T) {
	c := calibrated().withDefaults()
	if c.BattleBarTolerance != 80 {
		t.Errorf("domyślna tolerancja battle %d, oczekiwano 80", c.BattleBarTolerance)
	}
	if c.BattleBlackMax != 48 {
		t.Errorf("domyślny próg czerni battle %d, oczekiwano 48", c.BattleBlackMax)
	}
	if c.BattleEdgeTolerance != 1 {
		t.Errorf("domyślna tolerancja brzegu battle %d, oczekiwano 1", c.BattleEdgeTolerance)
	}
	// The game-window edge tolerance stays zero: zero is a legal, meaningful
	// value there, so withDefaults must not promote it.
	if c.BarEdgeTolerance != 0 {
		t.Errorf("tolerancja brzegu okna gry %d, oczekiwano 0", c.BarEdgeTolerance)
	}
}

func TestCombatConfigWideBattleBarAccepted(t *testing.T) {
	c := calibrated()
	c.BattleBarWidth = 262 // real 5K battle bar, wider than the old 256 cap
	if err := c.withDefaults().validate(); err != nil {
		t.Fatalf("pasek battle 262 px odrzucony: %v", err)
	}
}

func TestCombatConfigBattleBlackMaxAbsorbsColour(t *testing.T) {
	c := calibrated()
	// Battle pair must run the same "colour not swallowed" check as the game
	// window: darkest default channel 133 minus battle tolerance must stay
	// above battle black max.
	c.BattleBarTolerance = 80
	c.BattleBlackMax = 120
	if err := c.withDefaults().validate(); err == nil {
		t.Fatal("próg czerni battle pochłaniający barwę musi być odrzucony")
	}
}
```

Rozszerz też `TestCombatConfigRejections` o dwa wiersze w tabeli `tests`:

```go
		{"tolerancja brzegu poza zakresem", func(c *CombatConfig) { c.BattleEdgeTolerance = 9 }, "tolerancja brzegu"},
		{"tolerancja battle poza zakresem", func(c *CombatConfig) { c.BattleBarTolerance = 200 }, "tolerancja"},
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie przechodzą**

Run: `go test ./internal/brain/ -run TestCombatConfig -v`
Expected: błąd kompilacji `unknown field BattleBarTolerance` (i pozostałe nowe pola).

- [ ] **Step 3: Dodaj pola do struktury `CombatConfig`**

W `internal/brain/combatconfig.go`, w `CombatConfig`, po `BarTolerance`/`BlackMax` dołóż `BarEdgeTolerance`, a przy polach battle (po `BattleFrameCoverage`) dołóż trzy pola battle:

```go
	BarEdgeTolerance int `json:"bar_edge_tolerance"`
	// ...
	BattleBarTolerance  int `json:"battle_bar_tolerance"`
	BattleBlackMax      int `json:"battle_black_max"`
	BattleEdgeTolerance int `json:"battle_edge_tolerance"`
```

- [ ] **Step 4: Uzupełnij `withDefaults` o domyślne battle**

W `internal/brain/combatconfig.go`, w `withDefaults()`, przed `return c` dołóż (BarEdgeTolerance celowo nie dostaje domyślnej — zero jest tam prawidłowe):

```go
	if c.BattleBarTolerance == 0 {
		c.BattleBarTolerance = 80
	}
	if c.BattleBlackMax == 0 {
		c.BattleBlackMax = 48
	}
	if c.BattleEdgeTolerance == 0 {
		c.BattleEdgeTolerance = 1
	}
```

- [ ] **Step 5: Podnieś sufit szerokości w `checkBar` i rozszerz `validate`**

W `checkBar` zmień sufit szerokości z 256 na 1024:

```go
func checkBar(what string, w, h, border int) error {
	if w < 3 || w > 1024 || h < 3 || h > 64 {
		return fmt.Errorf("wymiary %s muszą mieścić się w zakresie 3–1024 na 3–64 px", what)
	}
	if border < 1 || border > 8 || 2*border >= w || 2*border >= h {
		return fmt.Errorf("obwódka %s musi mieć 1–8 px i zostawić miejsce na wypełnienie", what)
	}
	return nil
}
```

W `validate()`, tuż po istniejącej pętli sprawdzającej pochłanianie barwy dla `BarColors` (ta używa `c.BarTolerance`/`c.BlackMax`), dołóż walidację tolerancji brzegu okna gry i — gdy `Battle` niepuste — pary battle. Wstaw to w bloku `if !c.Battle.Empty() { ... }`, obok istniejących sprawdzeń battle:

```go
	if c.BarEdgeTolerance < 0 || c.BarEdgeTolerance > 4 {
		return fmt.Errorf("tolerancja brzegu paska musi mieścić się w zakresie 0–4")
	}
```

a wewnątrz `if !c.Battle.Empty()`:

```go
		if c.BattleBarTolerance < 1 || c.BattleBarTolerance > 128 || c.BattleBlackMax < 1 || c.BattleBlackMax > 128 {
			return fmt.Errorf("tolerancja i próg czerni battle listy muszą mieścić się w zakresie 1–128")
		}
		if c.BattleEdgeTolerance < 0 || c.BattleEdgeTolerance > 4 {
			return fmt.Errorf("tolerancja brzegu battle listy musi mieścić się w zakresie 0–4")
		}
		for _, s := range c.BarColors {
			col, err := parseColor(s)
			if err != nil {
				return fmt.Errorf("barwa paska %q: %w", s, err)
			}
			if mc := maxChannel(col); mc-c.BattleBarTolerance <= c.BattleBlackMax {
				return fmt.Errorf(
					"barwa paska %q na battle liście: próg czerni %d razem z tolerancją %d pochłania jej wypełnienie (największy kanał %d)",
					s, c.BattleBlackMax, c.BattleBarTolerance, mc)
			}
		}
```

- [ ] **Step 6: Przenieś nowe pola do opcji obu detektorów**

W `barOptions()` dołóż `EdgeTolerance: c.BarEdgeTolerance` do konstrukcji `vision.Options`.

W `battleOptions()` zmień źródło tolerancji i progu czerni z pól okna gry na pola battle. **Tolerancji brzegu battle listy tu jeszcze nie dodawaj** — pole `EdgeTolerance` w `battle.Options` powstaje dopiero w Task 3, więc dodanie go teraz nie skompiluje się. Ten krok zmienia wyłącznie `Tolerance` i `BlackMax`:

```go
	o := battle.Options{
		Geometry: vision.Geometry{Width: c.BattleBarWidth, Height: c.BattleBarHeight,
			Border: c.BattleBarBorder},
		Tolerance: c.BattleBarTolerance, BlackMax: c.BattleBlackMax,
		RowPitch: c.BattleRowPitch,
		Frame:    frame, FrameTolerance: c.BattleFrameTolerance,
		FrameCoverage: c.BattleFrameCoverage,
	}
```

Tolerancję brzegu i prostokąt ikonki dokłada do tej konstrukcji Task 3, Step 6.

- [ ] **Step 7: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/brain/ -run TestCombatConfig -race -v`
Expected: PASS, łącznie z nowymi.

- [ ] **Step 8: Commit**

```bash
git add internal/brain/combatconfig.go internal/brain/combatconfig_test.go
git commit -m "Rozdziel tolerancję barw okna gry i battle listy"
```

---

### Task 3: Ramka celu szukana wokół ikonki

Ramka celu to czerwony kwadrat wokół ikonki stwora, przesunięty w lewo od paska. Dziś `framed()` mierzy pokrycie jako ułamek szerokości całego wycinka i szuka w pasie wiersza — przez co ramki nigdy nie znajduje. Zadanie daje prostokąt ikonki i tolerancję brzegu battle listy.

**Files:**
- Modify: `internal/battle/list.go` (typ `Options`, `Read`, `framed`)
- Modify: `internal/brain/combatconfig.go` (pola ikonki, `validate`, `battleOptions`)
- Test: `internal/battle/list_test.go`, `internal/brain/combatconfig_test.go`

**Interfaces:**
- Consumes: `vision.Options.EdgeTolerance` (Task 1), pola battle z Task 2.
- Produces: `battle.Options.EdgeTolerance int`, `battle.Options.IconOffsetX/IconOffsetY/IconSize int`; `CombatConfig` pola `battle_icon_offset_x/y`, `battle_icon_size`.

- [ ] **Step 1: Napisz nieprzechodzący test ramki wokół ikonki**

W `internal/battle/list_test.go` zastąp helper `frame` funkcją rysującą kwadratową ramkę wokół ikonki i dopisz test. `mini` to `vision.Geometry{Width:20, Height:3, Border:1}`, `pitch` to 22.

```go
// iconFrame draws the attack border as a square around the creature icon,
// offset from the bar the way the client draws it: to the left of the bar,
// centred on the same rows.
func iconFrame(im *image.NRGBA, barAt image.Point, offX, offY, size int) {
	x0, y0 := barAt.X+offX, barAt.Y+offY
	for i := 0; i < size; i++ {
		set := func(x, y int) { im.SetNRGBA(x, y, color.NRGBA{R: frameColor.R, G: frameColor.G, B: frameColor.B, A: 255}) }
		set(x0+i, y0)          // top edge
		set(x0+i, y0+size-1)   // bottom edge
		set(x0, y0+i)          // left edge
		set(x0+size-1, y0+i)   // right edge
	}
}

func iconOpts() battle.Options {
	o := opts()
	o.IconOffsetX, o.IconOffsetY, o.IconSize = -14, -6, 12
	o.FrameCoverage = 0.8
	return o
}

func TestReadFindsTargetFrameAroundIcon(t *testing.T) {
	im := canvas(60, 100)
	paint(im, image.Pt(30, 10), 18)
	paint(im, image.Pt(30, 10+pitch), 9)
	// Frame around the icon of the SECOND row's bar (top-left corner at
	// (30, 10+pitch) minus the icon offset).
	iconFrame(im, image.Pt(30, 10+pitch), -14, -6, 12)
	list := battle.Read(im, iconOpts())
	if len(list.Rows) != 2 {
		t.Fatalf("odczytano %d wierszy, oczekiwano 2", len(list.Rows))
	}
	if list.Rows[0].Targeted {
		t.Error("pierwszy wiersz nie powinien mieć ramki")
	}
	if !list.Rows[1].Targeted {
		t.Error("drugi wiersz powinien mieć ramkę wokół ikonki")
	}
}
```

Usuń stary helper `frame` i dwa testy, które go używały w sposób sprzeczny z nową geometrią (`TestReadFindsTargetFrame`, `TestReadFrameOnRowBoundary`): zastąp je powyższym oraz testem, że ikonka sąsiedniego wiersza nie zalicza:

```go
func TestReadFrameDoesNotLeakToNeighbour(t *testing.T) {
	im := canvas(60, 100)
	paint(im, image.Pt(30, 10), 18)
	paint(im, image.Pt(30, 10+pitch), 9)
	// Frame around the FIRST row's icon must not mark the second row: icon
	// squares of adjacent rows do not overlap (size 12 < pitch 22).
	iconFrame(im, image.Pt(30, 10), -14, -6, 12)
	list := battle.Read(im, iconOpts())
	if !list.Rows[0].Targeted {
		t.Error("pierwszy wiersz powinien mieć ramkę")
	}
	if list.Rows[1].Targeted {
		t.Error("drugi wiersz nie powinien złapać ramki sąsiada")
	}
}
```

- [ ] **Step 2: Uruchom test i sprawdź, że nie przechodzi**

Run: `go test ./internal/battle/ -run TestReadFinds -v`
Expected: błąd kompilacji `unknown field IconOffsetX`.

- [ ] **Step 3: Dodaj pola do `battle.Options` i przekaż tolerancję brzegu do detektora**

W `internal/battle/list.go`, w `Options`, dołóż tolerancję brzegu (przekazywaną do `vision.Find`) i prostokąt ikonki:

```go
	// EdgeTolerance is forwarded to vision.Find: the client antialiases the
	// battle-list bar edges too.
	EdgeTolerance int
	// IconOffsetX/Y place the creature icon's top-left corner relative to the
	// bar's, and IconSize is the icon's side. The client draws the attack
	// frame round the icon, not round the row, so that square is where the
	// frame is looked for.
	IconOffsetX, IconOffsetY, IconSize int
```

W `Read`, przekaż `EdgeTolerance` do `vision.Options`:

```go
	bars := vision.Find(im, vision.Options{
		Geometry: o.Geometry, Colors: o.Colors,
		Tolerance: o.Tolerance, BlackMax: o.BlackMax, EdgeTolerance: o.EdgeTolerance,
	})
```

- [ ] **Step 4: Przepisz `framed` na prostokąt ikonki**

Zastąp `framed` (pas wiersza znika; `frameRun` liczy bieg tylko w kolumnach ikonki):

```go
// framed looks for the attack border in the square around this bar's creature
// icon. The client draws the frame round the icon, which sits at a fixed
// offset from the bar, so a run of the frame colour covering FrameCoverage of
// the icon's width on any one of its rows means this entry is the target.
// Icon squares of adjacent rows do not overlap, so at most one row is framed.
func (o Options) framed(im *image.NRGBA, b vision.Bar) bool {
	if o.IconSize < 1 {
		return false
	}
	want := int(o.FrameCoverage * float64(o.IconSize))
	if want < 1 {
		want = 1
	}
	x0 := b.X + o.IconOffsetX
	y0 := b.Y + o.IconOffsetY
	for y := y0; y < y0+o.IconSize; y++ {
		if o.frameRun(im, y, x0, x0+o.IconSize) >= want {
			return true
		}
	}
	return false
}

// frameRun is the longest unbroken run of the frame colour on one line, within
// the given x range.
func (o Options) frameRun(im *image.NRGBA, y, x0, x1 int) int {
	b := im.Bounds()
	if y < b.Min.Y || y >= b.Max.Y {
		return 0
	}
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	best, run := 0, 0
	for x := x0; x < x1; x++ {
		c := im.NRGBAAt(x, y)
		if near(c.R, o.Frame.R, o.FrameTolerance) &&
			near(c.G, o.Frame.G, o.FrameTolerance) &&
			near(c.B, o.Frame.B, o.FrameTolerance) {
			run++
			if run > best {
				best = run
			}
			continue
		}
		run = 0
	}
	return best
}
```

- [ ] **Step 5: Uruchom testy battle i sprawdź, że przechodzą**

Run: `go test ./internal/battle/ -race -v`
Expected: PASS. `TestReadCountsRowsTopDown` i pozostałe niezwiązane z ramką dalej działają (nikt nie jest tam atakowany).

- [ ] **Step 6: Dodaj pola ikonki do `CombatConfig` i przekaż je w `battleOptions`**

W `internal/brain/combatconfig.go`, w `CombatConfig` po polach battle:

```go
	BattleIconOffsetX int `json:"battle_icon_offset_x"`
	BattleIconOffsetY int `json:"battle_icon_offset_y"`
	BattleIconSize    int `json:"battle_icon_size"`
```

W `battleOptions()` dołóż do konstrukcji `battle.Options`:

```go
		EdgeTolerance: c.BattleEdgeTolerance,
		IconOffsetX:   c.BattleIconOffsetX, IconOffsetY: c.BattleIconOffsetY,
		IconSize: c.BattleIconSize,
```

W `validate()`, w bloku `if !c.Battle.Empty()`, dołóż:

```go
		if c.BattleIconSize < 4 || c.BattleIconSize > 256 {
			return fmt.Errorf("bok ikonki celu musi mieścić się w zakresie 4–256 px")
		}
		if c.BattleIconOffsetX < -512 || c.BattleIconOffsetX > 512 ||
			c.BattleIconOffsetY < -512 || c.BattleIconOffsetY > 512 {
			return fmt.Errorf("przesunięcie ikonki celu musi mieścić się w zakresie -512–512 px")
		}
```

- [ ] **Step 7: Dodaj pola ikonki do testu kalibracji i uruchom brain**

W `internal/brain/combatconfig_test.go`, w `calibrated()`, dołóż do literału (żeby pełna kalibracja dalej przechodziła walidację battle):

```go
		BattleIconOffsetX: -14, BattleIconOffsetY: -6, BattleIconSize: 12,
```

Run: `go test ./internal/brain/ -race`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/battle/list.go internal/battle/list_test.go internal/brain/combatconfig.go internal/brain/combatconfig_test.go
git commit -m "Szukaj ramki celu wokół ikonki stwora"
```

---

### Task 4: Fixture i testy na prawdziwej klatce

Testy realnej klatki używają sztywnych geometrii `classic`/`mini`, które nie pasują do żadnego klienta. Zadanie przenosi zmierzoną kalibrację do `CombatFixture` i przełącza cztery testy realnej klatki na jej odczyt.

**Files:**
- Modify: `internal/testenv/combatfixture.go` (`CombatFixture`, `CombatCalibration`)
- Modify: `internal/vision/bars_test.go` (`TestFindOnRealCapture`, `TestRealCaptureOffsets`)
- Modify: `internal/battle/list_test.go` (`TestReadOnRealCapture`)
- Modify: `internal/vitals/bar_test.go` (`TestReadOnRealCapture`)
- Asset: `testdata/combat-capture.png` (już w katalogu roboczym, nieśledzony)

**Interfaces:**
- Consumes: pola z Tasków 1–3.
- Produces: `CombatFixture` pola `BarGeometry`, `BarTolerance`, `BlackMax`, `BarEdge`, `BattleGeometry`, `BattleTolerance`, `BattleBlackMax`, `BattleEdge`, `RowPitch`, `Frame vision.Color`, `FrameTolerance`, `FrameCoverage`, `IconOffsetX/Y`, `IconSize`.

- [ ] **Step 1: Zdecyduj o śledzeniu fixture i dodaj plik do gita**

`testdata/combat-capture.png` (12,4 MB) jest w katalogu, nieśledzony. Repo trzyma już dwa PNG-i (`venore-*`), a notatka użytkownika mówi, że binaria w repo są OK. Dodaj:

```bash
git add -f testdata/combat-capture.png
```

- [ ] **Step 2: Rozszerz `CombatFixture` i wypełnij `CombatCalibration`**

W `internal/testenv/combatfixture.go` dołóż pola do `CombatFixture`:

```go
	BarGeometry     vision.Geometry
	BarTolerance    int
	BlackMax        int
	BarEdge         int
	BattleGeometry  vision.Geometry
	BattleTolerance int
	BattleBlackMax  int
	BattleEdge      int
	RowPitch        int
	Frame           vision.Color
	FrameTolerance  int
	FrameCoverage   float64
	IconOffsetX     int
	IconOffsetY     int
	IconSize        int
```

Dodaj import `"minimap-lab/internal/vision"`. Wypełnij `CombatCalibration()` zmierzonymi liczbami z 5K:

```go
func CombatCalibration() CombatFixture {
	return CombatFixture{
		Viewport: image.Rect(637, 245, 3778, 2548),
		Crop:     image.Rect(1056, 245, 3359, 2548),
		Battle:   image.Rect(4770, 900, 5100, 1150),
		HP:       image.Rect(24, 134, 2202, 136),
		Mana:     image.Rect(2217, 134, 4392, 136),
		GridCols: 15,
		GridRows: 11,
		// The character's own bar was drawn blue in this capture (a hover
		// marker, not the green health bar), so this point is measured from
		// pixels, not from a detected bar. The offset tests only bounds-check
		// the anchor derived from it, which tolerates the small imprecision.
		SelfBar:   image.Point{X: 1070, Y: 985},
		Monsters:  3,
		TargetRow: 0,

		BarGeometry:  vision.Geometry{Width: 62, Height: 8, Border: 3},
		BarTolerance: 20,
		BlackMax:     125,
		BarEdge:      0,

		BattleGeometry:  vision.Geometry{Width: 262, Height: 8, Border: 1},
		BattleTolerance: 80,
		BattleBlackMax:  48,
		BattleEdge:      1,
		RowPitch:        44,
		Frame:           vision.Color{R: 0xc9, G: 0x0a, B: 0x0a},
		FrameTolerance:  40,
		FrameCoverage:   0.8,
		IconOffsetX:     -45,
		IconOffsetY:     -31,
		IconSize:        40,
	}
}
```

- [ ] **Step 3: Przełącz `TestFindOnRealCapture` na fixture**

W `internal/vision/bars_test.go` zmień `TestFindOnRealCapture`, żeby budował opcje z fixture zamiast z `classic`:

```go
func TestFindOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.NRGBACrop(t, testenv.CombatCapture(t), fx.Crop)
	o := vision.Options{
		Geometry: fx.BarGeometry, Colors: vision.DefaultColors(),
		Tolerance: fx.BarTolerance, BlackMax: fx.BlackMax,
		EdgeTolerance: fx.BarEdge, ExcludeTolerance: 2,
	}
	if fx.SelfBar != (image.Point{}) {
		o.Exclude = []image.Point{fx.SelfBar}
	}
	bars := vision.Find(im, o)
	if len(bars) != fx.Monsters {
		t.Errorf("na prawdziwej klatce znaleziono %d pasków stworów, człowiek policzył %d: %v",
			len(bars), fx.Monsters, bars)
	}
	t.Logf("paski na prawdziwej klatce: %v", bars)
}
```

W `TestRealCaptureOffsets` zmień budowę `vision.Grid` i opcji, żeby brały geometrię i tolerancje z fixture (`classic` → `fx.BarGeometry`, `opts(classic)` → opcje jak wyżej).

- [ ] **Step 4: Przełącz `TestReadOnRealCapture` w battle na fixture**

W `internal/battle/list_test.go` zmień `TestReadOnRealCapture`, żeby budował `battle.Options` z fixture:

```go
func TestReadOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.NRGBACrop(t, testenv.CombatCapture(t), fx.Battle)
	o := battle.Options{
		Geometry: fx.BattleGeometry, Colors: vision.DefaultColors(),
		Tolerance: fx.BattleTolerance, BlackMax: fx.BattleBlackMax,
		EdgeTolerance: fx.BattleEdge, RowPitch: fx.RowPitch,
		Frame: fx.Frame, FrameTolerance: fx.FrameTolerance, FrameCoverage: fx.FrameCoverage,
		IconOffsetX: fx.IconOffsetX, IconOffsetY: fx.IconOffsetY, IconSize: fx.IconSize,
	}
	list := battle.Read(im, o)
	t.Logf("wiersze na prawdziwej klatce: %+v (przewinięta: %v)", list.Rows, list.Truncated)
	if len(list.Rows) == 0 {
		t.Fatal("na prawdziwej klatce nie znaleziono żadnego wiersza")
	}
	if fx.TargetRow >= 0 {
		if fx.TargetRow >= len(list.Rows) {
			t.Fatalf("człowiek wskazał wiersz %d, odczytano tylko %d", fx.TargetRow, len(list.Rows))
		}
		if !list.Rows[fx.TargetRow].Targeted {
			t.Errorf("wiersz %d nie został rozpoznany jako cel", fx.TargetRow)
		}
		for i, r := range list.Rows {
			if i != fx.TargetRow && r.Targeted {
				t.Errorf("wiersz %d fałszywie rozpoznany jako cel", i)
			}
		}
	}
}
```

- [ ] **Step 5: Uruchom cztery testy realnej klatki**

Run: `go test ./internal/vision/ ./internal/battle/ ./internal/vitals/ ./internal/testenv/ -run 'RealCapture|CombatFixture' -v`
Expected: PASS. `internal/vitals` `TestReadOnRealCapture` czyta HP ≈ 91,9 % i manę 100 % z prostokątów `Rect(24,134,2202,136)` / `Rect(2217,134,4392,136)` — 2 wiersze nad cyframi. `TestReadOnRealCapture` w battle daje 3 wiersze, `TargetRow=0`. `TestFindOnRealCapture` daje 3 paski. `TestRealCaptureOffsets` nie przekracza granic wycinka.

- [ ] **Step 6: Uruchom całość Go**

Run: `go test ./... -race`
Expected: PASS. Nic już się nie pomija — plik fixture jest w repo.

- [ ] **Step 7: Commit**

```bash
git add testdata/combat-capture.png internal/testenv/combatfixture.go internal/vision/bars_test.go internal/battle/list_test.go internal/vitals/bar_test.go
git commit -m "Wpnij zmierzoną kalibrację 5K w testy prawdziwej klatki"
```

---

### Task 5: Panel — nowe pola kalibracji i dokumentacja

Panel jest jedynym miejscem, gdzie człowiek wpisuje kalibrację. Zadanie dokłada pola dla tolerancji brzegu okna gry, tolerancji/progu/brzegu battle listy i prostokąta ikonki, pamięta je po reloadzie i opisuje w README.

**Files:**
- Modify: `web/index.html` (pola sekcji Walka)
- Modify: `web/vision.js` (`combatConfig()`)
- Modify: `web/form.js` (`REMEMBERED`)
- Modify: `README.md` (sekcja „Tolerancje i próg czerni", etykieta ramki)
- Test: `webtests/vision_test.mjs`

**Interfaces:**
- Consumes: klucze JSON z Tasków 2–3 (`bar_edge_tolerance`, `battle_bar_tolerance`, `battle_black_max`, `battle_edge_tolerance`, `battle_icon_offset_x/y`, `battle_icon_size`).
- Produces: pola HTML `bar-edge`, `battle-tolerance`, `battle-black-max`, `battle-edge`, `battle-icon-x`, `battle-icon-y`, `battle-icon-size`.

- [ ] **Step 1: Napisz nieprzechodzący test kształtu configu**

W `webtests/vision_test.mjs` dopisz test, że nowe pola trafiają do configu z ich wartościami:

```js
test('nowe pola tolerancji i ikonki jadą w konfiguracji', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  p.el('battle-tolerance').value = '80';
  p.el('battle-black-max').value = '48';
  p.el('battle-edge').value = '1';
  p.el('bar-edge').value = '0';
  p.el('battle-icon-x').value = '-45';
  p.el('battle-icon-y').value = '-31';
  p.el('battle-icon-size').value = '40';
  p.el('battle-tolerance').fire('input');
  await p.settled();

  const combat = lastConfig(p).brain.combat;
  assert.equal(combat.battle_bar_tolerance, 80);
  assert.equal(combat.battle_black_max, 48);
  assert.equal(combat.battle_edge_tolerance, 1);
  assert.equal(combat.bar_edge_tolerance, 0);
  assert.equal(combat.battle_icon_offset_x, -45);
  assert.equal(combat.battle_icon_offset_y, -31);
  assert.equal(combat.battle_icon_size, 40);
});
```

- [ ] **Step 2: Uruchom test i sprawdź, że nie przechodzi**

Run: `node --test webtests/vision_test.mjs`
Expected: FAIL — `p.el('battle-tolerance')` nie istnieje albo pola nie ma w configu.

- [ ] **Step 3: Dodaj pola do HTML**

W `web/index.html`, w sekcji `panel-walka`: dołóż `Tolerancja brzegu` do siatki z tolerancją okna gry (po `black-max`):

```html
          <label>Tolerancja brzegu<input id="bar-edge" type="number" min="0" max="4" value="0"></label>
```

W siatce battle (po `battle-frame-coverage`) dołóż:

```html
          <label>Battle: tolerancja barw<input id="battle-tolerance" type="number" min="1" max="128" value="80"></label>
          <label>Battle: próg czerni<input id="battle-black-max" type="number" min="1" max="128" value="48"></label>
          <label>Battle: tolerancja brzegu<input id="battle-edge" type="number" min="0" max="4" value="1"></label>
          <label>Ikonka: przesunięcie X<input id="battle-icon-x" type="number" min="-512" max="512" value="-45"></label>
          <label>Ikonka: przesunięcie Y<input id="battle-icon-y" type="number" min="-512" max="512" value="-31"></label>
          <label>Ikonka: bok<input id="battle-icon-size" type="number" min="4" max="256" value="40"></label>
```

Zmień etykietę pokrycia ramki na jednoznaczną:

```html
          <label>Battle: pokrycie ramki (ułamek boku ikonki)<input id="battle-frame-coverage" type="number" min="0.05" max="1" step="0.05" value="0.8"></label>
```

- [ ] **Step 4: Wstaw pola do `combatConfig()`**

W `web/vision.js`, w `combatConfig()`, dołóż do zwracanego obiektu (i przestań dublować `battle_frame_tolerance` z `bar-tolerance` — czytaj go z własnego pola battle tolerancji, bo to teraz osobny dial):

```js
      bar_edge_tolerance: num('bar-edge'),
      battle_bar_tolerance: num('battle-tolerance'),
      battle_black_max: num('battle-black-max'),
      battle_edge_tolerance: num('battle-edge'),
      battle_frame_tolerance: num('battle-tolerance'),
      battle_icon_offset_x: num('battle-icon-x'),
      battle_icon_offset_y: num('battle-icon-y'),
      battle_icon_size: num('battle-icon-size'),
```

(`battle_frame_tolerance` dalej dzieli pole — teraz z tolerancją battle, nie okna gry; ramka i pasek battle listy mają wspólny dial, jak dotąd, tylko przeniesiony na właściwą rodzinę.)

- [ ] **Step 5: Zapamiętaj nowe pola po reloadzie**

W `web/form.js`, do tablicy `REMEMBERED`, dołóż nowe id (do grupy battle):

```js
  'bar-edge', 'battle-tolerance', 'battle-black-max', 'battle-edge',
  'battle-icon-x', 'battle-icon-y', 'battle-icon-size',
```

- [ ] **Step 6: Uruchom test panelu i sprawdź, że przechodzi**

Run: `node --test webtests/vision_test.mjs`
Expected: PASS, łącznie z nowym testem.

- [ ] **Step 7: Zaktualizuj README**

W `README.md`, w sekcji „Tolerancje i próg czerni", dopisz akapit o rozdzieleniu par i tolerancji brzegu:

```markdown
Okno gry i battle lista mają **osobne** tolerancje barw i progi czerni, bo klient rysuje jedno i drugie inaczej: pasek nad stworem ma grubą obwódkę, która wchłania rozmyte brzegi, a mini-pasek na liście — cienką, więc jego rozmyty brzeg trzeba tolerować wprost. Stąd `bar_edge_tolerance` i `battle_edge_tolerance` (0–4 piksele): o tyle skrajny wiersz wypełnienia może być węższy od rdzenia, nigdy szerszy. Wiersze środkowe muszą się zgadzać co do piksela — to one odrzucają pocisk albo cyfrę obrażeń przecinającą pasek.
```

Zmień opis ramki celu w sekcji widzenia (ramka wokół ikonki, nie wiersza):

```markdown
Ramkę celu klient rysuje jako kwadrat wokół **ikonki** stwora w battle liście, nie wokół jego paska. Panel opisuje ten kwadrat trzema polami: przesunięciem ikonki względem paska (`Ikonka: przesunięcie X/Y`) i jej bokiem (`Ikonka: bok`). Pokrycie ramki liczy się jako ułamek boku ikonki, nie szerokości wycinka.
```

- [ ] **Step 8: Uruchom całość ostatni raz**

Run: `go test ./... -race && node --test webtests/*.mjs`
Expected: PASS w obu.

- [ ] **Step 9: Commit**

```bash
git add web/index.html web/vision.js web/form.js webtests/vision_test.mjs README.md
git commit -m "Wystaw w panelu tolerancje brzegu i prostokąt ikonki"
```
