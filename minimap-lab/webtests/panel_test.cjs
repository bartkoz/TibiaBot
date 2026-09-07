const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {join} = require('node:path');
const vm = require('node:vm');

const webFile = name => join(__dirname, '..', 'web', name);

// The panel is exercised with only drawing and media permission stubbed. What
// it decides - which rectangle, which settings, when to post - is all that is
// left of it, and all of it is checked here.
function panel({state = {}, onRequest = () => null, storage = {}} = {}) {
  const elements = new Map(), requests = [];
  let drawCount = 0;
  const context2d = new Proxy({}, {get: (_, key) => () => {
    if (key === 'drawImage') drawCount++;
    return {data: new Uint8ClampedArray(4)};
  }});
  let workerMessages = [], workerTick = null;
  let clock = 0, timerID = 0;
  const timers = new Map();

  function element(id = '') {
    return {
      id, value: '', width: 190, height: 190, disabled: false, checked: false,
      textContent: '', hidden: false, className: '', children: [], listeners: {},
      addEventListener(type, fn) { this.listeners[type] = fn; },
      append(...kids) { this.children.push(...kids); },
      replaceChildren(...kids) { this.children = kids; },
      getContext() { return context2d; },
      toBlob(fn) { fn(new Blob(['png'], {type: 'image/png'})); },
      getBoundingClientRect() { return {left: 0, top: 0, width: this.width, height: this.height}; },
      setPointerCapture() {}, setAttribute() {}, remove() {},
      click() { this.listeners.click?.(); this.onclick?.(); },
      fire(type, event = {}) { this.listeners[type]?.(event); },
      async play() {},
      videoWidth: 800, videoHeight: 600, currentTime: 0,
    };
  }
  const document = {
    getElementById(id) { if (!elements.has(id)) elements.set(id, element(id)); return elements.get(id); },
    // A created element joins the lookup table the moment it is given an id,
    // the way appending it to the document does in a browser. Without this the
    // panel's dynamically built rule rows would be unreachable from a test.
    createElement(tag) {
      const el = element();
      let id = '';
      Object.defineProperty(el, 'id', {
        get: () => id,
        set(value) { id = value; if (value) elements.set(value, el); },
      });
      return el;
    },
  };
  for (const [id, value] of Object.entries({
    zoom: '1', mask: '5', floor: '7', threshold: '0.85', gap: '0.015', speed: '20',
    'floor-radius': '8', 'route-every': '10', 'route-tolerance': '1',
    'grid-cols': '15', 'grid-rows': '11', 'decision-radius': '4',
    'bar-width': '27', 'bar-height': '4', 'bar-border': '1',
    'bar-tolerance': '12', 'black-max': '48',
    'battle-bar-width': '27', 'battle-bar-height': '4', 'battle-bar-border': '1',
    'battle-pitch': '22', 'battle-frame-coverage': '0.8',
  })) document.getElementById(id).value = value;
  document.getElementById('floor-auto').checked = true;
  document.getElementById('bar-colors').value = '#00bc00,#50a150,#a1a100,#bf0a0a,#910f0f,#850c0c';
  document.getElementById('battle-frame').value = '#ff5050';

  class Worker {
    constructor(url) { this.url = url; workerTick = () => this.onmessage?.({data: 'tick'}); }
    postMessage(m) { workerMessages.push(m); }
    terminate() {}
  }
  class ImageData { constructor(data, w, h) { Object.assign(this, {data, width: w, height: h}); } }
  class Image {
    set src(v) { this.naturalWidth = 190; this.naturalHeight = 190; }
    async decode() {}
  }
  const track = {stop() {}, addEventListener() {}};

  const sandbox = {
    document, Image, ImageData, Worker, Blob, FormData, URL: {createObjectURL: () => 'blob:x', revokeObjectURL() {}},
    performance: {now: () => clock}, console,
    localStorage: {
      store: new Map(Object.entries(storage)),
      getItem(k) { return this.store.has(k) ? this.store.get(k) : null; },
      setItem(k, v) { this.store.set(k, String(v)); },
      removeItem(k) { this.store.delete(k); },
    },
    setTimeout(fn, delay) { timers.set(++timerID, {fn, at: clock + (delay ?? 0)}); return timerID; },
    clearTimeout(id) { timers.delete(id); },
    navigator: {mediaDevices: {async getDisplayMedia() { return {getTracks: () => [track], getVideoTracks: () => [track]}; }}},
    async fetch(url, options = {}) {
      requests.push({url, method: options.method ?? 'GET', body: options.body});
      const custom = onRequest(url, options);
      if (custom) return custom;
      if (url === '/api/info') return {ok: true, async json() { return {floors: [6, 7], maps: 'maps', message: ''}; }};
      if (url === '/api/state') return {ok: true, async json() { return state; }};
      if (url === '/api/arm') return {ok: true, async json() { return {armed: true, session: '18446744073709551615'}; }};
      if (url === '/api/capture') return {ok: true, async json() { return {session: '123'}; }};
      if (url.startsWith('/api/grid')) {
        return {ok: true, headers: {get: () => '0,0'}, async arrayBuffer() { return new ArrayBuffer(65 * 65); }};
      }
      if (url === '/api/route') return {ok: true, async json() { return {version: 1, name: '', waypoints: []}; }, async text() { return '{}'; }};
      return {ok: true, async json() { return {ok: true}; }, async text() { return '{}'; }};
    },
  };
  sandbox.globalThis = sandbox;
  vm.createContext(sandbox);
  vm.runInContext(readFileSync(webFile('camera.js'), 'utf8'), sandbox);
  vm.runInContext(readFileSync(webFile('panel.js'), 'utf8'), sandbox);
  return {
    sandbox, requests, el: id => document.getElementById(id),
    draws: () => drawCount,
    stored: () => sandbox.localStorage.store,
    tick: () => workerTick?.(),
    advance(ms) {
      clock += ms;
      for (const [id, t] of [...timers]) if (t.at <= clock) { timers.delete(id); t.fn(); }
    },
    workerMessages: () => workerMessages,
    settled: () => new Promise(r => setImmediate(r)),
  };
}

