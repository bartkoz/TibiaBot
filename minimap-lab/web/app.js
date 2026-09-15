// The panel is a camera and a window onto the bot's state. Every decision it
// used to make - which step, which waypoint, what a failed step means - now
// lives in Go. What is left is choosing the rectangles, filling in the
// settings, and drawing what comes back.
//
// createPanel builds one panel over one environment. Nothing runs on import:
// the browser entry and every test construct their own, so no two panels share
// module state the way the old single script did.

import {Camera, makeCut} from './camera.js';
import {createDom} from './dom.js';
import {createApi} from './api.js';
import {createForm} from './form.js';
import {createSource} from './source.js';
import {createSelection} from './selection.js';
import {createPosition} from './position.js';
import {createRoute} from './route.js';
import {createVision} from './vision.js';
import {createHeal} from './heal.js';
import {createControl} from './control.js';
import {createBlocks} from './blocks.js';

const LOOP_INTERVAL_MS = 100;

export function createPanel(env) {
  const dom = createDom(env.document);
  const {$} = dom;
  const api = createApi(env);

  let controlAvailable = false, brainAvailable = false;
  let worker = null, statePending = false, lastStateVersion = -1;

  const ctx = {env, dom, api};
  ctx.status = (text, kind = '') => { $('status').textContent = text; $('status').className = kind; };
  ctx.controlAvailable = () => controlAvailable;
  ctx.brainConfig = brainConfig;
  ctx.pushConfig = pushConfig;
  ctx.loop = {start: startLoop, stop: stopLoop};
  // Replaced by the real tab strip in tabs.js; until then every panel counts
  // as on screen, which is what a single long page always was.
  ctx.tabs = {visible: () => true};

  ctx.camera = new Camera({
    fetch: (...a) => env.fetch(...a),
    cut: makeCut(env.document),
    onSnapshot: render,
    onError: reason => ctx.status(reason, 'error'),
  });

  // The modules reach each other only through ctx, and only at call time. That
  // is what keeps the genuine cycle - the preview is drawn by source.js but the
  // rectangles on it belong to selection.js and vision.js - from ever becoming
  // an import cycle.
  ctx.source = createSource(ctx);
  ctx.selection = createSelection(ctx);
  ctx.position = createPosition(ctx);
  ctx.route = createRoute(ctx);
  ctx.vision = createVision(ctx);
  ctx.heal = createHeal(ctx);
  ctx.control = createControl(ctx);
  ctx.blocks = createBlocks(ctx);
  ctx.form = createForm(ctx);

  // Order is the render order, and it is load-bearing in one place: position
  // reports a match reason into the status line, and the executor's "stopped"
  // has to be able to overwrite it, so control comes after position.
  const modules = [ctx.source, ctx.selection, ctx.position, ctx.route,
    ctx.vision, ctx.heal, ctx.control, ctx.blocks, ctx.form];

  // brainConfig is assembled from the modules but still travels as one
  // document. The server validates it as a single piece, so one bad field is
  // refused with a reason instead of half the form quietly taking effect.
  function brainConfig() {
    return Object.assign({}, ...modules.map(m => m.config?.() ?? {}));
  }

  async function pushConfig(tile) {
    if (!brainAvailable) return false;
    const body = {brain: brainConfig(), ...ctx.control.inputConfig()};
    if (tile) body.tile = tile;
    try {
      const r = await api.config(body);
      const answer = await r.json();
      if (!r.ok) ctx.status(answer.reason ?? 'Konfiguracja odrzucona.', 'error');
      return r.ok;
    } catch (e) { ctx.status(e.message, 'error'); return false; }
  }

  function startLoop() {
    if (worker) return;
    worker = new env.Worker('/worker.js');
    worker.onmessage = () => {
      if (!ctx.source.hasStream()) return;
      ctx.source.refreshPreview();
      if (!$('live').checked) return;
      ctx.camera.sendFrame(ctx.source.video, 0);
      pollState();
      ctx.position.tickAge();
    };
    worker.postMessage({intervalMS: LOOP_INTERVAL_MS});
  }

  function stopLoop() {
    if (!worker) return;
    worker.postMessage({stop: true});
    worker.terminate();
    worker = null;
  }

  async function pollState() {
    if (statePending) return;
    const session = ctx.camera.session;
    statePending = true;
    try {
      const response = await api.state();
      const state = await response.json();
      if (response.ok && session === ctx.camera.session && $('live').checked) render(state);
    } catch (e) { ctx.status(e.message, 'error'); }
    finally { statePending = false; }
  }

  // render holds the one guard the whole panel depends on: a snapshot no newer
  // than the one already drawn is dropped before any module sees it. Spread
  // across the modules it would be nine chances to get it wrong.
  function render(state) {
    if (state.state_version != null) {
      if (state.state_version <= lastStateVersion) return;
      lastStateVersion = state.state_version;
    }
    $('json').textContent = JSON.stringify(state, null, 2);
    for (const m of modules) m.render?.(state);
  }

  async function start() {
    ctx.form.restore();
    try {
      const info = await (await api.info()).json();
      controlAvailable = info.control_available ?? true;
      const floors = info.floors?.length ? info.floors : [...Array(16).keys()];
      $('floor').replaceChildren(...floors.map(z => {
        const o = env.document.createElement('option');
        o.value = String(z); o.textContent = `Z = ${z}`;
        return o;
      }));
      $('floor').value = floors.includes(7) ? '7' : String(floors[0]);
      $('maps').textContent = info.message || `Mapy: ${info.maps}`;
      // The floor list arrives after the first restore, so the remembered floor
      // is applied a second time, once the options it names actually exist.
      ctx.form.restore();
    } catch { /* panel działa też bez /api/info */ }
    // A state poll costs one request and tells the panel whether control is even
    // available, which is what every disabled button below depends on.
    try {
      const r = await api.state();
      if (r.ok) { brainAvailable = true; render(await r.json()); }
      else $('input-status').textContent = (await r.json()).reason ?? 'Sterowanie wyłączone.';
    } catch { /* jak wyżej */ }
  }

  // stop puts the panel down: the browser entry never calls it, but a test that
  // builds one has to be able to release the shared screen and the loop.
  function stop() { ctx.source.stopShare(); }

  for (const m of modules) m.mount?.();

  return {start, stop};
}
