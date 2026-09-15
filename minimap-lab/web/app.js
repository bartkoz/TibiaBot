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
import {createTabs} from './tabs.js';

const LOOP_INTERVAL_MS = 100;
const DIAGNOSTICS_TAB = 'diagnostyka';

export function createPanel(env) {
  const dom = createDom(env.document);
  const {$} = dom;
  const api = createApi(env);

  let controlAvailable = false, brainAvailable = false;
  let worker = null, statePending = false, lastStateVersion = -1, lastState = null;

  const ctx = {env, dom, api};
  ctx.status = (text, kind = '') => { $('status').textContent = text; $('status').className = kind; };
  ctx.controlAvailable = () => controlAvailable;
  ctx.brainConfig = brainConfig;
  ctx.pushConfig = pushConfig;
  ctx.loop = {start: startLoop, stop: stopLoop};
  ctx.reveal = reveal;
  ctx.forgetStateVersion = forgetStateVersion;

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
  ctx.tabs = createTabs(ctx);
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
  //
  // `modules` is read by brainConfig below, so no module may call
  // ctx.brainConfig or ctx.pushConfig from inside its own factory - it would
  // reach this binding before it exists. Doing it from mount() or a listener,
  // which is what they all do, is always safe.
  const modules = [ctx.tabs, ctx.source, ctx.selection, ctx.position, ctx.route,
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
    lastState = state;
    renderJSON(state);
    for (const m of modules) m.render?.(state);
  }

  // The dump is the most expensive thing the loop does - the whole snapshot
  // stringified with indentation - and it lives on a tab that is hidden by
  // default. Paying that ten times a second for a <pre> nobody can see is the
  // cost the tab gating exists to avoid.
  function renderJSON(state) {
    if (ctx.tabs.visible(DIAGNOSTICS_TAB)) {
      $('json').textContent = JSON.stringify(state, null, 2);
    }
  }

  // The version counter belongs to the server process, not to the panel. A
  // restarted server publishes version 1 again, and without this the guard
  // would drop every snapshot from it and freeze the panel on stale data with
  // nothing on screen to say so.
  function forgetStateVersion() {
    lastStateVersion = -1;
    lastState = null;
  }

  // reveal runs when a tab comes on screen. The two gated previews only fetch
  // while their own tab is visible, so without this a panel opened between
  // snapshots would sit empty - forever in single-shot mode, where no further
  // snapshot is coming. It is deliberately not a full re-render: position
  // stamps the age clock in render(), and restarting that on every tab switch
  // would make a stale reading look fresh.
  function reveal() {
    if (!lastState) return;
    renderJSON(lastState);
    for (const m of modules) m.reveal?.(lastState);
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
      // The tab is left alone: /api/info is a round trip, and a user who
      // picked a tab during it must not have it yanked back underneath them.
      ctx.form.restore({tab: false});
    } catch { /* panel działa też bez /api/info */ }
    // A state poll costs one request and tells the panel whether control is even
    // available, which is what every disabled button below depends on.
    try {
      const r = await api.state();
      if (r.ok) { brainAvailable = true; render(await r.json()); }
      else $('input-status').textContent = (await r.json()).reason ?? 'Sterowanie wyłączone.';
    } catch { /* jak wyżej */ }
    // Only now may anything be written back to storage: before this the floor
    // select had no options and would have been saved as empty.
    ctx.form.enable();
  }

  // stop puts the panel down: the browser entry never calls it, but a test that
  // builds one has to be able to release the shared screen and the loop.
  function stop() {
    // The arming countdown lives in a timer, not in the stream: putting the
    // panel down without cancelling it would still POST /api/arm five seconds
    // later, into a panel that is no longer running.
    ctx.control.cancelArm();
    ctx.source.stopShare();
  }

  // A module that gates itself on a tab declares which one, and the name is
  // checked here rather than trusted: tabs.visible() answers false for an id
  // that does not exist, so a typo would disable that preview forever without
  // a word. This turns it into a failure at construction, which every test
  // trips over immediately.
  for (const m of modules) {
    if (m.tab && !ctx.tabs.knows(m.tab)) throw new Error(`nieznana zakładka modułu: ${m.tab}`);
  }

  for (const m of modules) m.mount?.();

  return {start, stop};
}
