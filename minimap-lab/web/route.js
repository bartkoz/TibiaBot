// Waypoints live in the brain on the Go side. The panel loads them from a file,
// downloads them back, and is the one place where the recorder's guess about a
// transition type can be corrected.

export function createRoute(ctx) {
  const {$, num, document} = ctx.dom;
  const {api} = ctx;
  const {Blob, URL} = ctx.env;

  let cache = null, lastCount = -1;

  const routeStatus = text => { $('route-status').textContent = text; };

  async function refreshList() {
    try {
      const r = await api.getRoute();
      if (!r.ok) return;
      cache = await r.json();
      renderList();
    } catch { /* lista jest wygodą, nie warunkiem działania */ }
  }

  function renderList() {
    const points = cache?.waypoints ?? [];
    $('route-name').value ||= cache?.name ?? '';
    $('route-list').replaceChildren(...points.map((wp, index) => {
      const row = document.createElement('li');
      const label = document.createElement('span');
      label.className = 'where';
      label.textContent = `${wp.x}, ${wp.y}, ${wp.z}`;
      const select = document.createElement('select');
      for (const kind of ['walk', 'rope', 'ladder', 'stairs', 'hole', 'shovel']) {
        const option = document.createElement('option');
        option.value = kind; option.textContent = kind;
        select.append(option);
      }
      select.value = wp.type;
      select.addEventListener('change', async () => {
        cache.waypoints[index].type = select.value;
        cache.name = $('route-name').value.trim();
        const put = await api.putRoute(JSON.stringify(cache));
        routeStatus(put.ok ? `Waypoint ${index + 1} to teraz ${select.value}.`
          : ((await put.json()).reason ?? 'Nie udało się zapisać zmiany.'));
      });
      row.append(label, select);
      return row;
    }));
  }

  function mount() {
    $('route-file').addEventListener('change', async e => {
      const f = e.target.files[0]; if (!f) return;
      try {
        const r = await api.putRoute(await f.text());
        const answer = await r.json();
        if (!r.ok) { routeStatus(answer.reason ?? 'Nie udało się wczytać trasy.'); return; }
        routeStatus(`Wczytano ${answer.waypoints} waypointów z ${f.name}.`);
        refreshList();
      } catch (e) { routeStatus(e.message); }
      finally { $('route-file').value = ''; }
    });

    $('route-save').onclick = async () => {
      try {
        const r = await api.getRoute();
        if (!r.ok) { routeStatus('Nie udało się pobrać trasy.'); return; }
        const blob = new Blob([await r.text()], {type: 'application/json'});
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = ($('route-name').value.trim() || 'trasa') + '.json';
        a.click();
        URL.revokeObjectURL(a.href);
      } catch (e) { routeStatus(e.message); }
    };

    $('route-clear').onclick = async () => {
      await api.putRoute('{"version":1,"waypoints":[]}');
      routeStatus('Trasa wyczyszczona.');
      refreshList();
    };

    $('route-add').onclick = async () => {
      const r = await api.addWaypoint();
      if (!r.ok) { routeStatus((await r.json()).reason ?? 'Nie udało się dodać punktu.'); return; }
      routeStatus('Dodano waypoint na bieżącej kratce.');
      refreshList();
    };
  }

  function render(state) {
    $('route-add').disabled = !state.position;
    $('route-save').disabled = !state.route?.count;
    $('route-clear').disabled = !state.route?.count;

    const r = state.route ?? {};
    routeStatus(r.count
      ? `Waypoint ${Math.min(r.index + 1, r.count)} z ${r.count}${r.finished ? ' · ukończona' : ''}` +
        (state.recorder?.skipped ? ` · pominięto ${state.recorder.skipped} błędnych odczytów` : '') +
        (state.recorder?.waiting ? ' · czekam na dane przechodniości' : '')
      : 'Brak trasy.');
    $('route-next').textContent = r.next || '—';
    ctx.tabs.setBadge('trasa', r.count
      ? {kind: 'count', text: String(r.count), label: `${r.count} waypointów`}
      : null);
    // Refetched only when the count moves: the list is a thousand rows at worst
    // and has no business being rebuilt at frame rate.
    if (r.count !== lastCount) { lastCount = r.count; refreshList(); }
  }

  return {
    mount, render,
    config: () => ({
      follow: $('route-follow').checked,
      record_auto: $('route-record').checked,
      record_every: num('route-every'),
      tolerance: num('route-tolerance'),
      action_tolerance: 0,
      loop_route: $('route-loop').checked,
    }),
  };
}
