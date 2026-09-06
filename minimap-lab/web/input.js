// Panel-side client of the Go input driver. Owns the session token, the
// monotonically increasing sequence number and the heartbeat; contains no
// route logic and no decisions about when to send - that lives in the
// executor.
class InputClient {
  constructor(options = {}) {
    this.fetch = options.fetch ?? ((...a) => globalThis.fetch(...a));
    this.beatMS = options.beatMS ?? 200;
    this.onState = options.onState ?? (() => {});
    this.session = null;
    this.armed = false;
    this.seq = 0;
    this.timer = null;
  }
  async post(path, body) {
    return this.fetch(path, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(body),
    });
  }
  async arm() {
    const r = await this.post('/api/arm', {});
    const state = await r.json();
    if (!r.ok || !state.armed) { this.armed = false; this.onState(state); return state; }
    this.session = state.session;
    this.armed = true;
    this.startHeartbeat();
    this.onState(state);
    return state;
  }
  async disarm() {
    this.stopHeartbeat();
    if (!this.session) return;
    await this.post('/api/disarm', {session: this.session});
    this.armed = false;
  }
  async send(intent, ageMS) {
    if (!this.armed || !this.session) return {status: 'disarmed'};
    this.seq++;
    // A rejected fetch must still produce a result. If it escaped as a
    // rejection the panel would skip both confirming and resetting the step,
    // leaving the executor waiting forever on a key it can never account for.
    let result;
    try {
      const r = await this.post('/api/input', {
        ...intent, session: this.session, seq: this.seq,
        observation_age_ms: Math.round(ageMS),
      });
      result = await r.json();
    } catch (e) {
      result = {status: 'error', reason: e.message};
    }
    if (result.status === 'disarmed') { this.armed = false; this.stopHeartbeat(); }
    this.onState(result);
    return result;
  }
  async actionDone() {
    if (!this.session) return;
    await this.post('/api/input/done', {session: this.session});
  }
  async calibrate(nx, ny) {
    const r = await this.post('/api/input/calibrate', {session: this.session, x: nx, y: ny});
    return r.ok;
  }
  // The status poll is also the heartbeat: a gap longer than the driver's
  // timeout disarms it on the Go side, and a reply that already reports
  // disarmed (usually the game window lost focus) must stop this loop rather
  // than keep polling a session the server has already given up on.
  startHeartbeat() {
    this.stopHeartbeat();
    const beat = async () => {
      if (!this.armed || !this.session) return;
      // The token travels in a header, never in the URL: query strings land
      // in logs, history and Referer.
      const r = await this.fetch('/api/input/status', {headers: {'X-Input-Session': this.session}});
      const state = await r.json();
      if (!state.armed) { this.armed = false; this.stopHeartbeat(); }
      this.onState(state);
      if (this.armed) this.timer = setTimeout(beat, this.beatMS);
    };
    this.timer = setTimeout(beat, this.beatMS);
  }
  stopHeartbeat() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
  }
}

globalThis.InputClient = InputClient;
if (typeof module !== 'undefined') module.exports = {InputClient};
