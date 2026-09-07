# Warstwa widzenia (faza 1 modułu walki i lootu) — plan wdrożenia

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bot widzi, ile potworów stoi wokół postaci i gdzie dokładnie, ile ma HP i many oraz który wiersz battle listy jest celem — i pokazuje to w panelu, nie wciskając ani jednego klawisza.

**Architecture:** Trzy nowe pakiety są czystymi funkcjami piksel → liczba, bez stanu i bez zegara: `internal/vision` (paski życia nad stworami plus przeliczenie na frakcyjne kratki), `internal/battle` (wiersze listy i ramka celu), `internal/vitals` (procent z paska klienta). Panel dorzuca dwa nowe regiony do istniejącego `POST /api/frame` i wypełnia dwa już zdefiniowane. Mózg woła detektory w `handleFrame`, publikuje same skalary w snapshocie, a rysunki diagnostyczne wydaje osobnym `GET /api/vision`. Nic w tej fazie nie dotyka `Followera`, `Executora` ani sterownika.

**Tech Stack:** Go 1.22+ (biblioteka standardowa, `image`/`image/png`), testy `go test` tabelkowe; panel w czystym JS bez zależności, testy `node --test webtests/*.cjs`.

**Spec:** `docs/superpowers/specs/2026-09-07-combat-and-loot-design.md`

## Global Constraints

- **Wszystkie komunikaty dla użytkownika po polsku**, z pełną diakrytyką — tak jak każdy `fmt.Errorf` w tym repo. Komentarze w kodzie po angielsku, jak w istniejących plikach.
- **Zero nowych zależności.** Projekt trzyma dokładnie jedną (`purego`) i to się nie zmienia.
- **`internal/vision`, `internal/battle`, `internal/vitals` nie mają stanu ani zegara.** Pamięć o poprzednich klatkach powstanie w fazie 3 w `internal/combat`.
- **`internal/combat` nie importuje `internal/brain`** (cykl importów). Nie dotyczy tej fazy, ale nie twórz zależności w tę stronę.
- **Snapshot stanu zostaje mały.** Do `brain.State` wchodzą tylko skalary. Prostokąty pasków jadą wyłącznie przez `GET /api/vision`.
- **Brak regionu w klatce nie jest awarią.** Klatka bez regionu 4 wyłącza widzenie stworów na tę klatkę; pętla dalej lokalizuje postać i idzie po trasie.
- **Geometria paska jest konfiguracją, nie stałą w kodzie.** Nigdzie nie wpisuj `27` ani `4` jako literału w logice.
- **Odległość to Chebyshev**, `max(|dx|, |dy|)`, liczona frakcyjnie, bez zaokrąglania.
- **Testy uruchamiane lokalnie**: `go test ./... -race` i `node --test webtests/*.cjs`. W tym repo nie ma Dockera, świadomie (patrz README).
- Po każdym zadaniu commit. Wiadomości commitów po polsku, w trybie rozkazującym, jak w historii repo.

## Struktura plików

| Plik | Odpowiedzialność |
|---|---|
| `internal/vision/bars.go` (nowy) | detektor pasków życia: `Geometry`, `Color`, `Options`, `Bar`, `Find` |
| `internal/vision/grid.go` (nowy) | `Grid`, `Offset`, `Distance` — z piksela na frakcyjną kratkę |
| `internal/vision/bars_test.go`, `grid_test.go` (nowe) | testy tabelkowe na syntetycznych obrazkach + regresja na prawdziwej klatce |
| `internal/battle/list.go` (nowy) | `Options`, `Row`, `List`, `Read` — wiersze i ramka celu |
| `internal/vitals/bar.go` (nowy) | `Options`, `Reading`, `Read` — procent z paska klienta |
| `internal/frame/frame.go` (zmiana) | `RegionViewport = 4`, `RegionBattle = 5` w `knownRegion` |
| `internal/brain/combatconfig.go` (nowy) | `Rect`, `CombatConfig`, walidacja, konstruktory `Grid`/`Options` dla detektorów |
| `internal/brain/state.go` (zmiana) | `CombatState`, `VisionView`, `BarView`, `RowView` |
| `internal/brain/loop.go` (zmiana) | wołanie detektorów w `handleFrame`, `VisionSnapshot` |
| `internal/testenv/combatfixture.go` (nowy) | `CombatCalibration()` — prostokąty zmierzone na prawdziwej klatce |
| `testdata/combat-capture.png` (nowy) | prawdziwa klatka z gry: potwory na ekranie, cel zaznaczony |
| `visionapi.go` (nowy, katalog główny) | `GET /api/vision` |
| `server.go` (zmiana) | trasa `GET /api/vision` |
| `web/panel.js`, `web/index.html` (zmiany) | kalibracja pięciu prostokątów, wysyłka regionów, podgląd z obrysami, zapis pełnej klatki do PNG |
| `webtests/panel_test.cjs` (zmiana) | testy kalibracji, wyliczania wycinka i zapisu klatki |
| `README.md` (zmiana) | rozdział o kalibracji widzenia i o zmierzonych wartościach |

---

### Task 1: Prawdziwa klatka z gry jako fixture

Bez tego nie ma czym zweryfikować żadnego detektora. Zadanie dokłada do panelu zapis pełnej klatki do PNG, a potem **wymaga Twojego udziału**: musisz zgrać ekran z gry i zmierzyć na nim prostokąty.

**Files:**
- Modify: `web/index.html` (sekcja `toolbar` na górze, obok przycisku `snapshot`)
- Modify: `web/panel.js` (blok „źródło obrazu", po `$('snapshot').onclick`)
- Modify: `webtests/panel_test.cjs` (stub elementu: dodać `toBlob`)
- Create: `testdata/combat-capture.png` (zgrane ręcznie)
- Create: `internal/testenv/combatfixture.go`
- Test: `internal/testenv/combatfixture_test.go`

**Interfaces:**
- Produces: `testenv.CombatCalibration() testenv.CombatFixture` — struktura z prostokątami `Viewport`, `Crop`, `Battle`, `HP`, `Mana` (typ `image.Rectangle`), `GridCols`, `GridRows int`, `SelfBar image.Point`, `Monsters int` (ile potworów było na ekranie), `TargetRow int` (który wiersz miał ramkę, licząc od 0; `-1` gdy żaden). Wszystkie kolejne zadania czytają fixture przez tę funkcję.

- [ ] **Step 1: Napisz nieprzechodzący test JS zapisu klatki**

W `webtests/panel_test.cjs`, obok pozostałych testów:

```javascript
test('zapis pełnej klatki tworzy pobranie w rozdzielczości źródła', async () => {
  const p = panel();
  await p.settled();
  p.el('share').click();
  await p.settled();
  // Podglądamy tworzenie elementów, bo pobranie to element <a> z atrybutem
  // download - w sandboxie nie ma prawdziwego DOM, żeby je zobaczyć inaczej.
  const created = [];
  const make = p.sandbox.document.createElement;
  p.sandbox.document.createElement = tag => {
    const el = make(tag);
    created.push(el);
    return el;
  };
  p.el('frame-save').click();
  await p.settled();
  const link = created.find(el => el.download);
  assert.ok(link, 'nie utworzono odnośnika pobrania');
  assert.equal(link.download, 'combat-capture.png');
  assert.equal(link.href, 'blob:x');
});
```

Uwaga dla wykonawcy: harness w `panel_test.cjs` zwraca już `sandbox` i `el`, więc wystarczy jedna dokładka — do stubu elementu w funkcji `element(id)` dodaj `toBlob(cb) { cb({}); }`. Kanwa `source` powstaje przez `document.createElement`, więc bez tego `source.toBlob` jest `undefined` i przycisk rzuca.

- [ ] **Step 2: Uruchom test i sprawdź, że nie przechodzi**

Run: `node --test webtests/panel_test.cjs`
Expected: FAIL — `p.el('frame-save')` zwraca stub bez `onclick`, nic się nie tworzy, `link` jest `undefined` i asercja `assert.ok` pada.

- [ ] **Step 3: Dodaj przycisk do HTML**

W `web/index.html`, w `<section class="toolbar">`, po przycisku `snapshot`:

```html
    <button id="frame-save" class="secondary" disabled>Zapisz klatkę PNG</button>
```

W `web/panel.js` znajdź obie linie włączające i wyłączające przyciski udostępniania (`$('snapshot').disabled = $('stop').disabled = $('live').disabled = ...`) i dodaj do nich `$('frame-save').disabled`.

- [ ] **Step 4: Dodaj zapis w panelu**

W `web/panel.js`, bezpośrednio po `$('snapshot').onclick = ...`:

```javascript
// Zapis idzie z kanwy źródłowej, a nie z podglądu: podgląd jest przeskalowany
// do 800 px szerokości, a pomiary pikselowe pasków wymagają rozdzielczości,
// w jakiej klient je narysował.
$('frame-save').onclick = () => {
  if (!ready) { status('Najpierw udostępnij ekran albo wczytaj obraz.', 'error'); return; }
  source.toBlob(blob => {
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'combat-capture.png';
    link.click();
    URL.revokeObjectURL(url);
  }, 'image/png');
};
```

- [ ] **Step 5: Uruchom test i sprawdź, że przechodzi**

Run: `node --test webtests/panel_test.cjs`
Expected: PASS, wszystkie testy panelu zielone.

- [ ] **Step 6: Zgraj prawdziwą klatkę — krok ręczny, dla człowieka**

1. Uruchom panel: `go run . -input off` i otwórz `http://localhost:8080`.
2. Wejdź do gry, stań tak, żeby **na ekranie było co najmniej trzy potwory**, w tym jeden przy krawędzi okna gry i najlepiej jeden duży (dwukratkowy sprite).
3. **Zaatakuj jednego** z nich, żeby jego wiersz battle listy dostał ramkę celu.
4. W panelu kliknij „Udostępnij ekran", wybierz **cały ekran**, potem „Zapisz klatkę PNG".
5. Zapisany plik przenieś do `testdata/combat-capture.png`.

- [ ] **Step 7: Zmierz prostokąty — krok ręczny, dla człowieka**

W panelu przeciągnij zaznaczenie po obrazie i odczytaj liczby z napisu pod kanwą („Wycinek: x=…, y=…, … × … px"). Zmierz kolejno i zapisz sobie na boku:

- **okno gry** — sam teren, bez ramki panelu klienta; sprawdź, że stosunek szerokości do wysokości wychodzi około 15:11;
- **battle lista** — obszar z wierszami stworów, bez nagłówka i bez filtrów;
- **pasek HP** i **pasek many** — same paski, bez obwódki i bez cyfr.

Uwaga: przeciąganie ustawia też region minimapy, więc to sesja pomiarowa, nie łowiecka. Policz też, ile dokładnie potworów widać na ekranie i który wiersz listy (od góry, licząc od zera) ma ramkę celu.

- [ ] **Step 8: Napisz nieprzechodzący test fixture'u**

Create `internal/testenv/combatfixture_test.go`:

```go
package testenv_test

import (
	"testing"

	"minimap-lab/internal/testenv"
)

func TestCombatFixtureGeometry(t *testing.T) {
	im := testenv.LoadFixture(t, "combat-capture.png")
	fx := testenv.CombatCalibration()
	bounds := im.Bounds()

	// Każdy zmierzony prostokąt musi leżeć w obrazie, inaczej pomiar jest z
	// innego zrzutu niż zacommitowany plik.
	for name, r := range fx.Rects() {
		if !r.In(bounds) {
			t.Errorf("%s %v wychodzi poza obraz %v", name, r, bounds)
		}
	}
	// Okno gry musi mieć proporcje siatki, z tolerancją na skalowanie klienta.
	want := float64(fx.GridCols) / float64(fx.GridRows)
	got := float64(fx.Viewport.Dx()) / float64(fx.Viewport.Dy())
	if got < want*0.97 || got > want*1.03 {
		t.Errorf("okno gry ma proporcje %.3f, siatka %dx%d oczekuje %.3f",
			got, fx.GridCols, fx.GridRows, want)
	}
	if !fx.Crop.In(fx.Viewport) {
		t.Errorf("wycinek %v nie mieści się w oknie gry %v", fx.Crop, fx.Viewport)
	}
	if fx.Monsters < 3 {
		t.Errorf("fixture ma %d potworów, potrzebne co najmniej 3", fx.Monsters)
	}
}
```

- [ ] **Step 9: Uruchom test i sprawdź, że nie przechodzi**

Run: `go test ./internal/testenv/ -run TestCombatFixtureGeometry -v`
Expected: FAIL — `undefined: testenv.CombatCalibration`.

- [ ] **Step 10: Napisz fixture z prawdziwymi liczbami**

Create `internal/testenv/combatfixture.go`. **Podmień liczby na zmierzone w kroku 7** — poniższe są kształtem, nie pomiarem:

```go
package testenv

import "image"

// CombatFixture carries the panel settings testdata/combat-capture.png was
// captured with, and what a human counted on it. It lives here for the same
// reason VenoreCalibration does: three packages build the same fixture from
// these numbers, and that only works as a cross-check while all of them use
// the same ones.
type CombatFixture struct {
	Viewport image.Rectangle
	Crop     image.Rectangle
	Battle   image.Rectangle
	HP       image.Rectangle
	Mana     image.Rectangle

	GridCols, GridRows int

	// SelfBar is the top-left corner of the character's own health bar inside
	// Crop. The camera is centred on the character, so it never moves.
	SelfBar image.Point

	// Monsters is how many creatures a human counted on the capture, and
	// TargetRow which battle list entry carried the attack frame, counting
	// from zero; -1 when none did.
	Monsters  int
	TargetRow int
}

// Rects names every measured rectangle, for tests that check all of them.
func (f CombatFixture) Rects() map[string]image.Rectangle {
	return map[string]image.Rectangle{
		"okno gry":     f.Viewport,
		"wycinek":      f.Crop,
		"battle lista": f.Battle,
		"pasek HP":     f.HP,
		"pasek many":   f.Mana,
	}
}

// CombatCalibration reads testdata/combat-capture.png correctly.
func CombatCalibration() CombatFixture {
	return CombatFixture{
		Viewport:  image.Rect(0, 0, 0, 0), // ZMIERZ
		Crop:      image.Rect(0, 0, 0, 0), // ZMIERZ
		Battle:    image.Rect(0, 0, 0, 0), // ZMIERZ
		HP:        image.Rect(0, 0, 0, 0), // ZMIERZ
		Mana:      image.Rect(0, 0, 0, 0), // ZMIERZ
		GridCols:  15,
		GridRows:  11,
		SelfBar:   image.Point{}, // ZMIERZONE W ZADANIU 3
		Monsters:  0,             // POLICZ
		TargetRow: -1,            // ODCZYTAJ
	}
}
```

Wycinek liczy się z okna gry, promienia i siatki tym samym wzorem, którego użyje panel: kratka postaci to `(GridCols/2, GridRows/2)` (dzielenie całkowite), rozszerzona o `ceil(promień) + 1 = 5` kratek w każdą stronę i przycięta do okna gry. Przy 15×11 wychodzi 11 kolumn na 11 wierszy, czyli pełna wysokość okna.

- [ ] **Step 11: Uruchom test i sprawdź, że przechodzi**

Run: `go test ./internal/testenv/ -v`
Expected: PASS. Jeśli test proporcji nie przechodzi, pomiar okna gry jest zły albo Twój klient nie pokazuje 15×11 kratek — w drugim przypadku popraw `GridCols`/`GridRows` i zanotuj to, bo unieważnia założenie ze specu.

- [ ] **Step 12: Commit**

```bash
git add web/index.html web/panel.js webtests/panel_test.cjs \
  testdata/combat-capture.png internal/testenv/combatfixture.go \
  internal/testenv/combatfixture_test.go
git commit -m "Dodaj zapis klatki do PNG i prawdziwy fixture walki"
```

---

### Task 2: Detektor pasków życia

Serce fazy. Szuka **wypełnienia**, nie obwódki, i odrzuca pasek przycięty krawędzią wycinka — dlatego panel wycina jedną kratkę więcej niż promień decyzji.

**Files:**
- Create: `internal/vision/bars.go`
- Modify: `internal/testenv/combatfixture.go` (dodać `NRGBACrop`)
- Test: `internal/vision/bars_test.go`

**Interfaces:**
- Consumes: `testenv.CombatCalibration()`, `testenv.LoadFixture` z zadania 1.
- Produces:
  - `vision.Geometry{Width, Height, Border int}` z metodami `InnerWidth() int`, `InnerHeight() int`
  - `vision.Color{R, G, B uint8}`
  - `vision.Options{Geometry Geometry; Colors []Color; Tolerance, BlackMax int; Exclude []image.Point; ExcludeTolerance int}`
  - `vision.Bar{X, Y, Fill int}` z metodą `HP(g Geometry) float64`
  - `vision.Find(im *image.NRGBA, o Options) []Bar`
  - `vision.DefaultColors() []Color`
  - `testenv.NRGBACrop(t testing.TB, im image.Image, r image.Rectangle) *image.NRGBA`

- [ ] **Step 1: Napisz nieprzechodzące testy detektora**

Create `internal/vision/bars_test.go`:

```go
package vision_test

import (
	"image"
	"image/color"
	"testing"

	"minimap-lab/internal/testenv"
	"minimap-lab/internal/vision"
)

var classic = vision.Geometry{Width: 27, Height: 4, Border: 1}

// background is neither a fill colour nor dark, so nothing in an empty image
// can be mistaken for part of a bar.
var background = color.NRGBA{R: 100, G: 100, B: 100, A: 255}

func canvas(w, h int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, background)
		}
	}
	return im
}

// paint draws one bar the way the client does: a black rectangle with a
// coloured prefix inside it. The unfilled remainder stays black, which is why
// the detector's rule is the same at every health level.
func paint(im *image.NRGBA, g vision.Geometry, at image.Point, fill int, c vision.Color) {
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			im.SetNRGBA(at.X+x, at.Y+y, color.NRGBA{A: 255})
		}
	}
	for y := 0; y < g.InnerHeight(); y++ {
		for x := 0; x < fill; x++ {
			im.SetNRGBA(at.X+g.Border+x, at.Y+g.Border+y,
				color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
}

func opts(g vision.Geometry) vision.Options {
	return vision.Options{
		Geometry: g, Colors: vision.DefaultColors(),
		Tolerance: 12, BlackMax: 48, ExcludeTolerance: 2,
	}
}

func TestFind(t *testing.T) {
	green := vision.DefaultColors()[0]
	tests := []struct {
		name  string
		build func() (*image.NRGBA, vision.Options)
		want  []vision.Bar
	}{
		{
			name: "jeden pasek w pełni wypełniony",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), classic.InnerWidth(), green)
				return im, opts(classic)
			},
			want: []vision.Bar{{X: 10, Y: 20, Fill: 25}},
		},
		{
			name: "pasek w połowie",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), 12, green)
				return im, opts(classic)
			},
			want: []vision.Bar{{X: 10, Y: 20, Fill: 12}},
		},
		{
			name: "pasek pusty jest niewidoczny, bo cały jest czarny",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), 0, green)
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "dwa paski obok siebie",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(5, 10), 20, green)
				paint(im, classic, image.Pt(40, 30), 7, green)
				return im, opts(classic)
			},
			want: []vision.Bar{{X: 5, Y: 10, Fill: 20}, {X: 40, Y: 30, Fill: 7}},
		},
		{
			name: "pasek przycięty prawą krawędzią nie jest zgłaszany",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(30, 60)
				paint(im, classic, image.Pt(10, 20), 12, green)
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "pasek przycięty górną krawędzią nie jest zgłaszany",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				// Górna obwódka wypada nad obrazem.
				paint(im, classic, image.Pt(10, -1), 12, green)
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "własny pasek jest wykluczany po dokładnej pozycji",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(46, 28), 25, green)
				o := opts(classic)
				o.Exclude = []image.Point{{X: 46, Y: 28}}
				return im, o
			},
			want: nil,
		},
		{
			name: "potwór kratkę nad postacią nie jest wykluczany razem z własnym paskiem",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 120)
				paint(im, classic, image.Pt(46, 28), 25, green) // własny
				paint(im, classic, image.Pt(46, 60), 18, green) // kratkę niżej
				o := opts(classic)
				o.Exclude = []image.Point{{X: 46, Y: 28}}
				return im, o
			},
			want: []vision.Bar{{X: 46, Y: 60, Fill: 18}},
		},
		{
			name: "barwa poza tolerancją nie jest paskiem",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				paint(im, classic, image.Pt(10, 20), 12, vision.Color{R: 10, G: 10, B: 200})
				return im, opts(classic)
			},
			want: nil,
		},
		{
			name: "przeskalowana geometria działa tym samym kodem",
			build: func() (*image.NRGBA, vision.Options) {
				g := vision.Geometry{Width: 54, Height: 8, Border: 2}
				im := canvas(200, 90)
				paint(im, g, image.Pt(20, 30), 40, green)
				return im, opts(g)
			},
			want: []vision.Bar{{X: 20, Y: 30, Fill: 40}},
		},
		{
			name: "duża plama barwy paska nie jest paskiem",
			build: func() (*image.NRGBA, vision.Options) {
				im := canvas(120, 60)
				for y := 10; y < 50; y++ {
					for x := 10; x < 110; x++ {
						im.SetNRGBA(x, y, color.NRGBA{R: green.R, G: green.G, B: green.B, A: 255})
					}
				}
				return im, opts(classic)
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			im, o := tt.build()
			got := vision.Find(im, o)
			if len(got) != len(tt.want) {
				t.Fatalf("znaleziono %d pasków (%v), oczekiwano %d (%v)",
					len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("pasek %d: %v, oczekiwano %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBarHP(t *testing.T) {
	if hp := (vision.Bar{Fill: 25}).HP(classic); hp != 1 {
		t.Errorf("pełny pasek dał %.3f, oczekiwano 1", hp)
	}
	if hp := (vision.Bar{Fill: 5}).HP(classic); hp < 0.19 || hp > 0.21 {
		t.Errorf("pasek 5/25 dał %.3f, oczekiwano około 0,2", hp)
	}
}

// TestFindOnRealCapture jest progiem regresji: nie sprawdza dokładnej liczby,
// bo własny pasek nie jest jeszcze zmierzony (zadanie 3), tylko czy detektor
// widzi co najmniej tyle stworów, ile policzył człowiek.
func TestFindOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.NRGBACrop(t, testenv.LoadFixture(t, "combat-capture.png"), fx.Crop)
	bars := vision.Find(im, opts(classic))
	if len(bars) < fx.Monsters {
		t.Errorf("na prawdziwej klatce znaleziono %d pasków, człowiek policzył %d potworów; "+
			"sprawdź geometrię, barwy i prostokąt wycinka", len(bars), fx.Monsters)
	}
	t.Logf("paski na prawdziwej klatce: %v", bars)
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie przechodzą**

Run: `go test ./internal/vision/ -v`
Expected: FAIL — pakiet `vision` nie istnieje.

- [ ] **Step 3: Napisz detektor**

Create `internal/vision/bars.go`:

```go
// Package vision reads the health bars the game client draws above every
// creature. It is deliberately the only package that touches game-window
// pixels, and it holds neither state nor a clock: pixels in, bars out.
// Everything that needs memory of what was seen on an earlier frame lives in
// internal/combat.
package vision

import (
	"image"
	"image/color"
)

// Geometry describes one health bar as the client draws it: a black border
// with a coloured prefix inside. The classic numbers are 27x4 with a 1px
// border, but they are a calibration hypothesis rather than a constant - the
// client scales the game window and the operating system scales the client,
// so both can change underneath us.
type Geometry struct {
	Width  int
	Height int
	Border int
}

func (g Geometry) InnerWidth() int  { return g.Width - 2*g.Border }
func (g Geometry) InnerHeight() int { return g.Height - 2*g.Border }

func (g Geometry) valid() bool {
	return g.Border >= 1 && g.InnerWidth() >= 1 && g.InnerHeight() >= 1
}

// Color is one fill colour the client quantises creature health to.
type Color struct{ R, G, B uint8 }

// DefaultColors are the six values the client is known to quantise creature
// health to, brightest first. They are a starting point for calibration, not
// gospel: verify them against a real capture and adjust Tolerance rather than
// assuming a client build matches.
func DefaultColors() []Color {
	return []Color{
		{0x00, 0xBC, 0x00}, {0x50, 0xA1, 0x50}, {0xA1, 0xA1, 0x00},
		{0xBF, 0x0A, 0x0A}, {0x91, 0x0F, 0x0F}, {0x85, 0x0C, 0x0C},
	}
}

type Options struct {
	Geometry Geometry
	// Colors are the fill colours to look for and Tolerance the largest
	// difference allowed on any single channel.
	Colors    []Color
	Tolerance int
	// BlackMax is the highest value any channel may have and still count as
	// the bar's border or its unfilled background. Both are drawn black, which
	// is what lets one rule work at every health level.
	BlackMax int
	// Exclude lists top-left corners of bars that must never be reported: the
	// character's own, which sits at fixed pixels because the client's camera
	// is centred on the character. Matching on the exact position rather than
	// on nearness to the middle is deliberate - nearness would also swallow a
	// creature standing one tile away.
	Exclude          []image.Point
	ExcludeTolerance int
}

// Bar is one detected health bar, in the coordinates of the image it was found
// in. Fill is the width of the coloured part in pixels.
type Bar struct {
	X, Y int
	Fill int
}

// HP is how full the bar is, 0-1.
func (b Bar) HP(g Geometry) float64 {
	if g.InnerWidth() <= 0 {
		return 0
	}
	return float64(b.Fill) / float64(g.InnerWidth())
}

// Find returns every health bar in the image, in reading order.
//
// A bar clipped by the edge of the image is not returned: its border is not
// there to confirm, and a half-measured fill would be a lie about the
// creature's health. That is why the panel cuts one tile more than the
// decision radius - a creature at the very edge of the radius still has its
// whole bar inside the crop.
func Find(im *image.NRGBA, o Options) []Bar {
	g := o.Geometry
	if im == nil || !g.valid() || len(o.Colors) == 0 {
		return nil
	}
	b := im.Bounds()
	var out []Bar
	// claimed marks pixels already accounted for by a bar, so one bar is not
	// reported once per row of its fill.
	claimed := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if claimed.AlphaAt(x, y).A != 0 {
				continue
			}
			run := o.fillRun(im, x, y)
			if run == 0 {
				continue
			}
			bar := Bar{X: x - g.Border, Y: y - g.Border, Fill: run}
			if !o.confirm(im, bar, run) {
				continue
			}
			whole := image.Rect(bar.X, bar.Y, bar.X+g.Width, bar.Y+g.Height)
			for cy := whole.Min.Y; cy < whole.Max.Y; cy++ {
				for cx := whole.Min.X; cx < whole.Max.X; cx++ {
					claimed.SetAlpha(cx, cy, color.Alpha{A: 255})
				}
			}
			if o.excluded(bar) {
				continue
			}
			out = append(out, bar)
		}
	}
	return out
}

