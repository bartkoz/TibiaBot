const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {join} = require('node:path');
// Resolve the panel sources against this file, not the working directory, so
// the suite runs the same from the repository root or from webtests/.
const webFile = name => join(__dirname, '..', 'web', name);
const vm = require('node:vm');

// Exercise UI event flows and submitted options, with only browser drawing and
// media permission APIs stubbed. The Go tests verify the actual image matcher.
function app({respond, respondPath, respondInput, latency=3, storage={}, inputAvailable=false, gridCells} = {}) {
  const elements = new Map(), requests = [], pathRequests = [], sendRequests = [];
  const gridRequests = [], blockRequests = [];
  let clock = 100, timerID = 0;
  const timers = new Map();
  const context2d = new Proxy({}, {get: () => () => {}});
  function element(id = '') {
    return {id, value:'', width:190, height:190, disabled:false, checked:false, listeners:{}, textContent:'', children:[],
      addEventListener(type, fn) {this.listeners[type] = fn;},
      append(...kids) {this.children.push(...kids);},
      replaceChildren(...kids) {this.children = kids;},
      getContext() {return context2d;},
      toBlob(fn) {fn(new Blob(['png']));},
      getBoundingClientRect() {return {left:0, top:0, width:this.width, height:this.height};},
      setPointerCapture() {},
      setAttribute() {}, click() {this.listeners.click?.(); this.onclick?.();}, remove() {},
      async play() {}, videoWidth:103, videoHeight:110, currentTime:0};
  }
  const document = {getElementById(id) {if (!elements.has(id)) elements.set(id, element(id)); return elements.get(id);}, createElement:element};
  for (const [id,value] of Object.entries({zoom:'0',mask:'5',floor:'7',threshold:'0.85',gap:'0.015',hz:'10',speed:'20','floor-radius':'8'})) document.getElementById(id).value = value;
  document.getElementById('floor-auto').checked = true;
  class Image {
    set src(value) {this.naturalWidth = value === '/api/demo' ? 190 : 103; this.naturalHeight = value === '/api/demo' ? 190 : 110;}
    async decode() {}
  }
  const track = {stop() {}, addEventListener() {}};
  class ImageData {
    constructor(data, width, height) {this.data = data; this.width = width; this.height = height;}
  }
  const sandbox = {document, Image, ImageData, Blob, FormData, AbortController, performance:{now:()=>clock}, URL:{createObjectURL:()=>'capture', revokeObjectURL(){}},
    setTimeout(fn,delay) {timers.set(++timerID,{fn,at:clock+delay});return timerID;}, clearTimeout(id) {timers.delete(id);},
    navigator:{mediaDevices:{async getDisplayMedia() {return {getTracks:()=>[track],getVideoTracks:()=>[track]};}}},
    localStorage:{store:new Map(Object.entries(storage)), getItem(k) {return this.store.has(k)?this.store.get(k):null;},
      setItem(k,v) {this.store.set(k,String(v));}, removeItem(k) {this.store.delete(k);}},
    async fetch(url, options) {
      if (url === '/api/info') return {async json() {return {floors:[7,8],maps:'maps',message:''};}};
      // No test arms a session, so control simply reports itself unavailable,
      // the same reply the Go server gives when started without -input.
      if (url === '/api/input/status') return {async json() {return {available:inputAvailable, platform:'test'};}};
      if (url === '/api/input') {
        const body = JSON.parse(options.body); sendRequests.push(body);
        const result = respondInput?.(body, sendRequests.length) || {status:'emitted', key:'numpad6'};
        return {ok:true, async json() {return result;}};
      }
      if (url.startsWith('/api/grid')) {
        gridRequests.push(url);
        const r = Number(new URL(url, 'http://x').searchParams.get('r'));
        const side = 2 * r + 1;
        const cells = gridCells ?? new Uint8Array(side * side);
        // The real endpoint centres the window on the tile asked for; a fixed
        // origin would have the character walk out of its own preview.
        const q = new URL(url, 'http://x').searchParams;
        const origin = `${Number(q.get('x')) - r},${Number(q.get('y')) - r}`;
        return {ok:true, headers:{get:(k)=>({'x-grid-origin':origin,'x-grid-revision':'1'})[k.toLowerCase()] ?? null},
          async arrayBuffer() {return cells.buffer;}};
      }
      if (url.startsWith('/api/blocks')) {
        blockRequests.push(options ? JSON.parse(options.body) : url);
        if (!options) {
          const q = new URL(url, 'http://x').searchParams;
          return {ok:true, async json() {return [{x:Number(q.get('x')), y:Number(q.get('y')), z:Number(q.get('z')),
            kind:'perm', episodes:2, expires_in_ms:0}];}};
        }
        return {ok:true, async json() {return {result:'temp', reason:'Pierwszy epizod; blokada tymczasowa.', cleared:true};}};
      }
      if (url === '/api/path') {
        const req = JSON.parse(options.body); pathRequests.push(req);
        const steps = [[req.from.x, req.from.y], [req.from.x+1, req.from.y]];
        const result = respondPath?.(req, pathRequests.length) || {found:true, status:'ok', steps, tiles:1, cost:1, reason:'ok', elapsed_ms:1};
        return {ok:true, async json() {return result;}};
      }
      const req = JSON.parse(options.body.get('options')); requests.push(req);
      clock += latency;
      const result = respond?.(req,requests.length) || {found:true,position:{x:32958,y:32077,z:req.floor},zoom:req.demo?2:1,best:{score:.87},samples:1024,elapsed_ms:3,match_ms:2,mode:req.near?'local':'global',search_positions:req.near?121:5000000,reason:'ok'};
      return {ok:true, async json() {return result;}};
    }};
  vm.createContext(sandbox);
  vm.runInContext(readFileSync(webFile('tracker.js'),'utf8'), sandbox);
  vm.runInContext(readFileSync(webFile('route.js'),'utf8'), sandbox);
  vm.runInContext(readFileSync(webFile('recorder.js'),'utf8'), sandbox);
  vm.runInContext(readFileSync(webFile('follower.js'),'utf8'), sandbox);
  vm.runInContext(readFileSync(webFile('executor.js'),'utf8'), sandbox);
  vm.runInContext(readFileSync(webFile('input.js'),'utf8'), sandbox);
  vm.runInContext(readFileSync(webFile('blocks.js'),'utf8'), sandbox);
  vm.runInContext(readFileSync(webFile('app.js'),'utf8'), sandbox);
  // app.js's own const declarations do not become properties of the sandbox
  // (unlike its function declarations, e.g. normalisedPoint), so pull the two
  // control objects out explicitly - test-only, nothing app.js itself needs.
  vm.runInContext('globalThis.__inputClient = inputClient; globalThis.__executor = executor; globalThis.__blocksClient = blocksClient; globalThis.__recorder = recorder;', sandbox);
  return {get:id=>document.getElementById(id), requests, pathRequests, sendRequests, gridRequests, blockRequests, timers,
    blocksClient:sandbox.__blocksClient, gridPixels:sandbox.gridPixels, recorder:sandbox.__recorder,
    storage: sandbox.localStorage.store,
    normalisedPoint:sandbox.normalisedPoint, inputClient:sandbox.__inputClient, executor:sandbox.__executor,
    async loadRoute(route) {
      const input = document.getElementById('route-file');
      input.files = [{name:'route.json', async text() {return typeof route === 'string' ? route : JSON.stringify(route);}}];
      await input.listeners.change({target:input});
    }, async tick(fresh=true) {
    const entry=[...timers.entries()].sort((a,b)=>a[1].at-b[1].at)[0]; assert.ok(entry,'expected scheduled frame');
    timers.delete(entry[0]);clock=entry[1].at;if(fresh) document.getElementById('video').currentTime+=.1;await entry[1].fn();
  }};
}
// Loading the sandbox once at import time is enough to get the pure helper -
// its extraction has no dependency on the rest of a test's mocked state.
const {normalisedPoint} = app();

