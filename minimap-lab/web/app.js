const $ = id => document.getElementById(id);
const screen = $('screen'), crop = $('crop'), source = document.createElement('canvas');
const video = $('video');
let roi = null, marker = null, dragging = null, stream = null, demo = false, busy = false, revision = 0;
let timer = null, ready = false;
const tracker = new MinimapTracker();
let latestCrop = null, lastFrameTime = null, lastFrameAt = 0, lastPreviewAt = 0, lastScreenDrawAt = 0, controller = null;
const calibrationIDs = ['zoom','mask','floor','threshold','gap'];
let realCalibration = null;
function leaveDemo() {
  if (demo && realCalibration) for (const id of calibrationIDs) $(id).value = realCalibration[id];
  demo = false; $('floor').disabled = false;
}
const num = id => Number($(id).value);
function status(text, kind = '') { $('status').textContent = text; $('status').className = kind; }
function clearResult(keepAnchor = false) {
  revision++;
  controller?.abort();
  if (keepAnchor) { tracker.misses = 0; tracker.readings = []; } else tracker.reset();
  latestCrop = null; lastFrameTime = null; lastPreviewAt = 0;
  $('coordinates').textContent = '—, —, —'; $('metrics').textContent = '';
  $('reference').hidden = true; $('json').textContent = '{}';
  $('search-area').textContent = '—'; renderTelemetry();
}
function pause() { $('live').checked = false; clearTimeout(timer); timer = null; renderTelemetry(); }
function invalidate() { pause(); clearResult(); status('Ustawienia zmienione. Uruchom nowy odczyt.'); }
function drawScreen() {
  const c = screen.getContext('2d');
  c.clearRect(0, 0, screen.width, screen.height);
  if (!ready) return;
  c.drawImage(source, 0, 0, screen.width, screen.height);
  if (roi) {
    const sx = screen.width / source.width, sy = screen.height / source.height;
    c.strokeStyle = '#ff6e94'; c.lineWidth = 2;
    c.strokeRect(roi.x*sx, roi.y*sy, roi.w*sx, roi.h*sy);
  }
}
function cropImage() {
  const c = document.createElement('canvas'); c.width = roi.w; c.height = roi.h;
  c.getContext('2d').drawImage(source, roi.x, roi.y, roi.w, roi.h, 0, 0, roi.w, roi.h);
  return c;
}
function drawCrop() {
  if (!roi || !marker) { $('locate').disabled = $('relocalize').disabled = true; return; }
  crop.width = roi.w; crop.height = roi.h;
  const c = crop.getContext('2d'); c.drawImage(latestCrop || cropImage(), 0, 0);
  const r = num('mask'); c.fillStyle = '#ff578633'; c.strokeStyle = '#ff6e94'; c.lineWidth = 1;
  c.fillRect(marker.x-r, marker.y-r, 2*r+1, 2*r+1); c.strokeRect(marker.x-r-.5, marker.y-r-.5, 2*r+1, 2*r+1);
  c.fillStyle = '#fff'; c.fillRect(marker.x, marker.y, 1, 1);
  $('roi-info').textContent = `Wycinek: x=${roi.x}, y=${roi.y}, ${roi.w} × ${roi.h} px · znacznik: ${marker.x}, ${marker.y}`;
  $('locate').disabled = busy || roi.w < 8 || roi.h < 8 || roi.w > 1024 || roi.h > 1024;
  $('relocalize').disabled = $('locate').disabled;
}
function setSource(image, reset = true) {
  const w = image.videoWidth || image.naturalWidth || image.width;
  const h = image.videoHeight || image.naturalHeight || image.height;
  if (!w || !h) throw new Error('Źródło nie udostępniło jeszcze klatki.');
  if (!reset && (source.width !== w || source.height !== h)) {
    // Without a fresh position further movement is not permissible.
    roi = marker = null; invalidate(); inputClient.disarm();
    // disarm() never calls onState and stops the heartbeat, so nothing else
    // would ever refresh the toolbar - it would keep showing armed controls.
    // Its own !armed branch resets the executor, so that call is not repeated here.
    renderInputStatus({reason: 'zmieniła się rozdzielczość źródła'});
    status('Rozdzielczość źródła zmieniła się. Zaznacz minimapę ponownie.', 'error');
  }
  source.width = w; source.height = h; source.getContext('2d').drawImage(image, 0, 0);
  screen.width = Math.min(w, 1200); screen.height = Math.round(h * screen.width / w); ready = true;
  if (reset) {
    roi = w <= 512 && h <= 512 ? {x:0, y:0, w, h} : null;
    marker = roi ? {x:Math.floor(w/2), y:Math.floor(h/2)} : null;
    clearResult();
    $('roi-info').textContent = roi ? '' : 'Przeciągnij po obrazie, aby zaznaczyć minimapę.';
    if (!roi) { crop.getContext('2d').clearRect(0,0,crop.width,crop.height); }
  }
  drawScreen(); drawCrop();
}
function point(event, element, width, height) {
  const r = element.getBoundingClientRect();
  return {x:Math.max(0, Math.min(width-1, Math.floor((event.clientX-r.left)*width/r.width))),
    y:Math.max(0, Math.min(height-1, Math.floor((event.clientY-r.top)*height/r.height)))};
}
screen.addEventListener('pointerdown', e => {
  // A calibration click must only calibrate: invalidating the current
  // reading (and unticking live tracking) here would be a surprising side
  // effect of pointing at the character's tile.
  if (!ready || calibrating) return;
  invalidate(); dragging = point(e, screen, source.width, source.height); screen.setPointerCapture(e.pointerId);
});
screen.addEventListener('pointermove', e => {
  if (!dragging) return; const p = point(e, screen, source.width, source.height);
  roi = {x:Math.min(p.x,dragging.x), y:Math.min(p.y,dragging.y), w:Math.abs(p.x-dragging.x)+1, h:Math.abs(p.y-dragging.y)+1};
  marker = {x:Math.floor(roi.w/2), y:Math.floor(roi.h/2)}; drawScreen();
});
screen.addEventListener('pointerup', () => { dragging = null; drawCrop(); });
screen.addEventListener('pointercancel', () => { dragging = null; });
crop.addEventListener('pointerdown', e => {
  if (!roi) return; invalidate(); marker = point(e, crop, crop.width, crop.height); drawCrop();
});
for (const id of ['zoom','mask','threshold','gap']) $(id).addEventListener('input', () => { invalidate(); drawCrop(); });
$('floor').addEventListener('input', () => {
  clearResult(true); drawCrop();
  status('Zmieniono piętro. Następny odczyt wykorzysta poprzednie XY, jeśli różnica Z wynosi 1.');
  if ($('live').checked) locate();
});
function stopShare() {
  pause(); if (stream) stream.getTracks().forEach(t => t.stop()); stream = null; video.srcObject = null;
  $('snapshot').disabled = $('stop').disabled = $('live').disabled = true;
}
async function readImage(url) {
  const image = new Image(); image.src = url; await image.decode(); return image;
}
$('file').addEventListener('change', async e => {
  const f = e.target.files[0]; if (!f) return;
  stopShare(); leaveDemo(); const url = URL.createObjectURL(f);
  try { const image = await readImage(url); setSource(image); $('source').textContent = `Screenshot: ${f.name}`; status('Zaznacz teren minimapy i wskaż środek znacznika.'); }
  catch (e) { status(e.message, 'error'); } finally { URL.revokeObjectURL(url); $('file').value = ''; }
});
$('demo').onclick = async () => {
  stopShare();
  if (!demo) realCalibration = Object.fromEntries(calibrationIDs.map(id => [id, $(id).value]));
  demo = true; $('floor').disabled = true;
  try {
    setSource(await readImage('/api/demo')); $('zoom').value = 2; $('mask').value = 5;
    $('threshold').value = .94; $('gap').value = .015;
    marker = {x:94, y:94}; drawCrop();
    $('source').textContent = 'DEMO · syntetyczny obraz · oczekiwana pozycja: 32200, 32180, 7';
    await locate();
  } catch (e) { status(e.message, 'error'); }
};
$('share').onclick = async () => {
  if (!navigator.mediaDevices?.getDisplayMedia) { status('Ta przeglądarka nie obsługuje udostępniania ekranu. Wczytaj screenshot.', 'error'); return; }
  stopShare(); clearResult(); leaveDemo();
  try {
    stream = await navigator.mediaDevices.getDisplayMedia({video:true, audio:false}); video.srcObject = stream;
    await video.play();
    stream.getVideoTracks()[0].addEventListener('ended', () => { stopShare(); clearResult(); status('Udostępnianie zakończone.'); });
    setSource(video); $('snapshot').disabled = $('stop').disabled = $('live').disabled = false;
    $('source').textContent = 'Udostępniony ekran · wybierz minimapę i skalibruj znacznik.';
    status('Pobrano klatkę. Zaznacz minimapę.');
  } catch (e) { stopShare(); status(`Nie udało się udostępnić ekranu: ${e.message}`, 'error'); }
};
$('snapshot').onclick = () => { try { invalidate(); setSource(video, false); } catch(e) { status(e.message, 'error'); } };
$('stop').onclick = () => { stopShare(); clearResult(); status('Udostępnianie zakończone.'); };
async function locate() {
  clearTimeout(timer); timer = null;
  if (busy || !roi || !marker) return;
  const tickStarted = performance.now();
  let frame;
  if (stream) {
    if (video.videoWidth !== source.width || video.videoHeight !== source.height) {
      setSource(video, false); return;
    }
    if (lastFrameTime === video.currentTime) {
      if (tickStarted-lastFrameAt > 1000) { status('Źródło nie dostarczyło nowej klatki.', 'error'); $('coordinates').textContent = 'Brak świeżej klatki'; }
      renderTelemetry(); schedule(tickStarted); return;
    }
    lastFrameTime = video.currentTime; lastFrameAt = tickStarted;
    frame = document.createElement('canvas'); frame.width = roi.w; frame.height = roi.h;
    // Only transfer the minimap on the hot path, not a full Retina screenshot.
    frame.getContext('2d').drawImage(video, roi.x, roi.y, roi.w, roi.h, 0, 0, roi.w, roi.h);
  } else frame = cropImage();
  latestCrop = frame;
  const version = revision, capturedAt = performance.now();
  const hint = tracker.hint(capturedAt, demo?7:num('floor'), num('zoom'), Math.max(1,Math.min(100,num('speed')||20)));
  busy = true; $('locate').disabled = $('relocalize').disabled = true;
  controller = new AbortController();
  if (!hint) {
    status('Pełne wyszukiwanie na piętrze… Pozostań w miejscu do pierwszego potwierdzenia.');
    $('coordinates').textContent = 'Ustalanie pozycji…'; $('reference').hidden = true;
  }
  try {
    const blob = await new Promise(resolve => frame.toBlob(resolve, 'image/png'));
    if (!blob) throw new Error('Nie udało się przygotować obrazu.');
    const form = new FormData(); form.append('image', blob, 'minimap.png');
    form.append('options', JSON.stringify({floor:demo?7:num('floor'), demo, zoom:num('zoom'), marker_x:marker.x,
      marker_y:marker.y, mask_radius:num('mask'), min_score:num('threshold'), min_gap:num('gap'),
      ...(hint || {}), no_preview:!!hint && capturedAt-lastPreviewAt<1000,
      adjacent_floors:!demo && $('floor-auto').checked, floor_radius:Math.max(1,Math.min(32,num('floor-radius')||8))}));
    const response = await fetch('/api/locate', {method:'POST', body:form, signal:controller.signal});
    if (!response.ok) throw new Error(await response.text());
    const result = await response.json(); if (version !== revision) return;
    const completedAt = performance.now();
    tracker.observe(result, capturedAt, completedAt, completedAt-tickStarted);
    if (result.found && num('zoom') === 0) $('zoom').value = result.zoom;
    if (result.found && !demo) $('floor').value = result.position.z;
    if (result.found && result.position) updateRoute(result.position, capturedAt, completedAt);
    if (result.floor_changed) { lastPreviewAt = 0; previewPosition = null; $('reference').hidden = true; }
    status((result.mode === 'local' && result.found ? 'Śledzenie lokalne. ' : '') + result.reason, result.found ? 'ok' : 'error');
    $('coordinates').textContent = result.found ? `${result.position.x}, ${result.position.y}, ${result.position.z}` : 'Pozycja nieznana';
    $('metrics').textContent = `${result.best ? `Wynik: ${(result.best.score*100).toFixed(2)}% · ` : ''}${result.zoom ? `${result.zoom} px/kratkę · ` : ''}${result.samples} próbek · ${result.elapsed_ms} ms${result.searched_floors?.length ? ` · sprawdzone Z: ${result.searched_floors.join(', ')}` : ''}`;
    $('search-area').textContent = `${result.mode==='local'?'lokalny':'całe piętro'} · ${result.search_positions ?? '—'}`;
    if (result.preview) {
      $('reference').src = result.preview; $('reference').hidden = false; lastPreviewAt = completedAt;
      previewPosition = result.position && {...result.position};
    }
    if (!result.found) $('reference').hidden = true;
    const {preview, ...data} = result; $('json').textContent = JSON.stringify(data, null, 2);
    if (!result.found && result.mode !== 'local') pause();
    renderTelemetry();
  } catch (e) { if (version === revision) { status(e.message, 'error'); $('coordinates').textContent = 'Pozycja nieznana'; $('reference').hidden = true; pause(); } }
  finally {
    busy = false; controller = null;
    if (version === revision && stream && performance.now()-lastScreenDrawAt>1000) {
      lastScreenDrawAt = performance.now(); setSource(video, false);
    }
    drawCrop(); schedule(tickStarted);
  }
}
function renderTelemetry() {
  const stats = tracker.stats(performance.now());
  $('actual-hz').textContent = $('live').checked ? stats.hz.toFixed(1) : '0.0';
  $('round-trip').textContent = stats.roundTrip === null ? '—' : `${stats.roundTrip.toFixed(1)} ms`;
  $('match-time').textContent = stats.matchMS == null ? '—' : `${stats.matchMS.toFixed(1)} ms`;
  $('position-age').textContent = stats.ageMS === null ? '—' : `${Math.round(stats.ageMS)} ms`;
  $('success-rate').textContent = tracker.readings.length ? `${Math.round(stats.success*100)}%` : '—';
}
function schedule(started) {
  if ($('live').checked && stream) timer = setTimeout(locate, minimapNextDelay(started,performance.now(),num('hz')===5?5:10));
}
$('locate').onclick = locate;
$('relocalize').onclick = () => { clearResult(); locate(); };
$('live').onchange = () => { clearTimeout(timer); if ($('live').checked) {tracker.readings=[]; locate();} else renderTelemetry(); };
$('hz').onchange = () => { tracker.readings=[]; clearTimeout(timer); if ($('live').checked) locate(); };
fetch('/api/info').then(r => r.json()).then(info => {
  $('maps').textContent = `Mapy: ${info.maps}. ${info.message}`;
  for (const z of info.floors) { const option = document.createElement('option'); option.value = z; option.textContent = `${z}${z===7?' · powierzchnia':''}`; $('floor').append(option); }
  if (info.floors.includes(7)) $('floor').value = '7';
  if (!info.floors.length) { const option = document.createElement('option'); option.value = 7; option.textContent = '7 · brak map, dostępne demo'; $('floor').append(option); }
}).catch(e => status(`Błąd połączenia: ${e.message}`, 'error'));

