// Panel-side client of the Go input driver. Owns the session token, the
// monotonically increasing sequence number and the heartbeat; contains no
// route logic and no decisions about when to send - that lives in the
// executor.
class InputClient {
  constructor(options = {}) {
    this.fetch = options.fetch ?? ((...a) => globalThis.fetch(...a));
    this.beatMS = options.beatMS ?? 200;
    this.onState = options.onState ?? (() => {});
    // How many consecutive failed status polls are tolerated before the
    // heartbeat gives up on a server it must assume is unreachable.
    this.maxHeartbeatFailures = options.maxHeartbeatFailures ?? 5;
    this.session = null;
    this.armed = false;
    this.seq = 0;
    this.timer = null;
    // Bumped by startHeartbeat()/stopHeartbeat(). A tick captures the current
    // value when it begins and refuses to touch state or reschedule if the
    // generation moved on while it was awaiting its fetch - otherwise a tick
    // still in flight when the loop is stopped (or restarted by a later
    // arm()) could resurrect itself into a second, orphaned polling chain.
    this.heartbeatGen = 0;
    this.heartbeatFailures = 0;
  }
  async post(path, body) {
    return this.fetch(path, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(body),
    });
  }
  // A rejected fetch must still resolve to a result object, never propagate
  // as an exception: an escaped rejection would leave a caller unable to
  // tell "confirmed failed" from "never answered", and (for send()) the
  // executor waiting forever on a key it can never account for.
  async postSafe(path, body) {
    try {
      const r = await this.post(path, body);
      const state = await r.json();
      return {ok: r.ok, state};
    } catch (e) {
      return {ok: false, state: {status: 'error', reason: e.message}};
    }
  }
  async arm() {
    const {ok, state} = await this.postSafe('/api/arm', {});
    if (!ok || !state.armed) { this.armed = false; this.onState(state); return state; }
    this.session = state.session;
    this.armed = true;
    this.startHeartbeat();
    this.onState(state);
    return state;
  }
  async disarm() {
    this.stopHeartbeat();
    const session = this.session;
    // The local state must reflect the intent to stop even if the request
    // never arrives: this is the one path that exists to stop everything, so
    // a failing POST must not leave the panel believing it is still armed
    // with no heartbeat running.
    this.armed = false;
    if (!session) return;
    await this.postSafe('/api/disarm', {session});
  }
  async send(intent, ageMS) {
    if (!this.armed || !this.session) return {status: 'disarmed'};
    this.seq++;
    const {state: result} = await this.postSafe('/api/input', {
      ...intent, session: this.session, seq: this.seq,
      observation_age_ms: Math.round(ageMS),
    });
    if (result.status === 'disarmed') { this.armed = false; this.stopHeartbeat(); }
    this.onState(result);
    return result;
  }
  async actionDone() {
    if (!this.session) return;
    await this.postSafe('/api/input/done', {session: this.session});
  }
  async calibrate(nx, ny) {
    const {ok} = await this.postSafe('/api/input/calibrate', {session: this.session, x: nx, y: ny});
    return ok;
  }
  // The status poll is also the heartbeat: a gap longer than the driver's
  // timeout disarms it on the Go side. A single failed poll must cost one
  // beat, not the session, so a transient network blip does not end the
  // loop - but failures that keep piling up mean the server is genuinely
  // unreachable and has certainly disarmed by now.
  startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatFailures = 0;
    const gen = ++this.heartbeatGen;
    const beat = async () => {
      if (gen !== this.heartbeatGen) return; // superseded while scheduled
      if (!this.armed || !this.session) return;
      try {
        // The token travels in a header, never in the URL: query strings
        // land in logs, history and Referer.
        const r = await this.fetch('/api/input/status', {headers: {'X-Input-Session': this.session}});
        const state = await r.json();
        // Stopped or restarted while this tick was awaiting its fetch: it
        // must not touch state or reschedule, or it becomes a second,
        // orphaned polling chain running alongside whatever replaced it.
        if (gen !== this.heartbeatGen) return;
        this.heartbeatFailures = 0;
        if (!state.armed) { this.armed = false; this.onState(state); this.stopHeartbeat(); return; }
        this.onState(state);
        this.timer = setTimeout(beat, this.beatMS);
      } catch (e) {
        if (gen !== this.heartbeatGen) return;
        this.heartbeatFailures++;
        this.onState({status: 'error', reason: e.message});
        if (this.heartbeatFailures >= this.maxHeartbeatFailures) {
          // The server has had its chances; assume it disarmed long ago.
          this.armed = false;
          this.stopHeartbeat();
          return;
        }
        this.timer = setTimeout(beat, this.beatMS);
      }
    };
    this.timer = setTimeout(beat, this.beatMS);
  }
  stopHeartbeat() {
    this.heartbeatGen++;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
  }
}

globalThis.InputClient = InputClient;
if (typeof module !== 'undefined') module.exports = {InputClient};
