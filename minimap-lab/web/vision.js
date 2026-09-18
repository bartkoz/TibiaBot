// Seeing: which rectangles of the client the brain looks at, and what it
// reports back from them. The counting itself is Go's; this module chooses the
// rectangles, ships the settings and draws what came back.

import {point} from './dom.js';
import {REGION as FRAME_REGION} from './camera.js';

const VISION_RECTS = ['viewport', 'battle', 'hp', 'mana'];
const VISION_RECT_LABELS = {viewport: 'okno gry', battle: 'battle lista', hp: 'pasek HP', mana: 'pasek many'};
const EMPTY_RECT = {x: 0, y: 0, w: 0, h: 0};

// VISION_STATE_IDS covers every telemetry field the indicator fills - kept as
// a list so the "everything shows a dash" branch cannot forget one of them.
const VISION_STATE_IDS = [
  'vision-monsters', 'vision-bars', 'vision-rejected', 'vision-rows', 'vision-target', 'vision-hp', 'vision-mana',
];

const TAB = 'walka';

export function createVision(ctx) {
  const {$, num} = ctx.dom;
  const {api, status} = ctx;

  let rects = {viewport: null, battle: null, hp: null, mana: null};
  let pending = false;

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

  function applyRegions() {
    ctx.camera.setRegion(FRAME_REGION.viewport, cropRect());
    ctx.camera.setRegion(FRAME_REGION.battle, rects.battle);
    ctx.camera.setRegion(FRAME_REGION.hp, rects.hp);
    ctx.camera.setRegion(FRAME_REGION.mana, rects.mana);
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
      bar_edge_tolerance: num('bar-edge'),
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
      battle_frame_coverage: num('battle-frame-coverage'),
      battle_bar_tolerance: num('battle-tolerance'),
      battle_black_max: num('battle-black-max'),
      battle_edge_tolerance: num('battle-edge'),
      // Its own dial, independent from battle_bar_tolerance: that one is a
      // loose colour-match tolerance sized for matching a health-bar FILL
      // colour across antialiasing, and reusing it here would accept a very
      // wide band of reds/oranges/browns around the target-frame colour - a
      // real risk of a creature's own sprite tripping a false "targeted"
      // reading, which is the costly direction (clicking an already-targeted
      // entry cancels the attack).
      battle_frame_tolerance: num('battle-frame-tolerance'),
      battle_icon_offset_x: num('battle-icon-x'),
      battle_icon_offset_y: num('battle-icon-y'),
      battle_icon_size: num('battle-icon-size'),
    };
  }

  // fetchVision is diagnostics, so its failures are swallowed: a broken preview
  // must never stop the frame loop that the actual bot depends on.
  async function fetchVision() {
    if (pending) return;
    pending = true;
    try {
      const r = await api.vision();
      if (r.ok) drawVision(await r.json());
    } catch { /* the preview is diagnostics, not a condition for the loop */ }
    finally { pending = false; }
  }

  // renderState is the aggregate readout: monster count, battle rows, HP/mana.
  // It reads only state.combat, which rides on every snapshot - unlike the
  // per-bar preview from /api/vision, this needs no extra request.
  function renderState(combat) {
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
    if (ctx.source.hasStream()) {
      c.drawImage(ctx.source.video, crop.x, crop.y, crop.w, crop.h, 0, 0, crop.w, crop.h);
    } else c.clearRect(0, 0, crop.w, crop.h);
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

  // blind answers one question: is healing switched on while something it
  // actually reads cannot be read?
  function blind(combat) {
    if (!combat?.calibrated) return true;
    const rules = ctx.heal.rules().filter(r => r.enabled);
    const needsHP = rules.some(r => r.resource === 'hp');
    const needsMana = rules.some(r => r.resource === 'mana' || r.min_mana_pct > 0);
    return (needsHP && !combat.hp_ok) || (needsMana && !combat.mana_ok);
  }

  function mount() {
    $('vision-canvas').addEventListener('pointerdown', e => {
      const crop = cropRect();
      if (!crop) { status('Najpierw zaznacz okno gry.', 'error'); return; }
      const p = point(e, $('vision-canvas'), crop.w, crop.h);
      // The click lands on the bar's centre; the detector operates on its
      // top-left corner, so that is what gets stored.
      $('self-bar-x').value = Math.max(0, p.x - Math.floor(num('bar-width') / 2));
      $('self-bar-y').value = Math.max(0, p.y - Math.floor(num('bar-height') / 2));
      ctx.form.save();
      ctx.pushConfig();
    });
  }

  // reveal is the gated fetch on its own, so opening the tab pulls a preview
  // without waiting for the next snapshot.
  function reveal(state) {
    if ($('vision-preview').checked && state.combat?.calibrated && ctx.tabs.visible(TAB)) {
      fetchVision();
    }
  }

  function render(state) {
    renderState(state.combat);
    // The warning wins over the count: healing switched on without calibrated
    // bars is a switch that silently cannot work, and that is worth a mark on
    // a tab the user is not looking at. A monster count is merely useful.
    //
    // Which bars have to be readable comes from the rules themselves, exactly
    // as it does in Go: a rule reads its own resource, and only a rule that
    // costs mana needs the mana bar (internal/heal/rules.go:44-47 - a rule at
    // min_mana_pct 0 "deliberately does not care whether the mana bar is
    // readable at all"). Demanding both would put a permanent warning on the
    // common HP-only setup and swallow the monster count with it.
    const c = state.combat;
    if (state.heal?.enabled && blind(c)) {
      ctx.tabs.setBadge('walka', {kind: 'warn', text: '!', label: 'leczenie bez odczytu pasków'});
    } else if (c?.calibrated && c.monsters_in_range > 0) {
      ctx.tabs.setBadge('walka', {
        kind: 'count', text: String(c.monsters_in_range),
        label: `${c.monsters_in_range} potworów w promieniu`,
      });
    } else ctx.tabs.setBadge('walka', null);
    reveal(state);
  }

  return {
    tab: TAB,
    mount, render, reveal, applyRegions,
    config: () => ({combat: combatConfig()}),
    setRect: (key, box) => { rects[key] = box; },
    clearRects: () => {
      rects = {viewport: null, battle: null, hp: null, mana: null};
      applyRegions();
    },
    // drawnRects feeds the preview overlay in source.js and keeps the order
    // stable, so the rectangles are always drawn the same way round.
    drawnRects: () => VISION_RECTS.map(k => rects[k]).filter(Boolean),
  };
}