// shareAndSelect brings the panel to the state every frame test needs: a
// shared screen with a minimap rectangle on it.
async function shareAndSelect(p) {
  p.el('share').click();
  await p.settled();
  drag(p.el('screen'), [10, 10], [110, 110]);
  await p.settled();
}

// armNow clicks Arm and runs the countdown out, which is what a user does by
// switching to the game and waiting.
async function armNow(p) {
  p.el('input-arm').click();
  await p.settled();
  for (let i = 0; i < 6; i++) { p.advance(1000); await p.settled(); }
}

const drag = (el, from, to) => {
  el.fire('pointerdown', {clientX: from[0], clientY: from[1], pointerId: 1});
  el.fire('pointermove', {clientX: to[0], clientY: to[1]});
  el.fire('pointerup', {});
};

// pixelPerfect strips the preview's own scaling: the canvas stub reports its
// own size from getBoundingClientRect, and the panel maps clicks onto the
// source resolution. Making the two equal gives a one-to-one mapping, without
// which exact rectangle coordinates could not be checked.
function pixelPerfect(p) {
  p.el('screen').width = 800;
  p.el('screen').height = 600;
}

async function shareOnly(p) {
  p.el('share').click();
  await p.settled();
  pixelPerfect(p);
}

async function calibrate(p, target, from, to) {
  p.el('calib-target').value = target;
  drag(p.el('screen'), from, to);
  await p.settled();
}

const lastConfig = p => JSON.parse(p.requests.filter(r => r.url === '/api/config').at(-1).body);

test('panel pyta o piętra i stan zaraz po starcie', async () => {
  const p = panel();
  await p.settled();
  const urls = p.requests.map(r => r.url);
  assert.ok(urls.includes('/api/info'), urls.join(','));
  assert.ok(urls.includes('/api/state'), urls.join(','));
});

test('zaznaczenie minimapy wysyła całą konfigurację jednym dokumentem', async () => {
  const p = panel();
  await p.settled();
  await shareAndSelect(p);

  const put = p.requests.filter(r => r.url === '/api/config').at(-1);
  assert.ok(put, 'nie wysłano konfiguracji');
  const body = JSON.parse(put.body);
  assert.equal(put.method, 'PUT');
  assert.ok(body.brain, 'brak sekcji brain');
  assert.equal(body.brain.floor, 7);
  assert.equal(body.brain.adjacent_floors, true);
  assert.ok('keys' in body && 'directions' in body, 'konfiguracja rozbita na kawałki');
});

