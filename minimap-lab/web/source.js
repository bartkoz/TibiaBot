// Where the pixels come from: a screenshot, the demo image, or a shared
// screen. Everything downstream reads the same offscreen canvas, so nothing
// else has to know which of the three it is looking at.

import {REGION as FRAME_REGION} from './camera.js';

export function createSource(ctx) {
  const {$, num, document} = ctx.dom;
  const {status} = ctx;
  const {Image, URL, navigator} = ctx.env;

  const screenCanvas = $('screen'), cropCanvas = $('crop');
  const canvas = document.createElement('canvas');
  const video = $('video');

  let stream = null, demo = false, ready = false;
  let revision = 0, lastDrawTime = null;

  function drawScreen() {
    const c = screenCanvas.getContext('2d');
    c.clearRect(0, 0, screenCanvas.width, screenCanvas.height);
    if (!ready) return;
    c.drawImage(canvas, 0, 0, screenCanvas.width, screenCanvas.height);
    const sx = screenCanvas.width / canvas.width, sy = screenCanvas.height / canvas.height;
    const roi = ctx.selection.roi();
    if (roi) {
      c.strokeStyle = '#ff6e94'; c.lineWidth = 2;
      c.strokeRect(roi.x * sx, roi.y * sy, roi.w * sx, roi.h * sy);
    }
    for (const r of ctx.vision.drawnRects()) {
      c.strokeStyle = '#7cf';
      c.strokeRect(r.x * sx, r.y * sy, r.w * sx, r.h * sy);
    }
  }

  function drawCrop(configure = true) {
    ctx.position.syncLocateButton();
    const roi = ctx.selection.roi(), marker = ctx.selection.marker();
    if (!roi) { $('roi-info').textContent = 'Zaznacz minimapę na obrazie.'; return; }
    cropCanvas.width = roi.w; cropCanvas.height = roi.h;
    const c = cropCanvas.getContext('2d');
    c.drawImage(canvas, roi.x, roi.y, roi.w, roi.h, 0, 0, roi.w, roi.h);
    if (marker) {
      const r = num('mask');
      c.fillStyle = '#ff578633'; c.strokeStyle = '#ff6e94'; c.lineWidth = 1;
      c.fillRect(marker.x - r, marker.y - r, 2 * r + 1, 2 * r + 1);
      c.strokeRect(marker.x - r - .5, marker.y - r - .5, 2 * r + 1, 2 * r + 1);
      c.fillStyle = '#fff'; c.fillRect(marker.x, marker.y, 1, 1);
    }
    $('roi-info').textContent = `Wycinek: x=${roi.x}, y=${roi.y}, ${roi.w} × ${roi.h} px` +
      (marker ? ` · znacznik: ${marker.x}, ${marker.y}` : ' · wskaż znacznik postaci');
    ctx.camera.setRegion(FRAME_REGION.minimap, roi);
    if (configure) ctx.pushConfig();
  }

  function setSource(image, reset = true) {
    const w = image.videoWidth || image.naturalWidth || image.width;
    const h = image.videoHeight || image.naturalHeight || image.height;
    if (!w || !h) throw new Error('Źródło nie udostępniło jeszcze klatki.');
    if (!reset && (canvas.width !== w || canvas.height !== h)) {
      // Without a fresh rectangle further movement is not permissible: the
      // minimap is no longer where it was.
      ctx.selection.clear();
      revision++;
      ctx.position.stopTracking();
      ctx.vision.clearRects();
      ctx.control.disarm();
      status('Rozdzielczość źródła zmieniła się. Zaznacz minimapę ponownie.', 'error');
    }
    canvas.width = w; canvas.height = h;
    canvas.getContext('2d').drawImage(image, 0, 0);
    screenCanvas.width = Math.min(w, 1200);
    screenCanvas.height = Math.round(h * screenCanvas.width / w);
    ready = true;
    if (reset) {
      revision++;
      ctx.selection.autoSelect(w, h);
      ctx.position.clearReadout();
    }
    drawScreen(); drawCrop(reset);
  }

  function stopShare() {
    revision++;
    if (stream || ctx.camera.session) ctx.position.stopTracking();
    ctx.loop.stop();
    if (stream) stream.getTracks().forEach(t => t.stop());
    stream = null; video.srcObject = null;
    lastDrawTime = null;
    $('snapshot').disabled = $('stop').disabled = $('live').disabled = $('frame-save').disabled = true;
  }

  async function readImage(url) {
    const image = new Image(); image.src = url; await image.decode(); return image;
  }

  // refreshPreview keeps the visible preview moving even while a previous
  // frame is still being matched. Drawing must not resend config or invalidate
  // an in-flight match, and it must not fight a drag in progress.
  function refreshPreview() {
    if (lastDrawTime === video.currentTime) return;
    lastDrawTime = video.currentTime;
    if (!ctx.selection.isDragging()) setSource(video, false);
  }

  function mount() {
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
        ctx.selection.setMarker({x: 94, y: 94}); drawCrop();
        $('source').textContent = 'DEMO · syntetyczny obraz · oczekiwana pozycja: 32200, 32180, 7';
        await ctx.position.locateOnce();
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
        ctx.loop.start();
      } catch (e) { stopShare(); status(`Nie udało się udostępnić ekranu: ${e.message}`, 'error'); }
    };

    $('snapshot').onclick = () => { try { setSource(video, false); } catch (e) { status(e.message, 'error'); } };

    // The save comes off the source canvas, not the preview: the preview is
    // scaled down to 1200 px wide, and pixel measurements of the bars need the
    // resolution the client drew them at.
    $('frame-save').onclick = () => {
      if (!ready) { status('Najpierw udostępnij ekran albo wczytaj obraz.', 'error'); return; }
      canvas.toBlob(blob => {
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = 'combat-capture.png';
        link.click();
        URL.revokeObjectURL(url);
      }, 'image/png');
    };

    $('stop').onclick = () => { stopShare(); status('Udostępnianie zakończone.'); };
  }

  return {
    mount, drawScreen, drawCrop, setSource, stopShare, refreshPreview,
    canvas, video, screenCanvas, cropCanvas,
    isReady: () => ready,
    isDemo: () => demo,
    hasStream: () => !!stream,
    revision: () => revision,
    bump: () => { revision++; },
  };
}
