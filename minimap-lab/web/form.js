// The settings live on the server for as long as it runs, but the form has to
// survive a page reload too - twelve key fields retyped after every refresh is
// not a workflow. Only the fields are remembered; the server stays the truth.

import {DIRECTIONS, HOTKEYS} from './control.js';

const STORAGE_KEY = 'minimap-lab.panel';

const REMEMBERED = ['floor', 'zoom', 'mask', 'threshold', 'gap', 'floor-auto', 'floor-radius',
  'speed', 'route-every', 'route-tolerance', 'route-loop', 'input-own-tile',
  'calib-target', 'grid-cols', 'grid-rows', 'decision-radius',
  'bar-width', 'bar-height', 'bar-border', 'bar-tolerance', 'black-max', 'bar-edge',
  'bar-colors', 'self-bar-on', 'self-bar-x', 'self-bar-y',
  'battle-bar-width', 'battle-bar-height', 'battle-bar-border', 'battle-pitch',
  'battle-frame', 'battle-frame-coverage',
  'battle-tolerance', 'battle-black-max', 'battle-edge',
  'battle-icon-x', 'battle-icon-y', 'battle-icon-size',
  ...Object.values(HOTKEYS), ...Object.values(DIRECTIONS)];

// Everything remembered is also watched, plus the four switches that actually
// make the bot act: they push the new setting to the server but never come back
// after a reload, because a refresh must not resume walking on its own. Derived
// rather than copied - two hand-synced lists is one list that goes stale.
const WATCHED = [...REMEMBERED,
  'route-record', 'route-follow', 'input-walk', 'input-actions'];

export function createForm(ctx) {
  const {$} = ctx.dom;
  const {localStorage} = ctx.env;

  // Saving stays shut until the panel has finished starting. The floor list
  // arrives from /api/info, so until then `#floor` has no options and reads as
  // empty - and anything that saved during that window (a tab click, a typed
  // setting) would write that empty value over the remembered floor, which the
  // second restore would then push back into the now-populated select.
  let armed = false;

  function save() {
    if (!armed) return;
    const state = {};
    for (const id of REMEMBERED) {
      const el = $(id);
      state[id] = el.type === 'checkbox' ? el.checked : el.value;
    }
    // The rules are a list, not a field, so they ride beside the remembered
    // inputs rather than in them. The master switch deliberately stays out: it
    // is a switch that makes the bot act, and those never survive a reload.
    state.heal_rules = ctx.heal.rules();
    state.tab = ctx.tabs.active();
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); } catch { /* tryb prywatny */ }
  }

  function restore({tab = true} = {}) {
    let state;
    try { state = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null'); } catch { return; }
    if (!state) return;
    for (const id of REMEMBERED) {
      if (!(id in state)) continue;
      const el = $(id);
      if (el.type === 'checkbox') el.checked = !!state[id];
      else el.value = state[id];
    }
    if (Array.isArray(state.heal_rules)) ctx.heal.setRules(state.heal_rules);
    // Restored without remembering: writing the tab back out here would be
    // a save triggered by a load, and the first one of those to run before
    // the rules are in would blank them.
    if (tab && state.tab) ctx.tabs.show(state.tab, {remember: false});
  }

  function mount() {
    // Every watched field gets the handler on 'change' - a browser fires that
    // for all of them (typed value committed on blur, a select changed, a
    // checkbox toggled). Number inputs - the calibration dials, spinner
    // arrows and all - additionally get it on 'input', so a nudge or a typed
    // digit updates the preview live instead of lagging a full field-blur
    // behind what is on screen. Free-text fields (hex-colour lists like
    // bar-colors, a single #rrggbb like battle-frame) deliberately do NOT get
    // 'input': the value is invalid on every keystroke but the last, and the
    // server would reject and flash an error on each one. Checking the
    // element's own `type` here - rather than hand-listing which ids count as
    // dials - means any future number field added to WATCHED gets this for
    // free and can never drift out of sync with a maintained list.
    const handler = () => { save(); ctx.pushConfig(); };
    for (const id of WATCHED) {
      const el = $(id);
      el.addEventListener('change', handler);
      if (el.type === 'number') el.addEventListener('input', handler);
    }
  }

  return {mount, save, restore, enable: () => { armed = true; }};
}
