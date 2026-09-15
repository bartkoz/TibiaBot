// Uzbrajanie wykonawcy i klawisze, którymi chodzi.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel} from './harness.mjs';

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