test('demo does not leak its scale or threshold into screen sharing', async () => {
  const a = app(); await new Promise(setImmediate);
  a.get('floor').value = '8';
  await a.get('demo').onclick();
  assert.equal(Number(a.get('zoom').value), 2);
  await a.get('share').onclick();
  assert.equal(String(a.get('zoom').value), '0');
  assert.equal(String(a.get('threshold').value), '0.85');
  assert.equal(String(a.get('floor').value), '8');
  await a.get('locate').onclick();
  const req = a.requests.at(-1);
  assert.equal(req.demo, false); assert.equal(req.zoom, 0); assert.equal(req.min_score, .85);
  assert.equal(Number(a.get('zoom').value), 1); // Calibration locks the detected scale.
});

test('10 Hz and 5 Hz loops reuse the position and measure actual completed samples', async () => {
  for (const hz of [10,5]) {
    const a=app(); await new Promise(setImmediate);await a.get('share').onclick();
    a.get('hz').value=String(hz);a.get('live').checked=true;
    await a.get('locate').onclick();
    assert.equal(a.requests[0].near,undefined);
    for(let i=0;i<12;i++) await a.tick();
    assert.equal(a.requests.length,13);
    for(const r of a.requests.slice(1)){assert.deepEqual(r.near,{x:32958,y:32077,z:7});assert.equal(r.radius,Math.max(5,Math.ceil(20/hz)+2));}
    assert.equal(Number(a.get('actual-hz').textContent),hz);
    assert.ok(a.requests.some(r=>r.no_preview));
    assert.equal(a.timers.size,1,'exactly one future frame; no interval overlap');
  }
});

test('three local misses trigger one global reacquisition and clear displayed XYZ', async () => {
  const a=app({respond(req) {if(req.near) return {found:false,position:null,zoom:1,mode:'local',match_ms:2,samples:1024,reason:'unknown'};}});
  await new Promise(setImmediate);await a.get('share').onclick();a.get('live').checked=true;await a.get('locate').onclick();
  await a.tick();assert.equal(a.get('coordinates').textContent,'Pozycja nieznana');
  await a.tick();await a.tick();await a.tick();
  assert.deepEqual(a.requests.map(r=>!!r.near),[false,true,true,true,false]);
});

test('frozen video is not counted as new position samples', async () => {
  const a=app();await new Promise(setImmediate);await a.get('share').onclick();a.get('live').checked=true;await a.get('locate').onclick();
  for(let i=0;i<15;i++) await a.tick(false);
  assert.equal(a.requests.length,1);
  assert.equal(a.get('actual-hz').textContent,'0.0');
  assert.equal(a.get('coordinates').textContent,'Brak świeżej klatki');
});

test('slow requests reduce measured rate instead of queuing overlapping requests', async () => {
  const a=app({latency:250});await new Promise(setImmediate);await a.get('share').onclick();a.get('live').checked=true;await a.get('locate').onclick();
  for(let i=0;i<5;i++) await a.tick();
  assert.equal(a.get('actual-hz').textContent,'4.0');assert.equal(a.timers.size,1);
});

test('loading a real screenshot restores calibration after repeated demos', async () => {
  const a = app(); await new Promise(setImmediate);
  a.get('zoom').value = '1'; a.get('threshold').value = '0.8';
  await a.get('demo').onclick(); await a.get('demo').onclick();
  await a.get('file').listeners.change({target:{files:[{name:'minimap.png'}]}});
  await a.get('locate').onclick();
  const req = a.requests.at(-1);
  assert.equal(req.zoom, 1); assert.equal(req.min_score, .8); assert.equal(req.demo, false);
});

test('automatic floor recognition updates Z and continues local tracking without a global request',async()=>{
  const a=app({respond(req,n){if(n===2)return {found:true,position:{x:32959,y:32078,z:8},zoom:1,mode:'local',floor_changed:true,match_ms:8,samples:1024,reason:'floor changed',searched_floors:[7,6,8]};}});
  await new Promise(setImmediate);await a.get('share').onclick();a.get('live').checked=true;await a.get('locate').onclick();
  await a.tick();
  assert.equal(a.requests[1].adjacent_floors,true);assert.equal(a.requests[1].floor_radius,8);
  assert.equal(Number(a.get('floor').value),8);assert.equal(a.get('live').checked,true);
  await a.tick();
  assert.equal(a.requests[2].floor,8);assert.deepEqual(a.requests[2].near,{x:32959,y:32078,z:8});
});

test('manual adjacent Z selection keeps the anchor and the running sampling loop',async()=>{
  const a=app();await new Promise(setImmediate);await a.get('share').onclick();a.get('live').checked=true;await a.get('locate').onclick();
  a.get('floor').value='8';a.get('floor').listeners.input();await new Promise(setImmediate);
  assert.equal(a.get('live').checked,true);
  assert.equal(a.requests[1].floor,8);assert.deepEqual(a.requests[1].near,{x:32958,y:32077,z:7});
  await a.tick();assert.equal(a.requests[2].near.z,8);
});

test('manual Z jump larger than one floor requests global localization',async()=>{
  const a=app();await new Promise(setImmediate);await a.get('share').onclick();a.get('live').checked=true;await a.get('locate').onclick();
  a.get('floor').value='9';a.get('floor').listeners.input();await new Promise(setImmediate);
  assert.equal(a.requests[1].floor,9);assert.equal(a.requests[1].near,undefined);
});

const routeFile = (...waypoints) => ({version:1, name:'Testowa', waypoints});

test('loading a route file shows its name and size', async () => {
  const a = app(); await new Promise(setImmediate);
  await a.loadRoute(routeFile({x:32958,y:32077,z:7}, {x:32970,y:32077,z:7,type:'rope'}));
  assert.equal(a.get('route-name').value, 'Testowa');
  assert.match(a.get('route-status').textContent, /2/);
});

test('a malformed route file is refused without replacing the current one', async () => {
  const a = app(); await new Promise(setImmediate);
  await a.loadRoute(routeFile({x:32958,y:32077,z:7}));
  await a.loadRoute('{"version":9}');
  assert.match(a.get('status').textContent, /wersj/i);
  assert.match(a.get('route-status').textContent, /1/, 'the good route survives');
});

