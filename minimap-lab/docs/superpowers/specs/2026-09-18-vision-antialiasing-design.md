# Widzenie na prawdziwym kliencie — rozmyte brzegi, osobna tolerancja battle listy, ramka wokół ikonki

Data: 2026-09-18
Status: zaakceptowany do planowania
Gałąź: `brain-go`
Poprzednik: `2026-09-07-combat-and-loot-design.md` (faza 1) — ten dokument poprawia
fazę 1 tam, gdzie pierwsza prawdziwa klatka ją obaliła. Warunek wstępny dla
`2026-09-15-targeting-design.md`.

## Po co

Faza 1 widzenia została scalona 2026-09-07 bez ani jednej prawdziwej klatki z gry —
pomiary miały przyjść później. Przyszły 2026-09-15 i 2026-09-18, na trzech zrzutach,
i pokazały trzy rzeczy, których żaden syntetyczny test nie mógł pokazać:

1. Klient rysuje paski z **rozmytymi brzegami**. Skrajne wiersze wypełnienia są o
   1–2 px węższe od rdzenia, a `vision.confirm()` żąda identycznej szerokości w
   każdym wierszu. Pełny pasek przechodzi przypadkiem (prawa krawędź siedzi na
   stałej granicy kontenera), **ranny stwór nie przechodzi nigdy** — a to jego bot
   ma czytać w walce.
2. Battle lista potrzebuje **innej tolerancji barw niż okno gry** (80 kontra 20), a
   `CombatConfig` ma na to jedno wspólne pole. Przy 40 okno gry oddaje 1302
   fałszywe paski.
3. **Ramka celu to czerwony kwadrat 40×40 wokół ikonki stwora**, przesunięty w lewo
   od paska, a `battle.framed()` mierzy pokrycie jako ułamek szerokości całego
   wycinka — przy domyślnym 0,8 i wycinku 330 px żąda 264 px ciągłej czerwieni,
   gdy ramka daje 40.

Bez tych trzech poprawek celowanie nie ma na czym stanąć: `MonstersInRange` działa,
ale `BattleRows` i `TargetRow` — nie.

## Decyzje podjęte przed projektowaniem

Ustalone z użytkownikiem 2026-09-18. Nie wracamy do nich bez wyraźnej prośby.

1. **Najpierw ostry render, potem kod.** Pierwszy zrzut (3600×2338) był powiększeniem
   2× z wygładzaniem przez macOS. Użytkownik przełączył monitor na rozdzielczość
   natywną (5120×2880); rozmycie 2× zniknęło, ale rozmycie brzegów zostało — to
   klient sam wygładza krawędzie pasków. Kod trzeba dostosować, konfiguracja nie
   wystarczy.
2. **Tolerancja brzegowa w `confirm()`, nie rozluźnienie całego dopasowania.**
   Mediana ±2 px na wszystkich wierszach — mój pierwszy pomysł — odrzucona po
   konsultacji: przepuściłaby ukośne krawędzie, cyfry i plamy. Tolerancja dotyczy
   **tylko dwóch skrajnych wierszy wnętrza**, tylko **w dół** (węższy, nigdy
   szerszy) i domyślnie wynosi **zero**, czyli dzisiejsze zachowanie.
3. **Osobne pola dla battle listy** (`BattleBarTolerance`, `BattleBlackMax`,
   `BattleEdgeTolerance`), nie wspólny suwak. Okno gry już działa na 20/125/0 i nie
   wolno go ruszać.
4. **Ramka szukana w prostokącie ikonki**, opisanym względem paska
   (`IconOffsetX/Y`, `IconSize`), a `FrameCoverage` liczy się jako ułamek
   `IconSize`. Nie „poszerzyć pas szukania" — pas zależałby od tego, jak panel
   przyciął wycinek.
5. **Ścieżka architektoniczna**, bo zmiana dotyka `internal/vision`, z którego
   korzysta każdy detektor pasków w projekcie.

## Czego ten projekt NIE obejmuje

- Celowania, czarów, maszyny stanów — to spec z 2026-09-15, który czeka na ten.
- Trackera per stwór, lootu, odwrotu.
- Zmiany czytnika HP/many (`internal/vitals`) — działa na nasyceniu, przeszedł na
  prawdziwej klatce bez poprawek.