// --- Trasa: nagrywanie waypointów i podążanie w trybie podglądu ---
const DRAFT_KEY = 'minimap-lab-route';
const recorder = new RouteRecorder();
let follower = null, pathPending = false, lastPosition = null, lastPositionAt = 0, lastCapturedAt = 0, previewPosition = null;
// Tracks the executor's own blocked flag so a newly blocked target (the false
// -> true edge) can be told apart from one that has stayed blocked since the
// previous tick - only the edge should force a fresh path request.
let wasBlocked = false;
const executor = new StepExecutor();
const inputClient = new InputClient({onState: renderInputStatus});
const blocksClient = new BlocksClient();
// Preview radius. 32 gives a 65x65 window: wide enough to show the room the
// character is standing in, small enough to stay a single 4 kB response.
const GRID_RADIUS = 32;
let gridWindow = null;
// Counted rather than reported one by one: a bad patch of readings would
// otherwise overwrite the status line dozens of times a second.
let skippedWaypoints = 0;
let calibrating = false;

const clampNum = (id, low, high, fallback) => {
  const value = num(id);
  return Math.max(low, Math.min(high, Number.isFinite(value) && $(id).value !== '' ? value : fallback));
};

function routeStatus() {
  const n = recorder.waypoints.length;
  $('route-status').textContent = n ? `Waypointy: ${n}${recorder.full ? ' · osiągnięto limit 1000' : ''}` : 'Brak trasy.';
  $('route-save').disabled = $('route-clear').disabled = !n;
  $('route-add').disabled = !freshPosition();
}
function freshPosition() {
  return lastPosition && performance.now() - lastPositionAt < 1000 ? lastPosition : null;
}
function renderWaypoints() {
  $('route-list').replaceChildren(...recorder.waypoints.map((w, i) => {
    const row = document.createElement('li');
    const where = document.createElement('span');
    where.className = 'where';
    where.textContent = `${w.x}, ${w.y}, ${w.z}`;
    const type = document.createElement('select');
    for (const name of WAYPOINT_TYPES) {
      const option = document.createElement('option');
      option.value = option.textContent = name;
      type.append(option);
    }
    type.value = w.type;
    type.addEventListener('change', () => { w.type = type.value; saveDraft(); });
    const label = document.createElement('input');
    label.type = 'text';
    label.maxLength = 64;
    label.value = w.label;
    label.placeholder = 'opis';
    label.addEventListener('input', () => { w.label = label.value; saveDraft(); });
    const up = button('↑', () => moveWaypoint(i, -1));
    const down = button('↓', () => moveWaypoint(i, 1));
    const drop = button('✕', () => { recorder.waypoints.splice(i, 1); afterRouteEdit(); });
    row.append(where, type, label, up, down, drop);
    return row;
  }));
}
function button(text, onClick) {
  const b = document.createElement('button');
  b.className = 'secondary tiny';
  b.textContent = text;
  b.onclick = onClick;
  return b;
}
function moveWaypoint(index, delta) {
  const to = index + delta;
  if (to < 0 || to >= recorder.waypoints.length) return;
  const [w] = recorder.waypoints.splice(index, 1);
  recorder.waypoints.splice(to, 0, w);
  afterRouteEdit();
}
// Any edit invalidates a route being followed: indices and targets moved.
function afterRouteEdit() {
  follower = null;
  pathPending = false;
  renderWaypoints();
  routeStatus();
  saveDraft();
}
function saveDraft() {
  try {
    localStorage.setItem(DRAFT_KEY, serializeRoute({name: $('route-name').value, waypoints: recorder.waypoints}));
  } catch (e) { /* private mode or blocked storage: the file export still works */ }
}
function applyRoute(route) {
  recorder.waypoints = route.waypoints;
  recorder.lastSaved = null;
  $('route-name').value = route.name;
  afterRouteEdit();
}
function followText(out) {
  switch (out.action) {
    case 'walk': return `${out.direction} · ${out.remaining} kratek do #${follower.index + 1}${out.waypoint.label ? ` (${out.waypoint.label})` : ''}`;
    case 'transition': return `${out.instruction} · waypoint #${follower.index + 1}`;
    case 'path': return `Liczenie trasy do #${follower.index + 1}…`;
    case 'wait': return `Przeliczam trasę do #${follower.index + 1}…`;
    case 'blocked': return `Nie można dojść do #${follower.index + 1}: ${out.reason}`;
    case 'done': return 'Trasa ukończona.';
    default: return '—';
  }
}
function ensureFollower() {
  const options = {tolerance: clampNum('route-tolerance', 0, 10, 1), loop: $('route-loop').checked};
  if (!follower) follower = new RouteFollower(recorder.waypoints, options);
  else Object.assign(follower, options);
  return follower;
}
async function requestPath(from, to) {
  pathPending = true;
  try {
    const response = await fetch('/api/path', {method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({from, to, margin: clampNum('route-margin', 0, 256, 64)})});
    if (!response.ok) throw new Error(await response.text());
    follower?.setPath(await response.json(), performance.now(), to);
  } catch (e) {
    status(`Trasa: ${e.message}`, 'error');
    follower?.setPath({found: false, status: 'error', reason: e.message}, performance.now(), to);
  } finally {
    pathPending = false;
  }
}
// Called once per confirmed position, from the tracking loop. capturedAt is
// when the frame was grabbed, not when this reply was parsed - the executor's
// lock-step proof and the driver's freshness gate both depend on that
// distinction, since within one tick "now" and "reply parsed" share no gap.
function updateRoute(position, capturedAt, now) {
  lastPosition = {...position};
  lastPositionAt = now;
  lastCapturedAt = capturedAt;
  if ($('route-record').checked) {
    recorder.auto = true;
    recorder.every = clampNum('route-every', 1, 100, 10);
    // A position the map calls impassable is not a position the character was
    // ever standing on - the locator matched the wrong place, and water is the
    // usual culprit. Recording it produces a waypoint no route can ever reach,
    // and the user only finds out much later, when the route stops working.
    const flags = tileFlags(position);
    if (flags === null) {
      // No window covering this tile yet. Recording anyway would let exactly
      // the unverified points through, so wait - the window is one request
      // away and arrives within a tick or two.
      $('blocks-status').textContent = 'Czekam na dane przechodniości dla tej okolicy…';
    } else if (flags & 3) {
      skippedWaypoints++;
      $('blocks-status').textContent =
        `Pominięto ${skippedWaypoints} ${skippedWaypoints === 1 ? 'punkt' : 'punktów'}: ` +
        `${position.x}, ${position.y} to ${(flags & 2) ? 'obszar bez danych mapy' : 'kratka nieprzechodnia'}. ` +
        'Odczyt pozycji był najpewniej błędny.';
    } else if (recorder.observe(position)) { renderWaypoints(); saveDraft(); }
  } else {
    recorder.auto = false;
    recorder.observe(position);
  }
  routeStatus();
  // Fire and forget, and deliberately ahead of the route-follow gate: the
  // walkability preview is a diagnostic tool, useful precisely when no route
  // is running. Neither the report nor the preview may delay a reading.
  pumpBlocks(position, now);
  if (!$('route-follow').checked) {
    $('route-next').textContent = '—';
    return;
  }
  followStep(position, capturedAt, now);
}

