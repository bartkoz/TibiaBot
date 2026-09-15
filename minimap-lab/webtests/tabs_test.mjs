// Pasek zakładek: co jest widoczne, co przeżywa odświeżenie i co mówią odznaki.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel, shareAndSelect, shareOnly, calibrate, openTab} from './harness.mjs';

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

  p.el('tab-pozycja').fire('keydown', {key: 'End'});
  assert.equal(p.el('panel-diagnostyka').hidden, false, 'End nie skoczył na ostatnią');

  p.el('tab-diagnostyka').fire('keydown', {key: 'Home'});
  assert.equal(p.el('panel-pozycja').hidden, false, 'Home nie skoczył na pierwszą');
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

// Bez tego panel otwarty między klatkami stoi pusty - a w trybie pojedynczego
// odczytu kolejna klatka nigdy nie przyjdzie, więc stałby pusty na zawsze.
test('otwarcie zakładki dociąga podgląd bez czekania na kolejną klatkę', async () => {
  const p = panel({state: {combat: {calibrated: true}}});
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  p.el('vision-preview').checked = true;

  const calls = () => p.requests.filter(r => r.url === '/api/vision').length;
  assert.equal(calls(), 0, 'podgląd pobrany, choć zakładka schowana');

  openTab(p, 'walka');
  await p.settled();

  assert.ok(calls() >= 1, 'przełączenie na zakładkę nie dociągnęło podglądu');
});

// `calibrated` obiecuje tylko okno gry i wycinek. Przy niezaznaczonych paskach
// zostaje prawdziwe, a leczenie odmawia na każdej klatce - bez tej odznaki nic
// by o tym nie mówiło.
test('odznaka Walki ostrzega, gdy leczenie nie ma odczytu pasków', async () => {
  const p = panel({state: {
    heal: {enabled: true},
    combat: {calibrated: true, hp_ok: false, mana_ok: true, monsters_in_range: 2},
  }});
  await p.settled();

  assert.equal(p.el('badge-walka').textContent, '!',
    'skalibrowane okno gry przykryło brak odczytu pasków');
});

// Odliczanie siedzi w timerze, nie w strumieniu: panel odłożony w trakcie
// odliczania i tak wysłałby /api/arm pięć sekund później.
test('zatrzymanie panelu kasuje odliczanie uzbrojenia', async () => {
  const p = panel();
  await p.settled();
  await shareAndSelect(p);

  p.el('input-arm').click();
  await p.settled();
  p.stop();
  for (let i = 0; i < 6; i++) { p.advance(1000); await p.settled(); }

  assert.equal(p.requests.filter(r => r.url === '/api/arm').length, 0,
    'panel zatrzymany, a uzbrojenie i tak poleciało');
});