test('following asks the server for a path towards the current waypoint', async () => {
  const a = app(); await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970,y:32100,z:7}));
  a.get('route-follow').checked = true;
  await a.get('locate').onclick();
  assert.equal(a.pathRequests.length, 1);
  assert.deepEqual(a.pathRequests[0].from, {x:32958,y:32077,z:7});
  assert.deepEqual(a.pathRequests[0].to, {x:32970,y:32100,z:7});
});

test('a returned path is shown as a direction to walk', async () => {
  const a = app(); await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970,y:32100,z:7}));
  a.get('route-follow').checked = true; a.get('live').checked = true;
  await a.get('locate').onclick();
  await a.tick();
  assert.match(a.get('route-next').textContent, /E/);
});

test('a stale frame produces a non-zero observation age, not the time spent since it arrived', async () => {
  // C2: the loop used to send now - lastPositionAt, both stamped from the same
  // reading's completion time, so the age was always ~0 and the driver's
  // 400ms freshness gate could never fire. The age must instead reflect the
  // real gap since the frame was captured - here, the mocked fetch latency.
  const a = app({latency: 50});
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970, y:32100, z:7}));
  a.get('route-follow').checked = true; a.get('live').checked = true;
  a.get('input-walk').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';

  await a.get('locate').onclick(); // no path yet: nothing sent
  await a.tick(); // path resolved: a walk intent is sent
  await new Promise(setImmediate);

  assert.equal(a.sendRequests.length, 1, 'expected exactly one intent sent for the walk step');
  assert.ok(a.sendRequests[0].observation_age_ms >= 40,
    `age must track the real capture-to-send gap (~50ms), got ${a.sendRequests[0].observation_age_ms}`);
});

test('executor.observe dostaje prawdziwy czas przechwycenia klatki, nie czas bieżący', async () => {
  // Pins the panel/executor seam of C2: the observation_age_ms assertion
  // above only checks the age sent to the driver. It would stay green even
  // if followStep passed `now` in place of capturedAt into
  // executor.observe(position, capturedAt, now) - which would silently
  // disable executor.js's "a frame captured before the key was pressed is
  // not proof" guard, since capturedAt would then never be earlier than the
  // emission time it gets compared against.
  const a = app({latency: 50});
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970, y:32100, z:7}));
  a.get('route-follow').checked = true; a.get('live').checked = true;
  a.get('input-walk').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';

  const observeCalls = [];
  const realObserve = a.executor.observe.bind(a.executor);
  a.executor.observe = (position, capturedAt, now) => {
    observeCalls.push({capturedAt, now});
    return realObserve(position, capturedAt, now);
  };

  await a.get('locate').onclick();

  assert.ok(observeCalls.length > 0, 'expected at least one executor.observe() call');
  for (const {capturedAt, now} of observeCalls) {
    assert.ok(now - capturedAt >= 40,
      `capturedAt must be the real frame-capture time, not now (got now-capturedAt=${now - capturedAt})`);
  }
});

test('a newly blocked target forces the follower to drop its cached path', async () => {
  // I1: the character never moves (a wall), so the same walk step times out
  // twice and the executor blocks it. Without wiring follower.dropPath() to
  // that, the follower keeps handing back the very same cached path forever
  // and the executor keeps refusing it - frozen, never reaching a fresh
  // path request or the stopped escalation.
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32990, y:32077, z:7})); // far enough east to require pathing
  a.get('route-follow').checked = true;
  a.get('input-walk').checked = true;
  a.get('live').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  await a.get('locate').onclick();

  // The mocked position never advances (always 32958,32077), so every walk
  // attempt times out. Drive enough ticks (~100ms each) to clear three
  // stepTimeoutMS (1800ms) failures and reach the eventual stop.
  for (let i = 0; i < 70; i++) { await a.tick(); await new Promise(setImmediate); }

  assert.ok(a.pathRequests.length > 1,
    `a newly blocked target must trigger a fresh path request, got ${a.pathRequests.length} request(s)`);
  assert.equal(a.executor.state().stopped, true,
    'a route that truly never advances must eventually stop, not cycle between block and replan forever');
});

test('a waypoint on another floor asks for an action instead of a path', async () => {
  const a = app(); await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32958,y:32077,z:6,type:'rope'}));
  a.get('route-follow').checked = true;
  await a.get('locate').onclick();
  assert.equal(a.pathRequests.length, 0, 'transitions are not walked');
  assert.match(a.get('route-next').textContent, /lin/i);
});

test('recording collects waypoints while the tracker runs', async () => {
  let x = 32958;
  const a = app({respond(req) {return {found:true, position:{x:x+=6, y:32077, z:7}, zoom:1, best:{score:.9},
    samples:1024, elapsed_ms:3, match_ms:2, mode:req.near?'local':'global', reason:'ok'};}});
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('route-every').value = '10';
  a.get('route-record').checked = true;
  a.get('live').checked = true;
  await a.get('locate').onclick();
  // setImmediate between ticks lets the walkability window arrive: recording
  // waits for it rather than writing unverified points, so the very first
  // reading - taken before any window exists - is skipped and the count picks
  // up one tick later.
  for (let i = 0; i < 6; i++) { await a.tick(); await new Promise(setImmediate); }
  assert.match(a.get('route-status').textContent, /3/, 'one point every ten tiles walked');
});

test('the add button records the tracked position on demand', async () => {
  const a = app(); await new Promise(setImmediate); await a.get('share').onclick();
  await a.get('locate').onclick();
  await a.get('route-add').onclick();
  assert.match(a.get('route-status').textContent, /1/);
});

test('a loaded route stays idle until following is switched on', async () => {
  const a = app(); await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970,y:32100,z:7}));
  await a.get('locate').onclick();
  assert.equal(a.pathRequests.length, 0);
  assert.equal(a.get('route-next').textContent, '—');
});

test('one click on a waypoint control acts once', async () => {
  const a = app(); await new Promise(setImmediate);
  await a.loadRoute(routeFile({x:1,y:1,z:7}, {x:2,y:2,z:7}, {x:3,y:3,z:7}));
  a.get('route-list').children[0].children[5].click();
  assert.match(a.get('route-status').textContent, /2/, 'one delete removes one waypoint');
});

test('tolerance zero means standing exactly on the waypoint', async () => {
  const a = app(); await new Promise(setImmediate); await a.get('share').onclick();
  // The tracker reports 32958,32077; the waypoint is one tile east of it.
  await a.loadRoute(routeFile({x:32959,y:32077,z:7}));
  a.get('route-tolerance').value = '0';
  a.get('route-follow').checked = true;
  await a.get('locate').onclick();
  assert.ok(!/ukończona/i.test(a.get('route-next').textContent), a.get('route-next').textContent);
});

