# Walka i zbieranie łupu

Data: 2026-09-07
Status: zaakceptowany do planowania
Gałąź: nowa, odbita od `brain-go`

## Po co

Bot umie dziś dojść tam, gdzie mu każesz, i nic więcej. Chodzi po trasie, uczy
się blokad, wjeżdża i zjeżdża po piętrach — ale mija potwory, jakby ich nie
było, i zostawia za sobą cały łup. To ostatni brakujący element, po którym
`minimap-lab` przestaje być narzędziem do nawigacji i zaczyna być botem
łowieckim.

Zamówienie brzmiało: moduł ataku i zbierania łupu, **inteligentny** w tym
konkretnym sensie, że rzuca czary obszarowe, gdy potworów wokół postaci jest
więcej niż X. To wymaganie jest ostrzejsze, niż brzmi: „wokół postaci" znaczy,
że nie wystarczy wiedzieć **ile** potworów widzi klient — trzeba wiedzieć,
**gdzie** stoją. Battle lista, na której opierał się porzucony bot pythonowy,
odpowiada tylko na pierwsze pytanie. Stąd nowa warstwa widzenia i stąd waga
tego projektu.

## Decyzje podjęte przed projektowaniem

Ustalone z użytkownikiem 2026-09-07. Nie wracamy do nich bez wyraźnej prośby.

1. **Klient oficjalny 13.x/14.x.** Jest Quick Loot i kontenery łupu, więc
   klient sam segreguje itemy. Zbieranie to jedno kliknięcie w zwłoki, a nie
   rozpoznawanie przedmiotów w otwartym oknie.
2. **Pełny cavebot bojowy**, nie „stój i bij".
3. **Gatunków potworów nie rozróżniamy** w tym projekcie, ale warstwa widzenia
   ma zostawić na to miejsce w interfejsie.
4. **Łup z kolejki po zabiciu**: gdy cel znika, pamiętamy kratkę, na której
   stał. Bez rozpoznawania sprite'ów zwłok.
5. **Ruch w walce robi klient.** Włączony „Chase opponent" goni cel za nas. Bot
   w walce nie wciska klawiszy chodzenia; sami implementujemy tylko odwrót.
   To decyzja, która wycięła z zadania najtrudniejszą część — planowanie
   ścieżki w trakcie walki i rozstrzyganie jej konfliktów z trasą.
6. **Globalny sufit klawiszy rośnie z 5 na 8 na sekundę**, z podbudżetami per
   przeznaczenie. Jedyne rozluźnienie istniejącego zabezpieczenia w całym
   projekcie; zgoda wyrażona świadomie.

## Czego ten projekt NIE obejmuje

- **Rozpoznawania gatunków potworów.** Interfejs śladu ma pole na tożsamość,
  ale nikt go nie wypełnia. Priorytety celów i czarna lista to projekt osobny.
- **Rozpoznawania sprite'ów zwłok.** Kolejka łupu wie tylko to, co zapamiętała
  ze zniknięcia śladu.
- **Filtrowania przedmiotów.** Robi to klient przez kontenery łupu.
- **Planowania ścieżki w walce, gonienia i kitingu.** Robi to klient.
- **Reagowania na graczy.** Bot ich nie rozpoznaje i nie unika.
- **Leczenia.** Powstaje tu jedynie czytnik pasków, bo czary potrzebują progu
  many. Same reguły leczenia to projekt następny.
- **Wykrywania śmierci postaci i logowania się z powrotem.**

## Stan wyjściowy

Co już jest i na czym budujemy:

- `POST /api/frame` — binarne ciało z surowym RGBA regionów, odpowiedzią jest
  snapshot stanu. `frame.go` ma zdefiniowane trzy identyfikatory regionów
  (minimapa, HP, mana), z czego używany jest jeden, i `MaxRegions = 8`.
- `internal/brain/loop.go` — jedyny właściciel stanu i decyzji, jedna gorutyna,
  klatki na jednoslotowym kanale, wszystko inne jako polecenie do wykonania w
  kolejce.
- `internal/locate` — dopasowanie minimapy daje **dokładną kratkę X,Y,Z**.
- `internal/brain/follower.go` — z trasy waypointów robi cele i kierunki.
- `internal/brain/executor.go` — jeden krok w locie, potwierdzanie kroków,
  nauka blokad z nieudanych prób.
- `internal/nav` — A\* i sklep blokad.
- `internal/input/driver.go` — uzbrajanie, sprawdzanie focusu, brama świeżości
  (odrzuca obserwacje starsze niż `-stale-ms`), twardy limit 5 stuknięć na
  sekundę, `Walk` i `UseHotkey`.
