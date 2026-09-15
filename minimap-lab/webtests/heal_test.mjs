// Reguły leczenia: lista, jej kolejność i to, co z niej jedzie.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel, lastConfig} from './harness.mjs';

test('dodana reguła leczenia jedzie w konfiguracji', async () => {
  const p = panel();
  await p.settled();
  p.el('heal-on').checked = true;
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-0-below').value = '55';
  p.el('heal-0-below').fire('input');
  p.el('heal-0-hotkey').value = 'f2';
  p.el('heal-0-hotkey').fire('input');
  await p.settled();

  const heal = lastConfig(p).brain.heal;
  assert.equal(heal.enabled, true);
  assert.equal(heal.rules.length, 1);
  assert.deepEqual(heal.rules[0], {
    enabled: true, resource: 'hp', below_pct: 55, hotkey: 'f2',
    cooldown_ms: 1000, min_mana_pct: 0,
  });
});

test('strzałka zmienia kolejność reguł', async () => {
  const p = panel();
  await p.settled();
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-0-hotkey').value = 'f1';
  p.el('heal-0-hotkey').fire('input');
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-1-hotkey').value = 'f2';
  p.el('heal-1-hotkey').fire('input');
  await p.settled();

  p.el('heal-1-up').click();
  await p.settled();

  const keys = lastConfig(p).brain.heal.rules.map(r => r.hotkey);
  assert.deepEqual(keys, ['f2', 'f1']);
});

test('usunięcie reguły zdejmuje ją z konfiguracji', async () => {
  const p = panel();
  await p.settled();
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-0-del').click();
  await p.settled();
  assert.deepEqual(lastConfig(p).brain.heal.rules, []);
});

test('reguły przeżywają odświeżenie karty, a włącznik leczenia nie', async () => {
  const first = panel();
  await first.settled();
  first.el('heal-on').checked = true;
  first.el('heal-add').click();
  await first.settled();
  first.el('heal-0-hotkey').value = 'f3';
  first.el('heal-0-hotkey').fire('input');
  await first.settled();

  const second = panel({storage: Object.fromEntries(first.stored())});
  await second.settled();
  assert.equal(second.el('heal-0-hotkey').value, 'f3');
  assert.equal(second.el('heal-on').checked, false);
});

test('linijka stanu leczenia pokazuje ostatnią regułę i powód ciszy', async () => {
  const p = panel({state: {heal: {
    enabled: true, rule_count: 2, last_index: 0, last_hotkey: 'f1',
    last_age_ms: 2300, reason: '',
  }}});
  await p.settled();
  assert.match(p.el('heal-status').textContent, /f1/);
  assert.match(p.el('heal-status').textContent, /2,3 s/);

  const quiet = panel({state: {heal: {
    enabled: true, rule_count: 2, last_index: -1, last_hotkey: '',
    last_age_ms: null, reason: 'cooldown klawisza f1',
  }}});
  await quiet.settled();
  assert.match(quiet.el('heal-status').textContent, /cooldown klawisza f1/);
});

// The first heal test only drives below_pct and hotkey through their
// handlers; deepEqual there proves HEAL_DEFAULT is seeded, not that resource,
// cooldown, min_mana_pct and the row's own switch write back to healRules[i].
// This exercises the other four, so a handler writing to the wrong property
// would fail the deepEqual below.
test('pozostałe pola wiersza (zasób, cooldown, min. mana, włącznik) trafiają do konfiguracji', async () => {
  const p = panel();
  await p.settled();
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-0-resource').value = 'mana';
  p.el('heal-0-resource').fire('input');
  p.el('heal-0-cooldown').value = '5000';
  p.el('heal-0-cooldown').fire('input');
  p.el('heal-0-mana').value = '40';
  p.el('heal-0-mana').fire('input');
  p.el('heal-0-enabled').checked = false;
  p.el('heal-0-enabled').click();
  await p.settled();

  assert.deepEqual(lastConfig(p).brain.heal.rules[0], {
    enabled: false, resource: 'mana', below_pct: 60, hotkey: 'f1',
    cooldown_ms: 5000, min_mana_pct: 40,
  });
});

// The client prints and the hotkey dialog writes keys in capitals ("F1"), but
// the driver's key table is lowercase-only - a capital letter must not become
// a silent server-side refusal.
test('klawisz wpisany wielkimi literami jedzie małymi', async () => {
  const p = panel();
  await p.settled();
  p.el('heal-add').click();
  await p.settled();
  p.el('heal-0-hotkey').value = 'F1';
  p.el('heal-0-hotkey').fire('input');
  await p.settled();

  assert.equal(lastConfig(p).brain.heal.rules[0].hotkey, 'f1');
});