test('nudging tolerance mid-route does not send the player back to waypoint one', async () => {
  // The tracker walks the player east past the first two waypoints, so a reset
  // would be visible: those points are behind them and would not be re-reached.
  const walk = [32958, 32970, 32970, 32970];
  let reading = 0;
  const a = app({respond: () => ({found:true, position:{x:walk[Math.min(reading++, walk.length-1)], y:32077, z:7},
    zoom:1, best:{score:.9}, samples:1024, elapsed_ms:3, match_ms:2, mode:'local', reason:'ok'})});
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32958,y:32077,z:7}, {x:32970,y:32077,z:7}, {x:32990,y:32077,z:7}));
  a.get('route-follow').checked = true;
  a.get('live').checked = true;
  await a.get('locate').onclick();
  await a.tick();
  assert.match(a.get('route-next').textContent, /#3/, 'two waypoints walked past');

  a.get('route-tolerance').value = '2';
  a.get('route-tolerance').listeners.change?.();
  await a.tick();

  assert.match(a.get('route-next').textContent, /#3/, 'still heading for waypoint 3');
});

test('switching following on says what is missing when there is no route', async () => {
  const a = app(); await new Promise(setImmediate); await a.get('share').onclick();
  a.get('route-follow').checked = true;
  a.get('route-follow').listeners.change?.();
  assert.match(a.get('route-next').textContent, /tras/i, 'must name the missing piece, not stay blank');
});

test('switching following on says when the tracker is not running', async () => {
  const a = app(); await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970,y:32100,z:7}));
  a.get('route-follow').checked = true;
  a.get('route-follow').listeners.change?.();
  assert.match(a.get('route-next').textContent, /śledz|pozycj/i, 'no position yet, so say so');
});

test('switching following on starts guiding from the position already known', async () => {
  const a = app(); await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970,y:32100,z:7}));
  await a.get('locate').onclick();
  assert.equal(a.pathRequests.length, 0, 'not following yet');

  a.get('route-follow').checked = true;
  a.get('route-follow').listeners.change?.();

  assert.equal(a.pathRequests.length, 1, 'the tracked position is fresh, so start right away');
});

test('kalibracja przelicza piksel podglądu na ułamek ekranu', () => {
  // A Retina capture is twice the point size; a fraction cancels that out.
  const point = normalisedPoint({offsetX: 720, offsetY: 450}, {clientWidth: 1440, clientHeight: 900});

  assert.equal(point.x, 0.5);
  assert.equal(point.y, 0.5);
});

test('kalibracja odrzuca punkt poza podglądem', () => {
  assert.equal(normalisedPoint({offsetX: -5, offsetY: 10}, {clientWidth: 100, clientHeight: 100}), null);
});

test('kalibracja odrzuca podgląd o zerowym rozmiarze', () => {
  assert.equal(normalisedPoint({offsetX: 1, offsetY: 1}, {clientWidth: 0, clientHeight: 0}), null);
});

test('walk automation stays off while the checkbox is unticked, even with an armed client', async () => {
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970,y:32100,z:7}));
  a.get('route-follow').checked = true; a.get('live').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  await a.get('locate').onclick();
  await a.tick();
  await new Promise(setImmediate);
  assert.match(a.get('route-next').textContent, /E/, 'the preview keeps working exactly as before');
  assert.equal(a.sendRequests.length, 0, 'nothing is sent while the walk checkbox is unticked');
});

test('walk ticked on an armed client sends the intent and confirms it by the captured step id', async () => {
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970,y:32100,z:7}));
  a.get('route-follow').checked = true; a.get('live').checked = true;
  a.get('input-walk').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  await a.get('locate').onclick();
  await a.tick();
  await new Promise(setImmediate);
  assert.equal(a.sendRequests.length, 1, 'exactly one intent sent for the walk step');
  assert.equal(a.sendRequests[0].action, 'walk');
  assert.equal(a.executor.state().awaitingEmit, false,
    'the reply was confirmed against the step id captured for that step');
});

test('floor actions unticked leaves a transition waypoint unsent, with no pending step behind it', async () => {
  // Regression for the bug where the old post-intentFor gate discarded an
  // already-created pending step: re-ticking floor actions never recovered
  // it, because a block only clears once the follower's target changes,
  // which cannot happen while stuck on that same waypoint.
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32958, y:32077, z:6, type:'rope'}));
  a.get('route-follow').checked = true;
  a.get('input-walk').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  // input-actions stays unticked: floor actions are paused.
  await a.get('locate').onclick();
  await new Promise(setImmediate);
  assert.equal(a.sendRequests.length, 0, 'the transition is never sent while actions are paused');
  assert.equal(a.executor.state().waiting, false, 'no pending step was left behind for it to get stuck on');
});

test('a disarmed reply from the server leaves the panel showing disarmed controls', async () => {
  const a = app({respondInput: () => ({status: 'disarmed', reason: 'zmiana okna aktywnego'})});
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970,y:32100,z:7}));
  a.get('route-follow').checked = true; a.get('live').checked = true;
  a.get('input-walk').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  await a.get('locate').onclick();
  await a.tick();
  await new Promise(setImmediate);
  assert.equal(a.inputClient.armed, false);
  assert.equal(a.get('input-arm').disabled, false, 'arm is available again');
  assert.equal(a.get('input-disarm').disabled, true);
  assert.equal(a.get('input-walk').checked, false, 'walk is unticked when control stops');
  assert.match(a.get('input-status').textContent, /zmiana okna aktywnego/);
});

test('a refused send shows its reason and drops only the pending step, not the escalation counters', async () => {
  // I3: every refusal used to call executor.reset(), wiping cycles/retries/
  // blocked, and renderInputStatus only ever showed a reason on the
  // disarmed branch - so the driver's precise refusal ("limit klawiszy na
  // sekundę", "brak hotkeya...") vanished, even though refusals are routine
  // (the tap-rate limit is well below the 10Hz tracking loop).
  const a = app({respondInput: () => ({status: 'refused', reason: 'limit klawiszy na sekundę'})});
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970, y:32100, z:7}));
  a.get('route-follow').checked = true; a.get('live').checked = true;
  a.get('input-walk').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  await a.get('locate').onclick(); // no path yet: nothing sent
  // Pretend a previous target already failed once - a reset would erase this.
  // currentTarget is seeded to match the walk step about to be sent (the
  // default mock path steps one tile east of 32958,32077), so startTarget's
  // own "a genuinely new target gets a fresh allowance" rule does not fire
  // and mask the very reset this test is checking for.
  a.executor.retries = 2;
  a.executor.cycles = 2;
  a.executor.currentTarget = {x: 32959, y: 32077, z: null};

  await a.tick(); // path resolved: a walk intent is sent and refused
  await new Promise(setImmediate);

  assert.match(a.get('input-status').textContent, /limit klawiszy na sekundę/, 'the refusal reason must be shown while armed');
  assert.equal(a.executor.state().retries, 2, 'a refusal must not reset the retry count');
  assert.equal(a.executor.state().cycles, 2, 'a refusal must not reset the cycle count');
  assert.equal(a.executor.state().waiting, false, 'the refused attempt must not be left pending forever');
});

