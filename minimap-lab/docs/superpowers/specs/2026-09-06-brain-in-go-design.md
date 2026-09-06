# Mózg bota w Go

Data: 2026-09-06
Status: zaakceptowany do planowania
Gałąź: nowa, odbita od `go`

## Po co

Dziś ciężka obróbka obrazu i szukanie ścieżek są w Go, ale **cała logika
decyzyjna bota siedzi w przeglądarce**: co jest następnym krokiem trasy, kiedy
krok się nie udał, czy nieudany krok jest dowodem o mapie, kiedy przeplanować.
To 520 linii stanowej logiki w `web/`, pokrytych 2678 liniami testów w
`webtests/`.

Ten podział ma trzy konkretne koszty:

1. **Protokół intencji istnieje wyłącznie po to, żeby go obsłużyć.** Token
   sesji, numery sekwencyjne przeciw powtórkom, heartbeat co 200 ms, brama
   świeżości przekazywana przez pole w JSON-ie — cała ta maszyneria jest
   potrzebna tylko dlatego, że decydent i emiter klawiszy są w różnych
   procesach. Gdy są w jednym, wysłanie kroku to wywołanie funkcji.
2. **Karta w tle jest dławiona.** Panel jest w tle zawsze, gdy grasz.
   Przeglądarki dławią `setTimeout` w niewidocznych kartach do ~1 Hz, a
   `requestAnimationFrame` zatrzymują zupełnie. Mózg w karcie przeglądarki
   działa więc dokładnie wtedy, gdy nie patrzysz — czyli nigdy.
3. **Logika jest rozdarta na pół.** Blokady uczy się JS, przechowuje Go.
   Ścieżkę liczy Go, decyduje o niej JS. Każda zmiana dotyka obu stron.

Po migracji Go jest właścicielem stanu i decyzji, a przeglądarka robi to
jedno, czego Go zrobić nie może: przechwytuje obraz ekranu.

## Czego ten projekt NIE obejmuje

- **Zrzutu ekranu w Go.** `getDisplayMedia` zostaje w przeglądarce. Zrzut w Go
  na macOS 26 to albo ScreenCaptureKit (API Objective-C z delegatami, wołane
  przez `objc_msgSend` z purego), albo `CGWindowListCreateImage` — API C, wciąż
  działające, ale oznaczone jako przestarzałe od macOS 14. Osobny projekt,
  osobne ryzyko.
- **Modułu leczenia.** To projekt następny, budowany na tym fundamencie.
  Format klatki jest tu zaprojektowany tak, żeby leczenie dołożyło tylko nowe
  identyfikatory regionów, bez zmiany protokołu.
- **Zmian w dopasowaniu minimapy, A*, siatce kosztów i magazynie blokad.**
  `internal/locate`, `internal/nav` i `internal/mapdata` zostają bez zmian w
  rdzeniu. Zmienia się tylko to, kto je woła.

## Stan wyjściowy

| gdzie | co |
|---|---|
| `internal/locate` | dopasowanie minimapy szablonem, globalne i lokalne |
| `internal/mapdata` | atlas map, siatka kosztów |
| `internal/nav` | A*, magazyn nauczonych blokad |
| `internal/input` | emiter klawiszy z bramkami, uzbrajany wykonawca |
| `web/tracker.js` (41 l.) | kotwica pozycji, promień lokalnego szukania, kadencja |
| `web/follower.js` (183 l.) | wybór kroku po trasie, replan, backoff |
| `web/executor.js` (334 l.) | lock-step, retry, uczenie blokad, spóźnione dojście |
| `web/recorder.js` (57 l.) | nagrywanie waypointów |
| `web/route.js` (47 l.) | format pliku trasy |
| `web/input.js` (146 l.) | klient protokołu intencji |
| `web/blocks.js` (91 l.) | klient magazynu blokad |
| `web/app.js` (826 l.) | DOM, canvas oraz sklejka `updateRoute`/`followStep`/`pumpBlocks` |

## Architektura docelowa

### Podział odpowiedzialności

**Go** — pętla bota jako goroutine: śledzenie pozycji, nagrywanie tras,
podążanie, lock-step wykonawca, nauka blokad, planowanie ścieżek, emisja
klawiszy. Jeden właściciel stanu.

**Przeglądarka** — kamera i widok: `getDisplayMedia`, wycięcie skonfigurowanych
prostokątów, wysłanie pikseli, narysowanie odesłanego stanu. Konfiguracja
(prostokąt minimapy, znacznik, zoom, hotkeye, trasa) jest formularzem, którego
wartości jadą do Go. **Zero decyzji.**

