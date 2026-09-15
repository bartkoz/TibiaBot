// Pasek zakładek: co jest widoczne, co przeżywa odświeżenie i co mówią odznaki.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel} from './harness.mjs';

const PANELS = ['pozycja', 'trasa', 'walka', 'leczenie', 'sterowanie', 'diagnostyka'];

test('widoczna jest dokładnie jedna zakładka', async () => {
  const p = panel();
  await p.settled();
  const shown = () => PANELS.filter(id => !p.el(`panel-${id}`).hidden);

  assert.deepEqual(shown(), ['pozycja'], 'start nie na pierwszej zakładce');

  p.el('tab-walka').click();
  assert.deepEqual(shown(), ['walka'], 'po kliknięciu widać coś innego niż wybrane');
});

test('aktywna zakładka przeżywa odświeżenie karty', async () => {
  const p = panel({storage: {'minimap-lab.panel': JSON.stringify({tab: 'sterowanie'})}});
  await p.settled();

  assert.equal(p.el('panel-sterowanie').hidden, false, 'zapamiętana zakładka się nie otworzyła');
  assert.equal(p.el('panel-pozycja').hidden, true);
});

test('strzałki chodzą po pasku i zawijają się na końcach', async () => {
  const p = panel();
  await p.settled();

  p.el('tab-pozycja').fire('keydown', {key: 'ArrowLeft'});
  assert.equal(p.el('panel-diagnostyka').hidden, false, 'w lewo z pierwszej nie zawinęło na ostatnią');

  p.el('tab-diagnostyka').fire('keydown', {key: 'ArrowRight'});
  assert.equal(p.el('panel-pozycja').hidden, false, 'w prawo z ostatniej nie wróciło na pierwszą');
});

// Odznaka istnieje po to, żeby zakładka, na którą nikt nie patrzy, mogła
// powiedzieć, że coś się na niej dzieje.
test('odznaki pokazują uzbrojenie, licznik trasy i leczenie bez pokrycia', async () => {
  const p = panel({state: {
    armed: true,
    heal: {enabled: true},
    route: {count: 3, index: 0},
    combat: {calibrated: false},
  }});
  await p.settled();

  assert.equal(p.el('badge-sterowanie').hidden, false, 'uzbrojenie bez odznaki');
  assert.equal(p.el('badge-trasa').textContent, '3');
  // Leczenie włączone bez skalibrowanych pasków to przełącznik, który nie ma
  // jak zadziałać - to ostrzeżenie, nie licznik potworów.
  assert.equal(p.el('badge-walka').textContent, '!');
});

test('odznaka Walki liczy potwory, gdy paski są skalibrowane', async () => {
  const p = panel({state: {combat: {calibrated: true, monsters_in_range: 2}}});
  await p.settled();

  assert.equal(p.el('badge-walka').textContent, '2');
  assert.equal(p.el('badge-sterowanie').hidden, true, 'odznaka uzbrojenia bez uzbrojenia');
});
