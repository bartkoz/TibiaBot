// Healing rules. Go decides which rule fires; the panel owns the list, its
// order and the one line that says whether anything is happening.

const HEAL_DEFAULT = {enabled: true, resource: 'hp', below_pct: 60, hotkey: 'f1',
  cooldown_ms: 1000, min_mana_pct: 0};

const MAX_RULES = 8;

export function createHeal(ctx) {
  const {$, document, field, button} = ctx.dom;
  const {status} = ctx;

  // rules is the panel's copy of the list. The rows are rebuilt from it on
  // every change rather than read back out of the DOM: the array is the truth
  // that gets sent and remembered, and rebuilding keeps row ids in step with
  // the order after a move.
  let rules = [];

  function changed() {
    ctx.form.save();
    ctx.pushConfig();
  }

  function move(from, delta) {
    const to = from + delta;
    if (to < 0 || to >= rules.length) return;
    [rules[from], rules[to]] = [rules[to], rules[from]];
    renderRules();
    changed();
  }

  function renderRules() {
    const host = $('heal-rules');
    const rows = rules.map((rule, i) => {
      const row = document.createElement('div');
      row.className = 'route-grid';

      const on = field(row, `heal-${i}-enabled`, 'Włączona', rule.enabled, 'checkbox');
      on.onclick = () => { rules[i].enabled = on.checked; changed(); };

      const resource = document.createElement('select');
      resource.id = `heal-${i}-resource`;
      for (const [value, text] of [['hp', 'HP'], ['mana', 'mana']]) {
        const option = document.createElement('option');
        option.value = value; option.textContent = text;
        resource.append(option);
      }
      resource.value = rule.resource;
      resource.addEventListener('input', () => {
        rules[i].resource = resource.value; changed();
      });
      const resourceLabel = document.createElement('label');
      resourceLabel.textContent = 'Zasób';
      resourceLabel.append(resource);
      row.append(resourceLabel);

      const below = field(row, `heal-${i}-below`, 'Próg %', rule.below_pct, 'number',
        {min: 1, max: 99, step: 1});
      below.addEventListener('input', () => {
        rules[i].below_pct = Number(below.value); changed();
      });

      const hotkey = field(row, `heal-${i}-hotkey`, 'Klawisz', rule.hotkey, 'text');
      hotkey.addEventListener('input', () => {
        rules[i].hotkey = hotkey.value.trim().toLowerCase(); changed();
      });

      const cooldown = field(row, `heal-${i}-cooldown`, 'Cooldown (ms)', rule.cooldown_ms,
        'number', {min: 100, max: 60000, step: 50});
      cooldown.addEventListener('input', () => {
        rules[i].cooldown_ms = Number(cooldown.value); changed();
      });

      const mana = field(row, `heal-${i}-mana`, 'Min. mana %', rule.min_mana_pct,
        'number', {min: 0, max: 99, step: 1});
      mana.addEventListener('input', () => {
        rules[i].min_mana_pct = Number(mana.value); changed();
      });

      button(row, `heal-${i}-up`, '▲').onclick = () => move(i, -1);
      button(row, `heal-${i}-down`, '▼').onclick = () => move(i, 1);
      button(row, `heal-${i}-del`, 'Usuń').onclick = () => {
        rules.splice(i, 1); renderRules(); changed();
      };
      return row;
    });
    host.replaceChildren(...rows);
  }

  function mount() {
    $('heal-add').onclick = () => {
      if (rules.length >= MAX_RULES) {
        status('Reguł leczenia może być najwyżej osiem.', 'error');
        return;
      }
      rules.push({...HEAL_DEFAULT});
      renderRules();
      changed();
    };
    $('heal-on').onclick = changed;
  }

  // render is the one line the user reads to know whether healing is working:
  // what fired last and how long ago, or why nothing did.
  function render(state) {
    const heal = state.heal;
    ctx.tabs.setBadge('leczenie',
      heal?.enabled ? {kind: 'on', text: '●', label: 'włączone'} : null);
    if (!heal?.enabled) { $('heal-status').textContent = 'Leczenie wyłączone.'; return; }
    const parts = [];
    if (heal.last_hotkey && heal.last_age_ms != null) {
      parts.push(`ostatnie: ${heal.last_hotkey}, ${(heal.last_age_ms / 1000).toFixed(1).replace('.', ',')} s temu`);
    } else {
      parts.push('nic jeszcze nie poleciało');
    }
    if (heal.reason) parts.push(heal.reason);
    $('heal-status').textContent = parts.join(' · ');
  }

  return {
    mount, render,
    config: () => ({heal: {enabled: $('heal-on').checked, rules: rules.map(r => ({...r}))}}),
    // A copy, not the array: the list is this module's own state and the
    // only writer of it is this module.
    rules: () => rules.map(r => ({...r})),
    setRules: saved => {
      rules = saved.map(r => ({...HEAL_DEFAULT, ...r}));
      renderRules();
    },
    enabled: () => $('heal-on').checked,
  };
}
