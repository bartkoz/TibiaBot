// Route recording. The panel feeds it every confirmed position; it decides
// which of them become waypoints. Kept free of the DOM so it can be tested.
const RECORDER_LIMIT = 1000;

const tileDistance = (a, b) => Math.max(Math.abs(a.x - b.x), Math.abs(a.y - b.y));

// The minimap encodes no transition type: rope, ladder, stairs and hole look
// alike. Direction and displacement give the likely one; the panel lets the
// user correct it.
function guessTransition(from, to) {
  if (tileDistance(from, to) > 0) return 'stairs';
  return to.z < from.z ? 'rope' : 'hole';
}

class RouteRecorder {
  constructor(options = {}) {
    this.every = options.every ?? 10;
    this.auto = options.auto ?? false;
    this.waypoints = [];
    this.last = null;
    this.lastSaved = null;
  }
  get full() {
    return this.waypoints.length >= RECORDER_LIMIT;
  }
  push(position, type) {
    if (this.full) return false;
    this.waypoints.push({x: position.x, y: position.y, z: position.z, type, label: ''});
    this.lastSaved = {...position};
    return true;
  }
  addManual(position) {
    return this.push(position, 'walk');
  }
  // observe consumes one tracked position and returns how many waypoints it
  // added. A floor change always records the tile before the transition, which
  // the tracker only reveals once the player is already on the new floor.
  observe(position) {
    if (!this.auto) {
      this.last = {...position};
      return 0;
    }
    const previous = this.last;
    this.last = {...position};
    if (previous && previous.z !== position.z) {
      const added = this.push(previous, guessTransition(previous, position)) ? 1 : 0;
      return added + (this.push(position, 'walk') ? 1 : 0);
    }
    if (!this.lastSaved || tileDistance(this.lastSaved, position) >= this.every) {
      return this.push(position, 'walk') ? 1 : 0;
    }
    return 0;
  }
}

globalThis.RouteRecorder = RouteRecorder;
if (typeof module !== 'undefined') module.exports = {RouteRecorder, RECORDER_LIMIT};
