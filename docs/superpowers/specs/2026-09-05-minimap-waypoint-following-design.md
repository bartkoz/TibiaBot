# Podążanie za waypointami w minimap-lab (dry-run)

Data: 2026-09-05

## Cel

minimap-lab potrafi już ustalać `(x,y,z)` z obrazu minimapy i śledzić pozycję
przy 5–10 Hz, także przez przejścia Z±1. Ten dokument opisuje kolejny krok:
nagrywanie tras z waypointów, zapis do pliku JSON i prowadzenie po nich
w trybie podglądu.

Zakres obejmuje wyliczanie i pokazywanie trasy. Wysyłanie klawiszy ani
klikanie w klienta gry **nie wchodzi** w ten etap. Panel mówi, dokąd iść;
postać prowadzi człowiek.

## Decyzje

1. **Dry-run.** Brak sterowania postacią, więc brak CGO, robotgo i zależności
   natywnych. Projekt zostaje przy czystym Go i przeglądarce jako źródle obrazu.
2. **Serwer bezstanowy.** Trasy żyją w przeglądarce i w plikach JSON, które
   użytkownik sam wczytuje i pobiera. Serwer nigdy nie zapisuje trasy na dysk
   i nie pamięta sesji.
3. **Ścieżkę liczy Go.** Nowy endpoint `POST /api/path` liczy A* na kaflach
   `Minimap_WaypointCost_*.png`. Przeglądarka nie duplikuje logiki atlasu.
4. **Waypoint niesie typ akcji** od początku, mimo że w dry-runie typ jest tylko
   podpowiedzią dla człowieka. Dzięki temu pliki nagrane teraz zadziałają na
   etapie sterowania.

## Format pliku trasy

```json
{
  "version": 1,
  "name": "Venore → Cyclopolis",
  "waypoints": [
    {"x": 32958, "y": 32077, "z": 7, "type": "walk", "label": "depo"},
    {"x": 32960, "y": 32090, "z": 7, "type": "rope", "label": "lina w dół"},
    {"x": 32960, "y": 32090, "z": 8, "type": "walk", "label": "po zejściu"}
  ]
}
```

- `type` ∈ `walk | rope | ladder | stairs | hole | shovel`. Brak pola oznacza
  `walk`, więc ręcznie pisany plik może zawierać samo XYZ.
- `label` jest opcjonalna, do 64 znaków.
- Walidacja: `x`,`y` ∈ 0–65535, `z` ∈ 0–15, do 1000 punktów, `version` musi
  wynosić 1. Plik niezgodny z tym opisem jest odrzucany z komunikatem
  wskazującym numer wadliwego punktu.
- Pole `version` pozwoli w przyszłości dołożyć promień tolerancji per punkt bez
  unieważniania nagranych plików.

## Endpoint `POST /api/path`

Żądanie to czysty JSON — w odróżnieniu od `/api/locate` nie przenosi obrazu:

```json
{"from":{"x":32958,"y":32077,"z":7},"to":{"x":32970,"y":32090,"z":7},"margin":64}
```

Odpowiedź:

```json
{"found":true,"steps":[[32958,32077],[32959,32078]],"tiles":14,"cost":16.2,"elapsed_ms":3,"reason":"…"}
```

Trzy sytuacje brzegowe zwracają `found:false` z czytelnym `reason`, a nie błąd
HTTP, bo są normalnym stanem pracy, nie awarią:

- `from.z` różne od `to.z` — trasa międzypiętrowa wymaga akcji, nie chodzenia,
- start lub cel na kratce nieprzechodniej albo niezbadanej,
- brak trasy w zadanym obszarze.

Błędem 400 pozostaje wyłącznie nieprawidłowe wejście: zakres współrzędnych,
`margin` poza 0–256, niepoprawny JSON. `margin` równy 0 oznacza wartość domyślną
64, zgodnie z konwencją `floor_radius` w istniejącym `/api/locate`.

