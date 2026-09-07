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
let locating = false, sourceRevision = 0, controlAvailable = false, brainAvailable = false;
let startingTracking = false, statePending = false, lastStateVersion = -1;
let positionReceivedAt = 0, positionAgeAtReceipt = null, lastDrawTime = null;
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
  const sx = screenCanvas.width / source.width, sy = screenCanvas.height / source.height;
  if (roi) {
    c.strokeStyle = '#ff6e94'; c.lineWidth = 2;
    c.strokeRect(roi.x * sx, roi.y * sy, roi.w * sx, roi.h * sy);
  }
  for (const key of VISION_RECTS) {
    const r = rects[key];
    if (!r) continue;
    c.strokeStyle = '#7cf';
    c.strokeRect(r.x * sx, r.y * sy, r.w * sx, r.h * sy);
  }
}

function drawCrop(configure = true) {
  $('locate').disabled = locating || !ready || !roi || !marker;
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
  if (configure) pushConfig();
}

function setSource(image, reset = true) {
  const w = image.videoWidth || image.naturalWidth || image.width;
  const h = image.videoHeight || image.naturalHeight || image.height;
  if (!w || !h) throw new Error('Źródło nie udostępniło jeszcze klatki.');
  if (!reset && (source.width !== w || source.height !== h)) {
    // Without a fresh rectangle further movement is not permissible: the
    // minimap is no longer where it was.
    roi = marker = null;
    sourceRevision++;
    stopTracking();
    camera.setRegion(FRAME_REGION.minimap, null);
    rects = {viewport: null, battle: null, hp: null, mana: null};
    applyVisionRegions();
    disarm();
    status('Rozdzielczość źródła zmieniła się. Zaznacz minimapę ponownie.', 'error');
  }
  source.width = w; source.height = h;
  source.getContext('2d').drawImage(image, 0, 0);
  screenCanvas.width = Math.min(w, 1200);
  screenCanvas.height = Math.round(h * screenCanvas.width / w);
  ready = true;
  if (reset) {
    sourceRevision++;
    roi = w <= 512 && h <= 512 ? {x: 0, y: 0, w, h} : null;
    marker = roi ? {x: Math.floor(w / 2), y: Math.floor(h / 2)} : null;
    camera.setRegion(FRAME_REGION.minimap, roi);
    $('reference').hidden = true;
    $('coordinates').textContent = 'Pozycja nieznana';
  }
  drawScreen(); drawCrop(reset);
}

function stopShare() {
  sourceRevision++;
  if (stream || camera.session) stopTracking();
  stopLoop();
  if (stream) stream.getTracks().forEach(t => t.stop());
  stream = null; video.srcObject = null;
  lastDrawTime = null;
  $('snapshot').disabled = $('stop').disabled = $('live').disabled = $('frame-save').disabled = true;
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
    await locateOnce();
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
    $('snapshot').disabled = $('stop').disabled = $('live').disabled = $('frame-save').disabled = false;
    $('source').textContent = 'Udostępniony ekran · wybierz minimapę i skalibruj znacznik.';
    status('Pobrano klatkę. Zaznacz minimapę.');
    startLoop();
  } catch (e) { stopShare(); status(`Nie udało się udostępnić ekranu: ${e.message}`, 'error'); }
};

$('snapshot').onclick = () => { try { setSource(video, false); } catch (e) { status(e.message, 'error'); } };
// Zapis idzie z kanwy źródłowej, a nie z podglądu: podgląd jest przeskalowany
// do 800 px szerokości, a pomiary pikselowe pasków wymagają rozdzielczości,
// w jakiej klient je narysował.
$('frame-save').onclick = () => {
  if (!ready) { status('Najpierw udostępnij ekran albo wczytaj obraz.', 'error'); return; }
  source.toBlob(blob => {
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'combat-capture.png';
    link.click();
    URL.revokeObjectURL(url);
  }, 'image/png');
};
$('stop').onclick = () => { stopShare(); status('Udostępnianie zakończone.'); };

