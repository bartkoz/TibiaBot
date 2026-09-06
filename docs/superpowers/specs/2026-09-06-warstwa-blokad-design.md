# Warstwa nauczonych blokad

Data: 2026-09-06

## Cel

Bot chodzi po trasie waypointów, licząc drogę algorytmem A* na kaflach
`Minimap_WaypointCost_*.png`. Te dane znają teren i ściany budynków, ale **nie
znają mebli**: lada w sklepie, stół, skrzynia i zamknięte drzwi stoją na
kratkach opisanych jako w pełni przechodnie. Dowód jest w danych — porównanie
warstwy koloru i kosztu w Rookgaard pokazuje czerwone (255) obrysy ścian i
jednolicie zielone wnętrza budynków.

Skutkiem jest zakleszczenie. A* prowadzi trasę przez ladę, postać nie może
wejść, `StepExecutor` po dwóch próbach oznacza cel jako `blocked` i każe
followerowi przeliczyć trasę, A* zwraca dokładnie tę samą trasę — dane się nie
zmieniły — i po trzech cyklach wykonawca zatrzymuje się na dobre. Wiedza „tu
się nie da" nie ma dziś gdzie trafić.

Ten dokument opisuje warstwę nauczonych blokad: pamięć kratek, po których nie
da się chodzić mimo tego, co mówią dane mapy. Zasila ją nieudany krok
(„bump-and-learn") i tylko on.

Zakres obejmuje też podgląd na żywo w panelu: mapa wokół postaci pokazująca,
co jest przechodnie w danych, czego w danych nie ma, i co bot sam nauczył się
blokować.

## Skąd biorą się rozbieżności

`data/minimap` to paczka pobrana z sieci (`~/Downloads/minimap 2`, marzec
2026), nie eksport z klienta gracza. Niezależnie od źródła problem zostaje:
minimapa Tibii koduje podłoże i ściany, a meble rysuje wyłącznie viewport.
Żadna warstwa minimapy nie opisze lady.

Klient gry nie ratuje sytuacji. `staticmapdata-*.dat` w assetach zawiera POI i
miasta, nie kolizje świata.

Rozważana była druga droga: gotowa lista przechodnich obiektów — z wiki albo z
`appearances-*.dat` klienta — i rozpoznawanie z obrazu gry, co stoi na kratce.
**Odrzucona.** Lista jest niepełna, a brakujące ogniwo „piksel → itemid"
kosztuje osobny podsystem (dekoder sprite'ów, kalibracja siatki viewportu,
dopasowanie), którego wynik i tak trzeba by weryfikować w praktyce. Nauka z
nieudanych kroków jest kompletna z definicji: obejmuje meble, drzwi, graczy i
potwory jednym mechanizmem, bez żadnej listy do utrzymywania.

## Decyzje

1. **Właścicielem wiedzy jest serwer Go.** Panel zgłasza obserwacje, decyzję
   podejmuje serwer. Alternatywa — stan w JS wysyłany z każdym żądaniem trasy —
   odpada: `/api/path` ma limit ciała 4 KiB (około 150 kratek), stan ginie z
   odświeżeniem karty, a przyszły moduł wizji, który też mieszka w Go,
   musiałby przepychać wykryte meble do przeglądarki tylko po to, żeby ta
   odesłała je z powrotem.
2. **Blokada tymczasowa to kara kosztu, nie ściana.** Świeży wpis podnosi
   koszt kratki o 500; dopiero wpis trwały czyni ją nieprzechodnią. Gracz
   stojący w jednokratkowych drzwiach nie odcina wtedy trasy: jeśli objazd
   istnieje, A* go wybierze, a jeśli nie — trasa nadal prowadzi przez tę
   kratkę, bot czeka i próbuje ponownie, zamiast ogłosić „brak drogi" i stanąć.
3. **Krok po skosie nigdy nie uczy o kratce.** W Tibii skos zawodzi także
   wtedy, gdy zablokowane są oba kafle ortogonalne przy rogu — kratka docelowa
   bywa wtedy zupełnie pusta. Bot uczący się ze skosów systematycznie wycina z
   mapy przechodnie skrzyżowania. Nieudany skos blokuje wyłącznie **krawędź**
   `from→to`, tymczasowo, bez prawa awansu.
4. **Bazowa siatka kosztów pozostaje nietknięta.** `CostGrid.limitTo()` kopiuje
   strukturę, ale współdzieli tablicę pikseli z cache'em piętra; wpisanie do
   niej 255 zatrułoby wszystkie kolejne zapytania. Nakładka jest osobną,
   rzadką strukturą, czytaną obok siatki.
5. **Awans na trwałe wymaga dwóch niezależnych epizodów.** Retry tego samego
   kroku, ponowne przeliczenie tej samej trasy i powtórzone zgłoszenie to
   jeden epizod. Drugi liczy się dopiero po wygaśnięciu pierwszej blokady.
6. **Trwała blokada jest odwoływalna.** Potwierdzone wejście na kratkę kasuje
   wpis, także z pliku. Bez tego długo stojący NPC albo drzwi, które ktoś
   otworzył, trwale wykreślałyby poprawną drogę.

## Architektura

### Nowe i zmienione pliki

| Plik | Rola |
|---|---|
| `blocks.go` (nowy) | `BlockStore`: kratki i krawędzie, TTL, epizody, awans, zapis i odczyt pliku |
| `blocksapi.go` (nowy) | `POST /api/blocks/observe`, `GET /api/blocks`, `DELETE /api/blocks` |
| `gridapi.go` (nowy) | `GET /api/grid` — okno przechodności wokół postaci dla podglądu |
| `cost.go` | Rozróżnienie „brak danych" od „ściana": lista wczytanych chunków |
| `path.go` | `findPath` czyta przez `PathGrid` (baza + snapshot nakładki) |
| `pathapi.go` | Snapshot nakładki przed wyszukiwaniem; kasowanie blokady pod postacią |
| `web/executor.js` | Klasyfikacja wyniku próby, spóźnione przybycie, porównanie z Z |
| `web/blocks.js` (nowy) | Zgłaszanie obserwacji i pobieranie okna podglądu |
| `web/app.js` | Spięcie: obserwacje z executora, rysowanie podglądu |
| `web/index.html`, `web/tracking.css` | Sekcja podglądu, legenda, panel wybranej kratki |

### Magazyn blokad

```go
type Kind uint8 // KindTemp, KindPerm

type Blockage struct {
    Kind     Kind
    Episodes int       // niezależne epizody, nie retry
    Expires  time.Time // do kiedy wpływa na trasę; zero dla KindPerm
    Forget   time.Time // kiedy znika sam rekord
}

type BlockStore struct {
    mu    sync.RWMutex
    now   func() time.Time
    tiles map[[3]int]*Blockage  // x, y, z
    edges map[[5]int]*Blockage  // fromX, fromY, toX, toY, z
    path  string                // plik trwałych blokad
    dirty bool
}
```

`Expires` i `Forget` są rozdzielone celowo. Gdyby rekord znikał razem z
blokadą, drugi epizod nigdy nie zostałby rozpoznany jako drugi i awans na
trwałe nie zadziałałby ani razu. Blokada tymczasowa przestaje wpływać na trasę
po 60 s, ale rekord żyje 24 h.

Metody: `Observe(obs) Decision`, `Snapshot(area, z) Overlay`, `Clear(tile)`,
`Entered(tile)`, `Load()`, `Save()`. Zegar jest wstrzykiwany, więc TTL, awans i
zapominanie testują się bez czekania.

### Kwalifikacja obserwacji

`POST /api/blocks/observe`:

```json
{"session":"…",
 "from":{"x":32554,"y":32510,"z":7},
 "to":{"x":32554,"y":32509,"z":7},
 "outcome":"no_motion",
 "still_frames":3,
 "last_frame_age_ms":140}
```

`outcome` przyjmuje `no_motion` i `entered`. Serwer sprawdza warunki i
odpowiada tym, co postanowił (`ignored`, `temp`, `promoted`, `cleared`) wraz z
powodem — panel to pokazuje, więc odrzucenie nigdy nie wygląda jak zgubione
żądanie.

| Sytuacja | Reakcja |
|---|---|
| `outcome` inny niż `no_motion` / `entered` | `ignored` |
| `from` i `to` nie sąsiadują albo mają różne Z | `ignored` — to nie jest pojedynczy krok |
| `still_frames < 3` lub `last_frame_age_ms > 300` | `ignored` — za słaby dowód |
| Krok prosty, warunki spełnione | epizod na kratce `to`, blokada tymczasowa 60 s |

Panel zgłasza obserwację dopiero po wyczerpaniu retry, czyli gdy ten sam cel
zawiódł dwa razy z rzędu. Te dwie próby to **jeden** epizod, nie dwa: dzielą
jedno zdarzenie i jeden stan gry.
| Krok po skosie, warunki spełnione | blokada krawędzi `from→to`, 20 s, bez awansu |
| Drugi epizod na kratce po wygaśnięciu pierwszego | awans na trwałą, zapis pliku |
| `outcome: "entered"` | kasuje blokadę kratki, także trwałą, i zeruje epizody |

Poza tym `/api/path` kasuje blokadę kratki, na której stoi `from`: obecność
postaci jest mocniejszym dowodem niż jakakolwiek nauczona hipoteza, a kosztuje
to jedno sprawdzenie mapy, bez osobnego żądania.

Nie ma automatycznego kroku diagnostycznego („spróbuj w bok, żeby sprawdzić,
czy gra odpowiada"). Dwa epizody rozdzielone wygaśnięciem TTL wymagają, żeby
postać w międzyczasie normalnie chodziła, więc zawieszona gra nie wyprodukuje
trwałego wpisu, a bot nie wykonuje ruchów, o które nikt nie prosił.

### Trwałość

Trwałe blokady mieszkają w pliku wskazanym przez `-blocks`, domyślnie
`blocks.json` w katalogu roboczym. Katalog map celowo **nie** jest domyślną
lokalizacją: to pobrana paczka danych, a nasze wpisy nie powinny się z nią
mieszać ani ginąć przy jej podmianie. Format:

```json
{"version": 1,
 "tiles": [{"x": 32554, "y": 32509, "z": 7, "episodes": 2, "first_seen": "2026-09-06T20:14:11Z"}]}
```

Zapis atomowy: plik tymczasowy w tym samym katalogu, `fsync`, `rename`. Jeden
writer, wyzwalany zmianą, nie częściej niż co 5 s. Plik z inną wersją jest
odrzucany z komunikatem, nie nadpisywany po cichu. Krawędzie i blokady
tymczasowe nie trafiają na dysk.

### Wpięcie w A*

```go
type PathGrid struct {
    base    *CostGrid
    tiles   map[[2]int]uint8 // KindTemp / KindPerm, jedno piętro
    edges   map[[4]int]bool
}

func (g *PathGrid) Blocked(x, y int) bool     // baza 255 albo KindPerm
func (g *PathGrid) Cost(x, y int) float64     // baza, +500 dla KindTemp
func (g *PathGrid) EdgeBlocked(from, to [2]int) bool
```

Snapshot nakładki powstaje raz, przed wywołaniem `findPath`, pod zamkiem
magazynu: graf nie może zmienić się w trakcie wyszukiwania, bo A* zakłada
stały koszt zamkniętych wierzchołków. Ważność wpisów oceniana jest w chwili
tworzenia snapshotu.

`findPath` zmienia trzy odczyty: koszt sąsiada, test nieprzechodniości i test
zamkniętego rogu — wszystkie idą przez `PathGrid`, więc nauczony mebel
poprawnie zabrania skosu obok siebie. Kara +500 jest dodawana do kosztu
kratki przed przemnożeniem przez wagę kroku, tak samo jak koszt bazowy.

Heurystyka zostaje admisyjna: skalowanie najtańszą kratką obszaru dotyczy
kosztu bazowego, a kara tylko podnosi koszt rzeczywisty.

### Brak danych a ściana

Dziś `CostGrid` mapuje brakujący chunk i teren nieprzechodni na tę samą
wartość 255. Do uczciwego podglądu trzeba je rozróżnić, więc `loadCostArea`
zapamiętuje prostokąty wczytanych chunków, a `CostGrid` dostaje
`Covered(x, y) bool`. Wyszukiwanie trasy traktuje oba przypadki tak samo jak
dotąd — brak danych pozostaje nieprzechodni.

### Executor: co właściwie się stało

`failPending()` miesza dziś „postać nie ruszyła się" z „klawisz nigdy nie
wyszedł z drivera". Do nauki potrzebny jest jawny wynik próby:

| Wynik | Znaczenie | Uczy? |
|---|---|---|
| `no_motion` | ≥3 świeże klatki pokazują postać wciąż na `from` | tak |
| `moved_elsewhere` | postać jest gdzie indziej — zepchnięta albo prowadzona ręcznie | nie |
| `floor_changed` | zmieniło się Z | nie |
| `transport_error` | odmowa drivera, błąd HTTP, brak potwierdzenia emisji | nie |
| `observation_lost` | utrata pozycji albo brak świeżych klatek | nie |

`floor_changed` nie uczy nigdy — bez tego bot nauczyłby się, że schody to
ściana, bo wejście na nie zmienia Z i wygląda jak nieudany krok. Porównanie
pozycji obejmuje Z, nie samo XY.

**Spóźnione przybycie**: wejście na kratkę do 600 ms po upływie timeoutu
wysyła `entered` i odwołuje naukę — to był lag albo paraliż, nie przeszkoda.

`stepTimeoutMS` rośnie z 1200 do **1800**. Czas kroku w Tibii zależy od
prędkości postaci i kosztu terenu; na błocie albo pod paraliżem krok trwa
dłużej niż sekundę, a timeout krótszy od kroku zamieniałby każdy taki ruch w
fałszywą blokadę. Timeout adaptowany do zmierzonej prędkości i kosztu kratki
jest świadomie **odłożony**: dwa kwalifikowane epizody plus reguła spóźnionego
przybycia załatwiają lag taniej. Jeśli 1800 ms okaże się w praktyce za krótkie,
dołożymy pomiar.

Wygaśnięcie blokady w Go musi odblokować także panel. Dziś `blocked` w
executorze trwa, dopóki follower nie poprosi o inny cel — a przy karze kosztu
zamiast ściany A* nadal może prowadzić przez tę samą kratkę, więc cel się nie
zmienia i bot stoi mimo że przeszkoda zniknęła. `blocked` dostaje więc własny
czas życia równy TTL blokady tymczasowej (60 s), po którym ten sam cel
dostaje kolejną szansę. Executor nie odpytuje Go o stan nakładki — czas jest
tą samą informacją, tylko bez dodatkowego żądania.

### Podgląd na żywo

`GET /api/grid?x=&y=&z=&r=32` zwraca `application/octet-stream`, jeden bajt na
kratkę, wierszami od lewego górnego rogu okna. Promień 32 daje 65×65 = 4225
bajtów.

```
bit 0  nieprzechodni w danych mapy
bit 1  brak danych (nie ma kafla PNG)
bit 2  blokada tymczasowa (nauczona)
bit 3  blokada stała (nauczona)
```

Nagłówki niosą `X-Grid-Origin` i `X-Grid-Revision`; rewizja rośnie z każdą
zmianą nakładki, więc panel wie, kiedy przerysować.

Panel wypełnia `ImageData` i robi jedno `putImageData`, skalując przez CSS
`image-rendering: pixelated` — 4225 kratek to ułamek milisekundy. Żądanie
wychodzi przy zmianie kratki postaci albo nie częściej niż co 500 ms, zawsze
najwyżej jedno naraz, a odpowiedź spóźniona względem zmiany piętra jest
odrzucana. Endpoint nie korzysta z zamka pętli `/api/locate` i ma własny mały
cache, żeby nie wypierać cache'u planera trasy.

Kliknięcie kratki pokazuje jej koszt bazowy, rodzaj blokady, liczbę epizodów i
czas do wygaśnięcia oraz pozwala wpis usunąć (`DELETE /api/blocks`).

Podgląd jest też narzędziem diagnostycznym: lada, która dziś świeci na zielono
mimo że postać przez nią nie przejdzie, jest widocznym dowodem, że dane mapy
jej nie znają.

## Testy

Go (`go test ./...`, bez gry i bez przeglądarki):

- TTL, epizody, awans i zapominanie na wstrzykniętym zegarze.
- Odrzucanie obserwacji: nie-sąsiednie kratki, różne Z, za mało klatek, za
  stara klatka, nieznany `outcome`.
- Skos blokuje krawędź, nie kratkę; krawędź nigdy nie awansuje.
- `entered` kasuje wpis trwały i zeruje epizody.
- A*: objazd wokół nauczonej blokady; kara tymczasowa przepuszcza trasę, gdy
  objazdu nie ma; wpis trwały jest nieprzechodni; nauczona kratka zabrania
  skosu przez zamknięty róg.
- Bazowy `CostGrid` nie zmienia się po zapytaniu z nakładką, także przy
  równoległych zapytaniach (`-race`).
- Zapis pliku: atomowość, odrzucenie obcej wersji, przetrwanie restartu.
- `/api/grid`: rozmiar odpowiedzi, flagi, rozróżnienie braku danych od ściany,
  walidacja promienia.

JS (`node --test`):

- Klasyfikacja wyniku próby — każdy z pięciu przypadków.
- Spóźnione przybycie wysyła `entered` i nie uczy.
- Skos nie produkuje obserwacji kratki.
- Zmiana Z nie produkuje obserwacji.
- Wygaśnięcie blokady zdejmuje `blocked` w executorze.

Testy działają lokalnie — `minimap-lab` jest samodzielnym modułem Go bez
Docker Compose.

## Poza zakresem

- **Rozpoznawanie obiektów z viewportu.** Odrzucone, patrz wyżej.
- **Otwieranie drzwi.** Kratka z zamkniętymi drzwiami jest zwykłą blokadą.
- **Rozróżnianie przyczyny blokady.** Czas utrzymywania się przeszkody nie
  odróżnia postaci od mebla, a zgadywanie zapisywałoby w pliku hipotezy udające
  fakty.
- **Adaptacyjny timeout kroku** — patrz uzasadnienie wyżej.
