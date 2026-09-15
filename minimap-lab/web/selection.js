// Dragging rectangles out of the preview. Which rectangle a drag produces is
// the calibration target's business, not this module's: it measures, clamps
// and hands the box to whoever owns that kind of rectangle.

import {point} from './dom.js';
import {REGION as FRAME_REGION} from './camera.js';

export function createSelection(ctx) {
  const {$} = ctx.dom;
  const {status} = ctx;

  let roi = null, marker = null, dragging = null, calibrating = false;

  const target = () => $('calib-target').value || 'minimap';

  function clear() {
    roi = marker = null;
    ctx.camera.setRegion(FRAME_REGION.minimap, null);
  }

  // autoSelect takes the whole image when it is small enough to be a minimap
  // crop already; a full screenshot gets nothing and waits for a drag.
  function autoSelect(w, h) {
    roi = w <= 512 && h <= 512 ? {x: 0, y: 0, w, h} : null;
    marker = roi ? {x: Math.floor(w / 2), y: Math.floor(h / 2)} : null;
    ctx.camera.setRegion(FRAME_REGION.minimap, roi);
  }

  function mount() {
    const screenCanvas = ctx.source.screenCanvas, cropCanvas = ctx.source.cropCanvas;
    const size = () => ctx.source.canvas;

    screenCanvas.addEventListener('pointerdown', e => {
      if (!ctx.source.isReady()) return;
      if (calibrating) {
        const p = point(e, screenCanvas, size().width, size().height);
        calibrating = false;
        ctx.pushConfig({x: p.x / size().width, y: p.y / size().height});
        status(`Kratka postaci ustawiona na ${p.x}, ${p.y}.`, 'ok');
        return;
      }
      dragging = point(e, screenCanvas, size().width, size().height);
      // Only a new minimap rectangle invalidates the anchor, so only that one
      // stops tracking. The vision rectangles - game window, battle list, bars -
      // are read from the same frames and say nothing about where the character
      // is, so calibrating them mid-run must leave the run alone.
      if (target() === 'minimap' && $('live').checked) ctx.position.stopTracking();
      ctx.source.bump();
      screenCanvas.setPointerCapture(e.pointerId);
    });

    screenCanvas.addEventListener('pointermove', e => {
      if (!dragging) return;
      const p = point(e, screenCanvas, size().width, size().height);
      const box = {x: Math.min(p.x, dragging.x), y: Math.min(p.y, dragging.y),
        w: Math.abs(p.x - dragging.x) + 1, h: Math.abs(p.y - dragging.y) + 1};
      if (target() === 'minimap') {
        roi = box;
        marker = {x: Math.floor(roi.w / 2), y: Math.floor(roi.h / 2)};
      } else {
        ctx.vision.setRect(target(), box);
      }
      ctx.source.drawScreen();
    });

    screenCanvas.addEventListener('pointerup', () => {
      dragging = null;
      if (target() === 'minimap') { ctx.source.drawCrop(); return; }
      ctx.vision.applyRegions();
      ctx.pushConfig();
    });

    screenCanvas.addEventListener('pointercancel', () => { dragging = null; });

    cropCanvas.addEventListener('pointerdown', e => {
      if (!roi) return;
      if ($('live').checked) ctx.position.stopTracking();
      marker = point(e, cropCanvas, cropCanvas.width, cropCanvas.height);
      ctx.source.bump();
      ctx.source.drawCrop();
    });
  }

  return {
    mount, clear, autoSelect, target,
    roi: () => roi,
    marker: () => marker,
    setMarker: m => { marker = m; },
    isDragging: () => dragging !== null,
    startCalibration: () => { calibrating = true; },
  };
}