func (o Options) isFill(im *image.NRGBA, x, y int) bool {
	if !(image.Point{X: x, Y: y}).In(im.Bounds()) {
		return false
	}
	c := im.NRGBAAt(x, y)
	for _, want := range o.Colors {
		if diff(c.R, want.R) <= o.Tolerance &&
			diff(c.G, want.G) <= o.Tolerance &&
			diff(c.B, want.B) <= o.Tolerance {
			return true
		}
	}
	return false
}

func (o Options) isDark(im *image.NRGBA, x, y int) bool {
	if !(image.Point{X: x, Y: y}).In(im.Bounds()) {
		return false
	}
	c := im.NRGBAAt(x, y)
	return int(c.R) <= o.BlackMax && int(c.G) <= o.BlackMax && int(c.B) <= o.BlackMax
}

// fillRun measures the coloured run beginning at (x, y). It answers zero
// unless the run is bounded by dark pixels on both sides and is no wider than
// the bar's inside. The right-hand bound is what rejects a large patch of the
// same colour somewhere else on screen: in a wide patch the pixel after the
// widest allowed run is still coloured, never dark.
func (o Options) fillRun(im *image.NRGBA, x, y int) int {
	if !o.isDark(im, x-1, y) || !o.isFill(im, x, y) {
		return 0
	}
	max := o.Geometry.InnerWidth()
	n := 0
	for n < max && o.isFill(im, x+n, y) {
		n++
	}
	if n == 0 || !o.isDark(im, x+n, y) {
		return 0
	}
	return n
}

// confirm checks the whole rectangle: every inner row carries the same run,
// and the border is dark all the way round.
func (o Options) confirm(im *image.NRGBA, bar Bar, run int) bool {
	g := o.Geometry
	whole := image.Rect(bar.X, bar.Y, bar.X+g.Width, bar.Y+g.Height)
	if !whole.In(im.Bounds()) {
		return false
	}
	for dy := 0; dy < g.InnerHeight(); dy++ {
		if o.fillRun(im, bar.X+g.Border, bar.Y+g.Border+dy) != run {
			return false
		}
	}
	for dy := 0; dy < g.Border; dy++ {
		for dx := 0; dx < g.Width; dx++ {
			if !o.isDark(im, bar.X+dx, bar.Y+dy) {
				return false
			}
			if !o.isDark(im, bar.X+dx, bar.Y+g.Height-1-dy) {
				return false
			}
		}
	}
	return true
}

func (o Options) excluded(bar Bar) bool {
	for _, p := range o.Exclude {
		if abs(bar.X-p.X) <= o.ExcludeTolerance && abs(bar.Y-p.Y) <= o.ExcludeTolerance {
			return true
		}
	}
	return false
}

func diff(a, b uint8) int { return abs(int(a) - int(b)) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
```

- [ ] **Step 4: Dodaj pomocnik do fixture'ów**

W `internal/testenv/combatfixture.go` dopisz:

```go
// NRGBACrop cuts a rectangle out of a fixture and converts it to NRGBA - the
// layout the frame protocol delivers, and the one every detector expects.
// Going through draw.Draw rather than asserting the decoded type matters: a
// PNG can decode as paletted, and reading a paletted image's bytes as if they
// were colours is how this project once made every wall look walkable.
func NRGBACrop(t testing.TB, im image.Image, r image.Rectangle) *image.NRGBA {
	t.Helper()
	if r.Empty() {
		t.Fatalf("prostokąt wycinka jest pusty: %v — zmierz go w CombatCalibration", r)
	}
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), im, r.Min, draw.Src)
	return out
}
```

Dopisz importy `"image/draw"` i `"testing"`.

- [ ] **Step 5: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/vision/ ./internal/testenv/ -v -race`
Expected: PASS. Jeśli `TestFindOnRealCapture` znajduje mniej pasków niż potworów, sprawdź w tej kolejności: prostokąt wycinka, `BlackMax` (poświata i przezroczystość podnoszą czerń), `Tolerance`, geometrię (klient mógł przeskalować pasek). Zaloguj znalezione paski i porównaj z obrazem.

- [ ] **Step 6: Commit**

```bash
git add internal/vision/ internal/testenv/combatfixture.go
git commit -m "Dodaj detektor pasków życia nad stworami"
```

---

### Task 3: Z piksela na frakcyjną kratkę

Kluczowa obserwacja: **zakotwiczenie paska nie jest zgadywane, jest wyliczane**. Pasek postaci to pasek stwora na znanej kratce, więc jego przesunięcie względem środka tej kratki *jest* zakotwiczeniem.

**Files:**
- Create: `internal/vision/grid.go`
- Modify: `internal/vision/bars_test.go` (zacieśnić `TestFindOnRealCapture`)
- Modify: `internal/testenv/combatfixture.go` (wypełnić `SelfBar`)
- Test: `internal/vision/grid_test.go`

**Interfaces:**
- Consumes: `vision.Bar`, `vision.Geometry`, `vision.Find` z zadania 2.
- Produces:
  - `vision.Grid{Cols, Rows int; TileW, TileH, CropX, CropY, AnchorDX, AnchorDY float64; Geometry Geometry}`
  - `(Grid) Valid() bool`, `(Grid) PlayerCentre() (x, y float64)`, `(Grid) Offset(b Bar) (dx, dy float64)`, `(Grid) AnchorFrom(self Bar) (dx, dy float64)`
  - `vision.Distance(dx, dy float64) float64`

- [ ] **Step 1: Napisz nieprzechodzące testy siatki**

Create `internal/vision/grid_test.go`:

```go
package vision_test

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"

	"minimap-lab/internal/testenv"
	"minimap-lab/internal/vision"
)

func grid() vision.Grid {
	return vision.Grid{
		Cols: 15, Rows: 11, TileW: 32, TileH: 32,
		CropX: 64, CropY: 0, Geometry: classic,
	}
}

func TestPlayerCentreIsMiddleTile(t *testing.T) {
	x, y := grid().PlayerCentre()
	if x != 7.5*32 || y != 5.5*32 {
		t.Errorf("środek kratki postaci %v,%v, oczekiwano %v,%v", x, y, 7.5*32.0, 5.5*32.0)
	}
}

// Przesunięcie paska o dokładnie jedną kratkę musi zmieniać offset o dokładnie
// jeden. To jest właściwość, która nie zależy od żadnego pomiaru.
func TestOffsetMovesOneTilePerTile(t *testing.T) {
	g := grid()
	ax, ay := g.Offset(vision.Bar{X: 100, Y: 100})
	bx, by := g.Offset(vision.Bar{X: 132, Y: 164})
	if math.Abs((bx-ax)-1) > 1e-9 {
		t.Errorf("kratka w prawo zmieniła dx o %.9f, oczekiwano 1", bx-ax)
	}
	if math.Abs((by-ay)-2) > 1e-9 {
		t.Errorf("dwie kratki w dół zmieniły dy o %.9f, oczekiwano 2", by-ay)
	}
}

func TestAnchorPutsOwnBarAtZero(t *testing.T) {
	g := grid()
	self := vision.Bar{X: 150, Y: 170}
	g.AnchorDX, g.AnchorDY = g.AnchorFrom(self)
	dx, dy := g.Offset(self)
	if dx != 0 || dy != 0 {
		t.Errorf("własny pasek dał offset %.9f,%.9f, oczekiwano 0,0", dx, dy)
	}
}

func TestDistanceIsChebyshev(t *testing.T) {
	tests := []struct {
		dx, dy, want float64
	}{
		{0, 0, 0},
		{1, 0, 1},
		{0, -1, 1},
		{3, -4, 4},
		{-2.5, 1.2, 2.5},
	}
	for _, tt := range tests {
		if got := vision.Distance(tt.dx, tt.dy); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("Distance(%v,%v) = %v, oczekiwano %v", tt.dx, tt.dy, got, tt.want)
		}
	}
}

func TestNieskalibrowanaSiatkaNieWybucha(t *testing.T) {
	var g vision.Grid
	if g.Valid() {
		t.Fatal("pusta siatka nie powinna być poprawna")
	}
	if dx, dy := g.Offset(vision.Bar{X: 5, Y: 5}); dx != 0 || dy != 0 {
		t.Errorf("pusta siatka dała offset %v,%v, oczekiwano 0,0", dx, dy)
	}
}

// TestRealCaptureOffsets rysuje diagnostykę i sprawdza, że żaden stwór nie
// wypada dalej, niż wycinek fizycznie pozwala.
func TestRealCaptureOffsets(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.NRGBACrop(t, testenv.LoadFixture(t, "combat-capture.png"), fx.Crop)
	g := vision.Grid{
		Cols: fx.GridCols, Rows: fx.GridRows,
		TileW: float64(fx.Viewport.Dx()) / float64(fx.GridCols),
		TileH: float64(fx.Viewport.Dy()) / float64(fx.GridRows),
		CropX: float64(fx.Crop.Min.X - fx.Viewport.Min.X),
		CropY: float64(fx.Crop.Min.Y - fx.Viewport.Min.Y),
		Geometry: classic,
	}
	g.AnchorDX, g.AnchorDY = g.AnchorFrom(vision.Bar{X: fx.SelfBar.X, Y: fx.SelfBar.Y})
	t.Logf("zakotwiczenie wyliczone z własnego paska: dx=%.2f dy=%.2f", g.AnchorDX, g.AnchorDY)

	o := opts(classic)
	o.Exclude = []image.Point{fx.SelfBar}
	bars := vision.Find(im, o)
	limitX := float64(fx.Crop.Dx()) / g.TileW / 2
	limitY := float64(fx.Crop.Dy()) / g.TileH / 2
	for _, b := range bars {
		dx, dy := g.Offset(b)
		t.Logf("stwór na %.2f,%.2f (dystans %.2f, HP %.0f%%)",
			dx, dy, vision.Distance(dx, dy), 100*b.HP(classic))
		if math.Abs(dx) > limitX+1 || math.Abs(dy) > limitY+1 {
			t.Errorf("stwór na %.2f,%.2f wypada poza wycinek (%.2f x %.2f kratek) — "+
				"zakotwiczenie albo prostokąt wycinka są złe", dx, dy, 2*limitX, 2*limitY)
		}
	}
	// Diagnostyka do oczu: obrysy pasków na wycinku. Katalog .debug jest
	// ignorowany przez gita, więc na świeżym klonie go nie ma.
	debugDir := filepath.Join(testenv.RepoRoot(t), ".debug")
	if err := os.MkdirAll(debugDir, 0o700); err != nil {
		t.Fatal(err)
	}
	out := image.NewNRGBA(im.Bounds())
	copy(out.Pix, im.Pix)
	for _, b := range bars {
		for x := b.X; x < b.X+classic.Width; x++ {
			out.SetNRGBA(x, b.Y, color.NRGBA{R: 255, B: 255, A: 255})
			out.SetNRGBA(x, b.Y+classic.Height-1, color.NRGBA{R: 255, B: 255, A: 255})
		}
	}
	testenv.SavePNG(t, filepath.Join(debugDir, "vision-fixture.png"), out)
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie przechodzą**

Run: `go test ./internal/vision/ -run 'TestPlayerCentre|TestOffset|TestAnchor|TestDistance|TestNieskalibrowana|TestRealCapture' -v`
Expected: FAIL — `undefined: vision.Grid`.

- [ ] **Step 3: Napisz przeliczenie na kratki**

Create `internal/vision/grid.go`:

```go
package vision

import "math"

// Grid turns a bar's pixels into a fractional offset in tiles from the
// character. It works entirely inside the game window and needs no world
// position: the client's camera is centred on the character, so the character
// always stands on the middle tile. That is what lets the count of creatures
// around the character survive a failed minimap match.
type Grid struct {
	// Cols and Rows are the tiles the whole game window shows - 15 by 11 in
	// the official client.
	Cols, Rows int
	// TileW and TileH are pixels per tile, derived from the game window
	// rectangle rather than calibrated separately.
	TileW, TileH float64
	// CropX and CropY place the cut-out the bars were found in inside the
	// whole game window.
	CropX, CropY float64
	// AnchorDX and AnchorDY say where the centre of a bar sits relative to the
	// centre of the tile its creature stands on. Do not guess them: derive
	// them with AnchorFrom, from the character's own bar.
	AnchorDX, AnchorDY float64
	// Geometry is the bar geometry the offsets are computed with.
	Geometry Geometry
}

func (g Grid) Valid() bool {
	return g.Cols > 0 && g.Rows > 0 && g.TileW > 0 && g.TileH > 0
}

// PlayerCentre is the middle tile's centre, in game-window pixels. The
// division is integer on purpose: 15 columns put the character on column 7,
// with seven columns either side.
func (g Grid) PlayerCentre() (x, y float64) {
	return (float64(g.Cols/2) + 0.5) * g.TileW, (float64(g.Rows/2) + 0.5) * g.TileH
}

// barCentre is one bar's centre in game-window pixels.
func (g Grid) barCentre(b Bar) (x, y float64) {
	return g.CropX + float64(b.X) + float64(g.Geometry.Width)/2,
		g.CropY + float64(b.Y) + float64(g.Geometry.Height)/2
}

// Offset is where the creature stands relative to the character, in tiles.
// It is deliberately fractional: creatures slide between tiles as they walk,
// and rounding on every frame would make a count on the edge of a radius
// flicker between two values.
func (g Grid) Offset(b Bar) (dx, dy float64) {
	if !g.Valid() {
		return 0, 0
	}
	cx, cy := g.barCentre(b)
	px, py := g.PlayerCentre()
	return (cx - g.AnchorDX - px) / g.TileW, (cy - g.AnchorDY - py) / g.TileH
}

// AnchorFrom derives the anchor from the character's own bar. That bar belongs
// to a creature standing on a tile we know exactly - the middle one - so its
// displacement from that tile's centre is the anchor itself. Measuring it any
// other way means eyeballing pixels.
func (g Grid) AnchorFrom(self Bar) (dx, dy float64) {
	cx, cy := g.barCentre(self)
	px, py := g.PlayerCentre()
	return cx - px, cy - py
}

// Distance is the Chebyshev distance in tiles, which is how this game's area
// spells reach: everything in the 3x3 around the character is at distance 1.
func Distance(dx, dy float64) float64 {
	return math.Max(math.Abs(dx), math.Abs(dy))
}
```

- [ ] **Step 4: Uruchom testy syntetyczne i sprawdź, że przechodzą**

Run: `go test ./internal/vision/ -run 'TestPlayerCentre|TestOffset|TestAnchor|TestDistance|TestNieskalibrowana' -v`
Expected: PASS.

- [ ] **Step 5: Zmierz własny pasek — krok ręczny, dla człowieka**

`TestRealCaptureOffsets` jeszcze nie ma sensownego `SelfBar` (jest `image.Point{}`). Zrób tak:

1. Uruchom `go test ./internal/vision/ -run TestFindOnRealCapture -v` i przeczytaj wypisaną listę pasków.
2. Środek wycinka to `fx.Crop.Dx()/2, fx.Crop.Dy()/2`. **Pasek najbliższy tego punktu to pasek postaci** — kamera jest na niej wyśrodkowana.
3. Wpisz jego `X` i `Y` do `SelfBar` w `internal/testenv/combatfixture.go`.
4. Jeśli w kliencie masz wyłączony własny pasek życia, zostaw `image.Point{}` i **pomiń** `TestRealCaptureOffsets` przez `t.Skip` z komentarzem — wtedy zakotwiczenie trzeba zmierzyć z oka na `.debug/vision-fixture.png`, porównując obrys z kratką, na której stoi stwór.

- [ ] **Step 6: Zacieśnij test na prawdziwej klatce**

W `internal/vision/bars_test.go` zamień treść `TestFindOnRealCapture` na dokładne porównanie, teraz gdy własny pasek da się wykluczyć:

```go
func TestFindOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.NRGBACrop(t, testenv.LoadFixture(t, "combat-capture.png"), fx.Crop)
	o := opts(classic)
	if fx.SelfBar != (image.Point{}) {
		o.Exclude = []image.Point{fx.SelfBar}
	}
	bars := vision.Find(im, o)
	if len(bars) != fx.Monsters {
		t.Errorf("na prawdziwej klatce znaleziono %d pasków stworów, człowiek policzył %d: %v; "+
			"sprawdź w tej kolejności prostokąt wycinka, BlackMax, tolerancję barw i geometrię",
			len(bars), fx.Monsters, bars)
	}
}
```

- [ ] **Step 7: Uruchom wszystko i sprawdź, że przechodzi**

Run: `go test ./... -race`
Expected: PASS. Obejrzyj `.debug/vision-fixture.png` — obrysy muszą siedzieć na paskach stworów. Jeśli jeden stwór ma obrys, a inny nie, to najczęściej pasek przy krawędzi wycinka (odrzucany świadomie) albo barwa poza tolerancją.

- [ ] **Step 8: Sprawdź zakotwiczenie dużego stwora — krok ręczny, ryzyko ze specu nr 1**

W logu `TestRealCaptureOffsets` znajdź stwora, o którym wiesz, że ma duży sprite, i porównaj wypisany offset z tym, na której kratce stoi na obrazie. Jeśli rozjazd przekracza pół kratki, zapisz to w `docs/superpowers/specs/2026-09-07-combat-and-loot-design.md` w sekcji Ryzyka — faza 3 będzie musiała traktować kratkę dużego stwora jako niepewną.

- [ ] **Step 9: Commit**

```bash
git add internal/vision/ internal/testenv/combatfixture.go
git commit -m "Przelicz paski na frakcyjne kratki od postaci"
```

---

### Task 4: Czytnik battle listy

Liczy wiersze i — co ważniejsze — wskazuje wiersz z ramką celu. Bez ramki bot klikałby w już atakowanego stwora i zdejmował atak raz na klatkę.

**Files:**
- Create: `internal/battle/list.go`
- Test: `internal/battle/list_test.go`

**Interfaces:**
- Consumes: `vision.Find`, `vision.Geometry`, `vision.Color`, `vision.Bar`, `vision.Options`.
- Produces:
  - `battle.Options{Geometry vision.Geometry; Colors []vision.Color; Tolerance, BlackMax, RowPitch int; Frame vision.Color; FrameTolerance int; FrameCoverage float64}`
  - `battle.Row{Bar vision.Bar; HP float64; Targeted bool}`
  - `battle.List{Rows []Row; Truncated bool}`
  - `battle.Read(im *image.NRGBA, o Options) List`

- [ ] **Step 1: Napisz nieprzechodzące testy**

Create `internal/battle/list_test.go`:

```go
package battle_test

import (
	"image"
	"image/color"
	"testing"

	"minimap-lab/internal/battle"
	"minimap-lab/internal/testenv"
	"minimap-lab/internal/vision"
)

var mini = vision.Geometry{Width: 20, Height: 3, Border: 1}

const pitch = 22

// frameColor is deliberately far from every health colour in DefaultColors:
// #C00000 would sit inside the tolerance of the almost-dead bar (#BF0A0A) and
// the test would then pass for the wrong reason. The real client's frame
// colour is measured in step 4 and is allowed to be anything.
var frameColor = vision.Color{R: 0xFF, G: 0x50, B: 0x50}

func canvas(w, h int) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
		}
	}
	return im
}

func paint(im *image.NRGBA, at image.Point, fill int) {
	for y := 0; y < mini.Height; y++ {
		for x := 0; x < mini.Width; x++ {
			im.SetNRGBA(at.X+x, at.Y+y, color.NRGBA{A: 255})
		}
	}
	green := vision.DefaultColors()[0]
	for y := 0; y < mini.InnerHeight(); y++ {
		for x := 0; x < fill; x++ {
			im.SetNRGBA(at.X+mini.Border+x, at.Y+mini.Border+y,
				color.NRGBA{R: green.R, G: green.G, B: green.B, A: 255})
		}
	}
}

// frame draws the attack border as one horizontal line across the entry.
func frame(im *image.NRGBA, y int) {
	for x := 0; x < im.Bounds().Dx(); x++ {
		im.SetNRGBA(x, y, color.NRGBA{R: frameColor.R, G: frameColor.G, B: frameColor.B, A: 255})
	}
}

func opts() battle.Options {
	return battle.Options{
		Geometry: mini, Colors: vision.DefaultColors(), Tolerance: 12, BlackMax: 48,
		RowPitch: pitch, Frame: frameColor, FrameTolerance: 12, FrameCoverage: 0.8,
	}
}

func TestReadCountsRowsTopDown(t *testing.T) {
	im := canvas(60, 100)
	paint(im, image.Pt(30, 10), 18)
	paint(im, image.Pt(30, 10+pitch), 9)
	paint(im, image.Pt(30, 10+2*pitch), 2)
	list := battle.Read(im, opts())
	if len(list.Rows) != 3 {
		t.Fatalf("odczytano %d wierszy, oczekiwano 3: %+v", len(list.Rows), list.Rows)
	}
	if list.Rows[0].Bar.Y >= list.Rows[1].Bar.Y || list.Rows[1].Bar.Y >= list.Rows[2].Bar.Y {
		t.Errorf("wiersze nie są od góry: %+v", list.Rows)
	}
	if hp := list.Rows[0].HP; hp < 0.99 {
		t.Errorf("pierwszy wiersz ma HP %.2f, oczekiwano pełnego", hp)
	}
	for i, r := range list.Rows {
		if r.Targeted {
			t.Errorf("wiersz %d ma ramkę celu, choć nikt nie jest atakowany", i)
		}
	}
}

func TestReadFindsTargetFrame(t *testing.T) {
	im := canvas(60, 100)
	paint(im, image.Pt(30, 10), 18)
	paint(im, image.Pt(30, 10+pitch), 9)
	frame(im, 10+pitch-6) // linia ramki nad drugim wierszem
	list := battle.Read(im, opts())
	if len(list.Rows) != 2 {
		t.Fatalf("odczytano %d wierszy, oczekiwano 2", len(list.Rows))
	}
	if list.Rows[0].Targeted {
		t.Error("pierwszy wiersz nie powinien mieć ramki")
	}
	if !list.Rows[1].Targeted {
		t.Error("drugi wiersz powinien mieć ramkę celu")
	}
}

func TestReadEmptyList(t *testing.T) {
	list := battle.Read(canvas(60, 100), opts())
	if len(list.Rows) != 0 || list.Truncated {
		t.Errorf("pusta lista dała %+v", list)
	}
}

func TestReadMarksTruncatedList(t *testing.T) {
	im := canvas(60, 40)
	paint(im, image.Pt(30, 5), 18)
	paint(im, image.Pt(30, 5+pitch), 18)
	list := battle.Read(im, opts())
	if !list.Truncated {
		t.Error("lista dochodząca do dolnej krawędzi musi być oznaczona jako przewinięta")
	}
}

func TestReadOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.NRGBACrop(t, testenv.LoadFixture(t, "combat-capture.png"), fx.Battle)
	list := battle.Read(im, opts())
	t.Logf("wiersze na prawdziwej klatce: %+v (przewinięta: %v)", list.Rows, list.Truncated)
	if len(list.Rows) == 0 {
		t.Fatal("na prawdziwej klatce nie znaleziono żadnego wiersza; sprawdź prostokąt " +
			"battle listy i geometrię mini-paska")
	}
	if fx.TargetRow >= 0 {
		if fx.TargetRow >= len(list.Rows) {
			t.Fatalf("człowiek wskazał wiersz %d, odczytano tylko %d", fx.TargetRow, len(list.Rows))
		}
		if !list.Rows[fx.TargetRow].Targeted {
			t.Errorf("wiersz %d nie został rozpoznany jako cel; popraw Frame, FrameTolerance "+
				"albo FrameCoverage", fx.TargetRow)
		}
		for i, r := range list.Rows {
			if i != fx.TargetRow && r.Targeted {
				t.Errorf("wiersz %d fałszywie rozpoznany jako cel", i)
			}
		}
	}
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie przechodzą**

Run: `go test ./internal/battle/ -v`
Expected: FAIL — pakiet `battle` nie istnieje.

- [ ] **Step 3: Napisz czytnik**

Create `internal/battle/list.go`:

```go
// Package battle reads the client's battle list: how many creatures it shows
// and which entry carries the attack frame.
//
// The frame matters more than it looks. Clicking the entry already being
// attacked cancels the attack, so a bot that could not see the frame would
// toggle its own target on and off once per frame and never kill anything.
package battle

import (
	"image"

	"minimap-lab/internal/vision"
)

type Options struct {
	// Geometry is the small health bar drawn inside one entry. It is a
	// different size from the bars above creatures but exactly the same shape,
	// so the same detector reads both. It is not called Bar because Row.Bar
	// already means a detected bar, and one package with two meanings of Bar
	// is one too many.
	Geometry  vision.Geometry
	Colors    []vision.Color
	Tolerance int
	BlackMax  int
	// RowPitch is the vertical distance between two entries. It bounds where
	// the attack frame is looked for, and how close to the bottom edge the
	// last entry has to be for the list to count as scrolled.
	RowPitch int
	// Frame is the colour of the border the client draws round the entry being
	// attacked, with its own tolerance because it is not one of the health
	// colours.
	Frame          vision.Color
	FrameTolerance int
	// FrameCoverage is the fraction of the crop's width the frame colour must
	// cover on a single line to count as the frame rather than as some
	// coloured pixel that happens to match.
	FrameCoverage float64
}

// Row is one entry. Bar's centre is where a click on this entry goes - the
// health bar is part of the entry, so clicking it selects the creature, and
// unlike a click in the game window a click here can never move the
// character.
type Row struct {
	Bar      vision.Bar
	HP       float64
	Targeted bool
}

type List struct {
	Rows []Row
	// Truncated is true when the last entry sits against the bottom edge, so
	// the list is scrolled and the count is a floor rather than a total.
	Truncated bool
}

// Read answers what the list shows. Rows come out top to bottom because
// vision.Find scans row by row, so its output is already sorted by Y.
func Read(im *image.NRGBA, o Options) List {
	if im == nil || o.RowPitch < 1 {
		return List{}
	}
	bars := vision.Find(im, vision.Options{
		Geometry: o.Geometry, Colors: o.Colors,
		Tolerance: o.Tolerance, BlackMax: o.BlackMax,
	})
	var out List
	for _, b := range bars {
		out.Rows = append(out.Rows, Row{
			Bar: b, HP: b.HP(o.Geometry), Targeted: o.framed(im, b),
		})
	}
	if n := len(bars); n > 0 && bars[n-1].Y+o.RowPitch >= im.Bounds().Max.Y {
		out.Truncated = true
	}
	return out
}

// framed looks for the attack border in the band one entry tall around the
// bar - the band, not the bar's own rows, because the client draws the frame
// round the whole entry and the entry is taller than its health bar.
func (o Options) framed(im *image.NRGBA, b vision.Bar) bool {
	half := o.RowPitch / 2
	want := int(o.FrameCoverage * float64(im.Bounds().Dx()))
	if want < 1 {
		want = 1
	}
	for y := b.Y - half; y <= b.Y+o.Geometry.Height+half; y++ {
		if o.frameRun(im, y) >= want {
			return true
		}
	}
	return false
}

// frameRun is the longest unbroken run of the frame colour on one line.
func (o Options) frameRun(im *image.NRGBA, y int) int {
	b := im.Bounds()
	if y < b.Min.Y || y >= b.Max.Y {
		return 0
	}
	best, run := 0, 0
	for x := b.Min.X; x < b.Max.X; x++ {
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

func near(a, b uint8, tol int) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	return d <= tol
}
```

- [ ] **Step 4: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/battle/ -v -race`
Expected: PASS. `TestReadOnRealCapture` jest tym, który najpewniej wymaga strojenia: geometria mini-paska, `RowPitch` i barwa ramki są zgadnięte, a nie zmierzone. Odczytaj je z `.debug/vision-fixture.png` albo z powiększenia zapisanej klatki i popraw wartości w `opts()` w teście oraz — po zadaniu 6 — domyślne w panelu.

- [ ] **Step 5: Commit**

```bash
git add internal/battle/
git commit -m "Dodaj czytnik battle listy z rozpoznawaniem ramki celu"
```

---

### Task 5: Czytnik pasków HP i many

Powstaje tutaj, bo reguły czarów potrzebują progu many. Po tej fazie z zamówionego modułu leczenia zostanie sama lista reguł.

**Files:**
- Create: `internal/vitals/bar.go`
- Test: `internal/vitals/bar_test.go`

**Interfaces:**
- Produces:
  - `vitals.Options{Rows []int; MinSaturation, MinValue, MinValidRows float64}`
  - `vitals.Reading{Percent float64; OK bool; Reason string}`
  - `vitals.Read(im *image.NRGBA, o Options) Reading`
  - `vitals.DefaultOptions() Options`

- [ ] **Step 1: Napisz nieprzechodzące testy**

Create `internal/vitals/bar_test.go`:

```go
package vitals_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"minimap-lab/internal/testenv"
	"minimap-lab/internal/vitals"
)

// bar builds a client bar: a filled prefix in the given colour, the rest dark.
func bar(w, h, fill int, c color.NRGBA) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < fill {
				im.SetNRGBA(x, y, c)
			} else {
				im.SetNRGBA(x, y, color.NRGBA{R: 20, G: 20, B: 20, A: 255})
			}
		}
	}
	return im
}

var (
	green = color.NRGBA{R: 0, G: 200, B: 0, A: 255}
	red   = color.NRGBA{R: 200, G: 0, B: 0, A: 255}
	blue  = color.NRGBA{R: 0, G: 60, B: 220, A: 255}
)

func TestReadPercent(t *testing.T) {
	tests := []struct {
		name string
		im   *image.NRGBA
		want float64
	}{
		{"pełny zielony", bar(100, 8, 100, green), 1},
		{"połowa zielonego", bar(100, 8, 50, green), 0.5},
		{"pusty", bar(100, 8, 0, green), 0},
		{"czerwony liczy się tak samo jak zielony", bar(100, 8, 25, red), 0.25},
		{"mana na niebiesko", bar(100, 8, 80, blue), 0.8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vitals.Read(tt.im, vitals.DefaultOptions())
			if !got.OK {
				t.Fatalf("odczyt odrzucony: %s", got.Reason)
			}
			if math.Abs(got.Percent-tt.want) > 0.02 {
				t.Errorf("odczyt %.3f, oczekiwano %.3f", got.Percent, tt.want)
			}
		})
	}
}

// Rozjechana kalibracja daje rozsypane trafienia, nie ciągły prefiks. Zgłoszenie
// tego jako niskiego procentu kazałoby regule leczenia strzelać bez końca.
func TestReadRejectsScatteredFill(t *testing.T) {
	im := bar(100, 8, 0, green)
	for y := 0; y < 8; y++ {
		for _, x := range []int{5, 30, 70, 95} {
			im.SetNRGBA(x, y, green)
		}
	}
	got := vitals.Read(im, vitals.DefaultOptions())
	if got.OK {
		t.Errorf("rozsypane trafienia zostały przyjęte jako %.3f", got.Percent)
	}
	if got.Reason == "" {
		t.Error("odrzucony odczyt musi podać przyczynę")
	}
}

// Jeden wiersz przecięty cyfrą, którą klient rysuje na pasku, nie może przesunąć
// wyniku — stąd mediana z wielu wierszy.
func TestReadSurvivesOneCorruptedRow(t *testing.T) {
	im := bar(100, 8, 60, green)
	for _, x := range []int{80, 81, 82} {
		im.SetNRGBA(x, 4, green)
	}
	got := vitals.Read(im, vitals.DefaultOptions())
	if !got.OK {
		t.Fatalf("odczyt odrzucony: %s", got.Reason)
	}
	if math.Abs(got.Percent-0.6) > 0.02 {
		t.Errorf("odczyt %.3f, oczekiwano 0,6", got.Percent)
	}
}

func TestReadOnRealCapture(t *testing.T) {
	fx := testenv.CombatCalibration()
	im := testenv.LoadFixture(t, "combat-capture.png")
	for name, r := range map[string]image.Rectangle{"HP": fx.HP, "mana": fx.Mana} {
		got := vitals.Read(testenv.NRGBACrop(t, im, r), vitals.DefaultOptions())
		t.Logf("%s: %.1f%% (ok: %v, %s)", name, 100*got.Percent, got.OK, got.Reason)
		if !got.OK {
			t.Errorf("%s odrzucony na prawdziwej klatce: %s — sprawdź prostokąt, "+
				"powinien obejmować sam pasek, bez obwódki i bez cyfr", name, got.Reason)
		}
	}
}
```

- [ ] **Step 2: Uruchom testy i sprawdź, że nie przechodzą**

Run: `go test ./internal/vitals/ -v`
Expected: FAIL — pakiet `vitals` nie istnieje.

- [ ] **Step 3: Napisz czytnik**

Create `internal/vitals/bar.go`:

```go
// Package vitals reads one of the client's own bars - health or mana - as a
// percentage.
//
// A pixel counts as filled by saturation and brightness, never by hue. The
// health bar changes colour as it empties, green through yellow to red, so
// matching a colour would stop working exactly when the reading matters most.
package vitals

import (
	"fmt"
	"image"
	"sort"
)

type Options struct {
	// Rows are the image rows to sample; empty means every row. The answer is
	// their median, so one row crossing a number the client draws over the bar
	// cannot swing the reading.
	Rows []int
	// MinSaturation and MinValue are what a pixel must clear to count as
	// filled, both 0-1.
	MinSaturation float64
	MinValue      float64
	// MinValidRows is the fraction of sampled rows that must produce a clean
	// prefix before the reading is trusted at all.
	MinValidRows float64
}

func DefaultOptions() Options {
	return Options{MinSaturation: 0.35, MinValue: 0.25, MinValidRows: 0.5}
}

type Reading struct {
	Percent float64
	OK      bool
	Reason  string
}

// Read measures the filled prefix of the bar.
//
// The filled pixels must form a contiguous run from the left edge, and a row
// with anything filled after that run is thrown away. This is the guard
// against a calibration that has slipped off the bar: such a rectangle
// produces scattered matches, and reporting those as a low percentage would
// make a healing rule fire forever.
func Read(im *image.NRGBA, o Options) Reading {
	if im == nil || im.Bounds().Empty() {
		return Reading{Reason: "brak obrazu paska"}
	}
	b := im.Bounds()
	rows := o.Rows
	if len(rows) == 0 {
		rows = make([]int, 0, b.Dy())
		for y := b.Min.Y; y < b.Max.Y; y++ {
			rows = append(rows, y)
		}
	}
	var prefixes []int
	sampled := 0
	for _, y := range rows {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		sampled++
		if n, ok := o.prefix(im, y); ok {
			prefixes = append(prefixes, n)
		}
	}
	if sampled == 0 {
		return Reading{Reason: "żaden z wybranych wierszy nie leży w obrazie paska"}
	}
	need := o.MinValidRows * float64(sampled)
	if float64(len(prefixes)) < need {
		return Reading{Reason: fmt.Sprintf(
			"tylko %d z %d wierszy ma ciągłe wypełnienie — kalibracja paska najpewniej się rozjechała",
			len(prefixes), sampled)}
	}
	sort.Ints(prefixes)
	median := prefixes[len(prefixes)/2]
	return Reading{Percent: float64(median) / float64(b.Dx()), OK: true}
}

// prefix counts the filled pixels at the start of one row, and rejects the row
// if anything further right is filled too.
func (o Options) prefix(im *image.NRGBA, y int) (int, bool) {
	b := im.Bounds()
	n := 0
	for x := b.Min.X; x < b.Max.X && o.filled(im, x, y); x++ {
		n++
	}
	for x := b.Min.X + n + 1; x < b.Max.X; x++ {
		if o.filled(im, x, y) {
			return 0, false
		}
	}
	return n, true
}

func (o Options) filled(im *image.NRGBA, x, y int) bool {
	c := im.NRGBAAt(x, y)
	hi, lo := int(c.R), int(c.R)
	for _, v := range []int{int(c.G), int(c.B)} {
		if v > hi {
			hi = v
		}
		if v < lo {
			lo = v
		}
	}
	if hi == 0 {
		return false
	}
	value := float64(hi) / 255
	saturation := float64(hi-lo) / float64(hi)
	return value >= o.MinValue && saturation >= o.MinSaturation
}
```

- [ ] **Step 4: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/vitals/ -v -race`
Expected: PASS. Jeśli `TestReadOnRealCapture` odrzuca odczyt, prostokąt obejmuje coś poza paskiem — najczęściej obwódkę albo cyfry. Zwęź go i zmierz ponownie.

- [ ] **Step 5: Commit**

```bash
git add internal/vitals/
git commit -m "Dodaj czytnik pasków HP i many"
```

---

### Task 6: Konfiguracja widzenia w mózgu

Kalibracja wchodzi do `brain.Config` jako jedna zagnieżdżona struktura, walidowana hurtowo — jedno złe pole nie może cicho wyczyścić innego. Zakotwiczenie paska **nie jest polem konfiguracji, gdy widać własny pasek**: wtedy liczy je `Grid.AnchorFrom`.

**Files:**
- Create: `internal/brain/combatconfig.go`
- Modify: `internal/brain/loop.go` (pole `Combat` w `Config`, wołanie walidacji)
- Test: `internal/brain/combatconfig_test.go`

**Interfaces:**
- Consumes: `vision.Geometry`, `vision.Color`, `vision.Options`, `vision.Grid`, `vision.DefaultColors`, `battle.Options`, `vitals.Options`, `vitals.DefaultOptions`.
- Produces:
  - `brain.Rect{X, Y, W, H int}` z `Empty() bool` i `Bounds() image.Rectangle`
  - `brain.CombatConfig` (pola niżej) z `Enabled() bool`, `RecommendedCrop() Rect`
  - metody nieeksportowane, wołane tylko z pętli: `withDefaults() CombatConfig`, `validate() error`, `barOptions() (vision.Options, error)`, `grid() vision.Grid`, `battleOptions() (battle.Options, error)`, `vitalsOptions() vitals.Options`
  - `brain.Config` zyskuje pole `Combat CombatConfig \`json:"combat"\``

- [ ] **Step 1: Napisz nieprzechodzące testy walidacji**

Create `internal/brain/combatconfig_test.go`:

