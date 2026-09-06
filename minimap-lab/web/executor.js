const COMPASS = [['NW', 'N', 'NE'], ['W', '', 'E'], ['SW', 'S', 'SE']];
const stepDirection = (from, to) =>
  COMPASS[Math.sign(to.y - from.y) + 1][Math.sign(to.x - from.x) + 1];

// Lock-step execution state, kept free of the DOM and of fetch so it can be
// tested directly. The panel feeds it follower output and confirmed positions;
// it answers with the one intent that may be sent right now, or null.
class StepExecutor {
  constructor(options = {}) {
    this.stepTimeoutMS = options.stepTimeoutMS ?? 1200;
    this.actionTimeoutMS = options.actionTimeoutMS ?? 5000;
    this.maxFailedCycles = options.maxFailedCycles ?? 3;
    this.reset();
  }
  reset() {
    this.pending = null; // {kind, target, z, from, viaHotkey, emittedAt}
    this.last = null;
    this.retries = 0;
    this.cycles = 0;
    this.blocked = false;
    this.halted = false;
    this.stopped = false;
    this.actionDone = false;
  }
  state() {
    return {
      waiting: !!this.pending, retries: this.retries, cycles: this.cycles,
      blocked: this.blocked, halted: this.halted, stopped: this.stopped,
      actionDone: this.actionDone,
    };
  }
  // intentFor returns what to send now, or null when the executor must wait.
  intentFor(out, now) {
    if (this.stopped || this.halted) return null;
    if (this.pending) {
      const limit = this.pending.kind === 'transition' ? this.actionTimeoutMS : this.stepTimeoutMS;
      // A step whose emission was never confirmed is still in flight.
      if (this.pending.emittedAt === null) return null;
      if (now - this.pending.emittedAt < limit) return null;
      this.pending = null;
      this.cycles++;
      if (this.cycles >= this.maxFailedCycles) { this.stopped = true; return null; }
      if (this.retries >= 1) { this.blocked = true; this.retries = 0; return null; }
      this.retries++;
    }
    if (!out) return null;
    if (out.action === 'walk') {
      // from is where the character stood when the key was sent. Taking it
      // from the first observation after the press would make "did not move"
      // and "moved somewhere unexpected" indistinguishable.
      this.pending = {kind: 'walk', target: out.next, from: this.last, emittedAt: null};
      return {action: 'walk', direction: out.direction};
    }
    if (out.action === 'transition') {
      // Stairs carry no item: the tile is on the current floor and stepping
      // onto it moves the character. The next waypoint says which way that is.
      if (out.waypoint.type === 'stairs') {
        if (!out.next || !this.last) return null;
        this.pending = {kind: 'transition', z: out.waypoint.z, viaHotkey: false, emittedAt: null};
        return {action: 'walk', direction: stepDirection(this.last, out.next)};
      }
      this.actionDone = false;
      this.pending = {kind: 'transition', z: out.waypoint.z, viaHotkey: true, emittedAt: null};
      return {action: 'transition', type: out.waypoint.type, waypoint: out.index ?? 0};
    }
    return null;
  }
  // emitted records when the key actually left the driver. It is taken after
  // the reply arrives, so no frame captured before the press can be mistaken
  // for proof that the step happened.
  emitted(now) {
    if (this.pending) {
      this.pending.emittedAt = now;
    }
  }
  observe(position, capturedAt, now) {
    if (!position) { this.halted = true; this.pending = null; return; }
    this.halted = false;
    // Kept for stairs, whose direction comes from the current tile rather than
    // from a path.
    this.last = {...position};
    const p = this.pending;
    if (!p || p.emittedAt === null || capturedAt <= p.emittedAt) return;
    if (p.kind === 'transition') {
      // A floor change is the only proof, whether an item was used or the
      // character simply walked onto stairs.
      if (position.z !== p.z) {
        this.done();
        if (p.viaHotkey) this.actionDone = true;
      }
      return;
    }
    if (position.x === p.target[0] && position.y === p.target[1]) { this.done(); return; }
    // Standing still is a failed step and belongs to the retry counter, which
    // intentFor bumps after the timeout. Standing somewhere else entirely is a
    // changed situation: drop the step and let the follower replan.
    // With no reference tile the step cannot be judged, so it is left to the
    // timeout rather than guessed at.
    const stillThere = !p.from || (position.x === p.from.x && position.y === p.from.y);
    if (!stillThere) { this.pending = null; }
  }
  done() {
    this.pending = null;
    this.retries = 0;
    this.cycles = 0;
    this.blocked = false;
  }
}

globalThis.StepExecutor = StepExecutor;
if (typeof module !== 'undefined') module.exports = {StepExecutor};
