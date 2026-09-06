const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const vm = require('node:vm');

// Exercise UI event flows and submitted options, with only browser drawing and
// media permission APIs stubbed. The Go tests verify the actual image matcher.
function app({respond, respondPath, respondInput, latency=3} = {}) {
  const elements = new Map(), requests = [], pathRequests = [], sendRequests = [];
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
  const sandbox = {document, Image, Blob, FormData, AbortController, performance:{now:()=>clock}, URL:{createObjectURL:()=>'capture', revokeObjectURL(){}},
    setTimeout(fn,delay) {timers.set(++timerID,{fn,at:clock+delay});return timerID;}, clearTimeout(id) {timers.delete(id);},
    navigator:{mediaDevices:{async getDisplayMedia() {return {getTracks:()=>[track],getVideoTracks:()=>[track]};}}},
    localStorage:{store:new Map(), getItem(k) {return this.store.has(k)?this.store.get(k):null;},
      setItem(k,v) {this.store.set(k,String(v));}, removeItem(k) {this.store.delete(k);}},
    async fetch(url, options) {
      if (url === '/api/info') return {async json() {return {floors:[7,8],maps:'maps',message:''};}};
      // No test arms a session, so control simply reports itself unavailable,
      // the same reply the Go server gives when started without -input.
      if (url === '/api/input/status') return {async json() {return {available:false, platform:'test'};}};
      if (url === '/api/input') {
        const body = JSON.parse(options.body); sendRequests.push(body);
        const result = respondInput?.(body, sendRequests.length) || {status:'emitted', key:'numpad6'};
        return {ok:true, async json() {return result;}};
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
  vm.runInContext(readFileSync('web/tracker.js','utf8'), sandbox);
  vm.runInContext(readFileSync('web/route.js','utf8'), sandbox);
  vm.runInContext(readFileSync('web/recorder.js','utf8'), sandbox);
  vm.runInContext(readFileSync('web/follower.js','utf8'), sandbox);
  vm.runInContext(readFileSync('web/executor.js','utf8'), sandbox);
  vm.runInContext(readFileSync('web/input.js','utf8'), sandbox);
  vm.runInContext(readFileSync('web/app.js','utf8'), sandbox);
  // app.js's own const declarations do not become properties of the sandbox
  // (unlike its function declarations, e.g. normalisedPoint), so pull the two
  // control objects out explicitly - test-only, nothing app.js itself needs.
  vm.runInContext('globalThis.__inputClient = inputClient; globalThis.__executor = executor;', sandbox);
  return {get:id=>document.getElementById(id), requests, pathRequests, sendRequests, timers,
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
  for (let i = 0; i < 4; i++) await a.tick();
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

test('a changed capture resolution while armed leaves the panel showing disarmed controls', async () => {
  const a = app();
  await new Promise(setImmediate); await a.get('share').onclick();
  a.get('live').checked = true;
  a.inputClient.armed = true; a.inputClient.session = 'test-session';
  await a.get('locate').onclick();
  a.get('video').videoWidth = 200; // the capture source changed resolution mid-stream
  await a.tick();
  assert.equal(a.inputClient.armed, false);
  assert.equal(a.get('input-arm').disabled, false, 'arm is available again');
  assert.equal(a.get('input-disarm').disabled, true);
  assert.equal(a.get('input-calibrate').disabled, true);
  assert.equal(a.get('input-walk').disabled, true);
  assert.equal(a.get('input-actions').disabled, true);
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