### Ograniczenia wyszukiwania

A* działa w prostokącie rozpiętym na starcie i celu, powiększonym o `margin`
(domyślnie 64 kratki, maksymalnie 256) — nie na całym piętrze. Do tego limit
iteracji i timeout 5 s. Trasa wymagająca dużego objazdu poza tym prostokątem
zwróci `found:false`; użytkownik zwiększa `margin` albo dokłada waypoint pośredni.

### Koszty i przechodniość

Kafle `Minimap_WaypointCost_*.png` to obrazy 8-bitowe 256×256. Wartość 255
oznacza kratkę zablokowaną, 0 — teren niezbadany. Obie są nieprzechodnie.
Brakujący kafel traktujemy jak teren niezbadany, tak samo jak atlas kolorów
odróżnia brak danych od czarnej ściany.

### Współbieżność

`server.gate` jest semaforem na jedno żądanie i chroni pętlę `/api/locate`
działającą przy 10 Hz. `/api/path` **nie** korzysta z tego semafora — dostaje
własny mutex i własny cache kafli kosztów. Inaczej pojedyncze wyliczenie trasy
odbierałoby przepustowość śledzeniu pozycji i generowało odpowiedzi 429.

## Nagrywanie tras

Nowa sekcja „Trasa" w panelu, aktywna gdy tracking podaje świeżą pozycję.

**Tryb ręczny.** Przycisk „Dodaj waypoint" dopisuje aktualne XYZ na koniec listy.
Aktywny wyłącznie przy `found` i wieku pozycji poniżej 1 s, żeby nie zapisać
nieaktualnej kratki.

**Tryb automatyczny.** Przełącznik „Nagrywaj trasę" dopisuje punkt, gdy odległość
Chebysheva od ostatnio zapisanego punktu osiągnie N kratek (domyślnie 10) oraz zawsze przy wykrytej zmianie Z. Podczas nagrywania
focus ma klient gry, więc skróty klawiszowe w przeglądarce nie zadziałają —
tryb automatyczny jest jedynym wygodnym sposobem nagrania długiej trasy.

### Punkty przejść między piętrami

Recorder dowiaduje się o przejściu dopiero po nim, stojąc już na nowym piętrze,
a trasa potrzebuje kratki sprzed przejścia. Dlatego recorder trzyma bufor
ostatnich potwierdzonych pozycji i przy zmianie Z wstawia **parę** punktów:
ostatnią kratkę na starym piętrze z typem akcji oraz pierwszą na nowym jako `walk`.

Typ akcji jest zgadywany, bo minimapa nie koduje rodzaju przejścia — lina,
drabina, schody i dziura wyglądają w danych identycznie:

| Obserwacja | Typ |
|---|---|
| Z maleje (w górę), XY bez zmian | `rope` |
| Z rośnie (w dół), XY bez zmian | `hole` |
| Z się zmienia, XY przesuwa się o kratkę lub dwie | `stairs` |

Heurystyka trafia w typowe przypadki i myli się np. między liną a drabiną
w górę, dlatego typ jest edytowalnym dropdownem przy każdym wierszu.

Przy linie i dziurze XY się nie zmienia, więc punkt wychodzi dokładnie.
Przy schodach może wypaść o kratkę obok i wymagać poprawki.

**Znane ograniczenie**, odziedziczone po module śledzenia: jeżeli wycinek
minimapy pasuje równie dobrze na starym piętrze, przejście może nie zostać
wykryte. Wtedy użytkownik zmienia Z ręcznie w panelu, a recorder wstawia punkt
w momencie tej zmiany.

### Lista i pliki

Lista pokazuje numer, `x, y, z`, dropdown typu, pole `label` oraz przyciski
↑ / ↓ / ✕. Nad listą „Wczytaj JSON", „Pobierz JSON" i „Wyczyść".

