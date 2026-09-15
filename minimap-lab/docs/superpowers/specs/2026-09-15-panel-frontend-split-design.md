# Podział warstwy frontendowej panelu

Data: 2026-09-15. Gałąź: `brain-go`.

## Problem

`web/panel.js` ma 1072 linie i robi wszystko: przechwytywanie obrazu, zaznaczanie
prostokątów, formularze konfiguracji, reguły leczenia, widzenie, trasę waypointów,
nauczone blokady, uzbrajanie wykonawcy i render. `web/index.html` to jedna długa
strona z ośmioma numerowanymi sekcjami. `web/style.css` to jedna zminifikowana
linia. Testy (`webtests/panel_test.cjs`, 35 KB, 41 testów) sklejają `camera.js`
i `panel.js` w jeden sandbox `vm.runInContext`, więc każdy test dzieli stan
modułowy z poprzednim.

Cel: podzielić kod na moduły o jednej odpowiedzialności, wprowadzić zakładki
i uporządkować CSS — bez zmiany zachowania bota i formatu danych.

## Decyzje

### Stack: ES moduły, bez builda

Panel zostaje waniliowym JS-em rozbitym na moduły ES ładowane przez
`<script type="module">`. Go serwuje je bez żadnej zmiany w `server.go`.

Odrzucone:

- **React + bundler.** Nie daje tego, po co był rozważany — o wygląd odpowiada
  CSS, nie framework. Ma tu też realny minus: panel to w większości pola, w które
  użytkownik wpisuje wartości, gdy bot już chodzi, a przerysowywanie drzewa
  10 razy na sekundę wokół pola z fokusem gubi kursor. Koszt: `package-lock.json`,
  `node_modules`, krok builda przed `go:embed` (koniec z samym `go run .`)
  i 35 KB testów do przepisania. Tę samą rekomendację dały niezależnie Gemini
  i Codex.
- **Klasyczne skrypty i globalne.** Granice modułów byłyby umowne — nic ich nie
  pilnuje, a kolejność `<script>` staje się ukrytą zależnością.

### Moduł ESM rozpoznawany po `package.json` w korzeniu

Node czyta `web/*.js` jako moduły ES tylko wtedy, gdy najbliższy `package.json`
deklaruje `{"type": "module"}`. Rozważana alternatywa — rozszerzenie `.mjs` —
odpada: Go 1.24 zna ten typ MIME na tym Macu, ale na Windows
`mime.TypeByExtension` czyta rejestr i może go nie znać, a wtedy przeglądarka
odmówi wczytania modułu. README deklaruje wsparcie Windows.

### `createPanel(env)` zamiast skryptu wykonującego się przy imporcie

Cały panel jest fabryką przyjmującą jeden obiekt środowiska:

```js
createPanel(env) -> {start(), stop()}
```

`env` dostarcza: `document`, `fetch`, `localStorage`, `setTimeout`,
`clearTimeout`, `Worker`, `navigator`, `performance`, `Image`, `ImageData`,
`Blob`, `FormData`, `URL`, `console`. W przeglądarce to `globalThis`, w testach —
atrapa. Nic nie wykonuje się przy imporcie, więc każdy test dostaje własną
instancję zamiast wspólnego sandboxa.

Wywołania idą przez `env.fetch(...)` i `env.setTimeout(...)` jako metody, nie
przez destrukturyzację — odpięte od `window` rzucają w przeglądarce
`Illegal invocation`.

### Nazwy plików

`main_test.go:30-31` sprawdza, że `GET /panel.js` i `GET /camera.js` zwracają 200.
`panel.js` zostaje więc nazwą punktu wejścia i chudnie do trzech linii, które
importują `createPanel` z `app.js` i startują panel. Test Go nie wymaga zmian.

