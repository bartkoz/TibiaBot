# Minimap Lab (Go)

Samodzielny prototyp lokalizacji `(x,y,z)` z obrazu minimapy, prowadzenia po trasie waypointów i opcjonalnego sterowania klawiaturą i myszą. Bez OpenCV i bez CGO na każdej platformie; jedyna zewnętrzna zależność to `github.com/ebitengine/purego`, wpięta wyłącznie w emiter macOS — Windows i Linux jej nie potrzebują. Panel działa lokalnie i analizuje screenshoty lub klatki ekranu udostępnionego przez przeglądarkę. Moduł realizuje odczyt pozycji i wskazuje kolejny krok trasy; **sterowanie postacią jest opcjonalne i wyłączone domyślnie**. Uruchomiony z `-input system`, panel może wysyłać klawisze i kliknięcia do gry; `-input dry` ćwiczy tę samą ścieżkę, ale nie wysyła nic do systemu. Zobacz sekcję **Sterowanie**.

## Uruchomienie

```sh
cd /Users/Bartek/TibiaBot/minimap-lab
./minimap-lab
```

Otwórz **http://127.0.0.1:8095** i kliknij **Uruchom demo**. Oczekiwany wynik: **32200, 32180, 7**. Demo jest syntetyczne, pokazuje działanie algorytmu, nie dowodzi skuteczności na obrazie klienta gry.

Domyślny katalog map to `../data/minimap`, względem katalogu uruchomienia. W tym projekcie są już pliki referencyjne.

Gotowy plik wykonywalny jest zbudowany dla tego Maca (Apple Silicon). Kompilacja ze źródeł: `go run .`. Moduł wskazuje toolchain Go 1.24.2, dostępny już lokalnie; nowszy Go też może go zbudować. Na innym komputerze Go może pobrać wskazany toolchain. Domyślny Go 1.22.2 na tym Macu tworzył pliki odrzucane przez loader systemowy (`missing LC_UUID`), dlatego kompilacja i testy używają Go 1.24.2.

```sh
go run . -maps /sciezka/do/minimap -listen 127.0.0.1:8096
go test ./...
go build -o minimap-lab .
```

Na Windows: `go build -o minimap-lab.exe .`, a następnie `minimap-lab.exe -maps "C:\sciezka\do\minimap"`. Ten sam kod działa bez natywnych bibliotek; przechwytywanie obrazu obsługuje przeglądarka.

## Test z grą

1. Ustaw stały zoom minimapy i wycentruj ją na postaci.
2. Kliknij **Wczytaj screenshot** (najlepiej PNG) albo **Udostępnij ekran** i wybierz źródło w oknie przeglądarki. Dostęp do obrazu wymaga zgody przeglądarki/systemu. Jeżeli źródło daje czarny obraz, lokalizator nie będzie z niego działał.
3. Przeciągnij po podglądzie, zaznaczając teren minimapy bez ramki, przycisków i podpisów. Współrzędne zaznaczenia odnoszą się do oryginalnego obrazu, również na ekranach Retina.
4. Kliknij środek znacznika postaci na powiększonym wycinku. Różowy kwadrat maskuje znacznik.
5. Zostaw **piksele na kratkę → Auto**. Program sprawdza kolejno skale `1–4`, a pierwszą dającą jednoznaczne dopasowanie zachowuje w panelu. To kalibracja heurystyczna, nie porównanie wszystkich skal jednocześnie. Po zmianie zoomu gry wybierz Auto ponownie. Ręcznie dostępne są skale całkowite `1–8`; skala ułamkowa lub oddalenie poniżej 1 px/kratkę wymaga zmiany źródła.
6. Wybierz właściwe piętro Z, kliknij **Znajdź pozycję**. Obok współrzędnych zobaczysz fragment atlasu z zaznaczonym kandydatem. Przy wyniku niejednoznacznym współrzędne pozostają nieznane, a JSON pokazuje kandydatów diagnostycznych.
7. Wybierz **10 odczytów/s** albo **5 odczytów/s** i zaznacz **Włącz śledzenie XYZ**. Podczas pierwszego wyszukiwania pozostań w miejscu; po potwierdzeniu lokalnej pozycji przejdź ręcznie kilka kratek. Zmiana rozdzielczości zatrzymuje odczyt i wymaga ponownego zaznaczenia.
8. Gdy pozycja jest stabilna, przejdź do sekcji **4. Trasa**: zaznacz **Nagrywaj trasę** i przejdź planowaną drogę, potem **Pobierz JSON**. Do prowadzenia po zapisanej trasie wczytaj plik i zaznacz **Podążaj za trasą**.

## Tester 5–10 Hz

Pierwszy odczyt przeszukuje całe piętro. Kolejne wysyłają ostatnie XYZ i promień wyszukiwania, zwykle 5 kratek przy 10 Hz: 121 możliwych pozycji. Promień rośnie z wiekiem ostatniej pozycji i ustawioną maksymalną prędkością, do 64 kratek. Wynik z długiego pierwszego wyszukiwania zachowuje rzeczywisty wiek obrazu, więc pierwsze lokalne potwierdzenie może wymagać większego promienia.

Po nieudanym odczycie pole XYZ pokazuje **Pozycja nieznana**. Tester próbuje szerszego lokalnego obszaru; po trzech niepowodzeniach wykonuje jedno pełne wyszukiwanie. Jeśli ono też zawiedzie, zatrzymuje powtarzanie. Przycisk **Szukaj od nowa na całej mapie** pozwala ręcznie odrzucić poprzednią lokalizację. Wynik leżący dokładnie na granicy promienia wymaga szerszego potwierdzenia.

