// Pure tracking state and cadence calculations, shared by the UI and tests.
class MinimapTracker {
  constructor() { this.reset(); }
  reset() { this.anchor = null; this.misses = 0; this.readings = []; this.seeded = false; }
  hint(now, floor, zoom, speed = 20) {
    if (!this.anchor || this.misses >= 3 || Math.abs(this.anchor.position.z-floor)>1 || this.anchor.zoom !== zoom) return null;
    const age = Math.max(0, now - this.anchor.at);
    if (age > 30000 && !this.seeded) return null;
    // Global results describe an older captured frame; confirm them locally
    // in a wider region before starting the small, steady tracking window.
    const radius = Math.min(64, Math.max(5, Math.ceil(age / 1000 * speed) + 2 + this.misses * 5));
    return {near:{...this.anchor.position}, radius};
  }
  observe(result, capturedAt, completedAt, roundTrip) {
    if (result.mode === 'global') this.readings = [];
    if (result.mode === 'local') this.readings.push({at:completedAt, found:result.found, roundTrip, matchMS:result.match_ms});
    this.readings = this.readings.filter(r => completedAt-r.at <= 3000).slice(-40);
    if (result.found && result.position) {
      this.anchor = {position:{...result.position}, zoom:result.zoom, at:capturedAt};
      this.misses = 0; this.seeded = result.mode === 'global';
    } else {
      this.misses++;
      if (result.mode === 'global') this.anchor = null;
    }
  }
  stats(now) {
    const rows = this.readings.filter(r => now-r.at <= 3000);
    const latest = rows.at(-1), duration = rows.length>1 ? latest.at-rows[0].at : 0;
    return {
      hz:duration>0 && now-latest.at<600 ? (rows.length-1)*1000/duration : 0,
      success:rows.length ? rows.filter(r=>r.found).length/rows.length : 0,
      ageMS:this.anchor ? Math.max(0,now-this.anchor.at) : null,
      roundTrip:latest?.roundTrip ?? null,
      matchMS:latest?.matchMS ?? null
    };
  }
}
function minimapNextDelay(started, completed, hz) { return Math.max(0,1000/hz-(completed-started)); }
globalThis.MinimapTracker = MinimapTracker;
globalThis.minimapNextDelay = minimapNextDelay;
if (typeof module !== 'undefined') module.exports = {MinimapTracker, minimapNextDelay};