// Colours are deliberately far apart: this preview exists to answer "why did
// the bot go around there", and a subtle difference answers nothing.
const GRID_COLOURS = {
  free: [40, 70, 40, 255],
  wall: [150, 40, 40, 255],
  missing: [40, 40, 45, 255],
  temp: [220, 170, 40, 255],
  perm: [230, 80, 230, 255],
};
// gridPixels is split out of drawing so the colour rules can be tested without
// a canvas.
// tileFlags reads one tile out of the last window fetched, or null when that
// window does not cover it (or none has arrived yet).
function tileFlags(position) {
  const w = gridWindow;
  if (!w || !position || w.z !== position.z) return null;
  const side = 2 * GRID_RADIUS + 1;
  const col = position.x - w.origin[0], row = position.y - w.origin[1];
  if (col < 0 || row < 0 || col >= side || row >= side) return null;
  return w.cells[row * side + col];
}
function gridPixels(w, side) {
  const out = new Uint8ClampedArray(side * side * 4);
  for (let i = 0; i < w.cells.length && i < side * side; i++) {
    const c = w.cells[i];
    let colour = GRID_COLOURS.free;
    if (c & 2) colour = GRID_COLOURS.missing;
    else if (c & 1) colour = GRID_COLOURS.wall;
    // A learned block wins over the terrain underneath: showing it is the
    // whole point of the preview.
    if (c & 4) colour = GRID_COLOURS.temp;
    if (c & 8) colour = GRID_COLOURS.perm;
    out.set(colour, i * 4);
  }
  return out;
}
function drawGrid(w) {
  const canvas = $('grid-canvas');
  if (!canvas) return;
  const side = 2 * GRID_RADIUS + 1;
  canvas.width = canvas.height = side;
  canvas.getContext('2d').putImageData(new ImageData(gridPixels(w, side), side, side), 0, 0);
}
// pumpBlocks ships whatever the executor learned and refreshes the preview.
async function pumpBlocks(position, now) {
  const obs = executor.takeObservation();
  if (obs) {
    const decision = await blocksClient.report(obs);
    if (decision) {
      // The server's verdict is shown verbatim, so a refused observation is
      // visible instead of looking like a lost request.
      $('blocks-status').textContent = `${obs.to.x}, ${obs.to.y}: ${decision.reason}`;
      // Anything the server accepted changed the map under the cached route -
      // including a route request already in flight, which is what the
      // revision guards against.
      if (decision.result !== 'ignored' && follower) {
        if (decision.revision) {
          follower.minOverlayRevision = Math.max(follower.minOverlayRevision, decision.revision);
        }
        follower.dropPath();
      }
    }
  }
  // The window is fetched for the preview and for recording alike: a waypoint
  // may not be written without knowing whether the tile is one the character
  // could actually have stood on.
  if (!position || (!$('grid-preview-on').checked && !$('route-record').checked)) return;
  if (!blocksClient.shouldRefresh(position, now)) return;
  const w = await blocksClient.window(position.x, position.y, position.z, GRID_RADIUS);
  if (!w) return;
  gridWindow = w;
  if ($('grid-preview-on').checked) drawGrid(w);
}

