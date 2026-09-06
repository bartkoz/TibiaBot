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
    roi = marker = null; invalidate(); executor.reset(); inputClient.disarm();
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
  if (!ready) return; invalidate(); dragging = point(e, screen, source.width, source.height); screen.setPointerCapture(e.pointerId);
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
    if (result.found && result.position) updateRoute(result.position, completedAt);
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
let follower = null, pathPending = false, lastPosition = null, lastPositionAt = 0, previewPosition = null;
const executor = new StepExecutor();
const inputClient = new InputClient({onState: renderInputStatus});
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
// Called once per confirmed position, from the tracking loop.
function updateRoute(position, now) {
  lastPosition = {...position};
  lastPositionAt = now;
  if ($('route-record').checked) {
    recorder.auto = true;
    recorder.every = clampNum('route-every', 1, 100, 10);
    if (recorder.observe(position)) { renderWaypoints(); saveDraft(); }
  } else {
    recorder.auto = false;
    recorder.observe(position);
  }
  routeStatus();
  if (!$('route-follow').checked) {
    $('route-next').textContent = '—';
    return;
  }
  const out = followStep(position, now);
}

// followStep advances the follower and reports what the player should do. It
// runs both from the tracking loop and the moment following is switched on, so
// the panel never sits blank waiting for the next reading.
function followStep(position, now) {
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
  executor.observe(position, lastPositionAt, now);
  if (executor.state().actionDone) inputClient.actionDone();
  const intent = executor.intentFor(out, now);
  if (!intent) return out;
  if (intent.action === 'transition' && !$('input-actions').checked) return out;
  // The id ties the confirmation to this step: a reply that arrives after the
  // step was abandoned must not stamp its successor.
  const stepId = executor.state().stepId;
  inputClient.send(intent, now - lastPositionAt).then(result => {
    if (result.status === 'emitted') executor.emitted(performance.now(), stepId);
    else if (result.status !== 'in_progress') executor.reset();
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
  followStep(freshPosition(), performance.now());
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
  for (const id of ['input-disarm', 'input-calibrate', 'input-walk', 'input-actions']) $(id).disabled = !armed;
  if (!armed) {
    // Every way of stopping clears half-finished step state, so re-arming
    // never resumes a step whose confirmation was never seen.
    if ($('input-walk').checked) $('input-walk').checked = false;
    executor.reset();
    calibrating = false;
  }
  if (armed && state.target) {
    $('input-status').textContent = `Uzbrojono: ${state.target.path}${state.target.title ? ` — ${state.target.title}` : ''}`;
  } else if (!armed && state.reason) {
    $('input-status').textContent = `Sterowanie rozbrojone: ${state.reason}`;
  } else if (armed) {
    $('input-status').textContent = 'Uzbrojono.';
  }
}
$('input-arm').addEventListener('click', () => inputClient.arm());
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
  const point = normalisedPoint(event, event.currentTarget);
  if (!point) { $('input-status').textContent = 'Punkt poza podglądem.'; return; }
  const ok = await inputClient.calibrate(point.x, point.y);
  $('input-status').textContent = ok
    ? `Kratka postaci: ${point.x.toFixed(3)}, ${point.y.toFixed(3)}`
    : 'Nie udało się zapisać kalibracji.';
});
$('input-walk').addEventListener('change', () => { if (!$('input-walk').checked) executor.reset(); });
// Availability is only known once the server answers; the button starts
// disabled so a fresh page never looks armable before this resolves.
fetch('/api/input/status').then(r => r.json()).then(state => {
  if (state.available === false) return;
  $('input-arm').disabled = false;
  $('input-status').textContent = 'Gotowe do uzbrojenia. Uzbrój, gdy klient gry jest oknem aktywnym.';
}).catch(() => {});

try {
  const draft = localStorage.getItem(DRAFT_KEY);
  if (draft) applyRoute(parseRoute(draft));
} catch (e) {
  routeStatus();
}
routeStatus();
