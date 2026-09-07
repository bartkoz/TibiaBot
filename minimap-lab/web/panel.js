// The panel is a camera and a window onto the bot's state. Every decision it
// used to make - which step, which waypoint, what a failed step means - now
// lives in Go. What is left is choosing the rectangles, filling in the
// settings, and drawing what comes back.

const $ = id => document.getElementById(id);
const num = id => Number($(id).value);
const screenCanvas = $('screen'), cropCanvas = $('crop'), source = document.createElement('canvas');
const video = $('video');

let roi = null, marker = null, dragging = null, stream = null, demo = false, ready = false;
let calibrating = false, worker = null, lastPreviewRev = -1, gridWindow = null;
const GRID_RADIUS = 32;

const camera = new Camera({
  onSnapshot: render,
  onError: reason => status(reason, 'error'),
});

function status(text, kind = '') { $('status').textContent = text; $('status').className = kind; }
function routeStatus(text) { $('route-status').textContent = text; }

// --- źródło obrazu ---

function drawScreen() {
  const c = screenCanvas.getContext('2d');
  c.clearRect(0, 0, screenCanvas.width, screenCanvas.height);
  if (!ready) return;
  c.drawImage(source, 0, 0, screenCanvas.width, screenCanvas.height);
  if (roi) {
    const sx = screenCanvas.width / source.width, sy = screenCanvas.height / source.height;
    c.strokeStyle = '#ff6e94'; c.lineWidth = 2;
    c.strokeRect(roi.x * sx, roi.y * sy, roi.w * sx, roi.h * sy);
  }
}

function drawCrop() {
  if (!roi) { $('roi-info').textContent = 'Zaznacz minimapę na obrazie.'; return; }
  cropCanvas.width = roi.w; cropCanvas.height = roi.h;
  const c = cropCanvas.getContext('2d');
  c.drawImage(source, roi.x, roi.y, roi.w, roi.h, 0, 0, roi.w, roi.h);
  if (marker) {
    const r = num('mask');
    c.fillStyle = '#ff578633'; c.strokeStyle = '#ff6e94'; c.lineWidth = 1;
    c.fillRect(marker.x - r, marker.y - r, 2 * r + 1, 2 * r + 1);
    c.strokeRect(marker.x - r - .5, marker.y - r - .5, 2 * r + 1, 2 * r + 1);
    c.fillStyle = '#fff'; c.fillRect(marker.x, marker.y, 1, 1);
  }
  $('roi-info').textContent = `Wycinek: x=${roi.x}, y=${roi.y}, ${roi.w} × ${roi.h} px` +
    (marker ? ` · znacznik: ${marker.x}, ${marker.y}` : ' · wskaż znacznik postaci');
  camera.setRegion(FRAME_REGION.minimap, roi);
  pushConfig();
}

function setSource(image, reset = true) {
  const w = image.videoWidth || image.naturalWidth || image.width;
  const h = image.videoHeight || image.naturalHeight || image.height;
  if (!w || !h) throw new Error('Źródło nie udostępniło jeszcze klatki.');
  if (!reset && (source.width !== w || source.height !== h)) {
    // Without a fresh rectangle further movement is not permissible: the
    // minimap is no longer where it was.
    roi = marker = null;
    camera.setRegion(FRAME_REGION.minimap, null);
    disarm();
    status('Rozdzielczość źródła zmieniła się. Zaznacz minimapę ponownie.', 'error');
  }
  source.width = w; source.height = h;
  source.getContext('2d').drawImage(image, 0, 0);
  screenCanvas.width = Math.min(w, 1200);
  screenCanvas.height = Math.round(h * screenCanvas.width / w);
  ready = true;
  if (reset) {
    roi = w <= 512 && h <= 512 ? {x: 0, y: 0, w, h} : null;
    marker = roi ? {x: Math.floor(w / 2), y: Math.floor(h / 2)} : null;
  }
  drawScreen(); drawCrop();
}

function stopShare() {
  stopLoop();
  if (stream) stream.getTracks().forEach(t => t.stop());
  stream = null; video.srcObject = null;
  $('snapshot').disabled = $('stop').disabled = $('live').disabled = true;
}

async function readImage(url) {
  const image = new Image(); image.src = url; await image.decode(); return image;
}