test('a stopped executor is shown distinctly from a blocked or halted one', async () => {
  // I2: the panel only ever read actionDone and stepId, so stopped/blocked/
  // halted never reached the user - the toolbar kept showing "armed" and a
  // direction even after the executor gave up.
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  a.executor.stopped = true;
  a.inputClient.onState({armed: true, target: {path: '/Applications/Tibia.app'}});
  assert.match(a.get('input-status').textContent, /zatrzym/i);
});

test('a blocked executor is shown distinctly from a stopped or halted one', async () => {
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  a.executor.blocked = true;
  a.inputClient.onState({armed: true, target: {path: '/Applications/Tibia.app'}});
  assert.match(a.get('input-status').textContent, /blok/i);
});

test('a halted executor is shown distinctly from a stopped or blocked one', async () => {
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  a.executor.halted = true;
  a.inputClient.onState({armed: true, target: {path: '/Applications/Tibia.app'}});
  assert.match(a.get('input-status').textContent, /pozycj/i);
});

test('uzbrojenie startuje odliczanie zamiast wysyłać żądanie natychmiast', async () => {
  // C1: arming used to fire /api/arm straight from the click, so whichever
  // window was frontmost - the browser, since the user just clicked its
  // button - got recorded as the game. A countdown gives the user the time
  // to switch to the game before the request actually fires.
  const a = app();
  await new Promise(setImmediate);
  let armCalls = 0;
  a.inputClient.arm = async () => { armCalls++; return {armed: true, target: {path: '/Applications/Tibia.app'}}; };

  a.get('input-arm').click();

  assert.equal(armCalls, 0, 'arming must not happen immediately on click');
  assert.match(a.get('input-status').textContent, /5 s/, 'the remaining seconds must be shown');
});

test('drugie kliknięcie podczas odliczania anuluje uzbrojenie', async () => {
  const a = app();
  await new Promise(setImmediate);
  let armCalls = 0;
  a.inputClient.arm = async () => { armCalls++; return {armed: true}; };

  a.get('input-arm').click();
  a.get('input-arm').click();

  assert.equal(a.timers.size, 0, 'no countdown timer must remain after cancelling');
  assert.match(a.get('input-status').textContent, /anulowan/i);
  assert.equal(armCalls, 0, 'a cancelled countdown must never arm');
});

test('po odliczeniu uzbrojenie następuje automatycznie', async () => {
  const a = app();
  await new Promise(setImmediate);
  let armCalls = 0;
  a.inputClient.arm = async () => { armCalls++; return {armed: true, target: {path: '/Applications/Tibia.app'}}; };

  a.get('input-arm').click();
  for (let i = 0; i < 5; i++) await a.tick();

  assert.equal(armCalls, 1, 'the countdown must arm exactly once when it reaches zero');
});

test('zmiana klawisza akcji zapisuje konfigurację i wysyła ją do wykonawcy', async () => {
  // C3: without this wiring ActionKeys stays empty forever, so every floor
  // action is refused for lack of a hotkey and the panel retries invisibly.
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  const configCalls = [];
  a.inputClient.config = async (keys, clickAfterHotkey) => { configCalls.push({keys, clickAfterHotkey}); return {ok: true}; };

  a.get('hotkey-rope').value = 'f7';
  a.get('hotkey-rope').listeners.input();

  assert.equal(configCalls.length, 1, 'a hotkey change must be sent while armed');
  assert.equal(configCalls[0].keys.rope, 'f7');
  assert.equal(configCalls[0].clickAfterHotkey, true, 'the own-tile checkbox starts unticked, meaning a click follows the hotkey');
  const saved = JSON.parse(a.storage.get('minimap-lab-hotkeys'));
  assert.equal(saved.keys.rope, 'f7', 'the hotkey must also be persisted to localStorage');
});

test('klawisz akcji wpisany wielkimi literami jest wysyłany małymi', async () => {
  // Important: the natural way to write a function key is "F7", but
  // hotkeyNames on the Go side only knows lowercase names - the panel
  // trimmed but never lowercased, so the obvious spelling was rejected.
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  const configCalls = [];
  a.inputClient.config = async (keys, clickAfterHotkey) => { configCalls.push({keys, clickAfterHotkey}); return {ok: true}; };

  a.get('hotkey-rope').value = 'F7';
  a.get('hotkey-rope').listeners.input();

  assert.equal(configCalls[0].keys.rope, 'f7');
});

test('odrzucona konfiguracja klawiszy pokazuje powód wskazujący pole', async () => {
  // Important: sendHotkeyConfig used to discard the ok/reason from
  // inputClient.config entirely, so a rejected (all-or-nothing) config left
  // the status line reading "Uzbrojono:" as if nothing had happened.
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  a.inputClient.config = async () => ({ok: false, reason: 'nieznany klawisz dla akcji rope: control'});

  a.get('hotkey-rope').value = 'control';
  a.get('hotkey-rope').listeners.input();
  await new Promise(setImmediate);

  assert.match(a.get('input-status').textContent, /rope/);
});

test('konfiguracja klawiszy nie jest wysyłana, dopóki wykonawca jest rozbrojony', async () => {
  const a = app();
  await new Promise(setImmediate);
  const configCalls = [];
  a.inputClient.config = async (keys, clickAfterHotkey) => { configCalls.push({keys, clickAfterHotkey}); return {ok: true}; };

  a.get('hotkey-rope').value = 'f7';
  a.get('hotkey-rope').listeners.input();

  assert.equal(configCalls.length, 0, 'nothing must be sent to a driver with no active session');
});

test('zapisana wcześniej konfiguracja klawiszy jest wczytywana przy starcie panelu', async () => {
  const a = app({storage: {'minimap-lab-hotkeys': JSON.stringify(
    {keys: {rope: 'f7', ladder: 'f8', hole: '', shovel: ''}, clickAfterHotkey: false})}});
  await new Promise(setImmediate);

  assert.equal(a.get('hotkey-rope').value, 'f7');
  assert.equal(a.get('hotkey-ladder').value, 'f8');
  assert.equal(a.get('input-own-tile').checked, true, 'clickAfterHotkey:false means the hotkey acts on the own tile');
});

test('uzbrojenie po odliczeniu wysyła wcześniej wpisane klawisze akcji', async () => {
  const a = app();
  await new Promise(setImmediate);
  a.get('hotkey-rope').value = 'f7';
  const configCalls = [];
  a.inputClient.arm = async () => { a.inputClient.armed = true; return {armed: true, target: {path: '/Applications/Tibia.app'}}; };
  a.inputClient.config = async (keys, clickAfterHotkey) => { configCalls.push({keys, clickAfterHotkey}); return {ok: true}; };

  a.get('input-arm').click();
  for (let i = 0; i < 5; i++) await a.tick();
  await new Promise(setImmediate);

  assert.equal(configCalls.length, 1, 'the configured hotkeys must be sent right after arming');
  assert.equal(configCalls[0].keys.rope, 'f7');
});

