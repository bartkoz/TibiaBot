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