// A JSON number would already have been rounded; the panel must keep the
// session exact all the way into the frame header, or every frame is refused.
test('token sesji większy niż 2^53 trafia do nagłówka klatki bez zmian', async () => {
  const p = panel();
  await p.settled();
  await shareAndSelect(p);
  await armNow(p);

  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();

  const frame = p.requests.find(r => r.url === '/api/frame');
  assert.ok(frame, 'nie wysłano żadnej klatki');
  const view = new DataView(frame.body);
  assert.equal(view.getBigUint64(8, true).toString(), '18446744073709551615');
});

test('presety kierunków wypełniają pola i wysyłają konfigurację', async () => {
  const p = panel();
  await p.settled();
  p.el('dir-preset-numpad').click();
  await p.settled();
  assert.equal(p.el('dir-n').value, 'numpad8');
  const body = JSON.parse(p.requests.filter(r => r.url === '/api/config').at(-1).body);
  assert.equal(body.directions.N, 'numpad8');
  assert.equal(body.directions.NE, 'numpad9');
});

// An empty direction field means "this client has no key for that diagonal",
// which the driver must be told by omission rather than by an empty string.
test('puste pole kierunku nie jedzie jako pusty klawisz', async () => {
  const p = panel();
  await p.settled();
  p.el('dir-preset-numpad').click();
  p.el('dir-ne').value = '';
  p.el('dir-ne').fire('change');
  await p.settled();
  const body = JSON.parse(p.requests.filter(r => r.url === '/api/config').at(-1).body);
  assert.ok(!('NE' in body.directions), 'pusty klawisz został wysłany');
  assert.equal(body.directions.N, 'numpad8');
});

test('tyknięcie workera nie wysyła klatki, gdy nie ma udostępnionego ekranu', async () => {
  const p = panel();
  await p.settled();
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  assert.equal(p.requests.filter(r => r.url === '/api/frame').length, 0);
});

test('zegar pętli startuje w workerze z interwałem, nie na wątku głównym', async () => {
  const p = panel();
  await p.settled();
  await shareAndSelect(p);
  assert.equal(JSON.stringify(p.workerMessages()), JSON.stringify([{intervalMS: 100}]));
});

test('snapshot maluje pozycję, trasę i stan uzbrojenia', async () => {
  const p = panel({state: {
    armed: true,
    position: {x: 32958, y: 32077, z: 7},
    position_age_ms: 42,
    match: {found: true, mode: 'local', score: 0.99, hz: 9.8, match_ms: 3.5, success: 1},
    route: {loaded: true, count: 12, index: 3, next: 'Idź E, pozostało 4 kratek'},
    executor: {waiting: false},
    recorder: {auto: false, count: 12},
    preview_revision: 5,
    log: [{seq: 1, text: 'walk E -> emitted numpad6'}],
  }});
  await p.settled();

  assert.equal(p.el('coordinates').textContent, '32958, 32077, 7');
  assert.equal(p.el('position-age').textContent, '42 ms');
  assert.equal(p.el('actual-hz').textContent, '9.8');
  assert.equal(p.el('route-next').textContent, 'Idź E, pozostało 4 kratek');
  assert.match(p.el('route-status').textContent, /Waypoint 4 z 12/);
  assert.equal(p.el('input-arm').disabled, true, 'uzbrojony panel dalej pozwala uzbrajać');
  assert.equal(p.el('input-disarm').disabled, false);
  assert.match(p.el('blocks-status').textContent, /walk E/);
});

// The preview is a separate request now, so it must be fetched when the
// revision moves and left alone otherwise.
test('podgląd okolicy jest pobierany po zmianie rewizji', async () => {
  const p = panel({state: {position: {x: 100, y: 100, z: 7}, preview_revision: 5, match: {}, route: {}, executor: {}, recorder: {}}});
  await p.settled();
  assert.match(p.el('reference').src, /\/api\/preview\?v=5/);
  assert.equal(p.el('reference').hidden, true);
  p.el('reference').onload();
  assert.equal(p.el('reference').hidden, false);
  p.el('reference').onerror();
  assert.equal(p.el('reference').hidden, true);
});

