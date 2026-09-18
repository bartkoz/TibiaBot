// Widzenie: prostokąty klienta, wycinek decyzji i wskaźnik walki.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel, armNow, shareOnly, calibrate, openTab, lastConfig} from './harness.mjs';

test('zaznaczenie okna gry wysyła wycinek jedenastu kratek', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);

  const combat = lastConfig(p).brain.combat;
  assert.deepEqual(combat.viewport, {x: 100, y: 50, w: 240, h: 176});
  // A radius of 4 reaches 5 tiles, i.e. 11 columns of 16 px. The window only
  // has 11 rows, so vertically the crop is clipped to the full height - which
  // is why the crop saves one quarter here, not a multiple of it.
  assert.deepEqual(combat.crop, {x: 132, y: 50, w: 176, h: 176});
});

test('nowe pola tolerancji i ikonki jadą w konfiguracji', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  p.el('battle-tolerance').value = '80';
  p.el('battle-black-max').value = '48';
  p.el('battle-edge').value = '1';
  p.el('bar-edge').value = '0';
  p.el('battle-icon-x').value = '-45';
  p.el('battle-icon-y').value = '-31';
  p.el('battle-icon-size').value = '40';
  p.el('battle-tolerance').fire('input');
  await p.settled();

  const combat = lastConfig(p).brain.combat;
  assert.equal(combat.battle_bar_tolerance, 80);
  assert.equal(combat.battle_black_max, 48);
  assert.equal(combat.battle_edge_tolerance, 1);
  assert.equal(combat.bar_edge_tolerance, 0);
  assert.equal(combat.battle_icon_offset_x, -45);
  assert.equal(combat.battle_icon_offset_y, -31);
  assert.equal(combat.battle_icon_size, 40);
});

test('input na polu tekstowym nie wypycha configu, change nadal tak', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  const configPosts = () => p.requests.filter(r => r.url === '/api/config').length;
  const before = configPosts();

  // bar-colors is free text (a comma-separated list of #rrggbb values), not
  // a number dial - every keystroke fires 'input' but the value is invalid
  // until the last character, so it must not push on 'input' at all.
  p.el('bar-colors').value = '#0';
  p.el('bar-colors').fire('input');
  await p.settled();
  assert.equal(configPosts(), before,
    'input na polu tekstowym wypchnął config przed dokończeniem wpisywania');

  p.el('bar-colors').value = '#00bc00';
  p.el('bar-colors').fire('change');
  await p.settled();
  assert.equal(configPosts(), before + 1,
    'change na polu tekstowym powinien nadal wypychać config jak dotychczas');
});

test('cztery nowe regiony trafiają do klatki', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  await calibrate(p, 'battle', [400, 60], [559, 279]);
  await calibrate(p, 'hp', [20, 300], [119, 307]);
  await calibrate(p, 'mana', [20, 312], [119, 319]);
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  await armNow(p);

  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();

  const frame = p.requests.find(r => r.url === '/api/frame');
  assert.ok(frame, 'nie wysłano żadnej klatki');
  // Header byte 5 is the region count: the minimap plus the four new ones.
  assert.equal(new Uint8Array(frame.body)[5], 5);
});

test('kalibracja minimapy nie rusza prostokątów widzenia', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  const before = lastConfig(p).brain.combat.viewport;
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  assert.deepEqual(lastConfig(p).brain.combat.viewport, before);
});

test('podgląd widzenia nie jest pobierany, dopóki nie jest włączony', async () => {
  const seen = {combat: {calibrated: true}};
  const p = panel({
    state: seen,
    onRequest: url => url === '/api/frame'
      ? {ok: true, async json() { return seen; }}
      : null,
  });
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  await armNow(p);
  const visionCalls = () => p.requests.filter(r => r.url === '/api/vision').length;
  assert.equal(visionCalls(), 0, 'podgląd pobrany, choć wyłączony');

  openTab(p, 'walka');
  p.el('vision-preview').checked = true;
  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  assert.ok(visionCalls() >= 1, 'włączony podgląd nie pobrał widzenia');
});

// The preview costs a request on every frame, ten times a second. Paying that
// for a canvas nobody can see is the whole reason the tab strip is asked.
test('podgląd widzenia nie jedzie, gdy jego zakładka jest schowana', async () => {
  const seen = {combat: {calibrated: true}};
  const p = panel({
    state: seen,
    onRequest: url => url === '/api/frame'
      ? {ok: true, async json() { return seen; }}
      : null,
  });
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  await armNow(p);

  openTab(p, 'pozycja');
  p.el('vision-preview').checked = true;
  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();

  assert.equal(p.requests.filter(r => r.url === '/api/vision').length, 0,
    'podgląd pobrany mimo schowanej zakładki');
});

test('kliknięcie własnego paska na podglądzie wypełnia jego pozycję', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  p.el('bar-width').value = '13';
  p.el('bar-height').value = '4';
  // The preview has not received any data yet, so the canvas still has the
  // size from the HTML; the test sets it explicitly so the click maps 1:1.
  p.el('vision-canvas').width = 176;
  p.el('vision-canvas').height = 176;
  p.el('vision-canvas').fire('pointerdown', {clientX: 88, clientY: 76});
  await p.settled();
  // The click lands on the bar's centre; its top-left corner is what is stored.
  assert.equal(p.el('self-bar-x').value, 82);
  assert.equal(p.el('self-bar-y').value, 74);
});