### Co znika

Cały protokół intencji: token sesji przy każdej intencji, numery sekwencyjne
przeciw powtórkom, heartbeat co 200 ms, `observation_age_ms` w ciele żądania,
`POST /api/input`, `POST /api/input/done`, `GET /api/input/status`.

### Co zostaje, w innej formie

| gwarancja | dziś | po migracji |
|---|---|---|
| świadome uzbrojenie | `POST /api/arm` + token | bez zmian; token wiąże uzbrojenie z sesją przechwytywania |
| kontrola focusu okna | tuż przed emisją | bez zmian |
| limit klawiszy | 5/s w oknie przesuwnym | bez zmian |
| brama świeżości | `-stale-ms` (domyślnie 400), wiek z pola JSON | `-stale-ms` bez zmian, ale wiek liczony **od ostatniej dobrej obserwacji pozycji**, sprawdzany tuż przed emisją |
| martwy panel rozbraja | heartbeat 200 ms | watchdog: brak klatek dłużej niż 750 ms rozbraja — ta sama wartość, co dzisiejszy limit heartbeatu; działa też przy zupełnym braku żądań |
| odrzucanie starych obserwacji | numer sekwencyjny intencji | numer klatki w sesji przechwytywania |

**Brama świeżości ma jedno źródło: obserwację pozycji.** Świeża klatka paska HP
(projekt następny) nie odmładza pozycji z minimapy. To jest ta sama zasada, co
dziś, wypowiedziana wprost, bo po migracji istnieje więcej niż jedno źródło
obserwacji.

### Transport klatek

Jeden endpoint: `POST /api/frame`. Ciało binarne, odpowiedzią jest pełny
snapshot stanu bota w JSON-ie. Żadnego SSE, żadnego WebSocketu, żadnej nowej
zależności — projekt utrzymuje dokładnie jedną (`purego`). Strumień klatek jest
zarazem heartbeatem.

Rozmiar ruchu przy docelowej kadencji: minimapa ~106×109 RGBA co 100 ms to
około 460 kB/s, dwa paski ~120×12 co 50 ms to około 230 kB/s. Po loopbacku bez
znaczenia.

#### Format ciała

```
offset  rozmiar  pole
0       4        magic "MLF1"
4       1        wersja formatu (1)
5       1        liczba regionów N
6       2        flagi (zarezerwowane, muszą być zerowe)
8       8        identyfikator sesji przechwytywania (uint64)
16      8        numer klatki w sesji (uint64, rosnący)
24      8        czas wideo w mikrosekundach (uint64, z video.currentTime)
32      4        wiek w chwili wysłania, ms (uint32) — od wycięcia pikseli do wysłania
36      N×12     nagłówki regionów
```

Nagłówek regionu: `id` (uint8), `format` (uint8, 0 = RGBA8888), 2 bajty
wyrównania, `w` (uint16), `h` (uint16), `len` (uint32). Po nagłówkach idą
ładunki w tej samej kolejności, `len` = `w`×`h`×4.

Identyfikatory regionów: `1` minimapa, `2` pasek HP, `3` pasek many. Dwa
ostatnie należą do projektu następnego i dziś nigdy nie przychodzą.

Ciało ograniczone `http.MaxBytesReader` do 4 MB. Bufory z `sync.Pool`,
odczyt przez `io.ReadFull`, ciało **zawsze** doczytane do końca — inaczej Go
zrywa keep-alive i po chwili brakuje portów efemerycznych.

#### Reguły pętli

- **Jeden POST w locie.** Przeglądarka nie wysyła kolejnej klatki, dopóki
  poprzednia nie wróciła. Przy przeciążeniu klatki są pomijane, nigdy
  kolejkowane.
- **Zegar w Web Workerze**, nie w wątku głównym i nigdy w
  `requestAnimationFrame`. Timery w Workerach nie podlegają dławieniu w tle.
  Worker tyka, wątek główny wycina piksele i wysyła.
- **`video.currentTime` jedzie w nagłówku.** Ta sama klatka wysłana dwadzieścia
  razy to jedna obserwacja. Ruch sieciowy nie dowodzi świeżości obrazu.
- **`fetch` bez `keepalive: true`** — ta flaga w Chrome twardo ogranicza ciało
  do 64 kB i większy POST znika bez ostrzeżenia. Zwykły `fetch` i tak używa
  puli połączeń.
- **Handler nic nie liczy.** Parsuje, wkłada klatkę do jednoslotowego kanału
  (nadpisując starszą), budzi pętlę mózgu i zwraca aktualny snapshot. Nie
  obiecuje, że snapshot opisuje właśnie przesłaną klatkę — niesie
  `state_version` i `last_frame_seq`, po których panel to pozna.