$('file').addEventListener('change', async e => {
  const f = e.target.files[0]; if (!f) return;
  stopShare(); demo = false;
  const url = URL.createObjectURL(f);
  try {
    setSource(await readImage(url));
    $('source').textContent = `Screenshot: ${f.name}`;
    status('Zaznacz teren minimapy i wskaż środek znacznika. Bot potrzebuje udostępnionego ekranu, żeby ruszyć.');
  } catch (e) { status(e.message, 'error'); }
  finally { URL.revokeObjectURL(url); $('file').value = ''; }
});

$('demo').onclick = async () => {
  stopShare(); demo = true;
  try {
    setSource(await readImage('/api/demo'));
    $('zoom').value = 2; $('mask').value = 5;
    marker = {x: 94, y: 94}; drawCrop();
    $('source').textContent = 'DEMO · syntetyczny obraz · oczekiwana pozycja: 32200, 32180, 7';
    status('Obraz demo wczytany. To sprawdzian dopasowania, nie bot — bot potrzebuje udostępnionego ekranu.');
  } catch (e) { status(e.message, 'error'); }
};

$('share').onclick = async () => {
  if (!navigator.mediaDevices?.getDisplayMedia) {
    status('Ta przeglądarka nie obsługuje udostępniania ekranu. Wczytaj screenshot.', 'error');
    return;
  }
  stopShare(); demo = false;
  try {
    stream = await navigator.mediaDevices.getDisplayMedia({video: true, audio: false});
    video.srcObject = stream;
    await video.play();
    stream.getVideoTracks()[0].addEventListener('ended', () => {
      stopShare(); status('Udostępnianie zakończone.');
    });
    setSource(video);
    $('snapshot').disabled = $('stop').disabled = $('live').disabled = false;
    $('source').textContent = 'Udostępniony ekran · wybierz minimapę i skalibruj znacznik.';
    status('Pobrano klatkę. Zaznacz minimapę.');
  } catch (e) { stopShare(); status(`Nie udało się udostępnić ekranu: ${e.message}`, 'error'); }
};

$('snapshot').onclick = () => { try { setSource(video, false); } catch (e) { status(e.message, 'error'); } };
$('stop').onclick = () => { stopShare(); status('Udostępnianie zakończone.'); };

// --- zaznaczanie obszaru ---

function point(event, element, width, height) {
  const r = element.getBoundingClientRect();
  return {
    x: Math.max(0, Math.min(width - 1, Math.floor((event.clientX - r.left) * width / r.width))),
    y: Math.max(0, Math.min(height - 1, Math.floor((event.clientY - r.top) * height / r.height))),
  };
}

screenCanvas.addEventListener('pointerdown', e => {
  if (!ready) return;
  if (calibrating) {
    const p = point(e, screenCanvas, source.width, source.height);
    calibrating = false;
    pushConfig({x: p.x / source.width, y: p.y / source.height});
    status(`Kratka postaci ustawiona na ${p.x}, ${p.y}.`, 'ok');
    return;
  }
  dragging = point(e, screenCanvas, source.width, source.height);
  screenCanvas.setPointerCapture(e.pointerId);
});
screenCanvas.addEventListener('pointermove', e => {
  if (!dragging) return;
  const p = point(e, screenCanvas, source.width, source.height);
  roi = {x: Math.min(p.x, dragging.x), y: Math.min(p.y, dragging.y),
    w: Math.abs(p.x - dragging.x) + 1, h: Math.abs(p.y - dragging.y) + 1};
  marker = {x: Math.floor(roi.w / 2), y: Math.floor(roi.h / 2)};
  drawScreen();
});
screenCanvas.addEventListener('pointerup', () => { dragging = null; drawCrop(); });
screenCanvas.addEventListener('pointercancel', () => { dragging = null; });
cropCanvas.addEventListener('pointerdown', e => {
  if (!roi) return;
  marker = point(e, cropCanvas, cropCanvas.width, cropCanvas.height);
  drawCrop();
});

// --- konfiguracja ---

const DIRECTIONS = {NW: 'dir-nw', N: 'dir-n', NE: 'dir-ne', W: 'dir-w', E: 'dir-e',
  SW: 'dir-sw', S: 'dir-s', SE: 'dir-se'};
const HOTKEYS = {rope: 'hotkey-rope', ladder: 'hotkey-ladder', hole: 'hotkey-hole', shovel: 'hotkey-shovel'};