test('pola kierunków startują z domyślnym układem numpada', async () => {
  const a = app();
  await new Promise(setImmediate);

  assert.equal(a.get('dir-n').value, 'numpad8');
  assert.equal(a.get('dir-ne').value, 'numpad9');
  assert.equal(a.get('dir-e').value, 'numpad6');
  assert.equal(a.get('dir-se').value, 'numpad3');
  assert.equal(a.get('dir-s').value, 'numpad2');
  assert.equal(a.get('dir-sw').value, 'numpad1');
  assert.equal(a.get('dir-w').value, 'numpad4');
  assert.equal(a.get('dir-nw').value, 'numpad7');
});

test('zmiana klawisza kierunku zapisuje konfigurację i wysyła ją do wykonawcy', async () => {
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  const configCalls = [];
  a.inputClient.config = async (keys, clickAfterHotkey, directions) => {
    configCalls.push({keys, clickAfterHotkey, directions}); return {ok: true};
  };

  a.get('dir-n').value = 'w';
  a.get('dir-n').listeners.input();

  assert.equal(configCalls.length, 1, 'a direction change must be sent while armed');
  assert.equal(configCalls[0].directions.N, 'w');
  // The rest of the compass must still ride along at its current value -
  // the driver replaces the whole mapping, not just the one changed field.
  assert.equal(configCalls[0].directions.S, 'numpad2');
  const saved = JSON.parse(a.storage.get('minimap-lab-hotkeys'));
  assert.equal(saved.directions.N, 'w', 'the direction key must also be persisted to localStorage');
});

test('odrzucona konfiguracja kierunków pokazuje powód wskazujący pole', async () => {
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  a.inputClient.config = async () => ({ok: false, reason: 'brak skonfigurowanego klawisza dla kierunku NE'});

  a.get('dir-ne').value = '';
  a.get('dir-ne').listeners.input();
  await new Promise(setImmediate);

  assert.match(a.get('input-status').textContent, /NE/);
});

test('konfiguracja kierunków nie jest wysyłana, dopóki wykonawca jest rozbrojony', async () => {
  const a = app();
  await new Promise(setImmediate);
  const configCalls = [];
  a.inputClient.config = async (keys, clickAfterHotkey, directions) => {
    configCalls.push({directions}); return {ok: true};
  };

  a.get('dir-n').value = 'w';
  a.get('dir-n').listeners.input();

  assert.equal(configCalls.length, 0, 'nothing must be sent to a driver with no active session');
});

test('zapisana wcześniej konfiguracja kierunków jest wczytywana przy starcie panelu, także pole celowo wyczyszczone', async () => {
  const a = app({storage: {'minimap-lab-hotkeys': JSON.stringify(
    {keys: {}, clickAfterHotkey: true, directions: {N: 'w', S: 's', W: 'a', E: 'd', NE: ''}})}});
  await new Promise(setImmediate);

  assert.equal(a.get('dir-n').value, 'w');
  assert.equal(a.get('dir-w').value, 'a');
  // NE was deliberately left empty (no diagonal key) and saved that way; it
  // must not fall back to the numpad default the fields start with.
  assert.equal(a.get('dir-ne').value, '', 'an explicitly cleared direction must stay cleared after reload');
  // A direction absent from the saved object entirely (never touched by the
  // user) keeps the numpad default.
  assert.equal(a.get('dir-sw').value, 'numpad1');
});

test('przycisk Numpad wypełnia wszystkie pola kierunków i wysyła konfigurację', async () => {
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  const configCalls = [];
  a.inputClient.config = async (keys, clickAfterHotkey, directions) => {
    configCalls.push(directions); return {ok: true};
  };
  // Start from something else, so the preset is visibly the one that wins.
  a.get('dir-n').value = 'w';

  a.get('dir-preset-numpad').click();

  assert.equal(a.get('dir-n').value, 'numpad8');
  assert.equal(a.get('dir-nw').value, 'numpad7');
  assert.equal(configCalls.length, 1, 'a preset click must go through the same send path as a manual edit');
  assert.equal(configCalls[0].N, 'numpad8');
});

test('przycisk WSAD wypełnia pola kierunków konwencjonalnym układem liter', async () => {
  const a = app();
  await new Promise(setImmediate);
  a.inputClient.armed = true;
  const configCalls = [];
  a.inputClient.config = async (keys, clickAfterHotkey, directions) => {
    configCalls.push(directions); return {ok: true};
  };

  a.get('dir-preset-wsad').click();

  assert.equal(a.get('dir-n').value, 'w');
  assert.equal(a.get('dir-s').value, 's');
  assert.equal(a.get('dir-w').value, 'a');
  assert.equal(a.get('dir-e').value, 'd');
  assert.equal(a.get('dir-nw').value, 'q');
  assert.equal(a.get('dir-ne').value, 'e');
  assert.equal(a.get('dir-sw').value, 'z');
  assert.equal(a.get('dir-se').value, 'c');
  assert.equal(configCalls.length, 1, 'the WSAD preset must be sent the same way a manual edit is');
  assert.equal(configCalls[0].N, 'w');
  // The user can still edit any field afterwards; the preset is only a
  // starting point, not a locked-in choice.
  a.get('dir-n').value = 'z';
  a.get('dir-n').listeners.input();
  assert.equal(configCalls.at(-1).N, 'z');
});

test('uzbrojenie po odliczeniu wysyła wcześniej wpisane klawisze kierunków', async () => {
  const a = app();
  await new Promise(setImmediate);
  a.get('dir-n').value = 'w';
  const configCalls = [];
  a.inputClient.arm = async () => { a.inputClient.armed = true; return {armed: true, target: {path: '/Applications/Tibia.app'}}; };
  a.inputClient.config = async (keys, clickAfterHotkey, directions) => { configCalls.push(directions); return {ok: true}; };

  a.get('input-arm').click();
  for (let i = 0; i < 5; i++) await a.tick();
  await new Promise(setImmediate);

  assert.equal(configCalls.length, 1, 'the configured direction keys must be sent right after arming');
  assert.equal(configCalls[0].N, 'w');
});

test('chodzenie i akcje pięter pozostają dostępne do zaznaczenia, gdy sterowanie jest rozbrojone', async () => {
  // Critical: input-walk used to be disabled while disarmed, so the only way
  // to tick it was to come back to the browser AFTER arming - stealing focus
  // from the game and disarming on the very next tick, which then unticked
  // the box again. An endless loop that never lets a live test start.
  const a = app();
  await new Promise(setImmediate);

  // Simulate a full arm -> disarm cycle: even after having been armed once,
  // the checkboxes that express intent must stay tickable while disarmed.
  a.inputClient.armed = true;
  a.inputClient.onState({armed: true, target: {path: '/Applications/Tibia.app'}});
  a.inputClient.armed = false;
  a.inputClient.onState({armed: false, reason: 'zatrzymane z panelu'});

  assert.equal(a.get('input-walk').disabled, false, 'walking must be tickable before/after arming, not only while armed');
  assert.equal(a.get('input-actions').disabled, false, 'floor actions must be tickable before/after arming, not only while armed');
});