- Panel `web/` — kamera i widok, bez logiki decyzyjnej.

## Architektura docelowa

### Nowe pakiety

| Pakiet | Odpowiedzialność | Zależy od |
|---|---|---|
| `internal/vision` | z pikseli wycinka okna gry na paski życia nad stworami i na frakcyjne offsety w kratkach | nic poza `image` |
| `internal/battle` | z pikseli battle listy na wiersze i wskazanie wiersza z ramką celu | nic poza `image` |
| `internal/vitals` | z pikseli paska na procent HP albo many | nic poza `image` |
| `internal/combat` | śledzenie stworów w czasie, liczenie w promieniu, silnik reguł czarów | `vision`, `mapdata` |
| `internal/brain/activity.go` | maszyna stanów aktywności | resztą mózgu |
| `internal/brain/loot.go` | kolejka kandydatów na zwłoki | `mapdata` |

Trzy pierwsze pakiety są czystymi funkcjami piksel → liczba, bez stanu i bez
zegara. To one dostaną najwięcej testów tabelkowych i tylko one dotykają
pikseli.

`internal/combat` **nie importuje** `internal/brain`. Przechodniość kratki
dostaje jako predykat `func(mapdata.Position) bool`, nie jako `brain.TileVerdict`
— inaczej powstałby cykl importów, bo mózg importuje `combat`.

### Regiony klatki

Dochodzą dwa identyfikatory, dwa istniejące zaczynają być wypełniane:

| Id | Region | Kto używa |
|---|---|---|
| 1 | minimapa | `locate` (jak dotąd) |
| 2 | pasek HP | `vitals` (nowe) |
| 3 | pasek many | `vitals` (nowe) |
| 4 | **wycinek okna gry** | `vision` (nowe) |
| 5 | **battle lista** | `battle` (nowe) |

Wszystkie jadą **w tej samej klatce** co minimapa. To warunek konieczny:
pozycja w świecie pochodzi z minimapy, a offsety stworów z okna gry, więc
gdyby opisywały dwie różne chwile, kratka potwora byłaby liczona z pozycji,
której postać już nie zajmuje.

Transport zostaje bez zmian — nowe regiony to nowe stałe w `knownRegion`, nie
nowa wersja formatu. `MaxRegions = 8` już to przewidywało.

**Brak regionu w klatce nie jest awarią.** Klatka bez regionu 4 wyłącza walkę na
tę klatkę, bez regionu 5 — wybór celu, bez 2 i 3 — reguły z progiem many. Pętla
dalej lokalizuje postać i idzie po trasie. Dzięki temu kalibracja jest
przyrostowa: dokładasz jeden prostokąt i sprawdzasz go, nie tracąc reszty
działającego bota.

### Wycinek zamiast całego okna

Okno gry to największy region w systemie. Rachunek, bez upiększania:

| Na kratkę | Pełne okno 15×11 | Wycinek 11×11 | Oszczędność |
|---|---|---|---|
| 32 px | 660 kB/klatkę | 484 kB/klatkę | 27% |
| 64 px | 2,6 MB/klatkę | 1,9 MB/klatkę | 27% |

Panel wycina **okno o promieniu decyzji**: kratka postaci plus `decision_radius + 1`
kratek w każdą stronę, domyślnie 5, czyli 11×11 kratek. Kamera klienta jest
wyśrodkowana na postaci, więc ten prostokąt jest **stały względem okna gry** i
nie musi jechać w klatce.

Trzeba powiedzieć wyraźnie, ile to daje: **około jednej czwartej, nie
wielokrotność**. Okno gry ma tylko 11 kratek wysokości, więc wycinek o promieniu
4 oszczędza wyłącznie na szerokości. Prawdziwe powody wycinka są dwa inne:
ogranicza pracę detektora i **twardo ogranicza, jak daleko może stać stwór, żeby
się policzył** — pasek z przeciwnego końca ekranu nie ma jak wpłynąć na decyzję
o czarze obszarowym.

Pasma to jednak nie ścina i przy dużym oknie gry może zaboleć. Dwa wnioski
praktyczne: `frame.MaxBody` (dziś 4 MB) trzeba przeliczyć przy kalibracji i
prawdopodobnie podnieść, a użytkownik ma darmowy sposób na kilkukrotne cięcie
kosztu — **zmniejszyć okno gry w kliencie**. Kratek dalej jest 15×11, tylko
mniejszych.

Rozważano policzenie pasków w JavaScripcie na canvasie, który panel i tak
trzyma. Ścięłoby to pasmo do zera, ale przeniosłoby logikę widzenia z powrotem
do przeglądarki, wbrew decyzji z migracji mózgu. **Piksele zostają w Go.**

