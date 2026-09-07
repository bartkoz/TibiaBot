# Leczenie

Data: 2026-09-07
Status: zaakceptowany do planowania
Gałąź: `brain-go`, po scaleniu warstwy widzenia (`6939adc`)

## Po co

Bot chodzi po trasie, uczy się blokad, zjeżdża po piętrach i — od fazy 1
projektu walki — widzi, ile potworów stoi wokół postaci oraz ile ma HP i many.
Widzi, ale nic z tym nie robi. Postać, która straci życie, ginie z pełną torbą
potionów.

Zamówienie z 2026-09-06: monitorowanie HP i many, potiony po przekroczeniu
progu procentowego, leczenie czarami z konfigurowalnych hotkeys. Projekt
odkładany dwa razy, bo najpierw mózg musiał trafić do Go, a potem powstać
czytnik pasków. Oba warunki są spełnione, więc zostaje sama lista reguł i
klawiatura.

## Decyzje podjęte przed projektowaniem

Ustalone z użytkownikiem 2026-09-06 i 2026-09-07. Nie wracamy do nich bez
wyraźnej prośby.

1. **Odczyt z pasków w kliencie**, procent z udziału wypełnionych pikseli
   w szerokości. Bez OCR. Czytnik już istnieje: `internal/vitals`.
2. **Lista reguł z priorytetem**: zasób, próg %, hotkey, cooldown, minimalna
   mana; wygrywa pierwsza pasująca od góry. Jeden mechanizm dla potionów i
   czarów, bo w grze jedno i drugie jest hotkeyem.
3. **Hotkeye są w kliencie ustawione na „użyj na sobie"** — po stuknięciu nie
   klikamy niczego myszą.
4. **Leczenie wywłaszcza krok chodzenia.** Bez zamrażania trasy przy progu
   krytycznym.
5. **Leczenie działa niezależnie od śledzenia minimapy** — leczy również wtedy,
   gdy pozycja postaci jest nieznana.
6. **Reguły edytowane w panelu**, wierszami, i jadą w `PUT /api/config` razem
   z resztą konfiguracji.
7. **Dopasowanie minimapy wyjeżdża do gorutyny**, żeby pełne wyszukiwanie nie
   blokowało leczenia.

## Czego ten projekt NIE obejmuje

- Osobnych mechanizmów dla potionów i dla czarów. Jedna lista reguł, jeden
  silnik.
- Języka reguł ponad płaską listę: żadnych warunków złożonych, nawiasów,
  wyrażeń.
