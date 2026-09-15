// Skąd biorą się piksele: demo, udostępniony ekran, zapis klatki.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel, shareAndSelect, armNow, drag} from './harness.mjs';

test('demo uruchamia dopasowanie od razu', async () => {
  const p = panel();
  await p.settled();
  p.el('demo').click();
  await p.settled();
  const request = p.requests.find(r => r.url === '/api/locate');
  assert.ok(request);
  assert.equal(JSON.parse(request.body.get('options')).demo, true);
});

test('podgląd udostępnionego ekranu odświeża się bez wysyłania konfiguracji co klatkę', async () => {
  const p = panel();
  await p.settled();
  await shareAndSelect(p);
  const draws = p.draws();
  const configs = p.requests.filter(r => r.url === '/api/config').length;
  p.el('video').currentTime = 2;
  p.tick();
  await p.settled();
  assert.ok(p.draws() > draws);
  assert.equal(p.requests.filter(r => r.url === '/api/config').length, configs);
});

// Losing the source resolution means the minimap is no longer where it was,
// so the panel must stop rather than keep posting the wrong rectangle.
test('zmiana rozdzielczości źródła rozbraja i kasuje zaznaczenie', async () => {
  const p = panel();
  await p.settled();
  p.el('share').click();
  await p.settled();
  drag(p.el('screen'), [10, 10], [110, 110]);
  await p.settled();
  await armNow(p);

  p.el('video').videoWidth = 1024;
  p.el('snapshot').click();
  await p.settled();
  assert.ok(p.requests.some(r => r.url === '/api/disarm'), 'nie rozbrojono po zmianie rozdzielczości');

  // The old rectangle must be gone too, not merely unarmed: arming again
  // without selecting the minimap afresh must still send nothing.
  await armNow(p);
  const before = p.requests.filter(r => r.url === '/api/frame').length;
  p.el('video').currentTime = 9;
  p.el('live').checked = true;
  p.el('live').fire('change');
  p.tick();
  await p.settled();
  assert.equal(p.requests.filter(r => r.url === '/api/frame').length, before,
    'panel dalej wysyła klatki prostokątem sprzed zmiany rozdzielczości');
});

test('zapis pełnej klatki tworzy pobranie w rozdzielczości źródła', async () => {
  const p = panel();
  await p.settled();
  p.el('share').click();
  await p.settled();
  // Podglądamy tworzenie elementów, bo pobranie to element <a> z atrybutem
  // download - w sandboxie nie ma prawdziwego DOM, żeby je zobaczyć inaczej.
  const created = [];
  const make = p.sandbox.document.createElement;
  p.sandbox.document.createElement = tag => {
    const el = make(tag);
    created.push(el);
    return el;
  };
  p.el('frame-save').click();
  await p.settled();
  const link = created.find(el => el.download);
  assert.ok(link, 'nie utworzono odnośnika pobrania');
  assert.equal(link.download, 'combat-capture.png');
  assert.equal(link.href, 'blob:x');
});