test('brak pozycji nie pobiera nieistniejącego podglądu', async () => {
  const p = panel({state: {position: null, preview_revision: 0}});
  await p.settled();
  assert.equal(p.el('reference').src, undefined);
  assert.equal(p.el('reference').hidden, true);
});

test('pojedynczy odczyt działa bez sterowania i zachowuje Auto', async () => {
  const p = panel({onRequest(url) {
    if (url === '/api/state') return {ok: false, async json() { return {reason: 'Sterowanie wyłączone.'}; }};
    if (url === '/api/locate') return {ok: true, async json() { return {
      found: true, position: {x: 32200, y: 32180, z: 7}, zoom: 2,
      reason: 'Znaleziono.', preview: 'data:image/png;base64,test',
    }; }};
  }});
  await p.settled();
  p.el('file').fire('change', {target: {files: [{name: 'screen.png'}]}});
  await p.settled();
  p.el('zoom').value = '0';
  assert.equal(p.el('locate').disabled, false);
  p.el('locate').click();
  p.el('locate').click();
  await p.settled();
  const requests = p.requests.filter(r => r.url === '/api/locate');
  assert.equal(requests.length, 1);
  assert.equal(JSON.parse(requests[0].body.get('options')).zoom, 0);
  assert.ok(requests[0].body.get('image') instanceof Blob);
  assert.equal(p.el('coordinates').textContent, '32200, 32180, 7');
  assert.equal(p.el('zoom').value, 2);
  assert.equal(p.requests.filter(r => r.url === '/api/arm' || r.url === '/api/config').length, 0);
});

test('demo uruchamia dopasowanie od razu', async () => {
  const p = panel();
  await p.settled();
  p.el('demo').click();
  await p.settled();
  const request = p.requests.find(r => r.url === '/api/locate');
  assert.ok(request);
  assert.equal(JSON.parse(request.body.get('options')).demo, true);
});

test('Znajdź pozycję uruchamia kolejne odczyty z ekranu bez uzbrojenia', async () => {
  let seq = 0;
  const p = panel({onRequest(url) {
    if (url === '/api/info') return {ok: true, json: async () => ({floors: [7], control_available: false})};
    if (url === '/api/frame') seq++;
    if (url === '/api/frame' || url === '/api/state') return {ok: true, json: async () => ({
      state_version: seq, position: seq ? {x: 32000 + seq, y: 32000, z: 7} : null, position_age_ms: 10,
      zoom: 2, match: {mode: seq === 1 ? 'global' : 'local', hz: 10},
    })};
  }});
  await p.settled();
  await shareAndSelect(p);
  p.el('locate').click();
  await p.settled();
  assert.equal(p.el('live').checked, true);
  assert.equal(p.el('coordinates').textContent, '32001, 32000, 7');
  p.el('video').currentTime = 2;
  p.tick();
  await p.settled();
  assert.equal(p.el('coordinates').textContent, '32002, 32000, 7');
  assert.equal(p.el('search-area').textContent, 'lokalny');
  assert.equal(p.el('input-arm').disabled, true);
  assert.equal(p.requests.filter(r => r.url === '/api/arm' || r.url === '/api/locate').length, 0);
  assert.equal(p.requests.filter(r => r.url === '/api/frame').length, 2);
});

test('podgląd udostępnionego ekranu odświeża się bez wysyłania konfiguracji co klatkę', async () => {
  const p = panel();
  await p.settled();
  await shareAndSelect(p);
  const draws = p.draws();
  const configs = p.requests.filter(r => r.url === '/api/config').length;
  p.el('video').currentTime = 2;
  p.tick();
  await p.settled();
  assert.ok(p.draws() > draws);
  assert.equal(p.requests.filter(r => r.url === '/api/config').length, configs);
});

