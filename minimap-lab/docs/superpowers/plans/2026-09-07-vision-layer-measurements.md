# Pomiary z fazy 1

**Status: pomiary nie zostały jeszcze wykonane.** Wymagają zdjęcia z gry —
kliknięcia **Zapisz klatkę PNG** w panelu podczas walki z co najmniej trzema
stworami na ekranie, zapisania wyniku jako `testdata/combat-capture.png` i
wypełnienia zmierzonymi prostokątami `internal/testenv.CombatCalibration()`.
Dopóki ten plik i te liczby nie istnieją, pięć testów, na które ten dokument
się powołuje, pomija się same (`t.Skip`), a każda wartość w tabeli poniżej
jest pusta — nikt jeszcze nie zmierzył gry na żywo, więc żadna liczba tutaj
nie powinna wyglądać na policzoną.

Ten dokument jest **formularzem**: każdy wiersz mówi dokładnie, jaką komendą
albo jaką czynnością w panelu wypełnić brakującą komórkę. Osoba z dostępem do
gry powinna umieć przejść tabelę od góry do dołu bez ponownego wymyślania
metody.

Data pomiaru: *(brak — jeszcze nie wykonano)*
Klatka odniesienia: `testdata/combat-capture.png` *(jeszcze nie istnieje w repo)*

| Co | Wartość | Jak zmierzone |
|---|---|---|
| siatka okna gry | *(brak pomiaru)* | Proporcje zmierzonego prostokąta `viewport` w `internal/testenv.CombatCalibration()`. Sprawdza to automatycznie `go test ./internal/testenv/ -run TestCombatFixtureGeometry -v` — porównuje `Viewport.Dx()/Dy()` z `GridCols/GridRows` (domyślnie 15×11), z tolerancją 3% na skalowanie klienta. |
| piksele na kratkę | *(brak pomiaru)* | `Viewport.W / GridCols`, `Viewport.H / GridRows`, policzone z prostokąta `okno gry`, zaznaczonego w panelu w sekcji **7. Widzenie: potwory, battle lista, paski** (selektor „Kalibruję" → „okno gry"). |
| geometria paska życia nad stworem | *(brak pomiaru)* szerokość × wysokość px, obwódka *(brak pomiaru)* px | Powiększenie `testdata/combat-capture.png` (albo `.debug/vision-fixture.png`, które zapisuje `go test ./internal/vision/ -run TestRealCaptureOffsets -v` po zgraniu klatki): policz w poziomie i w pionie piksele koloru wypełnienia oraz grubość czarnej obwódki wokół nich. Wpisz do pól „Pasek: szerokość/wysokość/obwódka" w panelu (`bar_width`, `bar_height`, `bar_border`). |
| barwy wypełnienia | *(brak pomiaru)* | Odczyt koloru pikseli wypełnienia na kilku różnych poziomach HP na tej samej klatce. Porównaj z sześcioma wartościami `vision.DefaultColors()` (`internal/vision/bars.go`) i popraw pole „Barwy wypełnienia paska" (`bar_colors`) oraz „Tolerancja barw" (`bar_tolerance`) w panelu, jeśli klient je zmienił. |
| próg czerni (`black_max`) | *(brak pomiaru)* | Najciemniejsza wartość kanału RGB zmierzona w środku wypełnienia paska, zestawiona z najjaśniejszą wartością kanału zmierzoną w jego czarnej obwódce lub tle. `black_max` musi leżeć bezpiecznie między nimi — `CombatConfig.validate()` odmawia progu, który razem z tolerancją barw pochłonąłby którąkolwiek barwę wypełnienia, i nazywa tę barwę w komunikacie błędu. |
| zakotwiczenie paska (własny pasek / mały stwór) | dx = *(brak pomiaru)*, dy = *(brak pomiaru)* px | Zaznacz w panelu (sekcja 7) „Klient rysuje własny pasek postaci", włącz „Pokazuj podgląd widzenia" i kliknij pasek postaci na podglądzie — panel wylicza `AnchorDX`/`AnchorDY` sam (`Grid.AnchorFrom`), z pikseli, które właśnie kliknięto. Ta sama wartość trafia do logu `go test ./internal/vision/ -run TestRealCaptureOffsets -v`, w linii „zakotwiczenie wyliczone z własnego paska: dx=… dy=…". |
| zakotwiczenie dużego stwora — rozjazd względem małego | *(brak pomiaru)* kratki | W tym samym logu `TestRealCaptureOffsets` znajdź wpis stwora ze sprite'em wyraźnie większym niż jedna kratka i porównaj wypisany offset z kratką, na której ten stwór faktycznie stoi na obrazie — pomaga `.debug/vision-fixture.png`, ten sam test zapisuje tam obrysy pasków narysowane na wycinku. Wynik i jego konsekwencja dla fazy 3 idą do sekcji „Ryzyka" specyfikacji (`docs/superpowers/specs/2026-09-07-combat-and-loot-design.md`), punkt 1. |
| geometria paska w battle liście | *(brak pomiaru)* szerokość × wysokość px, obwódka *(brak pomiaru)* px | Powiększenie zrzutu w miejscu battle listy: policz piksele wypełnienia i grubość obwódki jednego mini-paska. Wpisz do „Battle: szerokość/wysokość/obwódka paska" (`battle_bar_width`, `battle_bar_height`, `battle_bar_border`). |
| odstęp wierszy battle listy | *(brak pomiaru)* px | Odległość w pikselach między środkami dwóch sąsiednich mini-pasków w battle liście, zmierzona na tym samym powiększeniu. Wpisz do „Battle: odstęp wierszy" (`battle_row_pitch`). |
| barwa i pokrycie ramki celu | *(brak pomiaru)*, *(brak pomiaru)* | Odczyt koloru pikseli obwódki wokół wiersza z aktywnym atakiem (`battle_frame`) i to, jaki ułamek szerokości wiersza ta obwódka pokrywa w jednej linii (`battle_frame_coverage`) — potrzebny wpis w battle liście z aktywnym atakiem na zrzucie. |
| pasek HP / many | *(brak pomiaru)* szerokość × wysokość px | Prostokąty zaznaczone w krokach „pasek HP" i „pasek many" kalibracji panelu (sekcja 7), zaznaczone bez obwódki i bez cyfr. |

## Co z tego wynika dla dalszych faz

Nie można tego jeszcze wypełnić: wnioski zależą od liczb powyżej, a żadna z
nich nie jest jeszcze zmierzona. Jedyny wniosek, który da się wyciągnąć bez
pomiaru, jest w specyfikacji (`docs/superpowers/specs/2026-09-07-combat-and-loot-design.md`,
sekcja „Ryzyka", punkt 1): procedura pomiaru zakotwiczenia dużego stwora i to,
co każdy z dwóch możliwych wyników znaczy dla fazy 3. Po wykonaniu pomiarów
ta sekcja powinna dostać krótką listę: czy geometria `27×4` z klasycznego
kalibracji nadal pasuje, czy siatka 15×11 się potwierdziła, i czy któraś z
domyślnych barw (`vision.DefaultColors()`) wymaga korekty na tym konkretnym
kliencie.