**Dlaczego handler nie liczy synchronicznie.** Dopasowanie minimapy trwa
kilkadziesiąt milisekund. Gdyby handler czekał na wynik, pętla przeglądarki
zwolniłaby do tempa dopasowania. To dziś nie boli, ale projekt następny — 20 Hz
odczytu pasków HP — właśnie na tym stoi. Rozdzielenie „szybka ścieżka w
handlerze, wolna w goroutine mózgu" jest tu wprowadzone od razu, żeby leczenie
nie wymagało przebudowy transportu.

#### Pozostałe endpointy

`POST /api/arm`, `POST /api/disarm` — bez zmian co do roli; `arm` zwraca token
sesji przechwytywania, którym znakowane są klatki. Klatka z cudzą sesją jest
odrzucana.

`PUT /api/config` — cała konfiguracja panelu naraz, walidowana wszystko-albo-nic
tak jak dziś `SetInputConfig`: prostokąt minimapy i znacznik, zoom, progi
dopasowania, klawisze kierunków, hotkeye akcji pięter, kratka postaci.

`PUT /api/route` — trasa wypychana z panelu przy starcie podążania.

`GET /api/state` — snapshot dla widoku, gdy kamera stoi.

`GET /api/grid`, `GET /api/blocks`, `DELETE /api/blocks` — bez zmian.

`GET /api/preview` — **nowy**. Dziś podgląd okolicy jedzie jako data URI w
odpowiedzi `/api/locate`; snapshot musi zostać mały, więc podgląd przenosi się
do osobnego żądania. Snapshot niesie `preview_revision`, po którym panel
poznaje, że warto go odświeżyć.

`GET /api/route` — **nowy**. Oddaje aktualną trasę wraz z waypointami nagranymi
przez mózg, żeby panel mógł zapisać je do pliku. Waypointów bywa tysiąc, więc
nie mogą jechać w każdym snapshocie.

#### Snapshot stanu

Odpowiedź na każdą klatkę i na `GET /api/state`. Ma stały, ograniczony rozmiar —
żadnych waypointów, żadnego atlasu, żadnej pełnej historii:

```
state_version    rosnący licznik zmian stanu
last_frame_seq   numer ostatniej przetworzonej klatki
armed, reason    stan wykonawcy i powód rozbrojenia
position         x, y, z albo brak
position_age_ms  wiek ostatniej dobrej obserwacji pozycji
match            found, mode, score, match_ms, samples, search_positions
route            wczytana, nazwa, indeks, liczba punktów, ukończona, opis kolejnego kroku
executor         waiting, retries, cycles, blocked, stopped, halted
last_action      rodzaj, kierunek albo typ, status, klawisz, wiek
recorder         auto, liczba nagranych, liczba pominiętych
preview_revision numer, po którym panel poznaje, że podgląd się zmienił
log              ogon ostatnich N wpisów z identyfikatorami
```

#### Źródła jednoklatkowe

Panel umie dziś wczytać screenshot z pliku i obraz demo. To narzędzia
diagnostyczne i zostają: wysyłają jedną klatkę i dostają snapshot. Bot i tak nie
zadziała bez świeżego strumienia — watchdog rozbroi wykonawcę, a brama świeżości
odrzuci każdą emisję — więc jednoklatkowe źródło służy wyłącznie do sprawdzenia
dopasowania.

### Struktura pakietów

```
internal/brain/
  tracker.go    port MinimapTracker
  recorder.go   port RouteRecorder
  executor.go   port StepExecutor
  follower.go   port RouteFollower
  loop.go       orkiestracja: odpowiednik updateRoute + followStep + pumpBlocks
  state.go      snapshot stanu oddawany panelowi
internal/route/
  route.go      format trasy, port route.js
internal/frame/
  frame.go      parsowanie binarnego formatu klatki
```

`internal/input` traci sesję intencji, numery sekwencyjne i heartbeat; zostaje
emiterem z bramkami (focus, limit klawiszy, kalibracja kratki, mapy klawiszy).

`locateapi.go` i `pathapi.go` dzielą się na warstwę HTTP (dekodowanie i
kodowanie) oraz rdzeń, który mózg woła bezpośrednio. To jedyny refaktor
istniejącego kodu, jaki ten projekt wprowadza, i wynika wprost z celu: mózg nie
może rozmawiać z własnym procesem przez HTTP.

### Model współbieżności

