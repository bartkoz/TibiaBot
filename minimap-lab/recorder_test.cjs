const {test} = require('node:test');
const assert = require('node:assert/strict');
const {RouteRecorder} = require('./web/recorder.js');
const pos = (x, y, z = 7) => ({x, y, z});

test('a manual waypoint records the current tile as walkable ground', () => {
  const r = new RouteRecorder();
  r.addManual(pos(100, 200, 7));
  assert.deepEqual(r.waypoints, [{x: 100, y: 200, z: 7, type: 'walk', label: ''}]);
});

test('automatic recording starts with the tile the player stands on', () => {
  const r = new RouteRecorder({every: 10, auto: true});
  r.observe(pos(100, 200));
  assert.equal(r.waypoints.length, 1);
});

test('automatic recording waits for the configured distance', () => {
  const r = new RouteRecorder({every: 10, auto: true});
  r.observe(pos(100, 200));
  r.observe(pos(105, 200));
  assert.equal(r.waypoints.length, 1, 'five tiles is not ten');
  r.observe(pos(110, 200));
  assert.equal(r.waypoints.length, 2);
});

test('distance counts diagonals as single tiles', () => {
  const r = new RouteRecorder({every: 10, auto: true});
  r.observe(pos(100, 200));
  r.observe(pos(106, 206));
  assert.equal(r.waypoints.length, 1, 'six diagonal steps are six tiles walked, not twelve');
  r.observe(pos(110, 210));
  assert.equal(r.waypoints.length, 2, 'ten diagonal steps reach the threshold');
});

test('recording nothing while switched off, not even floor changes', () => {
  const r = new RouteRecorder({every: 10});
  r.observe(pos(100, 200, 7));
  r.observe(pos(100, 200, 6));
  assert.equal(r.waypoints.length, 0);
});

test('a floor change records the tile before it and the tile after', () => {
  const r = new RouteRecorder({every: 10, auto: true});
  r.observe(pos(100, 200, 7));
  r.observe(pos(101, 201, 7));
  r.observe(pos(101, 201, 6));

  assert.equal(r.waypoints.length, 3);
  const [, before, after] = r.waypoints;
  assert.deepEqual({x: before.x, y: before.y, z: before.z}, {x: 101, y: 201, z: 7},
    'the action happens on the old floor');
  assert.deepEqual({x: after.x, y: after.y, z: after.z}, {x: 101, y: 201, z: 6});
  assert.equal(after.type, 'walk');
});

test('going up without moving is guessed as a rope', () => {
  const r = new RouteRecorder({every: 10, auto: true});
  r.observe(pos(100, 200, 7));
  r.observe(pos(100, 200, 6));
  assert.equal(r.waypoints[1].type, 'rope');
});

test('going down without moving is guessed as a hole', () => {
  const r = new RouteRecorder({every: 10, auto: true});
  r.observe(pos(100, 200, 7));
  r.observe(pos(100, 200, 8));
  assert.equal(r.waypoints[1].type, 'hole');
});

test('a floor change that shifts the tile is guessed as stairs', () => {
  const r = new RouteRecorder({every: 10, auto: true});
  r.observe(pos(100, 200, 7));
  r.observe(pos(101, 200, 6));
  assert.equal(r.waypoints[1].type, 'stairs');
});

test('a floor change is recorded even right after another waypoint', () => {
  const r = new RouteRecorder({every: 10, auto: true});
  r.observe(pos(100, 200, 7));
  r.observe(pos(100, 200, 6));
  r.observe(pos(100, 200, 5));
  assert.equal(r.waypoints.length, 5, 'each transition adds its own pair');
});

test('the recorder stops at the file format limit', () => {
  const r = new RouteRecorder({every: 1, auto: true});
  for (let i = 0; i < 1200; i++) r.observe(pos(100 + i, 200));
  assert.equal(r.waypoints.length, 1000);
});

test('manual and automatic points share one list', () => {
  const r = new RouteRecorder({every: 10, auto: true});
  r.observe(pos(100, 200));
  r.addManual(pos(103, 200));
  r.observe(pos(105, 200));
  assert.equal(r.waypoints.length, 2, 'the manual point resets the distance counter');
  r.observe(pos(113, 200));
  assert.equal(r.waypoints.length, 3);
});
