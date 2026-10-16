// Kiosk: a full-screen wall display. No navigation, large type, a clock,
// the screen kept awake and the cursor hidden when idle.
import { h, replace } from '../lib/dom.js';
import { get } from '../lib/api.js';
import { logoMark } from '../lib/icons.js';
import * as live from '../lib/live.js';
import * as fmt from '../lib/format.js';
import { colors, statusPill, pill } from '../lib/ui.js';
import { renderControl, renderFlow, renderToday } from './overview.js';

export async function render(root, { can }) {
  const c = colors();
  const title = h('h1', {}, 'AsyncOS');
  const status = h('span');
  const clockTime = h('b');
  const clockDate = h('span');
  const flow = h('div', { class: 'flow' });
  const control = h('div');
  const today = h('div', { class: 'stack' });
  const updated = h('span');
  const connection = h('span');

  replace(root, h('div', { class: 'kiosk' },
    h('header', { class: 'kiosk-head' },
      h('div', { class: 'brand' }, logoMark(), h('span', { class: 'brand-name' }, 'Async', h('span', {}, 'OS'))),
      h('div', {}, title),
      status,
      h('div', { class: 'kiosk-clock' }, clockTime, clockDate)),
    h('div', { class: 'kiosk-body' },
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Power flow'))), flow),
      h('div', { class: 'kiosk-side' },
        h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Controller'))), control),
        can('energy.view') && h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Today'))), today))),
    h('footer', { class: 'kiosk-foot' }, connection, updated)));

  const tick = () => {
    const now = new Date();
    clockTime.textContent = now.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
    clockDate.textContent = now.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' });
  };
  tick();
  const clock = setInterval(tick, 1000);

  live.connect(() => location.reload());
  const unsubscribe = live.subscribe((snap, meta) => {
    title.textContent = snap.site_name || 'AsyncOS';
    replace(status, statusPill(snap.decision?.status));
    renderFlow(flow, snap, c);
    renderControl(control, snap, () => false);
    replace(connection, meta?.stale ? pill('Reconnecting…', 'warn') : pill('Live', 'ok'));
    updated.textContent = `Updated ${fmt.time(snap.time)}`;
  });

  const loadToday = async () => {
    if (!can('energy.view')) return;
    try { renderToday(today, (await get('/energy/summary')).today, live.getLatest(), c); } catch { /* keep the last values */ }
  };
  loadToday();
  const energyTimer = setInterval(loadToday, 60000);

  // Keep the screen on, and re-acquire the lock when the page is shown again.
  let wakeLock = null;
  const lock = async () => { try { wakeLock = await navigator.wakeLock?.request('screen'); } catch { /* not supported */ } };
  const onVisible = () => { if (document.visibilityState === 'visible') lock(); };
  lock();
  document.addEventListener('visibilitychange', onVisible);

  // Hide the cursor after a few idle seconds.
  let idle;
  const wake = () => {
    document.body.style.cursor = '';
    clearTimeout(idle);
    idle = setTimeout(() => { document.body.style.cursor = 'none'; }, 4000);
  };
  wake();
  document.addEventListener('pointermove', wake);

  // A wall display runs for months: reload daily to pick up UI updates.
  const reload = setTimeout(() => location.reload(), 24 * 3600 * 1000);

  return () => {
    unsubscribe();
    clearInterval(clock);
    clearInterval(energyTimer);
    clearTimeout(reload);
    clearTimeout(idle);
    document.body.style.cursor = '';
    document.removeEventListener('visibilitychange', onVisible);
    document.removeEventListener('pointermove', wake);
    wakeLock?.release?.();
  };
}
