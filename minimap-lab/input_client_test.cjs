const test = require('node:test');
const assert = require('node:assert');
const {InputClient} = require('./web/input.js');

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
test('krok pulsu w locie w chwili stopHeartbeat() + arm() nie tworzy drugiego łańcucha', async () => {
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
  assert.equal(calls.filter((u) => u === '/api/input/status').length, 1);

  // Stop the loop while that tick is still in flight, then start a fresh
  // chain via arm() - exactly the sequence the reviewer reproduced.
  client.stopHeartbeat();
  await client.arm();

  // Now let the stale tick's fetch resolve. If it reschedules itself, a
  // second, orphaned chain starts running alongside the fresh one.
  resolveFirstPoll({ok: true, json: async () => ({armed: true})});
  await new Promise((resolve) => setTimeout(resolve, 40));

  const countBeforeFinalStop = calls.filter((u) => u === '/api/input/status').length;
  client.stopHeartbeat();
  await new Promise((resolve) => setTimeout(resolve, 40));
  const countAfterFinalStop = calls.filter((u) => u === '/api/input/status').length;

  assert.equal(countAfterFinalStop, countBeforeFinalStop,
    'polling must stop for good after the final stopHeartbeat() - a second chain would keep it growing');
});
