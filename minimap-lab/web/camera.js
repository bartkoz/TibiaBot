// The panel's whole job on the hot path: cut the configured rectangles out of
// the shared screen and post them. It holds no opinion about what the pixels
// mean - that moved to Go with the rest of the brain.

const MAGIC = 'MLF1';
const FORMAT_VERSION = 1;
const HEADER_SIZE = 36;
const REGION_HEADER = 12;

const REGION = {minimap: 1, hp: 2, mana: 3};

// defaultCut copies one rectangle out of a video frame as RGBA. It is replaced
// in tests, which have no canvas.
function defaultCut(video, rect) {
  const canvas = defaultCut.canvas ??= document.createElement('canvas');
  canvas.width = rect.w;
  canvas.height = rect.h;
  const c = canvas.getContext('2d', {willReadFrequently: true});
  c.drawImage(video, rect.x, rect.y, rect.w, rect.h, 0, 0, rect.w, rect.h);
  return c.getImageData(0, 0, rect.w, rect.h).data;
}

class Camera {
  constructor(options = {}) {
    this.fetch = options.fetch ?? ((...a) => globalThis.fetch(...a));
    this.cut = options.cut ?? defaultCut;
    this.onSnapshot = options.onSnapshot ?? (() => {});
    this.onError = options.onError ?? (() => {});
    this.regions = new Map();
    // The session is a uint64 and arrives as a decimal string, because a JSON
    // number would already have been rounded on the way here.
    this.session = 0n;
    this.seq = 0n;
    this.inFlight = false;
    this.lastVideoTime = null;
  }
  setSession(session) {
    this.session = session ? BigInt(session) : 0n;
    this.seq = 0n;
    this.lastVideoTime = null;
  }
  setRegion(id, rect) {
    if (!rect) this.regions.delete(id);
    else this.regions.set(id, rect);
  }
  buildBody(video, sentAtMS = 0) {
    const regions = [...this.regions.entries()]
      .map(([id, rect]) => ({id, rect, pixels: this.cut(video, rect)}));
    const payload = regions.reduce((total, r) => total + r.pixels.length, 0);
    const buffer = new ArrayBuffer(HEADER_SIZE + REGION_HEADER * regions.length + payload);
    const bytes = new Uint8Array(buffer);
    const view = new DataView(buffer);
    for (let i = 0; i < MAGIC.length; i++) bytes[i] = MAGIC.charCodeAt(i);
    bytes[4] = FORMAT_VERSION;
    bytes[5] = regions.length;
    // Bytes 6-7 are the reserved flags and must stay zero.
    view.setBigUint64(8, this.session, true);
    view.setBigUint64(16, this.seq, true);
    view.setBigUint64(24, BigInt(Math.round((video.currentTime ?? 0) * 1e6)), true);
    view.setUint32(32, Math.max(0, Math.round(sentAtMS)), true);
    let at = HEADER_SIZE + REGION_HEADER * regions.length;
    regions.forEach((r, i) => {
      const head = HEADER_SIZE + REGION_HEADER * i;
      bytes[head] = r.id;
      bytes[head + 1] = 0; // RGBA8888
      view.setUint16(head + 4, r.rect.w, true);
      view.setUint16(head + 6, r.rect.h, true);
      view.setUint32(head + 8, r.pixels.length, true);
      bytes.set(r.pixels, at);
      at += r.pixels.length;
    });
    return buffer;
  }
  // sendFrame posts one frame, or reports why it did not. Exactly one request
  // is ever in flight: two would arrive out of order and pile up behind a slow
  // match, and a skipped frame is always cheaper than a backlog.
  async sendFrame(video, cutAtMS = 0) {
    if (this.inFlight || !this.session || !this.regions.size) return null;
    // The same video frame twice is one observation. Posting it again would
    // spend a round trip to tell the brain something it already knows.
    if (this.lastVideoTime === video.currentTime) return null;
    this.lastVideoTime = video.currentTime;
    this.seq += 1n;
    this.inFlight = true;
    const session = this.session;
    try {
      const body = this.buildBody(video, cutAtMS);
      // Never keepalive: Chrome caps such requests at 64 kB of body and drops
      // anything larger without a word.
      const response = await this.fetch('/api/frame', {
        method: 'POST',
        headers: {'Content-Type': 'application/octet-stream'},
        body,
      });
      const state = await response.json();
      if (session !== this.session) return null;
      if (!response.ok) {
        this.onError(state?.reason ?? `błąd ${response.status}`);
        return null;
      }
      this.onSnapshot(state);
      return state;
    } catch (e) {
      this.onError(e.message);
      return null;
    } finally {
      this.inFlight = false;
    }
  }
}

globalThis.Camera = Camera;
globalThis.FRAME_REGION = REGION;
if (typeof module !== 'undefined') module.exports = {Camera, REGION, HEADER_SIZE, REGION_HEADER, MAGIC};
