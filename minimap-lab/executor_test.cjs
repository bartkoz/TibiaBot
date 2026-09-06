const test = require('node:test');
const assert = require('node:assert');
const {StepExecutor} = require('./web/executor.js');

const walk = (direction = 'N', next = [100, 99]) => ({action: 'walk', direction, next});
const at = (x, y, z = 7) => ({x, y, z});

test('pierwszy krok jest wysyłany od razu', () => {
  const ex = new StepExecutor();
  assert.deepEqual(ex.intentFor(walk(), 0), {action: 'walk', direction: 'N'});
});

test('drugi krok nie idzie, dopóki pierwszy nie jest potwierdzony', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk(), 0);
  ex.emitted(10, ex.state().stepId);

  assert.equal(ex.intentFor(walk(), 20), null);
});

test('klatka sprzed emisji nie jest dowodem wykonania kroku', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(100, ex.state().stepId);

  // Captured before the key was sent, even though it arrived after.
  ex.observe(at(100, 99), 50, 120);

  assert.equal(ex.state().waiting, true);
});

test('klatka po emisji z docelową kratką kończy krok', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(100, ex.state().stepId);

  ex.observe(at(100, 99), 150, 160);

  assert.equal(ex.state().waiting, false);
  assert.deepEqual(ex.intentFor(walk('N', [100, 98]), 170), {action: 'walk', direction: 'N'});
});

test('brak ruchu przed timeoutem nie powtarza kroku', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0, ex.state().stepId);
  ex.observe(at(100, 100), 500, 510);

  assert.equal(ex.intentFor(walk('N', [100, 99]), 900), null);
});

test('brak ruchu po timeoucie powtarza krok raz', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0, ex.state().stepId);
  ex.observe(at(100, 100), 500, 510);

  const retry = ex.intentFor(walk('N', [100, 99]), 1100);

  assert.deepEqual(retry, {action: 'walk', direction: 'N'});
  assert.equal(ex.state().retries, 1);
});

test('druga porażka tego samego kroku zgłasza blokadę', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0, ex.state().stepId);
  ex.intentFor(walk('N', [100, 99]), 1100); // retry
  ex.emitted(1100, ex.state().stepId);

  const third = ex.intentFor(walk('N', [100, 99]), 2200);

  assert.equal(third, null);
  assert.equal(ex.state().blocked, true);
});

test('trzy różne cele nieudane pod rząd zatrzymują wykonawcę', () => {
  // The realistic scenario for a hard backstop: the route gets recomputed
  // after each failure (a different target every time), and it still does
  // not work. cycles must keep counting across those replans - only a
  // confirmed step resets it, never becoming blocked.
  const ex = new StepExecutor({stepTimeoutMS: 100, maxFailedCycles: 3});

  assert.ok(ex.intentFor(walk('N', [100, 99]), 0));
  ex.emitted(0, ex.state().stepId);
  assert.ok(ex.intentFor(walk('N', [101, 99]), 200)); // first target timed out
  ex.emitted(200, ex.state().stepId);
  assert.ok(ex.intentFor(walk('N', [102, 99]), 400)); // second timed out; a fresh target gets its own retry, not a block
  ex.emitted(400, ex.state().stepId);

  assert.equal(ex.intentFor(walk('N', [102, 99]), 600), null); // third timed out too
  assert.equal(ex.state().stopped, true);
  assert.equal(ex.intentFor(walk(), 5000), null);
});

test('maxFailedCycles inny niż domyślny zatrzymuje wcześniej', () => {
  // Every other test either omits the option or passes the default (3);
  // this proves the option actually takes effect rather than being ignored.
  const ex = new StepExecutor({stepTimeoutMS: 100, maxFailedCycles: 2});
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0, ex.state().stepId);
  assert.ok(ex.intentFor(walk('N', [100, 99]), 200)); // first failure -> one retry allowed
  ex.emitted(200, ex.state().stepId);

  // Second failure hits maxFailedCycles: 2 and stops, instead of reporting
  // blocked the way the default of 3 would at this point.
  assert.equal(ex.intentFor(walk('N', [100, 99]), 400), null);
  assert.equal(ex.state().stopped, true);
});

