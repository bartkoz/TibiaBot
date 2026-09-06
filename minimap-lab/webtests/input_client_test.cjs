const test = require('node:test');
const assert = require('node:assert');
const {InputClient} = require('../web/input.js');

// A fetch stand-in that records calls (url, headers, parsed body) and answers
// with the next queued reply.
function fakeFetch(replies) {
  const calls = [];
  const fetch = async (url, opts = {}) => {
    calls.push({url, headers: opts.headers ?? {}, body: opts.body ? JSON.parse(opts.body) : null});
    const reply = replies.shift() ?? {};
    return {ok: reply.ok ?? true, json: async () => reply.json ?? {}};
  };
  return {fetch, calls};
}

test('uzbrojenie zapamiętuje token', async () => {
  const {fetch, calls} = fakeFetch([{json: {armed: true, session: 'abc', target: {pid: 42}}}]);
  const client = new InputClient({fetch, beatMS: 0});

  await client.arm();

  assert.equal(client.session, 'abc');
  assert.equal(client.armed, true);
  assert.ok(calls[0].url.includes('/api/arm'));
  client.stopHeartbeat();
});

test('nieudane uzbrojenie nie ustawia stanu uzbrojonego', async () => {
  const {fetch} = fakeFetch([{ok: false, json: {armed: false, reason: 'brak zgody'}}]);
  const client = new InputClient({fetch});

  const state = await client.arm();

  assert.equal(client.armed, false);
  assert.equal(state.reason, 'brak zgody');
});

test('wiek obserwacji jedzie w żądaniu, nie znacznik czasu', async () => {
  const {fetch, calls} = fakeFetch([{json: {status: 'emitted'}}]);
  const client = new InputClient({fetch});
  client.session = 'abc';
  client.armed = true;

  await client.send({action: 'walk', direction: 'N'}, 137.4);

  assert.equal(calls[0].body.observation_age_ms, 137);
  assert.equal(calls[0].body.session, 'abc');
  assert.ok(calls[0].body.seq > 0, 'każde żądanie musi mieć rosnący seq');
});

test('config wysyła klawisze akcji i flagę kliknięcia po hotkeyu', async () => {
  const {fetch, calls} = fakeFetch([{json: {ok: true}}]);
  const client = new InputClient({fetch});
  client.session = 'abc';

  const {ok} = await client.config({rope: 'f7', ladder: '', hole: '', shovel: ''}, false);

  assert.equal(ok, true);
  assert.ok(calls[0].url.includes('/api/input/config'));
  assert.equal(calls[0].body.session, 'abc');
  assert.deepEqual(calls[0].body.keys, {rope: 'f7', ladder: '', hole: '', shovel: ''});
  assert.equal(calls[0].body.click_after_hotkey, false);
});

test('config nieudany zwraca false i powód odmowy zamiast wyjątku', async () => {
  const {fetch} = fakeFetch([{ok: false, json: {reason: 'nieznany klawisz dla akcji rope: control'}}]);
  const client = new InputClient({fetch});
  client.session = 'abc';

  const {ok, reason} = await client.config({rope: 'control'}, true);

  assert.equal(ok, false);
  assert.equal(reason, 'nieznany klawisz dla akcji rope: control');
});

test('rozbrojenie po stronie serwera zatrzymuje wysyłanie', async () => {
  const {fetch} = fakeFetch([{json: {status: 'disarmed', reason: 'okno gry straciło focus'}}]);
  const client = new InputClient({fetch});
  client.session = 'abc';
  client.armed = true;

  const result = await client.send({action: 'walk', direction: 'N'}, 100);

  assert.equal(result.status, 'disarmed');
  assert.equal(client.armed, false, 'panel musi odzwierciedlić rozbrojenie po stronie Go');
});

test('rozbrojony klient nie wysyła żądań', async () => {
  const {fetch, calls} = fakeFetch([]);
  const client = new InputClient({fetch});

  const result = await client.send({action: 'walk', direction: 'N'}, 10);

  assert.equal(result.status, 'disarmed');
  assert.equal(calls.length, 0);
});

test('seq rośnie z każdym wysłanym zamiarem', async () => {
  const {fetch, calls} = fakeFetch([{json: {status: 'emitted'}}, {json: {status: 'emitted'}}]);
  const client = new InputClient({fetch});
  client.session = 'abc';
  client.armed = true;

  await client.send({action: 'walk', direction: 'N'}, 10);
  await client.send({action: 'walk', direction: 'E'}, 10);

  assert.ok(calls[1].body.seq > calls[0].body.seq);
});