// followStep advances the follower and reports what the player should do. It
// runs both from the tracking loop and the moment following is switched on, so
// the panel never sits blank waiting for the next reading.
function followStep(position, capturedAt, now) {
  if (!recorder.waypoints.length) {
    $('route-next').textContent = 'Brak trasy — wczytaj plik JSON albo nagraj waypointy.';
    return null;
  }
  if (!position) {
    $('route-next').textContent = 'Brak pozycji — uruchom śledzenie XYZ albo kliknij „Znajdź pozycję".';
    return null;
  }
  const out = ensureFollower().step(position, now);
  $('route-next').textContent = followText(out);
  drawRoutePath();
  // The request runs alongside the tracking loop; it never delays a reading.
  if (out.action === 'path' && !pathPending) requestPath(out.from, out.to);
  // Automatic control is opt-in and requires an armed session. With the
  // checkbox unticked (the default) nothing below ever runs, so the
  // preview-only behaviour above is completely untouched.
  if (!$('input-walk').checked || !inputClient.armed) return out;
  executor.observe(position, capturedAt, now);
  if (executor.state().actionDone) { executor.clearActionDone(); inputClient.actionDone(); }
  // A newly blocked target means the executor gave up on it (second failure);
  // without forcing the follower to drop its cached path, it keeps producing
  // the very same target forever and the executor keeps refusing it - frozen,
  // never reaching the escalation that would stop the route instead.
  const blockedNow = executor.state().blocked;
  if (blockedNow && !wasBlocked) follower.dropPath();
  wasBlocked = blockedNow;
  // Decided from the follower's own output, before the executor is asked for
  // anything: asking first would create a pending step that this gate then
  // discarded without confirming or resetting it, leaving it to time out into
  // a retry and then a permanent block - stalling the route at that waypoint
  // even after the checkbox is re-ticked, since a block only clears when the
  // target changes, which it cannot while stuck there.
  // Stairs are excluded: the follower reports them as a 'transition' output
  // too, but the executor converts them into an ordinary walk step (a floor
  // change confirms it, not a hotkey), so pausing "floor actions" must not
  // also refuse to walk onto stairs.
  if (out.action === 'transition' && out.waypoint.type !== 'stairs' && !$('input-actions').checked) return out;
  const intent = executor.intentFor(out, now);
  if (!intent) return out;
  // The id ties the confirmation to this step: a reply that arrives after the
  // step was abandoned must not stamp its successor.
  const stepId = executor.state().stepId;
  inputClient.send(intent, now - capturedAt).then(result => {
    if (result.status === 'emitted') executor.emitted(performance.now(), stepId);
    // A refusal means the key was never sent: the situation did not change,
    // so only the abandoned attempt is dropped, not the escalation counters
    // that are tracking how many times this route has actually failed.
    // 'disarmed' is handled by renderInputStatus's !armed branch instead,
    // which already resets the executor before this callback runs.
    else if (result.status === 'refused') executor.dropPending();
  });
  return out;
}
// The reference preview is a 129x129-tile window centred on the player, so a
// path tile maps straight onto it.
function drawRoutePath() {
  const canvas = $('route-overlay');
  if (!canvas) return;
  // The preview refreshes at most once a second while tracking, so the overlay
  // is anchored to the position that produced it, not to the current one.
  canvas.hidden = !follower?.path || !previewPosition || $('reference').hidden;
  if (canvas.hidden) return;
  const c = canvas.getContext('2d');
  c.clearRect(0, 0, canvas.width, canvas.height);
  c.fillStyle = '#73d6bd';
  for (const [x, y] of follower.path) {
    const px = 64 + x - previewPosition.x, py = 64 + y - previewPosition.y;
    if (px >= 0 && px < 129 && py >= 0 && py < 129) c.fillRect(px, py, 1, 1);
  }
}
$('route-file').addEventListener('change', async e => {
  const f = e.target.files[0];
  if (!f) return;
  try {
    applyRoute(parseRoute(await f.text()));
    status(`Wczytano trasę: ${recorder.waypoints.length} punktów.`, 'ok');
  } catch (err) {
    status(err.message, 'error');
  } finally {
    $('route-file').value = '';
  }
});
$('route-save').onclick = () => {
  const blob = new Blob([serializeRoute({name: $('route-name').value, waypoints: recorder.waypoints})],
    {type: 'application/json'});
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = `${($('route-name').value || 'trasa').replace(/[^\w\- ąćęłńóśźżĄĆĘŁŃÓŚŹŻ]/g, '_')}.json`;
  link.click();
  URL.revokeObjectURL(url);
};
$('route-clear').onclick = () => {
  recorder.waypoints = [];
  recorder.lastSaved = null;
  afterRouteEdit();
  status('Trasa wyczyszczona.');
};
$('route-add').onclick = () => {
  const position = freshPosition();
  if (!position) { status('Brak świeżej pozycji — waypoint nie został dodany.', 'error'); return; }
  if (!recorder.addManual(position)) { status('Trasa osiągnęła limit 1000 punktów.', 'error'); return; }
  afterRouteEdit();
};
$('route-name').addEventListener('input', saveDraft);
// Only switching following on or off starts a route over; tolerance and
// looping are applied live by ensureFollower on the next reading.
$('route-follow').addEventListener('change', () => {
  follower = null;
  pathPending = false;
  if (!$('route-follow').checked) { $('route-next').textContent = '—'; return; }
  followStep(freshPosition(), lastCapturedAt, performance.now());
});