```go
package brain

import (
	"strings"
	"testing"
)

// calibrated is a whole, valid calibration: a 240x176 game window at 16px per
// tile, cropped to the middle 11x11 tiles.
func calibrated() CombatConfig {
	return CombatConfig{
		Viewport:       Rect{X: 100, Y: 50, W: 240, H: 176},
		Crop:           Rect{X: 132, Y: 50, W: 176, H: 176},
		Battle:         Rect{X: 400, Y: 60, W: 160, H: 220},
		HP:             Rect{X: 20, Y: 300, W: 100, H: 8},
		Mana:           Rect{X: 20, Y: 312, W: 100, H: 8},
		GridCols:       15,
		GridRows:       11,
		BarWidth:       13,
		BarHeight:      4,
		BarBorder:      1,
		DecisionRadius: 4,
		BattleBarWidth: 13, BattleBarHeight: 4, BattleBarBorder: 1,
		BattleRowPitch: 22, BattleFrame: "#ff5050",
	}
}

func TestCombatConfigAcceptsCalibrated(t *testing.T) {
	if err := calibrated().withDefaults().validate(); err != nil {
		t.Fatalf("poprawna kalibracja odrzucona: %v", err)
	}
}

func TestCombatConfigUncalibratedIsLegal(t *testing.T) {
	if err := (CombatConfig{}).withDefaults().validate(); err != nil {
		t.Fatalf("brak kalibracji musi być dozwolony: %v", err)
	}
	if (CombatConfig{}).Enabled() {
		t.Error("pusta kalibracja nie może być włączona")
	}
	if !calibrated().Enabled() {
		t.Error("pełna kalibracja musi być włączona")
	}
}

func TestCombatConfigDefaults(t *testing.T) {
	c := CombatConfig{Viewport: Rect{W: 240, H: 176}, Crop: Rect{X: 32, W: 176, H: 176},
		BarWidth: 13, BarHeight: 4, BarBorder: 1,
		BattleBarWidth: 13, BattleBarHeight: 4, BattleBarBorder: 1, BattleRowPitch: 22,
		BattleFrame: "#ff5050"}.withDefaults()
	if c.GridCols != 15 || c.GridRows != 11 {
		t.Errorf("domyślna siatka %dx%d, oczekiwano 15x11", c.GridCols, c.GridRows)
	}
	if c.DecisionRadius != 4 {
		t.Errorf("domyślny promień %v, oczekiwano 4", c.DecisionRadius)
	}
	if c.BlackMax == 0 || c.BarTolerance == 0 || c.BattleFrameCoverage == 0 {
		t.Errorf("progi barw nie dostały wartości domyślnych: %+v", c)
	}
	if len(c.BarColors) == 0 {
		t.Error("lista barw paska nie dostała wartości domyślnych")
	}
}

func TestCombatConfigRecommendedCropIsElevenTiles(t *testing.T) {
	c := calibrated().withDefaults()
	got := c.RecommendedCrop()
	// Promień 4 daje zasięg 5 kratek w każdą stronę, czyli 11 kolumn po 16 px.
	// Okno ma tylko 11 wierszy, więc w pionie wycinek jest przycięty do pełnej
	// wysokości - i to jest powód, dla którego wycinek oszczędza jedną czwartą,
	// a nie wielokrotność.
	if got.W != 176 || got.H != 176 {
		t.Errorf("zalecany wycinek %dx%d, oczekiwano 176x176", got.W, got.H)
	}
	if got.X != 132 || got.Y != 50 {
		t.Errorf("zalecany wycinek zaczyna się na %d,%d, oczekiwano 132,50", got.X, got.Y)
	}
}

func TestCombatConfigRejections(t *testing.T) {
	tests := []struct {
		name string
		edit func(*CombatConfig)
		want string
	}{
		{"wycinek poza oknem gry", func(c *CombatConfig) { c.Crop.X = 0 }, "wycinek"},
		{"wycinek za mały na promień", func(c *CombatConfig) {
			c.Crop = Rect{X: 164, Y: 50, W: 112, H: 176}
		}, "promień"},
		{"okno gry mniejsze niż siatka", func(c *CombatConfig) { c.Viewport.W = 10 }, "okno gry"},
		{"za dużo kolumn", func(c *CombatConfig) { c.GridCols = 200 }, "siatka"},
		{"obwódka szersza niż pasek", func(c *CombatConfig) { c.BarBorder = 3 }, "obwódka"},
		{"promień poza zakresem", func(c *CombatConfig) { c.DecisionRadius = 99 }, "promień"},
		{"barwa nie do odczytania", func(c *CombatConfig) { c.BarColors = []string{"zielony"} }, "barwa"},
		{"barwa ramki nie do odczytania", func(c *CombatConfig) { c.BattleFrame = "xyz" }, "ramki"},
		{"odstęp wierszy mniejszy niż pasek", func(c *CombatConfig) { c.BattleRowPitch = 2 }, "odstęp"},
		{"pasek HP bez szerokości", func(c *CombatConfig) { c.HP = Rect{X: 20, Y: 300, W: 2, H: 8} }, "HP"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := calibrated()
			tt.edit(&c)
			err := c.withDefaults().validate()
			if err == nil {
				t.Fatalf("kalibracja przeszła, choć nie powinna")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("komunikat %q nie zawiera %q", err.Error(), tt.want)
			}
		})
	}
}

// Zakotwiczenie liczy się z własnego paska, a nie z konfiguracji. To jedyny
// sposób, żeby nie zgadywać pikseli.
func TestGridDerivesAnchorFromSelfBar(t *testing.T) {
	c := calibrated().withDefaults()
	c.HasSelfBar, c.SelfBarX, c.SelfBarY = true, 81, 74
	g := c.grid()
	dx, dy := g.Offset(vision.Bar{X: 81, Y: 74})
	if dx != 0 || dy != 0 {
		t.Errorf("własny pasek dał offset %.6f,%.6f, oczekiwano 0,0", dx, dy)
	}
}
```

Plik testowy jest w pakiecie `brain` (nie `brain_test`), bo sprawdza nieeksportowane `withDefaults`, `validate` i `grid`. Dopisz do jego importów `"minimap-lab/internal/vision"`.

- [ ] **Step 2: Uruchom testy i sprawdź, że nie przechodzą**

Run: `go test ./internal/brain/ -run TestCombatConfig -v`
Expected: FAIL — `undefined: CombatConfig`.

- [ ] **Step 3: Napisz konfigurację i walidację**

Create `internal/brain/combatconfig.go`:

```go
package brain

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"minimap-lab/internal/battle"
	"minimap-lab/internal/vision"
	"minimap-lab/internal/vitals"
)

// Rect is a rectangle in the shared screen's pixels, the way the panel
// measures it by dragging.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func (r Rect) Empty() bool             { return r.W <= 0 || r.H <= 0 }
func (r Rect) Bounds() image.Rectangle { return image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H) }

// CombatConfig is everything the vision layer needs. It is nested rather than
// flattened into Config because it is a dozen fields that only make sense
// together, and because an empty value has to mean "not calibrated yet"
// rather than "a bar zero pixels wide".
type CombatConfig struct {
	// Viewport is the whole game window; Crop is the part of it the panel
	// actually sends. Crop is an explicit field rather than a formula so the
	// panel and the brain cannot drift by a pixel: the panel offers a button
	// that fills it from RecommendedCrop, but the value lives here.
	Viewport Rect `json:"viewport"`
	Crop     Rect `json:"crop"`
	Battle   Rect `json:"battle"`
	HP       Rect `json:"hp"`
	Mana     Rect `json:"mana"`

	// GridCols and GridRows are the tiles the game window shows. 15x11 in the
	// official client, overridable because that is an assumption about a
	// client build, not a law.
	GridCols int `json:"grid_cols"`
	GridRows int `json:"grid_rows"`

	BarWidth     int      `json:"bar_width"`
	BarHeight    int      `json:"bar_height"`
	BarBorder    int      `json:"bar_border"`
	BarTolerance int      `json:"bar_tolerance"`
	BlackMax     int      `json:"black_max"`
	BarColors    []string `json:"bar_colors"`

	// HasSelfBar says the client draws the character's own bar. When it does,
	// AnchorDX and AnchorDY are ignored and derived from SelfBarX/SelfBarY
	// instead: the character's bar belongs to a creature on a tile we know
	// exactly, so it measures the anchor for us. The explicit fields are the
	// fallback for a client with the own bar switched off.
	HasSelfBar bool    `json:"has_self_bar"`
	SelfBarX   int     `json:"self_bar_x"`
	SelfBarY   int     `json:"self_bar_y"`
	AnchorDX   float64 `json:"anchor_dx"`
	AnchorDY   float64 `json:"anchor_dy"`

	// DecisionRadius is the upper bound on everything: it sizes the crop and
	// it is the radius the snapshot counts creatures in. A spell rule may not
	// ask for more, or it would be asking about tiles the panel never sent.
	DecisionRadius float64 `json:"decision_radius"`

	BattleBarWidth       int     `json:"battle_bar_width"`
	BattleBarHeight      int     `json:"battle_bar_height"`
	BattleBarBorder      int     `json:"battle_bar_border"`
	BattleRowPitch       int     `json:"battle_row_pitch"`
	BattleFrame          string  `json:"battle_frame"`
	BattleFrameTolerance int     `json:"battle_frame_tolerance"`
	BattleFrameCoverage  float64 `json:"battle_frame_coverage"`
}

// Enabled reports whether there is enough calibration to look at anything.
func (c CombatConfig) Enabled() bool { return !c.Viewport.Empty() && !c.Crop.Empty() }

// withDefaults fills the fields the panel may leave out. Zero is treated as
// "unset" for each of them, which is safe because none of these has a useful
// zero: a black threshold of zero, a tolerance of zero or an empty colour list
// would all mean "find nothing".
func (c CombatConfig) withDefaults() CombatConfig {
	if c.GridCols == 0 {
		c.GridCols = 15
	}
	if c.GridRows == 0 {
		c.GridRows = 11
	}
	if c.DecisionRadius == 0 {
		c.DecisionRadius = 4
	}
	if c.BarTolerance == 0 {
		c.BarTolerance = 12
	}
	if c.BlackMax == 0 {
		c.BlackMax = 48
	}
	if len(c.BarColors) == 0 {
		for _, col := range vision.DefaultColors() {
			c.BarColors = append(c.BarColors, fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B))
		}
	}
	if c.BattleFrameTolerance == 0 {
		c.BattleFrameTolerance = 12
	}
	if c.BattleFrameCoverage == 0 {
		c.BattleFrameCoverage = 0.8
	}
	return c
}

// tileSize is pixels per tile, derived from the game window rather than
// configured: the official client always shows the same number of tiles, so a
// separate tile size field could only ever disagree with the rectangle.
func (c CombatConfig) tileSize() (w, h float64) {
	return float64(c.Viewport.W) / float64(c.GridCols), float64(c.Viewport.H) / float64(c.GridRows)
}

// RecommendedCrop is the character's tile grown by the decision radius plus one
// tile of margin, clipped to the game window. The extra tile is what makes a
// creature at the very edge of the radius still show its whole health bar
// inside the crop - a clipped bar is not detected at all.
func (c CombatConfig) RecommendedCrop() Rect {
	c = c.withDefaults()
	if c.Viewport.Empty() {
		return Rect{}
	}
	tw, th := c.tileSize()
	reach := int(math.Ceil(c.DecisionRadius)) + 1
	col, row := c.GridCols/2, c.GridRows/2
	x0, x1 := max(0, col-reach), min(c.GridCols, col+reach+1)
	y0, y1 := max(0, row-reach), min(c.GridRows, row+reach+1)
	return Rect{
		X: c.Viewport.X + int(math.Round(float64(x0)*tw)),
		Y: c.Viewport.Y + int(math.Round(float64(y0)*th)),
		W: int(math.Round(float64(x1-x0) * tw)),
		H: int(math.Round(float64(y1-y0) * th)),
	}
}

func (c CombatConfig) validate() error {
	if !c.Enabled() {
		// Not calibrated is a legal state: the panel is meant to be
		// calibrated one rectangle at a time, checking each as it goes.
		if c.Viewport.Empty() && c.Crop.Empty() {
			return nil
		}
		return fmt.Errorf("okno gry i wycinek trzeba zaznaczyć razem")
	}
	if c.GridCols < 3 || c.GridCols > 64 || c.GridRows < 3 || c.GridRows > 64 {
		return fmt.Errorf("siatka kratek musi mieścić się w zakresie 3–64 w obu wymiarach")
	}
	if c.Viewport.W < c.GridCols || c.Viewport.H < c.GridRows {
		return fmt.Errorf("okno gry jest mniejsze niż jedna kratka na kolumnę")
	}
	if !c.Crop.Bounds().In(c.Viewport.Bounds()) {
		return fmt.Errorf("wycinek musi mieścić się w oknie gry")
	}
	if want := c.RecommendedCrop(); !want.Bounds().In(c.Crop.Bounds()) {
		return fmt.Errorf("wycinek nie obejmuje promienia decyzji: potrzebne co najmniej %d×%d px od %d,%d",
			want.W, want.H, want.X, want.Y)
	}
	if c.DecisionRadius < 0.5 || c.DecisionRadius > 16 {
		return fmt.Errorf("promień decyzji musi mieścić się w zakresie 0,5–16 kratek")
	}
	if err := checkBar("paska życia", c.BarWidth, c.BarHeight, c.BarBorder); err != nil {
		return err
	}
	if c.BarTolerance < 0 || c.BarTolerance > 128 || c.BlackMax < 0 || c.BlackMax > 128 {
		return fmt.Errorf("tolerancja barw i próg czerni muszą mieścić się w zakresie 0–128")
	}
	for _, s := range c.BarColors {
		if _, err := parseColor(s); err != nil {
			return fmt.Errorf("barwa paska %q: %w", s, err)
		}
	}
	if !c.Battle.Empty() {
		if err := checkBar("paska w battle liście", c.BattleBarWidth, c.BattleBarHeight, c.BattleBarBorder); err != nil {
			return err
		}
		if c.BattleRowPitch < c.BattleBarHeight || c.BattleRowPitch > 256 {
			return fmt.Errorf("odstęp wierszy battle listy musi być nie mniejszy niż wysokość paska i nie większy niż 256 px")
		}
		if _, err := parseColor(c.BattleFrame); err != nil {
			return fmt.Errorf("barwa ramki celu %q: %w", c.BattleFrame, err)
		}
		if c.BattleFrameCoverage < 0.05 || c.BattleFrameCoverage > 1 {
			return fmt.Errorf("pokrycie ramki celu musi mieścić się w zakresie 0,05–1")
		}
	}
	for name, r := range map[string]Rect{"HP": c.HP, "many": c.Mana} {
		if !r.Empty() && (r.W < 8 || r.H < 1) {
			return fmt.Errorf("prostokąt paska %s musi mieć co najmniej 8 px szerokości", name)
		}
	}
	return nil
}

func checkBar(what string, w, h, border int) error {
	if w < 3 || w > 256 || h < 3 || h > 64 {
		return fmt.Errorf("wymiary %s muszą mieścić się w zakresie 3–256 na 3–64 px", what)
	}
	if border < 1 || border > 8 || 2*border >= w || 2*border >= h {
		return fmt.Errorf("obwódka %s musi mieć 1–8 px i zostawić miejsce na wypełnienie", what)
	}
	return nil
}

// parseColor reads "#rrggbb". The panel sends colours as text because that is
// what a colour input produces and what a human can retype from a screenshot.
func parseColor(s string) (vision.Color, error) {
	s = strings.TrimSpace(s)
	if len(s) != 7 || s[0] != '#' {
		return vision.Color{}, fmt.Errorf("oczekiwano zapisu #rrggbb")
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return vision.Color{}, fmt.Errorf("oczekiwano zapisu #rrggbb")
	}
	return vision.Color{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}, nil
}

func (c CombatConfig) geometry() vision.Geometry {
	return vision.Geometry{Width: c.BarWidth, Height: c.BarHeight, Border: c.BarBorder}
}

// barOptions is only ever called on a validated config, so the colours parse.
func (c CombatConfig) barOptions() (vision.Options, error) {
	o := vision.Options{
		Geometry: c.geometry(), Tolerance: c.BarTolerance,
		BlackMax: c.BlackMax, ExcludeTolerance: 2,
	}
	for _, s := range c.BarColors {
		col, err := parseColor(s)
		if err != nil {
			return vision.Options{}, err
		}
		o.Colors = append(o.Colors, col)
	}
	if c.HasSelfBar {
		o.Exclude = []image.Point{{X: c.SelfBarX, Y: c.SelfBarY}}
	}
	return o, nil
}

func (c CombatConfig) grid() vision.Grid {
	tw, th := c.tileSize()
	g := vision.Grid{
		Cols: c.GridCols, Rows: c.GridRows, TileW: tw, TileH: th,
		CropX:    float64(c.Crop.X - c.Viewport.X),
		CropY:    float64(c.Crop.Y - c.Viewport.Y),
		Geometry: c.geometry(),
		AnchorDX: c.AnchorDX, AnchorDY: c.AnchorDY,
	}
	if c.HasSelfBar {
		g.AnchorDX, g.AnchorDY = g.AnchorFrom(vision.Bar{X: c.SelfBarX, Y: c.SelfBarY})
	}
	return g
}

func (c CombatConfig) battleOptions() (battle.Options, error) {
	frame, err := parseColor(c.BattleFrame)
	if err != nil {
		return battle.Options{}, err
	}
	o := battle.Options{
		Geometry: vision.Geometry{Width: c.BattleBarWidth, Height: c.BattleBarHeight,
			Border: c.BattleBarBorder},
		Tolerance: c.BarTolerance, BlackMax: c.BlackMax, RowPitch: c.BattleRowPitch,
		Frame: frame, FrameTolerance: c.BattleFrameTolerance,
		FrameCoverage: c.BattleFrameCoverage,
	}
	for _, s := range c.BarColors {
		col, err := parseColor(s)
		if err != nil {
			return battle.Options{}, err
		}
		o.Colors = append(o.Colors, col)
	}
	return o, nil
}

func (c CombatConfig) vitalsOptions() vitals.Options { return vitals.DefaultOptions() }
```

- [ ] **Step 4: Wepnij konfigurację do `Config`**

W `internal/brain/loop.go`, w strukturze `Config`, po polu `LoopRoute`:

