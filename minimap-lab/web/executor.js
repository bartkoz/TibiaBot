// Named distinctly from follower.js's identical COMPASS table: both scripts
// now share one global scope on the panel page, where two top-level consts
// of the same name would be a SyntaxError.
const EXECUTOR_COMPASS = [['NW', 'N', 'NE'], ['W', '', 'E'], ['SW', 'S', 'SE']];
const stepDirection = (from, to) =>
  EXECUTOR_COMPASS[Math.sign(to.y - from.y) + 1][Math.sign(to.x - from.x) + 1];

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

// MIN_STILL_FRAMES matches the server's own gate. One stale reading is not
// proof the character stayed put; three consecutive ones are the cheapest
// evidence that rules out a single dropped frame.
const MIN_STILL_FRAMES = 3;

// Lock-step execution state, kept free of the DOM and of fetch so it can be
// tested directly. The panel feeds it follower output and confirmed positions;
// it answers with the one intent that may be sent right now, or null.
class StepExecutor {
  constructor(options = {}) {
    // 1800 ms, not 1200: step duration in Tibia scales with the character's
    // speed and the ground cost, and a step on mud or under paralysis runs
    // well past a second. A timeout shorter than the step itself would turn
    // ordinary slow movement into learned blockages.
    this.stepTimeoutMS = options.stepTimeoutMS ?? 1800;
    // How long after a timeout an arrival still counts as "that was lag, not
    // a wall" and revokes what the failure taught.
    // Generous on purpose. Under heavy paralysis or on slow ground a step can
    // run several seconds; revoking a lesson that turned out wrong costs
    // nothing, while keeping a false block costs a permanently avoided tile.
    this.lateArrivalMS = options.lateArrivalMS ?? 2000;
    // A blocked target is a suspicion with a shelf life, not a verdict. It
    // matches the server's temporary-block TTL.
    this.blockedTTL = options.blockedTTL ?? 60000;
    this.actionTimeoutMS = options.actionTimeoutMS ?? 5000;
    this.maxFailedCycles = options.maxFailedCycles ?? 3;
    // Ids are never reused, including across reset(), so a late emitted()
    // for a step from before a reset can never be mistaken for a new one.
    this.nextId = 1;
    this.reset();
  }
  reset() {
    this.pending = null; // {kind, target, x, y, z, from, viaHotkey, emittedAt, sentAt, id}
    this.last = null;
    this.retries = 0;
    this.cycles = 0;
    this.blocked = false;
    this.blockedTarget = null;
    // The target the current retries count is charging failures against.
    this.currentTarget = null;
    this.halted = false;
    this.stopped = false;
    this.actionDone = false;
    // What the last failed step taught about the map, waiting to be shipped.
    this.observation = null;
    // The target of the most recent failure, kept just long enough to notice a
    // late arrival on it.
    this.recentFailure = null;
    // When blocked was set, so it can lapse instead of lasting all session.
    this.blockedAt = null;
    // Whether any step has succeeded since the last failed one. The server
    // needs it to tell "the bot went around and came back" from "the bot has
    // been standing in front of the same player for a minute" - only the first
    // is evidence of terrain.
    this.movedSinceFailure = false;
  }
  state() {
    return {
      waiting: !!this.pending, retries: this.retries, cycles: this.cycles,
      blocked: this.blocked, halted: this.halted, stopped: this.stopped,
      actionDone: this.actionDone,
      // True while a step is pending but emitted() has not yet confirmed the
      // key left the driver - distinct from waiting on the character to move.
      awaitingEmit: !!this.pending && this.pending.emittedAt === null,
      // Id of the pending step, for the caller to pass back into emitted().
      stepId: this.pending ? this.pending.id : null,
    };
  }
  // startTarget marks which tile the current retries count applies to. A
  // target that differs from whatever it was tracking - the route was
  // recomputed - gets a fresh retry allowance instead of inheriting a
  // failure count that was never charged against it.
  startTarget(target) {
    if (!sameTarget(target, this.currentTarget)) this.retries = 0;
    this.currentTarget = target;
  }
  // Consequence of a pending step that did not pan out, whether it timed out
  // waiting for movement or was never confirmed as emitted at all: charge a
  // cycle, stop the executor if that was one too many, otherwise allow one
  // retry or, on the second failure of the same target, block it until the
  // route changes. blocked never means "stop forever" - only cycles does.
  failPending(p, now) {
    // Only a step confirmed as emitted and then watched standing still is
    // evidence about the map. A key that never left the driver, a lost
    // position or a floor change say nothing about the tile.
    // The emittedAt check is defence in depth: observe() already refuses to
    // count frames before the emission, so stillFrames cannot reach the
    // threshold without it - but the two conditions are one rule and belong
    // together where the rule is applied.
    if (p.kind === 'walk' && p.emittedAt !== null && p.from && p.stillFrames >= MIN_STILL_FRAMES) {
      this.observation = {
        from: {x: p.from.x, y: p.from.y, z: p.from.z},
        to: {x: p.target[0], y: p.target[1], z: p.from.z},
        outcome: 'no_motion',
        still_frames: p.stillFrames,
        last_frame_age_ms: Math.round(now - p.lastFrameAt),
        moved_since: this.movedSinceFailure,
      };
      this.movedSinceFailure = false;
    }
    if (p.kind === 'walk' && p.from) {
      this.recentFailure = {
        from: {x: p.from.x, y: p.from.y, z: p.from.z},
        to: {x: p.target[0], y: p.target[1], z: p.from.z},
        at: now,
      };
    }
    this.pending = null;
    this.cycles++;
    if (this.cycles >= this.maxFailedCycles) { this.stopped = true; return; }
    if (this.retries >= 1) {
      this.blocked = true;
      this.blockedTarget = targetOf(p);
      this.blockedAt = now;
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
        this.failPending(p, now);
      } else {
        const limit = p.kind === 'transition' ? this.actionTimeoutMS : this.stepTimeoutMS;
        if (now - p.emittedAt < limit) return null;
        this.failPending(p, now);
      }
      if (this.stopped) return null;
    }
    if (this.blocked) {
      // blocked means "this particular target cannot be reached", not "stop
      // forever": once the follower asks for something else, the route was
      // recomputed and the new target deserves a fresh attempt.
      //
      // It also lapses on its own. The server penalises a temporary block
      // rather than walling it off, so A* may keep routing through the very
      // same tile - the target never changes, and without a deadline the
      // executor would refuse it for the rest of the session even after the
      // obstacle walked away.
      if (this.blockedAt !== null && now - this.blockedAt >= this.blockedTTL) {
        // A lapsed block starts a fresh attempt, not the continuation of the
        // failed series that produced it. Carrying the cycle count over would
        // stop the bot on its very next try - the one that would have taught
        // the permanent block.
        this.clearBlocked();
        this.cycles = 0;
      } else {
        if (!out) return null;
        if (sameTarget(targetFromOut(out), this.blockedTarget)) return null;
        this.clearBlocked();
      }
    }
    if (!out) return null;
    if (out.action === 'walk') {
      // from is where the character stood when the key was sent. Taking it
      // from the first observation after the press would make "did not move"
      // and "moved somewhere unexpected" indistinguishable.
      this.startTarget(targetFromOut(out));
      this.pending = {kind: 'walk', target: out.next, from: this.last,
        stillFrames: 0, lastFrameAt: null,
        emittedAt: null, sentAt: now, id: this.nextId++};
      return {action: 'walk', direction: out.direction};
    }
    if (out.action === 'transition') {
      const wp = out.waypoint;
      // Stairs carry no item: the tile is on the current floor and stepping
      // onto it moves the character. The next waypoint says which way that is.
      if (wp.type === 'stairs') {
        if (!out.next || !this.last) return null;
        const direction = stepDirection(this.last, out.next);
        // A stairs waypoint and its landing point recorded at the same x,y is
        // normal (e.g. straight-up stairs). stepDirection then has nothing to
        // point at ('' - the driver's "nieznany kierunek"), so send nothing
        // and leave it to the human, exactly like an unknown position does.
        if (!direction) return null;
        this.startTarget(targetFromOut(out));
        this.pending = {kind: 'transition', x: wp.x, y: wp.y, z: wp.z, viaHotkey: false, from: this.last, emittedAt: null, sentAt: now, id: this.nextId++};
        return {action: 'walk', direction};
      }
      this.actionDone = false;
      this.startTarget(targetFromOut(out));
      this.pending = {kind: 'transition', x: wp.x, y: wp.y, z: wp.z, viaHotkey: true, from: this.last, emittedAt: null, sentAt: now, id: this.nextId++};
      return {action: 'transition', type: wp.type, waypoint: out.index ?? 0};
    }
    return null;
  }
  // clearActionDone is called once the panel has reported a completed floor
  // action to the driver (POST /api/input/done). Without this, actionDone
  // stays true until the next transition intent is created, which may never
  // happen again once the route moves past its last floor action - leaving
  // the panel to repost the same confirmation on every following tick.
  clearActionDone() {
    this.actionDone = false;
  }
  // dropPending abandons the current attempt without charging a failure: a
  // refusal (rate limit, unknown hotkey, wrong session...) means the key was
  // never sent, so nothing was learned about whether the target is reachable.
  // Escalation counters (retries, cycles, blocked) are untouched; only the
  // stale attempt is cleared so the next reading gets a fresh intent instead
  // of waiting out the full emission grace period for no reason.
  dropPending() {
    this.pending = null;
  }
  // emitted records when the key actually left the driver, correlated by id
  // so a late confirmation for a step already dropped (see the emission
  // grace period in intentFor) cannot be mistaken for proof about whatever
  // pending replaced it. id is required: the only caller is the panel driver,
  // written to always pass the id it captured from state() right after
  // intentFor returned the intent.
  emitted(now, id) {
    if (this.pending && this.pending.id === id) {
      this.pending.emittedAt = now;
    }
  }
  observe(position, capturedAt, now) {
    if (!position) { this.halted = true; this.pending = null; return; }
    this.halted = false;
    // Kept for stairs, whose direction comes from the current tile rather than
    // from a path.
    this.last = {...position};
    this.noteLateArrival(position, capturedAt, now);
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
    // A step that changed the floor is not a failed step: walking onto stairs
    // does exactly this. Judging it as one would teach the bot that stairs are
    // a wall.
    if (p.from && position.z !== p.from.z) { this.pending = null; return; }
    if (position.x === p.target[0] && position.y === p.target[1]) { this.done(); return; }
    // Standing still is a failed step and belongs to the retry counter, which
    // intentFor bumps after the timeout. Standing somewhere else entirely is a
    // changed situation: drop the step and let the follower replan.
    // With no reference tile the step cannot be judged, so it is left to the
    // timeout rather than guessed at.
    const stillThere = !p.from || (position.x === p.from.x && position.y === p.from.y);
    if (!stillThere) { this.pending = null; return; }
    p.stillFrames++;
    p.lastFrameAt = capturedAt;
  }

  // takeObservation hands the pending observation to the caller and forgets
  // it, so one failed step is reported exactly once no matter how many times
  // the panel ticks before the report goes out.
  takeObservation() {
    const obs = this.observation;
    this.observation = null;
    return obs;
  }
  // noteLateArrival catches the character reaching a tile shortly after the
  // step to it was written off. That was lag or paralysis, not an obstacle, so
  // whatever the failure taught is revoked and the target gets another chance.
  noteLateArrival(position, capturedAt, now) {
    const late = this.recentFailure;
    // Judged by when the frame was captured, not when it was processed: a slow
    // match must not turn a genuine arrival into a missed deadline.
    if (!late || capturedAt - late.at > this.lateArrivalMS) return;
    if (position.x !== late.to.x || position.y !== late.to.y || position.z !== late.to.z) return;
    // from is the tile the failed step started on, so the server can also drop
    // the edge a failed diagonal blocked - to alone does not identify it.
    this.observation = {from: {...late.from}, to: {...late.to}, outcome: 'entered',
      still_frames: 1, last_frame_age_ms: Math.round(now - capturedAt)};
    this.recentFailure = null;
    // The step worked after all, so it is progress, not a failure: the whole
    // escalation it caused is rolled back. Without clearing cycles, three
    // unrelated lag spikes in a session would add up to a permanent stop.
    this.retries = 0;
    this.cycles = 0;
    this.movedSinceFailure = true;
    if (this.blocked && this.blockedTarget &&
        this.blockedTarget.x === late.to.x && this.blockedTarget.y === late.to.y) {
      this.clearBlocked();
    }
  }
  clearBlocked() {
    this.blocked = false;
    this.blockedTarget = null;
    this.blockedAt = null;
  }
  done() {
    this.pending = null;
    this.movedSinceFailure = true;
    this.retries = 0;
    this.cycles = 0;
    this.recentFailure = null;
    this.clearBlocked();
  }
}

globalThis.StepExecutor = StepExecutor;
if (typeof module !== 'undefined') module.exports = {StepExecutor};
