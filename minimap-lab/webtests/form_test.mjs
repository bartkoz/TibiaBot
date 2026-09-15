// Co przeżywa odświeżenie karty, a co nie ma prawa.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel} from './harness.mjs';

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

// Lista pięter przychodzi z /api/info, więc w chwili pierwszego restore
// #floor nie ma jeszcze żadnej opcji i czyta się jako pusty. Cokolwiek
// zapisałoby się w tym oknie — kliknięcie zakładki, wpisane ustawienie —
// nadpisałoby zapamiętane piętro pustką, którą drugie restore wepchnęłoby
// z powrotem do już wypełnionego selecta.
test('zapis jest zamknięty, dopóki panel się nie uruchomi', async () => {
  const p = panel({storage: {'minimap-lab.panel': JSON.stringify({floor: '6'})}});

  // jeszcze przed odpowiedzią z /api/info
  p.el('tab-walka').click();
  await p.settled();

  assert.equal(JSON.parse(p.stored().get('minimap-lab.panel')).floor, '6',
    'kliknięcie w trakcie startu nadpisało zapamiętane piętro');
  assert.equal(p.el('floor').value, '6', 'piętro nie wróciło do selecta');
});
