# Sterowanie klawiaturą w minimap-lab

Data: 2026-09-06

## Cel

minimap-lab ustala `(x,y,z)` z obrazu minimapy, śledzi pozycję przy 5–10 Hz i
prowadzi po trasie waypointów, pokazując kierunek następnego kroku. Postać
prowadzi człowiek.

Ten dokument opisuje kolejny krok: panel sam wysyła klawisze ruchu do klienta
gry i wykonuje akcje zmiany piętra. Zakres to chodzenie po trasie oraz akcje
`rope`, `ladder`, `hole`, `shovel` i `stairs`. Walka, leczenie i zbieranie łupu
**nie wchodzą** w ten etap.

## Zmiana wcześniejszej decyzji

Spec z 2026-09-05 zapisał: *„Dry-run. Brak sterowania postacią, więc brak CGO,
robotgo i zależności natywnych."* Ten dokument świadomie odwraca pierwszą część
i zawęża drugą:

- **CGO nadal nie wchodzi.** To była właściwa zasada — utrzymuje prostą
  cross-kompilację z macOS na Windows bez MinGW i bez SDK.
- **Zero zewnętrznych zależności przestaje obowiązywać na macOS.** Wysłanie
  zdarzenia klawiatury wymaga CoreGraphics, a stdlib Go nie ma `dlopen`.
  Wchodzi `github.com/ebitengine/purego`, przypięty do konkretnej wersji.
  Na Windows wystarcza `syscall.NewLazyDLL` — tam zależności nie ma żadnej.
- **robotgo odrzucone.** Potrzebujemy taplowania ośmiu klawiszy i jednego
  kliknięcia, czyli około 150 linii kodu. Biblioteka kosztowałaby toolchain C
  na obu platformach, a nie dałaby kontroli nad bramką focusu ani sekwencjami.

README minimap-lab musi zostać poprawione — dzisiaj twierdzi „bez zewnętrznych
bibliotek" i „sterowanie postacią nie jest zaimplementowane".

## Decyzje

1. **Wykonawca mieszka w module minimap-lab**, w plikach z build tagami. Osobny
   proces izolowałby kod natywny, ale za cenę IPC i cyklu życia drugiej binarki
   — nieuzasadnione przy tym zakresie.
2. **RouteFollower zostaje w JS.** Jest napisany, przetestowany (`follower_test.cjs`)
   i sprzężony z pętlą klatek, która żyje w przeglądarce. Go dostaje rolę
   sterownika: przyjmuje pojedynczy zamiar, sprawdza warunki, emituje zdarzenie.
3. **Wykonawca jest lock-step.** Jeden tap, potem oczekiwanie na dowód z nowej
   klatki. Nigdy kolejka kroków ani wysyłanie w ciemno na podstawie predykcji.
4. **Ruch wolno emitować wyłącznie przy aktywnym oknie gry.** Alt-tab rozbraja
   wykonawcę i jest podstawowym kill-switchem.

## Architektura

### Nowe pliki

| Plik | Rola |
|---|---|
| `input.go` | Interfejs `Emitter`, rejestr wciśniętych klawiszy, awaryjne zwolnienie, mapowanie kierunek→klawisz |
| `input_windows.go` | `SendInput` przez `syscall.NewLazyDLL("user32.dll")`, focus przez `GetForegroundWindow` |
| `input_darwin.go` | CoreGraphics przez `purego`, focus przez NSWorkspace |
| `input_other.go` | Stub zwracający `ErrUnsupported`; Linux nadal się kompiluje i testuje |
| `driver.go` | Stan wykonawcy: uzbrojenie, sesja, `seq`, limit, heartbeat, jedna akcja naraz |
| `inputapi.go` | `POST /api/arm`, `POST /api/disarm`, `POST /api/input`, `GET /api/input/status` |
| `web/input.js` | Klient wykonawcy w panelu: heartbeat, wysyłanie zamiarów, oczekiwanie na potwierdzenie |

### Interfejs Emitter

```go
type Emitter interface {
    TapKey(code KeyCode, holdMS int) error
    Click(nx, ny float64) error      // współrzędne znormalizowane 0–1
    Focused() (Window, error)        // pid, ścieżka procesu, tytuł
    ReleaseAll() error
    Preflight() error                // uprawnienia; macOS: CGPreflightPostEventAccess, Windows: nil
}
```

`driver.go` nie zna żadnego API systemu. Testy podstawiają atrapę.

### Przepływ jednego kroku

