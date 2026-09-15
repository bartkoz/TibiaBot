// The settings live on the server for as long as it runs, but the form has to
// survive a page reload too - twelve key fields retyped after every refresh is
// not a workflow. Only the fields are remembered; the server stays the truth.

import {DIRECTIONS, HOTKEYS} from './control.js';

export const STORAGE_KEY = 'minimap-lab.panel';

const REMEMBERED = ['floor', 'zoom', 'mask', 'threshold', 'gap', 'floor-auto', 'floor-radius',
  'speed', 'route-every', 'route-tolerance', 'route-loop', 'input-own-tile',
  'calib-target', 'grid-cols', 'grid-rows', 'decision-radius',
  'bar-width', 'bar-height', 'bar-border', 'bar-tolerance', 'black-max',
  'bar-colors', 'self-bar-on', 'self-bar-x', 'self-bar-y',
  'battle-bar-width', 'battle-bar-height', 'battle-bar-border', 'battle-pitch',
  'battle-frame', 'battle-frame-coverage',
  ...Object.values(HOTKEYS), ...Object.values(DIRECTIONS)];

// The three switches that actually make the bot act are deliberately absent
// from REMEMBERED but present here: they still push the new setting to the
// server, they just never come back after a reload. A refresh must not resume
// walking on its own.
const WATCHED = ['zoom', 'mask', 'threshold', 'gap', 'floor', 'floor-auto', 'floor-radius',
  'speed', 'route-every', 'route-tolerance', 'route-loop', 'route-record', 'route-follow',
  'input-walk', 'input-actions', 'input-own-tile',
  'calib-target', 'grid-cols', 'grid-rows', 'decision-radius',
  'bar-width', 'bar-height', 'bar-border', 'bar-tolerance', 'black-max',
  'bar-colors', 'self-bar-on', 'self-bar-x', 'self-bar-y',
  'battle-bar-width', 'battle-bar-height', 'battle-bar-border', 'battle-pitch',
  'battle-frame', 'battle-frame-coverage',
  ...Object.values(HOTKEYS), ...Object.values(DIRECTIONS)];

export function createForm(ctx) {
  const {$} = ctx.dom;
  const {localStorage} = ctx.env;

  function save() {
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

  function restore() {
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
    if (state.tab) ctx.tabs.show(state.tab, {remember: false});
  }

  function mount() {
    for (const id of WATCHED) {
      $(id).addEventListener('change', () => { save(); ctx.pushConfig(); });
    }
  }

  return {mount, save, restore};
}
