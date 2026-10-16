// Energy: totals and per-period bars, like a home energy dashboard.
import { h, replace } from '../lib/dom.js';
import { get } from '../lib/api.js';
import { icon } from '../lib/icons.js';
import * as fmt from '../lib/format.js';
import { barChart, legend } from '../lib/charts.js';
import { colors, emptyState, info, kpi, pageHead, ring } from '../lib/ui.js';

const views = {
  day: { label: 'Day', period: 'hour' },
  week: { label: 'Week', period: 'day' },
  month: { label: 'Month', period: 'day' },
  year: { label: 'Year', period: 'month' },
};

function rangeFor(view, anchor) {
  const a = new Date(anchor);
  let from, to;
  if (view === 'day') {
    from = new Date(a.getFullYear(), a.getMonth(), a.getDate());
    to = new Date(a.getFullYear(), a.getMonth(), a.getDate() + 1);
  } else if (view === 'week') {
    const monday = (a.getDay() + 6) % 7;
    from = new Date(a.getFullYear(), a.getMonth(), a.getDate() - monday);
    to = new Date(from.getFullYear(), from.getMonth(), from.getDate() + 7);
  } else if (view === 'month') {
    from = new Date(a.getFullYear(), a.getMonth(), 1);
    to = new Date(a.getFullYear(), a.getMonth() + 1, 1);
  } else {
    from = new Date(a.getFullYear(), 0, 1);
    to = new Date(a.getFullYear() + 1, 0, 1);
  }
  return { from, to };
}

function shift(view, anchor, dir) {
  const a = new Date(anchor);
  if (view === 'day') a.setDate(a.getDate() + dir);
  else if (view === 'week') a.setDate(a.getDate() + 7 * dir);
  else if (view === 'month') a.setMonth(a.getMonth() + dir);
  else a.setFullYear(a.getFullYear() + dir);
  return a;
}

/** Every bucket start in the range, so empty periods still show. */
function slots(view, from, to) {
  const out = [];
  const d = new Date(from);
  while (d < to) {
    out.push(new Date(d));
    if (view === 'day') d.setHours(d.getHours() + 1);
    else if (view === 'year') d.setMonth(d.getMonth() + 1);
    else d.setDate(d.getDate() + 1);
  }
  return out;
}

function rangeLabel(view, from) {
  if (view === 'day') return from.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
  if (view === 'week') {
    const end = new Date(from.getFullYear(), from.getMonth(), from.getDate() + 6);
    return `${from.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })} – ${end.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })}`;
  }
  if (view === 'month') return from.toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
  return String(from.getFullYear());
}

