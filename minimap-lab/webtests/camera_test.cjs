const {test} = require('node:test');
const assert = require('node:assert/strict');
const {Camera, HEADER_SIZE, REGION_HEADER, MAGIC, REGION} = require('../web/camera.js');

// The camera only ever reads these three properties off its source.
const fakeVideo = (currentTime = 1.5) => ({currentTime, videoWidth: 800, videoHeight: 600});
const fakeCut = (video, rect) => new Uint8ClampedArray(rect.w * rect.h * 4);
const ok = () => ({ok: true, json: async () => ({state_version: 1})});

function camera(options = {}) {
  return new Camera({cut: fakeCut, fetch: async () => ok(), ...options});
}

test('ciało zaczyna się magikiem MLF1 i niesie zadeklarowane regiony', () => {
  const cam = camera();
  cam.setSession('7');
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 4, h: 4});
  const body = new Uint8Array(cam.buildBody(fakeVideo(), 0));
  const view = new DataView(body.buffer);

  assert.equal(String.fromCharCode(...body.slice(0, 4)), MAGIC);
  assert.equal(body[4], 1, 'wersja formatu');
  assert.equal(body[5], 1, 'liczba regionów');
  assert.equal(view.getUint16(6, true), 0, 'flagi są zarezerwowane i muszą być zerowe');
  assert.equal(view.getBigUint64(8, true), 7n, 'sesja przechwytywania');
  assert.equal(view.getUint16(HEADER_SIZE + 4, true), 4, 'szerokość regionu');
  assert.equal(view.getUint32(HEADER_SIZE + 8, true), 4 * 4 * 4, 'długość ładunku');
  assert.equal(body.length, HEADER_SIZE + REGION_HEADER + 4 * 4 * 4);
});

// A JSON number would already have been rounded before it got here, so the
// session arrives as a decimal string and has to survive as one.
test('sesja większa niż 2^53 przechodzi bez utraty precyzji', () => {
  const cam = camera();
  const huge = '18446744073709551615';
  cam.setSession(huge);
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 1, h: 1});
  const view = new DataView(cam.buildBody(fakeVideo(), 0));
  assert.equal(view.getBigUint64(8, true).toString(), huge);
});

test('numer klatki rośnie z każdą wysyłką', async () => {
  const cam = camera();
  cam.setSession('1');
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 1, h: 1});
  const sent = [];
  cam.fetch = async (url, opts) => { sent.push(new DataView(opts.body).getBigUint64(16, true)); return ok(); };
  await cam.sendFrame(fakeVideo(1));
  await cam.sendFrame(fakeVideo(2));
  assert.deepEqual(sent, [1n, 2n]);
});

// Two POSTs in flight would arrive out of order and pile up behind a slow
// match; a skipped frame is always cheaper than a backlog.
test('druga klatka nie jedzie, dopóki pierwsza nie wróciła', async () => {
  let inFlight = 0, peak = 0, release;
  const gate = new Promise(r => { release = r; });
  const cam = camera({fetch: async () => {
    inFlight++; peak = Math.max(peak, inFlight);
    await gate;
    inFlight--;
    return ok();
  }});
  cam.setSession('1');
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 1, h: 1});

  const first = cam.sendFrame(fakeVideo(1));
  const second = cam.sendFrame(fakeVideo(2));
  release();
  await Promise.all([first, second]);

  assert.equal(peak, 1, 'dwa POST-y naraz');
});

// Network traffic is no proof that the picture moved.
test('niezmieniony currentTime nie tworzy nowej obserwacji', async () => {
  const sent = [];
  const cam = camera({fetch: async (url, opts) => { sent.push(opts.body); return ok(); }});
  cam.setSession('1');
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 1, h: 1});

  await cam.sendFrame(fakeVideo(1.5));
  await cam.sendFrame(fakeVideo(1.5));

  assert.equal(sent.length, 1, 'zamrożona klatka poszła dwa razy');
});

test('bez sesji ani bez regionu nic nie jest wysyłane', async () => {
  let calls = 0;
  const cam = camera({fetch: async () => { calls++; return ok(); }});
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 1, h: 1});
  await cam.sendFrame(fakeVideo(1));
  assert.equal(calls, 0, 'wysłano klatkę bez uzbrojenia');

  cam.setSession('1');
  cam.setRegion(REGION.minimap, null);
  await cam.sendFrame(fakeVideo(2));
  assert.equal(calls, 0, 'wysłano klatkę bez zaznaczonej minimapy');
});

test('snapshot z odpowiedzi trafia do wywołania zwrotnego', async () => {
  const seen = [];
  const cam = camera({onSnapshot: s => seen.push(s)});
  cam.setSession('1');
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 1, h: 1});
  await cam.sendFrame(fakeVideo(1));
  assert.equal(seen.length, 1);
  assert.equal(seen[0].state_version, 1);
});

// A refusal carries a reason the panel has to show; swallowing it would look
// exactly like a request that never happened.
test('odmowa serwera trafia do obsługi błędu, nie do snapshotu', async () => {
  const errors = [], snapshots = [];
  const cam = camera({
    fetch: async () => ({ok: false, status: 403, json: async () => ({reason: 'inna sesja'})}),
    onSnapshot: s => snapshots.push(s),
    onError: e => errors.push(e),
  });
  cam.setSession('1');
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 1, h: 1});

  await cam.sendFrame(fakeVideo(1));

  assert.deepEqual(errors, ['inna sesja']);
  assert.equal(snapshots.length, 0);
});

test('zerwane połączenie nie wysadza pętli', async () => {
  const errors = [];
  const cam = camera({fetch: async () => { throw new Error('sieć padła'); }, onError: e => errors.push(e)});
  cam.setSession('1');
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 1, h: 1});

  await cam.sendFrame(fakeVideo(1));

  assert.deepEqual(errors, ['sieć padła']);
  assert.equal(cam.inFlight, false, 'slot pozostał zajęty po błędzie');
});

test('dwa regiony jadą w kolejności nagłówków', () => {
  const cam = camera();
  cam.setSession('1');
  cam.setRegion(REGION.minimap, {x: 0, y: 0, w: 2, h: 2});
  cam.setRegion(REGION.hp, {x: 10, y: 10, w: 3, h: 1});
  const body = new Uint8Array(cam.buildBody(fakeVideo(), 0));
  assert.equal(body[5], 2);
  assert.equal(body[HEADER_SIZE], REGION.minimap);
  assert.equal(body[HEADER_SIZE + REGION_HEADER], REGION.hp);
  assert.equal(body.length, HEADER_SIZE + 2 * REGION_HEADER + 2 * 2 * 4 + 3 * 1 * 4);
});