test('zamrożona klatka nie utrzymuje starego XYZ i nie jedzie ponownie', async () => {
  const state = {state_version: 1, position: {x: 1, y: 2, z: 7}, position_age_ms: 0};
  const p = panel({onRequest(url) {
    if (url === '/api/frame' || url === '/api/state') return {ok: true, json: async () => state};
  }});
  await p.settled();
  await shareAndSelect(p);
  state.state_version = 2;
  p.el('locate').click();
  await p.settled();
  assert.equal(p.el('coordinates').textContent, '1, 2, 7');
  p.advance(1100);
  p.tick();
  await p.settled();
  assert.equal(p.el('coordinates').textContent, 'Pozycja nieznana');
  assert.equal(p.requests.filter(r => r.url === '/api/frame').length, 1);
});

test('zatrzymanie śledzenia zatrzymuje klatki, podgląd pozostaje żywy', async () => {
  const p = panel();
  await p.settled();
  await shareAndSelect(p);
  p.el('locate').click();
  await p.settled();
  const frames = p.requests.filter(r => r.url === '/api/frame').length;
  p.el('live').checked = false;
  p.el('live').fire('change');
  await p.settled();
  const draws = p.draws();
  p.el('video').currentTime = 4;
  p.tick();
  await p.settled();
  assert.equal(p.requests.filter(r => r.url === '/api/frame').length, frames);
  assert.ok(p.draws() > draws);
});

test('wczytanie pliku trasy leci PUT-em na serwer', async () => {
  const p = panel();
  await p.settled();
  const file = {name: 'trasa.json', async text() { return '{"version":1,"waypoints":[]}'; }};
  p.el('route-file').fire('change', {target: {files: [file]}});
  await p.settled();
  const put = p.requests.find(r => r.url === '/api/route' && r.method === 'PUT');
  assert.ok(put, 'trasa nie została wysłana');
  assert.equal(put.body, '{"version":1,"waypoints":[]}');
});

test('dodanie waypointa idzie osobnym żądaniem', async () => {
  const p = panel();
  await p.settled();
  p.el('route-add').click();
  await p.settled();
  assert.ok(p.requests.some(r => r.url === '/api/route/waypoint' && r.method === 'POST'));
});

// Losing the source resolution means the minimap is no longer where it was,
// so the panel must stop rather than keep posting the wrong rectangle.
test('zmiana rozdzielczości źródła rozbraja i kasuje zaznaczenie', async () => {
  const p = panel();
  await p.settled();
  p.el('share').click();
  await p.settled();
  drag(p.el('screen'), [10, 10], [110, 110]);
  await p.settled();
  await armNow(p);

  p.el('video').videoWidth = 1024;
  p.el('snapshot').click();
  await p.settled();
  assert.ok(p.requests.some(r => r.url === '/api/disarm'), 'nie rozbrojono po zmianie rozdzielczości');

  // The old rectangle must be gone too, not merely unarmed: arming again
  // without selecting the minimap afresh must still send nothing.
  await armNow(p);
  const before = p.requests.filter(r => r.url === '/api/frame').length;
  p.el('video').currentTime = 9;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  assert.equal(p.requests.filter(r => r.url === '/api/frame').length, before,
    'panel dalej wysyła klatki prostokątem sprzed zmiany rozdzielczości');
});

// The browser has focus at the moment its own button is clicked, and the
// driver memorises whatever window is focused when the request arrives.
// Arming straight away would memorise the panel, and the first key would
// disarm on lost focus. The countdown is the window to switch to the game.
test('uzbrojenie czeka pięć sekund, zanim cokolwiek wyśle', async () => {
  const p = panel();
  await p.settled();

  p.el('input-arm').click();
  await p.settled();
  assert.equal(p.requests.filter(r => r.url === '/api/arm').length, 0,
    'uzbrojono natychmiast — sterownik zapamiętałby okno przeglądarki');
  assert.match(p.el('status').textContent, /okno gry/);

  for (let i = 0; i < 6; i++) { p.advance(1000); await p.settled(); }
  assert.equal(p.requests.filter(r => r.url === '/api/arm').length, 1);
});

test('drugie kliknięcie w trakcie odliczania anuluje uzbrajanie', async () => {
  const p = panel();
  await p.settled();
  p.el('input-arm').click();
  await p.settled();
  p.el('input-arm').click();
  await p.settled();
  for (let i = 0; i < 8; i++) { p.advance(1000); await p.settled(); }
  assert.equal(p.requests.filter(r => r.url === '/api/arm').length, 0);
});

