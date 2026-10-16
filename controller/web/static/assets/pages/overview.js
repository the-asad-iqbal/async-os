// Overview: live power flow, controller state, today's energy, live chart.
import { h, s, replace } from '../lib/dom.js';
import { get, put } from '../lib/api.js';
import { icon } from '../lib/icons.js';
import { mdiIcon } from '../lib/mdi.js';
import * as live from '../lib/live.js';
import * as fmt from '../lib/format.js';
import { lineChart, legend } from '../lib/charts.js';
import { banner, colors, field, formDialog, kpi, numberInput, pageHead, ring, statusPill, pill, toast } from '../lib/ui.js';


let _powerFlowCardCtor = null;
async function loadPowerFlowCard() {
  if (_powerFlowCardCtor) return _powerFlowCardCtor;
  try {
    const mod = await import('/assets/power-flow-card-plus.js');
    _powerFlowCardCtor = mod.PowerFlowCardPlus || null;
  } catch (err) { console.error('power flow card module failed', err); }
  return _powerFlowCardCtor;
}
function ensureHACardElements() {
  if (!customElements.get('ha-card')) {
    class AsyncOSHaCard extends HTMLElement {
      connectedCallback() {
        this.style.display = 'block';
        this.style.overflow = 'hidden';
        this.style.position = 'relative';
      }
    }
    customElements.define('ha-card', AsyncOSHaCard);
  }
  if (!customElements.get('ha-ripple')) {
    class AsyncOSHaRipple extends HTMLElement {
      connectedCallback() { this.style.display = 'none'; }
    }
    customElements.define('ha-ripple', AsyncOSHaRipple);
  }
  if (!customElements.get('ha-icon')) {
    const iconMap = {
      'mdi:solar-power': 'mdi:solar-power',
      'mdi:solar-panel': 'mdi:solar-power',
      'mdi:transmission-tower': 'mdi:transmission-tower',
      'mdi:transmission-tower-off': 'mdi:transmission-tower-off',
      'mdi:home': 'mdi:home',
      'mdi:battery-high': 'mdi:battery-high',
      'mdi:battery-charging': 'mdi:battery-charging',
    };
    class AsyncOSHaIcon extends HTMLElement {
      static get observedAttributes() { return ['icon']; }
      connectedCallback() {
        // The card's own stylesheet sets `ha-icon { display:inline;
        // padding-bottom:2px }`; give the shadow content a definite box.
        this.style.display = 'inline-flex';
        this.style.width = 'var(--mdc-icon-size, 24px)';
        this.style.height = 'var(--mdc-icon-size, 24px)';
        this.style.alignItems = 'center';
        this.style.justifyContent = 'center';
        this._render();
      }
      attributeChangedCallback() { this._render(); }
      set icon(name) { this.setAttribute('icon', name || ''); }
      get icon() { return this.getAttribute('icon'); }
      _render() {
        const name = iconMap[this.getAttribute('icon')] || 'mdi:help-circle';
        const size = getComputedStyle(this).getPropertyValue('--mdc-icon-size').trim() || '24px';
        // The card stylesheet has `.circle svg { width:100%; height:100%;
        // position:absolute; fill:none }`, which would stretch and unfill any
        // plain <svg> inside a circle. The icons therefore live in this
        // element's own shadow root, out of reach of that rule.
        if (!this.shadowRoot) this.attachShadow({ mode: 'open' });
        this.shadowRoot.replaceChildren(mdiIcon(name, size));
      }
    }
    customElements.define('ha-icon', AsyncOSHaIcon);
  }
}
/**
 * The card's own :host{} rule sets its icon colours from Home Assistant theme
 * variables, and a :host rule beats a document stylesheet rule. So the theme
 * has to be applied as inline custom properties on the element itself.
 */