| plik | zawartość |
|---|---|
| `panel.js` | wejście przeglądarki: `createPanel(globalThis).start()` |
| `app.js` | korzeń: składa moduły, pętla klatek, rozsyłanie stanu |
| `dom.js` | `$`, `num`, budowanie pól i przycisków |
| `api.js` | wszystkie wywołania `/api/*` |
| `form.js` | `REMEMBERED`, `saveForm`, `restoreForm` |
| `tabs.js` | przełączanie zakładek, pamiętana zakładka, wskaźniki |
| `source.js` | źródło obrazu, udostępnianie, snapshot, rysowanie podglądu i wycinka |
| `selection.js` | `point`, handlery wskaźnika, `roi` / `marker` / `rects` |
| `position.js` | lokalizacja, śledzenie XYZ, telemetria, mapa referencyjna |
| `route.js` | trasa i lista waypointów |
| `vision.js` | prostokąty widzenia, `cropRect`, podgląd, telemetria walki |
| `heal.js` | reguły leczenia |
| `control.js` | uzbrajanie, klawisze kierunków i akcji |
| `blocks.js` | podgląd przechodności, kasowanie blokad |
| `camera.js` | bez zmian poza `export` (dziś `globalThis` + `module.exports`) |

### Kontrakt modułu

```js
export function createVision(ctx) {
  return {
    mount(),        // podpina listenery, buduje to, co dynamiczne
    render(state),  // dostaje cały stan z /api/state
    config(),       // swój fragment do PUT /api/config
  };
}
```

`ctx` to wspólna powierzchnia tworzona i posiadana przez `app.js`:
`{env, dom, api, source, selection, status, pushConfig, tabs}`. Bez magistrali
zdarzeń — przy tej skali szyna utrudnia śledzenie przepływu bardziej, niż pomaga.

### Dwie własności, których nie wolno zgubić

1. **`brainConfig()` wysyła całą powierzchnię naraz.** Serwer waliduje ją jako
   jeden dokument, więc jedno złe pole odbija się z powodem, zamiast po cichu
   wpuścić połowę formularza. Po podziale to sklejka z `module.config()`, a nie
   osobne żądania per moduł.
2. **Strażnik `state_version`** odrzucający spóźnione odpowiedzi zostaje
   w jednym miejscu — w `app.js`, przed rozesłaniem stanu do modułów.

## Układ panelu

Pasek zakładek biegnie przez całą szerokość, nad obiema kolumnami. Sześć etykiet
to około 510 px, a prawa kolumna ma 340 px — wciśnięte tam łamałyby się na trzy
rzędy.

```
┌──────────────────────────────────────────────────────┐
│ Minimap Lab              32200, 32180, 7    9.8/s    │ sticky
├──────────────────────────────────────────────────────┤
│ Pozycja │ Trasa │ Walka● │ Leczenie │ Sterow.│ Diagn. │
├────────────────────────────┬─────────────────────────┤
│ [demo] [zrzut] [udostępnij]│                         │
│ Zaznaczam: [ minimapę  ▾ ] │   ustawienia aktywnej   │
│ ┌────────────────────────┐ │   zakładki              │
│ │    podgląd ekranu      │ │                         │
│ └────────────────────────┘ │                         │
│ [wycinek]    [mapa ref.]   │                         │
└────────────────────────────┴─────────────────────────┘
```

### Poza zakładkami (stała lewa kolumna)

Pasek narzędzi, `#source`, `#screen`, `#roi-info`, `#crop`, `#reference`
z nakładką trasy — plus **`calib-target` przeniesiony z sekcji 7**. Dziś ten
select siedzi w sekcji „Widzenie", a przeciąga się po płótnie z sekcji 1, kilka
ekranów wyżej. Zakładki zamieniłyby tę niezręczność w pułapkę: użytkownik na
zakładce „Pozycja" przeciągałby po minimapie, a zapisałoby się to jako prostokąt
battle listy. Kontrola musi stać przy płótnie, którym steruje.

### Zakładki

| zakładka | dzisiejsze sekcje | wskaźnik |
|---|---|---|
| Pozycja | 3 | — |
| Trasa | 4 + 6 | licznik waypointów |
| Walka | 7 bez `calib-target` | ostrzeżenie, gdy `combat.calibrated` fałszywe |
| Leczenie | 8 | zielona kropka, gdy włączone |
| Sterowanie | 5 | zielona kropka, gdy uzbrojony |
| Diagnostyka | `#json`, `#maps`, log, stopka | — |

Wskaźniki ciągnie `render(state)` w `app.js` przez `tabs.setBadge()`.

Panele chowane są atrybutem `hidden`, nie klasą: `tracking.css` ma już
`[hidden]{display:none!important}`, a atrapa elementu w testach ma pole `hidden`.
`role="tablist"` / `tab` / `tabpanel`, `aria-selected`, strzałki lewo-prawo
przełączają. Aktywna zakładka pamiętana w `localStorage` pod istniejącym
`STORAGE_KEY`.

