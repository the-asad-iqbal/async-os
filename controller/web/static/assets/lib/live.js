// Live snapshots over server-sent events, shared by every page. A rolling
// history feeds the live charts, so it survives page changes.

const HISTORY_SECONDS = 15 * 60;
const listeners = new Set();
let source = null;
let latest = null;
const history = [];

function push(snapshot) {
  latest = snapshot;
  if (!snapshot.time || snapshot.time.startsWith('0001')) return;
  history.push({
    t: new Date(snapshot.time).getTime(),
    grid: snapshot.grid_w,
    pv: snapshot.pv_w,
    genset: snapshot.genset_w,
    load: snapshot.load_w,
    limit: snapshot.decision?.limit_pct,
  });
  const cutoff = Date.now() - HISTORY_SECONDS * 1000;
  while (history.length && history[0].t < cutoff) history.shift();
}

export function connect(onSignedOut) {
  if (source) return;
  source = new EventSource('/api/v1/live');
  source.addEventListener('snapshot', (event) => {
    push(JSON.parse(event.data));
    for (const fn of listeners) fn(latest);
  });
  source.addEventListener('signed_out', () => {
    disconnect();
    onSignedOut();
  });
  source.onerror = () => {
    for (const fn of listeners) fn(latest, { stale: true });
  };
}

export function disconnect() {
  source?.close();
  source = null;
  latest = null;
  history.length = 0;
}

/** Calls fn now (if data exists) and on every snapshot; returns unsubscribe. */
export function subscribe(fn) {
  listeners.add(fn);
  if (latest) fn(latest);
  return () => listeners.delete(fn);
}

export function getHistory() { return history; }
export function getLatest() { return latest; }