```go
	// Combat is the whole vision calibration. Zero value means "not
	// calibrated", which is legal: the panel is meant to be calibrated one
	// rectangle at a time, with each one checked before the next.
	Combat CombatConfig `json:"combat"`
```

Na końcu `func (c Config) validate() error`, przed `return nil`:

```go
	if err := c.Combat.withDefaults().validate(); err != nil {
		return err
	}
```

W `SetConfig`, w ciele przekazanym do `l.do`, po `l.cfg = c`, dopisz normalizację, żeby pętla pracowała na wartościach z domyślnymi:

```go
		l.cfg.Combat = c.Combat.withDefaults()
```

- [ ] **Step 5: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/brain/ -v -race`
Expected: PASS, w tym wszystkie istniejące testy pętli — puste `Combat` musi przechodzić walidację, inaczej każdy stary test się wywali.

- [ ] **Step 6: Commit**

```bash
git add internal/brain/combatconfig.go internal/brain/combatconfig_test.go internal/brain/loop.go
git commit -m "Dodaj kalibrację widzenia do konfiguracji mózgu"
```

---

### Task 7: Wpięcie widzenia w pętlę mózgu

Detekcja jest oddzielona od sita danych mapy, bo jedno nie potrzebuje pozycji w świecie, a drugie tak. `handleFrame` dostaje `defer`, który publikuje snapshot i domyka widzenie — dzięki temu każde wczesne wyjście z funkcji też opublikuje stan, a sito zobaczy pozycję **z tej** klatki, nie z poprzedniej.

**Files:**
- Modify: `internal/frame/frame.go` (dwie stałe i `knownRegion`)
- Modify: `internal/brain/state.go` (`CombatState`, `VisionView`, `BarView`, `RowView`, pole w `State`)
- Modify: `internal/brain/loop.go` (pola, `observeVision`, `finishVision`, `VisionSnapshot`, `defer` w `handleFrame`)
- Create: `visionapi.go`
- Modify: `server.go` (jedna trasa)
- Test: `internal/brain/vision_test.go`, `internal/frame/frame_test.go` (dopisać), `visionapi_test.go`

**Interfaces:**
- Consumes: wszystko z zadań 2–6.
- Produces:
  - `frame.RegionViewport RegionID = 4`, `frame.RegionBattle RegionID = 5`
  - `brain.CombatState`, `brain.VisionView`, `brain.BarView`, `brain.RowView`
  - `brain.State` zyskuje `Combat CombatState \`json:"combat"\``
  - `(*brain.Loop) VisionSnapshot(ctx context.Context) VisionView`
  - `GET /api/vision`

- [ ] **Step 1: Napisz nieprzechodzący test regionów klatki**

W `internal/frame/frame_test.go` dopisz:

```go
func TestParseAcceptsViewportAndBattleRegions(t *testing.T) {
	for _, id := range []RegionID{RegionViewport, RegionBattle} {
		body := oneRegion(id, 2, 2)
		f, err := Parse(body)
		if err != nil {
			t.Fatalf("region %d odrzucony: %v", id, err)
		}
		if _, ok := f.Image(id); !ok {
			t.Errorf("region %d nie wrócił jako obraz", id)
		}
	}
}
```

Uwaga dla wykonawcy: `internal/frame/frame_test.go` ma już pomocnik budujący ciało z jednym regionem. Jeśli nazywa się inaczej niż `oneRegion`, użyj istniejącej nazwy, nie dodawaj drugiego pomocnika.

- [ ] **Step 2: Uruchom test i sprawdź, że nie przechodzi**

Run: `go test ./internal/frame/ -run TestParseAcceptsViewport -v`
Expected: FAIL — `undefined: RegionViewport`.

- [ ] **Step 3: Dodaj regiony**

W `internal/frame/frame.go`, w bloku stałych `RegionID`:

```go
	RegionViewport RegionID = 4
	RegionBattle   RegionID = 5
```

i w `knownRegion`:

```go
func knownRegion(id RegionID) bool {
	return id == RegionMinimap || id == RegionHP || id == RegionMana ||
		id == RegionViewport || id == RegionBattle
}
```

- [ ] **Step 4: Uruchom test i sprawdź, że przechodzi**

Run: `go test ./internal/frame/ -v`
Expected: PASS.

- [ ] **Step 5: Napisz nieprzechodzące testy widzenia w pętli**

Create `internal/brain/vision_test.go`:

```go
package brain

import (
	"encoding/binary"
	"image"
	"image/color"
	"strings"
	"testing"

	"minimap-lab/internal/frame"
	"minimap-lab/internal/vision"
)

// Bary maluje się tu lokalnie, tak samo jak w testach internal/vision i
// internal/battle. Wspólny pomocnik w testenv byłby DRY, ale każdy z tych
// testów sprawdza inną geometrię i lokalny malarz czyta się lepiej niż
// funkcja z pięcioma parametrami. To duplikacja świadoma, nie przeoczenie.
func paintBar(im *image.NRGBA, g vision.Geometry, at image.Point, fill int, c vision.Color) {
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			im.SetNRGBA(at.X+x, at.Y+y, color.NRGBA{A: 255})
		}
	}
	for y := 0; y < g.InnerHeight(); y++ {
		for x := 0; x < fill; x++ {
			im.SetNRGBA(at.X+g.Border+x, at.Y+g.Border+y,
				color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
}

func filled(w, h int, c color.NRGBA) *image.NRGBA {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, c)
		}
	}
	return im
}

// visionCalibration matches internal/brain/combatconfig_test.go's calibrated():
// a 240x176 game window at 16px per tile, cropped to the middle 11x11 tiles.
func visionCalibration() CombatConfig {
	c := calibrated()
	c.HasSelfBar, c.SelfBarX, c.SelfBarY = true, 81, 74
	return c
}

// crop paints the character's own bar plus one creature per offset given in
// whole tiles, and returns the crop as raw RGBA the way the panel sends it.
func crop(offsets ...image.Point) *image.NRGBA {
	c := visionCalibration()
	g := vision.Geometry{Width: c.BarWidth, Height: c.BarHeight, Border: c.BarBorder}
	im := filled(c.Crop.W, c.Crop.H, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	green := vision.DefaultColors()[0]
	paintBar(im, g, image.Pt(c.SelfBarX, c.SelfBarY), g.InnerWidth(), green)
	for _, o := range offsets {
		paintBar(im, g, image.Pt(c.SelfBarX+16*o.X, c.SelfBarY+16*o.Y), 5, green)
	}
	return im
}

// region packs one image the way frame.Parse expects it.
type region struct {
	id   frame.RegionID
	im   *image.NRGBA
}

func (h *harness) visionFrame(t *testing.T, regions ...region) frame.Frame {
	t.Helper()
	h.seq++
	h.videoUS += 100_000
	specs := append([]region{{id: frame.RegionMinimap, im: filled(2, 2, color.NRGBA{})}}, regions...)
	body := make([]byte, frame.HeaderSize+frame.RegionHeader*len(specs))
	copy(body[0:4], frame.Magic)
	body[4], body[5] = frame.FormatVersion, byte(len(specs))
	binary.LittleEndian.PutUint64(body[16:], h.seq)
	binary.LittleEndian.PutUint64(body[24:], h.videoUS)
	for i, r := range specs {
		hdr := body[frame.HeaderSize+frame.RegionHeader*i:]
		hdr[0] = byte(r.id)
		binary.LittleEndian.PutUint16(hdr[4:], uint16(r.im.Bounds().Dx()))
		binary.LittleEndian.PutUint16(hdr[6:], uint16(r.im.Bounds().Dy()))
		binary.LittleEndian.PutUint32(hdr[8:], uint32(len(r.im.Pix)))
	}
	for _, r := range specs {
		body = append(body, r.im.Pix...)
	}
	f, err := frame.Parse(body)
	if err != nil {
		t.Fatalf("frame.Parse: %v", err)
	}
	return f
}

// submit posts one frame and waits until the loop has finished with it.
func (h *harness) submit(t *testing.T, f frame.Frame) *State {
	t.Helper()
	h.loop.Submit(f, h.clock.now())
	return h.await(t, f.Seq)
}

func TestCombatCountsCreaturesInRadius(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport,
		crop(image.Pt(1, 0), image.Pt(3, 0))}))
	if !s.Combat.Calibrated {
		t.Fatal("stan walki musi być oznaczony jako skalibrowany")
	}
	if s.Combat.BarsTotal != 2 {
		t.Errorf("BarsTotal = %d, oczekiwano 2 (własny pasek jest wykluczany)", s.Combat.BarsTotal)
	}
	if s.Combat.MonstersInRange != 2 {
		t.Errorf("MonstersInRange = %d, oczekiwano 2 przy promieniu 4", s.Combat.MonstersInRange)
	}
}

func TestCombatRadiusExcludesFartherCreature(t *testing.T) {
	h := newHarness(t)
	c := visionCalibration()
	c.DecisionRadius = 2
	// Mniejszy promień wymaga mniejszego zalecanego wycinka, a nasz wycinek
	// jest większy, więc walidacja dalej przechodzi.
	h.config(t, func(cfg *Config) { cfg.Combat = c })
	h.at(1000, 1000)
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport,
		crop(image.Pt(1, 0), image.Pt(3, 0))}))
	if s.Combat.BarsTotal != 2 {
		t.Errorf("BarsTotal = %d, oczekiwano 2", s.Combat.BarsTotal)
	}
	if s.Combat.MonstersInRange != 1 {
		t.Errorf("MonstersInRange = %d, oczekiwano 1 przy promieniu 2", s.Combat.MonstersInRange)
	}
}

func TestCombatSurvivesLostPosition(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.locator.miss()
	s := h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))}))
	if s.Position != nil {
		t.Fatal("test wymaga utraconego dopasowania minimapy")
	}
	if s.Combat.MonstersInRange != 1 {
		t.Errorf("MonstersInRange = %d, oczekiwano 1 — liczenie potworów nie potrzebuje pozycji",
			s.Combat.MonstersInRange)
	}
}

func TestCombatIsSilentWithoutViewportRegion(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	s := h.submit(t, h.visionFrame(t))
	if s.Combat.BarsTotal != 0 || s.Combat.MonstersInRange != 0 {
		t.Errorf("klatka bez regionu okna gry dała %+v", s.Combat)
	}
	if s.Position == nil {
		t.Error("brak regionu okna gry nie może przerwać lokalizacji")
	}
}

func TestCombatFlagsMixedCrowd(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	c := visionCalibration()
	g := vision.Geometry{Width: c.BattleBarWidth, Height: c.BattleBarHeight, Border: c.BattleBarBorder}
	list := filled(c.Battle.W, c.Battle.H, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
	paintBar(list, g, image.Pt(4, 4), 8, vision.DefaultColors()[0])
	s := h.submit(t, h.visionFrame(t,
		region{frame.RegionViewport, crop(image.Pt(1, 0), image.Pt(2, 0))},
		region{frame.RegionBattle, list}))
	if s.Combat.BattleRows != 1 {
		t.Fatalf("BattleRows = %d, oczekiwano 1", s.Combat.BattleRows)
	}
	if !s.Combat.MixedCrowd {
		t.Error("dwa paski przy jednym wierszu listy dowodzą, że w wycinku jest nie-potwór")
	}
}

func TestVitalsReachTheSnapshot(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	hp := filled(100, 8, color.NRGBA{R: 20, G: 20, B: 20, A: 255})
	for y := 0; y < 8; y++ {
		for x := 0; x < 40; x++ {
			hp.SetNRGBA(x, y, color.NRGBA{G: 200, A: 255})
		}
	}
	s := h.submit(t, h.visionFrame(t, region{frame.RegionHP, hp}))
	if !s.Combat.HPOK {
		t.Fatalf("odczyt HP odrzucony: %s", s.Combat.Reason)
	}
	if s.Combat.HPPct < 0.38 || s.Combat.HPPct > 0.42 {
		t.Errorf("HP = %.3f, oczekiwano około 0,4", s.Combat.HPPct)
	}
}

func TestVisionSnapshotCarriesOffsetsButStateDoesNot(t *testing.T) {
	h := newHarness(t)
	h.config(t, func(c *Config) { c.Combat = visionCalibration() })
	h.at(1000, 1000)
	h.submit(t, h.visionFrame(t, region{frame.RegionViewport, crop(image.Pt(1, 0))}))
	view := h.loop.VisionSnapshot(h.ctx)
	if !view.Have || len(view.Bars) != 1 {
		t.Fatalf("podgląd widzenia: %+v", view)
	}
	if dx := view.Bars[0].DX; dx < 0.9 || dx > 1.1 {
		t.Errorf("offset dx = %.3f, oczekiwano około 1", dx)
	}
	data, err := marshalState(h.loop.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	// Snapshot ma zostać mały: prostokąty pasków jadą tylko przez /api/vision.
	for _, forbidden := range []string{`"dx"`, `"fill"`} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("snapshot wiezie %s, a nie powinien: %s", forbidden, data)
		}
	}
}
```

Ten plik potrzebuje dwóch dokładek w istniejącym `internal/brain/loop_test.go`. Wyodrębnij oczekiwanie na klatkę z `tick`, żeby `submit` używało tej samej pętli, i daj scenariuszowemu lokalizatorowi tryb pudła:

```go
// await waits until the loop has published its answer to one frame.
func (h *harness) await(t *testing.T, seq uint64) *State {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s := h.loop.Snapshot()
		if s.LastFrameSeq == seq {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("pętla nie przetworzyła klatki %d", seq)
		}
		time.Sleep(time.Millisecond)
	}
}
```

W `tick` zostaw samo `h.loop.Submit(...)` i `return h.await(t, f.Seq)`.

```go
// miss makes the locator answer "not found", the way a match in the dark does.
func (s *scriptedLocator) miss() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.found = false
}
```

Uwaga: nazwy pól w `scriptedLocator` odczytaj z pliku — powyżej jest kształt, a nie kopia. Jeśli struktura nie ma pola `found`, dodaj je i uwzględnij w `Locate`.

W `vision_test.go` użyj `strings.Contains` wprost i zaimportuj `"strings"`; nie dodawaj własnego `contains`.

- [ ] **Step 6: Uruchom testy i sprawdź, że nie przechodzą**

Run: `go test ./internal/brain/ -run 'TestCombat|TestVitals|TestVision' -v`
Expected: FAIL — `s.Combat` nie istnieje.

- [ ] **Step 7: Dodaj stan widzenia**

W `internal/brain/state.go`, do struktury `State` po polu `Recorder`:

```go
	Combat CombatState `json:"combat"`
```

i na końcu pliku:

```go
// CombatState is what the panel is told about what the bot can see. Scalars
// only: the snapshot is answered on every single frame, so the rectangles
// behind these numbers go out through GET /api/vision instead.
type CombatState struct {
	// Calibrated is false until the game window and the crop are both
	// measured; everything below is then zero.
	Calibrated bool `json:"calibrated"`
	// BarsTotal counts creature bars inside the crop, excluding the
	// character's own. MonstersInRange counts those within the decision
	// radius, measured as a Chebyshev distance in tiles.
	BarsTotal       int `json:"bars_total"`
	MonstersInRange int `json:"monsters_in_range"`
	// RejectedByMap counts bars dropped because the map data calls their tile
	// impassable - a creature cannot stand in a wall, so such a bar was drawn
	// from another floor. Always zero while the position is unknown, because
	// the sieve has no tile to ask about.
	RejectedByMap int `json:"rejected_by_map"`
	// MixedCrowd is true when more creature bars sit in the crop than the
	// battle list shows monster rows for the whole screen, which proves
	// something in the crop is not a monster. The test is one-sided and
	// deliberately conservative: false does not mean the crowd is clean.
	MixedCrowd bool `json:"mixed_crowd"`
	BattleRows int  `json:"battle_rows"`
	// BattleTruncated says the list is scrolled, so BattleRows is a floor and
	// MixedCrowd stops meaning anything at all.
	BattleTruncated bool `json:"battle_truncated"`
	// TargetRow is the entry carrying the attack frame, counting from zero.
	// Nil means nothing is being attacked - which is what tells a click on the
	// list from a click that would cancel the attack.
	TargetRow *int `json:"target_row"`

	HPPct   float64 `json:"hp_pct"`
	HPOK    bool    `json:"hp_ok"`
	ManaPct float64 `json:"mana_pct"`
	ManaOK  bool    `json:"mana_ok"`
	// Reason carries why a reading was refused, for the panel to show.
	Reason string `json:"reason,omitempty"`
}

// VisionView is the panel's diagnostic picture of one frame. It never rides in
// the snapshot - dozens of rectangles per frame is exactly the payload the
// snapshot's own comment forbids - so the panel fetches it separately, and
// only while it is showing the preview.
type VisionView struct {
	Have      bool      `json:"have"`
	CropW     int       `json:"crop_w"`
	CropH     int       `json:"crop_h"`
	Bars      []BarView `json:"bars"`
	Battle    []RowView `json:"battle"`
	Truncated bool      `json:"truncated"`
	HP        float64   `json:"hp"`
	HPOK      bool      `json:"hp_ok"`
	Mana      float64   `json:"mana"`
	ManaOK    bool      `json:"mana_ok"`
	Reason    string    `json:"reason,omitempty"`
}

type BarView struct {
	X    int     `json:"x"`
	Y    int     `json:"y"`
	Fill int     `json:"fill"`
	HP   float64 `json:"hp"`
	DX   float64 `json:"dx"`
	DY   float64 `json:"dy"`
	Dist float64 `json:"dist"`
}

type RowView struct {
	X        int     `json:"x"`
	Y        int     `json:"y"`
	HP       float64 `json:"hp"`
	Targeted bool    `json:"targeted"`
}
```