Pętla `locate` w `app.js` zostaje nietknięta. Nowy kod wpina się za `followStep`:

```
klatka (capturedAt) → /api/locate → tracker.observe → follower.step(position)
  → {action:'walk', direction:'NE'}
  → POST /api/input {session, seq, action:'walk', direction:'NE', observation_age_ms}
  → Go: uzbrojony? focus? limit? poprzednia akcja skończona? obserwacja świeża?
  → tap klawisza → odpowiedź → panel notuje emittedAt = performance.now()
  → czeka na klatkę o capturedAt > emittedAt i pozycji równej oczekiwanej kratce
```

## Wysyłanie zdarzeń

### Windows

`SendInput` z `user32.dll`, struktura `INPUT` odwzorowana ręcznie: 40 bajtów na
amd64. Klawisze wysyłane jako **scan code** (`KEYEVENTF_SCANCODE`, `wVk = 0`),
bo klienci gier często ignorują same virtual keys. Klawisze rozszerzone
(strzałki, Numpad Enter) dostają `KEYEVENTF_EXTENDEDKEY`. Zwolnienie to ten sam
wpis z `KEYEVENTF_KEYUP`.

Zwracana przez `SendInput` liczba wstawionych zdarzeń jest sprawdzana — UIPI
blokuje wysyłanie do procesu o wyższym poziomie uprawnień i zgłasza to właśnie
tak.

**Nie ma podstaw, żeby zagwarantować działanie w aktualnym kliencie Tibii przed
testem na żywym kliencie.** Scan code nie czyni ze zdarzenia sprzętowego Raw
Inputu. Jeżeli klient odrzuci zdarzenia, alternatywy to `PostMessage` do okna
albo sterownik wejścia; obie są poza tym zakresem.

### macOS

`purego.Dlopen` na
`/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics`, rejestracja
`CGEventCreateKeyboardEvent`, `CGEventCreateMouseEvent`, `CGEventPost`,
`CGPreflightPostEventAccess`.

Pułapki ABI, które trzeba trafić dokładnie: `CGKeyCode` to `uint16`,
`CGEventFlags` to `uint64`, C `bool` to jeden bajt, `CGPoint` to para `float64`
przekazywana przez wartość. `purego` nie weryfikuje zadeklarowanych sygnatur —
błąd w paddingu nie jest błędem kompilacji, tylko losowym zachowaniem w locie.
Zdarzenia zwalniane przez `CFRelease`.

Kody klawiszy to fizyczne kody ANSI, nie ASCII i nie Windows VK.

Wymagana zgoda **Accessibility** dla procesu, który uruchamia binarkę.
Zgoda na przechwytywanie ekranu, którą ma przeglądarka, jej nie zastępuje.
Brak zgody sprawdzany przy uzbrajaniu, nie przy pierwszym kroku — inaczej
`CGEventPost` po cichu nic nie robi i wygląda to jak zawieszony bot.

### Mapowanie kierunków

Domyślnie numpad: `NW=7 N=8 NE=9 W=4 E=6 SW=1 S=2 SE=3`. Skos jednym klawiszem,
nie składany z dwóch strzałek — składanie jest zawodne i zależy od kolejności
zdarzeń. Mapowanie jest konfigurowalne, bo nie każdy ma ruch na numpadzie.

Tap to `KeyDown → 35 ms → KeyUp`. Klawisz nigdy nie jest trzymany — trzymanie
uruchamia systemowe powtarzanie i produkuje kroki, o których wykonawca nie wie.

## Wykrywanie aktywnego okna

**Windows:** `GetForegroundWindow` → `GetWindowThreadProcessId` →
`QueryFullProcessImageNameW`. Tożsamością jest PID i ścieżka procesu, nie tytuł
— stronę w przeglądarce też można nazwać „Tibia".

**macOS:** `NSWorkspace.sharedWorkspace.frontmostApplication` przez
`purego/objc`, stąd PID i bundle identifier. `CGWindowListCopyWindowInfo` nie
jest jednoznacznym odpowiednikiem focusu, a odczyt tytułów okien wymaga zgody
Screen Recording — dlatego NSWorkspace.

`osascript` odpalany w pętli jest wykluczony: fork procesu kilka razy na sekundę
kosztuje dziesiątki milisekund i obciąża CPU.

Sprawdzenie jest wykonywane bezpośrednio przed każdą emisją. Tożsamość procesu
może być cache'owana; **stan „gra ma focus" nie może** być cache'owany jako
jedyna kontrola.

### Granica gwarancji

