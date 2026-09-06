const test = require('node:test');
const assert = require('node:assert');
const {BlocksClient} = require('./web/blocks.js');

const okJSON = (body) => ({ok: true, status: 200, json: async () => body});
const okBytes = (bytes, headers = {}) => ({
  ok: true, status: 200,
  headers: {get: (k) => headers[k.toLowerCase()] ?? null},
  arrayBuffer: async () => new Uint8Array(bytes).buffer,
});

test('obserwacja idzie na endpoint jako JSON', async () => {
  const calls = [];
  const c = new BlocksClient({fetch: async (url, init) => { calls.push([url, JSON.parse(init.body)]); return okJSON({result: 'temp', reason: 'ok'}); }});
  const d = await c.report({from: {x: 1, y: 2, z: 7}, to: {x: 1, y: 1, z: 7}, outcome: 'no_motion', still_frames: 3, last_frame_age_ms: 100});
  assert.equal(d.result, 'temp');
  assert.equal(calls[0][0], '/api/blocks/observe');
  assert.equal(calls[0][1].outcome, 'no_motion');
});

test('odrzucone żądanie nie wybucha', async () => {
  const c = new BlocksClient({fetch: async () => { throw new Error('sieć'); }});
  assert.equal(await c.report({outcome: 'no_motion'}), null);
});

test('odpowiedź z błędem HTTP nie jest brana za decyzję', async () => {
  const c = new BlocksClient({fetch: async () => ({ok: false, status: 503, json: async () => ({result: 'temp'})})});
  assert.equal(await c.report({outcome: 'no_motion'}), null);
});

test('okno podglądu wraca jako bajty z pochodzeniem i rewizją', async () => {
  const c = new BlocksClient({fetch: async () => okBytes([0, 1, 2, 8], {'x-grid-origin': '100,200', 'x-grid-revision': '5'})});
  const w = await c.window(101, 201, 7, 1);
  assert.deepEqual(w.origin, [100, 200]);
  assert.equal(w.revision, 5);
  assert.equal(w.cells.length, 4);
  assert.equal(w.cells[3], 8);
});

test('nie ma dwóch żądań okna naraz', async () => {
  let inFlight = 0, peak = 0;
  const c = new BlocksClient({fetch: async () => {
    peak = Math.max(peak, ++inFlight);
    await new Promise(r => setTimeout(r, 5));
    inFlight--;
    return okBytes([0], {'x-grid-origin': '0,0', 'x-grid-revision': '1'});
  }});
  await Promise.all([c.window(0, 0, 7, 0), c.window(0, 0, 7, 0), c.window(0, 0, 7, 0)]);
  assert.equal(peak, 1, 'a second window request went out while the first was still running');
});

test('podgląd odświeża się po zmianie kratki albo po czasie', () => {
  let clock = 0;
  const c = new BlocksClient({minIntervalMS: 500, now: () => clock});
  assert.equal(c.shouldRefresh({x: 10, y: 10, z: 7}, 0), true, 'pierwszy odczyt zawsze odświeża');
  c.lastTile = '10,10,7'; c.lastAt = 0;
  assert.equal(c.shouldRefresh({x: 10, y: 10, z: 7}, 100), false, 'ta sama kratka zaraz po odczycie');
  assert.equal(c.shouldRefresh({x: 11, y: 10, z: 7}, 150), true, 'zmiana kratki');
  assert.equal(c.shouldRefresh({x: 10, y: 10, z: 7}, 700), true, 'upłynął pełny odstęp');
  assert.equal(c.shouldRefresh({x: 10, y: 10, z: 6}, 100), true, 'zmiana piętra to inna kratka');
});

test('brak pozycji nie wywołuje odświeżenia', () => {
  const c = new BlocksClient();
  assert.equal(c.shouldRefresh(null, 1000), false);
});

test('usunięcie blokady zwraca wynik serwera', async () => {
  const c = new BlocksClient({fetch: async () => okJSON({cleared: true})});
  assert.equal(await c.remove(1, 2, 7), true);
});

test('nieudane usunięcie nie udaje sukcesu', async () => {
  const c = new BlocksClient({fetch: async () => { throw new Error('sieć'); }});
  assert.equal(await c.remove(1, 2, 7), false);
});

test('raporty idą po kolei, nigdy równolegle', async () => {
  // A failure and its revocation are reported from separate ticks. If the
  // revocation overtakes the failure on the wire, the server clears a block
  // that does not exist yet and then creates it - marking a tile the character
  // is standing on as unreachable.
  const order = [];
  const c = new BlocksClient({fetch: async (url, init) => {
    const body = JSON.parse(init.body);
    order.push(`start:${body.outcome}`);
    await new Promise(r => setTimeout(r, body.outcome === 'no_motion' ? 20 : 1));
    order.push(`end:${body.outcome}`);
    return okJSON({result: 'temp', reason: ''});
  }});
  await Promise.all([
    c.report({outcome: 'no_motion'}),
    c.report({outcome: 'entered'}),
  ]);
  assert.deepEqual(order, ['start:no_motion', 'end:no_motion', 'start:entered', 'end:entered']);
});

test('błąd jednego raportu nie blokuje kolejnych', async () => {
  let calls = 0;
  const c = new BlocksClient({fetch: async () => {
    if (++calls === 1) throw new Error('sieć');
    return okJSON({result: 'temp', reason: ''});
  }});
  assert.equal(await c.report({outcome: 'no_motion'}), null);
  assert.notEqual(await c.report({outcome: 'entered'}), null);
});