export async function render(main) {
  const c = colors();
  let view = sessionStorage.getItem('energy.view') || 'day';
  let anchor = new Date();

  const tabs = h('div', { class: 'tabs', role: 'tablist' });
  const label = h('span', { class: 'small', style: { minWidth: '150px', textAlign: 'center', fontWeight: '500' } });
  const next = h('button', { class: 'btn sm icon-btn', 'aria-label': 'Next', 'data-tip': 'Next', onclick: () => { anchor = shift(view, anchor, 1); load(); } }, icon('chevronRight'));
  const controls = h('div', { class: 'row' },
    tabs,
    h('div', { class: 'row', style: { gap: '4px' } },
      h('button', { class: 'btn sm icon-btn', 'aria-label': 'Previous', 'data-tip': 'Previous', onclick: () => { anchor = shift(view, anchor, -1); load(); } }, icon('chevronLeft')),
      label, next,
      h('button', { class: 'btn sm', onclick: () => { anchor = new Date(); load(); } }, 'Today')));
  const totals = h('div', { class: 'stack' });
  const chart = h('div');
  main.append(
    pageHead('Energy', 'Where your energy came from and went'),
    h('div', { class: 'stack' },
      controls,
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Totals'), h('p', {}, 'For the selected period'))), totals),
      h('section', { class: 'card' }, h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Energy usage'), h('p', {}, 'Sources above the line, export below'))), chart)));

  const renderTabs = () => replace(tabs, Object.entries(views).map(([key, v]) =>
    h('button', { role: 'tab', 'aria-selected': String(key === view), onclick: () => { view = key; sessionStorage.setItem('energy.view', key); load(); } }, v.label)));

  async function load() {
    renderTabs();
    const { from, to } = rangeFor(view, anchor);
    label.textContent = rangeLabel(view, from);
    next.disabled = to > new Date() && from <= new Date();
    let data;
    try {
      data = await get(`/energy?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}&period=${views[view].period}`);
    } catch (err) {
      replace(totals, h('p', { class: 'muted' }, err.message));
      replace(chart);
      return;
    }
    const byTime = new Map(data.buckets.map((b) => [new Date(b.start).getTime(), b.energy_wh]));
    const sum = { pv: 0, grid_import: 0, grid_export: 0, genset: 0, load: 0 };
    const rows = slots(view, from, to).map((t) => {
      // Day/month buckets are aligned in site time; match on the nearest slot.
      let e = byTime.get(t.getTime());
      if (!e && view !== 'day') {
        for (const [k, v] of byTime) if (Math.abs(k - t.getTime()) < 14 * 3600 * 1000) { e = v; break; }
      }
      e = e || {};
      for (const k of Object.keys(sum)) sum[k] += e[k] || 0;
      const pv = e.pv || 0, exp = e.grid_export || 0;
      return {
        label: view === 'day' ? t.toLocaleTimeString(undefined, { hour: '2-digit' })
          : view === 'year' ? t.toLocaleDateString(undefined, { month: 'short' })
          : t.toLocaleDateString(undefined, view === 'week' ? { weekday: 'short' } : { day: 'numeric' }),
        title: view === 'day' ? `${t.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })} – ${new Date(t.getTime() + 3600000).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}`
          : t.toLocaleDateString(undefined, view === 'year' ? { month: 'long', year: 'numeric' } : { weekday: 'short', day: 'numeric', month: 'short' }),
        solar: Math.max(0, pv - exp),
        grid_import: e.grid_import || 0,
        genset: e.genset || 0,
        grid_export: exp,
      };
    });

    const hasGenset = sum.genset > 0;
    const selfConsumption = sum.pv > 0 ? Math.max(0, (sum.pv - sum.grid_export) / sum.pv) : null;
    const selfSufficiency = sum.load > 0 ? Math.max(0, Math.min(1, (sum.load - sum.grid_import - sum.genset) / sum.load)) : null;
    replace(totals,
      h('div', { class: 'kpis' },
        kpi('Solar produced', fmt.energy(sum.pv), c.solar),
        kpi('From grid', fmt.energy(sum.grid_import), c.grid),
        kpi('To grid', fmt.energy(sum.grid_export), c.export),
        hasGenset && kpi('Genset', fmt.energy(sum.genset), c.genset),
        kpi('Consumption', fmt.energy(sum.load), c.load)),
      h('div', { class: 'row', style: { gap: '28px' } },
        h('div', { class: 'ratio' }, ring(selfConsumption, c.solar),
          h('div', {}, h('b', {}, 'Self-consumption '), info('Solar energy used on site rather than exported, as a share of all solar produced.'),
            h('div', { class: 'muted small' }, `${fmt.energyText(sum.pv - sum.grid_export)} of ${fmt.energyText(sum.pv)} used on site`))),
        h('div', { class: 'ratio' }, ring(selfSufficiency, c.load),
          h('div', {}, h('b', {}, 'Self-sufficiency '), info('Share of consumption covered by solar instead of the grid or genset.'),
            h('div', { class: 'muted small' }, `${fmt.energyText(Math.max(0, sum.load - sum.grid_import - sum.genset))} of ${fmt.energyText(sum.load)} from solar`)))));

    const stacks = [
      { key: 'solar', label: 'Solar used', color: c.solar },
      { key: 'grid_import', label: 'From grid', color: c.grid },
      ...(hasGenset ? [{ key: 'genset', label: 'Genset', color: c.genset }] : []),
      { key: 'grid_export', label: 'To grid', color: c.export, negative: true },
    ];
    if (!rows.some((r) => r.solar || r.grid_import || r.genset || r.grid_export)) {
      replace(chart, emptyState('No energy recorded for this period yet.'));
      return;
    }
    replace(chart, barChart({ stacks, buckets: rows, height: 300, format: (v) => fmt.energyText(v) }), legend(stacks));
  }
  load();
}