async function locateOnce() {
  if (stream) { await startTracking(true); return; }
  if (locating || !ready || !roi || !marker) return;
  if (stream) setSource(video, false);
  if (!roi || !marker) return;
  locating = true;
  $('locate').disabled = true;
  const revision = sourceRevision;
  const started = performance.now();
  status('Szukam pozycji na całym piętrze. Pozostań w miejscu.');
  $('coordinates').textContent = 'Pozycja nieznana';
  $('reference').hidden = true;
  try {
    const canvas = document.createElement('canvas');
    canvas.width = roi.w; canvas.height = roi.h;
    canvas.getContext('2d').drawImage(source, roi.x, roi.y, roi.w, roi.h, 0, 0, roi.w, roi.h);
    const options = {...brainConfig(), zoom: num('zoom'), demo};
    const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
    if (!blob) throw new Error('Nie udało się pobrać wycinka minimapy.');
    const body = new FormData();
    body.append('image', blob, 'minimap.png');
    body.append('options', JSON.stringify(options));
    const response = await fetch('/api/locate', {method: 'POST', body});
    const result = await response.json();
    if (revision !== sourceRevision) return;
    if (!response.ok) throw new Error(result.reason ?? 'Nie udało się odczytać pozycji.');
    $('coordinates').textContent = result.found && result.position
      ? `${result.position.x}, ${result.position.y}, ${result.position.z}` : 'Pozycja nieznana';
    $('json').textContent = JSON.stringify(result, null, 2);
    $('round-trip').textContent = `${(performance.now() - started).toFixed(1)} ms`;
    $('match-time').textContent = `${(result.match_ms ?? 0).toFixed(1)} ms`;
    $('search-area').textContent = 'całe piętro';
    $('metrics').textContent = result.best ? `Wynik: ${(result.best.score * 100).toFixed(2)}%` : '';
    status(result.reason, result.found ? 'ok' : 'error');
    if (result.found) {
      $('zoom').value = result.zoom;
      saveForm();
      if (result.preview) showReference(result.preview);
    }
  } catch (e) { if (revision === sourceRevision) status(e.message, 'error'); }
  finally { locating = false; $('locate').disabled = !ready || !roi || !marker; }
}
$('locate').onclick = locateOnce;

async function startTracking(restart = false) {
  if (startingTracking || !stream || !roi || !marker) return;
  startingTracking = true;
  const revision = sourceRevision;
  $('locate').disabled = true;
  try {
    if (!await pushConfig()) throw new Error('Nie udało się ustawić odczytu.');
    if (revision !== sourceRevision) return;
    if (restart || !camera.session) {
      camera.setSession(null);
      const response = await fetch('/api/capture', {method: 'POST'});
      const answer = await response.json();
      if (revision !== sourceRevision) return;
      if (!response.ok || !answer.session) throw new Error(answer.reason ?? 'Nie udało się uruchomić śledzenia.');
      camera.setSession(answer.session);
      $('coordinates').textContent = 'Pozycja nieznana';
      $('reference').hidden = true;
      positionAgeAtReceipt = null;
    }
    $('live').checked = true;
    startLoop();
    status('Śledzenie XYZ włączone. Podczas pierwszego odczytu pozostań w miejscu.');
    camera.sendFrame(video, 0);
  } catch (e) { $('live').checked = false; status(e.message, 'error'); }
  finally { startingTracking = false; $('locate').disabled = !ready || !roi || !marker; }
}

function stopTracking() {
  sourceRevision++;
  $('live').checked = false;
  camera.setSession(null);
  disarm();
  $('coordinates').textContent = 'Pozycja nieznana';
  $('actual-hz').textContent = '0.0';
  $('reference').hidden = true;
  positionAgeAtReceipt = null;
}