Żeby obie strony nie wyliczały wycinka osobno i nie rozjechały się o piksel,
prostokąt wycinka jest **jawnym polem konfiguracji**, walidowanym w Go:
`viewport_crop` musi mieścić się w `viewport_rect` i musi zawierać kratkę
środkową. Panel ma przycisk „wylicz wycinek z promienia", ale wartość mieszka w
konfiguracji, nie we wzorze powtórzonym w dwóch językach.

### Detektor pasków życia

Klient rysuje nad każdym stworem pasek: czarna obwódka, wewnątrz wypełnienie o
barwie skwantowanej do kilku wartości zależnie od procentu życia.

Szukamy **wypełnienia, nie obwódki**: poziomych ciągów jednej ze znanych barw,
wysokich na `Height - 2·Border` pikseli, z czernią bezpośrednio nad ciągiem i
po jego lewej. Lewa krawędź obwódki to lewa krawędź wypełnienia minus grubość
obwódki. Szerokość wypełnienia podzielona przez szerokość wnętrza daje darmowo
procent życia stwora — przyda się, by nie porzucać dobijanego celu.

Geometria `27×4` jest **hipotezą kalibracyjną, nie stałą w kodzie**. Skalowanie
okna gry i skalowanie systemowe potrafią ją zmienić, więc wymiary i grubość
obwódki są polami konfiguracji, a barwy wypełnienia — listą z tolerancją na
kanał.

Pasek w 0% wypełnienia jest jednolicie czarny i nie do odróżnienia od tła. Taki
stwór zostanie pominięty na klatkę lub dwie. Zniknie i tak w następnej
sekundzie, a ślad w `combat.Tracker` przetrzyma tę dziurę.

### Z piksela na kratkę

Kalibrujesz **jeden prostokąt** — okno gry. Reszta wynika:

```
TileW = viewport_rect.W / Cols     // Cols = 15
TileH = viewport_rect.H / Rows     // Rows = 11
kratka postaci = (Cols/2, Rows/2)  // dzielenie całkowite: (7, 5)
```

Kamera jest wyśrodkowana na postaci, więc offset paska w kratkach to wprost
`(Δkolumna, Δwiersz)` od kratki środkowej, a **kratkę w świecie** dostajemy z
minimapy: `świat = pozycja_postaci + offset`.

Offsety liczymy **frakcyjnie**, bez zaokrąglania. Stwory przesuwają się płynnie
między kratkami; zaokrąglanie na każdej klatce dawałoby migotanie licznika na
granicy promienia. Promień to **odległość Chebysheva** `max(|dx|, |dy|)`, bo tak
działają obszary czarów w tej grze, a test brzmi `≤ R + 0.5`.

Zakotwiczenie paska względem kratki logicznej (`AnchorDX`, `AnchorDY`) jest
polem konfiguracji, bo dla dużych stworów sprite wystaje poza kratkę i pasek
może być rysowany wyżej. **Wartość mierzymy w fazie 1**, podglądem w panelu, na
małym i na dużym stworze — nie zgadujemy jej w kodzie.

Siatka 15×11 jest założeniem o kliencie. Panel pozwala ją nadpisać, a podgląd
pokazuje rozjazd natychmiast: obrysy pasków przestają siedzieć na stworach.

### Cztery rzeczy, które psują widzenie

1. **Własny pasek.** Kamera jest wyśrodkowana, więc pasek postaci jest **zawsze
   w tych samych pikselach**. Wykluczamy go po dokładnej pozycji z kalibracji
   (`self_bar`, tolerancja 2 piksele), a nie po bliskości do środka. Ta różnica
   ma znaczenie: wykluczanie po bliskości gubiłoby potwora stojącego kratkę nad
   postacią. Gdy użytkownik wyłączy w kliencie własny pasek, pole zostaje puste
   i nic się nie wyklucza.
2. **Gracze i NPC wyglądają identycznie.** W tym projekcie ich nie rozróżnimy.
   Battle lista z filtrami klienta („ukryj graczy", „ukryj NPC") daje **sufit**
   na liczbę potworów na ekranie. Gdy pasków jest więcej niż wierszy, odczyt
   dostaje flagę `mixed_crowd` — widoczną w panelu, z przełącznikiem „nie
   rzucaj obszarowych przy mieszanym tłumie".

   Ten test jest **jednostronny i tym samym zachowawczy**, bo paski liczymy w
   wycinku, a wiersze dotyczą całego ekranu. Wnioskowanie zostaje poprawne:
   `wiersze_ekran ≥ potwory_ekran ≥ potwory_wycinek`, więc
   `paski_wycinek > wiersze_ekran` dowodzi, że w wycinku jest coś, co nie jest
   potworem. Odwrotnie nie wnioskujemy nic — brak flagi **nie** znaczy, że tłum
   jest czysty.
