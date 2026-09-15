// Finding the character on the map and keeping it found. One full sweep of a
// floor to start, then local confirmations at frame rate; everything here is
// about which of the two is running and what it last reported.

export function createPosition(ctx) {
  const {$, num} = ctx.dom;
  const {api, status} = ctx;
  const {FormData, performance, document} = ctx.env;

  let locating = false, startingTracking = false, lastPreviewRev = -1;
  let receivedAt = 0, ageAtReceipt = null;

  function syncLocateButton() {
    $('locate').disabled = locating || !ctx.source.isReady()
      || !ctx.selection.roi() || !ctx.selection.marker();
  }

  function clearReadout() {
    $('reference').hidden = true;
    $('coordinates').textContent = 'Pozycja nieznana';
  }

  function showReference(url) {
    const img = $('reference');
    img.hidden = true;
    img.onload = () => { img.hidden = false; };
    img.onerror = () => { img.hidden = true; };
    img.src = url;
  }

  async function locateOnce() {
    if (ctx.source.hasStream()) { await startTracking(true); return; }
    const roi = ctx.selection.roi(), marker = ctx.selection.marker();
    if (locating || !ctx.source.isReady() || !roi || !marker) return;
    locating = true;
    $('locate').disabled = true;
    const revision = ctx.source.revision();
    const started = performance.now();
    status('Szukam pozycji na całym piętrze. Pozostań w miejscu.');
    $('coordinates').textContent = 'Pozycja nieznana';
    $('reference').hidden = true;
    try {
      const canvas = document.createElement('canvas');
      canvas.width = roi.w; canvas.height = roi.h;
      canvas.getContext('2d').drawImage(ctx.source.canvas, roi.x, roi.y, roi.w, roi.h, 0, 0, roi.w, roi.h);
      const options = {...ctx.brainConfig(), zoom: num('zoom'), demo: ctx.source.isDemo()};
      const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      if (!blob) throw new Error('Nie udało się pobrać wycinka minimapy.');
      const body = new FormData();
      body.append('image', blob, 'minimap.png');
      body.append('options', JSON.stringify(options));
      const response = await api.locate(body);
      const result = await response.json();
      if (revision !== ctx.source.revision()) return;
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
        ctx.form.save();
        if (result.preview) showReference(result.preview);
      }
    } catch (e) { if (revision === ctx.source.revision()) status(e.message, 'error'); }
    finally { locating = false; syncLocateButton(); }
  }

  async function startTracking(restart = false) {
    if (startingTracking || !ctx.source.hasStream()
      || !ctx.selection.roi() || !ctx.selection.marker()) return;
    startingTracking = true;
    const revision = ctx.source.revision();
    $('locate').disabled = true;
    try {
      if (!await ctx.pushConfig()) throw new Error('Nie udało się ustawić odczytu.');
      if (revision !== ctx.source.revision()) return;
      if (restart || !ctx.camera.session) {
        ctx.camera.setSession(null);
        const response = await api.capture();
        const answer = await response.json();
        if (revision !== ctx.source.revision()) return;
        if (!response.ok || !answer.session) throw new Error(answer.reason ?? 'Nie udało się uruchomić śledzenia.');
        ctx.camera.setSession(answer.session);
        clearReadout();
        ageAtReceipt = null;
      }
      $('live').checked = true;
      ctx.loop.start();
      status('Śledzenie XYZ włączone. Podczas pierwszego odczytu pozostań w miejscu.');
      ctx.camera.sendFrame(ctx.source.video, 0);
    } catch (e) { $('live').checked = false; status(e.message, 'error'); }
    finally { startingTracking = false; syncLocateButton(); }
  }

  function stopTracking() {
    ctx.source.bump();
    $('live').checked = false;
    ctx.camera.setSession(null);
    ctx.control.disarm();
    clearReadout();
    $('actual-hz').textContent = '0.0';
    ageAtReceipt = null;
  }

  // tickAge runs between snapshots so the age keeps counting up while nothing
  // arrives. A second of silence is what turns a stale XYZ into "unknown":
  // showing the last good reading forever would be a lie the user acts on.
  function tickAge() {
    if (ageAtReceipt == null) return;
    const age = ageAtReceipt + performance.now() - receivedAt;
    $('position-age').textContent = `${Math.round(age)} ms`;
    if (age > 1000) {
      $('coordinates').textContent = 'Pozycja nieznana';
      $('reference').hidden = true;
      $('actual-hz').textContent = '0.0';
    }
  }

  function mount() {
    $('locate').onclick = locateOnce;
    $('live').addEventListener('change', () => {
      if ($('live').checked) startTracking(); else stopTracking();
    });
  }

  function render(state) {
    receivedAt = performance.now();
    ageAtReceipt = state.position ? state.position_age_ms : null;
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

    if (state.position && state.preview_revision > 0 && state.preview_revision !== lastPreviewRev) {
      lastPreviewRev = state.preview_revision;
      showReference(`/api/preview?v=${state.preview_revision}`);
    } else if (!state.position) {
      $('reference').hidden = true;
      lastPreviewRev = -1;
    }
  }

  return {
    mount, render, locateOnce, startTracking, stopTracking,
    syncLocateButton, clearReadout, tickAge,
    config: () => ({
      zoom: num('zoom'),
      marker_x: ctx.selection.marker()?.x ?? 0,
      marker_y: ctx.selection.marker()?.y ?? 0,
      mask_radius: num('mask'),
      min_score: num('threshold'),
      min_gap: num('gap'),
      floor: num('floor'),
      adjacent_floors: $('floor-auto').checked,
      floor_radius: num('floor-radius'),
      speed: num('speed'),
    }),
  };
}