// --- Sterowanie: uzbrajanie sesji i podłączenie wykonawcy do pętli śledzenia ---
// The panel sends fractions of the shared image, never pixels. Go multiplies
// them by the screen size in points, so Retina and DPI never reach this side.
function normalisedPoint(event, element) {
  const w = element.clientWidth, h = element.clientHeight;
  if (!w || !h) return null;
  const x = event.offsetX / w, y = event.offsetY / h;
  if (x < 0 || x > 1 || y < 0 || y > 1) return null;
  return {x, y};
}
// Single place that reflects inputClient.armed onto the toolbar: called after
// arm, disarm, every send() reply and every heartbeat.
function renderInputStatus(state = {}) {
  const armed = inputClient.armed;
  $('input-arm').disabled = armed;
  for (const id of ['input-disarm', 'input-calibrate']) $(id).disabled = !armed;
  // input-walk and input-actions stay tickable regardless of armed state: the
  // intent to walk automatically must be expressible before arming, so it is
  // already in place - with the game already focused - the instant arming
  // completes. Ticking them while disarmed does nothing on its own; the
  // !inputClient.armed guard in followStep is what keeps that inert.
  if (!armed) {
    // Every way of stopping clears half-finished step state, so re-arming
    // never resumes a step whose confirmation was never seen.
    if ($('input-walk').checked) $('input-walk').checked = false;
    executor.reset();
    calibrating = false;
    if (state.reason) $('input-status').textContent = `Sterowanie rozbrojone: ${state.reason}`;
    return;
  }
  // The executor's own escalation state takes priority over a routine reply:
  // it is what tells a user "stopped after repeated failures" apart from
  // "waiting" and from "position unknown", none of which a bare "Uzbrojono."
  // or a rate-limit reason from a single reply would convey on their own.
  const es = executor.state();
  if (es.stopped) {
    $('input-status').textContent = 'Zatrzymano: zbyt wiele nieudanych prób pod rząd. Sprawdź trasę albo rozbrój i uzbrój ponownie.';
  } else if (es.blocked) {
    $('input-status').textContent = 'Zablokowano bieżący cel — przeliczam trasę.';
  } else if (es.halted) {
    $('input-status').textContent = 'Wstrzymano: brak znanej pozycji.';
  } else if (state.reason) {
    // Refusals are routine (the tap-rate limit is well below the tracking
    // rate), so the precise reason must be visible, not swallowed.
    $('input-status').textContent = state.reason;
  } else if (state.target) {
    $('input-status').textContent = `Uzbrojono: ${state.target.path}${state.target.title ? ` — ${state.target.title}` : ''}`;
  } else {
    $('input-status').textContent = 'Uzbrojono.';
  }
}
// Arming from a click on this panel would record the browser as the focused
// window - the user just clicked it - so the focus gate would pass while the
// browser has focus and disarm the instant the game is actually focused. A
// countdown gives the user the window they need to switch to the game before
// the request that captures the frontmost window actually fires.
const ARM_COUNTDOWN_S = 5;
let armCountdownTimer = null, armCountdownRemaining = 0;
function armCountdownStatus() {
  return `Przełącz się na okno gry — uzbrajanie za ${armCountdownRemaining} s. Kliknij ponownie, aby anulować.`;
}
function tickArmCountdown() {
  armCountdownRemaining--;
  if (armCountdownRemaining <= 0) {
    armCountdownTimer = null;
    inputClient.arm().then(() => sendHotkeyConfig());
    return;
  }
  $('input-status').textContent = armCountdownStatus();
  armCountdownTimer = setTimeout(tickArmCountdown, 1000);
}
$('input-arm').addEventListener('click', () => {
  if (armCountdownTimer) {
    clearTimeout(armCountdownTimer);
    armCountdownTimer = null;
    $('input-status').textContent = 'Uzbrojenie anulowane.';
    return;
  }
  armCountdownRemaining = ARM_COUNTDOWN_S;
  $('input-status').textContent = armCountdownStatus();
  armCountdownTimer = setTimeout(tickArmCountdown, 1000);
});
$('input-disarm').addEventListener('click', async () => {
  await inputClient.disarm();
  executor.reset();
  renderInputStatus({reason: 'zatrzymane z panelu'});
});
$('input-calibrate').addEventListener('click', () => {
  calibrating = true;
  $('input-status').textContent = 'Kliknij kratkę postaci na podglądzie ekranu.';
});
// The screen canvas is the same preview already used to select the minimap.
$('screen').addEventListener('click', async event => {
  if (!calibrating) return;
  calibrating = false;
  const frac = normalisedPoint(event, event.currentTarget);
  if (!frac) { $('input-status').textContent = 'Punkt poza podglądem.'; return; }
  const ok = await inputClient.calibrate(frac.x, frac.y);
  $('input-status').textContent = ok
    ? `Kratka postaci: ${frac.x.toFixed(3)}, ${frac.y.toFixed(3)}`
    : 'Nie udało się zapisać kalibracji.';
});
// Floor-action hotkeys and direction keys are configured from the panel and
// sent to the driver on arm and whenever they change. Without this,
// ActionKeys stays empty forever (every floor action refused for lack of a
// hotkey) and DirectionKeys stays on its numpad default.
const HOTKEY_TYPES = ['rope', 'ladder', 'hole', 'shovel'];
const HOTKEY_STORAGE_KEY = 'minimap-lab-hotkeys';
// Same eight compass names the follower and the driver use.
const DIRECTIONS = ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'];
function directionFieldId(dir) { return `dir-${dir.toLowerCase()}`; }
// The driver's own built-in default - shown here too so the fields already
// match it before the user ever touches them.
const NUMPAD_DIRECTION_PRESET = {
  N: 'numpad8', NE: 'numpad9', E: 'numpad6', SE: 'numpad3',
  S: 'numpad2', SW: 'numpad1', W: 'numpad4', NW: 'numpad7',
};
// The conventional WASD compass: the four letters around the home position,
// plus q/e/z/c surrounding them for the diagonals.
const WSAD_DIRECTION_PRESET = {
  N: 'w', NE: 'e', E: 'd', SE: 'c',
  S: 's', SW: 'z', W: 'a', NW: 'q',
};
function hotkeyConfig() {
  const keys = {};
  // input.go's hotkeyNames only knows lowercase names; "F7" is the natural
  // way to type a function key, so it must still be accepted.
  for (const type of HOTKEY_TYPES) keys[type] = $(`hotkey-${type}`).value.trim().toLowerCase();
  const directions = {};
  for (const dir of DIRECTIONS) directions[dir] = $(directionFieldId(dir)).value.trim().toLowerCase();
  return {keys, clickAfterHotkey: !$('input-own-tile').checked, directions};
}
function saveHotkeyConfig() {
  try { localStorage.setItem(HOTKEY_STORAGE_KEY, JSON.stringify(hotkeyConfig())); }
  catch (e) { /* private mode or blocked storage: the driver still gets the live value */ }
}
function sendHotkeyConfig() {
  if (!inputClient.armed) return;
  const {keys, clickAfterHotkey, directions} = hotkeyConfig();
  // The config is all-or-nothing server-side: one typo in any hotkey or
  // direction field voids the whole request and the driver keeps its
  // previous mapping. Without surfacing this, the only clue was "brak
  // hotkeya dla akcji ..." (or, for a direction, "brak skonfigurowanego
  // klawisza dla kierunku ...") flashing between heartbeats - so show the
  // reason, which already names the rejected field.
  inputClient.config(keys, clickAfterHotkey, directions).then(({ok, reason}) => {
    if (!ok) $('input-status').textContent = `Konfiguracja klawiszy odrzucona: ${reason || 'nieznany błąd'}`;
  });
}
for (const type of HOTKEY_TYPES) {
  $(`hotkey-${type}`).addEventListener('input', () => { saveHotkeyConfig(); sendHotkeyConfig(); });
}
$('input-own-tile').addEventListener('change', () => { saveHotkeyConfig(); sendHotkeyConfig(); });
for (const dir of DIRECTIONS) {
  $(directionFieldId(dir)).addEventListener('input', () => { saveHotkeyConfig(); sendHotkeyConfig(); });
}
// A preset is only a starting point: it fills the eight fields, then goes
// through the exact same save+send path as a manual edit - nothing here is
// special-cased server-side, and the user's own edits afterward are what
// actually gets persisted.
function applyDirectionPreset(preset) {
  for (const dir of DIRECTIONS) $(directionFieldId(dir)).value = preset[dir];
  saveHotkeyConfig();
  sendHotkeyConfig();
}
$('dir-preset-numpad').addEventListener('click', () => applyDirectionPreset(NUMPAD_DIRECTION_PRESET));
$('dir-preset-wsad').addEventListener('click', () => applyDirectionPreset(WSAD_DIRECTION_PRESET));
// Fields start on the numpad default - the same one the driver itself starts
// with - so a numpad user needs to touch nothing.
for (const dir of DIRECTIONS) $(directionFieldId(dir)).value = NUMPAD_DIRECTION_PRESET[dir];
try {
  const savedHotkeys = JSON.parse(localStorage.getItem(HOTKEY_STORAGE_KEY) || 'null');
  if (savedHotkeys) {
    for (const type of HOTKEY_TYPES) if (savedHotkeys.keys?.[type]) $(`hotkey-${type}`).value = savedHotkeys.keys[type];
    $('input-own-tile').checked = !savedHotkeys.clickAfterHotkey;
    // A direction deliberately cleared (no key for that diagonal) must still
    // override the numpad default set above, so this checks presence in the
    // saved object, not truthiness.
    for (const dir of DIRECTIONS) {
      if (savedHotkeys.directions && Object.prototype.hasOwnProperty.call(savedHotkeys.directions, dir)) {
        $(directionFieldId(dir)).value = savedHotkeys.directions[dir];
      }
    }
  }
} catch (e) { /* private mode or blocked storage: fields keep the numpad default set above */ }
$('input-walk').addEventListener('change', () => { if (!$('input-walk').checked) executor.reset(); });
// Availability is only known once the server answers; the button starts
// disabled so a fresh page never looks armable before this resolves.
fetch('/api/input/status').then(r => r.json()).then(state => {
  if (state.available === false) return;
  $('input-arm').disabled = false;
  $('input-status').textContent = 'Gotowe do uzbrojenia. Kliknij „Uzbrój” i przełącz się na okno gry podczas odliczania.';
}).catch(() => {});

