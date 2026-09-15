// Every URL the panel knows, in one place. The calls hand back the raw
// response rather than a parsed result: the call sites differ in what they
// need from it - JSON, text, a header, an array buffer - and in what a failure
// there means, and flattening that here would only hide it.

export function createApi(env) {
  const fetch = (...a) => env.fetch(...a);
  const json = (method, url, body) => fetch(url, {
    method, headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body),
  });

  return {
    info: () => fetch('/api/info'),
    state: () => fetch('/api/state'),
    locate: body => fetch('/api/locate', {method: 'POST', body}),
    capture: () => fetch('/api/capture', {method: 'POST'}),
    arm: () => fetch('/api/arm', {method: 'POST'}),
    disarm: () => fetch('/api/disarm', {method: 'POST'}),
    config: body => json('PUT', '/api/config', body),
    getRoute: () => fetch('/api/route'),
    // The route goes up as the caller's own text: a file picked from disk is
    // forwarded byte for byte so the server's validator sees what the user
    // actually has, not a re-serialised copy of it.
    putRoute: body => fetch('/api/route', {method: 'PUT', body}),
    addWaypoint: () => fetch('/api/route/waypoint', {method: 'POST'}),
    vision: () => fetch('/api/vision'),
    grid: (x, y, z, r) => fetch(`/api/grid?x=${x}&y=${y}&z=${z}&r=${r}`),
    deleteBlock: p => json('DELETE', '/api/blocks', p),
    frame: (...a) => fetch(...a),
  };
}