3. **Inne piętra.** Pozycja na ekranie nie mówi nic o `Z`, a klient rysuje
   stwory z innych pięter przez dziury i na zboczach. Mamy jednak tanie sito:
   **dane mapy**. Pasek zmapowany na kratkę, którą mapa uważa za nieprzechodnią,
   jest odrzucany. Sito nie jest pełne — potwór na przechodniej kratce piętro
   wyżej przejdzie przez nie — i to jest świadomie przyjęte ograniczenie,
   wypisane w panelu jako liczba odrzuconych pasków.
4. **Zasłanianie i duże stwory.** Nakładające się paski potrafią się zlać albo
   całkowicie zakryć; żaden detektor tego nie odzyska. Dlatego nad detektorem
   siedzi śledzenie, a licznik raportuje **stwory pewne**, nie „wszystko, co
   widać".

### Walka nie potrzebuje pozycji w świecie

Warto to wyodrębnić, bo wychodzi z architektury i jest wygodne: **licznik
stworów w promieniu działa w układzie ekranu**, a ekran jest wyśrodkowany na
postaci. Gdy dopasowanie minimapy zawiedzie — a zawodzi, po ciemku i w wodzie —
bot dalej wie, ile potworów go otacza, i dalej rzuca obszarowe.

Pozycji w świecie wymagają tylko dwie rzeczy: sito danych mapy (bo pyta o
konkretną kratkę) i kolejka łupu (bo zapamiętuje kratkę na później). Przy
nieznanej pozycji sito się nie stosuje, a kandydaci na zwłoki nie powstają.

### Śledzenie stworów

`combat.Tracker` przypisuje śladom identyfikatory, kojarząc paski między
klatkami po najbliższym sąsiedzie z ciągłością życia (skok życia w górę
oznacza inny stwór, nie ten sam). Ślad żyje jeszcze `GhostMS` (domyślnie
400 ms) po tym, jak przestał być widziany — to on, nie detektor, odpowiada na
pytanie „gdzie stał ten, który przepadł".

Ślad staje się **pewny** po `ConfirmMS` (domyślnie 150 ms) widoczności. Tylko
pewne ślady wchodzą do licznika w promieniu, więc jednoklatkowy artefakt nie
rzuci czaru za 200 many.

**Zniknięcie to nie śmierć.** Tak samo wygląda ucieczka za krawędź ekranu,
zasłonięcie i zmiana piętra. Ślad, który wygasł, oddaje więc do kolejki łupu
**kandydata**, nie zwłoki.

### Czytnik battle listy

Dwie odpowiedzi:

- **liczba wierszy** — po powtarzalnych mini-paskach życia w stałej kolumnie
  listy, wykrywanych tym samym mechanizmem co paski w oknie gry, tylko o innej
  geometrii;
- **który wiersz ma ramkę celu** — po barwnej obwódce wokół wiersza.

To drugie jest krytyczne dla bezpieczeństwa: **klik w już atakowanego stwora
zdejmuje atak**. Bez odczytu ramki bot włączałby i wyłączał atak na przemian,
raz na klatkę, i nigdy nic nie zabił.

Przewinięta lista obcina liczbę wierszy. Czytnik zgłasza `Truncated`, gdy
wiersze dochodzą do dolnej krawędzi wycinka, a sufit na potwory przestaje wtedy
obowiązywać.

### Maszyna stanów aktywności

```
                  ┌──────────────┐
        ┌────────►│  Travelling  │◄────────┐
        │         └──────┬───────┘         │
        │                │ pewny potwór    │ kolejka pusta
        │                │ w promieniu     │ albo limity
        │                ▼                 │
        │         ┌──────────────┐         │
        │         │   Fighting   │─────────┼──► Escape na wyjściu
        │         └──────┬───────┘         │
        │  potwór wrócił │ brak potworów   │
        │         ┌──────▼───────┐         │
        └─────────┤   Looting    ├─────────┘
                  └──────────────┘
```

`Retreating` dochodzi w fazie 5 i ma pierwszeństwo nad `Fighting`.

Przejścia mają **histerezę**: wejście w `Fighting` wymaga pewnego potwora,
wyjście — braku potworów przez `LeaveFightMS` (domyślnie 600 ms). Oba
konsultowane modele nazwały „state flapping" pierwszym trybem awarii tej
architektury i mają rację: bez opóźnienia wyjścia bot migałby między trasą a
walką na każdej klatce, przepalając budżet klawiszy na kroki w przeciwnych
kierunkach.

