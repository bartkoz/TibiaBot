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
  const context2d = new Proxy({}, {get: () => () => ({data: new Uint8ClampedArray(4)})});
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
    createElement: element,
  };
  for (const [id, value] of Object.entries({
    zoom: '1', mask: '5', floor: '7', threshold: '0.85', gap: '0.015', speed: '20',
    'floor-radius': '8', 'route-every': '10', 'route-tolerance': '1',
  })) document.getElementById(id).value = value;
  document.getElementById('floor-auto').checked = true;

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
    document, Image, ImageData, Worker, Blob, URL: {createObjectURL: () => 'blob:x', revokeObjectURL() {}},
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
  p.el('live').checked = true;
  p.el('live').fire('change');
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
  const p = panel({state: {preview_revision: 5, match: {}, route: {}, executor: {}, recorder: {}}});
  await p.settled();
  assert.match(p.el('reference').src, /\/api\/preview\?v=5/);
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