Sprawdzenie focusu i emisja zdarzenia nie są atomowe. Alt-tab może nastąpić
pomiędzy nimi i pojedynczy klawisz trafi do innego okna. Ryzyko da się mocno
ograniczyć, wyeliminować tymi API się nie da. Trafia to do README jako znane
ograniczenie, a nie do kodu jako obietnica.

## Timing kroku

Dowodem wykonania kroku jest **wyłącznie pozycja z klatki przechwyconej po
emisji**: `tracker.anchor.at > emittedAt`. `capturedAt` już istnieje w pętli
panelu i niesie tę informację. Późna odpowiedź HTTP opisująca starszą klatkę nie
jest potwierdzeniem.

`emittedAt` notowane jest **po** otrzymaniu odpowiedzi z `/api/input`, nie przed
wysłaniem żądania — konserwatywnie, żeby żadna klatka sprzed emisji nie mogła
zostać uznana za dowód.

Panel wysyła **wiek** obserwacji (`observation_age_ms`), nie jej znacznik czasu.
`performance.now()` liczy od startu dokumentu i nie ma wspólnego zera z zegarem
Go; przeliczanie ich na siebie wprowadzałoby błąd większy niż sam próg
świeżości.

Cztery wyniki oczekiwania:

| Sytuacja | Reakcja |
|---|---|
| Pozycja równa oczekiwanej kratce | Krok zaliczony, następny |
| Pozycja bez zmian po timeoucie | Jedna powtórka; druga porażka → kratka chwilowo zablokowana, `dropPath()`, nowe `/api/path` |
| Pozycja inna niż oczekiwana | Porzucenie ścieżki i przeliczenie; zaległe komendy nie są odtwarzane |
| `found:false` | Natychmiastowe wstrzymanie ruchu |

Trzy kolejne cykle „tap → timeout → powtórka → timeout" bez zmiany kratki
kończą się STOP-em i sygnałem w panelu. Licznik zeruje każdy potwierdzony krok.

Timeout startowo 1200 ms. Później liczony z mediany zmierzonych czasów
potwierdzeń, osobno dla ruchu prostego i po skosie, powiększonej o interwał
odczytu i margines. Predykcja czasu kroku służy **tylko** do wyznaczenia
timeoutu.

## Akcje zmiany piętra

### Kalibracja obszaru gry

Minimapa nie mówi, gdzie na ekranie jest postać. Panel dostaje drugą
kalibrację, analogiczną do istniejącej kalibracji minimapy: użytkownik klika
w podglądzie kratkę postaci w obszarze gry. Wszystkie akcje celują we własną
kratkę, więc jeden punkt wystarcza.

Panel wysyła współrzędne **znormalizowane** względem udostępnionego obrazu, a Go
mnoży je przez rozmiar ekranu w punktach systemowych. Retina jest w ten sposób
załatwiona bez wiedzy o DPI po stronie JS.

Wynikają z tego dwa ograniczenia, sprawdzane przy uzbrajaniu i opisane w README:

- udostępniony musi być **cały ekran**, nie okno ani karta — tylko wtedy
  mapowanie obraz→ekran jest afiniczne,
- jeden monitor.

### Sekwencje

| Typ | Sekwencja |
|---|---|
| `rope`, `ladder`, `hole`, `shovel` | tap hotkeya przedmiotu → 120 ms → klik LPM w kratkę postaci |
| `stairs` | sam krok; schody pokonuje się chodzeniem |

Hotkey per typ pochodzi z konfiguracji panelu. Kto ma hotkey ustawiony w kliencie
jako „use on yourself", wyłącza klik — sekwencja skraca się do samego tapa.

Po sekwencji czekamy wyłącznie na potwierdzoną zmianę `z`, z timeoutem 5 s —
wjazd na linie i animacja schodów trwają wyraźnie dłużej niż krok. Jedna
powtórka całej sekwencji, potem STOP — postać stojąca w nieznanym miejscu
w trakcie akcji nie jest stanem do automatycznego ratowania.

### Dwie wymagane poprawki w follower.js

1. **Osobna `actionTolerance`, domyślnie 0.** Dzisiejsze `standingOnAction`
   używa wspólnej `tolerance` (domyślnie 1), więc uznaje kratkę obok za dojście
   do waypointa akcji. Do pokazania instrukcji człowiekowi to wystarcza, do
   użycia liny nie — lina użyta kratkę obok ropespotu nic nie robi.