function brainConfig() {
  return {
    zoom: Math.max(1, num('zoom') || 1),
    marker_x: marker?.x ?? 0,
    marker_y: marker?.y ?? 0,
    mask_radius: num('mask'),
    min_score: num('threshold'),
    min_gap: num('gap'),
    floor: num('floor'),
    adjacent_floors: $('floor-auto').checked,
    floor_radius: num('floor-radius'),
    speed: num('speed'),
    follow: $('route-follow').checked,
    walk: $('input-walk').checked,
    floor_actions: $('input-actions').checked,
    record_auto: $('route-record').checked,
    record_every: num('route-every'),
    tolerance: num('route-tolerance'),
    action_tolerance: 0,
    loop_route: $('route-loop').checked,
  };
}

// pushConfig sends the whole surface at once. The server validates it as one
// piece, so a single bad field is refused with a reason instead of half the
// form quietly taking effect.
async function pushConfig(tile) {
  const body = {
    brain: brainConfig(),
    keys: Object.fromEntries(Object.entries(HOTKEYS)
      .map(([kind, id]) => [kind, $(id).value.trim()]).filter(([, key]) => key)),
    click_after_hotkey: !$('input-own-tile').checked,
    directions: Object.fromEntries(Object.entries(DIRECTIONS)
      .map(([dir, id]) => [dir, $(id).value.trim()]).filter(([, key]) => key)),
  };
  if (tile) body.tile = tile;
  try {
    const r = await fetch('/api/config', {
      method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body),
    });
    const answer = await r.json();
    if (!r.ok) status(answer.reason ?? 'Konfiguracja odrzucona.', 'error');
    return r.ok;
  } catch (e) { status(e.message, 'error'); return false; }
}

for (const id of ['zoom', 'mask', 'threshold', 'gap', 'floor', 'floor-auto', 'floor-radius',
  'speed', 'route-every', 'route-tolerance', 'route-loop', 'route-record', 'route-follow',
  'input-walk', 'input-actions', 'input-own-tile',
  ...Object.values(HOTKEYS), ...Object.values(DIRECTIONS)]) {
  $(id).addEventListener('change', () => pushConfig());
}

const NUMPAD = {NW: 'numpad7', N: 'numpad8', NE: 'numpad9', W: 'numpad4', E: 'numpad6',
  SW: 'numpad1', S: 'numpad2', SE: 'numpad3'};
const WSAD = {NW: 'q', N: 'w', NE: 'e', W: 'a', E: 'd', SW: 'z', S: 's', SE: 'c'};
function applyPreset(preset) {
  for (const [dir, id] of Object.entries(DIRECTIONS)) $(id).value = preset[dir] ?? '';
  pushConfig();
}
$('dir-preset-numpad').onclick = () => applyPreset(NUMPAD);
$('dir-preset-wsad').onclick = () => applyPreset(WSAD);

// --- uzbrajanie i pętla klatek ---

async function arm() {
  try {
    const r = await fetch('/api/arm', {method: 'POST'});
    const answer = await r.json();
    if (!r.ok || !answer.armed) {
      status(answer.reason ?? 'Nie udało się uzbroić.', 'error');
      return;
    }
    camera.setSession(answer.session);
    await pushConfig();
    status('Uzbrojono. Wykonawca działa, dopóki okno gry ma focus.', 'ok');
  } catch (e) { status(e.message, 'error'); }
}

async function disarm() {
  camera.setSession(null);
  try { await fetch('/api/disarm', {method: 'POST'}); } catch { /* nic tu nie pomoże */ }
}

$('input-arm').onclick = arm;
$('input-disarm').onclick = () => { disarm(); status('Rozbrojono.'); };
$('input-calibrate').onclick = () => {
  calibrating = true;
  status('Kliknij na obrazie kratkę, na której stoi postać.');
};

function startLoop() {
  if (worker) return;
  worker = new Worker('/worker.js');
  worker.onmessage = () => {
    if (!stream) return;
    camera.sendFrame(video, 0);
  };
  worker.postMessage({intervalMS: 100});
}

function stopLoop() {
  if (!worker) return;
  worker.postMessage({stop: true});
  worker.terminate();
  worker = null;
}

$('live').addEventListener('change', () => {
  if ($('live').checked) startLoop(); else stopLoop();
});

// --- trasa ---

$('route-file').addEventListener('change', async e => {
  const f = e.target.files[0]; if (!f) return;
  try {
    const r = await fetch('/api/route', {method: 'PUT', body: await f.text()});
    const answer = await r.json();
    if (!r.ok) { routeStatus(answer.reason ?? 'Nie udało się wczytać trasy.'); return; }
    routeStatus(`Wczytano ${answer.waypoints} waypointów z ${f.name}.`);
    refreshList();
  } catch (e) { routeStatus(e.message); }
  finally { $('route-file').value = ''; }
});

