const {test} = require('node:test');
const assert = require('node:assert/strict');
const {RouteFollower} = require('./web/follower.js');

const wp = (x, y, z = 7, type = 'walk') => ({x, y, z, type, label: ''});
const pos = (x, y, z = 7) => ({x, y, z});
// A straight eastward path the server would return for from -> to.
const straight = (from, to) => {
  const steps = [];
  for (let x = from.x; x <= to.x; x++) steps.push([x, from.y]);
  return {found: true, status: 'ok', steps, tiles: steps.length - 1};
};

test('an empty route is finished before it starts', () => {
  assert.equal(new RouteFollower([]).step(pos(10, 10), 0).action, 'done');
});

test('reaching the last waypoint finishes the route', () => {
  const f = new RouteFollower([wp(10, 10)]);
  assert.equal(f.step(pos(10, 10), 0).action, 'done');
});

test('standing on a waypoint advances to the next one', () => {
  const f = new RouteFollower([wp(10, 10), wp(20, 10)]);
  f.step(pos(10, 10), 0);
  assert.equal(f.index, 1);
});

test('a diagonal neighbour counts as reaching the waypoint', () => {
  const f = new RouteFollower([wp(10, 10), wp(20, 10)]);
  f.step(pos(11, 11), 0);
  assert.equal(f.index, 1, 'Chebyshev distance 1 is within the default tolerance');
});

test('a waypoint two tiles away is not reached yet', () => {
  const f = new RouteFollower([wp(10, 10), wp(20, 10)]);
  f.step(pos(12, 10), 0);
  assert.equal(f.index, 0);
});

test('without a path the follower asks for one', () => {
  const f = new RouteFollower([wp(20, 10)]);
  const out = f.step(pos(10, 10), 0);
  assert.equal(out.action, 'path');
  assert.deepEqual(out.from, pos(10, 10));
  assert.deepEqual(out.to, pos(20, 10));
});

test('a path turns into a walking direction', () => {
  const f = new RouteFollower([wp(20, 10)]);
  f.step(pos(10, 10), 0);
  f.setPath(straight(pos(10, 10), pos(20, 10)), 10, {x: 20, y: 10, z: 7});
  const out = f.step(pos(10, 10), 20);
  assert.equal(out.action, 'walk');
  assert.equal(out.direction, 'E');
  assert.deepEqual(out.next, [11, 10]);
  assert.equal(out.remaining, 10, 'ten tiles left to the waypoint');
});

test('every compass direction comes out of the next step', () => {
  const cases = {N: [10, 9], NE: [11, 9], E: [11, 10], SE: [11, 11], S: [10, 11], SW: [9, 11], W: [9, 10], NW: [9, 9]};
  for (const [want, next] of Object.entries(cases)) {
    // The waypoint sits far enough away that it is not reached on the spot.
    const far = wp(10 + (next[0] - 10) * 9, 10 + (next[1] - 10) * 9);
    const f = new RouteFollower([far]);
    f.step(pos(10, 10), 0);
    f.setPath({found: true, status: 'ok', steps: [[10, 10], next], tiles: 1}, 10, far);
    assert.equal(f.step(pos(10, 10), 20).direction, want, `direction towards ${next}`);
  }
});

test('walking along the path consumes it without asking again', () => {
  const f = new RouteFollower([wp(20, 10)]);
  f.step(pos(10, 10), 0);
  f.setPath(straight(pos(10, 10), pos(20, 10)), 10, {x: 20, y: 10, z: 7});
  const out = f.step(pos(13, 10), 5000);
  assert.equal(out.action, 'walk');
  assert.deepEqual(out.next, [14, 10]);
  assert.equal(out.remaining, 7);
});

