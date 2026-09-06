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
  ex.emitted(10);

  assert.equal(ex.intentFor(walk(), 20), null);
});

test('klatka sprzed emisji nie jest dowodem wykonania kroku', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(100);

  // Captured before the key was sent, even though it arrived after.
  ex.observe(at(100, 99), 50, 120);

  assert.equal(ex.state().waiting, true);
});

test('klatka po emisji z docelową kratką kończy krok', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(100);

  ex.observe(at(100, 99), 150, 160);

  assert.equal(ex.state().waiting, false);
  assert.deepEqual(ex.intentFor(walk('N', [100, 98]), 170), {action: 'walk', direction: 'N'});
});

test('brak ruchu przed timeoutem nie powtarza kroku', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0);
  ex.observe(at(100, 100), 500, 510);

  assert.equal(ex.intentFor(walk('N', [100, 99]), 900), null);
});

test('brak ruchu po timeoucie powtarza krok raz', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0);
  ex.observe(at(100, 100), 500, 510);

  const retry = ex.intentFor(walk('N', [100, 99]), 1100);

  assert.deepEqual(retry, {action: 'walk', direction: 'N'});
  assert.equal(ex.state().retries, 1);
});

test('druga porażka tego samego kroku zgłasza blokadę', () => {
  const ex = new StepExecutor({stepTimeoutMS: 1000});
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(0);
  ex.intentFor(walk('N', [100, 99]), 1100); // retry
  ex.emitted(1100);

  const third = ex.intentFor(walk('N', [100, 99]), 2200);

  assert.equal(third, null);
  assert.equal(ex.state().blocked, true);
});

test('trzy nieudane cykle zatrzymują wykonawcę', () => {
  const ex = new StepExecutor({stepTimeoutMS: 100, maxFailedCycles: 3});
  for (let i = 0; i < 8; i++) {
    const now = i * 200;
    const intent = ex.intentFor(walk('N', [100, 99]), now);
    if (intent) ex.emitted(now);
  }

  assert.equal(ex.state().stopped, true);
  assert.equal(ex.intentFor(walk(), 5000), null);
});

test('nieznana pozycja natychmiast wstrzymuje ruch', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk(), 0);
  ex.emitted(10);

  ex.observe(null, 100, 110);

  assert.equal(ex.state().halted, true);
  assert.equal(ex.intentFor(walk(), 120), null);
});

test('powrót poprawnego odczytu odblokowuje wykonawcę', () => {
  const ex = new StepExecutor();
  ex.intentFor(walk(), 0);
  ex.emitted(10);
  ex.observe(null, 100, 110);

  ex.observe(at(100, 100), 200, 210);

  assert.equal(ex.state().halted, false);
  assert.ok(ex.intentFor(walk(), 220));
});

test('nieoczekiwana kratka porzuca krok zamiast liczyć go jako nieudany', () => {
  const ex = new StepExecutor();
  ex.observe(at(100, 100), 0, 0);
  ex.intentFor(walk('N', [100, 99]), 0);
  ex.emitted(10);

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
  ex.emitted(20);

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

test('akcja piętra czeka na zmianę Z, nie na kratkę', () => {
  const ex = new StepExecutor({actionTimeoutMS: 5000});
  const rope = {action: 'transition', index: 3, waypoint: {x: 100, y: 100, z: 7, type: 'rope'}};

  assert.deepEqual(ex.intentFor(rope, 0), {action: 'transition', type: 'rope', waypoint: 3});
  ex.emitted(10);

  ex.observe(at(100, 100, 7), 200, 210);
  assert.equal(ex.intentFor(rope, 220), null, 'to samo piętro nie kończy akcji');

  ex.observe(at(100, 100, 6), 400, 410);
  assert.equal(ex.state().waiting, false);
  assert.equal(ex.state().actionDone, true);
});