try {
  const draft = localStorage.getItem(DRAFT_KEY);
  if (draft) applyRoute(parseRoute(draft));
} catch (e) {
  routeStatus();
}
routeStatus();

function describeBlock(b) {
  if (!b) return 'nauczoną';
  const episodes = `${b.episodes} ${b.episodes === 1 ? 'epizod' : 'epizody'}`;
  if (b.kind === 'perm') return `trwałą (${episodes})`;
  return `tymczasową (${episodes}, zostało ${Math.round(b.expires_in_ms / 1000)} s)`;
}
// Clicking a tile in the preview revokes what the executor learned about it.
// The character walking onto it does the same thing on the server side; this
// is the manual override for a block that is simply wrong.
$('grid-canvas').addEventListener('click', async event => {
  if (!gridWindow) return;
  const side = 2 * GRID_RADIUS + 1;
  const rect = event.target.getBoundingClientRect();
  const col = Math.floor((event.clientX - rect.left) / rect.width * side);
  const row = Math.floor((event.clientY - rect.top) / rect.height * side);
  if (col < 0 || row < 0 || col >= side || row >= side) return;
  const x = gridWindow.origin[0] + col, y = gridWindow.origin[1] + row;
  // Bits 4 and 8 are the two learned kinds; map terrain is not ours to delete.
  if (!(gridWindow.cells[row * side + col] & 12)) {
    $('blocks-status').textContent = `${x}, ${y}: brak nauczonej blokady.`;
    return;
  }
  // Read the details before deleting them, so the panel can say what it just
  // removed rather than only that something is gone.
  const details = (await blocksClient.list(x, y, gridWindow.z, 1))
    .find(b => b.x === x && b.y === y);
  const cleared = await blocksClient.remove(x, y, gridWindow.z);
  $('blocks-status').textContent = cleared
    ? `${x}, ${y}: usunięto blokadę ${describeBlock(details)}.`
    : `${x}, ${y}: nie udało się usunąć blokady.`;
  if (cleared) follower?.dropPath();
});