function applyCardTheme(el) {
  const dark = window.matchMedia?.('(prefers-color-scheme: dark)').matches;
  const cs = getComputedStyle(document.documentElement);
  const v = (name, fallback) => (cs.getPropertyValue(name).trim() || fallback);
  const theme = {
    '--primary-text-color': v('--foreground', dark ? '#fafafa' : '#09090b'),
    '--secondary-text-color': v('--muted-foreground', dark ? '#a1a1aa' : '#71717a'),
    '--primary-color': v('--primary', dark ? '#7e7ff9' : '#5b5cf6'),
    '--card-background-color': v('--card', dark ? '#09090b' : '#ffffff'),
    '--divider-color': v('--border', dark ? '#27272a' : '#e4e4e7'),
    '--energy-solar-color': v('--chart-solar', '#f59e0b'),
    '--energy-grid-consumption-color': v('--chart-grid', dark ? '#7e7ff9' : '#5b5cf6'),
    '--energy-grid-return-color': v('--chart-export', '#06b6d4'),
    '--energy-battery-in-color': v('--chart-genset', '#f43f5e'),
    '--energy-battery-out-color': v('--chart-genset', '#f43f5e'),
    '--energy-non-fossil-color': v('--success', '#16a34a'),
    '--non-fossil-color': v('--success', '#16a34a'),
  };
  // Icon colours are separate variables in the card; mirror the flow colours.
  theme['--icon-solar-color'] = theme['--energy-solar-color'];
  theme['--icon-grid-color'] = theme['--energy-grid-consumption-color'];
  theme['--icon-home-color'] = theme['--energy-grid-consumption-color'];
  theme['--icon-battery-color'] = theme['--energy-battery-in-color'];
  theme['--icon-non-fossil-color'] = theme['--non-fossil-color'];
  theme['--cirlce-grid-color'] = theme['--energy-grid-consumption-color'];
  theme['--circle-battery-color'] = theme['--energy-battery-in-color'];
  theme['--battery-grid-line'] = theme['--energy-grid-return-color'];
  theme['--text-solar-color'] = theme['--primary-text-color'];
  theme['--text-home-color'] = theme['--primary-text-color'];
  theme['--text-non-fossil-color'] = theme['--primary-text-color'];
  theme['--text-grid-consumption-color'] = theme['--energy-grid-consumption-color'];
  theme['--text-grid-return-color'] = theme['--energy-grid-return-color'];
  theme['--text-battery-in-color'] = theme['--energy-battery-in-color'];
  theme['--text-battery-out-color'] = theme['--energy-battery-out-color'];
  // Layout corrections on top of the colours. The card paints its flow lines in
  // an absolutely positioned .lines layer that sits above the circles, so the
  // lines run straight through the node discs. Lift the circles above that
  // layer and hide the small seam where the SVG paths meet the ring edges.
  theme['--clickable-cursor'] = 'default';
  theme['--lines-svg-not-flat-line-top'] = '0%';
  theme['--lines-svg-flat-width'] = 'calc(100% - 156px)';
  theme['--lines-svg-not-flat-width'] = 'calc(103% - 160px)';
  theme['--size-circle-entity'] = '84px';
  for (const [name, value] of Object.entries(theme)) el.style.setProperty(name, value);

  patchCardStyles(el);
}

/** Shadow-scoped corrections. Re-applied until the shadow root exists. */
function patchCardStyles(el) {
  const root = el.shadowRoot;
  if (!root) { requestAnimationFrame(() => patchCardStyles(el)); return; }
  if (root.getElementById('asyncos-flow-patch')) return;
  const style = document.createElement('style');
  style.id = 'asyncos-flow-patch';
  style.textContent = `
    .card-content > .row { position: relative; z-index: 2; }
    .circle-container { position: relative; z-index: 2; }
    .circle { background: var(--card-background-color, #fff); }
    .lines { z-index: 1; }
    .lines svg { overflow: visible; }
    ha-icon { display: inline-flex; width: var(--mdc-icon-size, 24px); height: var(--mdc-icon-size, 24px); align-items: center; justify-content: center; }
  `;
  root.prepend(style);
}

/** The handful of Home Assistant localisation strings the card asks for. */
const CARD_LABELS = {
  solar: 'Solar',
  grid: 'Grid',
  home: 'Home',
  battery: 'Battery',
  non_fossil: 'Non-fossil',
  consumption: 'Consumption',
  return: 'Return',
  production: 'Production',
};

/** HA localisation keys are dotted paths; match on the last segment. */
function cardLocalize(key) {
  if (!key) return '';
  const tail = String(key).split('.').pop();
  return CARD_LABELS[tail] || '';
}

function makeHass(snap) {
  return {
    states: {
      'sensor.solar_power': { state: String(snap?.pv_w ?? 0), attributes: { unit_of_measurement: 'W', friendly_name: 'Solar' } },
      'sensor.grid_power': { state: String(snap?.grid_w ?? 0), attributes: { unit_of_measurement: 'W', friendly_name: 'Grid' } },
      'sensor.home_power': { state: String(snap?.load_w ?? 0), attributes: { unit_of_measurement: 'W', friendly_name: 'Home' } },
      'sensor.battery_power': { state: String(snap?.genset_w ?? 0), attributes: { unit_of_measurement: 'W', friendly_name: 'Genset' } },
    },
    locale: { language: 'en-US' },
    config: { components: [] },
    user: { name: 'Controller' },
    connection: { subscribeMessage: async () => () => {}, sendMessagePromise: async () => ({}) },
    localize: cardLocalize,
    formatEntityState: (st) => (st?.state ?? '') + ' ' + (st?.attributes?.unit_of_measurement || ''),
    formatEntityAttributeValue: (st, attr, value) => value,
  };
}

export async function render(main, { can }) {
  const c = colors();
  const head = h('div');
  const alarms = h('div', { class: 'stack' });
  const flowBody = h('div', { class: 'flow' });
  let flowEl = null;
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

  const PowerFlowCard = await loadPowerFlowCard();
  if (PowerFlowCard) {
    try {
      ensureHACardElements();
      flowEl = document.createElement('power-flow-card-plus');
      applyCardTheme(flowEl);
      flowEl.setConfig({
        type: 'custom:power-flow-card-plus',
        entities: {
          solar: { entity: 'sensor.solar_power' },
          grid: { entity: 'sensor.grid_power' },
          home: { entity: 'sensor.home_power' },
          battery: { entity: 'sensor.battery_power', name: 'Genset' },
        },
        watt_threshold: 1000,
        clickable_entities: false,
      });
      replace(flowBody, flowEl);
      } catch (err) { console.error('power flow card render failed', err); console.log(err && err.stack); }
  }

  let lastChart = 0;
  const unsubscribe = live.subscribe((snap, meta) => {
    renderHead(head, snap, meta);
    renderAlarms(alarms, snap);
    if (flowEl) { flowEl.hass = makeHass(snap); } else { renderFlow(flowBody, snap, c); }
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
