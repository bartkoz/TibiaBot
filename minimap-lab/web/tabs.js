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

const TAB_IDS = TABS.map(t => t.id);

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
    // Clicking the tab you are already on is not a change: without this it
    // would rewrite the whole stored form and fire another preview request.
    if (id === active && remember) return;
    active = id;
    for (const t of TABS) {
      const on = t.id === id;
      tab(t.id).setAttribute('aria-selected', String(on));
      tab(t.id).tabIndex = on ? 0 : -1;
      pane(t.id).hidden = !on;
    }
    if (remember) ctx.form.save();
    ctx.reveal();
  }

  // The glyph is hidden from assistive technology and its meaning is put on
  // the tab itself: a screen reader announcing "Sterowanie czarne koło" is
  // worse than no badge at all.
  function setBadge(id, badge) {
    const el = $(`badge-${id}`);
    const name = TABS.find(t => t.id === id).label;
    tab(id).setAttribute('aria-label', badge ? `${name} — ${badge.label}` : name);
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
        const next = e.key === 'ArrowRight' ? TABS[(i + 1) % TABS.length]
          : e.key === 'ArrowLeft' ? TABS[(i - 1 + TABS.length) % TABS.length]
            : e.key === 'Home' ? TABS[0]
              : e.key === 'End' ? TABS[TABS.length - 1]
                : null;
        if (!next) return;
        e.preventDefault?.();
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
    knows: id => TAB_IDS.includes(id),
  };
}