// Go marshals an unset creature list as JSON null (vision.Find returns a nil
// slice when the screen holds no creature bar, which is the common case), so
// the preview must render an empty screen instead of throwing on it.
test('podgląd widzenia nie wywraca się na pustym ekranie (bars: null)', async () => {
  const p = panel({
    onRequest: url => {
      if (url === '/api/frame') return {ok: true, async json() { return {combat: {calibrated: true}}; }};
      if (url === '/api/vision') {
        return {ok: true, async json() { return {have: true, crop_w: 176, crop_h: 176, bars: null}; }};
      }
      return null;
    },
  });
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  openTab(p, 'walka');
  p.el('vision-preview').checked = true;
  await armNow(p);

  p.el('video').currentTime = 1.5;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();

  assert.equal(p.el('vision-info').textContent, 'Nie widzę żadnego stwora.');
});

// Mirrors 'zmiana rozdzielczości źródła rozbraja i kasuje zaznaczenie' above,
// but for a vision rectangle instead of the minimap: the failure mode is a
// stale crop from the previous resolution still being cut and posted.
test('zmiana rozdzielczości źródła kasuje też prostokąty widzenia', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  await armNow(p);

  p.el('video').currentTime = 1;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  const before = p.requests.filter(r => r.url === '/api/frame').length;
  assert.ok(before >= 1, 'nie wysłano klatki przed zmianą rozdzielczości');

  p.el('video').videoWidth = 1024;
  p.el('snapshot').click();
  await p.settled();

  // Re-arming without recalibrating must still send nothing: if the old
  // viewport rectangle survived, the panel would keep cutting and posting a
  // crop measured against a screen that no longer exists.
  await armNow(p);
  p.el('video').currentTime = 9;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  assert.equal(p.requests.filter(r => r.url === '/api/frame').length, before,
    'panel dalej wysyła klatki prostokątem widzenia sprzed zmiany rozdzielczości');
});

// The existing minimap-then-vision test above covers one order; the two
// orders run through different pointerup branches, so both need covering.
test('kalibracja prostokąta widzenia po minimapie nie rusza jej regionu', async () => {
  const p = panel();
  await p.settled();
  await shareOnly(p);
  await calibrate(p, 'minimap', [0, 0], [105, 108]);
  await armNow(p);

  p.el('video').currentTime = 1;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  const withMinimapOnly = new Uint8Array(p.requests.find(r => r.url === '/api/frame').body)[5];
  assert.equal(withMinimapOnly, 1, 'oczekiwano samej minimapy przed kalibracją okna gry');

  await calibrate(p, 'viewport', [100, 50], [339, 225]);
  p.el('video').currentTime = 2;
  p.tick();
  await p.settled();

  const last = p.requests.filter(r => r.url === '/api/frame').at(-1);
  // If calibrating the vision rectangle had cleared the minimap's region,
  // the count would still be one instead of growing to two.
  assert.equal(new Uint8Array(last.body)[5], 2,
    'region minimapy zniknął po kalibracji okna gry');
});

test('wskaźnik widzenia pokazuje potwory, cel liczony od jedynki i niewiarygodny odczyt HP jako kreskę', async () => {
  const p = panel({state: {
    combat: {
      calibrated: true, bars_total: 4, monsters_in_range: 3, rejected_by_map: 1,
      mixed_crowd: true, battle_rows: 5, battle_truncated: false, target_row: 2,
      hp_pct: 0.42, hp_ok: false, mana_pct: 0.77, mana_ok: true,
    },
    match: {}, route: {}, executor: {}, recorder: {},
  }});
  await p.settled();

  assert.equal(p.el('vision-monsters').textContent, '3 (mieszany tłum)');
  assert.equal(p.el('vision-bars').textContent, '4');
  assert.equal(p.el('vision-rejected').textContent, '1');
  assert.equal(p.el('vision-rows').textContent, '5');
  // target_row jest liczone od zera na drucie; człowiek czyta wiersze od jedynki.
  assert.equal(p.el('vision-target').textContent, '3');
  // hp_ok: false - kalibracja najpewniej się rozjechała, więc kreska, nie 42%.
  assert.equal(p.el('vision-hp').textContent, '—');
  assert.equal(p.el('vision-mana').textContent, '77%');
});

test('wskaźnik widzenia pokazuje same kreski bez kalibracji', async () => {
  const p = panel({state: {match: {}, route: {}, executor: {}, recorder: {}}});
  await p.settled();

  for (const id of ['vision-monsters', 'vision-bars', 'vision-rejected', 'vision-rows', 'vision-target', 'vision-hp', 'vision-mana']) {
    assert.equal(p.el(id).textContent, '—', `${id} powinno pokazać kreskę bez state.combat`);
  }

  // Ten sam wynik, gdy combat istnieje, ale calibrated jest false - reguła
  // mówi "combat brak LUB calibrated false", nie tylko brak pola.
  const p2 = panel({state: {
    combat: {calibrated: false, monsters_in_range: 9, hp_ok: true, hp_pct: 1},
    match: {}, route: {}, executor: {}, recorder: {},
  }});
  await p2.settled();
  assert.equal(p2.el('vision-monsters').textContent, '—');
  assert.equal(p2.el('vision-hp').textContent, '—');
});
