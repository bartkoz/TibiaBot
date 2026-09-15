// The tab strip. One panel visible at a time, and this module is the only
// place that knows which: the previews ask it whether they are on screen
// rather than guessing from the DOM, so a hidden canvas stops costing a
// request every frame.

const TABS = [
  {id: 'pozycja', label: 'Pozycja'},
  {id: 'trasa', label: 'Trasa'},
  {id: 'walka', label: 'Walka'},
  {id: 'leczenie', label: 'Leczenie'},
  {id: 'sterowanie', label: 'Sterowanie'},
  {id: 'diagnostyka', label: 'Diagnostyka'},
];

export const TAB_IDS = TABS.map(t => t.id);

export function createTabs(ctx) {
  const {$} = ctx.dom;

  let active = TABS[0].id;

  const tab = id => $(`tab-${id}`);
  const pane = id => $(`panel-${id}`);

  // Panels are hidden with the attribute rather than a class: the stylesheet
  // already makes [hidden] stick, and a canvas keeps its bitmap while hidden,
  // so switching away and back never loses what was drawn.
  function show(id, {remember = true} = {}) {
    if (!TAB_IDS.includes(id)) return;
    active = id;
    for (const t of TABS) {
      const on = t.id === id;
      tab(t.id).setAttribute('aria-selected', String(on));
      tab(t.id).tabIndex = on ? 0 : -1;
      pane(t.id).hidden = !on;
    }
    if (remember) ctx.form.save();
  }

  function setBadge(id, badge) {
    const el = $(`badge-${id}`);
    if (!badge) {
      el.hidden = true;
      el.textContent = '';
      el.className = 'badge';
      return;
    }
    el.hidden = false;
    el.textContent = badge.text;
    el.className = `badge badge-${badge.kind}`;
  }

  function mount() {
    TABS.forEach((t, i) => {
      const el = tab(t.id);
      el.addEventListener('click', () => show(t.id));
      el.addEventListener('keydown', e => {
        const delta = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
        if (!delta) return;
        e.preventDefault?.();
        const next = TABS[(i + delta + TABS.length) % TABS.length];
        show(next.id);
        tab(next.id).focus?.();
      });
      setBadge(t.id, null);
    });
    show(active, {remember: false});
  }

  return {
    mount, show, setBadge,
    active: () => active,
    visible: id => active === id,
  };
}
