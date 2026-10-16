// Control settings: what the controller protects and how it reacts.
import { h, replace } from '../lib/dom.js';
import { get, put } from '../lib/api.js';
import * as fmt from '../lib/format.js';
import { card, confirmDialog, field, info, numberInput, pageHead, pill, select, switchInput, toast } from '../lib/ui.js';

// [key, label, unit, help, toForm, fromForm, attrs]
const kw = { to: (w) => w / 1000, from: (v) => Math.round(v * 1000) };
const sec = { to: (d) => parseDuration(d), from: (v) => `${v}s` };
const same = { to: (v) => v, from: (v) => v };

const groups = [
  ['Grid', 'Export limiting at the grid connection', [
    ['export_limit_w', 'Export limit', 'kW', 'The most power the site may send to the grid. 0 means zero export.', kw, { min: 0, step: 0.1 }],
    ['grid_margin_w', 'Safety margin', 'kW', 'Kept in reserve below the export limit so sudden load drops do not breach it.', kw, { min: 0, step: 0.05 }],
    ['grid_present_min_voltage', 'Grid present above', 'V', 'The grid counts as available when the grid meter reads at least this voltage. 0 assumes it is available whenever the meter answers.', same, { min: 0, step: 1 }],
    ['export_alarm_delay', 'Export alarm after', 's', 'How long export may stay above the limit before a critical alarm is raised.', sec, { min: 1, step: 1 }],
  ]],
  ['Genset', 'Minimum load protection while gensets run', [
    ['genset_min_load_pct', 'Minimum load', '%', 'Lowest load per running genset, as a share of its rating. A per-genset kW floor in Site setup overrides this.', same, { min: 0, max: 90, step: 1 }],
    ['genset_margin_w', 'Safety margin', 'kW', 'Kept in reserve above the genset minimum load.', kw, { min: 0, step: 0.1 }],
    ['genset_running_min_w', 'Running above', 'kW', 'Without a running signal, a genset counts as running when it delivers more than this.', kw, { min: 0, step: 0.1 }],
    ['source_debounce', 'Source change debounce', 's', 'A genset starting or stopping must hold this long before the controller reacts, so an ATS transfer does not whipsaw the inverters.', sec, { min: 0, max: 30, step: 0.5 }],
  ]],
  ['Inverters', 'How limits are sent', [
    ['ramp_up_pct_per_sec', 'Ramp-up rate', '%/s', 'How fast PV may rise again, as a share of controllable rating per second. Cuts are always immediate.', same, { min: 0.1, max: 100, step: 0.1 }],
    ['min_limit_pct', 'Minimum limit', '%', 'Never limit inverters below this in normal operation.', same, { min: 0, max: 99, step: 1 }],
    ['max_limit_pct', 'Maximum limit', '%', 'Never allow inverters above this, e.g. a contractual cap.', same, { min: 1, max: 100, step: 1 }],
    ['min_step_pct', 'Minimum change', '%', 'Smaller increases are not sent, to spare inverters from constant writes. Breaches are always sent.', same, { min: 0, max: 10, step: 0.1 }],
    ['keepalive_interval', 'Re-send every', 's', 'The current limit is re-sent at this interval even when unchanged.', sec, { min: 5, step: 1 }],
  ]],
  ['Fail-safe', 'When a required meter stops answering', [
    ['measurement_timeout', 'Reading counts as stale after', 's', 'A meter reading older than this triggers the fail-safe.', sec, { min: 1, max: 60, step: 1 }],
    ['failsafe_limit_pct', 'Fail-safe limit', '%', 'Limit applied by the "curtail" action.', same, { min: 0, max: 100, step: 1 }],
  ]],
];

function parseDuration(text) {
  const m = /^(?:(\d+)h)?(?:(\d+)m(?!s))?(?:([\d.]+)s)?(?:([\d.]+)ms)?$/.exec(text || '');
  if (!m) return 0;
  return (Number(m[1] || 0) * 3600) + (Number(m[2] || 0) * 60) + Number(m[3] || 0) + Number(m[4] || 0) / 1000;
}

export async function render(main, { can }) {
  const site = await get('/site');
  const settings = site.control;
  const editable = can('control.configure');

  main.append(pageHead('Control', editable ? 'How the controller protects the site' : 'Current control settings (read only)'));

  if (can('control.setpoint') && !editable) {
    const form = h('form', { class: 'row', style: { alignItems: 'flex-end' } },
      field('Export limit', numberInput('limit', settings.export_limit_w / 1000, { min: 0, max: site.operator_export_limit_max_w / 1000, step: 0.1 }),
        { unit: 'kW', hint: `You may set 0 to ${fmt.number(site.operator_export_limit_max_w / 1000, 1)} kW.` }),
      h('button', { class: 'btn primary', type: 'submit' }, 'Apply'));
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      try { await put('/site/export-limit', { export_limit_w: Math.round(Number(form.limit.value) * 1000) }); toast('Export limit updated'); } catch (err) { toast(err.message, 'error'); }
    });
    main.append(card('Export limit', 'The setpoint operators may adjust', form), h('div', { style: { height: '16px' } }));
  }

  const form = h('form', { class: 'stack' });
  const mode = switchInput('mode', settings.mode === 'control', 'Actively limit PV');
  const failsafeAction = select('failsafe_action', [
    { value: 'limit', label: 'Curtail to the fail-safe limit' },
    { value: 'hold', label: 'Hold the last limit' },
  ], settings.failsafe_action);
  form.append(card('Mode', 'In monitoring mode the controller records everything but never writes to inverters.',
    h('div', { class: 'row' }, mode, settings.mode === 'control' ? pill('Control on', 'ok') : pill('Monitoring only', 'warn'))));

  for (const [title, description, fields] of groups) {
    const grid = h('div', { class: 'form-grid' });
    for (const [key, label, unit, help, conv, attrs] of fields) {
      grid.append(field(h('span', {}, label, ' ', info(help)), numberInput(key, round(conv.to(settings[key])), attrs), { unit }));
    }
    if (title === 'Fail-safe') grid.prepend(field(h('span', {}, 'Action ', info('Curtailing is the safe choice when export must never exceed the limit. Holding avoids lost production when a meter drops out briefly.')), failsafeAction));
    form.append(card(title, description, grid));
  }

  if (editable) {
    form.append(h('div', { class: 'row' }, h('button', { class: 'btn primary', type: 'submit' }, 'Save settings'), h('button', { class: 'btn', type: 'reset' }, 'Reset')));
  } else {
    for (const el of form.elements) el.disabled = true;
  }

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!form.reportValidity()) return;
    const next = { ...settings, mode: form.mode.checked ? 'control' : 'monitor', failsafe_action: form.failsafe_action.value };
    for (const [, , fields] of groups) for (const [key, , , , conv] of fields) next[key] = conv.from(Number(form[key].value));
    if (next.mode === 'control' && settings.mode !== 'control') {
      const ok = await confirmDialog('Start controlling the inverters?',
        'The controller will start writing power limits to every controllable inverter. Make sure meters read correctly on the Devices page first.',
        { label: 'Start control' });
      if (!ok) return;
    }
    try {
      Object.assign(settings, await put('/site/control', next));
      toast('Control settings saved');
    } catch (err) {
      toast(err.message, 'error');
    }
  });
  main.append(form);
}

function round(v) {
  return Math.round(v * 1000) / 1000;
}
