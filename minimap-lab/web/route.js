// Route file format, shared by the panel and its tests. A route is a list of
// waypoints; the type says what to do there, and every unknown field is dropped
// so a hand-edited file cannot smuggle anything into the panel.
const WAYPOINT_TYPES = ['walk', 'rope', 'ladder', 'stairs', 'hole', 'shovel'];
const ROUTE_VERSION = 1;
const MAX_WAYPOINTS = 1000;
const MAX_LABEL = 64;

const isTile = v => Number.isInteger(v) && v >= 0 && v <= 65535;

function parseWaypoint(raw, index) {
  const at = `Waypoint ${index + 1}`;
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error(`${at}: oczekiwano obiektu.`);
  if (!isTile(raw.x) || !isTile(raw.y)) throw new Error(`${at}: X i Y muszą być liczbami całkowitymi 0–65535.`);
  if (!Number.isInteger(raw.z) || raw.z < 0 || raw.z > 15) throw new Error(`${at}: Z musi być liczbą całkowitą 0–15.`);
  const type = raw.type === undefined ? 'walk' : raw.type;
  if (!WAYPOINT_TYPES.includes(type)) throw new Error(`${at}: nieznany typ „${type}”. Dozwolone: ${WAYPOINT_TYPES.join(', ')}.`);
  const label = raw.label === undefined || raw.label === null ? '' : String(raw.label).slice(0, MAX_LABEL);
  return {x: raw.x, y: raw.y, z: raw.z, type, label};
}

function parseRoute(text) {
  let data;
  try {
    data = JSON.parse(text);
  } catch (e) {
    throw new Error(`Plik nie jest poprawnym JSON-em: ${e.message}`);
  }
  if (!data || typeof data !== 'object' || Array.isArray(data)) throw new Error('Plik trasy musi być obiektem JSON.');
  if (data.version !== ROUTE_VERSION) throw new Error(`Nieobsługiwana wersja pliku: ${data.version}. Ten panel czyta wersję ${ROUTE_VERSION}.`);
  if (!Array.isArray(data.waypoints)) throw new Error('Pole waypoints musi być listą.');
  if (data.waypoints.length > MAX_WAYPOINTS) throw new Error(`Trasa ma ${data.waypoints.length} punktów; limit to ${MAX_WAYPOINTS}.`);
  return {
    version: ROUTE_VERSION,
    name: data.name === undefined || data.name === null ? '' : String(data.name).slice(0, MAX_LABEL),
    waypoints: data.waypoints.map(parseWaypoint),
  };
}

function serializeRoute(route) {
  return JSON.stringify({version: ROUTE_VERSION, name: route.name || '', waypoints: route.waypoints}, null, 2);
}

globalThis.parseRoute = parseRoute;
globalThis.serializeRoute = serializeRoute;
globalThis.WAYPOINT_TYPES = WAYPOINT_TYPES;
if (typeof module !== 'undefined') module.exports = {parseRoute, serializeRoute, WAYPOINT_TYPES, ROUTE_VERSION};
