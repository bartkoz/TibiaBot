// What the executor learned the hard way. Route costs know terrain and walls
// but not furniture, so a shop counter sits on a tile the map calls walkable;
// the executor works those out from failed steps and this is where they show.

import {point} from './dom.js';

const TAB = 'trasa';
const GRID_RADIUS = 32;

// How long a click's answer holds the status line against the frame loop.
const CLICK_MESSAGE_MS = 4000;

const GRID_COLOURS = {
  free: [40, 70, 40, 255], wall: [150, 40, 40, 255], missing: [40, 40, 45, 255],
  temp: [220, 170, 40, 255], perm: [230, 80, 230, 255],
};

export function gridPixels(cells, side) {
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

export function createBlocks(ctx) {
  const {$} = ctx.dom;
  const {api} = ctx;
  const {ImageData} = ctx.env;

  let gridWindow = null, pending = false, clickMessageUntil = 0;

  async function refresh(p) {
    if (pending) return;
    pending = true;
    try {
      const res = await api.grid(p.x, p.y, p.z, GRID_RADIUS);
      if (!res.ok) return;
      const origin = (res.headers.get('X-Grid-Origin') ?? '0,0').split(',').map(Number);
      const cells = new Uint8Array(await res.arrayBuffer());
      gridWindow = {origin, z: p.z, cells};
      const side = 2 * GRID_RADIUS + 1;
      const canvas = $('grid-canvas');
      canvas.width = canvas.height = side;
      canvas.getContext('2d').putImageData(new ImageData(gridPixels(cells, side), side, side), 0, 0);
    } catch { /* podgląd jest diagnostyką, nie blokuje pętli */ }
    finally { pending = false; }
  }

  function mount() {
    $('grid-canvas').addEventListener('click', async event => {
      if (!gridWindow) return;
      const side = 2 * GRID_RADIUS + 1;
      const p = point(event, $('grid-canvas'), side, side);
      const x = gridWindow.origin[0] + p.x, y = gridWindow.origin[1] + p.y;
      const r = await api.deleteBlock({x, y, z: gridWindow.z});
      const answer = await r.json().catch(() => ({}));
      // The frame loop writes this same line ten times a second, so without a
      // hold the answer to a click would be gone before it could be read.
      clickMessageUntil = ctx.env.performance.now() + CLICK_MESSAGE_MS;
      $('blocks-status').textContent = answer.cleared
        ? `Usunięto nauczoną blokadę na ${x}, ${y}.`
        : `Na ${x}, ${y} nie ma nauczonej blokady.`;
    });
  }

  function reveal(state) {
    if ($('grid-preview-on').checked && state.position && ctx.tabs.visible(TAB)) {
      refresh(state.position);
    }
  }

  function render(state) {
    if (ctx.env.performance.now() >= clickMessageUntil) {
      const log = state.log ?? [];
      $('blocks-status').textContent = log.length ? log[log.length - 1].text : '—';
    }
    reveal(state);
  }

  return {tab: TAB, mount, render, reveal};
}