### Dlaczego zakładki niczego nie psują

- `putImageData` na ukrytym płótnie działa normalnie — bitmapa żyje niezależnie
  od widoczności, więc przełączenie zakładki nie gubi narysowanego podglądu.
- `getBoundingClientRect()` na ukrytym elemencie zwraca zera, ale wszystkie
  pomiary kliknięć dotyczą albo płócien ze stałej kolumny (`#screen`, `#crop`),
  albo płócien w zakładkach, w które da się kliknąć tylko wtedy, gdy są widoczne.

## Jedyna zmiana zachowania

`fetchVision()` i `refreshGrid()` strzelają co klatkę, gdy ich checkbox jest
zaznaczony. Dochodzi warunek: **tylko gdy ich zakładka jest widoczna**. Inaczej
przy 10 Hz panel mieliłby żądania na niewidoczne płótno. Poza tym refaktor nie
zmienia zachowania ani formatu danych.

## CSS

`style.css` to dziś jedna zminifikowana linia (2.8 KB) — nieedytowalna po ludzku,
a minifikacja lokalnego panelu serwowanego z `embed` nie kupuje nic.
Rozminifikowanie i podział na `tokens.css` (paleta, skala odstępów
4/8/12/16/24/32, promienie, rozmiary pisma), `base.css`, `layout.css`,
`components.css`.

Długi do spłacenia:

- `.hint{font-size:12px!important}` — `!important` znika; kontrast `#a0b2bf`
  na `#17212a` to 7,5:1 i przechodzi, problemem jest sam rozmiar 12 px → 13 px
- etykieta ma `margin:14px 0 7px`, pole pod nią `margin-top:5px` — po tokenach
  jeden rytm zamiast dwóch
- ściany prozy przy kontrolkach zwijane do `<details><summary>Dlaczego tak</summary>`;
  **treść zostaje co do słowa**

## Testy

- Atrapa DOM z `panel_test.cjs` (~110 linii) wyjeżdża do `webtests/harness.mjs`
  i zostaje — jest dobra.
- 41 istniejących testów dzieli się na pliki per moduł; asercje przenoszone
  w większości dosłownie. Nic nie jest kasowane.
- `camera_test.cjs` i `panel_test.cjs` → `.mjs`.
- `node --test webtests/` zamiast `node --test webtests/*.cjs`.
- Camera dostaje jawne `fetch` i `cut` z `env`, bo pod ESM `globalThis` w Node
  to prawdziwy globalThis, a nie sandbox.

## Fazy

Każda kończy się zielonym `node --test webtests/` **i** `go test ./...`.

1. **Fabryka bez podziału** — `app.js` z `createPanel(env)` (wciąż monolit),
   `panel.js` jako wejście, `camera.js` na ESM, root `package.json`,
   `index.html` na `type="module"`, harness testów przestawiony na fabrykę.
2. **Wydzielanie modułów** — `dom` → `api` → `form` → `source` → `selection` →
   `position` → `route` → `vision` → `heal` → `control` → `blocks`.
3. **Zakładki** — `tabs.js`, przebudowa `index.html`, przeniesienie
   `calib-target`, wskaźniki, warunek widoczności podglądów.
4. **CSS** — rozminifikowanie, tokeny, układ, zwinięcie prozy.
5. **README** — dziesięć odwołań do numerów sekcji: linie 37, 75 (`4. Trasa`),
   167 (`6. Podgląd przechodności`), 175 (`7. Widzenie…`), 187 (`sekcji 7`),
   210 (`Sekcja 7`, `sekcji 3`), 280, 330, 336 (`5. Sterowanie`).

## Ryzyka

1. **`restoreForm()` jest wołany dwa razy** w starcie — przed i po pobraniu listy
   pięter, żeby zapamiętane Z miało już swoje `<option>`. Łatwo to zgubić przy
   przenoszeniu, a objawia się dopiero po odświeżeniu karty.
2. **Zmiana rozdzielczości źródła** rozbraja, kasuje zaznaczenie i prostokąty
   widzenia — dotyka trzech modułów naraz. Ma na to trzy testy.
3. 41 testów to jedyna siatka bezpieczeństwa. Faza bez zielonych testów nie jest
   skończona.