$('route-save').onclick = async () => {
  try {
    const r = await fetch('/api/route');
    if (!r.ok) { routeStatus('Nie udało się pobrać trasy.'); return; }
    const blob = new Blob([await r.text()], {type: 'application/json'});
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = ($('route-name').value.trim() || 'trasa') + '.json';
    a.click();
    URL.revokeObjectURL(a.href);
  } catch (e) { routeStatus(e.message); }
};

$('route-clear').onclick = async () => {
  await fetch('/api/route', {method: 'PUT', body: '{"version":1,"waypoints":[]}'});
  routeStatus('Trasa wyczyszczona.');
  refreshList();
};

$('route-add').onclick = async () => {
  const r = await fetch('/api/route/waypoint', {method: 'POST'});
  if (!r.ok) { routeStatus((await r.json()).reason ?? 'Nie udało się dodać punktu.'); return; }
  routeStatus('Dodano waypoint na bieżącej kratce.');
  refreshList();
};

// The recorder guesses a transition type from direction and displacement,
// because the minimap cannot tell a rope from a ladder. The list is where the
// user corrects that guess, so it survives the move of the route into Go: the
// panel fetches it, edits one field, and sends the whole thing back.
let routeCache = null, routeCount = -1;

async function refreshList(force = false) {
  try {
    const r = await fetch('/api/route');
    if (!r.ok) return;
    routeCache = await r.json();
    renderList();
  } catch { /* lista jest wygodą, nie warunkiem działania */ }
}

function renderList() {
  const points = routeCache?.waypoints ?? [];
  $('route-name').value ||= routeCache?.name ?? '';
  $('route-list').replaceChildren(...points.map((wp, index) => {
    const row = document.createElement('li');
    const label = document.createElement('span');
    label.textContent = `${wp.x}, ${wp.y}, ${wp.z}`;
    const select = document.createElement('select');
    for (const kind of ['walk', 'rope', 'ladder', 'stairs', 'hole', 'shovel']) {
      const option = document.createElement('option');
      option.value = kind; option.textContent = kind;
      select.append(option);
    }
    select.value = wp.type;
    select.addEventListener('change', async () => {
      routeCache.waypoints[index].type = select.value;
      routeCache.name = $('route-name').value.trim();
      const put = await fetch('/api/route', {method: 'PUT', body: JSON.stringify(routeCache)});
      routeStatus(put.ok ? `Waypoint ${index + 1} to teraz ${select.value}.`
        : ((await put.json()).reason ?? 'Nie udało się zapisać zmiany.'));
    });
    row.append(label, select);
    return row;
  }));
}

// --- widok ---

function render(state) {
  $('coordinates').textContent = state.position
    ? `${state.position.x}, ${state.position.y}, ${state.position.z}`
    : 'Pozycja nieznana';
  const m = state.match ?? {};
  $('metrics').textContent = [
    m.score ? `Wynik: ${(m.score * 100).toFixed(2)}%` : '',
    m.samples ? `${m.samples} próbek` : '',
    m.searched_floors?.length ? `sprawdzone Z: ${m.searched_floors.join(', ')}` : '',
  ].filter(Boolean).join(' · ');
  $('actual-hz').textContent = (m.hz ?? 0).toFixed(1);
  $('round-trip').textContent = m.round_trip_ms ? `${m.round_trip_ms.toFixed(1)} ms` : '—';
  $('match-time').textContent = m.match_ms ? `${m.match_ms.toFixed(1)} ms` : '—';
  $('position-age').textContent = state.position_age_ms == null ? '—' : `${state.position_age_ms} ms`;
  $('success-rate').textContent = m.success == null ? '—' : `${Math.round(m.success * 100)}%`;
  $('search-area').textContent = m.mode === 'local' ? 'lokalny' : (m.mode ? 'całe piętro' : '—');
  if (m.reason) status(m.reason, m.found ? 'ok' : 'error');

  $('input-status').textContent = state.armed
    ? 'Uzbrojony. Alt-tab albo cisza kamery rozbraja.'
    : 'Rozbrojony.';
  $('input-arm').disabled = state.armed;
  $('input-disarm').disabled = !state.armed;
  $('input-calibrate').disabled = !ready;
  $('route-add').disabled = !state.position;
  $('route-save').disabled = !state.route?.count;
  $('route-clear').disabled = !state.route?.count;

  const r = state.route ?? {};
  routeStatus(r.count
    ? `Waypoint ${Math.min(r.index + 1, r.count)} z ${r.count}${r.finished ? ' · ukończona' : ''}` +
      (state.recorder?.skipped ? ` · pominięto ${state.recorder.skipped} błędnych odczytów` : '') +
      (state.recorder?.waiting ? ' · czekam na dane przechodniości' : '')
    : 'Brak trasy.');
  $('route-next').textContent = r.next || '—';
  // Refetched only when the count moves: the list is a thousand rows at worst
  // and has no business being rebuilt at frame rate.
  if (r.count !== routeCount) { routeCount = r.count; refreshList(); }

  const e = state.executor ?? {};
  $('json').textContent = JSON.stringify(state, null, 2);
  if (e.stopped) status('Wykonawca zatrzymany po serii nieudanych kroków.', 'error');

  const log = state.log ?? [];
  $('blocks-status').textContent = log.length ? log[log.length - 1].text : '—';

  if (state.preview_revision !== lastPreviewRev) {
    lastPreviewRev = state.preview_revision;
    $('reference').src = `/api/preview?v=${state.preview_revision}`;
    $('reference').hidden = false;
  }
  if ($('grid-preview-on').checked && state.position) refreshGrid(state.position);
}