test('zaznaczenie chodzenia automatycznego przed uzbrojeniem uruchamia automatykę od razu po uzbrojeniu', async () => {
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970, y:32100, z:7}));
  a.get('route-follow').checked = true;
  a.get('live').checked = true;

  // Ticked while still disarmed - the game is the focused window right now,
  // and must stay that way; nothing here should require touching the browser.
  a.get('input-walk').checked = true;

  a.inputClient.arm = async () => {
    a.inputClient.armed = true; a.inputClient.session = 'test-session';
    return {armed: true, target: {path: '/Applications/Tibia.app'}};
  };
  a.get('input-arm').click();
  for (let i = 0; i < 5; i++) await a.tick(); // the 5s countdown
  await new Promise(setImmediate);

  await a.get('locate').onclick();
  await a.tick();
  await new Promise(setImmediate);

  assert.ok(a.sendRequests.length > 0,
    'automation must start once arming completes, without the user touching the checkbox again');
});

test('rozbrojenie z serwera nadal odznacza chodzenie automatyczne', async () => {
  // The safety half of the fix: a real disarm must still untick input-walk,
  // so a disarmed panel never silently resumes walking on the next arm.
  const a = app({respondInput: () => ({status: 'disarmed', reason: 'okno gry straciło focus'})});
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32970,y:32100,z:7}));
  a.get('route-follow').checked = true; a.get('live').checked = true;
  a.get('input-walk').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';

  await a.get('locate').onclick();
  await a.tick();
  await new Promise(setImmediate);

  assert.equal(a.get('input-walk').checked, false, 'a real disarm must still clear the walk checkbox');
});

test('podpowiedź startowa opisuje odliczanie, nie kliknięcie na aktywnym oknie gry', async () => {
  // Minor: the pre-countdown instruction ("uzbrój, gdy klient gry jest oknem
  // aktywnym") contradicted the countdown - it told the user to focus the
  // game *before* clicking Uzbrój, when the whole point of the countdown is
  // to let them do that *after* clicking it.
  const a = app({inputAvailable: true});
  await new Promise(setImmediate);

  assert.doesNotMatch(a.get('input-status').textContent, /gdy klient gry jest oknem aktywnym/);
  assert.match(a.get('input-status').textContent, /odliczani|kliknij.*uzbrój/i);
});

test('floor actions unticked still lets the character walk onto stairs', async () => {
  // The follower reports every floor-change waypoint as 'transition',
  // stairs included, but the executor turns a stairs transition into an
  // ordinary walk step confirmed by a floor change, not a hotkey. Pausing
  // "wykonuj akcje pięter" must pause rope/shovel/ladder, not stairs.
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32958, y:32077, z:6, type:'stairs'}, {x:32960, y:32077, z:6}));
  a.get('route-follow').checked = true;
  a.get('input-walk').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  // input-actions stays unticked.
  await a.get('locate').onclick();
  await new Promise(setImmediate);
  assert.equal(a.sendRequests.length, 1, 'a stairs step is an ordinary walk, not a paused action');
  assert.equal(a.sendRequests[0].action, 'walk');
});

test('a completed floor action reports done once, not on every following tick', async () => {
  // Regression for M2: actionDone used to be cleared only when a new
  // transition intent was created, so once the route moved past its last
  // floor action nothing ever cleared it again and every subsequent tick
  // re-posted /api/input/done forever.
  const positions = [{x:100,y:100,z:7}, {x:100,y:100,z:7}, {x:100,y:100,z:6}, {x:100,y:100,z:6}, {x:100,y:100,z:6}];
  let n = 0;
  const a = app({respond: () => ({found:true, position: positions[Math.min(n++, positions.length-1)], zoom:1,
    best:{score:.9}, samples:1024, elapsed_ms:1, match_ms:1, mode:'local', reason:'ok'})});
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:100, y:100, z:7, type:'rope'}, {x:100, y:100, z:6}));
  a.get('route-follow').checked = true;
  a.get('input-walk').checked = true;
  a.get('input-actions').checked = true;
  a.get('live').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  let doneCalls = 0;
  a.inputClient.actionDone = async () => { doneCalls++; };

  await a.get('locate').onclick(); // standing on the rope: hotkey sent
  await new Promise(setImmediate);
  await a.tick(); // same floor: action still pending
  await new Promise(setImmediate);
  await a.tick(); // floor changed: actionDone becomes true, reported once
  await new Promise(setImmediate);
  await a.tick(); // still on the new floor: must not report again
  await new Promise(setImmediate);
  await a.tick();
  await new Promise(setImmediate);

  assert.equal(doneCalls, 1, 'a completed floor action must be reported exactly once');
});

test('a changed capture resolution while armed leaves the panel showing disarmed controls', async () => {
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  a.get('input-walk').checked = true;
  await a.get('locate').onclick();
  a.get('video').videoWidth = 200; // the capture source changed resolution mid-stream
  await a.tick();
  assert.equal(a.inputClient.armed, false);
  assert.equal(a.get('input-arm').disabled, false, 'arm is available again');
  assert.equal(a.get('input-disarm').disabled, true);
  assert.equal(a.get('input-calibrate').disabled, true);
  // input-walk and input-actions stay enabled even while disarmed, so the
  // user can re-express the intent to walk before arming again; this real
  // disarm must still untick input-walk itself, so it never silently resumes.
  assert.equal(a.get('input-walk').disabled, false);
  assert.equal(a.get('input-actions').disabled, false);
  assert.equal(a.get('input-walk').checked, false, 'a real disarm must still untick walking');
});

test('a calibration click does not invalidate live tracking, but a normal pointerdown still does', async () => {
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  await a.get('locate').onclick();
  assert.equal(a.get('live').checked, true, 'baseline: live tracking is on after the first reading');

  a.get('input-calibrate').listeners.click();
  a.get('screen').listeners.pointerdown({clientX:0, clientY:0, pointerId:1});
  assert.equal(a.get('live').checked, true, 'a calibration click must not invalidate live tracking');

  // Clear calibration mode the way the real click handler does: an offscreen
  // point (the mock has no clientWidth/clientHeight) makes normalisedPoint
  // return null before any network call, but calibrating is still cleared.
  a.get('screen').listeners.click({offsetX:1, offsetY:1, currentTarget:a.get('screen')});
  a.get('screen').listeners.pointerdown({clientX:0, clientY:0, pointerId:2});
  assert.equal(a.get('live').checked, false, 'a normal click still invalidates afterwards');
});

test('nieudany krok jest zgłaszany do magazynu blokad', async () => {
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  await a.loadRoute(routeFile({x:32990, y:32077, z:7}));
  a.get('route-follow').checked = true;
  a.get('input-walk').checked = true;
  a.get('live').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  await a.get('locate').onclick();

  // The mocked position never advances, so the walk step times out and the
  // executor learns something about the tile it could not enter.
  for (let i = 0; i < 40; i++) { await a.tick(); await new Promise(setImmediate); }

  const observations = a.blockRequests.filter(r => r && r.outcome);
  assert.ok(observations.length > 0, 'a failed step taught the executor nothing that reached the server');
  assert.equal(observations[0].outcome, 'no_motion');
  assert.ok(observations[0].still_frames >= 3, `still_frames=${observations[0].still_frames}`);
});