test('stepping off the path triggers a new request once the throttle allows it', () => {
  const f = new RouteFollower([wp(20, 10)]);
  f.step(pos(10, 10), 0);
  f.setPath(straight(pos(10, 10), pos(20, 10)), 10, {x: 20, y: 10, z: 7});
  assert.equal(f.step(pos(13, 17), 100).action, 'wait', 'too soon after the last request');
  assert.equal(f.step(pos(13, 17), 700).action, 'path');
});

test('a waypoint on another floor waits for the transition instead of pathing', () => {
  const f = new RouteFollower([wp(10, 10, 6, 'rope')]);
  const out = f.step(pos(10, 10, 7), 0);
  assert.equal(out.action, 'transition');
  assert.match(out.instruction, /lin/i);
  assert.equal(out.waypoint.z, 6);
});

test('each transition type has its own instruction', () => {
  for (const [type, pattern] of [['ladder', /drabin/i], ['stairs', /schod/i], ['hole', /dziur/i], ['shovel', /kop/i]]) {
    const f = new RouteFollower([wp(10, 10, 8, type)]);
    assert.match(f.step(pos(10, 10, 7), 0).instruction, pattern);
  }
});

test('arriving on the new floor resumes normal following', () => {
  const f = new RouteFollower([wp(10, 10, 6), wp(20, 10, 6)]);
  f.step(pos(10, 10, 7), 0);
  f.step(pos(10, 10, 6), 100);
  assert.equal(f.index, 1);
});

test('a stale path for a previous waypoint is discarded', () => {
  const f = new RouteFollower([wp(20, 10), wp(30, 10)]);
  f.step(pos(10, 10), 0);
  f.setPath(straight(pos(10, 10), pos(20, 10)), 10, {x: 20, y: 10, z: 7});
  f.step(pos(20, 10), 20);
  assert.equal(f.index, 1);
  assert.equal(f.step(pos(20, 10), 1000).action, 'path', 'the old path led to waypoint 1');
});

test('a blocked waypoint is reported rather than retried in a tight loop', () => {
  const f = new RouteFollower([wp(20, 10)]);
  f.step(pos(10, 10), 0);
  f.setPath({found: false, status: 'blocked_goal', steps: [], reason: 'nieprzechodnia'}, 10, {x: 20, y: 10, z: 7});
  const out = f.step(pos(10, 10), 20);
  assert.equal(out.action, 'blocked');
  assert.equal(out.status, 'blocked_goal');
  assert.match(out.reason, /nieprzechodnia/);
});

test('a looping route restarts at the first waypoint', () => {
  const f = new RouteFollower([wp(10, 10), wp(20, 10)], {loop: true});
  f.step(pos(10, 10), 0);
  const out = f.step(pos(20, 10), 100);
  assert.equal(f.index, 0);
  assert.notEqual(out.action, 'done');
});

test('tolerance is configurable for open ground', () => {
  const f = new RouteFollower([wp(10, 10), wp(20, 10)], {tolerance: 3});
  f.step(pos(13, 12), 0);
  assert.equal(f.index, 1);
});

test('skipping to a waypoint drops the path built for the old one', () => {
  const f = new RouteFollower([wp(20, 10), wp(30, 10)]);
  f.step(pos(10, 10), 0);
  f.setPath(straight(pos(10, 10), pos(20, 10)), 10, {x: 20, y: 10, z: 7});
  f.skipTo(1);
  assert.equal(f.step(pos(10, 10), 1000).action, 'path');
});

test('editing the current waypoint while following invalidates the path', () => {
  const waypoints = [wp(20, 10)];
  const f = new RouteFollower(waypoints);
  f.step(pos(10, 10), 0);
  f.setPath(straight(pos(10, 10), pos(20, 10)), 10, {x: 20, y: 10, z: 7});
  assert.equal(f.step(pos(10, 10), 20).action, 'walk');

  waypoints[0].x = 25; // the panel lets the user move a waypoint mid-route

  assert.equal(f.step(pos(10, 10), 1000).action, 'path', 'the path still led to the old tile');
});