function showReference(url) {
  const img = $('reference');
  img.hidden = true;
  img.onload = () => { img.hidden = false; };
  img.onerror = () => { img.hidden = true; };
  img.src = url;
}

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
  // Only a new minimap rectangle invalidates the anchor, so only that one
  // stops tracking. The vision rectangles - game window, battle list, bars -
  // are read from the same frames and say nothing about where the character
  // is, so calibrating them mid-run must leave the run alone.
  if (calibTarget() === 'minimap' && $('live').checked) stopTracking();
  sourceRevision++;
  screenCanvas.setPointerCapture(e.pointerId);
});
screenCanvas.addEventListener('pointermove', e => {
  if (!dragging) return;
  const p = point(e, screenCanvas, source.width, source.height);
  const box = {x: Math.min(p.x, dragging.x), y: Math.min(p.y, dragging.y),
    w: Math.abs(p.x - dragging.x) + 1, h: Math.abs(p.y - dragging.y) + 1};
  if (calibTarget() === 'minimap') {
    roi = box;
    marker = {x: Math.floor(roi.w / 2), y: Math.floor(roi.h / 2)};
  } else {
    rects[calibTarget()] = box;
  }
  drawScreen();
});
screenCanvas.addEventListener('pointerup', () => {
  dragging = null;
  if (calibTarget() === 'minimap') { drawCrop(); return; }
  applyVisionRegions();
  pushConfig();
});
screenCanvas.addEventListener('pointercancel', () => { dragging = null; });
cropCanvas.addEventListener('pointerdown', e => {
  if (!roi) return;
  if ($('live').checked) stopTracking();
  marker = point(e, cropCanvas, cropCanvas.width, cropCanvas.height);
  sourceRevision++;
  drawCrop();
});

// --- konfiguracja ---

const DIRECTIONS = {NW: 'dir-nw', N: 'dir-n', NE: 'dir-ne', W: 'dir-w', E: 'dir-e',
  SW: 'dir-sw', S: 'dir-s', SE: 'dir-se'};
const HOTKEYS = {rope: 'hotkey-rope', ladder: 'hotkey-ladder', hole: 'hotkey-hole', shovel: 'hotkey-shovel'};

// --- leczenie ---

// healRules is the panel's copy of the rule list. The rows are rebuilt from it
// on every change rather than read back out of the DOM: the array is the truth
// that gets sent and remembered, and rebuilding keeps row ids in step with the
// order after a move.
let healRules = [];

const HEAL_DEFAULT = {enabled: true, resource: 'hp', below_pct: 60, hotkey: 'f1',
  cooldown_ms: 1000, min_mana_pct: 0};

function healConfig() {
  return {enabled: $('heal-on').checked, rules: healRules.map(r => ({...r}))};
}

function healField(row, id, label, value, type, attrs = {}) {
  const wrap = document.createElement('label');
  wrap.textContent = label;
  const input = document.createElement('input');
  input.id = id;
  input.type = type;
  if (type === 'checkbox') input.checked = value; else input.value = value;
  for (const [k, v] of Object.entries(attrs)) input.setAttribute(k, v);
  wrap.append(input);
  row.append(wrap);
  return input;
}

function healButton(row, id, text) {
  const b = document.createElement('button');
  b.id = id;
  b.className = 'secondary';
  b.textContent = text;
  row.append(b);
  return b;
}