test('podgląd rysuje każdy rodzaj kratki innym kolorem', () => {
  const a = app();
  const cells = new Uint8Array([0, 1, 2, 4, 8]); // wolna, ściana, brak danych, temp, perm
  const pixels = a.gridPixels({origin:[0,0], revision:1, cells}, 5);
  const colour = (i) => pixels.slice(i * 4, i * 4 + 4).join(',');
  const all = [colour(0), colour(1), colour(2), colour(3), colour(4)];
  assert.equal(new Set(all).size, all.length, `kolory się powtarzają: ${all.join(' | ')}`);
});

test('nauczona blokada przykrywa kolor terenu pod nią', () => {
  const a = app();
  // A learned block on a tile the map data calls walkable, and on one it calls
  // a wall: both must read as the learned block, not as the terrain.
  const pixels = a.gridPixels({origin:[0,0], revision:1, cells:new Uint8Array([4, 5])}, 2);
  assert.equal(pixels.slice(0, 4).join(','), pixels.slice(4, 8).join(','));
});

test('podgląd nie odpytuje serwera, dopóki nie jest włączony', async () => {
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  await a.get('locate').onclick();
  for (let i = 0; i < 5; i++) { await a.tick(); await new Promise(setImmediate); }
  assert.equal(a.gridRequests.length, 0);

  a.get('grid-preview-on').checked = true;
  for (let i = 0; i < 5; i++) { await a.tick(); await new Promise(setImmediate); }
  assert.ok(a.gridRequests.length > 0, 'podgląd włączony, a żadne okno nie zostało pobrane');
});

test('kliknięcie kratki z blokadą prosi o jej usunięcie', async () => {
  const side = 65;
  const cells = new Uint8Array(side * side);
  cells[10 * side + 20] = 8; // permanent block
  const a = app({gridCells: cells});
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  a.get('grid-preview-on').checked = true;
  await a.get('locate').onclick();
  for (let i = 0; i < 3; i++) { await a.tick(); await new Promise(setImmediate); }

  const canvas = a.get('grid-canvas');
  canvas.width = canvas.height = 320;
  await canvas.listeners.click({target: canvas, clientX: 20 / side * 320 + 1, clientY: 10 / side * 320 + 1});

  const deletes = a.blockRequests.filter(r => r && r.x !== undefined);
  assert.equal(deletes.length, 1, `oczekiwano jednego żądania usunięcia, jest ${deletes.length}`);
  assert.deepEqual({x: deletes[0].x, y: deletes[0].y}, {x: 32926 + 20, y: 32045 + 10});
});

test('kliknięcie kratki bez blokady niczego nie kasuje', async () => {
  const a = app({gridCells: new Uint8Array(65 * 65)});
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  a.get('grid-preview-on').checked = true;
  await a.get('locate').onclick();
  for (let i = 0; i < 3; i++) { await a.tick(); await new Promise(setImmediate); }

  const canvas = a.get('grid-canvas');
  canvas.width = canvas.height = 320;
  await canvas.listeners.click({target: canvas, clientX: 100, clientY: 100});

  assert.equal(a.blockRequests.filter(r => r && r.x !== undefined).length, 0);
  assert.match(a.get('blocks-status').textContent, /brak nauczonej blokady/);
});

test('kasowanie celuje w piętro z podglądu, nie w bieżące', async () => {
  // The character walks downstairs while the last drawn window still shows the
  // old floor. A click must delete the block the picture shows.
  const side = 65;
  const cells = new Uint8Array(side * side);
  cells[10 * side + 20] = 8;
  const a = app({gridCells: cells});
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  a.get('grid-preview-on').checked = true;
  await a.get('locate').onclick();
  for (let i = 0; i < 3; i++) { await a.tick(); await new Promise(setImmediate); }

  const canvas = a.get('grid-canvas');
  canvas.width = canvas.height = 320;
  await canvas.listeners.click({target: canvas, clientX: 20 / side * 320 + 1, clientY: 10 / side * 320 + 1});

  const deletes = a.blockRequests.filter(r => r && r.x !== undefined);
  assert.equal(deletes.length, 1);
  assert.equal(deletes[0].z, 7, 'usunięto blokadę na piętrze innym niż pokazane w podglądzie');
});

test('komunikat po skasowaniu mówi, co dokładnie zniknęło', async () => {
  const side = 65;
  const cells = new Uint8Array(side * side);
  cells[10 * side + 20] = 8;
  const a = app({gridCells: cells});
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  a.get('grid-preview-on').checked = true;
  await a.get('locate').onclick();
  for (let i = 0; i < 3; i++) { await a.tick(); await new Promise(setImmediate); }

  const canvas = a.get('grid-canvas');
  canvas.width = canvas.height = 320;
  await canvas.listeners.click({target: canvas, clientX: 20 / side * 320 + 1, clientY: 10 / side * 320 + 1});

  assert.match(a.get('blocks-status').textContent, /trwałą \(2 epizody\)/);
});

test('nagrywanie pomija kratkę, której dane nie uznają za przechodnią', async () => {
  // The locator occasionally returns a position on water. A waypoint recorded
  // there is worthless: A* will refuse it later, and the user finds out only
  // when the route stops working.
  const side = 65;
  const cells = new Uint8Array(side * side);
  cells[32 * side + 32] = 1; // the character's own tile reads as impassable
  const a = app({gridCells: cells});
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  a.get('route-record').checked = true;
  a.get('route-every').value = '1';
  await a.get('locate').onclick();
  for (let i = 0; i < 6; i++) { await a.tick(); await new Promise(setImmediate); }

  assert.equal(a.recorder.waypoints.length, 0,
    'zapisano waypoint na kratce, na której postać nie mogła stać');
  assert.match(a.get('route-status').textContent + a.get('blocks-status').textContent, /pomini/i);
});

test('nagrywanie pobiera okno przechodniości nawet bez włączonego podglądu', async () => {
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  a.get('route-record').checked = true;
  await a.get('locate').onclick();
  for (let i = 0; i < 4; i++) { await a.tick(); await new Promise(setImmediate); }
  assert.ok(a.gridRequests.length > 0, 'bez okna nie da się sprawdzić, czy kratka jest przechodnia');
});

test('kratka przechodnia nagrywa się normalnie', async () => {
  const a = app({gridCells: new Uint8Array(65 * 65)});
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  a.get('route-record').checked = true;
  a.get('route-every').value = '1';
  await a.get('locate').onclick();
  for (let i = 0; i < 6; i++) { await a.tick(); await new Promise(setImmediate); }
  assert.ok(a.recorder.waypoints.length > 0, 'poprawna kratka nie została nagrana');
});
