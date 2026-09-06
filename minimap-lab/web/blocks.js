// Panel-side client of the learned-blockage store. Owns nothing but the
// transport: what to report and when is decided by the executor and the panel
// loop, exactly as with the input driver.
class BlocksClient {
  constructor(options = {}) {
    this.fetch = options.fetch ?? ((...a) => globalThis.fetch(...a));
    // The preview is for a human's eyes; ten times a second buys nothing and
    // would compete with the tracking loop for the connection.
    this.minIntervalMS = options.minIntervalMS ?? 500;
    this.now = options.now ?? (() => performance.now());
    this.windowInFlight = false;
    this.lastTile = null;
    this.lastAt = null;
    // Reports are chained, never sent in parallel. A failure and the arrival
    // that revokes it are reported from separate ticks; if the revocation
    // overtook the failure on the wire, the server would clear a block that
    // does not exist yet and then create it - marking as unreachable the very
    // tile the character just walked onto.
    this.reportChain = Promise.resolve();
  }
  // A failed report is not worth retrying: the same failure will be reported
  // again on the next attempt at the same tile, and a queue of stale
  // observations would teach the map about a situation long gone.
  report(observation) {
    const sent = this.reportChain.then(() => this.sendReport(observation));
    // The chain must survive a failed report; only the caller sees the error.
    this.reportChain = sent.catch(() => {});
    return sent;
  }
  async sendReport(observation) {
    try {
      const r = await this.fetch('/api/blocks/observe', {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(observation),
      });
      if (!r.ok) return null;
      return await r.json();
    } catch { return null; }
  }
  // list is what the panel uses to say what it just deleted: the raw preview
  // window carries only the kind bits, not the episode count or the countdown.
  async list(x, y, z, r) {
    try {
      const res = await this.fetch(`/api/blocks?x=${x}&y=${y}&z=${z}&r=${r}`);
      if (!res.ok) return [];
      return await res.json();
    } catch { return []; }
  }
  async remove(x, y, z) {
    try {
      const r = await this.fetch('/api/blocks', {
        method: 'DELETE', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({x, y, z}),
      });
      if (!r.ok) return false;
      return !!(await r.json()).cleared;
    } catch { return false; }
  }
  // shouldRefresh answers whether the preview is worth re-fetching now: the
  // character moved to another tile, or the interval elapsed anyway - a block
  // can appear without the character moving at all.
  shouldRefresh(position, now) {
    if (!position) return false;
    if (this.lastTile !== `${position.x},${position.y},${position.z}`) return true;
    return this.lastAt === null || now - this.lastAt >= this.minIntervalMS;
  }
  async window(x, y, z, r) {
    if (this.windowInFlight) return null;
    this.windowInFlight = true;
    try {
      const res = await this.fetch(`/api/grid?x=${x}&y=${y}&z=${z}&r=${r}`);
      if (!res.ok) return null;
      const origin = (res.headers.get('X-Grid-Origin') ?? '0,0').split(',').map(Number);
      const revision = Number(res.headers.get('X-Grid-Revision') ?? 0);
      const cells = new Uint8Array(await res.arrayBuffer());
      this.lastTile = `${x},${y},${z}`;
      this.lastAt = this.now();
      // z travels with the window: the click handler must delete a block on the
      // floor the picture shows, not on whatever floor the character has since
      // walked to.
      return {origin, z, revision, cells};
    } catch {
      return null;
    } finally {
      this.windowInFlight = false;
    }
  }
}

globalThis.BlocksClient = BlocksClient;
if (typeof module !== 'undefined') module.exports = {BlocksClient};