function renderHealRules() {
  const host = $('heal-rules');
  const rows = healRules.map((rule, i) => {
    const row = document.createElement('div');
    row.className = 'route-grid';

    const on = healField(row, `heal-${i}-enabled`, 'Włączona', rule.enabled, 'checkbox');
    on.onclick = () => { healRules[i].enabled = on.checked; healChanged(); };

    const resource = document.createElement('select');
    resource.id = `heal-${i}-resource`;
    for (const [value, text] of [['hp', 'HP'], ['mana', 'mana']]) {
      const option = document.createElement('option');
      option.value = value; option.textContent = text;
      resource.append(option);
    }
    resource.value = rule.resource;
    resource.addEventListener('input', () => {
      healRules[i].resource = resource.value; healChanged();
    });
    const resourceLabel = document.createElement('label');
    resourceLabel.textContent = 'Zasób';
    resourceLabel.append(resource);
    row.append(resourceLabel);

    const below = healField(row, `heal-${i}-below`, 'Próg %', rule.below_pct, 'number',
      {min: 1, max: 99, step: 1});
    below.addEventListener('input', () => {
      healRules[i].below_pct = Number(below.value); healChanged();
    });

    const hotkey = healField(row, `heal-${i}-hotkey`, 'Klawisz', rule.hotkey, 'text');
    hotkey.addEventListener('input', () => {
      healRules[i].hotkey = hotkey.value.trim().toLowerCase(); healChanged();
    });

    const cooldown = healField(row, `heal-${i}-cooldown`, 'Cooldown (ms)', rule.cooldown_ms,
      'number', {min: 100, max: 60000, step: 50});
    cooldown.addEventListener('input', () => {
      healRules[i].cooldown_ms = Number(cooldown.value); healChanged();
    });

    const mana = healField(row, `heal-${i}-mana`, 'Min. mana %', rule.min_mana_pct,
      'number', {min: 0, max: 99, step: 1});
    mana.addEventListener('input', () => {
      healRules[i].min_mana_pct = Number(mana.value); healChanged();
    });

    healButton(row, `heal-${i}-up`, '▲').onclick = () => moveHealRule(i, -1);
    healButton(row, `heal-${i}-down`, '▼').onclick = () => moveHealRule(i, 1);
    healButton(row, `heal-${i}-del`, 'Usuń').onclick = () => {
      healRules.splice(i, 1); renderHealRules(); healChanged();
    };
    return row;
  });
  host.replaceChildren(...rows);
}

function moveHealRule(from, delta) {
  const to = from + delta;
  if (to < 0 || to >= healRules.length) return;
  [healRules[from], healRules[to]] = [healRules[to], healRules[from]];
  renderHealRules();
  healChanged();
}

function healChanged() {
  saveForm();
  pushConfig();
}

$('heal-add').onclick = () => {
  if (healRules.length >= 8) {
    status('Reguł leczenia może być najwyżej osiem.', 'error');
    return;
  }
  healRules.push({...HEAL_DEFAULT});
  renderHealRules();
  healChanged();
};
$('heal-on').onclick = healChanged;

// renderHealState is the one line the user reads to know whether healing is
// working: what fired last and how long ago, or why nothing did.
function renderHealState(heal) {
  if (!heal?.enabled) { $('heal-status').textContent = 'Leczenie wyłączone.'; return; }
  const parts = [];
  if (heal.last_hotkey && heal.last_age_ms != null) {
    parts.push(`ostatnie: ${heal.last_hotkey}, ${(heal.last_age_ms / 1000).toFixed(1).replace('.', ',')} s temu`);
  } else {
    parts.push('nic jeszcze nie poleciało');
  }
  if (heal.reason) parts.push(heal.reason);
  $('heal-status').textContent = parts.join(' · ');
}

function brainConfig() {
  return {
    zoom: num('zoom'),
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
    combat: combatConfig(),
    heal: healConfig(),
  };
}

// pushConfig sends the whole surface at once. The server validates it as one
// piece, so a single bad field is refused with a reason instead of half the
// form quietly taking effect.
async function pushConfig(tile) {
  if (!brainAvailable) return false;
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

// The settings live on the server for as long as it runs, but the form has to
// survive a page reload too - twelve key fields retyped after every refresh is
// not a workflow. Only the fields are remembered; the server stays the truth.
const REMEMBERED = ['floor', 'zoom', 'mask', 'threshold', 'gap', 'floor-auto', 'floor-radius',
  'speed', 'route-every', 'route-tolerance', 'route-loop', 'input-own-tile',
  'calib-target', 'grid-cols', 'grid-rows', 'decision-radius',
  'bar-width', 'bar-height', 'bar-border', 'bar-tolerance', 'black-max',
  'bar-colors', 'self-bar-on', 'self-bar-x', 'self-bar-y',
  'battle-bar-width', 'battle-bar-height', 'battle-bar-border', 'battle-pitch',
  'battle-frame', 'battle-frame-coverage',
  ...Object.values(HOTKEYS), ...Object.values(DIRECTIONS)];
const STORAGE_KEY = 'minimap-lab.panel';

function saveForm() {
  const state = {};
  for (const id of REMEMBERED) {
    const el = $(id);
    state[id] = el.type === 'checkbox' ? el.checked : el.value;
  }
  // The rules are a list, not a field, so they ride beside the remembered
  // inputs rather than in them. The master switch deliberately stays out: it
  // is a switch that makes the bot act, and those never survive a reload.
  state.heal_rules = healRules;
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); } catch { /* tryb prywatny */ }
}

