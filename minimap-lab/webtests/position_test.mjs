// Znajdowanie postaci i utrzymywanie jej znalezionej.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel, shareAndSelect} from './harness.mjs';

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