// The heartbeat is the status poll. Its token must ride in a header, never in
// the URL (query strings leak into logs, history, Referer), and a reply that
// says the driver is no longer armed must stop the polling loop locally.
test('puls sesji niesie token w nagłówku i zatrzymuje się po rozbrojeniu z odpowiedzi statusu', async () => {
  const {fetch, calls} = fakeFetch([{json: {armed: false, reason: 'utracono fokus okna gry'}}]);
  const client = new InputClient({fetch, beatMS: 5});
  client.session = 'abc';
  client.armed = true;

  client.startHeartbeat();
  await new Promise((resolve) => setTimeout(resolve, 30));

  assert.equal(calls.length, 1);
  assert.ok(calls[0].url.includes('/api/input/status'));
  assert.ok(!calls[0].url.includes('abc'), 'token nie może trafić do URL');
  assert.equal(calls[0].headers['X-Input-Session'], 'abc');
  assert.equal(client.armed, false, 'odpowiedź rozbrojona zatrzymuje puls');

  // No further polls should have been scheduled once armed went false.
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.equal(calls.length, 1);

  client.stopHeartbeat();
});

// Regression test for the fetch-rejection handling send() already had: a
// mutation that deletes its try/catch must be caught by this test alone.
test('odrzucone zapytanie fetch w send() rozwiązuje się wynikiem, nie ucieka jako wyjątek', async () => {
  const fetch = async () => { throw new Error('sieć padła'); };
  const client = new InputClient({fetch});
  client.session = 'abc';
  client.armed = true;

  const result = await client.send({action: 'walk', direction: 'N'}, 10);

  assert.equal(result.status, 'error');
});

test('puls odpytuje wielokrotnie, dopóki klient jest uzbrojony', async () => {
  const calls = [];
  const fetch = async (url) => {
    calls.push(url);
    return {ok: true, json: async () => ({armed: true})};
  };
  const client = new InputClient({fetch, beatMS: 5});
  client.session = 'abc';
  client.armed = true;

  client.startHeartbeat();
  await new Promise((resolve) => setTimeout(resolve, 40));
  client.stopHeartbeat();

  assert.ok(calls.length > 1, 'jedno uderzenie to za mało - pętla musi się powtarzać, dopóki klient jest uzbrojony');
});

test('nieudany odczyt statusu nie kończy pulsu - kolejne uderzenia nadal następują', async () => {
  let n = 0;
  const fetch = async () => {
    n++;
    if (n === 1) throw new Error('chwilowy błąd sieci');
    return {ok: true, json: async () => ({armed: true})};
  };
  const client = new InputClient({fetch, beatMS: 5});
  client.session = 'abc';
  client.armed = true;

  client.startHeartbeat();
  await new Promise((resolve) => setTimeout(resolve, 40));
  client.stopHeartbeat();

  assert.ok(n > 2, 'jedna nieudana próba nie może zakończyć pętli pulsu');
  assert.equal(client.armed, true, 'chwilowy błąd sieci nie rozbraja klienta');
});

test('powtarzające się niepowodzenia odczytu statusu w końcu zatrzymują puls i rozbrajają klienta', async () => {
  let n = 0;
  const fetch = async () => { n++; throw new Error('serwer nieosiągalny'); };
  const client = new InputClient({fetch, beatMS: 5, maxHeartbeatFailures: 3});
  client.session = 'abc';
  client.armed = true;

  client.startHeartbeat();
  await new Promise((resolve) => setTimeout(resolve, 60));

  assert.equal(client.armed, false, 'po serii niepowodzeń klient musi uznać się za rozbrojony');
  assert.equal(client.timer, null, 'pętla pulsu musi zatrzymać się na stałe, nie tylko raz');

  // Confirm it really is permanent, not just paused between retries.
  const countAfterGivingUp = n;
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.equal(n, countAfterGivingUp, 'po rezygnacji nie mogą następować kolejne próby');
});

test('rozbrojenie z odrzuconym fetch nadal ustawia armed na false', async () => {
  const fetch = async () => { throw new Error('sieć padła'); };
  const client = new InputClient({fetch});
  client.session = 'abc';
  client.armed = true;

  await client.disarm();

  assert.equal(client.armed, false, 'zamiar rozbrojenia musi się liczyć nawet gdy żądanie nie dotarło');
});

// Reproduces the orphan-chain bug: a tick already awaiting its fetch when
// stopHeartbeat() (then a fresh arm()) fires must not resurrect itself into a
// second, independent polling chain once its stale fetch finally resolves.
// The real hazard is not the extra HTTP call (the entry-of-tick generation
// check already suppresses that) - it is a stale reply landing on a session
// that has since been re-armed and corrupting *its* state. Two variants:
// a stale {armed: false} reply, and a stale rejection.