test('a recorded transition pair keeps the action instruction', () => {
  // Exactly what the recorder writes for a rope: the tile before the
  // transition carries the action, the tile after it is plain walking.
  const f = new RouteFollower([wp(10, 10, 7, 'rope'), wp(10, 10, 6, 'walk')]);

  const out = f.step(pos(10, 10, 7), 0);

  assert.equal(out.action, 'transition');
  assert.match(out.instruction, /lin/i, 'standing on a rope waypoint means use the rope');
  assert.equal(f.index, 0, 'the action waypoint is not consumed by walking onto it');
});

test('an action waypoint is consumed once the floor actually changes', () => {
  const f = new RouteFollower([wp(10, 10, 7, 'rope'), wp(10, 10, 6, 'walk'), wp(20, 10, 6)]);
  f.step(pos(10, 10, 7), 0);

  f.step(pos(10, 10, 6), 100);

  assert.equal(f.index, 2, 'both the rope point and the tile after it are done');
});

test('an action waypoint still has to be walked to first', () => {
  const f = new RouteFollower([wp(20, 10, 7, 'rope')]);
  const out = f.step(pos(10, 10, 7), 0);
  assert.equal(out.action, 'path', 'ten tiles away, so walk there before using the rope');
});

test('a path that arrives after the waypoint changed is discarded', () => {
  // The next waypoint lies west, so an eastward path for the old one would
  // steer the player the wrong way.
  const f = new RouteFollower([wp(20, 10), wp(0, 10)]);
  const asked = f.step(pos(10, 10), 0);
  assert.deepEqual(asked.to, {x: 20, y: 10, z: 7});

  // The player reaches waypoint 1 while the request is still in flight.
  f.step(pos(19, 10), 100);
  assert.equal(f.index, 1);
  f.setPath(straight(pos(10, 10), pos(20, 10)), 200, asked.to);

  const out = f.step(pos(19, 10), 1000);
  assert.notEqual(out.direction, 'E', 'the stale path pointed east, the new waypoint is west');
  assert.equal(out.action, 'path');
  assert.deepEqual(out.to, {x: 0, y: 10, z: 7});
});

test('a transition completed between two readings still counts', () => {
  // The tracker samples at 10 Hz; the player can use the rope and be reported
  // on the new floor without any reading in between showing them standing on
  // the action waypoint.
  const f = new RouteFollower([wp(10, 10, 7, 'rope'), wp(10, 10, 6), wp(20, 10, 6)]);

  const out = f.step(pos(10, 10, 6), 0);

  assert.ok(out.action !== 'transition' || !/piętro 7/.test(out.instruction ?? ''),
    `must not send the player back up: ${JSON.stringify(out)}`);
  assert.equal(f.index, 2, 'the rope and the tile after it are both done');
});

test('a stale reply leaves a newer path alone', () => {
  const f = new RouteFollower([wp(20, 10), wp(40, 10)]);
  const first = f.step(pos(10, 10), 0);
  // Reaching waypoint 1 and asking for a path to waypoint 2 is one step.
  const second = f.step(pos(19, 10), 100);
  assert.deepEqual(second.to, {x: 40, y: 10, z: 7});
  f.setPath(straight(pos(19, 10), pos(40, 10)), 150, second.to);
  assert.equal(f.step(pos(19, 10), 160).action, 'walk');

  f.setPath(straight(pos(10, 10), pos(20, 10)), 200, first.to); // late reply

  assert.equal(f.step(pos(19, 10), 210).action, 'walk', 'the good path must survive');
});

test('skipping away and back does not reuse an old floor change', () => {
  const f = new RouteFollower([wp(10, 10, 7, 'rope'), wp(50, 50, 7)]);
  f.step(pos(10, 10, 7), 0);      // stands on the rope, arms the action
  f.skipTo(1);
  f.skipTo(0);

  const out = f.step(pos(10, 10, 6), 100);

  assert.equal(f.index, 0, 'a skipped-away action must be armed again, not remembered');
  assert.equal(out.action, 'transition');
});