function restoreForm() {
  let state;
  try { state = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null'); } catch { return; }
  if (!state) return;
  for (const id of REMEMBERED) {
    if (!(id in state)) continue;
    const el = $(id);
    if (el.type === 'checkbox') el.checked = !!state[id];
    else el.value = state[id];
  }
  if (Array.isArray(state.heal_rules)) {
    healRules = state.heal_rules.map(r => ({...HEAL_DEFAULT, ...r}));
    renderHealRules();
  }
}

// The three switches that actually make the bot act are deliberately not
// remembered: a reload must never resume walking on its own.
for (const id of ['zoom', 'mask', 'threshold', 'gap', 'floor', 'floor-auto', 'floor-radius',
  'speed', 'route-every', 'route-tolerance', 'route-loop', 'route-record', 'route-follow',
  'input-walk', 'input-actions', 'input-own-tile',
  'calib-target', 'grid-cols', 'grid-rows', 'decision-radius',
  'bar-width', 'bar-height', 'bar-border', 'bar-tolerance', 'black-max',
  'bar-colors', 'self-bar-on', 'self-bar-x', 'self-bar-y',
  'battle-bar-width', 'battle-bar-height', 'battle-bar-border', 'battle-pitch',
  'battle-frame', 'battle-frame-coverage',
  ...Object.values(HOTKEYS), ...Object.values(DIRECTIONS)]) {
  $(id).addEventListener('change', () => { saveForm(); pushConfig(); });
}

const NUMPAD = {NW: 'numpad7', N: 'numpad8', NE: 'numpad9', W: 'numpad4', E: 'numpad6',
  SW: 'numpad1', S: 'numpad2', SE: 'numpad3'};
const WSAD = {NW: 'q', N: 'w', NE: 'e', W: 'a', E: 'd', SW: 'z', S: 's', SE: 'c'};
function applyPreset(preset) {
  for (const [dir, id] of Object.entries(DIRECTIONS)) $(id).value = preset[dir] ?? '';
  saveForm();
  pushConfig();
}
$('dir-preset-numpad').onclick = () => applyPreset(NUMPAD);
$('dir-preset-wsad').onclick = () => applyPreset(WSAD);

// --- widzenie ---

const VISION_RECTS = ['viewport', 'battle', 'hp', 'mana'];
const VISION_RECT_LABELS = {viewport: 'okno gry', battle: 'battle lista', hp: 'pasek HP', mana: 'pasek many'};
const EMPTY_RECT = {x: 0, y: 0, w: 0, h: 0};
let rects = {viewport: null, battle: null, hp: null, mana: null};
let visionPending = false;

function calibTarget() { return $('calib-target').value || 'minimap'; }

// cropRect is the window the brain actually looks at: the character's tile
// grown by the decision radius plus one tile of margin, clipped to the game
// window. The margin is what lets a creature at the very edge of the radius
// still show its whole health bar - a clipped bar is not detected at all.
//
// The same formula lives in Go as CombatConfig.RecommendedCrop, but the value
// travels inside the config rather than being recomputed there, so the two
// sides cannot drift by a pixel.
function cropRect() {
  const v = rects.viewport;
  if (!v) return null;
  const cols = num('grid-cols') || 15, rows = num('grid-rows') || 11;
  const tw = v.w / cols, th = v.h / rows;
  const reach = Math.ceil(num('decision-radius') || 4) + 1;
  const col = Math.floor(cols / 2), row = Math.floor(rows / 2);
  const x0 = Math.max(0, col - reach), x1 = Math.min(cols, col + reach + 1);
  const y0 = Math.max(0, row - reach), y1 = Math.min(rows, row + reach + 1);
  return {
    x: v.x + Math.round(x0 * tw), y: v.y + Math.round(y0 * th),
    w: Math.round((x1 - x0) * tw), h: Math.round((y1 - y0) * th),
  };
}

function applyVisionRegions() {
  camera.setRegion(FRAME_REGION.viewport, cropRect());
  camera.setRegion(FRAME_REGION.battle, rects.battle);
  camera.setRegion(FRAME_REGION.hp, rects.hp);
  camera.setRegion(FRAME_REGION.mana, rects.mana);
  const named = VISION_RECTS.filter(k => rects[k]);
  $('vision-rects').textContent = named.length
    ? named.map(k => `${VISION_RECT_LABELS[k]}: ${rects[k].w} × ${rects[k].h} px`).join(' · ')
    : 'Nic jeszcze nie zaznaczone.';
}

function combatConfig() {
  return {
    viewport: rects.viewport ?? EMPTY_RECT,
    crop: cropRect() ?? EMPTY_RECT,
    battle: rects.battle ?? EMPTY_RECT,
    hp: rects.hp ?? EMPTY_RECT,
    mana: rects.mana ?? EMPTY_RECT,
    grid_cols: num('grid-cols'),
    grid_rows: num('grid-rows'),
    bar_width: num('bar-width'),
    bar_height: num('bar-height'),
    bar_border: num('bar-border'),
    bar_tolerance: num('bar-tolerance'),
    black_max: num('black-max'),
    bar_colors: $('bar-colors').value.split(/[\s,]+/).filter(Boolean),
    has_self_bar: $('self-bar-on').checked,
    self_bar_x: num('self-bar-x'),
    self_bar_y: num('self-bar-y'),
    decision_radius: num('decision-radius'),
    battle_bar_width: num('battle-bar-width'),
    battle_bar_height: num('battle-bar-height'),
    battle_bar_border: num('battle-bar-border'),
    battle_row_pitch: num('battle-pitch'),
    battle_frame: $('battle-frame').value.trim(),
    // The target frame shares its tolerance field with the bar colours: two
    // different thresholds in Go, one dial in the panel, because tuning them
    // separately has no practical benefit and every extra field is one more
    // thing to get wrong.
    battle_frame_tolerance: num('bar-tolerance'),
    battle_frame_coverage: num('battle-frame-coverage'),
  };
}

// fetchVision is diagnostics, so its failures are swallowed: a broken preview
// must never stop the frame loop that the actual bot depends on.
async function fetchVision() {
  if (visionPending) return;
  visionPending = true;
  try {
    const r = await fetch('/api/vision');
    if (r.ok) drawVision(await r.json());
  } catch { /* the preview is diagnostics, not a condition for the loop */ }
  finally { visionPending = false; }
}

// VISION_STATE_IDS covers every telemetry field the indicator fills - kept as
// a list so the "everything shows a dash" branch cannot forget one of them.
const VISION_STATE_IDS = [
  'vision-monsters', 'vision-bars', 'vision-rejected', 'vision-rows', 'vision-target', 'vision-hp', 'vision-mana',
];

// renderVisionState is the aggregate readout the spec's panel section calls
// for and phase 1's acceptance criterion depends on: monster count, HP/mana.
// It reads only state.combat, which rides on every snapshot - unlike the
// per-bar preview from /api/vision, this needs no extra request.
function renderVisionState(combat) {
  const c = combat?.calibrated ? combat : null;
  if (!c) {
    for (const id of VISION_STATE_IDS) $(id).textContent = '—';
    return;
  }
  $('vision-monsters').textContent = `${c.monsters_in_range}${c.mixed_crowd ? ' (mieszany tłum)' : ''}`;
  $('vision-bars').textContent = String(c.bars_total);
  $('vision-rejected').textContent = String(c.rejected_by_map);
  $('vision-rows').textContent = `${c.battle_rows}${c.battle_truncated ? ' (przewinięta)' : ''}`;
  // target_row counts from zero in the wire format, because that is what a
  // click into the battle list's row array needs; a human reading the panel
  // counts rows from one.
  $('vision-target').textContent = c.target_row == null ? 'brak' : String(c.target_row + 1);
  // An unreliable reading must not look like a real one: hp_ok/mana_ok false
  // means the calibration has likely slipped, and a dash says so where a
  // plausible-looking 0% would not.
  $('vision-hp').textContent = c.hp_ok ? `${Math.round(c.hp_pct * 100)}%` : '—';
  $('vision-mana').textContent = c.mana_ok ? `${Math.round(c.mana_pct * 100)}%` : '—';
}

function drawVision(view) {
  const crop = cropRect();
  if (!crop || !view?.have) return;
  const canvas = $('vision-canvas');
  canvas.width = crop.w; canvas.height = crop.h;
  const c = canvas.getContext('2d');
  if (stream) c.drawImage(video, crop.x, crop.y, crop.w, crop.h, 0, 0, crop.w, crop.h);
  else c.clearRect(0, 0, crop.w, crop.h);
  // Go marshals a nil slice as JSON null, and there is no creature bar at all
  // for most of a frame's life - an empty screen must not throw here.
  const bars = view.bars ?? [];
  // in_range is Go's own answer to the same radius threshold that decides
  // MonstersInRange - the preview colours by it rather than recomputing the
  // comparison, so the two can never silently drift apart.
  for (const b of bars) {
    c.strokeStyle = b.in_range ? '#ff2bd1' : '#8899aa';
    c.strokeRect(b.x + 0.5, b.y + 0.5, num('bar-width') - 1, num('bar-height') - 1);
  }
  $('vision-info').textContent = bars.length
    ? bars.map(b => `${b.dx.toFixed(2)},${b.dy.toFixed(2)} · ${b.dist.toFixed(2)} kratki · HP ${Math.round(100 * b.hp)}%`).join('  |  ')
    : 'Nie widzę żadnego stwora.';
}

$('vision-canvas').addEventListener('pointerdown', e => {
  const crop = cropRect();
  if (!crop) { status('Najpierw zaznacz okno gry.', 'error'); return; }
  const p = point(e, $('vision-canvas'), crop.w, crop.h);
  // The click lands on the bar's centre; the detector operates on its
  // top-left corner, so that is what gets stored.
  $('self-bar-x').value = Math.max(0, p.x - Math.floor(num('bar-width') / 2));
  $('self-bar-y').value = Math.max(0, p.y - Math.floor(num('bar-height') / 2));
  saveForm();
  pushConfig();
});

// --- uzbrajanie i pętla klatek ---

// ARM_DELAY_MS exists because the browser has focus at the moment its own
// button is clicked, and the driver memorises whatever window is focused when
// the request arrives. Without the wait it would always memorise the panel,
// and the first key would disarm on "okno gry straciło focus". The countdown
// is the window in which the user switches to the game.
const ARM_DELAY_MS = 5000;
let armTimer = null;

function cancelArm() {
  clearTimeout(armTimer);
  armTimer = null;
  $('input-arm').textContent = 'Uzbrój';
}

function beginArm() {
  if (armTimer) { cancelArm(); status('Uzbrajanie anulowane.'); return; }
  let left = Math.round(ARM_DELAY_MS / 1000);
  const tick = () => {
    if (left <= 0) {
      cancelArm();
      arm();
      return;
    }
    status(`Przełącz się na okno gry — uzbrojenie za ${left} s. Kliknij ponownie, żeby anulować.`);
    $('input-arm').textContent = `Anuluj (${left})`;
    left--;
    armTimer = setTimeout(tick, 1000);
  };
  tick();
}

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

$('input-arm').onclick = beginArm;
$('input-disarm').onclick = () => { cancelArm(); disarm(); status('Rozbrojono.'); };
$('input-calibrate').onclick = () => {
  calibrating = true;
  status('Kliknij na obrazie kratkę, na której stoi postać.');
};

function startLoop() {
  if (worker) return;
  worker = new Worker('/worker.js');
  worker.onmessage = () => {
    if (!stream) return;
    // Keep the visible preview moving even while matching a previous frame.
    // Drawing must not resend config or invalidate an in-flight match.
    if (lastDrawTime !== video.currentTime) {
      lastDrawTime = video.currentTime;
      if (!dragging) setSource(video, false);
    }
    if (!$('live').checked) return;
    camera.sendFrame(video, 0);
    pollState();
    if (positionAgeAtReceipt != null) {
      const age = positionAgeAtReceipt + performance.now() - positionReceivedAt;
      $('position-age').textContent = `${Math.round(age)} ms`;
      if (age > 1000) {
        $('coordinates').textContent = 'Pozycja nieznana';
        $('reference').hidden = true;
        $('actual-hz').textContent = '0.0';
      }
    }
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
  if ($('live').checked) startTracking(); else stopTracking();
});

async function pollState() {
  if (statePending) return;
  const session = camera.session;
  statePending = true;
  try {
    const response = await fetch('/api/state');
    const state = await response.json();
    if (response.ok && session === camera.session && $('live').checked) render(state);
  } catch (e) { status(e.message, 'error'); }
  finally { statePending = false; }
}

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
  if (state.state_version != null) {
    if (state.state_version <= lastStateVersion) return;
    lastStateVersion = state.state_version;
  }
  positionReceivedAt = performance.now();
  positionAgeAtReceipt = state.position ? state.position_age_ms : null;
  if (state.position && state.zoom > 0) {
    $('zoom').value = state.zoom;
    $('floor').value = state.position.z;
  }
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

  $('input-status').textContent = !controlAvailable ? 'Sterowanie wyłączone. Śledzenie XYZ działa niezależnie.' : state.armed
    ? 'Uzbrojony. Alt-tab albo cisza kamery rozbraja.'
    : 'Rozbrojony.';
  $('input-arm').disabled = !controlAvailable || (state.armed && !armTimer);
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

  if (state.position && state.preview_revision > 0 && state.preview_revision !== lastPreviewRev) {
    lastPreviewRev = state.preview_revision;
    showReference(`/api/preview?v=${state.preview_revision}`);
  } else if (!state.position) {
    $('reference').hidden = true;
    lastPreviewRev = -1;
  }
  if ($('grid-preview-on').checked && state.position) refreshGrid(state.position);

  renderVisionState(state.combat);
  renderHealState(state.heal);
  if ($('vision-preview').checked && state.combat?.calibrated) fetchVision();
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
  restoreForm();
  try {
    const info = await (await fetch('/api/info')).json();
    controlAvailable = info.control_available ?? true;
    const floors = info.floors?.length ? info.floors : [...Array(16).keys()];
    $('floor').replaceChildren(...floors.map(z => {
      const o = document.createElement('option');
      o.value = String(z); o.textContent = `Z = ${z}`;
      return o;
    }));
    $('floor').value = floors.includes(7) ? '7' : String(floors[0]);
    $('maps').textContent = info.message || `Mapy: ${info.maps}`;
    // The floor list arrives after restoreForm, so the remembered floor is
    // applied once the options it names actually exist.
    restoreForm();
  } catch { /* panel działa też bez /api/info */ }
  // A state poll costs one request and tells the panel whether control is even
  // available, which is what every disabled button below depends on.
  try {
    const r = await fetch('/api/state');
    if (r.ok) { brainAvailable = true; render(await r.json()); }
    else $('input-status').textContent = (await r.json()).reason ?? 'Sterowanie wyłączone.';
  } catch { /* jak wyżej */ }
})();
