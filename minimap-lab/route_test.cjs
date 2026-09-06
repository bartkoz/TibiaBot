const {test} = require('node:test');
const assert = require('node:assert/strict');
const {parseRoute, serializeRoute, WAYPOINT_TYPES} = require('./web/route.js');

const minimal = {version: 1, name: 'Trasa', waypoints: [{x: 32958, y: 32077, z: 7}]};

test('a waypoint without a type walks', () => {
  const route = parseRoute(JSON.stringify(minimal));
  assert.equal(route.waypoints[0].type, 'walk');
  assert.equal(route.name, 'Trasa');
});

test('every documented action type is accepted', () => {
  for (const type of WAYPOINT_TYPES) {
    const route = parseRoute(JSON.stringify({...minimal, waypoints: [{x: 1, y: 2, z: 3, type}]}));
    assert.equal(route.waypoints[0].type, type);
  }
});

test('an unknown action type names the offending waypoint', () => {
  const text = JSON.stringify({...minimal, waypoints: [{x: 1, y: 2, z: 7}, {x: 1, y: 2, z: 7, type: 'teleport'}]});
  assert.throws(() => parseRoute(text), /2/);
});

test('coordinates outside the map are rejected', () => {
  for (const bad of [{x: -1, y: 0, z: 7}, {x: 70000, y: 0, z: 7}, {x: 0, y: 0, z: 16}, {x: 0.5, y: 0, z: 7}]) {
    assert.throws(() => parseRoute(JSON.stringify({...minimal, waypoints: [bad]})), Error,
      `accepted ${JSON.stringify(bad)}`);
  }
});

test('a file from a future version is refused rather than guessed at', () => {
  assert.throws(() => parseRoute(JSON.stringify({...minimal, version: 2})), /wersj/i);
});

test('an empty route is valid, a thousand-and-one point route is not', () => {
  assert.equal(parseRoute(JSON.stringify({...minimal, waypoints: []})).waypoints.length, 0);
  const many = Array.from({length: 1001}, () => ({x: 1, y: 2, z: 7}));
  assert.throws(() => parseRoute(JSON.stringify({...minimal, waypoints: many})), /1000/);
});

test('labels are kept but capped', () => {
  const long = 'x'.repeat(200);
  const route = parseRoute(JSON.stringify({...minimal, waypoints: [{x: 1, y: 2, z: 7, label: long}]}));
  assert.equal(route.waypoints[0].label.length, 64);
});

test('malformed input fails with a message, not a crash', () => {
  for (const text of ['', '{', 'null', '[]', '{"version":1}', '{"version":1,"waypoints":{}}']) {
    assert.throws(() => parseRoute(text), Error, `accepted ${text}`);
  }
});

test('a parsed route survives a round trip', () => {
  const original = {version: 1, name: 'Venore', waypoints: [
    {x: 32958, y: 32077, z: 7, type: 'rope', label: 'lina'},
    {x: 32958, y: 32077, z: 6, type: 'walk', label: ''},
  ]};
  assert.deepEqual(parseRoute(serializeRoute(parseRoute(JSON.stringify(original)))), parseRoute(JSON.stringify(original)));
});
