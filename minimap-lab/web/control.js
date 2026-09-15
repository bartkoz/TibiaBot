// Arming the executor and the keys it presses. Nothing here decides to walk -
// that is Go's - but nothing walks until this module has handed over a window
// to type into and a name for every direction.

export const DIRECTIONS = {NW: 'dir-nw', N: 'dir-n', NE: 'dir-ne', W: 'dir-w', E: 'dir-e',
  SW: 'dir-sw', S: 'dir-s', SE: 'dir-se'};
export const HOTKEYS = {rope: 'hotkey-rope', ladder: 'hotkey-ladder', hole: 'hotkey-hole',
  shovel: 'hotkey-shovel'};

const NUMPAD = {NW: 'numpad7', N: 'numpad8', NE: 'numpad9', W: 'numpad4', E: 'numpad6',
  SW: 'numpad1', S: 'numpad2', SE: 'numpad3'};
const WSAD = {NW: 'q', N: 'w', NE: 'e', W: 'a', E: 'd', SW: 'z', S: 's', SE: 'c'};

// ARM_DELAY_MS exists because the browser has focus at the moment its own
// button is clicked, and the driver memorises whatever window is focused when
// the request arrives. Without the wait it would always memorise the panel,
// and the first key would disarm on "okno gry straciło focus". The countdown
// is the window in which the user switches to the game.
const ARM_DELAY_MS = 5000;

export function createControl(ctx) {
  const {$} = ctx.dom;
  const {api, status} = ctx;
  const setTimeout = (...a) => ctx.env.setTimeout(...a);
  const clearTimeout = (...a) => ctx.env.clearTimeout(...a);

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
      const r = await api.arm();
      const answer = await r.json();
      if (!r.ok || !answer.armed) {
        status(answer.reason ?? 'Nie udało się uzbroić.', 'error');
        return;
      }
      ctx.camera.setSession(answer.session);
      await ctx.pushConfig();
      status('Uzbrojono. Wykonawca działa, dopóki okno gry ma focus.', 'ok');
    } catch (e) { status(e.message, 'error'); }
  }

  async function disarm() {
    ctx.camera.setSession(null);
    try { await api.disarm(); } catch { /* nic tu nie pomoże */ }
  }

  function applyPreset(preset) {
    for (const [dir, id] of Object.entries(DIRECTIONS)) $(id).value = preset[dir] ?? '';
    ctx.form.save();
    ctx.pushConfig();
  }

  function mount() {
    $('input-arm').onclick = beginArm;
    $('input-disarm').onclick = () => { cancelArm(); disarm(); status('Rozbrojono.'); };
    $('input-calibrate').onclick = () => {
      ctx.selection.startCalibration();
      status('Kliknij na obrazie kratkę, na której stoi postać.');
    };
    $('dir-preset-numpad').onclick = () => applyPreset(NUMPAD);
    $('dir-preset-wsad').onclick = () => applyPreset(WSAD);
  }

  function render(state) {
    $('input-status').textContent = !ctx.controlAvailable()
      ? 'Sterowanie wyłączone. Śledzenie XYZ działa niezależnie.'
      : state.armed
        ? 'Uzbrojony. Alt-tab albo cisza kamery rozbraja.'
        : 'Rozbrojony.';
    ctx.tabs.setBadge('sterowanie', state.armed ? {kind: 'on', text: '●'} : null);
    $('input-arm').disabled = !ctx.controlAvailable() || (state.armed && !armTimer);
    $('input-disarm').disabled = !state.armed;
    $('input-calibrate').disabled = !ctx.source.isReady();
    if (state.executor?.stopped) {
      status('Wykonawca zatrzymany po serii nieudanych kroków.', 'error');
    }
  }

  return {
    mount, render, disarm, cancelArm,
    config: () => ({walk: $('input-walk').checked, floor_actions: $('input-actions').checked}),
    // The keys ride outside `brain`, next to it rather than in it: they belong
    // to the driver, not to the decision loop.
    inputConfig: () => ({
      keys: Object.fromEntries(Object.entries(HOTKEYS)
        .map(([kind, id]) => [kind, $(id).value.trim()]).filter(([, key]) => key)),
      click_after_hotkey: !$('input-own-tile').checked,
      directions: Object.fromEntries(Object.entries(DIRECTIONS)
        .map(([dir, id]) => [dir, $(id).value.trim()]).filter(([, key]) => key)),
    }),
  };
}
