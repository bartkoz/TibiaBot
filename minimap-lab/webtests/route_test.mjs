// Trasa waypointów: wczytanie z pliku i dopisanie punktu.

import {test} from 'node:test';
import assert from 'node:assert/strict';

import {panel} from './harness.mjs';

test('wczytanie pliku trasy leci PUT-em na serwer', async () => {
  const p = panel();
  await p.settled();
  const file = {name: 'trasa.json', async text() { return '{"version":1,"waypoints":[]}'; }};
  p.el('route-file').fire('change', {target: {files: [file]}});
  await p.settled();
  const put = p.requests.find(r => r.url === '/api/route' && r.method === 'PUT');
  assert.ok(put, 'trasa nie została wysłana');
  assert.equal(put.body, '{"version":1,"waypoints":[]}');
});

test('dodanie waypointa idzie osobnym żądaniem', async () => {
  const p = panel();
  await p.settled();
  p.el('route-add').click();
  await p.settled();
  assert.ok(p.requests.some(r => r.url === '/api/route/waypoint' && r.method === 'POST'));
});