// --- podgląd przechodności ---

const GRID_COLOURS = {
  free: [40, 70, 40, 255], wall: [150, 40, 40, 255], missing: [40, 40, 45, 255],
  temp: [220, 170, 40, 255], perm: [230, 80, 230, 255],
};

function gridPixels(cells, side) {
  const out = new Uint8ClampedArray(side * side * 4);
  for (let i = 0; i < cells.length && i < side * side; i++) {
    const c = cells[i];
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

let gridPending = false;
async function refreshGrid(p) {
  if (gridPending) return;
  gridPending = true;
  try {
    const res = await fetch(`/api/grid?x=${p.x}&y=${p.y}&z=${p.z}&r=${GRID_RADIUS}`);
    if (!res.ok) return;
    const origin = (res.headers.get('X-Grid-Origin') ?? '0,0').split(',').map(Number);
    const cells = new Uint8Array(await res.arrayBuffer());
    gridWindow = {origin, z: p.z, cells};
    const side = 2 * GRID_RADIUS + 1;
    const canvas = $('grid-canvas');
    canvas.width = canvas.height = side;
    canvas.getContext('2d').putImageData(new ImageData(gridPixels(cells, side), side, side), 0, 0);
  } catch { /* podgląd jest diagnostyką, nie blokuje pętli */ }
  finally { gridPending = false; }
}

$('grid-canvas').addEventListener('click', async event => {
  if (!gridWindow) return;
  const side = 2 * GRID_RADIUS + 1;
  const p = point(event, $('grid-canvas'), side, side);
  const x = gridWindow.origin[0] + p.x, y = gridWindow.origin[1] + p.y;
  const r = await fetch('/api/blocks', {
    method: 'DELETE', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({x, y, z: gridWindow.z}),
  });
  const answer = await r.json().catch(() => ({}));
  $('blocks-status').textContent = answer.cleared
    ? `Usunięto nauczoną blokadę na ${x}, ${y}.`
    : `Na ${x}, ${y} nie ma nauczonej blokady.`;
});

// --- start ---

(async () => {
  try {
    const info = await (await fetch('/api/info')).json();
    const floors = info.floors?.length ? info.floors : [...Array(16).keys()];
    $('floor').replaceChildren(...floors.map(z => {
      const o = document.createElement('option');
      o.value = String(z); o.textContent = `Z = ${z}`;
      return o;
    }));
    $('floor').value = floors.includes(7) ? '7' : String(floors[0]);
    $('maps').textContent = info.message || `Mapy: ${info.maps}`;
  } catch { /* panel działa też bez /api/info */ }
  // A state poll costs one request and tells the panel whether control is even
  // available, which is what every disabled button below depends on.
  try {
    const r = await fetch('/api/state');
    if (r.ok) render(await r.json());
    else $('input-status').textContent = (await r.json()).reason ?? 'Sterowanie wyłączone.';
  } catch { /* jak wyżej */ }
})();