Jedna goroutine mózgu jest jedynym właścicielem stanu decyzyjnego. Handlery HTTP
nigdy nie dotykają go bezpośrednio:

- klatki trafiają do jednoslotowego kanału (nowsza nadpisuje starszą),
- konfiguracja i trasa trafiają do kanału poleceń,
- snapshot jest publikowany przez mózg do `atomic.Pointer`, z którego handlery
  czytają bez blokowania.

Zegar jest wstrzykiwany (`now func() time.Time`), tak jak w `nav.BlockStore` —
bez tego testów czasowych nie da się napisać uczciwie.

Osobna, mała goroutine watchdoga rozbraja wykonawcę, gdy klatki przestają
przychodzić.

## Kontrakty przenoszonych modułów

Zachowanie ma pozostać identyczne. Poniżej to, co w każdym module jest
nieoczywiste i najłatwiej zgubić przy porcie.

### Tracker

Kotwica pozycji ważna, dopóki nie minęły 3 chybienia, różnica pięter nie
przekracza 1 i zoom się nie zmienił. Promień lokalnego szukania rośnie z
wiekiem kotwicy i liczbą chybień, ograniczony do 64. Wynik globalny czyszczy
listę odczytów; wynik globalny bez trafienia kasuje kotwicę.

### Recorder

Zmiana piętra zapisuje **dwa** waypointy: kratkę sprzed przejścia (typ zgadnięty
z kierunku i przemieszczenia) oraz kratkę po przejściu. Poza tym waypoint co
`every` kratek odległości Czebyszewa od ostatnio zapisanego.

Bramka z `app.js` przenosi się razem z modułem: **pozycja na kratce, którą mapa
uznaje za nieprzechodnią albo pozbawioną danych, nie zostaje waypointem** —
to dowód błędnego dopasowania, nie miejsce, w którym postać stała. Gdy okno
przechodniości jeszcze nie dotarło, nagrywanie czeka.

### Executor

Najgęstszy moduł i największe ryzyko regresji. Cztery sytuacje, w których krok
**nie** jest dowodem o mapie:

1. klawisz nigdy nie opuścił drivera (`emittedAt` puste),
2. postać zmieniła piętro (wejście na schody wygląda jak nieudany krok),
3. postać stoi gdzie indziej niż przy wysłaniu klawisza (zepchnięta, albo gracz
   przejął sterowanie),
4. odmowa drivera (limit klawiszy, brak hotkeya, zły token) — klawisz nie
   poszedł, więc nic się nie wydarzyło.

Dowodem jest wyłącznie krok potwierdzony jako wyemitowany, po którym przez co
najmniej 3 kolejne klatki postać stała w miejscu.

**Spóźnione dojście unieważnia naukę.** Dojście na kratkę w ciągu
`lateArrivalMS` od spisania kroku na straty oznacza lag albo paraliż, nie
przeszkodę: wysyłany jest przeciwny dowód (`entered`), a liczniki retries i
cycles wracają do zera. Bez tego trzy niezwiązane zwolnienia w sesji sumują się
w trwałe zatrzymanie.

**Wygasanie blokady.** `blocked` dotyczy jednego celu, nie całego biegu, i wygasa
po `blockedTTL`. Wygaśnięcie zaczyna świeżą próbę, a nie ciąg dalszy serii
porażek — dlatego zeruje `cycles`.

Identyfikatory kroków nigdy się nie powtarzają, także po resecie, żeby spóźnione
potwierdzenie nie ostemplowało następcy.

### Follower

Waypoint akcji wymaga dokładnej kratki (`actionTolerance`), waypoint marszu może
mieć luźniejszą tolerancję. Waypoint akcji jest zaliczony przez zmianę piętra —
albo przez stanie na piętrze, na którym trasa się kontynuuje, gdy postać
przekroczyła je między odczytami.

`advance` wykonuje najwyżej jedno okrążenie listy: trasa zapętlona, której
wszystkie punkty mieszczą się w tolerancji, w przeciwnym razie kręciłaby się w
kółko.

Odpowiedź planera starsza niż aktualna rewizja nakładki blokad jest odrzucana —
inaczej trasa policzona przed nauczeniem blokady wysłałaby bota z powrotem w
kratkę, o której właśnie się dowiedział.

### Sklejka (loop.go)

Kolejność w `followStep` jest wynikiem naprawionych błędów i musi zostać
zachowana:

1. `follower.step` **przed** pytaniem wykonawcy o cokolwiek — bramka „akcje
   pięter wyłączone" musi odrzucić wyjście followera, zanim powstanie krok w
   toku, który potem wygasłby w retry i trwałą blokadę,