**Wyjście z `Fighting` wysyła Escape.** Bez tego klient, który goni za nas,
ciągnie postać za uciekającym potworem w następną komnatę, a trasa zostaje z
tyłu. To nie kosmetyka, to warunek, żeby delegowanie gonienia klientowi w
ogóle działało.

W stanie `Fighting` bot **nie emituje żadnych klawiszy chodzenia**. Minimapa i
tak mówi, gdzie postać wylądowała, a `Follower` przeplanuje trasę po walce —
już to potrafi.

### Jeden Follower na raz

Gemini i Codex zgodnie proponowały wyodrębnić współdzielony `Navigator` z A\*,
pamięcią blokad i jednym krokiem w locie. Ten obiekt już istnieje i nazywa się
`Follower` + `Executor`.

Zamiast go przepisywać: **w danej chwili aktywny jest dokładnie jeden
`Follower`** — ten od trasy w `Travelling`, albo efemeryczny nad jednopunktową
trasą w `Looting` i `Retreating`. `Executor` zostaje jeden, więc nauka blokad
jest współdzielona sama z siebie, a przetestowany kod chodzenia nie jest
ruszany. Właściwość, o którą chodziło obu modelom — jedna nawigacja, jedna
pamięć blokad, cele zgłaszane przez zachowania — jest zachowana; koszt jest
znacznie mniejszy.

Efemeryczny `Follower` powstaje przez `NewFollower([]route.Waypoint{cel}, opts)`
i po zmianie stanu jest porzucany. Postęp trasy żyje w osobnym `Follower`, więc
nie ginie.

**Przy zmianie stanu czekamy, aż krok w locie się rozstrzygnie.** Wysłanego
klawisza nie da się odwołać, a podmiana właściciela kroku pod spodem to
dokładnie „pending-step misattribution", które nazwał Codex: nowa aktywność
potwierdzałaby albo unieważniała krok, którego nie zamawiała, i zatruwałaby
pamięć blokad. Czekanie jest ograniczone istniejącym limitem czasu kroku.

### Reguły czarów

Uporządkowana lista, wygrywa **pierwsza pasująca od góry**, jeden czar na
klatkę:

```go
type SpellRule struct {
    Enabled        bool    `json:"enabled"`
    Hotkey         string  `json:"hotkey"`
    MinMonsters    int     `json:"min_monsters"`
    Radius         float64 `json:"radius"`
    CooldownMS     int     `json:"cooldown_ms"`
    MinManaPct     float64 `json:"min_mana_pct"`
    RequiresTarget bool    `json:"requires_target"`
}
```

`decision_radius` (domyślnie 4) jest górną granicą: wyznacza wycinek, jest
promieniem, w którym snapshot podaje `monsters_in_range`, i walidacja odrzuca
regułę o `Radius` większym od niego. Bez tego reguła mogłaby pytać o kratki,
których panel nawet nie przysłał.

Reguła pasuje, gdy jest włączona, liczba **pewnych** stworów w promieniu
`Radius` sięga `MinMonsters`, mana nie jest niżej niż `MinManaPct`, minął
`CooldownMS` od ostatniego rzucenia tej reguły i — jeśli `RequiresTarget` —
jakiś wiersz battle listy ma ramkę.

To ta sama konstrukcja, którą użytkownik zamówił dla leczenia. W grze wszystko
jest hotkeyem, więc nie ma powodu na dwa różne silniki. Wymaganie „obszarowy
przy N+ potworach" to wprost reguła `{min_monsters: 4, radius: 1}`; czar na
pojedynczy cel to `{min_monsters: 1, requires_target: true}` niżej na liście.

### Wybór celu

W `Fighting`, gdy żaden wiersz nie ma ramki, bot klika **pierwszy wiersz battle
listy** — najbliższy stwór. Klikamy listę, nie stwora w oknie gry: chybione
kliknięcie w viewport przesuwa postać, kliknięcie w listę nigdy nie ma skutków
ubocznych.

Gdy po `TargetAttempts` (domyślnie 3) próbach ramka się nie pojawia, bot
przestaje próbować na `TargetBackoffMS` (domyślnie 2000 ms), zamiast tłuc w to
samo miejsce.

### Kolejka łupu

```go
type Candidate struct {
    Tile     mapdata.Position
    At       time.Time
    Attempts int
}
```