2. **Blokada powtórzeń.** `step()` zwraca `transition` przy każdym odczycie,
   dopóki piętro się nie zmieni. Driver trzyma klucz trwającej akcji
   `(index, typ)`; żądanie z tym samym kluczem dostaje odpowiedź „w toku"
   i nie emituje niczego. Bez tego hotkey byłby wciskany pięć razy na sekundę.

## Bezpieczniki

- **Jawne uzbrojenie.** `POST /api/arm` zapamiętuje PID i ścieżkę procesu
  z aktywnego okna, sprawdza uprawnienia i zwraca token sesji. Start zawsze
  rozbrojony.
- **Utrata focusu rozbraja.** Brak automatycznego wznowienia po powrocie do gry
  — trzeba kliknąć ponownie.
- **Heartbeat** z panelu co 200 ms, wygaśnięcie po 750 ms.
- **Wiek obserwacji.** Ruch odrzucany, gdy pozycja pochodzi z klatki starszej niż
  400 ms. To jest ważniejsze niż heartbeat: heartbeat dowodzi, że JS żyje, a nie
  że obraz jest świeży. Panel ma pracować w karcie w tle, którą przeglądarka
  throttluje — brak świeżych klatek musi twardo zatrzymywać ruch.
- **Limit 5 tapów/s**, bez kumulacji niewykorzystanego budżetu.
- **Jedna akcja naraz.** `seq` idempotentny: powtórzone żądanie zwraca poprzedni
  wynik, nie wciska klawisza drugi raz.
- **Rejestr wciśniętych klawiszy i awaryjne `ReleaseAll`.** To jedyny świadomy
  wyjątek od zakazu emisji po utracie focusu — inaczej można zostawić klawisz
  wciśnięty na stałe.
- **STOP** przy: nieznanej pozycji, nieoczekiwanym piętrze, zmianie rozdzielczości
  źródła obrazu, błędzie API, trzech nieudanych krokach z rzędu.
- **Ruch myszy człowieka** przerywa trwającą sekwencję przedmiotową.
- **Ochrona endpointu.** Token sesji w nagłówku, kontrola `Origin`, zamknięta
  lista akcji. Adres `127.0.0.1` nie jest zabezpieczeniem: dowolna strona otwarta
  w tej samej przeglądarce może wysłać POST na localhost.

Globalny hotkey awaryjny **nie wchodzi w ten zakres**. Na Windows byłby prosty
(`GetAsyncKeyState`), na macOS wymaga event tapa z osobnymi uprawnieniami
i działającego run loopa. Skoro utrata focusu rozbraja, alt-tab jest
kill-switchem, a przycisk w panelu drugim.

## Testy

**Go, bez systemu.** `driver.go` testowany na atrapie `Emitter`: bramka focusu,
limit tapów, wygaśnięcie heartbeatu, odrzucenie starej obserwacji, idempotencja
`seq`, jedna akcja naraz, blokada powtórzonej akcji, awaryjne zwolnienie
klawiszy.

**Go, API.** `inputapi.go`: brak tokenu, zły `Origin`, żądanie przy rozbrojeniu,
nieznana akcja, walidacja zakresu współrzędnych kliknięcia.

**Tryb `-input dry`.** Emitter, który zamiast do systemu pisze do logu i zwraca
opis zdarzenia do panelu. Cały przepływ przechodzi bez gry i bez uprawnień.
To jest pierwszy uruchamiany test.

**JS.** `follower_test.cjs` rozszerzony o `actionTolerance` równe 0 oraz
o przypadek waypointa akcji z sąsiedniej kratki, który nie może być uznany za
osiągnięty.

**Ręcznie, na kliencie**, w tej kolejności: cztery kierunki po jednym kroku →
skos → krok w ścianę (ma zatrzymać, nie zapętlić) → trasa 20 kratek → lina.

Emiterów per-platforma nie da się sensownie testować automatycznie — wymagają
sesji graficznej i zgody systemu. Ta część jest sprawdzana ręcznie i tak jest
opisana.

## Znane ograniczenia

- Zdarzenia syntetyczne mogą nie zostać przyjęte przez klient gry; wiadomo to
  dopiero po teście na żywym kliencie.
- Bramka focusu nie jest atomowa względem emisji.
- Wymagane udostępnienie całego ekranu i jeden monitor.
- macOS wymaga zgody Accessibility dla procesu uruchamiającego binarkę.
- Przeglądarka throttluje karty w tle; przy zbyt rzadkich klatkach bot stoi
  zamiast chodzić — jest to zachowanie zamierzone.