2. świeżo zablokowany cel każe followerowi porzucić trasę z pamięci, inaczej
   produkuje ten sam cel w nieskończoność,
3. schody są wyjątkiem: follower zgłasza je jako `transition`, wykonawca
   zamienia je w zwykły krok, więc pauza akcji pięter nie może ich blokować.

## Migracja

Duży skok na gałęzi odbitej od `go`. Bez flagi wyboru mózgu, bez trybu cienia —
stary tor znika w tym samym ciągu, w którym powstaje nowy.

Kolejność oddolna, bo tak każdy moduł domyka się testami zanim cokolwiek od
niego zależy:

1. `internal/route` — format trasy, czysta funkcja
2. `internal/frame` — parser klatki
3. `internal/brain/tracker.go`
4. `internal/brain/recorder.go`
5. `internal/brain/executor.go` — najtrudniejszy, robiony po rozgrzewce
6. `internal/brain/follower.go`
7. `internal/brain/loop.go` — orkiestracja
8. Podział `locateapi.go` i `pathapi.go` na HTTP i rdzeń
9. `POST /api/frame`, snapshot, `PUT /api/config`, `PUT /api/route`
10. Przebudowa panelu: kamera w Web Workerze, widok stanu, usunięcie logiki
11. Usunięcie protokołu intencji z `internal/input`
12. Potwierdzenie w grze, aktualizacja README

## Testy

**Przypadki testowe portowane 1:1, konstrukcja pisana idiomatycznie.** Te same
wejścia, te same czasy, te same oczekiwane skutki — przepisane na tabelkowe
testy Go ze wstrzykniętym zegarem, bez `Sleep`. W 2678 liniach testów `.cjs`
siedzi wiedza o błędach, które już raz wystąpiły; odtwarzanie ich „z opisu
zachowania" gubi dokładnie te przypadki brzegowe, dla których je napisano.

Testy powstają **przed** implementacją każdego modułu.

Dodatkowo test całej pętli na scenariuszach: przejście zwykłej trasy, obejście
przeszkody, spóźnione potwierdzenie, utrata pozycji, utrata focusu okna,
milczenie kamery, zapętlona trasa.

Testy `webtests/` kurczą się do tego, co panelowi zostaje: wycinanie regionów,
kadencja Workera, rysowanie stanu.

Uruchamianie lokalnie: `go test ./...` oraz `node --test webtests/*.cjs`. To
repozytorium nie ma Dockera.

## Ryzyka

**Regresja w uczeniu blokad.** Najgęstsza logika w projekcie, a objawia się
najpóźniej: jako kratka trwale omijana bez powodu, zauważona wiele sesji
później. Mitygacja: przypadki testowe przed implementacją, `executor.go` robiony
dopiero po trzech łatwiejszych modułach.

**Dławienie karty w tle.** Panel jest w tle zawsze, gdy grasz. Zegar musi być w
Web Workerze; do zweryfikowania w praktyce, bo w pełni przesłonięte okno
przeglądarki bywa traktowane jak ukryte.

**Brak toru zapasowego.** Duży skok oznacza, że między startem a końcem nie ma
działającego bota do porównania. Mitygacja: gałąź, częste commity po każdym
domkniętym module, `go` nietknięte do końca.

**Presja na GC przy 20 Hz.** `sync.Pool` na bufory klatek i `io.ReadFull`
zamiast `io.ReadAll` od pierwszej wersji, nie jako późniejsza optymalizacja.

## Do usunięcia po migracji

`web/tracker.js`, `web/follower.js`, `web/executor.js`, `web/recorder.js`,
`web/route.js`, `web/input.js`, `web/blocks.js` oraz sklejka decyzyjna z
`web/app.js`. Odpowiadające im pliki w `webtests/`. Z `internal/input`: token
sesji intencji, numery sekwencyjne, heartbeat, `Intent`, `InputResult`.
Endpointy `POST /api/input`, `POST /api/input/done`, `GET /api/input/status`,
`POST /api/input/config` i `POST /api/input/calibrate` — dwa ostatnie wchłonięte
przez `PUT /api/config`. Endpoint `POST /api/locate` znika razem z nimi: jedynym
wejściem obrazu jest `POST /api/frame`.

## Projekt następny

Moduł leczenia: odczyt pasków HP i many z regionów 2 i 3, reguły z progami
procentowymi i priorytetem, hotkeye w slotach, wywłaszczanie kroku, rozdział
budżetu klawiszy. Buduje się na `POST /api/frame` i na pętli mózgu bez zmian w
transporcie.