Kandydat powstaje z wygasłego śladu, na jego ostatniej pewnej kratce. W
`Looting` bot bierze najbliższego i stawia cel nawigacji: **kratka kandydata,
jeśli mapa uważa ją za przechodnią** (zwłoki są przechodnie, więc zwykle tak
jest), a w przeciwnym razie najbliższa przechodnia kratka z nią sąsiadująca.
Quick Loot działa z odległości jednej kratki, więc oba warianty wystarczają. Po
dojściu bot klika modyfikatorem.

**Pozycję ekranową kliknięcia liczymy dopiero w chwili kliknięcia**, z
aktualnej pozycji postaci — między zaplanowaniem a dojściem świat się przesunął,
a klik w stary piksel trafiłby w inną kratkę. Wskazał to Codex i to jest
prawdziwy błąd, nie hipotetyczny.

Ograniczniki, wszystkie konfigurowalne: 2 próby na kandydata, 4 s na kandydata,
15 s na całą fazę, maks. 8 kratek od miejsca walki, maks. 10 w kolejce.
Kandydat, którego nie da się osiągnąć — bo zwłoki leżą za zamkniętymi drzwiami
albo na polu ognia — wygasa cicho. Bez tych limitów „ghost corpse deadlock"
(nazwa Gemini) zatrzymuje bota na zawsze przed nieosiągalną kratką.

Pojawienie się pewnego potwora w trakcie lootu wraca do `Fighting` **z
zachowaną kolejką**.

### Sterownik

Trzy dokładki, wszystkie za istniejącą bramką `guardLocked` (uzbrojenie,
świeżość obserwacji, focus okna, budżet):

```go
func (d *Driver) ClickPoint(nx, ny float64, button string, mods []string, observationAge time.Duration) Result
func (d *Driver) CastSpell(key string, observationAge time.Duration) Result
func (d *Driver) CancelTarget(observationAge time.Duration) Result
func (d *Driver) SetClickZones(zones []Zone) error

// Zone to dozwolony prostokąt kliknięć w znormalizowanych współrzędnych
// ekranu, tych samych, którymi posługuje się Calibrate i Emitter.Click.
type Zone struct {
    Name           string
    X0, Y0, X1, Y1 float64
}
```

- `ClickPoint` dostaje **regułę własną: kliknięcie musi trafić w jedną ze
  skalibrowanych strefy** (battle lista albo wycinek okna gry). Klik poza nimi
  jest odrzucany z podaniem przyczyny. Kliknięcie jest znacznie groźniejsze od
  stuknięcia klawisza — potrafi przenieść przedmiot albo zaatakować gracza —
  więc dostaje ograniczenie, którego klawisze nie mają.
- `CastSpell` to samo stuknięcie, bez semantyki „akcji w locie", którą ma
  `UseHotkey`; ta jest sprzężona ze zmianą piętra i czeka na potwierdzenie.
- `CancelTarget` stuka Escape. Nazwa `escape` dochodzi do `hotkeyNames` i do
  tablic kodów w `input_darwin.go` oraz `input_windows.go`.

`Emitter` rośnie o `ClickButton(nx, ny float64, button string, mods []string) error`
— do zaimplementowania na oba systemy. Istniejący `Click` staje się jego
wywołaniem z lewym przyciskiem i bez modyfikatorów, żeby akcje pięter nie
zmieniły zachowania.

Interfejs `Controls` w mózgu rośnie o te same trzy metody.

### Budżet klawiszy

Dziś jest twarde `maxTapsPerSecond = 5` na wszystko. Nowy układ — okna
przesuwne per przeznaczenie plus twardy sufit globalny:

| Przeznaczenie | Limit |
|---|---|
| globalnie | 8 stuknięć/s |
| chodzenie | 3 stuknięcia/s |
| czary | 3 stuknięcia/s |
| leczenie (rezerwa, użyta w projekcie następnym) | 2 stuknięcia/s |
| kliknięcia | 2/s, liczone osobno |

Suma podbudżetów przekracza sufit **celowo**: sufit jest twardą granicą, a
podbudżety mają nie dopuścić, żeby jedno przeznaczenie zjadło całość. Rezerwa
leczenia nie może być odzyskana priorytetem — pięciu wydanych stuknięć nikt nie
cofnie — więc musi być wygrodzona z góry, nie wywłaszczana.

### Konfiguracja

`brain.Config` rośnie; walidacja hurtowa, jak dziś — jedno złe pole nie może
cicho wyczyścić innego:

```
viewport_rect, viewport_crop, grid_cols, grid_rows, anchor_dx, anchor_dy
bar_width, bar_height, bar_border, bar_colors[], bar_tolerance, self_bar
battle_rect, battle_row_pitch, battle_frame_color
hp_rect, mana_rect
attack, loot, retreat                    // trzy przełączniki
decision_radius, aoe_block_mixed_crowd
spells[]                                 // lista reguł
loot_modifier, loot_button, loot_max_tiles, loot_candidate_ms,
  loot_phase_ms, loot_max_attempts, loot_queue_max
confirm_ms, ghost_ms, leave_fight_ms, target_attempts, target_backoff_ms
```

### Snapshot stanu

`State` rośnie o same skalary, bo komentarz przy nim słusznie zabrania wożenia
w nim historii:

```go
type CombatState struct {
    Activity        string  `json:"activity"`
    BarsTotal       int     `json:"bars_total"`
    MonstersInRange int     `json:"monsters_in_range"`
    RejectedByMap   int     `json:"rejected_by_map"`
    MixedCrowd      bool    `json:"mixed_crowd"`
    BattleRows      int     `json:"battle_rows"`
    TargetRow       *int    `json:"target_row"`
    LastSpell       string  `json:"last_spell,omitempty"`
    ManaPct         float64 `json:"mana_pct"`
    HPPct           float64 `json:"hp_pct"`
}

type LootState struct {
    QueueLen int               `json:"queue_len"`
    Current  *mapdata.Position `json:"current"`
    Attempts int               `json:"attempts"`
}
```

Offsety poszczególnych stworów **nie jadą w snapshocie na każdej klatce** —
tylko w odpowiedzi na osobne żądanie podglądu, tak jak dziś działa podgląd
przechodniości.

### Panel

Zostaje kamerą i widokiem:

- kalibracja prostokątów: okno gry, wycinek, własny pasek, battle lista, HP,
  mana;
- podgląd wyciętego okna z obrysowanymi paskami i ich offsetami w kratkach —
  to nim mierzymy `anchor_dx`/`anchor_dy` i nim widać rozjazd siatki;
- trzy przełączniki: Atakuj, Zbieraj, Odwrót;
- tabelka reguł czarów;
- wskaźnik stanu: aktywność, liczba potworów w promieniu, flaga mieszanego
  tłumu, wiersz celu, długość kolejki łupu.

## Fazy

Każda faza kończy się czymś, co działa i da się sprawdzić.

### Faza 1 — widzenie, bez ani jednego klawisza

Nowe identyfikatory regionów, panel je wycina i wysyła. `internal/vision`,
`internal/battle`, `internal/vitals` z testami tabelkowymi na syntetycznych
obrazkach. Kalibracja i podgląd w panelu. Zgranie kilku **prawdziwych klatek z
gry do `testdata/`**.

*Akceptacja:* panel pokazuje liczbę potworów, ich offsety i procent HP oraz
many, a obrysy siedzą na stworach. Zmierzone `anchor_dx`/`anchor_dy` dla małego
i dużego stwora. Sterownik nie wysyła nic.

### Faza 2 — maszyna stanów, `Fighting` jeszcze bezczynny

`activity.go`, przełączanie aktywnego `Followera`, histereza, czekanie na krok
w locie przy zmianie stanu, Escape na wyjściu z `Fighting`. Stan `Fighting`
tylko zamraża trasę.

*Akceptacja:* wszystkie istniejące testy trasy zielone. Wejście potwora w
promień zamraża trasę, jego zniknięcie ją wznawia po histerezie. Tabelka
przejść nie pokazuje migania.

### Faza 3 — walka

Silnik reguł czarów, wybór celu przez klik w battle listę, `CastSpell`,
`ClickPoint` ze strefami, podbudżety klawiszy, `Emitter.ClickButton` na oba
systemy.

*Akceptacja:* w trybie `dry` zadana sekwencja klatek daje oczekiwane klawisze i
kliknięcia. Na żywym kliencie bot atakuje najbliższego stwora i rzuca czar
obszarowy po przekroczeniu progu.

### Faza 4 — łup

Kolejka kandydatów, efemeryczny `Follower` do zwłok, klik modyfikatorem z
pozycją liczoną w chwili kliknięcia, wszystkie limity.

*Akceptacja:* po walce bot podchodzi i klika zwłoki, wraca na trasę, a zwłoki
nieosiągalne wygasają bez zablokowania bota.

### Faza 5 — odwrót

`Retreating` z pierwszeństwem nad `Fighting`: Escape plus cel nawigacji z dala
od pewnych stworów. Wyzwalacze: zbyt wielu potworów w promieniu i — bo czytnik
już jest — zbyt niskie HP.

*Akceptacja:* przy przekroczeniu progu bot zdejmuje cel i odchodzi, a po
uspokojeniu wraca na trasę.

## Testy

Tabelkowe, ze wstrzykniętym zegarem, jak w całym projekcie.