- Predykcji obrażeń, uczenia się skuteczności leczenia, statystyk DPS.
- Czytania komunikatów klienta (exhaust, „you are exhausted") — nie mamy OCR
  i nie zamierzamy go tu wprowadzać.
- Samonaprawy kalibracji pasków. Rozjechany prostokąt jest zgłaszany, nie
  naprawiany.
- Kolejki zaległych klawiszy. Stuknięcie odrzucone przez budżet albo bramkę
  świeżości przepada; następna klatka rozstrzygnie na nowo.
- Leczenia innych postaci, rzucania czarów wsparcia, jedzenia.

## Stan wyjściowy

Co już jest i na czym ten projekt stoi:

- `internal/vitals.Read(im, opts) Reading` — procent z paska klienta jako
  `{Percent, OK, Reason}`. Piksel liczy się jako wypełniony po nasyceniu i
  jasności, nigdy po barwie, bo pasek HP zmienia kolor, gdy pustoszeje.
  Wymóg ciągłego prefiksu odrzuca wiersz, w którym coś jest zapalone dalej —
  to bariera przed rozjechaną kalibracją.
- `brain.CombatConfig` z prostokątami `HP` i `Mana`, kalibrowanymi w panelu.
  **Leczenie nie wprowadza żadnej nowej kalibracji obrazu.**
- `brain.CombatState.HPPct/HPOK/ManaPct/ManaOK` — publikowane w snapshocie.
- `input.Driver` z bramką świeżości obserwacji, sprawdzeniem focusu okna,
  jedną akcją „w locie" i twardym limitem 5 stuknięć na sekundę na wszystko.
- `brain.Loop` — jedna gorutyna, jedyny właściciel decyzji.

Czego brakuje: silnika reguł, podbudżetów klawiszy, ścieżki dosłownego
klawisza w sterowniku i responsywności pętli w czasie pełnego wyszukiwania.

## Architektura docelowa

### Nowy pakiet

`internal/heal` — silnik reguł. Czysta decyzja: dostaje dwa odczyty i czas,
oddaje „stuknij ten klawisz" albo „nic, z tego powodu". Nie widzi pikseli, nie
widzi klawiatury, nie ma własnego zegara. Jedyny stan, jaki trzyma, to znaczniki
czasu ostatnich emisji.

```go
type Rule struct {
    Enabled    bool
    Resource   string  // "hp" albo "mana"
    BelowPct   float64 // próg: reguła pasuje przy odczycie <= próg
    Hotkey     string
    CooldownMS int
    MinManaPct float64 // 0 = reguła nie potrzebuje many
}

type Decision struct {
    Fire   bool
    Index  int    // która reguła, licząc od zera; -1 gdy żadna
    Hotkey string
    Reason string // dlaczego nic nie poleciało — dla panelu i logu
}

type Engine struct{ /* reguły + znaczniki czasu */ }

func (e *Engine) SetRules(rules []Rule)
func (e *Engine) Decide(hp, mana vitals.Reading, capturedAt time.Time) Decision
func (e *Engine) Emitted(hotkey string, at time.Time)
```

### Kolejność w klatce

```
sesja → duplikat → widzenie i paski → LECZENIE → dopasowanie → follower
```

Leczenie stoi **przed** dopasowaniem i przed bramką „wyszukiwanie poddane".
Kamera klienta jest wyśrodkowana na postaci, więc paski są w tych samych
pikselach niezależnie od tego, gdzie postać stoi w świecie — i niezależnie od
tego, czy w ogóle wiadomo, gdzie stoi. Poddane wyszukiwanie pozycji nie ma
prawa oślepić leczenia.

Na klatce zduplikowanej (ten sam `VideoTimeUS`) leczenie nie jest liczone.
Ten sam obraz to jedna obserwacja, nie dwie; ruch w sieci nie jest dowodem, że
obraz się zmienił.

### Dopasowanie reguły

Wygrywa pierwsza **wykonalna** reguła od góry — nie pierwsza, która pasuje
progiem. Reguła odpada, gdy:

1. jest wyłączona,
2. odczyt jej zasobu jest nieufny (`OK == false`),
3. odczyt jest powyżej progu,
4. `MinManaPct > 0`, a mana jest niżej albo jej odczyt jest nieufny,
5. od ostatniej **emisji tego samego hotkeya** nie minął `CooldownMS`.

Punkt 4 ma ważną asymetrię: reguła z `MinManaPct == 0` **nie wymaga
wiarygodnego odczytu many**. Inaczej rozjechany prostokąt paska many
blokowałby picie potionów życia, czyli dokładnie to, co ratuje postać.

Punkt 5 wiąże cooldown z **hotkeyem**, nie z regułą. Dwie reguły na tym samym
klawiszu (np. „HP < 70%" i „HP < 40%" na tej samej butelce) dzielą jeden
cooldown. To najtańsza proteza na exhaust w kliencie: gra i tak nie pozwoli
użyć tego samego przedmiotu dwa razy pod rząd.

Cooldown liczy się **od faktycznej emisji**, nie od decyzji. `Decide` niczego
nie zapisuje; dopiero gdy sterownik odpowie `emitted`, pętla woła `Emitted`.
Stuknięcie odrzucone przez budżet albo utracony focus nie może wypalić
cooldownu.

### Trzy zabezpieczenia odczytu

**Wspólny odstęp 500 ms.** Między dowolnymi dwiema emisjami leczenia, niezależnie
od reguł i klawiszy. Sprawdzany względem **czasu zgrania klatki**, nie czasu
decyzji: kolejna próba wymaga obserwacji zrobionej po wygaśnięciu odstępu.
Pasek w kliencie aktualizuje się z opóźnieniem, więc bez tego bot wypiłby
drugi raz, patrząc na obraz sprzed pierwszego łyku. Wartość jest stałą w kodzie
z uzasadnieniem, nie polem konfiguracji: pokrywa się z rezerwą 2 stuknięć na
sekundę, więc luźniejsza i tak nie zostałaby przepuszczona przez budżet.

**HP na dokładnym zerze nie leczy.** Żywa postać nigdy nie pokazuje 0% życia —
takie zero znaczy albo śmierć, albo prostokąt zsunięty na czarne tło. Czytnik
pasków sam tych dwóch przypadków nie odróżni (zero wypełnionych pikseli jest
poprawnym ciągłym prefiksem), więc odróżnia je ta reguła. Zero many jest
normalnym stanem gry i pozostaje dozwolone.

**Nieufny odczyt nie znaczy zero.** `OK == false` blokuje reguły tego zasobu z
czytelnym powodem w panelu, zamiast być traktowane jak stan krytyczny.

Histerezy świadomie nie ma. Powtórne leczenie przy dalej niskim HP jest
pożądane — nie chcemy wymagać, żeby pasek najpierw wrócił ponad próg — a przed
drganiem dokładnie na progu broni cooldown reguły.

### Budżet klawiszy

Dziś jest jedno twarde `maxTapsPerSecond = 5` na wszystko. Nowy układ: okna
przesuwne po sekundzie, osobne dla każdego przeznaczenia, sprawdzane i
zapisywane pod tym samym mutexem, co reszta bramek sterownika.

| Przeznaczenie | Limit |
|---|---|
| globalnie | 8 stuknięć/s |
| chodzenie | 3 stuknięcia/s |
| akcje pięter (lina, drabina, dziura, łopata) | 3 stuknięcia/s |
| leczenie | 2 stuknięcia/s |
| wszystko nieleczące razem | 6 stuknięć/s |

Suma podbudżetów przekracza sufit celowo: sufit jest twardą granicą, a
podbudżety mają nie dopuścić, żeby jedno przeznaczenie zjadło całość. Wiersz
„wszystko nieleczące razem" jest tym, co naprawdę wygradza rezerwę: bez niego
chodzenie i akcje pięter mogłyby razem wybrać sześć stuknięć i zostawić
leczeniu dwa miejsca tylko na papierze.

Rezerwy nie da się pożyczyć: niewykorzystane miejsca leczenia nie podnoszą
limitu chodzenia i odwrotnie. Rezerwa gwarantuje **dostępność budżetu**, nie
natychmiastową emisję — wysłanego kroku nikt nie cofnie.

Liczą się **emisje**, nie zgłoszenia i nie odmowy. Uzbrojenie **przestaje
zerować historię stuknięć**: dziś `Arm()` czyści `taps`, więc przezbrojenie
kasowałoby limit.

Kliknięcia myszą (przyszły łup) będą liczone osobno; ten projekt ich nie
dotyka.

### Ścieżka klawisza leczenia w sterowniku

`Controls` w mózgu i `input.Driver` dostają:

```go
Heal(key string, observationAge time.Duration) input.Result
```

To dosłowny klawisz, nie „rodzaj akcji": bez kliknięcia po stuknięciu i bez
semantyki „akcja w locie", którą ma `UseHotkey`. Przepuszczenie leczenia przez
`UseHotkey` blokowałoby chodzenie aż do potwierdzenia zmiany piętra, która
nigdy nie nastąpi. Wszystkie pozostałe bramki — uzbrojenie, świeżość
obserwacji, focus okna — są wspólne i obowiązują tak samo.

Nazwa klawisza jest walidowana tą samą tablicą, co hotkeye pięter i kierunki.

### Wywłaszczenie kroku

W `follow()`, **za** `executor.Observe` i **przed** `executor.IntentFor` —
dokładnie tam, gdzie stoi dziś pauza akcji pięter. Kolejność nie jest
przypadkowa i ma w kodzie własny komentarz: gdyby wywłaszczać po pobraniu
intencji, powstałby krok oczekujący, którego nikt nie potwierdzi ani nie
wyzeruje. Taki krok wygasa w ponowienie, potem w trwałą blokadę, i trasa staje
na tym waypoincie na dobre.

Wywłaszczenie obowiązuje przez klatkę, na której leczenie faktycznie coś
wysłało. Kroku już wysłanego nie odwołujemy — stuknięcia nie da się cofnąć.

### Dopasowanie minimapy w gorutynie

Dziś `handleFrame` woła `Locate` synchronicznie, a pełne wyszukiwanie ma limit
45 sekund. Przez ten czas pętla nie przetwarza niczego: ani klatek, ani zmian
konfiguracji, ani komend trasy. Moduł, który ma reagować w setkach
milisekund, nie może stać za taką blokadą.

Dopasowanie przenosi się do gorutyny i wraca komendą na kanał `cmds` — tak samo,
jak działa dziś planowanie trasy:

- `matchPending` pilnuje, żeby leciało jedno dopasowanie naraz. Klatki
  przychodzące w międzyczasie normalnie karmią widzenie i leczenie; do
  dopasowania idzie najnowsza, gdy poprzednie się skończy. Nadrabiania nie ma.
- `tracker.Observe`, zapis waypointu, nauka blokad i `follow()` przenoszą się do
  callbacku, z zachowanym oryginalnym czasem zgrania klatki.
- Snapshot dostaje `last_match_seq`: numer klatki, na którą odpowiada
  dopasowanie. Bez tego ani panel, ani testy nie odróżnią „klatka
  przetworzona" od „pozycja gotowa", bo `last_frame_seq` publikuje się teraz
  wcześniej.

Zysk poza leczeniem: `SetConfig`, wczytanie trasy i ręczne waypointy przestają
czekać do 45 sekund za pełnym wyszukiwaniem.

### Konfiguracja

`brain.Config` rośnie o jedno pole, walidowane hurtowo jak reszta — jedno złe
pole nie może cicho wyczyścić innego:

```go
type HealConfig struct {
    Enabled bool
    Rules   []HealRule
}
```

Walidacja odrzuca:

- więcej niż 8 reguł,
- zasób inny niż `hp` albo `mana`,
- próg poza zakresem 1–99% (0 nie zapali się nigdy, 100 paliłoby się zawsze),
- minimalną manę poza zakresem 0–99%,
- cooldown poza zakresem 100–60000 ms,
- nazwę klawisza spoza tablicy sterownika,
- **klawisz przypisany już do liny, drabiny, dziury albo łopaty** — to nie
  konfiguracja, to pomyłka, i kończyłaby się kopaniem dziur zamiast picia.

Pusta lista reguł jest legalna i znaczy „nie lecz". Włącznik `Enabled` jest
przełącznikiem każącym botowi działać, więc — jak `Follow`, `Walk` i
`FloorActions` — **nie jest pamiętany po odświeżeniu karty**.

### Snapshot stanu

`State` rośnie o `Heal HealState`, same skalary:

```go
type HealState struct {
    Enabled    bool
    RuleCount  int
    LastIndex  int     // ostatnia zapalona reguła; -1 gdy żadnej nie było
    LastHotkey string
    LastAgeMS  *int    // wiek ostatniej emisji; nil gdy nie było żadnej
    Reason     string  // dlaczego nic nie poleciało na tej klatce
}
```

`Reason` jest tym, co użytkownik zobaczy, gdy leczenie milczy: „pasek many
nieczytelny", „cooldown F1", „odstęp między leczeniami", „HP na zerze".

### Panel

Nowa sekcja **Leczenie**:

- włącznik `Lecz automatycznie`,
- do ośmiu wierszy reguł: zasób (lista), próg %, hotkey, cooldown ms,
  minimalna mana %, włącznik wiersza, strzałki zmieniające kolejność,
  przycisk usuwania; przycisk `Dodaj regułę`,
- linijka stanu: `ostatnie: F1, 2,3 s temu` albo powód milczenia.

Wiersze reguł są pamiętane po odświeżeniu karty (to konfiguracja, nie
przełącznik działania), tak samo jak hotkeye pięter i kierunki. Zmiana
dowolnego pola wysyła całą konfigurację jednym `PUT /api/config`.

Procenty HP i many są już pokazywane przez wskaźnik widzenia i nie dublujemy
ich w tej sekcji.

## Testy

- `internal/heal` — tabelkowo: wybór pierwszej wykonalnej reguły, pomijanie
  wyłączonych, nieufny odczyt, brak many, cooldown wspólny dla hotkeya,
  odstęp 500 ms liczony od zgrania klatki, HP na zerze, pusta lista.
- `internal/input` — każdy podbudżet osobno, sufit globalny, suma nieleczących,
  brak pożyczania rezerwy, historia przeżywająca przezbrojenie, `Heal` bez
  kliknięcia i bez akcji w locie.
- `internal/brain` — leczenie bez pozycji i bez regionu minimapy, leczenie przy
  poddanym wyszukiwaniu, brak leczenia na klatce zduplikowanej, wywłaszczenie
  kroku, `Emitted` wołane tylko po `emitted`, walidacja konfiguracji, kolizja
  klawisza z akcją piętra, dopasowanie w gorutynie (jedno naraz,
  `last_match_seq`, brak blokady `SetConfig`).
- `webtests` — edytor reguł: dodawanie, usuwanie, kolejność, wysyłka w
  `PUT /api/config`, pamiętanie po odświeżeniu, nietrwałość włącznika.

Testy uruchamiane lokalnie: `go test ./... -race` i `node --test webtests/*.cjs`.
Dockera w tym repo nie ma, świadomie.

## Ryzyka

1. **Opóźnienie paska w kliencie.** Odstęp 500 ms liczony od zgrania klatki
   jest przybliżeniem czasu reakcji interfejsu. Jeśli okaże się za krótki,
   objawi się podwójnym piciem przy gwałtownym spadku HP; wtedy stała rośnie.
   Nie potwierdzamy skutku wzrostem HP, bo obrażenia potrafią go zamaskować.
2. **Fałszywy odczyt przy rozjechanej kalibracji.** Wymóg ciągłego prefiksu
   i bariera zera na HP zamykają najgroźniejsze przypadki, ale nie wszystkie:
   prostokąt zsunięty na inny element interfejsu może dawać stabilny, zupełnie
   fałszywy procent. Panel pokazuje odczyt na żywo — to jedyna kontrola.
3. **Exhaust w kliencie.** Nie widzimy go. Cooldown per hotkey jest protezą
   dobraną przez użytkownika; źle dobrany objawi się stuknięciami bez skutku.
4. **Wywłaszczanie kroku spowalnia chodzenie.** Przy ciężkiej walce leczenie
   będzie zjadać klatki chodzenia. To zamierzone: żywa postać stojąca w miejscu
   jest lepsza niż martwa w drodze.
5. **Ręczny `POST /api/locate` dzieli mutex z `locate.Service`.** Kliknięcie
   „Znajdź pozycję" w trakcie biegu wstrzyma dopasowanie mózgu do końca
   ręcznego odczytu. Po tym projekcie kosztuje to już tylko opóźnioną pozycję,
   bo dopasowanie czeka w swojej gorutynie, a pętla — a z nią leczenie — biegnie
   dalej. Panel nie pozwala na oba naraz, ale kod tego nie gwarantuje.
   Zapisane jako znane ograniczenie nawigacji, nie leczenia.
6. **Podniesiony sufit klawiszy.** Osiem stuknięć na sekundę zamiast pięciu to
   szersze okno dla zapętlonego błędu. Podbudżety ograniczają szkodę do jednego
   przeznaczenia, a bramka focusu i tak zatrzymuje wszystko poza oknem gry.

## Co zostaje na potem

Fazy 2–5 projektu walki i lootu: maszyna stanów aktywności, wybór celu, kolejka
łupu, odwrót. Ten projekt zostawia im gotową konstrukcję: dołożenie
przeznaczenia „czary" to jedno okno przesuwne więcej, a reguły czarów — druga
lista na tym samym silniku, rozszerzona o próg liczby potworów w promieniu.
