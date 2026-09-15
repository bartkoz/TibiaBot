// gridPixels zamienia kafle na piksele podglądu przechodności. Reguła
// pierwszeństwa barw jest jedynym miejscem, w którym panel cokolwiek
// interpretuje z danych mapy, więc ma własny test.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {gridPixels} from '../web/blocks.js';

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