Robocza trasa jest autozapisywana w `localStorage` — bez tego odświeżenie karty
kasuje godzinę nagrywania. Zapis na dysk zawsze wymaga świadomego kliknięcia
„Pobierz", zgodnie z decyzją o bezstanowym serwerze.

## Pętla podążania

Po każdym udanym odczycie pozycji, gdy podążanie jest włączone:

1. Bieżący waypoint na innym Z niż pozycja → panel pokazuje instrukcję wynikającą
   z typu („użyj liny", „wejdź po drabinie") i czeka, aż tracker potwierdzi nowe Z.
   Ścieżka nie jest liczona.
2. Odległość Chebysheva do waypointa nie większa niż tolerancja (domyślnie
   1 kratka, czyli także po skosie) → punkt zaliczony, przechodzimy do następnego. Koniec listy oznacza „Trasa ukończona"
   albo powrót na początek, jeśli włączone jest zapętlenie.
3. W pozostałych przypadkach: jeżeli nie mamy ścieżki, jej cel nie jest bieżącym
   waypointem albo bieżąca pozycja nie jest żadną z pozostałych kratek ścieżki,
   prosimy `/api/path`. Nie częściej niż raz
   na 500 ms i nigdy dwa żądania równolegle.
4. Z posiadanej ścieżki przycinamy przebyty fragment, bierzemy następną kratkę
   i zamieniamy na kierunek (N, NE, E, …). Panel pokazuje strzałkę oraz dystans
   i numer docelowego waypointa.

Ścieżka jest rysowana na istniejącym podglądzie referencyjnym — wycinku 129×129
kratek wokół gracza, w którym krok `(x,y)` wypada na pikselu
`(64 + x − px, 64 + y − py)`.

## Moduły

Go, w płaskim pakiecie `main`, zgodnie z układem reszty projektu:

- `cost.go` — ładowanie kafli `Minimap_WaypointCost_*.png`, cache obszarowy pod
  własnym mutexem, odczyt kosztu kratki.
- `path.go` — A* z metryką octile, bounding box, limit iteracji, timeout.
- handler `POST /api/path` w `main.go`.

JavaScript w `web/`, wzorem istniejącego `tracker.js` — czysta logika oddzielona
od DOM, żeby dała się testować w node:

- `route.js` — parsowanie, walidacja i serializacja formatu trasy.
- `follower.js` — stan podążania: bieżący waypoint, warunek zaliczenia, decyzja
  o przeliczeniu ścieżki, wyliczenie kierunku.
- `app.js` — spięcie z interfejsem.

## Testy

Go:

- A*: prosta droga, obejście ściany, brak trasy, zablokowany start, zablokowany
  cel, różne piętra, wynik przy granicy bounding boxa, zadziałanie limitu iteracji.
- Kafle kosztów: ładowanie, brakujący kafel jako teren nieprzechodni, trasa
  przechodząca przez granicę kafli.
- `/api/path`: walidacja wejścia i odpowiedzi 400, poprawne żądanie i kształt JSON.
- Test na mapach z repozytorium za flagą `MINIMAP_REAL_MAP_TEST=1`, jak istniejące
  testy lokalizacji.

JavaScript, przez `node --test`:

- `route_test.cjs` — walidacja formatu, obsługa `version`, odrzucanie plików
  niezgodnych, domyślny `walk` przy braku `type`.
- `follower_test.cjs` — zaliczenie waypointa, zejście z trasy i przeliczenie,
  ograniczenie częstotliwości żądań, oczekiwanie na zmianę piętra, koniec trasy
  i zapętlenie.

W repozytorium nie ma `docker-compose.yml` ani `Dockerfile`; testy uruchamiane są
lokalnie komendami z README (`go test ./...`, `node --test`).

## Poza zakresem

- Wysyłanie klawiszy i kliknięć do klienta gry.
- Automatyczne używanie liny, łopaty i drabin.
- Trasy międzypiętrowe liczone jednym wywołaniem A*.
- Zapis tras po stronie serwera i biblioteka tras współdzielona między sesjami.