test('blokada trzyma cel, ale inny cel ją czyści', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0, ex.state().stepId);
  ex.intentFor(walk('N', [100, 99]), 1100); // retry
  ex.emitted(1100, ex.state().stepId);
  ex.intentFor(walk('N', [100, 99]), 2200); // blocked
  assert.equal(ex.state().blocked, true);

  // Same target again: still refused, the block is not decorative.
  assert.equal(ex.intentFor(walk('N', [100, 99]), 2300), null);
  assert.equal(ex.state().blocked, true);

  // A different target: the route was recomputed, so the block lifts.
  assert.deepEqual(ex.intentFor(walk('N', [105, 99]), 2400), {action: 'walk', direction: 'N'});
  assert.equal(ex.state().blocked, false);
});

test('inny cel po jednej porażce dostaje własną szansę, nie od razu blokadę', () => {
  // Reproduces the bug where retries carried over across a replan: target A
  // fails once, the follower moves on to a brand-new target B, and B's own
  // first failure must not inherit A's already-spent retry.
  const ex = new StepExecutor({stepTimeoutMS: 100});
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0, ex.state().stepId);

  const forB = ex.intentFor(walk('N', [101, 99]), 200); // A timed out, follower replans to B
  assert.ok(forB);
  ex.emitted(200, ex.state().stepId);

  // B's first failure: must be retried, not blocked.
  const retryForB = ex.intentFor(walk('N', [101, 99]), 400);

  assert.deepEqual(retryForB, {action: 'walk', direction: 'N'});
  assert.equal(ex.state().blocked, false);
  assert.equal(ex.state().retries, 1);
});

test('nieznana pozycja natychmiast wstrzymuje ruch', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk(), 0);
  ex.emitted(10, ex.state().stepId);

  ex.observe(null, 100, 110);

  assert.equal(ex.state().halted, true);
  assert.equal(ex.intentFor(walk(), 120), null);
});

test('powrót poprawnego odczytu odblokowuje wykonawcę', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk(), 0);
  ex.emitted(10, ex.state().stepId);
  ex.observe(null, 100, 110);

  ex.observe(at(100, 100), 200, 210);

  assert.equal(ex.state().halted, false);
  assert.ok(ex.intentFor(walk(), 220));
});

test('nieoczekiwana kratka porzuca krok zamiast liczyć go jako nieudany', () => {
  const ex = new StepExecutor();
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(10, ex.state().stepId);

  // Pushed by a creature, or the player took over: not a failed step.
  ex.observe(at(105, 120), 100, 110);

  assert.equal(ex.state().waiting, false);
  assert.equal(ex.state().retries, 0, 'przesunięcie postaci to nie jest nieudany krok');
});

test('schody są pokonywane krokiem w stronę następnego waypointa', () => {
  const ex = new StepExecutor();
  const stairs = {action: 'transition', index: 1,
    waypoint: {x: 100, y: 100, z: 7, type: 'stairs'},
    next: {x: 101, y: 100, z: 6}};
  ex.observe(at(100, 100, 7), 0, 0);

  // The stairs tile is on the current floor, so this is a walk. What makes it
  // a transition is that the proof is a changed floor, not a reached tile.
  assert.deepEqual(ex.intentFor(stairs, 10), {action: 'walk', direction: 'E'});
  ex.emitted(20, ex.state().stepId);

  ex.observe(at(101, 100, 6), 100, 110);

  assert.equal(ex.state().waiting, false);
  assert.equal(ex.state().actionDone, false, 'krok na schody nie zajmuje slotu akcji w driverze');
});

test('schody bez następnego waypointa nie dają kierunku', () => {
  const ex = new StepExecutor();
  ex.observe(at(100, 100, 7), 0, 0);

  const intent = ex.intentFor({action: 'transition', index: 0,
    waypoint: {x: 100, y: 100, z: 7, type: 'stairs'}, next: null}, 10);

  assert.equal(intent, null);
});

test('schody bez wcześniejszej obserwacji nie dają kierunku', () => {
  // Same guard, other half: next exists but there is no known tile to step
  // from, so no direction can be computed either.
  const ex = new StepExecutor();
  const stairs = {action: 'transition', index: 1,
    waypoint: {x: 100, y: 100, z: 7, type: 'stairs'},
    next: {x: 101, y: 100, z: 6}};

  assert.equal(ex.intentFor(stairs, 0), null);
});