- Rozpoznawania niebieskiego paska własnego. W obu zrzutach pasek postaci był
  niebieski (patrz „Pomiary"); nie zgadujemy mechaniki klienta, tylko notujemy
  pozycję do zakotwiczenia.

## Stan wyjściowy i pomiary

Zrzut odniesienia: `testdata/combat-capture.png`, 5120×2880, 2026-09-18 13:37,
rozdzielczość natywna. Trzy stwory w oknie gry (dwa Muglex Clan Assassin z pełnym
HP, jeden Muglex Clan Footman ~50% — ranny i atakowany), trzy wiersze w battle
liście, ramka celu na wierszu 0.

### Co zmierzono

| Co | Wartość |
|---|---|
| okno gry | `Rect(637, 245, 3778, 2548)` — 3141×2303, proporcja 1,3639 (15/11 = 1,3636), kratka 209,4 px |
| wycinek | `Rect(1056, 245, 3359, 2548)` — `RecommendedCrop()` dla promienia 4 |
| battle lista | `Rect(4770, 900, 5100, 1150)` |
| pasek HP | `Rect(24, 154, 2202, 157)` — 3 wiersze, bo cyfry „147/160" zajmują 140–153 |
| pasek many | `Rect(2217, 154, 4392, 157)` |
| pasek stwora | wypełnienie 56×2 px rdzenia, profil pionowy `18 → 40 → 121 → 161 → 161 → 121 → 40 → 18` |
| pasek battle | wypełnienie 260×4 px rdzenia, profil `48 → 144 → 192 ×4 → 144 → 48` |
| odstęp wierszy battle | 44 px |
| ramka celu | kwadrat 40×40, krawędź 2 px, barwa `(201, 10, 10)` = `#c90a0a`, lewy górny róg `(4776, 936)` |
| pasek celu (wiersz 0) | róg `(4821, 967)` przy obwódce 1 → ikonka na `(−45, −31)` od rogu paska |
| pasek własny | niebieski `(0,0,255)`, rdzeń x 2129–2184, y 1233–1234 → z geometrią 62×8/3 róg `(2126, 1230)`, w wycinku `(1070, 985)` |
| zakotwiczenie | środek paska własnego względem środka kratki: `(−50, −163)` px = `(−0,24, −0,78)` kratki; na zrzucie z 2026-09-15 `(−0,23, −0,86)` — stabilne |

Zakotwiczenie sprawdzone na stworach: Assassin 1 wypada na `(0, −1)`, Footman na
`(−1, +1)` — całe kratki. Assassin 2 na `(3,78, 3,0)` — w pół kroku.

### Co działa, co nie

**Okno gry działa** przy `62×8`, obwódka `3`, tolerancja `20`, próg czerni `125`:
prawdziwy `vision.Find` oddaje dokładnie trzy paski, zero fałszywych, pasek własny
nie łapie się (jest niebieski). Działa, bo obwódka 3 wciąga oba wiersze przejścia
(40 i 121) do strefy „ma być ciemne", a 121 ≤ 125. Zostają dwa wiersze rdzenia
(161, 161) — identyczne. **Ta konfiguracja nie potrzebuje żadnej zmiany kodu.**

**Battle lista nie działa** i nie da się jej naprawić samą konfiguracją:

- wiersz przejścia ma wartość 144, a `validate()` tnie `BlackMax` do 128 — nigdy nie
  będzie „ciemny", więc musi być „wypełnieniem", co wymaga tolerancji ≥ 48;
- przy tolerancji 80 pełne paski przechodzą (`Fill=260` we wszystkich sześciu
  wierszach), ale ranny stwór daje `137, 138, 138, 138, 138, 137` — skrajne wiersze
  o piksel węższe, `confirm()` odrzuca;
- tolerancja 80 przeniesiona na okno gry (wspólne pole) daje 1302 fałszywe paski;
- ramka celu nigdy nie znaleziona, bo 40 px < 0,8 × 330 px.

Wszystko powyżej sprawdzone **prawdziwym kodem Go**, nie reimplementacją.

## Konsultacja

Gemini i Codex, 2026-09-18, na pytaniu o medianę ±2 px. Zgodnie:

- **±2 px na wszystkich wierszach jest za luźne** — 4 px rozrzutu przepuszcza ukośne
  krawędzie sprite'ów, glify liter z cieniem, pasy z podziałkami. Realne rozmycie
  natywnego rastra to najwyżej 1 px, i tyle pokazał pomiar.
- **Tolerancja tylko na skrajnych wierszach wnętrza.** Rozmycie siedzi na brzegu
  fizycznie; wiersze środkowe dzielą ten sam raster i muszą być identyczne — to one
  odrzucają pocisk, cyfrę obrażeń albo podziałkę przecinającą środek.
- **Asymetria** (Gemini): brzeg miesza się z ciemnym tłem, więc jest węższy lub równy
  rdzeniowi, nigdy szerszy. Wiersz szerszy od rdzenia to nie rozmycie.
- **Każdy wiersz musi dać niezerowy bieg** (Codex): `fillRun() == 0` to porażka, nie
  szerokość do tolerowania.
- **Zwracać rdzeń jako `Fill`** (Codex): dziś `run` bierze się z pierwszego
  napotkanego wiersza, czyli brzegowego — `HP()` zaniża o rozmyty piksel.
- **Jasność obwódki i dryf szerokości to osobne problemy** (Codex) — osobne pole
  progu czerni dla battle listy jest potrzebne niezależnie od tolerancji brzegowej.

Odrzucone alternatywy: wykrywanie najpierw statycznego kontenera paska (Gemini B) —
paski stworów nie mają widocznego kontenera; binaryzacja po nasyceniu (Gemini C) —
to inny detektor, nie poprawka.

## Architektura docelowa

### A. `internal/vision` — tolerancja brzegowa w `confirm()`

```go
type Options struct {
    // ... Geometry, Colors, Tolerance, BlackMax, Exclude, ExcludeTolerance ...

    // EdgeTolerance is how many pixels narrower than the core the first and
    // last inner rows may be. The client antialiases the edge of a bar, and
    // the antialiased row blends toward the dark background, so it measures
    // shorter - never longer. Zero, the default, demands the exact match the
    // detector always demanded.
    EdgeTolerance int
}
```

`confirm` zmienia sygnaturę: `confirm(im, bar) (fill int, ok bool)`. Parametr `run`
znika — jego źródłem był pierwszy wiersz, na jaki natrafił `Find`, czyli brzegowy.

Algorytm:
1. Prostokąt `bar.X..+Width × bar.Y..+Height` musi mieścić się w obrazie (bez zmian).
2. Dla każdego z `InnerHeight` wierszy wnętrza policz `fillRun`. **Zero w
   którymkolwiek = odrzucenie.**
3. Wyznacz rdzeń: przy `InnerHeight ≥ 3` to wiersze od drugiego do przedostatniego,
   **wszystkie identyczne**; przy 1–2 wierszach rdzeniem jest pierwszy i wszystkie
   muszą mu równać (jak dziś).
4. Skrajne wiersze (pierwszy i ostatni, gdy `InnerHeight ≥ 3`): `0 ≤ rdzeń − bieg ≤
   EdgeTolerance`. Szerszy od rdzenia — odrzucenie, niezależnie od tolerancji.
5. Obwódka ciemna dookoła — bez zmian.
6. Zwróć `rdzeń` jako `Fill`.

`Find` zapisuje `bar.Fill = fill` z `confirm`, nie z pierwszego `fillRun`. Przy
`EdgeTolerance == 0` krok 4 wymaga równości, więc każdy istniejący test przechodzi
bez zmian — to jest sprawdzane wprost.

### B. `internal/brain/combatconfig.go` — osobne pola battle listy

```go
// Okno gry - istniejące:
BarTolerance     int `json:"bar_tolerance"`      // dom. 12 → pomiar: 20
BlackMax         int `json:"black_max"`          // dom. 48 → pomiar: 125
BarEdgeTolerance int `json:"bar_edge_tolerance"` // NOWE, dom. 0

// Battle lista - NOWE:
BattleBarTolerance  int `json:"battle_bar_tolerance"`  // dom. 80
BattleBlackMax      int `json:"battle_black_max"`      // dom. 60
BattleEdgeTolerance int `json:"battle_edge_tolerance"` // dom. 1
```

`withDefaults()`: zero = domyślne dla trzech nowych pól battle; `BarEdgeTolerance`
zostaje zerem (zero jest tu prawidłową wartością, nie brakiem — więc **nie** ma
domyślnej powyżej zera). `validate()`: tolerancje 1–128, progi czerni 1–128,
tolerancje brzegowe 0–4; sprawdzenie „barwa nie zostanie pochłonięta"
(`maxChannel − tolerancja > BlackMax`) uruchamiane **dwa razy**, dla pary okna gry i
dla pary battle listy, ta druga tylko gdy `Battle` niepuste. `battleOptions()` czyta
pola battle, `barOptions()` pola okna gry.

Domyślne 80/60/1 to wartości zmierzone. Domyślne okna gry (12/48) zostają jak były —
zmiana domyślnych to nie jest cel tego projektu; kalibracja idzie przez panel.

`BarColors` **zostaje jedną, wspólną listą.** Battle lista rysuje te same barwy
(rdzeń 192 zamiast 161 to znów faza subpikselowa), a tolerancja 80 wokół `#009b00`
sięga od 75 do 235 — obejmuje i 192, i wiersz przejścia 144. Sprawdzenie
pochłaniania barwy przechodzi dla obu par: `155 − 20 = 135 > 125` i
`155 − 80 = 75 > 60`.

`checkBar` podnosi sufit szerokości paska z 256 do **1024 px**: zmierzony pasek
battle listy ma 262 px na ekranie 5K, a panel boczny klienta da się jeszcze
rozszerzyć. Bez tego `validate()` odrzuciłoby zmierzoną geometrię z komunikatem o
zakresie 3–256. Sufit wysokości (64) i obwódki (8) zostają.

### C. `internal/battle` — ramka wokół ikonki

```go
type Options struct {
    // ... Geometry, Colors, Tolerance, BlackMax, EdgeTolerance, RowPitch ...

    // IconOffsetX/Y place the creature icon's top-left corner relative to the
    // bar's, and IconSize is the icon's side. The client draws the attack
    // frame round the icon, not round the row, so that square is where the
    // frame is looked for.
    IconOffsetX, IconOffsetY, IconSize int
    Frame          vision.Color
    FrameTolerance int
    // FrameCoverage is now the fraction of IconSize a run of the frame colour
    // must cover on one line.
    FrameCoverage float64
}
```

`framed(im, b)`: prostokąt ikonki `(b.X+IconOffsetX, b.Y+IconOffsetY)` o boku
`IconSize`, przycięty do obrazu. Dla każdego wiersza prostokąta: najdłuższy bieg
barwy ramki **w kolumnach prostokąta**; trafienie, gdy `≥ FrameCoverage × IconSize`.
Pas `RowPitch` znika z `framed` — zostaje w `Truncated`. Ikonki sąsiednich wierszy
nie nachodzą (40 px boku przy 44 px odstępu), więc dwóch celów naraz nie będzie.

`CombatConfig` dostaje `BattleIconOffsetX`, `BattleIconOffsetY`, `BattleIconSize`
(walidacja: offsety −512..512, bok 4–256, pole wymagane gdy `Battle` niepuste).
`BattleFrameCoverage` zachowuje zakres 0,05–1, ale zmienia znaczenie — README i
etykieta w panelu mówią „ułamek boku ikonki".

### D. Fixture i testy realnej klatki

`testenv.CombatFixture` rośnie o parametry detektora — dziś testy realnej klatki
używają sztywnych `classic` (27×4/1) i `mini` (20×3/1), które nigdy nie pasowały do
żadnego klienta:

```go
BarGeometry     vision.Geometry   // 62×8/3
BarTolerance    int               // 20
BlackMax        int               // 125
BattleGeometry  vision.Geometry   // 262×8/1
BattleTolerance int               // 80
BattleBlackMax  int               // 60
BattleEdge      int               // 1
RowPitch        int               // 44
Frame           vision.Color      // 201,10,10
IconOffsetX, IconOffsetY, IconSize int  // -45, -31, 40
```

`TestFindOnRealCapture`, `TestRealCaptureOffsets`, `TestReadOnRealCapture` czytają je
z fixture zamiast z `classic`/`mini`. Syntetyczne testy zostają na `classic`/`mini`.

`SelfBar = (1070, 985)` z komentarzem, że pasek był niebieski i pozycja pochodzi z
pomiaru pikseli, nie z detektora.

### Panel

Zakładka **Walka**, sekcja kalibracji: obok „Battle: szerokość/wysokość/obwódka
paska" dochodzą „Battle: tolerancja barw", „Battle: próg czerni", „Battle:
tolerancja brzegu", „Ikonka: przesunięcie X/Y", „Ikonka: bok"; obok „Tolerancja
barw"/„Próg czerni" okna gry — „Tolerancja brzegu". Etykieta pokrycia ramki zmienia
się na „Pokrycie ramki (ułamek boku ikonki)". Pola jadą w `PUT /api/config` jak
reszta; nic w panelu nie decyduje.

## Testy

- `internal/vision`:
  - syntetyczny pasek z brzegami o 1 px węższymi od rdzenia: odrzucony przy
    `EdgeTolerance=0`, przyjęty przy `1`, `Fill` równy rdzeniowi, nie brzegowi;
  - brzeg **szerszy** od rdzenia o 1 px: odrzucony przy każdej tolerancji;
  - wiersz środkowy o 1 px inny niż pozostałe: odrzucony przy `EdgeTolerance=4`;
  - `InnerHeight ∈ {1, 2}` z `EdgeTolerance=2`: zachowanie jak przy 0;
  - jeden wiersz z `fillRun == 0`: odrzucony;
  - **cały istniejący zestaw** przechodzi bez zmian przy domyślnym zerze;
  - realna klatka: 3 paski, `Fill` 56/28/56.
- `internal/battle`:
  - syntetyczna ramka wokół ikonki na `(−45, −31)` od paska: dziś `Targeted=false`,
    po zmianie `true`; ramka w ikonce **sąsiedniego** wiersza nie zalicza;
  - realna klatka: 3 wiersze, `Fill` 260/138/260 (pomiar przy tolerancji 80),
    `TargetRow=0`, tylko wiersz 0 `Targeted`.
- `internal/brain/combatconfig_test.go`: `withDefaults()` dla nowych pól; walidacja
  zakresów; pochłanianie barwy sprawdzane osobno dla obu par; `battleOptions()`
  przenosi pola battle, `barOptions()` okna gry.
- `internal/testenv`: `TestCombatFixtureGeometry` z liczbami z tabeli.
- `internal/vitals`: `TestReadOnRealCapture` — HP ≈ 91,9 % (147/160), mana 100 %.
- `webtests/vision_test.mjs`: nowe pola w `config()`, etykiety.

## Ryzyka

1. **Inny klient, inne rozmycie.** Zmierzono jeden klient na jednym monitorze.
   `EdgeTolerance` jest polem, nie stałą — jeśli inny zoom da 2 px, użytkownik
   podnosi wartość. Walidacja kończy się na 4, bo więcej to już nie brzeg.
2. **Próg czerni 125 w oknie gry** jest wysoki: ciemna trawa i podłogi lochów
   przechodzą jako „ciemne". Chroni `confirm()` — trawa nie tworzy prostokąta o
   identycznych wierszach rdzenia w barwie paska. Zmierzone: zero fałszywych na
   pełnym wycinku 2303×2303.
3. **Niebieski pasek własny.** Jeśli w innej sytuacji klient narysuje go zielono,
   wykluczenie po pozycji (`SelfBar`, tolerancja 2 px) działa jak zaprojektowano.
   Jeśli pozycja się przesuwa, licznik zyskuje jednego fałszywego stwora na środku —
   to widać w panelu jako pasek na `(0, 0)`.
4. **Ikonka innego rozmiaru** przy innym skalowaniu interfejsu klienta —
   `BattleIconSize` jest polem z tego samego powodu co geometria paska.

## Co zostaje na potem

- Celowanie i czary — spec z 2026-09-15, po tym planie.
- Automatyczne wyliczanie `IconOffset` z kliknięcia w panelu (jak `AnchorFrom`) —
  gdy okaże się, że ręczne wpisanie trzech liczb boli.