Domyślnie włączone jest **Rozpoznawaj przejścia Z ±1**. Jeżeli dopasowanie na aktualnym piętrze zawiedzie, ten sam odczyt sprawdza Z−1 i Z+1 w obszarze **±8 kratek XY** od ostatniej potwierdzonej pozycji. Promień przejścia można zmienić w **Zakres śledzenia → Zmiana piętra: promień XY** (1–32). Oba sąsiednie piętra są porównywane ze sobą oraz z kandydatem na pierwotnym piętrze; podobne wyniki pozostawiają pozycję nieznaną. Potwierdzenie nowego Z automatycznie aktualizuje selektor i kolejne odczyty pozostają lokalne.

Ręczna zmiana selektora Z o jeden poziom również zachowuje ostatnie XY i nie zatrzymuje uruchomionej pętli. Wtedy sprawdzane jest tylko wybrane piętro. Zmiana ręczna większa niż jeden poziom wymaga pełnego wyszukiwania. Po trzech nieudanych odczytach obejmujących sąsiednie piętra pełny skan odbywa się na piętrze wskazanym w panelu; przy nieznanym, dalszym Z trzeba je wskazać ręcznie.

Na nowym piętrze wczytywane są wyłącznie kafle obejmujące lokalny obszar i cały wycinek minimapy. Do trzech takich małych atlasów pozostaje w pamięci obok atlasu pełnego. Brak danych na sąsiednim piętrze jest raportowany w `unavailable_floors`. Domyślnie poprawne dopasowanie na aktualnym Z kończy odczyt bez sprawdzania innych pięter: identyczny obraz na dwóch piętrach może więc uniemożliwić wykrycie przejścia. W takiej sytuacji Z można wybrać ręcznie.

Panel pokazuje:

- **Odczyty/s** — zmierzoną częstotliwość zakończonych lokalnych odczytów z ostatnich 3 sekund; pełne wyszukiwanie nie jest wliczane do tego pomiaru.
- **Cały odczyt** — czas przechwycenia wycinka, kodowania PNG, żądania i odbioru odpowiedzi.
- **Dopasowanie Go** — czas samego algorytmu i przygotowania próbek na serwerze.
- **Wiek pozycji** — czas od przechwycenia ostatniego poprawnie dopasowanego obrazu, a nie od końca obliczeń.
- **Poprawne odczyty** — udział zaakceptowanych lokalnych dopasowań w ostatnich 3 sekundach.
- **Obszar** — lokalne wyszukiwanie albo całe piętro i liczbę możliwych pozycji.

Pętla odejmuje czas obliczeń od okresu 100/200 ms i nigdy nie kolejkuje równoległych żądań. Przy wolnym przetwarzaniu pokazuje niższą rzeczywistą częstotliwość. Nie liczy ponownie klatki o tym samym czasie odtwarzania źródła; po sekundzie bez nowej klatki ukrywa stare XYZ. Ograniczenia przechwytywania i harmonogramu przeglądarki nadal mogą obniżać częstotliwość — sprawdzaj pomiar w panelu. Jest to tester odczytu podczas ręcznego chodzenia, także między sąsiednimi piętrami; sam z siebie nie wysyła klawiszy — robi to dopiero uzbrojony wykonawca po zaznaczeniu **Chodź automatycznie**, opisany w sekcji **Sterowanie**.

Benchmarki na zapisanym prawdziwym wycinku:

```sh
go test -run '^$' -bench 'Benchmark(HTTP)?TrackActualCapture' -benchtime=2s -benchmem
node --test ui_test.cjs tracker_test.cjs route_test.cjs recorder_test.cjs follower_test.cjs blocks_test.cjs
```

Benchmark HTTP obejmuje dekodowanie PNG, obsługę żądania i odpowiedź JSON wewnątrz procesu. Nie obejmuje przeglądarki ani sieci; rzeczywisty czas całego odczytu pokazuje panel.

Najpierw sprawdź znane miejsce, pojedyncze kroki w czterech kierunkach i granicę kafli. Nie obniżaj progów tylko po to, żeby wymusić wynik. Duży wycinek z charakterystycznymi ścianami/ścieżkami pomaga bardziej niż jednolita przestrzeń. Odsuń kursor i wybierz obszar z niewielką liczbą oznaczeń; dodatkowe ikony nie są automatycznie wykrywane.

## Trasy waypointów

Sekcja **4. Trasa** nagrywa waypointy i prowadzi po nich w trybie podglądu. Panel liczy trasę i pokazuje kierunek następnego kroku; postać prowadzisz sam.

Waypointy żyją w przeglądarce i w pliku JSON, który sam wczytujesz i pobierasz. Serwer nie zapisuje tras na dysku i nie pamięta sesji. Robocza trasa jest przechowywana w `localStorage`, żeby odświeżenie karty nie skasowało nagrywania; na dysk trafia dopiero po kliknięciu **Pobierz JSON**.

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

`type` przyjmuje `walk`, `rope`, `ladder`, `stairs`, `hole` i `shovel`; brak pola oznacza `walk`, więc ręcznie pisany plik może zawierać samo XYZ. Limity: `x` i `y` w zakresie 0–65535, `z` 0–15, `label` do 64 znaków, do 1000 punktów na trasę. Plik z inną wartością `version` jest odrzucany, a komunikat wskazuje numer wadliwego punktu.

### Nagrywanie

**Dodaj waypoint** zapisuje aktualne XYZ; przycisk jest aktywny tylko wtedy, gdy śledzenie podało pozycję w ciągu ostatniej sekundy. **Nagrywaj trasę** dopisuje punkt co zadaną liczbę kratek — mierzoną po przekątnej jak w grze, więc dziesięć kroków na ukos to dziesięć kratek, nie dwadzieścia. Podczas nagrywania focus ma klient gry, więc tryb automatyczny jest jedynym wygodnym sposobem zapisania długiej trasy.