- `internal/vision` — syntetyczne wycinki z paskami w znanych miejscach;
  przypadki: pasek na krawędzi wycinka, dwa nachodzące, 0% wypełnienia,
  przeskalowana geometria, barwa poza tolerancją, własny pasek wykluczony,
  potwór kratkę nad postacią **nie** wykluczony.
- `internal/battle` — liczba wierszy, wiersz z ramką, lista pusta, lista
  przewinięta.
- `internal/vitals` — paski na znanych procentach; **wymóg ciągłego prefiksu**
  wypełnionych kolumn jako ochrona przed fałszywym 0% po rozjechaniu
  kalibracji (ustalone przy projekcie leczenia).
- `internal/combat` — kojarzenie śladów między klatkami, ciągłość życia,
  wygaszanie po `GhostMS`, potwierdzanie po `ConfirmMS`, licznik w promieniu na
  granicy `R + 0.5`, sito danych mapy.
- reguły czarów — tabelka (liczba stworów, mana, cooldown, obecność celu) →
  oczekiwany hotkey albo brak.
- maszyna stanów — sekwencje obserwacji → oczekiwane przejścia, z przypadkami
  migania i z krokiem w locie w chwili zmiany stanu.
- kolejka łupu — cykl życia, każdy z limitów osobno, powrót do walki w trakcie
  lootu z zachowaniem kolejki.
- sterownik — klik odrzucony poza strefami, przy nieświeżej obserwacji, po
  utracie focusu; każdy podbudżet osobno i sufit globalny.
- prawdziwe klatki z `testdata/` — próg regresji na detektorze pasków i
  czytniku battle listy.

## Ryzyka

1. **Zakotwiczenie paska dużego stwora.** Jeśli offset zależy od wyglądu, a nie
   od kratki, jedna stała nie wystarczy. **Jeszcze niezmierzone** — brakuje
   `testdata/combat-capture.png` (zob.
   `docs/superpowers/plans/2026-09-07-vision-layer-measurements.md`). Procedura
   pomiaru: uruchom `go test ./internal/vision/ -run TestRealCaptureOffsets -v`,
   znajdź w logu wpis stwora ze sprite'em wyraźnie większym niż jedna kratka i
   porównaj wypisany offset z kratką, na której ten stwór faktycznie stoi na
   obrazie — pomaga `.debug/vision-fixture.png`, rysunek diagnostyczny z
   obrysami pasków, który ten sam test zapisuje. Dwa możliwe wyniki i ich
   konsekwencja dla fazy 3: rozjazd poniżej pół kratki — jedna stała
   `AnchorDX`/`AnchorDY` wystarcza, tak jak dla małego stwora i dla własnego
   paska; rozjazd pół kratki lub większy — faza 3 nie może traktować kratki
   dużego stwora jako pewnej, a kandydat na zwłoki po takim stworze dostaje
   sąsiedztwo kratek zamiast jednego punktu.
2. **Sito pięter nie jest pełne.** Potwór na przechodniej kratce piętro wyżej
   policzy się jako nasz. Skutek: czar obszarowy rzucony bez powodu. Panel
   pokazuje liczbę odrzuconych, więc rozjazd da się zauważyć.
3. **Klient skaluje okno.** Zmiana rozmiaru okna gry unieważnia całą
   kalibrację. Podgląd pokazuje to natychmiast, ale nic nie wykryje tego
   automatycznie.
4. **Podniesiony sufit klawiszy.** Osiem stuknięć na sekundę zamiast pięciu to
   szersze okno dla zapętlonego błędu. Podbudżety ograniczają szkodę do jednego
   przeznaczenia.
5. **Powiązanie Quick Loota.** Modyfikator i przycisk są konfigurowalne, bo
   domyślne powiązanie w kliencie 13/14 nie jest w tym projekcie potwierdzone
   empirycznie. Faza 4 zaczyna się od ustalenia go na żywym kliencie.
6. **Przewinięta battle lista** znosi sufit na liczbę potworów, więc flaga
   mieszanego tłumu przestaje działać. Zgłaszane jako `Truncated`.
7. **Klient goni tam, gdzie my nie chcemy.** Escape na wyjściu z walki jest
   jedyną barierą; jeśli okaże się niewystarczająca, faza 5 dostaje dodatkowy
   cel nawigacji „wróć na trasę".

## Projekt następny

Moduł leczenia, zamówiony 2026-09-06 i odłożony dwa razy. Po tym projekcie
zostaje z niego sama lista reguł: czytnik pasków, budżet klawiszy z rezerwą i
wywłaszczanie kroku będą już gotowe.
