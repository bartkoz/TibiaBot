// The fake browser every panel test runs against. Only drawing and media
// permission are stubbed; what the panel decides - which rectangle, which
// settings, when to post - is all that is left of it, and all of it is checked
// against this.

import {createPanel} from '../web/app.js';

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
  // One factory call per test: the panel no longer runs on import, so nothing
  // leaks from one test into the next the way a shared vm context did.
  const app = createPanel(sandbox);
  app.start();
  return {
    sandbox, requests, el: id => document.getElementById(id),
    stop: () => app.stop(),
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

// openTab switches the panel to one tab, the way clicking it does. The two
// previews only fetch while their own tab is on screen, so a test that wants
// one has to be looking at it.
const openTab = (p, id) => p.el(`tab-${id}`).click();

const lastConfig = p => JSON.parse(p.requests.filter(r => r.url === '/api/config').at(-1).body);
export {panel, shareAndSelect, armNow, drag, pixelPerfect, shareOnly, calibrate, openTab, lastConfig};