test('a failed path is retried once the player moves', () => {
  const f = new RouteFollower([wp(30, 10)]);
  f.step(pos(10, 10), 0);
  f.setPath({found: false, status: 'error', reason: 'sieć padła'}, 10, {x: 30, y: 10, z: 7});
  assert.equal(f.step(pos(10, 10), 20).action, 'blocked');

  const out = f.step(pos(11, 10), 30);

  assert.equal(out.action, 'path', 'a new tile is a new situation, not the same failure');
});

test('a failed path is retried after a backoff even standing still', () => {
  const f = new RouteFollower([wp(30, 10)]);
  f.step(pos(10, 10), 0);
  f.setPath({found: false, status: 'blocked_goal', steps: [], reason: 'ściana'}, 10, {x: 30, y: 10, z: 7});
  assert.equal(f.step(pos(10, 10), 500).action, 'blocked', 'no hammering the server');

  assert.equal(f.step(pos(10, 10), 5000).action, 'path', 'but never give up for good');
});

test('a looped single-waypoint route settles instead of spinning', () => {
  const f = new RouteFollower([wp(10, 10)], {loop: true});

  const out = f.step(pos(10, 10), 0);

  assert.ok(out.action !== 'path', `standing on the only waypoint must not ask for a route: ${out.action}`);
});

test('a looped route whose points are all within tolerance settles too', () => {
  const f = new RouteFollower([wp(10, 10), wp(10, 11), wp(11, 10)], {loop: true});

  const out = f.step(pos(10, 10), 0);

  assert.ok(out.action !== 'path', `every point is reached at once: ${out.action}`);
});

test('waypoint akcji nie jest osiągnięty z sąsiedniej kratki', () => {
  const f = new RouteFollower([
    {x: 100, y: 100, z: 7, type: 'rope'},
    {x: 100, y: 100, z: 6, type: 'walk'},
  ], {tolerance: 1});

  const out = f.step({x: 101, y: 100, z: 7}, 0);

  // Walking tolerance may be loose; a rope used one tile off the rope spot
  // does nothing at all.
  assert.notEqual(out.action, 'transition');
});

test('waypoint akcji jest osiągnięty z dokładnie tej kratki', () => {
  const f = new RouteFollower([
    {x: 100, y: 100, z: 7, type: 'rope'},
    {x: 100, y: 100, z: 6, type: 'walk'},
  ], {tolerance: 1});

  const out = f.step({x: 100, y: 100, z: 7}, 0);

  assert.equal(out.action, 'transition');
});

test('instrukcja przejścia niesie następny waypoint', () => {
  const f = new RouteFollower([
    {x: 100, y: 100, z: 7, type: 'stairs'},
    {x: 101, y: 100, z: 6, type: 'walk'},
  ]);

  const out = f.step({x: 100, y: 100, z: 7}, 0);

  // The stairs tile sits on the current floor; the next waypoint is what says
  // which way to step onto it.
  assert.equal(out.action, 'transition');
  assert.deepEqual(out.next, {x: 101, y: 100, z: 6, type: 'walk'});
});

test('ostatni waypoint przejścia nie ma następnika', () => {
  const f = new RouteFollower([{x: 100, y: 100, z: 7, type: 'rope'}]);

  const out = f.step({x: 100, y: 100, z: 7}, 0);

  assert.equal(out.next, null);
});

test('actionTolerance można poluzować świadomie', () => {
  const f = new RouteFollower([
    {x: 100, y: 100, z: 7, type: 'stairs'},
    {x: 101, y: 100, z: 6, type: 'walk'},
  ], {tolerance: 0, actionTolerance: 1});

  const out = f.step({x: 101, y: 100, z: 7}, 0);

  // A tight walking tolerance paired with a loose action tolerance: only reading
  // actionTolerance (not falling back to tolerance) can satisfy this.
  assert.equal(out.action, 'transition');
});