// Twelve key fields retyped after every refresh is not a workflow.
test('klawisze przeżywają odświeżenie karty', async () => {
  const first = panel();
  await first.settled();
  first.el('dir-preset-wsad').click();
  first.el('hotkey-rope').value = 'f7';
  first.el('hotkey-rope').fire('change');
  await first.settled();

  const saved = Object.fromEntries(first.stored());
  const second = panel({storage: saved});
  await second.settled();

  assert.equal(second.el('dir-n').value, 'w');
  assert.equal(second.el('hotkey-rope').value, 'f7');
});

test('zapis pełnej klatki tworzy pobranie w rozdzielczości źródła', async () => {
  const p = panel();
  await p.settled();
  p.el('share').click();
  await p.settled();
  // Podglądamy tworzenie elementów, bo pobranie to element <a> z atrybutem
  // download - w sandboxie nie ma prawdziwego DOM, żeby je zobaczyć inaczej.
  const created = [];
  const make = p.sandbox.document.createElement;
  p.sandbox.document.createElement = tag => {
    const el = make(tag);
    created.push(el);
    return el;
  };
  p.el('frame-save').click();
  await p.settled();
  const link = created.find(el => el.download);
  assert.ok(link, 'nie utworzono odnośnika pobrania');
  assert.equal(link.download, 'combat-capture.png');
  assert.equal(link.href, 'blob:x');
});

// A reload must never resume walking on its own.
test('przełączniki, które każą botowi działać, nie są zapamiętywane', async () => {
  const first = panel();
  await first.settled();
  for (const id of ['input-walk', 'input-actions', 'route-follow', 'route-record']) {
    first.el(id).checked = true;
    first.el(id).fire('change');
  }
  await first.settled();

  const second = panel({storage: Object.fromEntries(first.stored())});
  await second.settled();

  for (const id of ['input-walk', 'input-actions', 'route-follow', 'route-record']) {
    assert.equal(second.el(id).checked, false, `${id} wrócił zaznaczony po odświeżeniu`);
  }
});

test('zaznaczenie okna gry wysyła wycinek jedenastu kratek', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);

  const combat = lastConfig(p).brain.combat;
  assert.deepEqual(combat.viewport, {x: 100, y: 50, w: 240, h: 176});
  // A radius of 4 reaches 5 tiles, i.e. 11 columns of 16 px. The window only
  // has 11 rows, so vertically the crop is clipped to the full height - which
  // is why the crop saves one quarter here, not a multiple of it.
  assert.deepEqual(combat.crop, {x: 132, y: 50, w: 176, h: 176});
});

test('cztery nowe regiony trafiają do klatki', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  await calibrate(p, 'battle', [400, 60], [559, 279]);
  await calibrate(p, 'hp', [20, 300], [119, 307]);
  await calibrate(p, 'mana', [20, 312], [119, 319]);
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  await armNow(p);

  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();

  const frame = p.requests.find(r => r.url === '/api/frame');
  assert.ok(frame, 'nie wysłano żadnej klatki');
  // Header byte 5 is the region count: the minimap plus the four new ones.
  assert.equal(new Uint8Array(frame.body)[5], 5);
});

test('kalibracja minimapy nie rusza prostokątów widzenia', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  const before = lastConfig(p).brain.combat.viewport;
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  assert.deepEqual(lastConfig(p).brain.combat.viewport, before);
});

test('podgląd widzenia nie jest pobierany, dopóki nie jest włączony', async () => {
  const seen = {combat: {calibrated: true}};
  const p = panel({
    state: seen,
    onRequest: url => url === '/api/frame'
      ? {ok: true, async json() { return seen; }}
      : null,
  });
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  await armNow(p);
  const visionCalls = () => p.requests.filter(r => r.url === '/api/vision').length;
  assert.equal(visionCalls(), 0, 'podgląd pobrany, choć wyłączony');

  p.el('vision-preview').checked = true;
  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  assert.ok(visionCalls() >= 1, 'włączony podgląd nie pobrał widzenia');
});

