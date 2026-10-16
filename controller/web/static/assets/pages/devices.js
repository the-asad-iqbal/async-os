// Devices: live state of every meter and inverter.
import { h, replace } from '../lib/dom.js';
import * as live from '../lib/live.js';
import * as fmt from '../lib/format.js';
import { emptyState, pageHead, pill } from '../lib/ui.js';

const roleLabels = { grid_meter: 'Grid meter', genset_meter: 'Genset meter', source_meter: 'Grid / genset meter (ATS)', pv_inverter: 'PV inverter' };

const readingLabels = [
  ['power', 'Power', (v) => fmt.powerText(v)],
  ['voltage_l1', 'Voltage L1', (v) => `${fmt.number(v, 1)} V`],
  ['voltage_l2', 'Voltage L2', (v) => `${fmt.number(v, 1)} V`],
  ['voltage_l3', 'Voltage L3', (v) => `${fmt.number(v, 1)} V`],
  ['current_l1', 'Current L1', (v) => `${fmt.number(v, 2)} A`],
  ['frequency', 'Frequency', (v) => `${fmt.number(v, 2)} Hz`],
  ['power_factor', 'Power factor', (v) => fmt.number(v, 2)],
  ['dc_power', 'DC power', (v) => fmt.powerText(v)],
  ['energy_import', 'Imported', (v) => fmt.energyText(v * 1000)],
  ['energy_export', 'Exported', (v) => fmt.energyText(v * 1000)],
  ['energy_total', 'Lifetime yield', (v) => fmt.energyText(v * 1000)],
  ['power_limit', 'Limit read back', (v) => `${fmt.number(v, 1)} %`],
  ['running', 'Running signal', (v) => (v ? 'On' : 'Off')],
];

export async function render(main, { can, navigate }) {
  const grid = h('div', { class: 'grid cols-3' });
  main.append(pageHead('Devices', 'Live readings from every meter and inverter',
    can('site.configure') && h('a', { class: 'btn', href: '/site', 'data-link': true }, 'Configure devices')), grid);

  const unsubscribe = live.subscribe((snap) => {
    const devices = snap.devices || [];
    if (!devices.length) {
      replace(grid, h('div', { class: 'card span-2' }, emptyState('No devices configured yet.')));
      return;
    }
    replace(grid, devices.map((d) => deviceCard(d)));
  });
  return unsubscribe;
}

/** The ATS meter's energy, split by source. */
function sourceTotals(t) {
  const kwh = (v) => `${fmt.number(v, v < 100 ? 2 : 0)} kWh`;
  return h('div', { class: 'stack', style: { gap: '6px', marginTop: '8px' } },
    h('small', { class: 'muted', 'data-tip': 'Counter increments go to the source that was live. While the genset runs, live power is integrated and tops up anything the counters missed.' }, 'Energy by source'),
    h('div', { class: 'readings' },
      [['Grid import', t.grid_import_kwh], ['Grid export', t.grid_export_kwh], ['Genset output', t.genset_import_kwh], ['Genset reverse', t.genset_export_kwh]]
        .map(([label, v]) => h('div', {}, h('small', {}, label), h('b', {}, kwh(v ?? 0))))));
}

function deviceCard(d) {
  const state = !d.enabled ? pill('Disabled', 'plain') : d.online ? pill('Online', 'ok') : pill('Offline', 'danger');
  const readings = readingLabels.filter(([key]) => d.reading?.[key] != null).slice(0, 9);
  return h('section', { class: 'card device-card' },
    h('div', { class: 'card-head' },
      h('div', {}, h('h2', {}, d.name), h('p', {}, `${roleLabels[d.role] || d.role} · ${d.manufacturer} ${d.model}`)),
      state),
    h('div', { class: 'row' },
      d.verified
        ? pill('Verified profile', 'ok')
        : h('span', { class: 'pill warn', 'data-tip': 'This register map has not been checked against real hardware yet. Compare readings with the device display during commissioning.' }, 'Unverified profile'),
      d.role === 'source_meter' && d.source && (d.source === 'genset' ? pill('On genset', 'warn') : pill('On grid', 'info')),
      d.role === 'pv_inverter' && (d.controllable ? pill('Controllable', 'info') : pill('Read only', 'plain')),
      d.rated_w ? h('span', { class: 'muted small' }, `Rated ${fmt.powerText(d.rated_w)}`) : null),
    readings.length
      ? h('div', { class: 'readings' }, readings.map(([key, label, f]) => h('div', {}, h('small', {}, label), h('b', {}, f(d.reading[key])))))
      : h('p', { class: 'muted small' }, d.enabled ? 'No readings yet.' : 'This device is disabled.'),
    d.source_totals && sourceTotals(d.source_totals),
    (d.error || d.write_error || d.limit_pct != null) && h('div', { class: 'card-foot' },
      d.error ? h('span', { class: 'error-text' }, d.error)
        : d.write_error ? h('span', { class: 'error-text' }, `Limit write failed: ${d.write_error}`)
        : h('span', {}, `Last limit sent: ${fmt.number(d.limit_pct, 1)} %`),
      h('span', { class: 'spacer' }),
      d.updated && h('span', {}, fmt.relative(d.updated))));
}
