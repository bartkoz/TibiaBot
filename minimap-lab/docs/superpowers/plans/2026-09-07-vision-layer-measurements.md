# Pomiary z fazy 1

**Status: pomiary wykonane 2026-09-18** na `testdata/combat-capture.png` (5120×2880,
rozdzielczość natywna, bez skalowania systemowego). Pierwszy zrzut z 2026-09-15
(3600×2338) był powiększeniem 2× z wygładzaniem przez macOS i został odrzucony —
patrz spec `2026-09-18-vision-antialiasing-design.md`, który opisuje, co te pomiary
zmieniły w kodzie.

Metoda: skrypt w Pythonie (PIL + numpy) do profili pikseli, a każdy wynik detekcji
potwierdzony prawdziwym `vision.Find` i `battle.Read` uruchomionym na wycinkach
zrzutu. Liczby w tabeli to te, które trafiają do `internal/testenv.CombatCalibration()`.

Data pomiaru: 2026-09-18
Klatka odniesienia: `testdata/combat-capture.png`

| Co | Wartość | Jak zmierzone |
|---|---|---|
| siatka okna gry | 15×11, potwierdzona: okno 3141×2303 ma proporcję 1,3639 przy oczekiwanej 1,3636 | rzut kolorowości na kolumny i wiersze, potem profil pikseli krawędzi w `y=1400` i `x=2000`; UI klienta jest szare, świat kolorowy |
| piksele na kratkę | 209,4 × 209,4 | `3141/15`, `2303/11` |
| geometria paska życia nad stworem | 62×8 px, obwódka 3 (rdzeń 56×2) | profil pionowy `18 → 40 → 121 → 161 → 161 → 121 → 40 → 18`; oba wiersze przejścia (40, 121) wciągnięte do obwódki, bo 121 ≤ próg czerni 125 |
| barwy wypełnienia | zieleń `(0,161,0)` i `(0,149,0)`, żółć `(161,161,0)` — rdzeń zależy od fazy subpikselowej; kalibracja `#009b00`, `#9b9b00`, `#aa0a0a` z tolerancją 20 | odczyt środka rdzenia trzech pasków; przeszukanie tolerancji 15–25 × progu 120–128 dało zawsze te same 3 paski |
| próg czerni (`black_max`) | **125** | między 121 (najjaśniejszy wiersz przejścia) a 141 (najciemniejszy rdzeń minus tolerancja); `validate()` przyjmuje, bo `161 − 20 = 141 > 125` |
| zakotwiczenie paska (własny pasek) | dx = −50, dy = −163 px → `(−0,24, −0,78)` kratki | środek paska własnego względem środka okna gry; pasek własny był **niebieski** `(0,0,255)` na x 2129–2184, y 1233–1234 — pozycja z pikseli, nie z detektora. Na zrzucie z 2026-09-15: `(−0,23, −0,86)` — stabilne między klatkami |
| zakotwiczenie dużego stwora — rozjazd | brak dużego stwora na zrzucie; Assassin 1 wypada na `(0, −1)`, Footman na `(−1, +1)` — całe kratki; Assassin 2 na `(3,78, 3,0)` — w pół kroku | `Grid.Offset` z zakotwiczeniem wyżej |
| geometria paska w battle liście | 262×8 px, obwódka 1 (rdzeń 260×6) | profil `48 → 144 → 192 ×4 → 144 → 48`; wiersz 144 nie może być ciemny (próg tnie się na 128), więc jest wypełnieniem przy tolerancji 80 |
| odstęp wierszy battle listy | 44 px | paski wierszy 1 i 2 na y 1011 i 1055 |
| barwa i pokrycie ramki celu | `(201,10,10)` = `#c90a0a`; kwadrat 40×40 o krawędzi 2 px **wokół ikonki**, nie wokół wiersza; ikonka na `(−45, −31)` od rogu paska; pokrycie 0,8 boku ikonki = 32 px z 40 | bbox czerwieni w oknie battle listy; biegi 40 px w wierszach 936–937 i 974–975 |
| pasek HP / many | HP `Rect(24, 134, 2202, 136)`, mana `Rect(2217, 134, 4392, 136)` — 2178 × 2 i 2175 × 2 px | z profilu `y=135` (zieleń do 2025 = 91,9 % = 147/160 ✓, kontener do 2202); cyfry „147/160” przecinają wypełnienie w wierszach 136–157, więc jedyny czysty pas to 134–135 nad nimi |
| okno gry / wycinek / battle lista | `Rect(637,245,3778,2548)` / `Rect(1056,245,3359,2548)` / `Rect(4770,900,5100,1150)` | j.w.; wycinek to `RecommendedCrop()` dla promienia 4 |
| stwory / wiersz celu | 3 stwory w wycinku, `TargetRow = 0` | policzone na oczy i potwierdzone detekcją |

## Co z tego wynika dla dalszych faz

1. **Geometria 27×4 z klasycznej kalibracji nie pasuje** i nigdy nie pasowała do
   tego klienta — pasek nad stworem ma 62×8 przy natywnym 5K, a w battle liście
   262×8. Domyślne w `CombatConfig` zostają jako hipoteza startowa, kalibracja idzie
   przez panel; testy realnej klatki czytają geometrię z fixture.
2. **Siatka 15×11 potwierdzona** z dokładnością 0,02 %.
3. **Barwy `vision.DefaultColors()`** (`#00bc00` itd.) są jaśniejsze niż zmierzone
   rdzenie (161, 149) — to skutek wygładzania brzegów, które zaniża szczyt. Tolerancja
   20 wokół zmierzonych barw działa; wokół domyślnych trzeba by 30–40 i ryzykować
   fałszywe trafienia. Kalibracja barw idzie z pomiaru, nie z tabeli.
4. **Klient wygładza brzegi pasków** — to nie skalowanie systemu, tylko sam klient.
   Pełny pasek przechodzi przez `confirm()` przypadkiem, ranny stwór nie. Stąd
   tolerancja brzegowa, osobne pola battle listy i ramka szukana wokół ikonki —
   wszystko w specu z 2026-09-18.
5. **Pasek własny był niebieski** na obu zrzutach — palety HP nie dotyka, więc
   wykluczenie po pozycji nie ma dziś nic do roboty. Pozycja zanotowana, mechanika
   klienta niezgadywana.