Zmiana piętra zawsze zapisuje **parę** punktów: kratkę sprzed przejścia z typem akcji oraz pierwszą kratkę po nim jako `walk`. Tracker rozpoznaje przejście dopiero na nowym piętrze, a trasa potrzebuje miejsca, w którym użyto liny — stąd para. Typ jest zgadywany: ruch w górę bez zmiany XY to `rope`, w dół bez zmiany XY to `hole`, a przesunięcie o kratkę to `stairs`. Minimapa nie koduje rodzaju przejścia, więc lina, drabina i schody wyglądają identycznie — popraw typ na liście. Przy linie i dziurze punkt wychodzi dokładnie, przy schodach bywa o kratkę obok.

Jeżeli obraz pasuje równie dobrze na starym piętrze, przejście może w ogóle nie zostać wykryte. Wtedy zmień Z ręcznie w selektorze; recorder zapisze parę punktów w momencie tej zmiany.

### Podążanie

**Podążaj za trasą** po każdym potwierdzonym odczycie sprawdza bieżący waypoint:

- Waypoint z typem innym niż `walk`, na którym już stoisz — panel pokazuje instrukcję akcji („Użyj liny") i czeka. Taki punkt **nie jest zaliczany przez samo dojście**: uznaje się go za wykonany dopiero wtedy, gdy tracker potwierdzi zmianę piętra. Bez tego para punktów nagrana przy linie zostałaby przejechana jako zwykłe chodzenie i instrukcja przepadłaby. Gdy przejście zdarzy się między dwoma odczytami i panel nigdy nie zobaczy Cię stojącego na tym punkcie, dowodem jest stanięcie na piętrze kolejnego waypointa — punkt również zostaje zaliczony.
- Waypoint na innym piętrze niż pozycja — panel pokazuje instrukcję wynikającą z typu („Użyj liny → piętro 6") i czeka na potwierdzenie nowego Z. Trasa nie jest liczona, bo przejścia się nie przechodzi krokami.
- Odległość nie większa niż tolerancja (domyślnie 1 kratka, także po skosie; 0 oznacza dokładnie tę samą kratkę) — punkt zaliczony, panel przechodzi do następnego. Po ostatnim pokazuje „Trasa ukończona" albo wraca na początek, jeśli włączysz **Zapętl trasę**.
- W pozostałych przypadkach panel prosi serwer o ścieżkę, nie częściej niż raz na 500 ms i nigdy dwa żądania naraz, po czym pokazuje kierunek następnego kroku, liczbę kratek do celu i rysuje trasę na podglądzie mapy referencyjnej. Podgląd odświeża się najwyżej raz na sekundę, więc nakładka jest kotwiczona do pozycji, dla której ten obraz powstał — inaczej rozjeżdżałaby się z mapą pod spodem.

Nieudane wyliczenie trasy — zerwane połączenie, timeout, waypoint chwilowo poza wczytanym obszarem — nie jest wyrokiem: panel pokazuje powód i ponawia próbę, gdy przejdziesz na inną kratkę albo po kilku sekundach. Zmiana tolerancji i zapętlenia działa od następnego odczytu i **nie cofa trasy** do pierwszego punktu; robi to dopiero wyłączenie i włączenie podążania.

Zejście z zaplanowanej ścieżki, zmiana bieżącego waypointa i edycja listy unieważniają trasę i wymuszają przeliczenie. Odpowiedź, która dotrze po zmianie celu, jest odrzucana — inaczej ścieżka policzona do poprzedniego waypointa kierowałaby w złą stronę. Żądanie trasy biegnie obok pętli śledzenia i nigdy jej nie opóźnia.

## Nauczone blokady

Trasy liczy A* na kaflach `Minimap_WaypointCost`, a te znają teren i ściany budynków — nie znają mebli. Lada w sklepie, stół i zamknięte drzwi stoją na kratkach opisanych jako w pełni przechodnie, więc trasa prowadzi wprost przez nie: postać dostaje kolejne klawisze, nie rusza się, a przeliczenie trasy zwraca dokładnie tę samą drogę. Wykonawca uczy się takich miejsc sam, z nieudanych kroków.

Krok prosty, po którym trzy kolejne świeże klatki pokazują postać wciąż na kratce startowej, tworzy **blokadę tymczasową** na 60 sekund. Drugi taki epizod — liczony dopiero po wygaśnięciu pierwszego, nie z ponowionej próby w ramach tego samego zdarzenia — awansuje kratkę na **blokadę trwałą**, zapisywaną w pliku wskazanym przez `-blocks` (domyślnie `blocks.json` w katalogu uruchomienia). Katalog map nie jest tu domyślną lokalizacją celowo: to pobrana paczka danych i nasze wpisy nie powinny się z nią mieszać ani ginąć przy jej podmianie.

Blokada tymczasowa **nie jest ścianą** — podnosi koszt kratki o 500. Gracz stojący w jednokratkowych drzwiach nie odcina wtedy trasy: jeśli objazd istnieje, zostanie wybrany, a jeśli nie — trasa nadal prowadzi tamtędy, bot czeka i próbuje ponownie, zamiast ogłosić brak drogi i stanąć. Dopiero blokada trwała czyni kratkę nieprzechodnią, tak samo jak 255 w danych mapy.

Wykonawca nie uczy się ze wszystkiego. Nieudany **skos** blokuje samo przejście na 20 sekund, nigdy kratkę: skos zawodzi także na zamkniętym rogu, gdzie oba kafle ortogonalne są zablokowane, a kratka docelowa bywa wtedy zupełnie pusta — nauka z takich prób stopniowo wycinałaby z mapy przechodnie skrzyżowania. Zmiana piętra nie uczy niczego, inaczej schody dostałyby blokadę: wejście na nie zmienia Z, więc kratka startowa i docelowa mają te same X i Y i krok wygląda na nieudany. Odmowa drivera, błąd połączenia i utrata pozycji też nie uczą — to problemy sterowania, nie mapy. Wejście na kratkę do 600 ms po upływie timeoutu odwołuje naukę: to był lag albo paraliż.

Limit czasu na krok wynosi 1800 ms. Czas przejścia kratki w Tibii zależy od prędkości postaci i kosztu terenu; krok na błocie albo pod paraliżem trwa dłużej niż sekundę, a timeout krótszy od samego kroku zamieniałby zwykły powolny ruch w fałszywe blokady.

Blokada jest odwoływalna. Postać stojąca na kratce kasuje jej wpis, także trwały — obecność jest mocniejszym dowodem niż jakakolwiek nauczona hipoteza. Sprawdza to każde żądanie trasy, bez osobnego zapytania.

### Podgląd przechodności

Sekcja **6. Podgląd przechodności** rysuje okno 65×65 kratek wokół postaci. Ciemna zieleń to teren przejezdny, czerwień — nieprzechodni w danych mapy, grafit — brak danych (nie ma kafla PNG; to nie to samo co ściana), żółć — blokada nauczona tymczasowa, fiolet — trwała. Kliknięcie kratki z nauczoną blokadą usuwa ją; kratki opisanej przez dane mapy nie da się w ten sposób ruszyć.

Okno odświeża się po zmianie kratki postaci albo co pół sekundy i nigdy nie ma dwóch żądań naraz. Endpoint nie korzysta z zamka pętli `/api/locate` i ma własny cache kafli — planer trasy pyta o prostokąt rozpięty na całej trasie, podgląd o małe okno wokół postaci, a jeden wspólny cache kazałby im wypierać się nawzajem przy każdym odczycie. Podgląd działa niezależnie od podążania za trasą; przydaje się właśnie wtedy, gdy żadna trasa nie jest uruchomiona.

To także narzędzie diagnostyczne: lada, przez którą postać nie przejdzie, a która świeci na zielono, jest dowodem, że dane mapy jej nie znają.

## Sterowanie

Flaga `-input` wybiera tryb: `off` (domyślny — każda trasa `/api/arm`, `/api/input` itd. odpowiada 503, panel działa wyłącznie jako podgląd), `dry` (emiter zapamiętuje zdarzenia w pamięci i nic nie wysyła do systemu — cały przepływ da się przećwiczyć bez ryzyka) albo `system` (prawdziwe zdarzenia klawiatury i myszy). `-input system` ma emiter tylko na macOS (CoreGraphics przez `purego`) i Windows (`user32.dll`/`SendInput`); na Linuksie i innych platformach nie ma jeszcze emitera systemowego, więc start z `-input system` tam kończy się błędem — dostępne pozostają `off` i `dry`.

### Uzbrajanie i rozbrajanie

Kliknięcie **Uzbrój** w panelu (sekcja **5. Sterowanie**) nie uzbraja od razu — uruchamia **5-sekundowe odliczanie**, widoczne w `#input-status`. Dopiero po jego upływie panel wysyła `POST /api/arm`, a Go zapamiętuje aktywne w tej właśnie chwili okno (PID i identyfikator procesu — bundle ID na macOS, ścieżka pliku na Windows) jako jedyny cel, do którego wolno coś wysłać. **W tym oknie przełącz się na klienta gry** — panel nie rozpoznaje, które okno to Tibia, tylko zapamiętuje to, co ma focus w chwili wysłania żądania, a bez odliczenia tym oknem byłaby zawsze przeglądarka, bo to jej przycisk został właśnie kliknięty. Drugie kliknięcie **Uzbrój** w trakcie odliczania je anuluje, bez wysyłania czegokolwiek.

Każde zdarzenie sprawdza focus tuż przed wysłaniem, więc utrata focusu przez zapamiętany proces (np. alt-tab) rozbraja wykonawcę — to podstawowy, ręczny kill-switch. Wykonawca rozbraja się też sam, gdy panel przestanie odpowiadać na heartbeat dłużej niż 750 ms (np. zamknięta karta) — działa to niezależnie od alt-taba.

### Świeżość obserwacji

Każdy krok niesie wiek pozycji, na której się opiera; wykonawca odrzuca krok starszy niż `-stale-ms` (domyślnie **400 ms**) komunikatem „pozycja starsza niż … ms" — to zabezpieczenie ważniejsze niż heartbeat, bo nie pozwala chodzić na podstawie nieaktualnego obrazu (np. z zakładki throttlowanej w tle). Jeśli na danym sprzęcie krok po kroku wraca sama ta odmowa, sprawdź w panelu telemetrię **Cały odczyt** — to czas przechwycenia klatki, dopasowania i odpowiedzi razem; gdy regularnie przekracza próg, podnieś go: `go run . -input system -stale-ms 600`. Zbyt wysoki próg to świadomy kompromis (starsza pozycja jako podstawa kroku), nie błąd konfiguracji.

`-stale-ms` przyjmuje wyłącznie **100–600**; poza tym zakresem program kończy się błędem przy starcie zamiast po cichu rozstroić bramkę. Dolna granica to najszybszy takt śledzenia (10 Hz = 100 ms) — poniżej niej żaden odczyt nie miałby szans zmieścić się w budżecie. Górna zostaje wyraźnie poniżej limitu heartbeatu (750 ms): przy wartości bliskiej temu progowi wykonawca i tak rozbroiłby się z powodu martwego pulsu, zanim obserwacja zdążyłaby aż tak się zestarzeć, więc bramka świeżości przestałaby cokolwiek znaczyć.

### macOS: zgoda Accessibility

`-input system` na macOS wymaga zgody **Accessibility** dla procesu uruchamiającego `minimap-lab`: Ustawienia → Prywatność i ochrona → Dostępność. Bez niej uzbrojenie zakończy się błędem — sprawdzane jest to wprost przy uzbrajaniu, a nie dopiero przy pierwszym kroku, bo inaczej `CGEventPost` po cichu nic nie robi i bot tylko wygląda na zawieszony. Po nadaniu zgody zwykle trzeba **zrestartować program** — macOS nie zawsze honoruje uprawnienie przyznane już działającemu procesowi. Zgoda na **nagrywanie ekranu**, którą ma przeglądarka (do udostępniania obrazu), jest osobnym uprawnieniem i **jej nie zastępuje**.

### Zależności

Jedyna zewnętrzna zależność w `go.mod` to `github.com/ebitengine/purego`, przypięta do `v0.9.0`. Używa jej wyłącznie plik emitera macOS (`input_darwin.go`, build tag `darwin`) do wywołań CoreGraphics/AppKit. Emiter Windows (`input_windows.go`) korzysta jedynie z `syscall` i `user32.dll`/`kernel32.dll` ze standardowej biblioteki — zero zależności. CGO jest wyłączone wszędzie, na wszystkich trzech platformach (patrz buildy krzyżowe niżej).

### Cały ekran, jeden monitor

Kliknięcia akcji (lina, drabina, dziura, łopata) celują we współrzędne kratki postaci, wskazane przy kalibracji jako **ułamek udostępnionego obrazu** (0–1 w obu osiach). Każdy emiter przelicza to inaczej: macOS mnoży współrzędne przez rozmiar głównego ekranu w punktach systemowych (`CGDisplayBounds`), Windows mapuje je na stałą skalę 0–65535 rozciągniętą na cały wirtualny pulpit (`MOUSEEVENTF_ABSOLUTE | MOUSEEVENTF_VIRTUALDESK`) i w ogóle nie odpytuje o rozdzielczość. Oba przeliczenia trafiają we właściwe miejsce tylko wtedy, gdy źródłem obrazu jest **cały ekran**, a nie pojedyncze okno ani karta przeglądarki, i tylko na **jednym monitorze** — inne źródło albo drugi monitor przesuwają klik w losowe miejsce.

### Znane ograniczenia

- Sprawdzenie focusu i wysłanie zdarzenia **nie są atomowe**: alt-tab może zdarzyć się dokładnie pomiędzy nimi i pojedynczy klawisz trafi wtedy do innego okna. Ryzyko jest mocno ograniczone, ale nie da się go wyeliminować tymi API.
- Ruch myszy wykonany ręcznie przez człowieka **nie przerywa** trwającej sekwencji tap→klik — klik celuje w skalibrowaną kratkę bezwzględnymi współrzędnymi ekranu, niezależnie od tego, gdzie akurat stoi kursor. Jedyną ochroną przed realnym zagrożeniem — zmianą aktywnego okna w trakcie 120 ms przerwy między tapnięciem hotkeya a kliknięciem — jest ponowne sprawdzenie focusu tuż przed samym kliknięciem.
- Przeglądarka **throttluje kartę w tle**; brak świeżych klatek z udostępnionego ekranu zatrzymuje ruch — to zachowanie zamierzone (bez świeżej pozycji nie ma dowodu, że krok się wykonał), nie błąd.
- **Nie ma gwarancji, że klient gry przyjmie zdarzenia syntetyczne.** Na Windows kod skanowania wysyłany przez `SendInput` nie czyni ze zdarzenia sprzętowego Raw Inputu; jeśli klient je odrzuci, jedyną drogą naprzód są `PostMessage` do okna albo sterownik wejścia — oba poza zakresem tego projektu. Rozstrzyga to wyłącznie test na żywym kliencie, opisany niżej.

### Dwa checkboxy

**Chodź automatycznie** włącza rzeczywiste wysyłanie kroków: dopóki jest odznaczony (albo wykonawca nie jest uzbrojony), panel tylko pokazuje kierunek i liczy trasę, dokładnie jak przed tą funkcją. **Wykonuj akcje pięter** dotyczy wyłącznie akcji na przedmiotach — liny, drabiny, dziury i łopaty: gdy jest odznaczony, wykonawca zatrzymuje się przed takim waypointem i czeka, mimo że chodzenie jest włączone. Nie dotyczy to **schodów** (`stairs`) — schody pokonuje się zwykłym krokiem w ich stronę, bez żadnego hotkeya, więc są wykonywane zawsze, gdy tylko włączone jest chodzenie automatyczne, niezależnie od stanu tego checkboxa.

Oba checkboxy można zaznaczyć **przed uzbrojeniem**, także w trakcie odliczania — zaznaczenie samo w sobie nic nie wysyła (blokuje to `!inputClient.armed`), więc nie trzeba wracać do przeglądarki po uzbrojeniu, żeby dopiero wtedy je zaznaczyć: to właśnie kradłoby focus grze i rozbrajało wykonawcę na najbliższym kroku. Prawdziwe rozbrojenie — z panelu albo z Go (utrata focusu, martwy heartbeat) — zawsze odznacza **Chodź automatycznie**, więc uzbrojenie nigdy nie wznawia chodzenia po cichu.

### Klawisze akcji pięter

Wykonawca nie zna żadnego hotkeya, dopóki nie zostanie skonfigurowany z panelu — bez tego każda akcja piętra (lina, drabina, dziura, łopata) kończy się odmową „brak hotkeya dla akcji …”, a **Wykonuj akcje pięter** wygląda na włączony, ale nic nie robi. Cztery pola tekstowe w sekcji **5. Sterowanie** przyjmują nazwę klawisza dla każdego typu (np. `f7`); zaakceptowane nazwy to `f1`–`f12`, `up`/`down`/`left`/`right`, `numpad1`–`numpad9` (bez `numpad5`) oraz litery `a`–`z` i cyfry `0`–`9` — te same, których używają emitery macOS i Windows. Pusty klawisz zostawia daną akcję odrzucaną. Checkbox **Klawisz działa na własnej kratce (bez klikania po nim)** odpowiada temu, czy hotkey sam kończy akcję (np. lina użyta na sobie) czy wymaga kliknięcia we wskazaną wcześniej kratkę postaci (**Wskaż kratkę postaci**) — to drugie dodaje krótkie kliknięcie ~120 ms po tapnięciu klawisza.

Konfiguracja jest zapisywana w `localStorage` (przeżywa odświeżenie karty) i wysyłana do wykonawcy przy każdym uzbrojeniu oraz przy każdej zmianie pola, o ile sesja jest aktywna — zmiana klawisza z rozbrojonym wykonawcą tylko zapisuje wartość lokalnie, wysyła ją dopiero kolejne uzbrojenie. Schody (`stairs`) nie mają tu żadnego pola: pokonuje się je krokiem, nie hotkeyem.

### Klawisze kierunków

Chodzenie **nie jest** przywiązane do numpada na sztywno — mapowanie ośmiu kierunków (`N`, `NE`, `E`, `SE`, `S`, `SW`, `W`, `NW`) na klawisze jest konfigurowalne z panelu, bo nie każdy ma ruch przypisany do numpada. Osiem pól w sekcji **5. Sterowanie**, ułożonych w siatkę 3×3 z pustym środkiem (jak róża wiatrów), domyślnie zawiera układ numpada (`N`→`numpad8`, `NE`→`numpad9` itd.) — bez żadnej konfiguracji chodzenie działa dokładnie tak jak wcześniej. Przyjmowane nazwy klawiszy są te same, co dla akcji pięter, plus litery i cyfry.

Dwa przyciski wypełniają wszystkie osiem pól naraz: **Numpad** (wbudowany domyślny układ) i **WSAD** (`w`/`s`/`a`/`d` na głównych kierunkach, `q`/`e`/`z`/`c` na skosach dookoła nich). To tylko punkt startowy — każde pole można potem dowolnie zmienić, a to, co w nim zostanie, jest zapisywane i wysyłane; przyciski przechodzą przez dokładnie tę samą ścieżkę zapisu i wysyłki co ręczna edycja pojedynczego pola, żadnych specjalnych przypadków po stronie Go.

Skos to pojedynczy klawisz, tak jak dotąd — **wykonawca nigdy nie rozkłada skosu na dwa kroki proste**, nawet jeśli klient nie ma osobnego klawisza skosu (typowe dla WSAD). Puste pole nie oznacza „pomiń” — to świadoma odmowa: próba chodzenia w tym kierunku kończy się komunikatem „brak skonfigurowanego klawisza dla kierunku …”, widocznym w panelu, zamiast cichego braku ruchu.

### Test w trybie dry, bez gry

```sh
cd minimap-lab && go run . -input dry
```

W panelu: uzbrój, włącz **Chodź automatycznie** na nagranej trasie i sprawdź, że sekwencja kierunków zgadza się z trasą pokazywaną w podglądzie. Żadne zdarzenie nie trafia do systemu — `DryEmitter` tylko je zapamiętuje.

### Test na żywym kliencie

```sh
cd minimap-lab && go run . -input system
```

Przed punktem 1 sprawdź, że osiem pól **Klawisze kierunków** (wyżej) odpowiada faktycznemu ruchowi w kliencie gry — domyślny układ to numpad; klient z ruchem na literach potrzebuje np. przycisku **WSAD** i ewentualnej poprawki skosów. Przed punktem 6 wpisz w panelu hotkey przypisany linie w kliencie gry (sekcja **Klawisze akcji pięter** wyżej) — bez tego akcja zawsze kończy się odmową.

Zaznacz **Chodź automatycznie** (i **Wykonuj akcje pięter**, jeśli test tego wymaga) **przed** kliknięciem „Uzbrój" — patrz **Dwa checkboxy** wyżej. Zaznaczenie ich dopiero po uzbrojeniu wymaga kliknięcia w przeglądarce, co kradnie focus grze i rozbraja wykonawcę na najbliższym kroku.

W tej kolejności:

1. Cztery kierunki po jednym kroku — postać rusza we właściwą stronę.
2. Krok po skosie.
3. Krok w ścianę — wykonawca zatrzymuje się po dwóch próbach, nie zapętla.
4. Alt-tab w trakcie chodzenia — panel pokazuje rozbrojenie, klawisze ustają.
5. Trasa 20 kratek bez przejść między piętrami.
6. Waypoint z liną.
7. Waypoint ze schodami — postać wchodzi krokiem, panel potwierdza nowe Z.

Zapisz wynik każdego punktu w opisie commita — to jedyna weryfikacja emiterów, których nie da się objąć `go test`.

### Testy

```sh
go test ./...
node --test ui_test.cjs tracker_test.cjs route_test.cjs recorder_test.cjs follower_test.cjs executor_test.cjs input_client_test.cjs blocks_test.cjs
```

## Jak działa

- Ładuje `Minimap_Color_X_Y_Z.png` (256×256, jedna kratka na piksel) i scala piętro z zachowaniem początku współrzędnych. Wycinek może przechodzić przez granice kafli. Brakujące kafle są przezroczyste i nie mogą być uznane za zgodny teren.
- Próbkuje kratki względem wskazanego znacznika, pomijając maskę i przezroczystość. Zachowuje czarne ściany, które opisują kształt jaskiń. Używa do 1024 próbek z pierwszeństwem granic kolorów. Wymaga co najmniej 64 nieczarnych kratek i zróżnicowania kolorów.
- Szuka najmniejszego średniego błędu bezwzględnego RGB. Wynik `1 − błąd/255` to podobieństwo kolorów **próbek**, nie prawdopodobieństwo poprawnej pozycji. Domyślny próg: `0.85`, uwzględniający różnice kolorów w przechwytywaniu; demo używa `0.94`. Po wyjściu z demo przywracane są ustawienia rzeczywistego źródła.
- Pokazuje najlepszego kandydata i wynik także poniżej progu, ale nie uznaje go wtedy za pozycję postaci.
- Drugie wyszukiwanie sprawdza, czy inna kratka pasuje z wynikiem bliższym niż `0.015`. Jeśli tak, zwraca `found: false` i `position: null`.
- Współrzędne dotyczą znacznika postaci, nie lewego górnego rogu wycinka. Piętro startowe jest podane ręcznie; późniejsze przejścia Z ±1 mogą być potwierdzane lokalnie z obrazu.

Pełne wyszukiwanie ma limit 45 s; przy wolnym działaniu podaj katalog z mapami mniejszego obszaru. Limit atlasu: 32 mln pikseli. Lokalne śledzenie zakłada ciągłość ruchu w wybranym obszarze; nie potwierdza unikalności miejsca na całej mapie. Powtarzalne otoczenie, zmiany palety, skala ułamkowa i ikony mogą powodować brak dopasowania lub błędnego kandydata; sprawdź odczyty na własnych zrzutach.

## HTTP API

`POST /api/locate`, multipart:

- `image`: **już wycięta** minimapa PNG/JPEG, bok 8–1024 px, formularz do 8 MB.
- `options`: JSON, np. `{"floor":7,"demo":false,"zoom":0,"marker_x":52,"marker_y":57,"mask_radius":5,"min_score":0.85,"min_gap":0.015}`. `zoom:0` oznacza automatyczną kalibrację 1–4. Pole `zoom` w odpowiedzi wskazuje użytą skalę; `scale_scores` pokazuje sprawdzone skale.
- Kolejny, lokalny odczyt: ustaw wykrytą skalę `zoom:1` i dodaj `"near":{"x":32958,"y":32077,"z":7},"radius":5,"no_preview":true`. Wymagane Z zgodne z `near.z` lub różniące się o 1, znana skala i promień ruchu 1–64. Odpowiedź zawiera `mode`, `search_positions`, `match_ms`. Brak `near` oznacza pełne wyszukiwanie.
- Automatyczne przejścia: dodaj `"adjacent_floors":true,"floor_radius":8`. Gdy `floor == near.z` i bieżące dopasowanie zawodzi, serwer sprawdza sąsiednie poziomy. Gdy `floor` różni się od `near.z` o 1, sprawdza tylko wskazane piętro wokół poprzedniego XY. `floor_radius:0` oznacza domyślne 8. Odpowiedź dodaje `searched_floors`, `unavailable_floors` i `floor_changed`.

`POST /api/path`, czysty JSON (bez obrazu i bez multipart):

```json
{"from":{"x":32786,"y":32061,"z":7},"to":{"x":32786,"y":32121,"z":7},"margin":64}
```

Odpowiedź: `{"found":true,"status":"ok","steps":[[32786,32061],[32786,32062]],"tiles":63,"cost":102.6,"reason":"","elapsed_ms":5.2,"overlay_revision":12}`. `overlay_revision` rośnie z każdą zmianą warstwy nauczonych blokad, więc panel odróżni trasę policzoną przed swoją ostatnią obserwacją od tej po niej. Pole `status` przyjmuje `ok`, `blocked_start`, `blocked_goal`, `no_route`, `different_floor`, `limit` i `cancelled`; `reason` opisuje to samo słowami. Żadna z tych sytuacji nie jest błędem HTTP.

Kod 400 zwracany jest wyłącznie przy niepoprawnym wejściu: brakujące lub niepełne `from`/`to` (każde wymaga `x`, `y` i `z`), współrzędne poza 0–65535, piętro poza 0–15, `margin` poza 0–256, uszkodzony JSON, treść z doklejonym drugim dokumentem oraz obszar wyszukiwania przekraczający 4 mln kratek — same współrzędne nie są ograniczeniem, dwa poprawne punkty na przeciwległych krańcach mapy alokowałyby gigabajty. Żądanie porzucone przez przeglądarkę kończy się kodem 408 i nie wczytuje kafli.

Przed wyszukiwaniem serwer robi jedno zdjęcie warstwy nauczonych blokad dla całego obszaru — A* zakłada, że koszt zamkniętego wierzchołka się nie zmienia, więc graf nie może się przesunąć w trakcie. Kratka `from` jest przy okazji kasowana z tej warstwy: skoro postać tam stoi, wpis jest po prostu błędny. Bazowa siatka kosztów nigdy nie jest modyfikowana, bo współdzieli tablicę pikseli z cache'em piętra.

A* działa na kaflach `Minimap_WaypointCost_X_Y_Z.png` z katalogu podanego przez `-maps`, w prostokącie rozpiętym na obu punktach i powiększonym o `margin` (0 oznacza domyślne 64, maksimum 256), z limitem 5 s. Trasa wymagająca objazdu poza tym prostokątem zwróci `no_route` — zwiększ margines albo dodaj waypoint pośredni. Ruch po przekątnej między dwiema ścianami jest niemożliwy, tak jak w grze. Koszt kratki pochodzi wprost z indeksu palety: 255 to teren nieprzechodni, niższe wartości to koszt chodzenia, gdzie 100 odpowiada jednemu krokowi. W danych występują kratki tańsze niż 100, więc oszacowanie odległości jest skalowane najtańszą kratką w obszarze — inaczej A* przestaje zwracać najtańszą trasę. Brakujące kafle są nieprzechodnie. Zapytania o trasę mają własny zamek i własny cache, więc nie odbierają przepustowości pętli `/api/locate`.

`POST /api/blocks/observe`, czysty JSON: `{"from":{"x":…,"y":…,"z":…},"to":{…},"outcome":"no_motion","still_frames":3,"last_frame_age_ms":140}`. `outcome` przyjmuje `no_motion` i `entered`. Decyzję podejmuje serwer i zwraca ją jako `{"result":"temp","reason":"Pierwszy epizod; blokada tymczasowa."}`; `result` to `ignored`, `temp`, `promoted` albo `cleared`, a `reason` zawsze tłumaczy dlaczego — odrzucona obserwacja nie może wyglądać jak zgubione żądanie.

`GET /api/blocks?x=&y=&z=&r=` zwraca nauczone blokady w oknie (promień 1–64) jako listę `{"x","y","z","kind","episodes","expires_in_ms"}`. `DELETE /api/blocks` z ciałem `{"x":…,"y":…,"z":…}` kasuje jedną, także trwałą, i odpowiada `{"cleared":true}`.

`GET /api/grid?x=&y=&z=&r=` zwraca `application/octet-stream`: po jednym bajcie na kratkę, wierszami od lewego górnego rogu okna, `(2r+1)²` bajtów. Bity: `1` teren nieprzechodni w danych mapy, `2` brak danych, `4` blokada tymczasowa, `8` trwała. Nagłówki `X-Grid-Origin` i `X-Grid-Revision` mówią, gdzie zaczyna się okno i w jakiej rewizji warstwy powstało. Wszystkie trzy trasy blokad odpowiadają kodem 503, gdy magazyn nie jest włączony.

`GET /api/demo` zwraca demonstracyjną minimapę PNG; `GET /api/info` listę dostępnych pięter. W `options` użyj `demo:true`, aby dopasować obraz demonstracyjny do atlasu syntetycznego.

Warstwa blokad testowana jest osobno, na wstrzykniętym zegarze: czas życia wpisu, liczenie epizodów, awans na trwały, zapominanie historii, odrzucanie obserwacji bez dowodu, skos blokujący przejście zamiast kratki, odwołanie wpisu przez wejście na kratkę, atomowy zapis pliku i odrzucenie obcej wersji. Po stronie tras sprawdzane są objazd wokół nauczonej blokady, przepuszczenie trasy przez blokadę tymczasową tam, gdzie objazdu nie ma, zakaz skosu obok nauczonego mebla oraz to, że bazowa siatka kosztów nie zmienia się przy równoległych zapytaniach.

Testy obejmują znane współrzędne, przesunięcie, skalę i maskę, brak dopasowania, niejednoznaczność, brak map, granicę kafli i żądania HTTP. Wyszukiwanie trasy sprawdzane jest osobno: odczyt kosztów z palety (konwersja do skali szarości zamieniłaby blokadę 255 na teren przechodni), brakujące kafle, granica kafli, obejście ściany, zakaz przecinania zamkniętego rogu, wybór tańszego terenu, zablokowany start i cel, limit kroków, anulowanie oraz walidacja `/api/path` wraz z dowodem, że zapytanie o trasę nie czeka na zamek śledzenia. Optymalność sprawdzana jest przez porównanie z Dijkstrą na dwustu losowych siatkach mieszanego terenu. Testy nie wymagają gry.

Dodatkowy test na mapach obecnych w repozytorium: `MINIMAP_REAL_MAP_TEST=1 go test -run TestLocalMapIntegration -v`. Porównuje wycinek atlasu ze znaną pozycją `(32369,32241,7)`; nadal nie jest to test zrzutu z klienta gry.

Test prawdziwego wycinka z przechwytywania: `MINIMAP_REAL_MAP_TEST=1 go test -run TestActualCaptureAgainstWholeFloor -v`. Dla zapisanej minimapy z Venore znajduje wskazany punkt `(32958,32077,7)` przy skali 1 i wyniku około 86,5%. Zwykłe `go test ./...` sprawdza też ten obraz na mniejszym atlasie oraz jego wariant powiększony 2×. `node --test ui_test.cjs` sprawdza przepływ demo → screenshot/udostępnianie, przywracanie kalibracji oraz wczytanie trasy, nagrywanie i podążanie; nie wymaga zainstalowanej przeglądarki. `route_test.cjs`, `recorder_test.cjs` i `follower_test.cjs` obejmują format pliku, nagrywanie z parowaniem punktów przejścia oraz stan podążania.

Testy trasy na mapach z repozytorium: `MINIMAP_REAL_MAP_TEST=1 go test -run TestRealMap -v`. Prowadzą 63-kratkową trasę przez największy spójny obszar powierzchni Venore i sprawdzają, że każdy krok stoi na terenie przechodnim, sąsiaduje z poprzednim i nie przecina zamkniętego rogu. Sprawdzają też, że ściana jako waypoint zwraca `blocked_goal`, a teren odgrodzony murem — `no_route`.

Test lokalnego przejścia na rzeczywistych mapach: `MINIMAP_REAL_MAP_TEST=1 go test -run TestFloorTransitionRealAtlas -v`. Używa zapisanego wycinka oraz zasymulowanej ostatniej pozycji na Z=8; szuka właściwego Z=7 w pobliżu XY. Nie wymaga nagrania prawdziwego przejścia. Przy pierwszej próbie z wczytaniem kafli zmierzono około 36 ms na tym Macu. `go test -run '^$' -bench BenchmarkAdjacentFloorCold -benchtime=2s` mierzy osobno wyszukiwanie i wczytywanie małego atlasu z fixture.

## Diagnostyka

Ostatni rzeczywisty wycinek jest zapisywany lokalnie w `.debug/last-input.png`, jego ustawienia w `.debug/last-options.json`, a wynik w `.debug/last-result.json`. Podczas lokalnego śledzenia zapis i logowanie są ograniczone do raz na sekundę; pełne wyszukiwania są zapisywane za każdym razem. Podgląd referencyjny również jest odświeżany najwyżej raz na sekundę podczas śledzenia. Pliki nie są udostępniane przez HTTP i są wyłączone z Gita. Pełny ekran nie jest zapisywany.