test('krok pulsu w locie, który rozwiązuje się jako {armed: false}, nie rozbraja świeżo uzbrojonej sesji', async () => {
  let resolveFirstPoll;
  const calls = [];
  const fetch = async (url) => {
    const isFirstStatusCall = url === '/api/input/status' && !calls.includes('/api/input/status');
    calls.push(url);
    if (isFirstStatusCall) {
      // Hangs until the test releases it by hand, simulating a slow reply
      // that outlives the heartbeat generation that sent it.
      return new Promise((resolve) => { resolveFirstPoll = resolve; });
    }
    if (url === '/api/arm') return {ok: true, json: async () => ({armed: true, session: 'xyz'})};
    return {ok: true, json: async () => ({armed: true})};
  };
  const client = new InputClient({fetch, beatMS: 5});
  client.session = 'abc';
  client.armed = true;
  client.startHeartbeat();

  // Let the first tick fire; its fetch is now hanging in flight.
  await new Promise((resolve) => setTimeout(resolve, 15));

  // Stop the loop while that tick is still in flight, then start a fresh
  // chain via arm() - exactly the sequence the reviewer reproduced.
  client.stopHeartbeat();
  await client.arm();
  assert.equal(client.session, 'xyz');

  const countBeforeStaleReply = calls.filter((u) => u === '/api/input/status').length;

  // The stale tick, from a superseded generation, now resolves saying the
  // driver is no longer armed. That verdict was about the OLD session and
  // must not touch the freshly re-armed one.
  resolveFirstPoll({ok: true, json: async () => ({armed: false})});
  await new Promise((resolve) => setTimeout(resolve, 30));

  assert.equal(client.armed, true,
    'a stale, superseded tick must not disarm the freshly re-armed session');
  const countAfterStaleReply = calls.filter((u) => u === '/api/input/status').length;
  assert.ok(countAfterStaleReply > countBeforeStaleReply,
    'the fresh heartbeat must keep polling - the stale reply must not cancel it');

  client.stopHeartbeat();
});

test('krok pulsu w locie, który się odrzuca, nie rozbraja i nie zatrzymuje świeżego pulsu', async () => {
  let rejectFirstPoll;
  const calls = [];
  const fetch = async (url) => {
    const isFirstStatusCall = url === '/api/input/status' && !calls.includes('/api/input/status');
    calls.push(url);
    if (isFirstStatusCall) {
      return new Promise((_resolve, reject) => { rejectFirstPoll = reject; });
    }
    if (url === '/api/arm') return {ok: true, json: async () => ({armed: true, session: 'xyz'})};
    return {ok: true, json: async () => ({armed: true})};
  };
  // A low threshold makes even a single mishandled stale failure visible
  // immediately, instead of needing several to accumulate.
  const client = new InputClient({fetch, beatMS: 5, maxHeartbeatFailures: 1});
  client.session = 'abc';
  client.armed = true;
  client.startHeartbeat();

  await new Promise((resolve) => setTimeout(resolve, 15));

  client.stopHeartbeat();
  await client.arm();
  assert.equal(client.session, 'xyz');

  const countBeforeStaleReject = calls.filter((u) => u === '/api/input/status').length;

  // The stale tick, from a superseded generation, now rejects. That failure
  // belongs to the OLD chain and must not be charged against the fresh one.
  rejectFirstPoll(new Error('sieć padła'));
  await new Promise((resolve) => setTimeout(resolve, 30));

  assert.equal(client.armed, true,
    'a stale, superseded rejection must not disarm the freshly re-armed session');
  const countAfterStaleReject = calls.filter((u) => u === '/api/input/status').length;
  assert.ok(countAfterStaleReject > countBeforeStaleReject,
    'the fresh heartbeat must keep polling - the stale rejection must not cancel it');

  client.stopHeartbeat();
});

// Without resetting the failure counter on every success, five failures
// scattered across a long session - never consecutive, always recovered
// from - would eventually disarm a perfectly healthy connection.
test('niepowodzenia przeplecione sukcesami nie sumują się do rozbrojenia', async () => {
  const outcomes = ['fail', 'ok', 'fail', 'ok', 'fail', 'ok']; // 3 failures, never consecutive
  let i = 0;
  const fetch = async () => {
    const outcome = outcomes[i++] ?? 'ok';
    if (outcome === 'fail') throw new Error('przejściowy błąd sieci');
    return {ok: true, json: async () => ({armed: true})};
  };
  // Threshold of 2: three total failures exceed it, but none of them are
  // back-to-back, so a working reset must keep the client armed throughout.
  const client = new InputClient({fetch, beatMS: 5, maxHeartbeatFailures: 2});
  client.session = 'abc';
  client.armed = true;

  client.startHeartbeat();
  await new Promise((resolve) => setTimeout(resolve, 60));
  client.stopHeartbeat();

  assert.ok(i >= outcomes.length, 'wszystkie zaplanowane próby musiały się odbyć');
  assert.equal(client.armed, true,
    'sukcesy między niepowodzeniami muszą zerować licznik, inaczej zdrowe połączenie zostanie rozbrojone bez powodu');
});
