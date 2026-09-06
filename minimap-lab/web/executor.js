const COMPASS = [['NW', 'N', 'NE'], ['W', '', 'E'], ['SW', 'S', 'SE']];
const stepDirection = (from, to) =>
  COMPASS[Math.sign(to.y - from.y) + 1][Math.sign(to.x - from.x) + 1];

// The tile identity a pending step (or an incoming follower output) is aimed
// at, so a block can be compared against "the same target" vs "a different
// one" regardless of whether it came from a walk or a transition.
const targetOf = (p) => p.kind === 'walk'
  ? {x: p.target[0], y: p.target[1], z: null}
  : {x: p.x, y: p.y, z: p.z};
const targetFromOut = (out) => {
  if (!out) return null;
  if (out.action === 'walk') return {x: out.next[0], y: out.next[1], z: null};
  if (out.action === 'transition') return {x: out.waypoint.x, y: out.waypoint.y, z: out.waypoint.z};
  return null;
};
const sameTarget = (a, b) => !!a && !!b && a.x === b.x && a.y === b.y && a.z === b.z;

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
    this.pending = null; // {kind, target, x, y, z, from, viaHotkey, emittedAt, sentAt}
    this.last = null;
    this.retries = 0;
    this.cycles = 0;
    this.blocked = false;
    this.blockedTarget = null;
    this.halted = false;
    this.stopped = false;
    this.actionDone = false;
  }
  state() {
    return {
      waiting: !!this.pending, retries: this.retries, cycles: this.cycles,
      blocked: this.blocked, halted: this.halted, stopped: this.stopped,
      actionDone: this.actionDone,
      // True while a step is pending but emitted() has not yet confirmed the
      // key left the driver - distinct from waiting on the character to move.
      awaitingEmit: !!this.pending && this.pending.emittedAt === null,
    };
  }
  // Consequence of a pending step that did not pan out, whether it timed out
  // waiting for movement or was never confirmed as emitted at all: charge a
  // cycle, stop the executor if that was one too many, otherwise allow one
  // retry or, on the second failure of the same target, block it until the
  // route changes. blocked never means "stop forever" - only cycles does.
  failPending(p) {
    this.pending = null;
    this.cycles++;
    if (this.cycles >= this.maxFailedCycles) { this.stopped = true; return; }
    if (this.retries >= 1) {
      this.blocked = true;
      this.blockedTarget = targetOf(p);
      this.retries = 0;
      return;
    }
    this.retries++;
  }
  // intentFor returns what to send now, or null when the executor must wait.
  intentFor(out, now) {
    if (this.stopped || this.halted) return null;
    if (this.pending) {
      const p = this.pending;
      if (p.emittedAt === null) {
        // The key press was never confirmed (e.g. a rejected fetch). Waiting
        // forever would freeze the executor silently, looking exactly like a
        // legitimate wait, so give up on it too after a generous grace period.
        if (now - p.sentAt < 2 * this.stepTimeoutMS) return null;
        this.failPending(p);
      } else {
        const limit = p.kind === 'transition' ? this.actionTimeoutMS : this.stepTimeoutMS;
        if (now - p.emittedAt < limit) return null;
        this.failPending(p);
      }
      if (this.stopped) return null;
    }
    if (this.blocked) {
      // blocked means "this particular target cannot be reached", not "stop
      // forever": once the follower asks for something else, the route was
      // recomputed and the new target deserves a fresh attempt.
      if (!out) return null;
      if (sameTarget(targetFromOut(out), this.blockedTarget)) return null;
      this.blocked = false;
      this.blockedTarget = null;
    }
    if (!out) return null;
    if (out.action === 'walk') {
      // from is where the character stood when the key was sent. Taking it
      // from the first observation after the press would make "did not move"
      // and "moved somewhere unexpected" indistinguishable.
      this.pending = {kind: 'walk', target: out.next, from: this.last, emittedAt: null, sentAt: now};
      return {action: 'walk', direction: out.direction};
    }
    if (out.action === 'transition') {
      const wp = out.waypoint;
      // Stairs carry no item: the tile is on the current floor and stepping
      // onto it moves the character. The next waypoint says which way that is.
      if (wp.type === 'stairs') {
        if (!out.next || !this.last) return null;
        this.pending = {kind: 'transition', x: wp.x, y: wp.y, z: wp.z, viaHotkey: false, from: this.last, emittedAt: null, sentAt: now};
        return {action: 'walk', direction: stepDirection(this.last, out.next)};
      }
      this.actionDone = false;
      this.pending = {kind: 'transition', x: wp.x, y: wp.y, z: wp.z, viaHotkey: true, from: this.last, emittedAt: null, sentAt: now};
      return {action: 'transition', type: wp.type, waypoint: out.index ?? 0};
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
        return;
      }
      // Same floor: no proof yet. If the character is no longer where it
      // stood when the key was sent, the situation changed - pushed by a
      // creature, or the player took over - so drop the step without
      // charging retries/cycles, exactly like an unexpected walk destination.
      const stillThere = !p.from || (position.x === p.from.x && position.y === p.from.y);
      if (!stillThere) { this.pending = null; }
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
    this.blockedTarget = null;
  }
}

globalThis.StepExecutor = StepExecutor;
if (typeof module !== 'undefined') module.exports = {StepExecutor};
