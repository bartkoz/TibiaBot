# Celowanie i czary — fazy 2 i 3 projektu walki

Data: 2026-09-15
Status: zaakceptowany do planowania
Gałąź: `brain-go`, po leczeniu (`3cddc40`) i podziale panelu (`a3f508e`)
Poprzednik: `2026-09-07-combat-and-loot-design.md` — ten dokument realizuje jego
fazy 2 i 3 i **zastępuje** ich opis tam, gdzie się różni.

## Po co

Bot widzi potwory wokół postaci, czyta battle listę razem z ramką celu, leczy
się i chodzi po trasie. Nie atakuje. Postać otoczona przez stwory idzie dalej,
jakby ich nie było, a klient nie zaatakuje nikogo sam.

Ten projekt daje botowi ręce: wchodzi w walkę, gdy stwór jest blisko, wskazuje
cel klawiszem, rzuca czary według listy reguł, i wraca na trasę, gdy nie ma
kogo bić. Nie zbiera łupu i nie ucieka — to fazy 4 i 5.

## Decyzje podjęte przed projektowaniem

Ustalone z użytkownikiem 2026-09-07 (spec walki) i 2026-09-15. Nie wracamy do
nich bez wyraźnej prośby.

1. **Ruch w walce robi klient** („Chase opponent"). Sami planujemy wyłącznie
   odwrót, a ten jest w fazie 5. W `Fighting` bot nie wysyła klawiszy chodzenia.
2. **Gatunków potworów nie rozróżniamy.**
3. **Cel wskazujemy hotkeyem „Attack next target"**, który klient oficjalny
   udostępnia i który użytkownik ma przypisany — u niego jest to **spacja**. To zmiana względem specu
   walki, który zakładał klik w battle listę: klik jest zbędny, skoro jest
   klawisz, a klawisz nie ma skutków ubocznych chybionego kliknięcia.
   `ClickPoint`, strefy kliknięć i `Emitter.ClickButton` przesuwają się do
   fazy lootu, która naprawdę ich potrzebuje.
4. **Battle lista w kliencie jest przefiltrowana do potworów.** Bot polega na
   tym, że „następny cel" nigdy nie jest graczem; to ustawienie klienta, nie
   nasze.
5. **Zakres to faza 2 i cała faza 3**: maszyna stanów, celowanie, reguły
   czarów, budżet, panel. Nie sama maszyna stanów i nie sam atak.
6. **Przed planem użytkownik zgrywa prawdziwą klatkę z walki** do
   `testdata/combat-capture.png` i wypełnia `testenv.CombatCalibration()`.
   Pomiary z fazy 1 nie zostały wykonane, a rozpoznanie ramki celu jest
   fundamentem celowania — bez realnego zrzutu testy fazy 3 stałyby na
   syntetykach.
7. **Testowanie na żywo na macOS.** Kody klawiszy dochodzą na oba systemy, ale
   ręcznie sprawdzany jest `input_darwin.go`.

## Czego ten projekt NIE obejmuje

- **Kliknięć myszą.** Żadnego `ClickPoint`, stref ani `ClickButton` — faza 4.
- **Śledzenia stworów per identyfikator** (`Tracker` ze specu walki). Faza 3
  potrzebuje tylko *ilu* stworów jest w promieniu, nie *których*; heurystyki
  asocjacji strojone bez jednej prawdziwej klatki byłyby zgadywaniem. Tracker
  wraca w fazie 4, gdzie kolejka łupu pyta „gdzie stał ten, który przepadł".
- **Lootu i odwrotu.** Fazy 4 i 5.
- **Czarów poza `Fighting`** — buffów w drodze, przywołań. Reguły oceniane są
  tylko w walce.
- **Czytania komunikatów klienta** („You are exhausted"). Emisja klawisza jest
  jedynym dowodem, jaki mamy, i wystarcza do liczenia cooldownów.
- **Wykrywania, czy czar naprawdę trafił.** Liczba pasków w promieniu nie
  dowodzi pokrycia obszarem czaru; przyjmujemy to świadomie.

## Stan wyjściowy

Faza 1 walki scalona (`6939adc`): `internal/vision` znajduje paski nad
stworami i przelicza je na kratki od postaci, `internal/battle` czyta wiersze
battle listy i ramkę celu, `internal/vitals` czyta HP i manę. Pętla publikuje
`CombatState` z `MonstersInRange`, `BattleRows`, `TargetRow`, `MixedCrowd`.
Sterownik nie wysyła w związku z walką ani jednego klawisza; `Controls` zna
`Walk`, `UseHotkey`, `Heal`.

Leczenie (`3cddc40`) dało wzór, który ten projekt kopiuje: czysty silnik reguł
w osobnym pakiecie, konfiguracja z walidacją w `brain`, krok w klatce przed
dopasowaniem minimapy, cooldown startujący dopiero po potwierdzonej emisji,
budżet klawiszy rozbity na przeznaczenia i wywłaszczenie kroku flagą.

Nazwa `brain.Tracker` jest zajęta przez śledzenie pozycji z minimapy — dlatego
w tym dokumencie nie ma żadnego „trackera".

## Konsultacja

Szkic konsultowany 2026-09-15 z Gemini i Codeksem. Co zmieniło projekt:

- **Zgodnie oba:** tracker per stwór odłożyć, debounce liczników wystarcza;
  cooldown per hotkey, nie per reguła; 300 ms między próbami celowania to za
  mało przy 350–500 ms obiegu klawisz → klient → render → capture → mózg, bo
  drugi tap „Attack next" przed pojawieniem się ramki **przeskakuje na kolejny
  cel**; bezpiecznik na nieosiągalny cel dodać teraz, ale nie jako gołe
  wyjście po czasie.
- **Gemini:** wyjście z walki po „brak pasków w promieniu" jest pułapką na
  uciekającego potwora — klient go goni, a bot po 600 ms stuka Escape i
  odchodzi. Ramka celu widoczna = zostajemy w walce.
- **Codex:** wejście musi wymagać niepustej battle listy, inaczej paski bez
  wierszy dają pętlę wejście → Escape → wyjście; Escape na wyjściu ma być akcją
  oczekującą, bo leczenie albo budżet mogą go zablokować w klatce wyjścia; krok
  w locie przy wejściu w walkę nie może uczyć blokad, bo ruch klienta w pogoni
  fałszuje ocenę kroku; `bars > rows` to heurystyka, nie dowód bezpieczeństwa
  AoE.
- **Odrzucone:** globalny odstęp między czarami. Cooldown grupowy to sprawa
  `cooldown_ms` w regule użytkownika, a globalny odstęp gubi DPS i blokuje
  czary wsparcia. Jeden klawisz na klatkę i cooldown per hotkey wystarczą.

## Architektura docelowa

### Nowy pakiet i nowe pliki

```
internal/fight/
  presence.go      debounce jednego warunku logicznego w czasie zgrania klatki
  activity.go      maszyna stanów Travelling ↔ Fighting
  targeter.go      próby celowania, backoff, bezpiecznik zastoju
  rules.go         silnik reguł czarów
  *_test.go        tabelkowe

internal/brain/
  fight.go         krok walki w klatce; jedyny właściciel l.fight*
  fightconfig.go   FightConfig, withDefaults, validate
```

`internal/fight` jest **czystą decyzją**: nie ma zegara, nie czyta pikseli, nie
dotyka klawiatury. Dostaje liczby i czas zgrania klatki, oddaje decyzję. To ta
sama zasada, która pozwala testować `internal/heal` tabelką.

### Kolejność w klatce

```
sesja → duplikat → observeVision → healStep → fightStep → bramka wyszukiwania
      → match (gorutyna) → applyMatch → follow
```

`fightStep` stoi **przed** dopasowaniem minimapy z tego samego powodu co
leczenie: kamera klienta jest wyśrodkowana na postaci, więc walka nie
potrzebuje pozycji w świecie, a zgubiona pozycja — po ciemku, w wodzie — nie
może oznaczać bezczynnej postaci. Sito danych mapy używa ostatniej znanej
pozycji, tak jak dziś na ścieżce klatki-duplikatu.

Priorytet klawiszy w jednej klatce: **leczenie → oczekujący Escape →
celowanie → czar → krok.** Jeden klawisz nieleczący na klatkę: flaga
`fightKeyLastFrame` wywłaszcza krok w `follow()` dokładnie tak, jak
`healedLastFrame`.

### Pewność: debounce liczników (`fight.Presence`)

Zamiast trackera per stwór — jeden mały typ, który odpowiada na pytanie „czy
warunek logiczny trzyma się wystarczająco długo":

```go
type Presence struct { /* since, frames, lastAt */ }

// Observe notuje wynik warunku na świeżej klatce. Zwraca true, gdy warunek
// był prawdziwy na każdej klatce od `since`, przez co najmniej confirm czasu
// zgrania i na co najmniej dwóch klatkach.
func (p *Presence) Observe(holds bool, capturedAt time.Time, confirm time.Duration) bool
```

Trzy własności, każda odpowiada na nazwany tryb awarii:

- **≥ 2 klatki i ≥ `ConfirmMS`** — 150 ms przy 10 Hz to 1–2 klatki; sam czas
  przepuściłby jednoklatkowy artefakt, sama liczba klatek — dwa artefakty z
  rzędu przy przyspieszonej kamerze.
- **Przerwa > 500 ms między klatkami zeruje ciągłość.** Kamera, która stanęła
  i wróciła, nie może „mieć za sobą" 150 ms, których nikt nie obserwował.
- **Duplikat klatki nie posuwa debounce'u.** `handleFrame` odrzuca go, zanim
  cokolwiek zobaczy, więc to wychodzi samo — ale test to sprawdza.

Debounce jest per warunek: wejście w walkę ma swój, każda reguła czaru swój.
Ktoś mógłby zauważyć, że „trzy stwory przez 150 ms" nie znaczy „te same trzy
stwory". Znaczy „dość stworów, by rzucić czar obszarowy", i tylko o to pytamy.

### Maszyna aktywności (`fight.Activity`)

```go
type State int
const (
    Travelling State = iota
    Fighting
)

type Observation struct {
    Enabled    bool      // przełącznik „Atakuj" i skalibrowana battle lista
    InRange    int       // pewne stwory w promieniu decyzyjnym po sicie mapy
    BattleRead bool      // region battle listy przyszedł w klatce
    Rows       int
    HasTarget  bool      // któryś wiersz ma ramkę
    Blocked    bool      // pauza po zastoju albo oczekujący Escape
    CapturedAt time.Time
}

// Options to czasy z FightConfig przeliczone na time.Duration; jedna struktura
// dla Activity, Targetera i Engine, żeby pętla nie żonglowała siedmioma
// liczbami w każdym wywołaniu.
type Options struct {
    Confirm, LeaveFight, TargetRetry, TargetBackoff, TargetStall time.Duration
    TargetAttempts int
}

type Transition int
const (
    None Transition = iota
    Entered
    Left
)

func (a *Activity) Observe(o Observation, opts Options) Transition
func (a *Activity) State() State
func (a *Activity) Force(to State)   // wyłączenie „Atakuj", rozbrojenie
```

**Wejście** w `Fighting`, gdy wszystko naraz: `Enabled` · `BattleRead && Rows >
0` · `InRange ≥ 1` potwierdzone przez `Presence` (`ConfirmMS`, dom. 150 ms) ·
`!Blocked`. Wymaganie `Rows > 0` jest poprawką Codeksa: paski w promieniu bez
wierszy w battle liście — stwór niewidoczny dla listy, filtr, złe zaznaczenie —
dawałyby pętlę wejście → Escape → wyjście co 750 ms.

**Wyjście**, gdy warunek `!HasTarget && (InRange == 0 || Rows == 0)` trzyma się
przez `LeaveFightMS` (dom. 600 ms). Jeden timer na złożony warunek: obie
połówki znaczą „nie ma kogo bić", więc ich przeplatanie się wolno sumować.
**Widoczna ramka celu zawsze trzyma w walce** — to poprawka Gemini: ranny
stwór odbiega na 5+ kratek, klient go goni, a bot bez tej klauzuli po 600 ms
stukałby Escape i zostawiał go żywego.

**Nieodczytana battle lista** (`BattleRead == false`: region nie przyszedł w
klatce) jest **nieznana**, nie pusta: blokuje wejście, nie napędza wyjścia.

Zdarzenia przejść, obsługiwane w `brain/fight.go`:

- **Entered** → `executor.DropPending()`. Krok w locie jest porzucony bez
  nauki blokad: klucz już poleciał, postać się ruszy albo nie, ale to, czy
  doszła, zależy teraz od pogoni klienta i stworów w drodze, nie od ściany.
  Uczenie z tego kroku byłoby dokładnie tym „pending-step misattribution", o
  którym oba modele mówiły w specu walki. `Follower` nie jest ruszany: postać
  stojąca po walce poza ścieżką dostaje od `remainingPath` nil i sama wraca do
  planera — już to potrafi.
- **Left** → `escapeDue = true`. Każda kolejna klatka próbuje
  `Driver.CancelTarget`, chyba że w tej klatce poleciało leczenie; dopóki
  `escapeDue`, **krok jest zablokowany i wejście w walkę też**. Escape jest
  akcją oczekującą, nie jednorazową próbą, bo klatka wyjścia może przypaść na
  leczenie albo pusty budżet, a Escape, który nie poleciał, zostawia klienta
  goniącego stwora do następnej komnaty. Celownik zeruje próby i backoff.
- w `Fighting`: `follow()` woła `executor.Observe` (żeby stan wykonawcy był
  aktualny), ale nie pyta o intencję; `routeNext` = „Walka."; dopasowanie
  minimapy biegnie dalej, pozycja jest śledzona i publikowana.
- wyłączenie „Atakuj" w walce → `Force(Travelling)` z `escapeDue`. Rozbrojenie
  sterownika → `Force(Travelling)` **bez** Escape, bo nie ma go jak wysłać;
  po uzbrojeniu wejście od zera przez pełny debounce.

Tabela przejść jest testowana na przeplot klatek, który migałby bez histerezy:
stwór na granicy promienia wchodzący i wychodzący co klatkę nie może dawać
więcej niż jednego wejścia i jednego wyjścia na cykl `LeaveFightMS`.

### Celowanie (`fight.Targeter`)

```go
type Targeter struct { /* lastTap, lastSeen, attempts, backoffUntil, minHP, stallSince */ }

type TargetInput struct {
    HasTarget  bool
    TargetHP   float64   // HP wiersza z ramką, 0–1; ważne tylko gdy HasTarget
    Rows       int
    CapturedAt time.Time
}

type TargetDecision struct {
    Tap    bool          // stuknij attack_key
    Stall  bool          // cel nie ginie — wyjdź i zrób pauzę
    Reason string
}

func (t *Targeter) Decide(in TargetInput, opts Options) TargetDecision
func (t *Targeter) Tapped(at time.Time)      // emisja potwierdzona
func (t *Targeter) Reset()                   // wyjście z walki
```

W `Fighting`, gdy `!HasTarget && Rows > 0`: tap `attack_key`, ale tylko gdy
`CapturedAt ≥ kotwica + TargetRetryMS` (dom. **600 ms**), gdzie kotwica to
późniejsza z: ostatnia **emisja** klawisza ataku, ostatnia klatka z widoczną
ramką. Jedno pole załatwia dwie rzeczy z konsultacji: nie stukamy drugi raz,
zanim klient zdąży pokazać ramkę po pierwszym (przeskok na kolejny cel), i nie
stukamy po jednoklatkowym zaniku ramki (przeskok z prawidłowego celu). Pauza
0,6 s między zabiciami to cena, którą przyjmujemy.

Kotwica jest mierzona od **emisji do czasu zgrania**, nie od decyzji do
decyzji: pytamy, czy klatka zrobiona 600 ms po stuknięciu nadal nie pokazuje
ramki. To ta sama zasada, co odstęp 500 ms w leczeniu.

Próby liczą **wyłącznie emisje** — `Tapped` woła się dopiero po
`Status == "emitted"`; odmowa budżetu nic nie zużywa. Po `TargetAttempts`
(dom. 3) emisjach bez ramki → backoff `TargetBackoffMS` (dom. 2000 ms), w
którym celownik milczy, ale reguły czarów bez `requires_target` dalej działają.
Ramka widoczna → próby = 0.

**Bezpiecznik zastoju.** Stwór za wodą albo za płotem stoi w promieniu, ma
ramkę, klient go „goni" w miejscu, i bez tego bezpiecznika bot stoi tak do
rozbrojenia. Celownik śledzi HP wiersza z ramką jako **najniższe dotąd
widziane dla tego celu** — monotoniczne, więc odporne na drgania odczytu
mini-paska o rozdzielczości kilku procent. `stallSince` zeruje się, gdy ramka
pojawia się po nieobecności (nowy cel) albo minimum spada (trafiliśmy). Gdy
`CapturedAt − stallSince ≥ TargetStallMS` (dom. **15 s**) → `Stall`: pętla
robi `Force(Travelling)` z `escapeDue` i ustawia `pauseUntil = now +
FightPauseMS` (dom. **10 s**). W pauzie trasa idzie, leczenie działa, wejście w
walkę jest zablokowane. Bot odjeżdża od nieosiągalnego stwora plasterkami
czasu, bez wykrywania „zmiany otoczenia", które oba modele nazwały trudnym.

Odrzucone: gołe `FightMaxMS`. Wyjście po czasie do `Travelling` wraca do tej
samej walki 150 ms później — „epileptic loop" — i bez pauzy nic nie daje.

Użytkownik z wolno ginącymi celami podnosi `TargetStallMS`; z tanku bossa ten
bezpiecznik nie jest dla niego.

### Reguły czarów (`fight.Engine`)

```go
type Rule struct {
    Enabled        bool    `json:"enabled"`
    Hotkey         string  `json:"hotkey"`
    MinMonsters    int     `json:"min_monsters"`
    Radius         float64 `json:"radius"`
    CooldownMS     int     `json:"cooldown_ms"`
    MinManaPct     float64 `json:"min_mana_pct"`
    RequiresTarget bool    `json:"requires_target"`
}

type Crowd struct {
    Distances []float64  // pewne stwory po sicie mapy, w kratkach, rosnąco
    Mixed     bool       // więcej pasków niż wierszy battle listy
}

func (c Crowd) Within(radius float64) int   // dist <= radius + 0.5

type Decision struct {
    Fire   bool
    Index  int
    Hotkey string
    Reason string
}

func (e *Engine) SetRules(rules []Rule)   // zeruje też debounce'y
func (e *Engine) Decide(crowd Crowd, mana vitals.Reading, hasTarget, blockMixed bool,
    capturedAt time.Time, confirm time.Duration) Decision
func (e *Engine) Emitted(hotkey string, at time.Time)
```

`Crowd.Within` używa **tego samego zaokrąglenia** co `MonstersInRange`:
`dist ≤ R + 0,5`, czyli najbliższa cała kratka. Reguła o promieniu 1 widzi
pierścień 3×3, o promieniu 2 — 5×5. Odległości liczy `brain/fight.go` z
`l.bars` przez `visionGrid.Offset`, po odrzuceniu kratek, które mapa nazywa
ścianą — dokładnie jak `finishVision`.

Każda reguła ma własny `Presence`: warunek `Within(Radius) ≥ MinMonsters` musi
trzymać się przez `ConfirmMS`, tak jak wejście w walkę. Jednoklatkowy artefakt
nie rzuca czaru za 200 many.

Reguła jest **wykonalna**, gdy: włączona ∧ licznik potwierdzony ∧
(`MinManaPct == 0` ∨ mana czytelna i `≥ MinManaPct`) ∧ cooldown **hotkeya**
minął (`capturedAt − emisja ≥ CooldownMS`) ∧ (`!RequiresTarget` ∨ `hasTarget`)
∧ ¬(`blockMixed` ∧ `crowd.Mixed` ∧ `MinMonsters > 1`).

Wygrywa **pierwsza wykonalna od góry** — nie pierwsza pasująca — jeden czar na
klatkę. `Emitted` startuje cooldown dopiero po potwierdzonej emisji. `Reason`
mówi, co zablokowało najwyższą regułę, która by pasowała; to zdanie panel
pokazuje użytkownikowi.

Cooldown jest per hotkey, bo dwie reguły z tym samym czarem (obszarowy przy 4
stworach w promieniu 1 i ten sam przy 6 w promieniu 2) muszą dzielić jego
cooldown, a nie omijać go na przemian. Cooldown grupowy klienta (2 s dla czarów
ataku) użytkownik wpisuje w `cooldown_ms` — bot go nie zna i nie udaje, że zna.

Blokada mieszanego tłumu jest **bramką ostrożności, nie dowodem**:
`bars > rows` wykrywa niektóre sytuacje z graczem na ekranie, `bars ≤ rows`
niczego nie gwarantuje (przewinięta lista, stwór poza wycinkiem). Domyślnie
włączona; użytkownik na spawnie z NPC-em na stałe w kadrze ją wyłącza.

Czary tylko w `Fighting`, po celowaniu w priorytecie klatki: bez ramki klient
nie zaatakuje wręcz, a stwór bez celu nie ginie. W backoffie celownika czary
bez `requires_target` dalej lecą.

### Sterownik i budżet

```go
func (d *Driver) Cast(key string, observationAge time.Duration) Result
func (d *Driver) CancelTarget(observationAge time.Duration) Result
```

`Cast` to bliźniak `Heal`: literalny klawisz z listy reguł, sprawdzony w
`hotkeyNames`, bez semantyki „akcji w locie", której ma `UseHotkey`, i bez
oglądania się na akcję pięter w locie. `CancelTarget` stuka `escape`. Oba idą
przez `guardLocked` z nowym przeznaczeniem `purposeCombat`.

`Controls` w mózgu rośnie o te same dwie metody; `fakeControls` w testach
pętli dostaje `casts` i `cancels`.

| Przeznaczenie | Limit na sekundę |
|---|---|
| sufit globalny | 8 |
| chodzenie | 3 |
| akcje pięter | 3 |
| **walka** (attack_key, czary, escape) | **3** |
| leczenie | 2 |
| nieleczące razem | 6 |

Płot nieleczący (6) obejmuje walkę bez zmian w kodzie, bo liczy każdy tap
`≠ purposeHeal`. Rezerwa leczenia zostaje wygrodzona tak, jak była.

`hotkeyNames` dochodzą `escape`, `space`, `tab`, z kodami w obu tablicach:
darwin 53 / 49 / 48, Windows `0x1B` / `0x20` / `0x09`. `Emitter` bez zmian —
żadnego `ClickButton`.

### Kolizje klawiszy w `PUT /api/config`

`healKeyConflict` uogólnia się do `keyConflicts`. Kategorie: akcje pięter,
kierunki, leczenie, walka (`attack_key` i hotkeye czarów). Klawisz
współdzielony **między kategoriami** jest odrzucany z komunikatem nazywającym
obie strony („klawisz f3: reguła czaru 2 i reguła leczenia 1"). W obrębie
kategorii duplikaty wolno — dwie reguły z tym samym czarem to jeden cooldown.
`escape` jako klawisz ataku, czaru, leczenia lub akcji — odrzucony, bo Escape
ma w tym projekcie jedno znaczenie.

Handler HTTP jest jedynym miejscem, które widzi wszystkie kategorie naraz —
klawisze akcji i kierunków idą do sterownika, reguły do pętli — dlatego
sprawdzenie jest tam, przed zapisaniem czegokolwiek.

### Konfiguracja

```go
type FightConfig struct {
    Enabled         bool         `json:"enabled"`          // „Atakuj"
    AttackKey       string       `json:"attack_key"`
    Spells          []fight.Rule `json:"spells"`
    BlockMixedCrowd bool         `json:"block_mixed_crowd"`

    ConfirmMS       int `json:"confirm_ms"`
    LeaveFightMS    int `json:"leave_fight_ms"`
    TargetRetryMS   int `json:"target_retry_ms"`
    TargetAttempts  int `json:"target_attempts"`
    TargetBackoffMS int `json:"target_backoff_ms"`
    TargetStallMS   int `json:"target_stall_ms"`
    FightPauseMS    int `json:"fight_pause_ms"`
}
```

| Pole | Zakres | Domyślnie |
|---|---|---|
| `confirm_ms` | 50–1000 | 150 |
| `leave_fight_ms` | 200–5000 | 600 |
| `target_retry_ms` | 300–3000 | 600 |
| `target_attempts` | 1–10 | 3 |
| `target_backoff_ms` | 500–30000 | 2000 |
| `target_stall_ms` | 3000–120000 | 15000 |
| `fight_pause_ms` | 1000–60000 | 10000 |
| `block_mixed_crowd` | — | true |
| reguł czarów | ≤ 8 | — |
| `min_monsters` | 1–64 | 1 |
| `radius` | 0.5–`decision_radius` | 1 |
| `cooldown_ms` | 100–60000 | 2000 |
| `min_mana_pct` | 0–99 | 0 |

`withDefaults()` jak w `CombatConfig`: zero w polu liczbowym znaczy „domyślne",
żeby zapis panelu sprzed tego projektu przeszedł walidację. `block_mixed_crowd`
jest wyjątkiem — `false` to prawidłowa wartość, więc panel wysyła je zawsze, a
domyślne `true` obowiązuje tylko dla braku klucza. Walidacja hurtowa, jak
wszędzie: jedno złe pole odrzuca całość, nic nie jest czyszczone po cichu.
`enabled` bez `attack_key` → odrzucone. Reguły są sprawdzane także wyłączone.

Włączenie ataku wymaga skalibrowanej battle listy (`Combat.Battle` niepuste).
Bez niej krok publikuje `Reason: "brak kalibracji battle listy"` i nic nie
stuka — celowanie bez odczytu ramki włączałoby i wyłączało atak na przemian.

### Snapshot stanu

```go
type FightState struct {
    Enabled         bool   `json:"enabled"`
    Activity        string `json:"activity"`           // travelling, fighting
    EscapeDue       bool   `json:"escape_due"`
    TargetAttempts  int    `json:"target_attempts"`
    BackoffMSLeft   *int   `json:"backoff_ms_left"`
    PauseMSLeft     *int   `json:"pause_ms_left"`
    LastSpell       string `json:"last_spell,omitempty"`
    LastSpellAgeMS  *int   `json:"last_spell_age_ms"`
    Reason          string `json:"reason,omitempty"`
}
```

`State.Fight` obok `State.Heal`. Same skalary; `CombatState` zostaje czystym
widzeniem i nie dostaje ani jednego pola. Wieki (`*MSLeft`, `*AgeMS`) liczone
w `publish()`, reszta zapisywana w chwili zdarzenia — jak `healSnapshot`.

`SetConfig` z `enabled == false` czyści `FightState` i wymusza `Travelling` od
razu, nie na następnej klatce, z tego samego powodu co leczenie: kamera mogła
stanąć, a panel nie może pokazywać „walka" w nieskończoność. Jedno pole
przeżywa czyszczenie: `EscapeDue`. Wyłączenie ataku w trakcie walki ma
odwołać cel, a Escape, którego nikt nie wysłał, zostawiłby klienta w pogoni.

### Panel

`web/fight.js` na wzór `heal.js`, w zakładce **Walka**, pod wskaźnikiem
widzenia:

- checkbox **Atakuj** — nie pamiętany po reloadzie, jak „Lecz automatycznie":
  to przełącznik każący botowi działać, nie ustawienie;
- pole **Klawisz Attack next target**;
- tabela czarów, wiersz: włączona · klawisz · min. potworów · promień ·
  cooldown (ms) · min. mana % · wymaga celu · ▲ ▼ Usuń; **Dodaj regułę**;
  lista pamiętana po reloadzie;
- pola czasów (siedem liczb z tabeli wyżej) i checkbox **Blokuj AoE przy
  mieszanym tłumie**;
- linia stanu: „Walka · próby 2/3 · ostatni czar f3, 1,2 s temu · cooldown
  klawisza f4" albo „W drodze · pauza po zastoju: 7 s" — to jedno zdanie
  użytkownik czyta, żeby wiedzieć, czy atak działa i dlaczego nie;
- kropka na zakładce Walka, gdy „Atakuj" włączone.

Panel zostaje kamerą i widokiem: nic tu nie decyduje, kiedy stuknąć.

## Testy

- `internal/fight` — tabelkowe:
  - `Presence`: dwie klatki i 150 ms; jedna długa klatka nie wystarcza;
    przerwa 500 ms zeruje; fałsz w środku zeruje.
  - `Activity`: tabela przejść na przeplotach; stwór migający na granicy nie
    daje więcej niż jedno wejście i wyjście na cykl; ramka celu trzyma mimo
    `InRange == 0`; lista nieznana blokuje wejście i nie napędza wyjścia;
    `Blocked` blokuje wejście; `Force`.
  - `Targeter`: pierwsze stuknięcie natychmiast; drugie dopiero po 600 ms od
    emisji; zanik ramki na jedną klatkę nie stuka; odmowa nie liczy próby;
    trzecia emisja bez ramki → backoff; ramka zeruje próby; zastój po 15 s
    bez spadku minimum HP; spadek zeruje; nowy cel zeruje.
  - `Engine`: pierwsza wykonalna nie pierwsza pasująca; cooldown per hotkey
    dzielony między regułami; `requires_target`; mana nieczytelna blokuje
    tylko reguły z `MinManaPct > 0`; blokada tłumu tylko dla `MinMonsters >
    1`; `Within` z zaokrągleniem `+0,5`.
- `internal/brain` — harness z `fakeControls` rozszerzonym o `casts` i
  `cancels`: wejście mrozi krok i porzuca pending bez blokady; Escape
  oczekujący po odmowie leci w następnej klatce, a krok czeka; jeden klawisz
  nieleczący na klatkę (leczenie + czar w tej samej klatce = tylko leczenie);
  wyłączenie „Atakuj" w walce → Escape; zastój → pauza → trasa idzie i nie
  wchodzi w walkę; rozbrojenie → `Travelling` bez Escape; `SetConfig` z
  `enabled=false` czyści snapshot; brak kalibracji battle listy → `Reason` i
  zero klawiszy; pozycja nieznana nie przeszkadza walce.
- `internal/input`: `Cast` i `CancelTarget` liczą się w `purposeCombat`, czwarty
  w sekundzie odmówiony, płot nieleczący nadal chroni rezerwę; `escape`,
  `space`, `tab` w `hotkeyNames` i w obu tablicach kodów.
- `frameapi`: `keyConflicts` — każda para kategorii, duplikat w kategorii
  przechodzi, `escape` odrzucony.
- `webtests/fight_test.mjs`: edytor reguł (dodaj, przenieś, usuń, limit 8),
  kształt `config()`, linia stanu w obu aktywnościach i w pauzie, kropka na
  zakładce, „Atakuj" nie zapisuje się.
- **Prawdziwa klatka** — `testdata/combat-capture.png` zgrana przed planem:
  test w `battle` sprawdza, że ramka celu jest rozpoznana na wierszu, który
  człowiek policzył (`CombatFixture.TargetRow`). To pierwszy punkt kontrolny
  planu: dopóki nie przechodzi, celowanie nie ma na czym stać.

## Ryzyka

1. **Ramka celu na żywym kliencie.** Cały mechanizm stoi na tym, że
   `battle.Read` widzi ramkę. Kolor i pokrycie są konfigurowalne, ale
   nigdy nie zmierzone — stąd decyzja 6 i pierwszy punkt kontrolny planu.
   Jeśli ramka nie jest rozpoznawana, celownik wpada w backoff co trzy
   stuknięcia, a panel to pokazuje.
2. **Obieg klawisz → ramka dłuższy niż 600 ms.** Wtedy drugie stuknięcie
   przeskakuje cel. `target_retry_ms` jest polem konfiguracji właśnie po to;
   pomiar na żywo powie, czy domyślne 600 ms starcza.
3. **Stwór goniony poza trasę.** Klient goni uciekającego, bot zostaje w
   walce, a postać ląduje dwie komnaty dalej. To świadomy koszt decyzji 1 i
   poprawki Gemini — alternatywą było zostawianie rannych stworów. Trasa
   przeplanuje się z nowej pozycji; jeśli okaże się to za drogie, faza 5
   dostaje limit odległości od trasy.
4. **Escape zamyka też okna dialogowe klienta.** Nic w naszym przepływie ich
   nie otwiera, a użytkownik uzbraja bota z zamkniętymi oknami — ale to
   zależność od stanu klienta, którą warto znać.
5. **Mieszany tłum jest heurystyką.** Gracz stojący w kadrze na spawnie z
   przewiniętą battle listą nie zostanie wykryty. Bot nie ma lepszego
   narzędzia bez rozpoznawania graczy; decyzja o AoE na PvP jest po stronie
   użytkownika i jego konfiguracji.
6. **Bezpiecznik zastoju na wolnych celach.** Postać, która trafia rzadko
   (niskie obrażenia, cel o dużym HP), może spaść poniżej jednego widocznego
   spadku mini-paska na 15 s. Objaw jest jasny w panelu („pauza po zastoju"),
   a lek to podniesienie `target_stall_ms`.

## Co zostaje na potem

- **Faza 4 — łup:** tracker per stwór, kolejka kandydatów, efemeryczny
  `Follower`, `ClickPoint` ze strefami, `Emitter.ClickButton`, budżet
  kliknięć.
- **Faza 5 — odwrót:** `Retreating` z pierwszeństwem nad `Fighting`, limit
  odległości od trasy, próg liczby stworów, próg HP.
- Czary poza walką (buffy, przywołania) — osobna lista reguł z własnym
  kontekstem, jeśli będzie potrzebna.