test('schody z lądowaniem na tej samej kratce nie wysyłają pustego kierunku', () => {
  // Recording a stairs waypoint and its landing point at the same x,y is
  // normal. stepDirection(from, next) then computes dx=dy=0, which the
  // compass maps to '' - a direction the driver refuses as "nieznany
  // kierunek". That must never be sent; the human climbs these manually.
  const ex = new StepExecutor();
  const stairs = {action: 'transition', index: 1,
    waypoint: {x: 100, y: 100, z: 7, type: 'stairs'},
    next: {x: 100, y: 100, z: 6}};
  ex.observe(at(100, 100, 7), 0, 0);

  const intent = ex.intentFor(stairs, 10);

  assert.equal(intent, null);
  assert.equal(ex.state().waiting, false, 'no pending step must be left behind for an empty direction');
});

test('akcja piętra czeka na zmianę Z, nie na kratkę', () => {
  const ex = new StepExecutor({actionTimeoutMS: 5000});
  const rope = {action: 'transition', index: 3, waypoint: {x: 100, y: 100, z: 7, type: 'rope'}};

  assert.deepEqual(ex.intentFor(rope, 0), {action: 'transition', type: 'rope', waypoint: 3});
  ex.emitted(10, ex.state().stepId);

  ex.observe(at(100, 100, 7), 200, 210);
  assert.equal(ex.intentFor(rope, 220), null, 'to samo piętro nie kończy akcji');

  ex.observe(at(100, 100, 6), 400, 410);
  assert.equal(ex.state().waiting, false);
  assert.equal(ex.state().actionDone, true);
});

test('nieoczekiwana kratka podczas akcji piętra porzuca krok zamiast liczyć porażkę', () => {
  const ex = new StepExecutor({actionTimeoutMS: 5000});
  const rope = {action: 'transition', index: 3, waypoint: {x: 100, y: 100, z: 7, type: 'rope'}};
  ex.observe(at(100, 100, 7), 0, 0);

  ex.intentFor(rope, 10);
  ex.emitted(20, ex.state().stepId);

  // Pushed elsewhere on the same floor: no floor change, so no proof either
  // way, but the situation changed - this must not be charged as a failure.
  ex.observe(at(120, 140, 7), 100, 110);

  assert.equal(ex.state().waiting, false);
  assert.equal(ex.state().retries, 0, 'przesunięcie postaci to nie jest nieudany krok');
});

test('krok bez potwierdzenia emisji jest porzucany po czasie i liczy się jako porażka', () => {
  // emitted() is never called here, simulating a rejected fetch: the caller
  // never told the executor the key actually left the driver.
  const ex = new StepExecutor({stepTimeoutMS: 100});
  ex.intentFor(walk('N', [100, 99]), 0);

  assert.equal(ex.state().awaitingEmit, true);
  assert.equal(ex.intentFor(walk('N', [100, 99]), 150), null, 'wciąż w oknie łaski');

  const retry = ex.intentFor(walk('N', [100, 99]), 250);

  assert.deepEqual(retry, {action: 'walk', direction: 'N'});
  assert.equal(ex.state().retries, 1);
  assert.equal(ex.state().awaitingEmit, true);
});

test('spóźnione potwierdzenie porzuconego kroku nie przesuwa terminu następnego', () => {
  const ex = new StepExecutor({stepTimeoutMS: 100});
  ex.intentFor(walk('N', [100, 99]), 0);
  const id1 = ex.state().stepId;
  // No emitted(id1): the confirmation for step 1 never arrives in time.

  // Grace period (2 * stepTimeoutMS = 200ms) expires: step 1 is dropped as a
  // failed cycle, and step 2 (its retry) is sent in the same call.
  ex.intentFor(walk('N', [100, 99]), 250);
  const id2 = ex.state().stepId;
  assert.notEqual(id2, id1);
  ex.emitted(300, id2);

  // Step 1's late confirmation finally arrives, long after it was abandoned.
  ex.emitted(310, id1);

  // Had that stamped step 2's emittedAt to 310 instead of being ignored,
  // step 2 would still look in flight here (310 + stepTimeoutMS = 410 > 405).
  // It must instead have timed out from its real baseline of 300
  // (300 + 100 = 400 <= 405), reporting its second failure as blocked.
  ex.intentFor(walk('N', [100, 99]), 405);
  assert.equal(ex.state().blocked, true);
  assert.equal(ex.state().waiting, false);
});
