// Overview: live power flow, controller state, today's energy, live chart.
import { h, s, replace } from '../lib/dom.js';
import { get, put } from '../lib/api.js';
import { icon } from '../lib/icons.js';
import * as live from '../lib/live.js';
import * as fmt from '../lib/format.js';
import { lineChart, legend } from '../lib/charts.js';
import { banner, colors, field, formDialog, kpi, numberInput, pageHead, ring, statusPill, pill, toast } from '../lib/ui.js';

export async function render(main, { can }) {
  const c = colors();
  const head = h('div');
  const alarms = h('div', { class: 'stack' });
  const flowBody = h('div', { class: 'flow' });
  const controlBody = h('div');
  const todayBody = h('div', { class: 'stack' }, h('div', { class: 'kpis' }, h('div', { class: 'kpi skeleton' }, '.')));
  const chartBody = h('div');

  main.append(
    head,
    h('div', { class: 'stack' },
      alarms,
      h('div', { class: 'grid cols-hero' },
        h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Power flow'), h('p', {}, 'Live, updated every second'))), flowBody),
        h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Controller'), h('p', {}, 'PV limit and the reason for it'))), controlBody)),
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Today'), h('p', {}, 'Energy since midnight, site time'))), todayBody),
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Power'), h('p', {}, 'Last 15 minutes'))), chartBody)));

  let lastChart = 0;
  const unsubscribe = live.subscribe((snap, meta) => {
    renderHead(head, snap, meta);
    renderAlarms(alarms, snap);
    renderFlow(flowBody, snap, c);
    renderControl(controlBody, snap, can);
    if (Date.now() - lastChart > 2000) {
      lastChart = Date.now();
      renderChart(chartBody, snap, c);
    }
  });
  if (!live.getLatest()) replace(head, pageHead('Overview', 'Waiting for the first reading…'));

  const loadToday = async () => {
    try {
      renderToday(todayBody, (await get('/energy/summary')).today, live.getLatest(), c);
    } catch (err) {
      replace(todayBody, h('p', { class: 'muted' }, err.message));
    }
  };
  loadToday();
  const timer = setInterval(loadToday, 60000);
  return () => { unsubscribe(); clearInterval(timer); };
}

function renderHead(el, snap, meta) {
  const status = snap.decision?.status;
  const mode = snap.mode === 'control' ? pill('Control on', 'plain') : pill('Monitor only', 'warn');
  const stale = meta?.stale ? pill('Reconnecting…', 'warn') : null;
  replace(el, pageHead(snap.site_name || 'Overview', 'Live view of the site', stale, mode, statusPill(status)));
}

function renderAlarms(el, snap) {
  const list = snap.decision?.alarms || [];
  replace(el, list.filter((a) => a.critical).map((a) => banner('danger', [h('b', {}, a.message), a.device ? ` (${a.device})` : ''])));
}

/** Power-flow diagram: solar top, grid left, load right, genset below. */
export function renderFlow(el, snap, c) {
  const hasGenset = snap.genset_w != null;
  const hasGrid = snap.grid_w != null || snap.devices?.some((d) => d.role === 'grid_meter');
  const height = hasGenset ? 372 : 262;
  const J = { x: 210, y: 160 };
  const nodes = [
    { key: 'solar', label: 'Solar', icon: 'sun', color: c.solar, x: 210, y: 46, value: snap.pv_w },
    hasGrid && { key: 'grid', label: snap.grid_w < 0 ? 'Grid · exporting' : 'Grid', icon: 'grid', color: snap.grid_w < 0 ? c.export : c.grid, x: 58, y: 160, value: snap.grid_w == null ? null : Math.abs(snap.grid_w) },
    { key: 'load', label: 'Load', icon: 'load', color: c.load, x: 362, y: 160, value: snap.load_w },
    hasGenset && { key: 'genset', label: snap.genset_w < 0 ? 'Genset · reverse!' : 'Genset', icon: 'genset', color: c.genset, x: 210, y: 292, value: snap.genset_w },
  ].filter(Boolean);

  const R = 32;
  const svg = s('svg', { viewBox: `0 0 420 ${height}`, role: 'img', 'aria-label': 'Power flow' });
  const wire = (d, color, watts, reverse) => {
    svg.append(s('path', { class: 'wire', d }));
    const idle = watts == null || Math.abs(watts) < 20;
    const path = s('path', { class: ['current', reverse && 'reverse', idle && 'idle'], d, stroke: color });
    path.style.animationDuration = `${Math.max(0.35, 1.6 - Math.log10(Math.abs(watts || 1) + 1) * 0.25)}s`;
    svg.append(path);
  };
  wire(`M${J.x},${46 + R} V${J.y}`, c.solar, snap.pv_w, false);
  if (hasGrid) wire(`M${58 + R},${J.y} H${J.x}`, snap.grid_w < 0 ? c.export : c.grid, snap.grid_w, snap.grid_w < 0);
  wire(`M${J.x},${J.y} H${362 - R}`, c.load, snap.load_w, false);
  if (hasGenset) wire(`M${J.x},${292 - R} V${J.y}`, c.genset, snap.genset_w, snap.genset_w < 0);
  svg.append(s('circle', { cx: J.x, cy: J.y, r: 4, fill: 'var(--muted-foreground)' }));

  for (const n of nodes) {
    svg.append(s('circle', { class: 'node-ring', cx: n.x, cy: n.y, r: R, stroke: n.color }));
    svg.append(icon(n.icon, { x: n.x - 14, y: n.y - 14, width: 28, height: 28, class: 'node-icon', stroke: n.color }));
    // Labels sit beside the vertical wire for top and bottom nodes, below the others.
    const side = n.key === 'solar' || n.key === 'genset';
    const tx = side ? n.x + R + 10 : n.x;
    const ty = side ? n.y - 2 : n.y + R + 20;
    const anchor = side ? 'start' : 'middle';
    svg.append(s('text', { class: 'node-value', x: tx, y: ty, 'text-anchor': anchor }, fmt.powerText(n.value)));
    svg.append(s('text', { class: 'node-label', x: tx, y: ty + 16, 'text-anchor': anchor }, n.label));
  }
  replace(el, svg);
}

export function renderControl(el, snap, can) {
  const d = snap.decision || {};
  const limit = d.limit_pct ?? 100;
  const ctl = h('div', { class: 'stack' },
    h('div', { class: 'row' },
      h('div', { class: 'big-number' }, fmt.number(limit, limit % 1 ? 1 : 0), h('small', {}, '% PV limit')),
      h('div', { class: 'spacer' }),
      statusPill(d.status)),
    h('div', { class: 'meter', role: 'meter', 'aria-valuenow': limit, 'aria-valuemin': 0, 'aria-valuemax': 100 },
      h('div', { style: { width: `${Math.max(0, Math.min(100, limit))}%` } })),
    h('p', { class: 'muted small', style: { margin: 0 } }, d.reason || 'Starting…'),
    h('dl', { class: 'facts' },
      fact('PV output', fmt.powerText(snap.pv_w)),
      fact('Controllable', fmt.powerText(d.controllable_rated_w)),
      fact('Headroom', snap.headroom_w == null ? '–' : fmt.powerText(snap.headroom_w)),
      fact('Holding for', d.binding === 'grid' ? 'Export limit' : d.binding === 'genset' ? 'Genset minimum' : '–'),
      fact('Grid', d.grid_present ? 'Available' : snap.grid_w == null ? 'Not metered' : 'Down'),
      fact('Gensets running', String(d.gensets_running ?? 0))));
  if (can('control.setpoint')) {
    ctl.append(h('div', { class: 'row' },
      h('button', { class: 'btn sm', onclick: () => editExportLimit() }, icon('edit'), 'Change export limit')));
  }
  replace(el, ctl);
}

function fact(label, value) {
  return h('div', {}, h('dt', {}, label), h('dd', {}, value));
}

async function editExportLimit() {
  const site = await get('/site');
  const kw = site.control.export_limit_w / 1000;
  await formDialog({
    title: 'Export limit',
    description: site.operator_export_limit_max_w > 0
      ? `Operators may set 0 to ${fmt.number(site.operator_export_limit_max_w / 1000, 1)} kW. 0 means zero export.`
      : 'The most power the site may send to the grid. 0 means zero export.',
    body: field('Export limit', numberInput('limit', kw, { min: 0, step: 0.1 }), { unit: 'kW' }),
    onSubmit: async (form) => {
      await put('/site/export-limit', { export_limit_w: Math.round(Number(form.limit.value) * 1000) });
      toast('Export limit updated');
    },
  });
}

export function renderToday(el, today, snap, c) {
  const e = today.energy_wh;
  const tiles = [
    kpi('Solar', fmt.energy(e.pv), c.solar),
    kpi('From grid', fmt.energy(e.grid_import), c.grid),
    kpi('To grid', fmt.energy(e.grid_export), c.export),
  ];
  if (e.genset > 0 || snap?.genset_w != null) tiles.push(kpi('Genset', fmt.energy(e.genset), c.genset));
  tiles.push(kpi('Consumption', fmt.energy(e.load), c.load));
  replace(el,
    h('div', { class: 'kpis' }, tiles),
    h('div', { class: 'row', style: { gap: '28px', marginTop: '4px' } },
      h('div', { class: 'ratio' }, ring(today.self_consumption, c.solar),
        h('div', {}, h('b', {}, 'Self-consumption'), h('div', { class: 'muted small' }, 'Share of solar used on site'))),
      h('div', { class: 'ratio' }, ring(today.self_sufficiency, c.load),
        h('div', {}, h('b', {}, 'Self-sufficiency'), h('div', { class: 'muted small' }, 'Share of load covered by solar')))));
}

function renderChart(el, snap, c) {
  const points = live.getHistory();
  const series = [
    { key: 'pv', label: 'Solar', color: c.solar, area: true },
    { key: 'load', label: 'Load', color: c.load },
  ];
  if (points.some((p) => p.grid != null)) series.push({ key: 'grid', label: 'Grid (+ import)', color: c.grid });
  if (points.some((p) => p.genset != null)) series.push({ key: 'genset', label: 'Genset', color: c.genset });
  // The axis spans the data collected so far, up to 15 minutes.
  const now = Date.now();
  const first = points[0]?.t ?? now;
  const xMin = Math.min(now - 60000, Math.max(now - 15 * 60000, first));
  replace(el,
    lineChart({
      series, points, height: 260, xMin, xMax: now,
      format: (v) => fmt.powerText(v),
      timeFormat: (t, long) => new Date(t).toLocaleTimeString(undefined, long || now - xMin < 10 * 60000 ? { hour: '2-digit', minute: '2-digit', second: '2-digit' } : { hour: '2-digit', minute: '2-digit' }),
    }),
    legend(series));
}
