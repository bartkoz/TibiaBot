// Route following state, kept free of the DOM so it can be tested directly.
// The panel feeds it positions from the tracker and paths from /api/path; it
// answers with what the player should do next.
const TRANSITION_INSTRUCTIONS = {
  rope: 'Użyj liny',
  ladder: 'Wejdź po drabinie',
  stairs: 'Wejdź na schody',
  hole: 'Zejdź dziurą',
  shovel: 'Kop łopatą',
  walk: 'Przejdź na piętro',
};
const COMPASS = [['NW', 'N', 'NE'], ['W', '', 'E'], ['SW', 'S', 'SE']];

const chebyshev = (a, b) => Math.max(Math.abs(a.x - b.x), Math.abs(a.y - b.y));

class RouteFollower {
  constructor(waypoints = [], options = {}) {
    this.waypoints = waypoints;
    this.tolerance = options.tolerance ?? 1;
    // An action waypoint needs the exact tile: a rope used one tile off the
    // rope spot does nothing, while walking tolerance may stay loose.
    this.actionTolerance = options.actionTolerance ?? 0;
    this.loop = options.loop ?? false;
    this.replanMS = options.replanMS ?? 500;
    // A failure is a moment, not a verdict: one dropped request must not freeze
    // guidance until the user toggles following off and on again.
    this.retryMS = options.retryMS ?? 4000;
    this.reset();
  }
  reset() {
    this.index = 0;
    this.finished = false;
    this.path = null;
    this.pathTo = null;
    this.blocked = null;
    this.requestedAt = null;
    this.actionAt = null;
  }
  get waypoint() {
    return this.finished ? null : this.waypoints[this.index] ?? null;
  }
  // skipTo jumps to a waypoint, dropping work done for the previous one.
  skipTo(index) {
    this.index = Math.max(0, Math.min(this.waypoints.length - 1, index));
    this.finished = false;
    this.actionAt = null;
    this.dropPath();
  }
  dropPath() {
    this.path = null;
    this.pathTo = null;
    this.blocked = null;
    this.blockedAt = null;
    this.requestedAt = null;
  }
  // askedFor is the waypoint the request was made for; a reply that took long
  // enough for the target to move describes a route the follower no longer wants.
  setPath(result, now, askedFor) {
    const target = this.waypoint;
    // Leave state untouched: whatever replaced this request is more current.
    if (!target || !askedFor || !sameTile(askedFor, target)) return;
    this.requestedAt = now;
    if (!result || !result.found) {
      this.path = null;
      this.blocked = result ? {status: result.status, reason: result.reason} : null;
      this.blockedAt = {at: now};
      return;
    }
    this.blocked = null;
    this.path = result.steps.map(s => [s[0], s[1]]);
    this.pathTo = this.waypoint && {...this.waypoint};
  }
  step(position, now) {
    const target = this.advance(position);
    if (!target) return {action: 'done'};
    // Two ways to end up waiting for a floor change: standing on a waypoint
    // that carries an action, or facing a waypoint on another floor.
    const standingOnAction = target.type !== 'walk' && target.z === position.z &&
      chebyshev(target, position) <= this.actionTolerance;
    if (standingOnAction || target.z !== position.z) {
      this.dropPath();
      if (standingOnAction) this.actionAt = {index: this.index, z: position.z};
      const verb = TRANSITION_INSTRUCTIONS[target.type] ?? TRANSITION_INSTRUCTIONS.walk;
      const floor = standingOnAction ? '' : ` → piętro ${target.z}`;
      return {action: 'transition', waypoint: target, next: this.waypoints[this.index + 1] ?? null,
        instruction: `${verb}${floor}`};
    }
    if (this.pathTo && !sameTile(this.pathTo, target)) this.dropPath();
    const ahead = this.path && remainingPath(this.path, position);
    if (ahead && ahead.length > 1) {
      return {
        action: 'walk',
        direction: direction(position, ahead[1]),
        next: ahead[1],
        remaining: ahead.length - 1,
        waypoint: target,
      };
    }
    if (this.blocked) {
      const moved = this.blockedAt?.from && !sameTile(this.blockedAt.from, position);
      const waited = now - this.blockedAt?.at >= this.retryMS;
      if (!moved && !waited) {
        this.blockedAt.from ??= {...position};
        return {action: 'blocked', waypoint: target, ...this.blocked};
      }
      this.blocked = null;
      this.blockedAt = null;
      this.requestedAt = null; // the situation changed, so ask again now
    }
    if (this.requestedAt !== null && now - this.requestedAt < this.replanMS) return {action: 'wait', waypoint: target};
    this.requestedAt = now;
    this.path = null;
    return {action: 'path', from: {...position}, to: {x: target.x, y: target.y, z: target.z}, waypoint: target};
  }
  // advance consumes every waypoint already reached and returns the next one.
  advance(position) {
    // One pass at most: a looped route whose points all sit within tolerance
    // would otherwise cycle forever and re-request a path on every reading.
    for (let visited = 0; !this.finished; visited++) {
      if (visited > this.waypoints.length) {
        this.finished = true;
        return null;
      }
      const target = this.waypoints[this.index];
      if (!target) {
        this.finished = true;
        return null;
      }
      // An action waypoint is done once the action has been carried out, which
      // the tracker reports as a change of floor. The player can also cross
      // between two readings, never being seen standing on the waypoint - then
      // standing on the floor the route continues on is the evidence.
      const armed = this.actionAt?.index === this.index && this.actionAt.z !== position.z;
      const next = this.waypoints[this.index + 1];
      const crossed = target.type !== 'walk' && target.z !== position.z && next?.z === position.z;
      const acted = armed || crossed;
      if (acted) this.actionAt = null;
      if (!acted) {
        if (target.z !== position.z || chebyshev(target, position) > this.tolerance) return target;
        if (target.type !== 'walk') return target;
      }
      this.dropPath();
      if (this.index + 1 < this.waypoints.length) {
        this.index++;
      } else if (this.loop && this.waypoints.length) {
        this.index = 0;
      } else {
        this.finished = true;
        return null;
      }
    }
    return null;
  }
}

const sameTile = (a, b) => a.x === b.x && a.y === b.y && a.z === b.z;

// remainingPath cuts the path back to the player's tile. A player standing off
// the path gets null, which sends the follower back to /api/path.
function remainingPath(path, position) {
  const at = path.findIndex(s => s[0] === position.x && s[1] === position.y);
  return at < 0 ? null : path.slice(at);
}

function direction(from, next) {
  const dx = Math.sign(next[0] - from.x) + 1;
  const dy = Math.sign(next[1] - from.y) + 1;
  return COMPASS[dy][dx];
}

globalThis.RouteFollower = RouteFollower;
if (typeof module !== 'undefined') module.exports = {RouteFollower};
