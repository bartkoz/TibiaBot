// Składanie panelu, pętla klatek i jeden dokument konfiguracji.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel, shareAndSelect, armNow} from './harness.mjs';

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