- [ ] **Step 8: Wepnij widzenie w pętlę**

W `internal/brain/loop.go` dodaj do struktury `Loop`, po polu `match`:

```go
	combat CombatState
	view   VisionView
	// bars is what the detector found on the last frame, kept raw so the map
	// sieve can be applied again once the position for that frame is known.
	bars      []vision.Bar
	visionGrid vision.Grid
```

Dodaj stałą obok `logDepth`:

```go
	// maxVisionBars bounds the diagnostic payload. A crop calibrated onto the
	// wrong part of the screen can match hundreds of things; the panel needs
	// to see that it went wrong, not to receive all of it.
	maxVisionBars = 64
```

Przebuduj początek `handleFrame` tak, żeby publikowanie i domknięcie widzenia siedziały w jednym `defer`, i **usuń wszystkie dotychczasowe wywołania `l.publish()` z tej funkcji**:

```go
func (l *Loop) handleFrame(ctx context.Context, env frameEnvelope) {
	l.lastFrameSeq = env.f.Seq
	l.lastFrameAt = env.receivedAt
	// Vision is finished and the snapshot published on every path out of this
	// function, including the early returns. The two go together because the
	// map sieve needs the position this frame produced - or the absence of it -
	// and that is only settled once the match is over.
	defer func() {
		l.finishVision()
		l.publish()
	}()
	im, ok := env.f.Image(frame.RegionMinimap)
	if !ok {
		return
	}
	if l.hasVideoUS && env.f.VideoTimeUS == l.lastVideoUS {
		return
	}
	l.lastVideoUS, l.hasVideoUS = env.f.VideoTimeUS, true
	// Detection runs before the match and regardless of it: the client's
	// camera is centred on the character, so counting the creatures around her
	// needs no world position whatsoever.
	l.observeVision(env.f)
	...  // dalej bez zmian, tylko bez publish()
}
```

Dopisz dwie metody:

```go
// observeVision reads the client's own panels and finds the creature bars. It
// is pure with respect to the world: nothing here consults the position.
func (l *Loop) observeVision(f frame.Frame) {
	l.combat, l.view, l.bars = CombatState{}, VisionView{}, nil
	cc := l.cfg.Combat
	if !cc.Enabled() {
		return
	}
	l.combat.Calibrated = true
	if im, ok := f.Image(frame.RegionHP); ok {
		r := vitals.Read(im, cc.vitalsOptions())
		l.combat.HPPct, l.combat.HPOK = r.Percent, r.OK
		l.view.HP, l.view.HPOK = r.Percent, r.OK
		if !r.OK {
			l.combat.Reason = r.Reason
		}
	}
	if im, ok := f.Image(frame.RegionMana); ok {
		r := vitals.Read(im, cc.vitalsOptions())
		l.combat.ManaPct, l.combat.ManaOK = r.Percent, r.OK
		l.view.Mana, l.view.ManaOK = r.Percent, r.OK
		if !r.OK && l.combat.Reason == "" {
			l.combat.Reason = r.Reason
		}
	}
	if im, ok := f.Image(frame.RegionBattle); ok {
		o, err := cc.battleOptions()
		if err != nil {
			l.combat.Reason = err.Error()
		} else {
			list := battle.Read(im, o)
			l.combat.BattleRows = len(list.Rows)
			l.combat.BattleTruncated, l.view.Truncated = list.Truncated, list.Truncated
			for i, row := range list.Rows {
				if row.Targeted && l.combat.TargetRow == nil {
					idx := i
					l.combat.TargetRow = &idx
				}
				l.view.Battle = append(l.view.Battle, RowView{
					X: row.Bar.X, Y: row.Bar.Y, HP: row.HP, Targeted: row.Targeted,
				})
			}
		}
	}
	im, ok := f.Image(frame.RegionViewport)
	if !ok {
		return
	}
	o, err := cc.barOptions()
	if err != nil {
		l.combat.Reason = err.Error()
		return
	}
	l.visionGrid = cc.grid()
	l.bars = vision.Find(im, o)
	l.view.Have = true
	l.view.CropW, l.view.CropH = im.Bounds().Dx(), im.Bounds().Dy()
}

// finishVision turns the raw bars into counts. It is separate from
// observeVision because it needs the position, and it recomputes from the raw
// bars rather than accumulating, so running it twice on one frame - which a
// repeated video frame does - cannot double any count.
func (l *Loop) finishVision() {
	if !l.combat.Calibrated {
		return
	}
	cc := l.cfg.Combat
	l.combat.BarsTotal, l.combat.MonstersInRange, l.combat.RejectedByMap = 0, 0, 0
	l.view.Bars = nil
	for _, b := range l.bars {
		dx, dy := l.visionGrid.Offset(b)
		if l.blockedTile(dx, dy) {
			l.combat.RejectedByMap++
			continue
		}
		dist := vision.Distance(dx, dy)
		l.combat.BarsTotal++
		if dist <= cc.DecisionRadius+0.5 {
			l.combat.MonstersInRange++
		}
		if len(l.view.Bars) < maxVisionBars {
			l.view.Bars = append(l.view.Bars, BarView{
				X: b.X, Y: b.Y, Fill: b.Fill, HP: b.HP(cc.geometry()),
				DX: dx, DY: dy, Dist: dist,
			})
		}
	}
	// One-sided on purpose: more bars than rows proves a non-monster is in the
	// crop, but equal or fewer proves nothing, because the rows cover the whole
	// screen while the bars cover only the crop.
	l.combat.MixedCrowd = !l.combat.BattleTruncated &&
		l.combat.BattleRows > 0 && l.combat.BarsTotal > l.combat.BattleRows
}

// blockedTile is the cheap sieve against creatures the client drew from
// another floor: a creature cannot stand in a wall. It does nothing while the
// position is unknown, which costs nothing - counting never needed it.
func (l *Loop) blockedTile(dx, dy float64) bool {
	if l.position == nil {
		return false
	}
	p := *l.position
	return l.deps.Tile(mapdata.Position{
		X: p.X + int(math.Round(dx)), Y: p.Y + int(math.Round(dy)), Z: p.Z,
	}) == TileBlocked
}

// VisionSnapshot hands the panel the diagnostic picture of the last frame.
func (l *Loop) VisionSnapshot(ctx context.Context) VisionView {
	var out VisionView
	l.do(ctx, func() {
		out = l.view
		out.Bars = append([]BarView(nil), l.view.Bars...)
		out.Battle = append([]RowView(nil), l.view.Battle...)
	})
	return out
}
```

W `publish()`, do budowanego `State`, dodaj `Combat: l.combat,`. Dopisz importy `"math"`, `"minimap-lab/internal/battle"`, `"minimap-lab/internal/vision"`, `"minimap-lab/internal/vitals"`.

- [ ] **Step 9: Uruchom testy i sprawdź, że przechodzą**

Run: `go test ./internal/brain/ -v -race`
Expected: PASS, wraz ze wszystkimi istniejącymi testami pętli. Jeśli któryś stary test się wywali, prawie na pewno chodzi o usunięte wywołania `publish()` — sprawdź, czy `defer` stoi przed pierwszym wczesnym wyjściem.

- [ ] **Step 10: Wystaw podgląd po HTTP**

Create `visionapi.go`:

```go
package main

import "net/http"

// visionView answers the panel's diagnostic picture of the last frame: where
// every bar was, which tile it maps to, what the battle list showed. It is a
// route of its own for the same reason the neighbourhood preview is: dozens of
// rectangles have no business riding in a snapshot answered on every frame.
func (s *server) visionView(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	writeJSON(w, s.loop.VisionSnapshot(r.Context()))
}
```

W `server.go`, w `routes()`, obok `GET /api/preview`:

```go
	mux.HandleFunc("GET /api/vision", s.visionView)
```

Create `visionapi_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVisionRouteRefusedWithoutControl(t *testing.T) {
	s, _ := venoreServer(t)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://localhost/api/vision", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("bez sterowania /api/vision odpowiedziało %d, oczekiwano 503", rec.Code)
	}
}
```

- [ ] **Step 11: Uruchom wszystko i sprawdź, że przechodzi**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add internal/frame/ internal/brain/ visionapi.go visionapi_test.go server.go
git commit -m "Wepnij widzenie w pętlę mózgu i wystaw podgląd"
```

---

### Task 8: Kalibracja i podgląd w panelu

Panel zostaje kamerą i widokiem. Dochodzi wybór kalibrowanego prostokąta, wyliczanie wycinka, wysyłka czterech nowych regionów i podgląd z obrysami — plus jedna rzecz, która oszczędza godzinę zgadywania: **kliknięcie własnego paska na podglądzie** wyznacza zakotwiczenie.

**Files:**
- Modify: `web/camera.js` (dwa identyfikatory w `REGION`)
- Modify: `web/index.html` (nowa sekcja 7)
- Modify: `web/panel.js` (blok widzenia, obsługa wskaźnika, `brainConfig`, `REMEMBERED`, pętla nasłuchu)
- Test: `webtests/panel_test.cjs`, `webtests/camera_test.cjs`

**Interfaces:**
- Consumes: `GET /api/vision`, pole `combat` w `PUT /api/config`, pole `combat` w snapshocie.
- Produces: `FRAME_REGION.viewport = 4`, `FRAME_REGION.battle = 5`.

- [ ] **Step 1: Napisz nieprzechodzące testy panelu**

W `webtests/panel_test.cjs` dopisz — testy używają istniejącego harnessu: `panel()` jest synchroniczne, zwraca `el`, `settled`, `requests` i `sandbox`, a `drag(el, [x, y], [x, y])` bierze tablice:

```javascript
// pixelPerfect zdejmuje przeskalowanie podglądu: stub kanwy zwraca swój
// własny rozmiar z getBoundingClientRect, a panel przelicza kliknięcia na
// rozdzielczość źródła. Zrównanie obu daje mapowanie jeden do jednego, bez
// którego nie da się sprawdzić dokładnych współrzędnych prostokąta.
function pixelPerfect(p) {
  p.el('screen').width = 800;
  p.el('screen').height = 600;
}

async function shareOnly(p) {
  p.el('share').click();
  await p.settled();
  pixelPerfect(p);
}

async function calibrate(p, target, from, to) {
  p.el('calib-target').value = target;
  drag(p.el('screen'), from, to);
  await p.settled();
}

const lastConfig = p => JSON.parse(p.requests.filter(r => r.url === '/api/config').at(-1).body);

test('zaznaczenie okna gry wysyła wycinek jedenastu kratek', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);

  const combat = lastConfig(p).brain.combat;
  assert.deepEqual(combat.viewport, {x: 100, y: 50, w: 240, h: 176});
  // Promień 4 daje zasięg 5 kratek, czyli 11 kolumn po 16 px. Okno ma tylko
  // 11 wierszy, więc w pionie wycinek jest przycięty do pełnej wysokości - i
  // to jest powód, dla którego wycinek oszczędza jedną czwartą, a nie
  // wielokrotność.
  assert.deepEqual(combat.crop, {x: 132, y: 50, w: 176, h: 176});
});

test('cztery nowe regiony trafiają do klatki', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  await calibrate(p, 'battle', [400, 60], [559, 279]);
  await calibrate(p, 'hp', [20, 300], [119, 307]);
  await calibrate(p, 'mana', [20, 312], [119, 319]);
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  await armNow(p);

  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();

  const frame = p.requests.find(r => r.url === '/api/frame');
  assert.ok(frame, 'nie wysłano żadnej klatki');
  // Bajt 5 nagłówka to liczba regionów: minimapa plus cztery nowe.
  assert.equal(new Uint8Array(frame.body)[5], 5);
});

test('kalibracja minimapy nie rusza prostokątów widzenia', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  const before = lastConfig(p).brain.combat.viewport;
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  assert.deepEqual(lastConfig(p).brain.combat.viewport, before);
});

test('podgląd widzenia nie jest pobierany, dopóki nie jest włączony', async () => {
  const seen = {combat: {calibrated: true}};
  const p = panel({
    state: seen,
    onRequest: url => url === '/api/frame'
      ? {ok: true, async json() { return seen; }}
      : null,
  });
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  await armNow(p);
  const visionCalls = () => p.requests.filter(r => r.url === '/api/vision').length;
  assert.equal(visionCalls(), 0, 'podgląd pobrany, choć wyłączony');

  p.el('vision-preview').checked = true;
  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  assert.ok(visionCalls() >= 1, 'włączony podgląd nie pobrał widzenia');
});

test('kliknięcie własnego paska na podglądzie wypełnia jego pozycję', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  p.el('bar-width').value = '13';
  p.el('bar-height').value = '4';
  // Podgląd jeszcze nie dostał danych, więc kanwa ma rozmiar z HTML-a; w
  // teście ustawiamy go wprost, żeby kliknięcie mapowało się jeden do jednego.
  p.el('vision-canvas').width = 176;
  p.el('vision-canvas').height = 176;
  p.el('vision-canvas').fire('pointerdown', {clientX: 88, clientY: 76});
  await p.settled();
  // Klikasz środek paska, zapisywany jest jego lewy górny róg.
  assert.equal(p.el('self-bar-x').value, 82);
  assert.equal(p.el('self-bar-y').value, 74);
});
```

Uwaga dla wykonawcy: w harnessie, w mapie inicjalizującej wartości pól na początku `panel()`, dopisz wartości domyślne nowych liczb — inaczej `num()` zwraca zero i geometria paska wychodzi zerowa:

```javascript
    'grid-cols': '15', 'grid-rows': '11', 'decision-radius': '4',
    'bar-width': '27', 'bar-height': '4', 'bar-border': '1',
    'bar-tolerance': '12', 'black-max': '48',
    'battle-bar-width': '27', 'battle-bar-height': '4', 'battle-bar-border': '1',
    'battle-pitch': '22', 'battle-frame-coverage': '0.8',
