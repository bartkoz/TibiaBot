// gridPixels zamienia kafle na piksele podglądu przechodności. Reguła
// pierwszeństwa barw jest jedynym miejscem, w którym panel cokolwiek
// interpretuje z danych mapy, więc ma własny test.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {gridPixels} from '../web/blocks.js';
import {panel, shareAndSelect, armNow, openTab} from './harness.mjs';

const FREE = 0, WALL = 1, MISSING = 2, TEMP = 4, PERM = 8;
const colourOf = (cell, at = 0) => [...gridPixels([cell], 1).slice(at * 4, at * 4 + 4)];

const GREEN = [40, 70, 40, 255];
const RED = [150, 40, 40, 255];
const GRAPHITE = [40, 40, 45, 255];
const AMBER = [220, 170, 40, 255];
const VIOLET = [230, 80, 230, 255];

test('teren maluje się zielenią, ścianą i grafitem', () => {
  assert.deepEqual(colourOf(FREE), GREEN);
  assert.deepEqual(colourOf(WALL), RED);
  assert.deepEqual(colourOf(MISSING), GRAPHITE);
});

// Pokazanie nauczonej blokady jest całym sensem podglądu: gdyby teren ją
// przykrywał, kratka z ladą wyglądałaby jak każda inna przechodnia.
test('nauczona blokada wygrywa z terenem pod spodem', () => {
  assert.deepEqual(colourOf(FREE | TEMP), AMBER);
  assert.deepEqual(colourOf(WALL | TEMP), AMBER, 'ściana przykryła blokadę tymczasową');
  assert.deepEqual(colourOf(MISSING | TEMP), AMBER, 'brak danych przykrył blokadę tymczasową');
});

test('blokada trwała wygrywa z tymczasową', () => {
  assert.deepEqual(colourOf(TEMP | PERM), VIOLET);
  assert.deepEqual(colourOf(WALL | TEMP | PERM), VIOLET);
});

// Serwer oddaje okno o stałym boku, ale krótsza odpowiedź nie ma prawa
// wysypać podglądu ani przemalować kratek, których nie opisała.
test('krótsza odpowiedź zostawia resztę okna przezroczystą', () => {
  const out = gridPixels([WALL], 2);
  assert.equal(out.length, 2 * 2 * 4);
  assert.deepEqual([...out.slice(0, 4)], RED);
  assert.deepEqual([...out.slice(4)], new Array(12).fill(0));
});

// #blocks-status ma dwóch piszących: pętla klatek wpisuje tam ostatnią linię
// logu dziesięć razy na sekundę. Bez przytrzymania odpowiedź na kliknięcie
// znikałaby, zanim dałoby się ją przeczytać.
test('odpowiedź na kliknięcie w kratkę nie znika pod pętlą klatek', async () => {
  const snapshot = {
    state_version: 1,
    position: {x: 100, y: 200, z: 7},
    log: [{seq: 1, text: 'walk E -> emitted numpad6'}],
  };
  const p = panel({
    onRequest: url => {
      if (url === '/api/frame' || url === '/api/state') {
        return {ok: true, async json() { return {...snapshot, state_version: snapshot.state_version++}; }};
      }
      if (url === '/api/blocks') return {ok: true, async json() { return {cleared: true}; }};
      return null;
    },
  });
  await p.settled();
  await shareAndSelect(p);
  await armNow(p);

  const tick = async at => {
    p.el('video').currentTime = at;
    p.tick();
    for (let i = 0; i < 3; i++) await p.settled();
  };

  p.el('grid-preview-on').checked = true;
  openTab(p, 'trasa');
  p.el('live').checked = true;
  p.el('live').fire('change');
  for (let i = 0; i < 4; i++) await p.settled();
  await tick(1.5);

  p.el('grid-canvas').fire('click', {clientX: 5, clientY: 5});
  await p.settled();
  assert.match(p.el('blocks-status').textContent, /Usunięto nauczoną blokadę/);

  await tick(2.5);
  assert.match(p.el('blocks-status').textContent, /Usunięto nauczoną blokadę/,
    'pętla zdeptała odpowiedź na kliknięcie');

  // po upływie przytrzymania linia wraca do logu
  p.advance(5000);
  await tick(3.5);
  assert.match(p.el('blocks-status').textContent, /walk E/,
    'przytrzymanie nigdy nie puszcza linii statusu');
});