test('kliknięcie własnego paska na podglądzie wypełnia jego pozycję', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  p.el('bar-width').value = '13';
  p.el('bar-height').value = '4';
  // The preview has not received any data yet, so the canvas still has the
  // size from the HTML; the test sets it explicitly so the click maps 1:1.
  p.el('vision-canvas').width = 176;
  p.el('vision-canvas').height = 176;
  p.el('vision-canvas').fire('pointerdown', {clientX: 88, clientY: 76});
  await p.settled();
  // The click lands on the bar's centre; its top-left corner is what is stored.
  assert.equal(p.el('self-bar-x').value, 82);
  assert.equal(p.el('self-bar-y').value, 74);
});

// Go marshals an unset creature list as JSON null (vision.Find returns a nil
// slice when the screen holds no creature bar, which is the common case), so
// the preview must render an empty screen instead of throwing on it.
test('podgląd widzenia nie wywraca się na pustym ekranie (bars: null)', async () => {
  const p = panel({
    onRequest: url => {
      if (url === '/api/frame') return {ok: true, async json() { return {combat: {calibrated: true}}; }};
      if (url === '/api/vision') {
        return {ok: true, async json() { return {have: true, crop_w: 176, crop_h: 176, bars: null}; }};
      }
      return null;
    },
  });
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  p.el('vision-preview').checked = true;
  await armNow(p);

  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();

  assert.equal(p.el('vision-info').textContent, 'Nie widzę żadnego stwora.');
});

// Mirrors 'zmiana rozdzielczości źródła rozbraja i kasuje zaznaczenie' above,
// but for a vision rectangle instead of the minimap: the failure mode is a
// stale crop from the previous resolution still being cut and posted.
test('zmiana rozdzielczości źródła kasuje też prostokąty widzenia', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  await armNow(p);

  p.el('video').currentTime = 1;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  const before = p.requests.filter(r => r.url === '/api/frame').length;
  assert.ok(before >= 1, 'nie wysłano klatki przed zmianą rozdzielczości');

  p.el('video').videoWidth = 1024;
  p.el('snapshot').click();
  await p.settled();

  // Re-arming without recalibrating must still send nothing: if the old
  // viewport rectangle survived, the panel would keep cutting and posting a
  // crop measured against a screen that no longer exists.
  await armNow(p);
  p.el('video').currentTime = 9;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  assert.equal(p.requests.filter(r => r.url === '/api/frame').length, before,
    'panel dalej wysyła klatki prostokątem widzenia sprzed zmiany rozdzielczości');
});

// The existing minimap-then-vision test above covers one order; the two
// orders run through different pointerup branches, so both need covering.
test('kalibracja prostokąta widzenia po minimapie nie rusza jej regionu', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  await armNow(p);

  p.el('video').currentTime = 1;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  const withMinimapOnly = new Uint8Array(p.requests.find(r => r.url === '/api/frame').body)[5];
  assert.equal(withMinimapOnly, 1, 'oczekiwano samej minimapy przed kalibracją okna gry');

  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  p.el('video').currentTime = 2;
  p.tick();
  await p.settled();

  const last = p.requests.filter(r => r.url === '/api/frame').at(-1);
  // If calibrating the vision rectangle had cleared the minimap's region,
  // the count would still be one instead of growing to two.
  assert.equal(new Uint8Array(last.body)[5], 2,
    'region minimapy zniknął po kalibracji okna gry');
});

test('wskaźnik widzenia pokazuje potwory, cel liczony od jedynki i niewiarygodny odczyt HP jako kreskę', async () => {
  const p = panel({state: {
    combat: {
      calibrated: true, bars_total: 4, monsters_in_range: 3, rejected_by_map: 1,
      mixed_crowd: true, battle_rows: 5, battle_truncated: false, target_row: 2,
      hp_pct: 0.42, hp_ok: false, mana_pct: 0.77, mana_ok: true,
    },
    match: {}, route: {}, executor: {}, recorder: {},
  }});
  await p.settled();

  assert.equal(p.el('vision-monsters').textContent, '3 (mieszany tłum)');
  assert.equal(p.el('vision-bars').textContent, '4');
  assert.equal(p.el('vision-rejected').textContent, '1');
  assert.equal(p.el('vision-rows').textContent, '5');
  // target_row jest liczone od zera na drucie; człowiek czyta wiersze od jedynki.
  assert.equal(p.el('vision-target').textContent, '3');
  // hp_ok: false - kalibracja najpewniej się rozjechała, więc kreska, nie 42%.
  assert.equal(p.el('vision-hp').textContent, '—');
  assert.equal(p.el('vision-mana').textContent, '77%');
});