```

oraz `document.getElementById('bar-colors').value` i `document.getElementById('battle-frame').value` na te same napisy, co w HTML-u.

- [ ] **Step 2: Uruchom testy i sprawdź, że nie przechodzą**

Run: `node --test webtests/panel_test.cjs`
Expected: FAIL — `combat` nie ma w wysyłanej konfiguracji, więc `lastConfig(p).brain.combat` jest `undefined`.

- [ ] **Step 3: Dodaj identyfikatory regionów**

W `web/camera.js`:

```javascript
const REGION = {minimap: 1, hp: 2, mana: 3, viewport: 4, battle: 5};
```

- [ ] **Step 4: Dodaj sekcję do HTML**

W `web/index.html`, przed `<footer>`:

```html
  <section class="panel vision">
    <h2>7. Widzenie: potwory, battle lista, paski</h2>
    <p>Bot liczy potwory z pasków życia, które klient rysuje nad każdym stworem. Kamera klienta jest wyśrodkowana na postaci, więc do policzenia, ile ich stoi wokół, nie jest potrzebna żadna pozycja na mapie — wystarczy okno gry.</p>
    <label>Kalibruję<select id="calib-target">
      <option value="minimap">minimapę</option>
      <option value="viewport">okno gry</option>
      <option value="battle">battle listę</option>
      <option value="hp">pasek HP</option>
      <option value="mana">pasek many</option>
    </select></label>
    <p class="hint">Przeciągnij po obrazie u góry, żeby zaznaczyć wybrany prostokąt. Okno gry zaznacz bez ramki klienta — z niego wyliczy się rozmiar kratki i wycinek, który naprawdę jedzie do bota. Paski HP i many zaznacz bez obwódki i bez cyfr.</p>
    <div id="vision-rects">Nic jeszcze nie zaznaczone.</div>
    <div class="route-grid">
      <label>Kolumn kratek<input id="grid-cols" type="number" min="3" max="64" value="15"></label>
      <label>Wierszy kratek<input id="grid-rows" type="number" min="3" max="64" value="11"></label>
      <label>Promień decyzji (kratki)<input id="decision-radius" type="number" min="0.5" max="16" step="0.5" value="4"></label>
      <label>Pasek: szerokość<input id="bar-width" type="number" min="3" max="256" value="27"></label>
      <label>Pasek: wysokość<input id="bar-height" type="number" min="3" max="64" value="4"></label>
      <label>Pasek: obwódka<input id="bar-border" type="number" min="1" max="8" value="1"></label>
      <label>Tolerancja barw<input id="bar-tolerance" type="number" min="0" max="128" value="12"></label>
      <label>Próg czerni<input id="black-max" type="number" min="0" max="128" value="48"></label>
    </div>
    <label>Barwy wypełnienia paska<input id="bar-colors" type="text" value="#00bc00,#50a150,#a1a100,#bf0a0a,#910f0f,#850c0c"></label>
    <p class="hint">Sześć wartości, do których klient kwantuje życie stwora. To punkt startowy zdjęty z klienta odniesienia, nie prawda objawiona — jeśli podgląd nie obrysowuje pasków, popraw barwy albo podnieś tolerancję.</p>
    <label class="check"><input id="self-bar-on" type="checkbox" checked>Klient rysuje własny pasek postaci</label>
    <p class="hint">Zaznaczone: kliknij własny pasek na podglądzie niżej. Z tego jednego punktu wylicza się zakotwiczenie paska nad kratką — nie trzeba go mierzyć z oka. Odznaczone: wypełnij zakotwiczenie ręcznie w polach obok.</p>
    <div class="route-grid">
      <label>Własny pasek: x<input id="self-bar-x" type="number" min="0" max="4096" value="0"></label>
      <label>Własny pasek: y<input id="self-bar-y" type="number" min="0" max="4096" value="0"></label>
      <label>Battle: szerokość paska<input id="battle-bar-width" type="number" min="3" max="256" value="27"></label>
      <label>Battle: wysokość paska<input id="battle-bar-height" type="number" min="3" max="64" value="4"></label>
      <label>Battle: obwódka<input id="battle-bar-border" type="number" min="1" max="8" value="1"></label>
      <label>Battle: odstęp wierszy<input id="battle-pitch" type="number" min="3" max="256" value="22"></label>
      <label>Battle: barwa ramki celu<input id="battle-frame" type="text" value="#ff5050"></label>
      <label>Battle: pokrycie ramki<input id="battle-frame-coverage" type="number" min="0.05" max="1" step="0.05" value="0.8"></label>
    </div>
    <label class="check"><input id="vision-preview" type="checkbox">Pokazuj podgląd widzenia</label>
    <canvas id="vision-canvas" class="grid-canvas" width="176" height="176"></canvas>
    <div id="vision-info" role="status" aria-live="polite">—</div>
    <p class="hint">Różowy obrys: stwór w promieniu decyzji. Szary: widziany, ale za daleko. Pasek przycięty krawędzią wycinka nie jest obrysowywany wcale — dlatego wycinek bierze jedną kratkę zapasu.</p>
  </section>
```

- [ ] **Step 5: Dodaj blok widzenia do panelu**

W `web/panel.js`, po bloku „konfiguracja", dodaj:

```javascript
// --- widzenie ---

const VISION_RECTS = ['viewport', 'battle', 'hp', 'mana'];
const EMPTY_RECT = {x: 0, y: 0, w: 0, h: 0};
let rects = {viewport: null, battle: null, hp: null, mana: null};
let visionPending = false;

function calibTarget() { return $('calib-target').value || 'minimap'; }

// cropRect is the window the brain actually looks at: the character's tile
// grown by the decision radius plus one tile of margin, clipped to the game
// window. The margin is what lets a creature at the very edge of the radius
// still show its whole health bar - a clipped bar is not detected at all.
//
// The same formula lives in Go as CombatConfig.RecommendedCrop, but the value
// travels inside the config rather than being recomputed there, so the two
// sides cannot drift by a pixel.
function cropRect() {
  const v = rects.viewport;
  if (!v) return null;
  const cols = num('grid-cols') || 15, rows = num('grid-rows') || 11;
  const tw = v.w / cols, th = v.h / rows;
  const reach = Math.ceil(num('decision-radius') || 4) + 1;
  const col = Math.floor(cols / 2), row = Math.floor(rows / 2);
  const x0 = Math.max(0, col - reach), x1 = Math.min(cols, col + reach + 1);
  const y0 = Math.max(0, row - reach), y1 = Math.min(rows, row + reach + 1);
  return {
    x: v.x + Math.round(x0 * tw), y: v.y + Math.round(y0 * th),
    w: Math.round((x1 - x0) * tw), h: Math.round((y1 - y0) * th),
  };
}

function applyVisionRegions() {
  camera.setRegion(FRAME_REGION.viewport, cropRect());
  camera.setRegion(FRAME_REGION.battle, rects.battle);
  camera.setRegion(FRAME_REGION.hp, rects.hp);
  camera.setRegion(FRAME_REGION.mana, rects.mana);
  const named = VISION_RECTS.filter(k => rects[k]);
  $('vision-rects').textContent = named.length
    ? named.map(k => `${k}: ${rects[k].w} × ${rects[k].h} px`).join(' · ')
    : 'Nic jeszcze nie zaznaczone.';
}

function combatConfig() {
  return {
    viewport: rects.viewport ?? EMPTY_RECT,
    crop: cropRect() ?? EMPTY_RECT,
    battle: rects.battle ?? EMPTY_RECT,
    hp: rects.hp ?? EMPTY_RECT,
    mana: rects.mana ?? EMPTY_RECT,
    grid_cols: num('grid-cols'),
    grid_rows: num('grid-rows'),
    bar_width: num('bar-width'),
    bar_height: num('bar-height'),
    bar_border: num('bar-border'),
    bar_tolerance: num('bar-tolerance'),
    black_max: num('black-max'),
    bar_colors: $('bar-colors').value.split(/[\s,]+/).filter(Boolean),
    has_self_bar: $('self-bar-on').checked,
    self_bar_x: num('self-bar-x'),
    self_bar_y: num('self-bar-y'),
    decision_radius: num('decision-radius'),
    battle_bar_width: num('battle-bar-width'),
    battle_bar_height: num('battle-bar-height'),
    battle_bar_border: num('battle-bar-border'),
    battle_row_pitch: num('battle-pitch'),
    battle_frame: $('battle-frame').value.trim(),
    // Ramka celu dzieli pole tolerancji z barwami pasków: to dwa różne progi
    // w Go, ale jedno pokrętło w panelu, bo strojenie ich osobno nie ma
    // praktycznego powodu, a każde dodatkowe pole to jedno więcej do pomylenia.
    battle_frame_tolerance: num('bar-tolerance'),
    battle_frame_coverage: num('battle-frame-coverage'),
  };
}

// fetchVision is diagnostics, so its failures are swallowed: a broken preview
// must never stop the frame loop that the actual bot depends on.
async function fetchVision() {
  if (visionPending) return;
  visionPending = true;
  try {
    const r = await fetch('/api/vision');
    if (r.ok) drawVision(await r.json());
  } catch { /* podgląd jest diagnostyką */ }
  finally { visionPending = false; }
}

function drawVision(view) {
  const crop = cropRect();
  if (!crop || !view?.have) return;
  const canvas = $('vision-canvas');
  canvas.width = crop.w; canvas.height = crop.h;
  const c = canvas.getContext('2d');
  if (stream) c.drawImage(video, crop.x, crop.y, crop.w, crop.h, 0, 0, crop.w, crop.h);
  else c.clearRect(0, 0, crop.w, crop.h);
  const radius = num('decision-radius');
  for (const b of view.bars) {
    c.strokeStyle = b.dist <= radius + 0.5 ? '#ff2bd1' : '#8899aa';
    c.strokeRect(b.x + 0.5, b.y + 0.5, num('bar-width') - 1, num('bar-height') - 1);
  }
  $('vision-info').textContent = view.bars.length
    ? view.bars.map(b => `${b.dx.toFixed(2)},${b.dy.toFixed(2)} · ${b.dist.toFixed(2)} kratki · HP ${Math.round(100 * b.hp)}%`).join('  |  ')
    : 'Nie widzę żadnego stwora.';
}

$('vision-canvas').addEventListener('pointerdown', e => {
  const crop = cropRect();
  if (!crop) { status('Najpierw zaznacz okno gry.', 'error'); return; }
  const p = point(e, $('vision-canvas'), crop.w, crop.h);
  // Klikasz środek paska; zapisujemy jego lewy górny róg, bo tym operuje detektor.
  $('self-bar-x').value = Math.max(0, p.x - Math.floor(num('bar-width') / 2));
  $('self-bar-y').value = Math.max(0, p.y - Math.floor(num('bar-height') / 2));
  saveForm();
  pushConfig();
});
```

- [ ] **Step 6: Zepnij blok z resztą panelu**

Cztery drobne zmiany w `web/panel.js`:

1. W `brainConfig()` dodaj na końcu obiektu `combat: combatConfig(),`.
2. W `pointermove` i `pointerup` rozgałęź po celu kalibracji:

```javascript
screenCanvas.addEventListener('pointermove', e => {
  if (!dragging) return;
  const p = point(e, screenCanvas, source.width, source.height);
  const box = {x: Math.min(p.x, dragging.x), y: Math.min(p.y, dragging.y),
    w: Math.abs(p.x - dragging.x) + 1, h: Math.abs(p.y - dragging.y) + 1};
  if (calibTarget() === 'minimap') {
    roi = box;
    marker = {x: Math.floor(roi.w / 2), y: Math.floor(roi.h / 2)};
  } else {
    rects[calibTarget()] = box;
  }
  drawScreen();
});
screenCanvas.addEventListener('pointerup', () => {
  dragging = null;
  if (calibTarget() === 'minimap') { drawCrop(); return; }
  applyVisionRegions();
  pushConfig();
});
```

3. W `drawScreen()`, po obrysie minimapy, obrysuj też prostokąty widzenia:

```javascript
    for (const key of VISION_RECTS) {
      const r = rects[key];
      if (!r) continue;
      c.strokeStyle = '#7cf';
      c.strokeRect(r.x * sx, r.y * sy, r.w * sx, r.h * sy);
    }
```

4. W `render(state)` na końcu dodaj pobieranie podglądu i wyzeruj prostokąty przy zmianie rozdzielczości źródła — w `setSource`, w gałęzi resetu, obok `roi = marker = null`, dopisz `rects = {viewport: null, battle: null, hp: null, mana: null}; applyVisionRegions();`:

```javascript
  if ($('vision-preview').checked && state.combat?.calibrated) fetchVision();
```

5. Dodaj nowe pola do zapamiętywanych i do nasłuchu. Do `REMEMBERED` i do listy w pętli `addEventListener('change', ...)` dopisz:

```javascript
  'calib-target', 'grid-cols', 'grid-rows', 'decision-radius',
  'bar-width', 'bar-height', 'bar-border', 'bar-tolerance', 'black-max',
  'bar-colors', 'self-bar-on', 'self-bar-x', 'self-bar-y',
  'battle-bar-width', 'battle-bar-height', 'battle-bar-border', 'battle-pitch',
  'battle-frame', 'battle-frame-coverage',
```

`vision-preview` **nie** wchodzi do `REMEMBERED`: to przełącznik diagnostyczny, który po odświeżeniu karty ma być zgaszony, tak jak przełączniki każące botowi działać.

- [ ] **Step 7: Uruchom testy i sprawdź, że przechodzą**

Run: `node --test webtests/*.cjs`
Expected: PASS, wraz ze wszystkimi istniejącymi testami panelu i kamery.

- [ ] **Step 8: Commit**

```bash
git add web/ webtests/
git commit -m "Dodaj kalibrację i podgląd widzenia do panelu"
```

---

### Task 9: Dokumentacja i pomiary z fazy 1

Faza 1 nie kończy się kodem, a **liczbami**: zmierzonym zakotwiczeniem, potwierdzoną (albo nie) siatką 15×11 i prawdziwą barwą ramki celu. Fazy 3–5 stoją na tych wartościach, więc muszą trafić do repo, nie do pamięci.

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-09-07-combat-and-loot-design.md` (sekcja Ryzyka)
- Create: `docs/superpowers/plans/2026-09-07-vision-layer-measurements.md`

- [ ] **Step 1: Zapisz pomiary**

Create `docs/superpowers/plans/2026-09-07-vision-layer-measurements.md` i wypełnij **zmierzonymi** wartościami:

```markdown
# Pomiary z fazy 1

Data pomiaru: ROK-MM-DD
Klatka odniesienia: `testdata/combat-capture.png`

| Co | Wartość | Jak zmierzone |
|---|---|---|
| siatka okna gry | 15 × 11 kratek | proporcje prostokąta okna gry, test `TestCombatFixtureGeometry` |
| piksele na kratkę | … × … px | `viewport.W / 15`, `viewport.H / 11` |
| geometria paska życia | … × … px, obwódka … px | powiększenie zapisanej klatki |
| barwy wypełnienia | … | odczyt pikseli, tolerancja … |
| próg czerni | … | najciemniejsze wypełnienie kontra najjaśniejsza obwódka |
| zakotwiczenie paska | dx = …, dy = … px | `Grid.AnchorFrom` z własnego paska, log `TestRealCaptureOffsets` |
| zakotwiczenie dużego stwora | rozjazd … kratki | porównanie logu z obrazem, zadanie 3 krok 8 |
| geometria paska w battle liście | … × … px, obwódka … px | powiększenie |
| odstęp wierszy battle listy | … px | odległość między dwoma mini-paskami |
| barwa i pokrycie ramki celu | …, … | odczyt pikseli wiersza z ramką |
| pasek HP / many | … × … px | prostokąty z kalibracji |

## Co z tego wynika dla dalszych faz

- …
```

- [ ] **Step 2: Uzupełnij ryzyko o zakotwiczenie dużych stworów**

W `docs/superpowers/specs/2026-09-07-combat-and-loot-design.md`, w punkcie 1 sekcji „Ryzyka", dopisz zdanie z faktycznym wynikiem pomiaru: albo „zmierzone, rozjazd poniżej pół kratki, stała wystarcza", albo „rozjazd X kratki — faza 3 traktuje kratkę dużego stwora jako niepewną".

- [ ] **Step 3: Opisz kalibrację w README**

W `README.md`, po rozdziale „Podgląd przechodności", dodaj rozdział „Widzenie: potwory i paski". Napisz w nim:

- co panel wycina i dlaczego wycinek, a nie całe okno gry — z uczciwym rachunkiem: **oszczędność około jednej czwartej**, bo okno ma tylko 11 kratek wysokości, a prawdziwe powody to ograniczenie pracy detektora i twarde ograniczenie zasięgu decyzji;
- że `frame.MaxBody` może wymagać podniesienia przy dużym oknie gry, i że **zmniejszenie okna gry w kliencie** tnie koszt kilkukrotnie bez żadnej straty, bo kratek dalej jest 15×11;
- kolejność kalibracji: okno gry → kliknięcie własnego paska na podglądzie → battle lista → paski HP i many;
- że własny pasek wyklucza się po **dokładnej pozycji**, a nie po bliskości do środka, i dlaczego (potwór stojący kratkę nad postacią);
- że sito danych mapy odrzuca paski na kratkach nieprzechodnich i że **nie jest pełne** — potwór na przechodniej kratce piętro wyżej przez nie przejdzie;
- że test „mieszany tłum" jest **jednostronny**: więcej pasków niż wierszy dowodzi obecności nie-potwora, ale brak flagi niczego nie dowodzi;
- że liczenie potworów **nie potrzebuje pozycji z minimapy**, bo kamera jest wyśrodkowana na postaci;
- jak uruchomić testy: `go test ./... -race` i `node --test webtests/*.cjs`, oraz że `.debug/vision-fixture.png` jest rysunkiem diagnostycznym z obrysami.

W rozdziale „Testy" dopisz zdanie o nowych pakietach i o tym, co sprawdza fixture `combat-capture.png`. W „Układ katalogów" dopisz `internal/vision`, `internal/battle`, `internal/vitals`.

- [ ] **Step 4: Uruchom całość ostatni raz**

Run: `go test ./... -race && node --test webtests/*.cjs`
Expected: PASS w obu.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/
git commit -m "Opisz kalibrację widzenia i zapisz pomiary z fazy 1"
```

---

## Czego ta faza świadomie nie robi

Żeby nikt nie szukał tego w kodzie: **nie ma tu śledzenia stworów w czasie** (identyfikatory śladów, ślad-duch po zniknięciu, potwierdzanie po `ConfirmMS`). `internal/combat` powstanie w fazie 3, gdzie jest naprawdę konsumowane — przez reguły czarów i przez kolejkę łupu. Do tego czasu `monsters_in_range` liczy **surowe** wykrycia z jednej klatki, bez potwierdzania, i tak jest opisane w panelu. Nie ma też maszyny stanów, dotykania sterownika ani jednego wciśniętego klawisza.