test('wskaźnik widzenia pokazuje same kreski bez kalibracji', async () => {
  const p = panel({state: {match: {}, route: {}, executor: {}, recorder: {}}});
  await p.settled();

  for (const id of ['vision-monsters', 'vision-bars', 'vision-rejected', 'vision-rows', 'vision-target', 'vision-hp', 'vision-mana']) {
    assert.equal(p.el(id).textContent, '—', `${id} powinno pokazać kreskę bez state.combat`);
  }

  // Ten sam wynik, gdy combat istnieje, ale calibrated jest false - reguła
  // mówi "combat brak LUB calibrated false", nie tylko brak pola.
  const p2 = panel({state: {
    combat: {calibrated: false, monsters_in_range: 9, hp_ok: true, hp_pct: 1},
    match: {}, route: {}, executor: {}, recorder: {},
  }});
  await p2.settled();
  assert.equal(p2.el('vision-monsters').textContent, '—');
  assert.equal(p2.el('vision-hp').textContent, '—');
});

test('dodana reguła leczenia jedzie w konfiguracji', async () => {
  const p = panel();
  await p.settled();
  p.el('heal-on').checked = true;
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-0-below').value = '55';
  p.el('heal-0-below').fire('input');
  p.el('heal-0-hotkey').value = 'f2';
  p.el('heal-0-hotkey').fire('input');
  await p.settled();

  const heal = lastConfig(p).brain.heal;
  assert.equal(heal.enabled, true);
  assert.equal(heal.rules.length, 1);
  assert.deepEqual(heal.rules[0], {
    enabled: true, resource: 'hp', below_pct: 55, hotkey: 'f2',
    cooldown_ms: 1000, min_mana_pct: 0,
  });
});

test('strzałka zmienia kolejność reguł', async () => {
  const p = panel();
  await p.settled();
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-0-hotkey').value = 'f1';
  p.el('heal-0-hotkey').fire('input');
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-1-hotkey').value = 'f2';
  p.el('heal-1-hotkey').fire('input');
  await p.settled();

  p.el('heal-1-up').click();
  await p.settled();

  const keys = lastConfig(p).brain.heal.rules.map(r => r.hotkey);
  assert.deepEqual(keys, ['f2', 'f1']);
});

test('usunięcie reguły zdejmuje ją z konfiguracji', async () => {
  const p = panel();
  await p.settled();
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-0-del').click();
  await p.settled();
  assert.deepEqual(lastConfig(p).brain.heal.rules, []);
});

test('reguły przeżywają odświeżenie karty, a włącznik leczenia nie', async () => {
  const first = panel();
  await first.settled();
  first.el('heal-on').checked = true;
  first.el('heal-add').click();
  await first.settled();
  first.el('heal-0-hotkey').value = 'f3';
  first.el('heal-0-hotkey').fire('input');
  await first.settled();

  const second = panel({storage: Object.fromEntries(first.stored())});
  await second.settled();
  assert.equal(second.el('heal-0-hotkey').value, 'f3');
  assert.equal(second.el('heal-on').checked, false);
});

test('linijka stanu leczenia pokazuje ostatnią regułę i powód ciszy', async () => {
  const p = panel({state: {heal: {
    enabled: true, rule_count: 2, last_index: 0, last_hotkey: 'f1',
    last_age_ms: 2300, reason: '',
  }}});
  await p.settled();
  assert.match(p.el('heal-status').textContent, /f1/);
  assert.match(p.el('heal-status').textContent, /2,3 s/);

  const quiet = panel({state: {heal: {
    enabled: true, rule_count: 2, last_index: -1, last_hotkey: '',
    last_age_ms: null, reason: 'cooldown klawisza f1',
  }}});
  await quiet.settled();
  assert.match(quiet.el('heal-status').textContent, /cooldown klawisza f1/);
});
