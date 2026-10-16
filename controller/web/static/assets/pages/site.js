// Site setup: buses, devices and commissioning tools (installers).
import { h, replace } from '../lib/dom.js';
import { get, post, put } from '../lib/api.js';
import { icon } from '../lib/icons.js';
import * as fmt from '../lib/format.js';
import { card, checkbox, confirmDialog, emptyState, field, formDialog, info, numberInput, pageHead, pill, select, textInput, toast } from '../lib/ui.js';

const transports = [
  { value: 'tcp', label: 'Modbus TCP' },
  { value: 'rtuovertcp', label: 'Modbus RTU over TCP (RS-485 gateway)' },
  { value: 'rtu', label: 'Modbus RTU (RS-485 serial port)' },
];
const roles = [
  { value: 'grid_meter', label: 'Grid meter', kind: 'meter' },
  { value: 'genset_meter', label: 'Genset meter', kind: 'meter' },
  { value: 'pv_inverter', label: 'PV inverter', kind: 'inverter' },
];

export async function render(main) {
  const [site, profiles] = await Promise.all([get('/site'), get('/profiles')]);
  const draft = structuredClone(site);
  let dirty = false;

  const saveBar = h('div', { class: 'row' });
  const details = h('div');
  const buses = h('div');
  const devices = h('div');
  const reader = h('div');
  main.append(
    pageHead('Site setup', 'Meters, inverters and how the controller reaches them', saveBar),
    h('div', { class: 'stack' }, details, buses, devices, reader));

  const markDirty = () => { dirty = true; renderSaveBar(); };
  const profileById = (id) => profiles.find((p) => p.id === id);
  const busName = (id) => draft.buses.find((b) => b.id === id)?.name || id;

  function renderSaveBar() {
    replace(saveBar,
      dirty && pill('Unsaved changes', 'warn'),
      h('button', { class: 'btn', disabled: !dirty, onclick: () => { Object.assign(draft, structuredClone(site)); dirty = false; renderAll(); } }, 'Discard'),
      h('button', { class: 'btn primary', disabled: !dirty, onclick: save }, 'Save and apply'));
  }

  async function save() {
    try {
      const saved = await put('/site', {
        name: draft.name, timezone: draft.timezone, buses: draft.buses, devices: draft.devices,
        operator_export_limit_max_w: draft.operator_export_limit_max_w,
      });
      Object.assign(site, structuredClone(saved));
      dirty = false;
      renderAll();
      toast('Site configuration applied');
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  function renderDetails() {
    const zones = Intl.supportedValuesOf?.('timeZone') || [draft.timezone];
    const form = h('div', { class: 'form-grid' },
      field('Site name', textInput('name', draft.name, { required: true, maxlength: 80, oninput: (e) => { draft.name = e.target.value; markDirty(); } })),
      field('Time zone', select('timezone', [...new Set(['UTC', ...zones])].map((z) => ({ value: z, label: z })), draft.timezone,
        { onchange: (e) => { draft.timezone = e.target.value; markDirty(); } }), { hint: 'Days and months in energy reports follow this zone.' }),
      field(h('span', {}, 'Operator export limit up to ', info('Operators may move the export limit between 0 and this value. Installers and owners are not bound by it.')),
        numberInput('op_max', draft.operator_export_limit_max_w / 1000, { min: 0, step: 0.1, oninput: (e) => { draft.operator_export_limit_max_w = Math.round(Number(e.target.value) * 1000); markDirty(); } }), { unit: 'kW' }));
    replace(details, card('Site', null, form));
  }

  function renderBuses() {
    const body = draft.buses.length
      ? h('div', { class: 'table-wrap' }, h('table', {},
        h('thead', {}, h('tr', {}, h('th', {}, 'Name'), h('th', {}, 'Type'), h('th', {}, 'Address'), h('th', {}, 'Devices'), h('th', {}))),
        h('tbody', {}, draft.buses.map((b, i) => h('tr', {},
          h('td', {}, h('b', {}, b.name), h('div', { class: 'muted small mono' }, b.id)),
          h('td', {}, transports.find((t) => t.value === b.transport)?.label),
          h('td', { class: 'mono' }, b.transport === 'rtu' ? `${b.device} · ${b.baud_rate} ${8}${b.parity}${b.stop_bits}` : `${b.host}:${b.port}`),
          h('td', {}, String(draft.devices.filter((d) => d.bus_id === b.id).length)),
          h('td', { class: 'num' }, h('div', { class: 'row', style: { justifyContent: 'flex-end', gap: '4px' } },
            h('button', { class: 'btn sm ghost icon-btn', 'aria-label': 'Edit', 'data-tip': 'Edit bus', onclick: () => editBus(i) }, icon('edit')),
            h('button', {
              class: 'btn sm ghost icon-btn danger', 'aria-label': 'Remove', 'data-tip': 'Remove bus',
              onclick: async () => {
                if (draft.devices.some((d) => d.bus_id === b.id)) return toast('Move or remove the devices on this bus first.', 'error');
                draft.buses.splice(i, 1); markDirty(); renderBuses();
              },
            }, icon('trash')))))))))
      : emptyState('No buses yet. Add the Modbus connection your devices sit on.');
    replace(buses, h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Communication'), h('p', {}, 'Modbus TCP, RS-485 gateways and serial ports')),
        h('button', { class: 'btn', onclick: () => editBus(-1) }, icon('plus'), 'Add bus')),
      body));
  }

  function busForm(b) {
    const serial = h('div', { class: 'form-grid' },
      field('Serial port', textInput('device', b.device || '/dev/ttyUSB0', { placeholder: '/dev/ttyUSB0' }), { hint: '/dev/tty* or /dev/serial/by-id/…' }),
      field('Baud rate', select('baud_rate', [1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200].map((v) => ({ value: String(v), label: String(v) })), String(b.baud_rate || 9600))),
      field('Parity', select('parity', [{ value: 'N', label: 'None' }, { value: 'E', label: 'Even' }, { value: 'O', label: 'Odd' }], b.parity || 'N')),
      field('Stop bits', select('stop_bits', [{ value: '1', label: '1' }, { value: '2', label: '2' }], String(b.stop_bits || 1))));
    const network = h('div', { class: 'form-grid' },
      field('Host', textInput('host', b.host || '', { placeholder: '192.168.1.50' })),
      field('Port', numberInput('port', b.port || 502, { min: 1, max: 65535, step: 1, required: false })));
    const transport = select('transport', transports, b.transport || 'tcp');
    const sync = () => { serial.hidden = transport.value !== 'rtu'; network.hidden = transport.value === 'rtu'; };
    transport.addEventListener('change', sync);
    sync();
    return h('div', { class: 'stack' },
      h('div', { class: 'form-grid' },
        field('Name', textInput('name', b.name || '', { required: true, placeholder: 'Main RS-485 line' })),
        field('ID', textInput('id', b.id || '', { required: true, pattern: '[a-z0-9][a-z0-9_\\-]{0,31}', placeholder: 'rs485-1' }), { hint: 'Lowercase, used in configuration' })),
      field('Type', transport),
      network, serial,
      field('Timeout', numberInput('timeout_ms', b.timeout_ms || 1000, { min: 100, max: 10000, step: 50 }), { unit: 'ms' }));
  }

  function readBus(f) {
    const rtu = f.transport.value === 'rtu';
    return {
      id: f.id.value.trim(), name: f.name.value.trim(), transport: f.transport.value,
      host: rtu ? '' : f.host.value.trim(), port: rtu ? 0 : Number(f.port.value),
      device: rtu ? f.device.value.trim() : '', baud_rate: rtu ? Number(f.baud_rate.value) : 0,
      parity: rtu ? f.parity.value : '', stop_bits: rtu ? Number(f.stop_bits.value) : 0,
      timeout_ms: Number(f.timeout_ms.value),
    };
  }

  async function editBus(i) {
    const current = i >= 0 ? draft.buses[i] : {};
    await formDialog({
      title: i >= 0 ? `Edit ${current.name}` : 'Add bus',
      body: busForm(current),
      submitLabel: i >= 0 ? 'Update' : 'Add',
      onSubmit: (f) => {
        const b = readBus(f);
        if (draft.buses.some((x, j) => x.id === b.id && j !== i)) throw new Error('Another bus already uses this ID.');
        if (i >= 0) {
          for (const d of draft.devices) if (d.bus_id === current.id) d.bus_id = b.id;
          draft.buses[i] = b;
        } else {
          draft.buses.push(b);
        }
        markDirty();
        renderBuses();
        renderDevices();
      },
    });
  }

  function renderDevices() {
    const body = draft.devices.length
      ? h('div', { class: 'table-wrap' }, h('table', {},
        h('thead', {}, h('tr', {}, h('th', {}, 'Device'), h('th', {}, 'Role'), h('th', {}, 'Profile'), h('th', {}, 'Bus · unit'), h('th', {}, 'Rating'), h('th', {}, 'Enabled'), h('th', {}))),
        h('tbody', {}, draft.devices.map((d, i) => {
          const p = profileById(d.profile);
          return h('tr', {},
            h('td', {}, h('b', {}, d.name), h('div', { class: 'muted small mono' }, d.id)),
            h('td', {}, roles.find((r) => r.value === d.role)?.label),
            h('td', {}, p ? `${p.manufacturer} ${p.model}` : d.profile, ' ', p && !p.verified && h('span', { class: 'pill warn plain', 'data-tip': 'Register map not yet verified on hardware' }, 'unverified')),
            h('td', { class: 'mono' }, `${busName(d.bus_id)} · ${d.unit_id}`),
            h('td', {}, d.rated_w ? fmt.powerText(d.rated_w) : '–'),
            h('td', {}, d.enabled ? pill('Yes', 'ok') : pill('No', 'plain')),
            h('td', { class: 'num' }, h('div', { class: 'row', style: { justifyContent: 'flex-end', gap: '4px' } },
              h('button', { class: 'btn sm ghost icon-btn', 'aria-label': 'Edit', 'data-tip': 'Edit device', onclick: () => editDevice(i) }, icon('edit')),
              h('button', {
                class: 'btn sm ghost icon-btn danger', 'aria-label': 'Remove', 'data-tip': 'Remove device',
                onclick: async () => {
                  if (!await confirmDialog(`Remove ${d.name}?`, 'It stops being read and controlled once you save.', { label: 'Remove', danger: true })) return;
                  draft.devices.splice(i, 1); markDirty(); renderDevices();
                },
              }, icon('trash')))));
        }))))
      : emptyState(draft.buses.length ? 'No devices yet. Add your grid meter, gensets and inverters.' : 'Add a bus first.');
    replace(devices, h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('div', {}, h('h2', {}, 'Devices'), h('p', {}, `${profiles.length} profiles in the library`)),
        h('button', { class: 'btn', disabled: !draft.buses.length, onclick: () => editDevice(-1) }, icon('plus'), 'Add device')),
      body));
  }

  function profileOptions(kind) {
    const groups = new Map();
    for (const p of profiles.filter((x) => x.kind === kind)) {
      if (!groups.has(p.manufacturer)) groups.set(p.manufacturer, []);
      groups.get(p.manufacturer).push({ value: p.id, label: `${p.model}${p.verified ? '' : ' (unverified)'}` });
    }
    return [...groups].map(([group, options]) => ({ group, options }));
  }

  async function editDevice(i) {
    const d = i >= 0 ? structuredClone(draft.devices[i]) : { enabled: true, role: 'pv_inverter', unit_id: 1, bus_id: draft.buses[0].id };
    const role = select('role', roles, d.role);
    const profile = h('select', { name: 'profile', required: true });
    const notes = h('p', { class: 'muted small', style: { margin: 0 } });
    const gensetFields = h('div', { class: 'form-grid' },
      field(h('span', {}, 'Minimum load ', info('Optional. Overrides the site-wide percentage for this genset.')), numberInput('min_load_kw', d.min_load_w ? d.min_load_w / 1000 : '', { min: 0, step: 0.5, required: false }), { unit: 'kW' }),
      field(h('span', {}, 'Running signal register ', info('Optional. A register on this device that reports the genset running, e.g. a digital input wired to the genset or ATS. Leave empty to infer from power.')),
        numberInput('sig_address', d.running_signal?.address ?? '', { min: 0, max: 65535, step: 1, required: false })),
      field('Signal table', select('sig_table', [{ value: 'holding', label: 'Holding' }, { value: 'input', label: 'Input' }], d.running_signal?.table || 'holding')),
      field('Signal bit', numberInput('sig_bit', d.running_signal?.bit ?? 0, { min: 0, max: 15, step: 1, required: false })));
    const ratingHint = h('small');
    const testResult = h('div', { class: 'small' });

    const syncRole = () => {
      const kind = roles.find((r) => r.value === role.value).kind;
      replace(profile, profileOptions(kind).map((g) => h('optgroup', { label: g.group }, g.options.map((o) => h('option', { value: o.value, selected: o.value === d.profile }, o.label)))));
      gensetFields.hidden = role.value !== 'genset_meter';
      ratingHint.textContent = role.value === 'genset_meter' ? 'Required: the genset’s rated active power.' : 'Only needed if the inverter does not report its rating.';
      syncProfile();
    };
    const syncProfile = () => {
      const p = profileById(profile.value);
      notes.textContent = p ? `${p.verified ? '' : 'Unverified register map. '}${p.notes || ''} Source: ${p.source}` : '';
    };
    role.addEventListener('change', syncRole);
    profile.addEventListener('change', syncProfile);

    const readDevice = (f) => {
      const out = {
        id: f.id.value.trim(), name: f.name.value.trim(), profile: f.profile.value, role: f.role.value,
        bus_id: f.bus_id.value, unit_id: Number(f.unit_id.value), enabled: f.enabled.checked,
        invert_power: f.invert_power.checked, rated_w: f.rated_kw.value ? Math.round(Number(f.rated_kw.value) * 1000) : 0,
        poll_interval_ms: Number(f.poll_interval_ms.value) || 0,
      };
      if (out.role === 'genset_meter') {
        if (f.min_load_kw.value) out.min_load_w = Math.round(Number(f.min_load_kw.value) * 1000);
        if (f.sig_address.value !== '') out.running_signal = { table: f.sig_table.value, address: Number(f.sig_address.value), type: 'uint16', bit: Number(f.sig_bit.value || 0) };
      }
      return out;
    };

    const body = h('div', { class: 'stack' },
      h('div', { class: 'form-grid' },
        field('Name', textInput('name', d.name || '', { required: true, placeholder: 'Inverter 1' })),
        field('ID', textInput('id', d.id || '', { required: true, pattern: '[a-z0-9][a-z0-9_\\-]{0,31}', placeholder: 'inv-1' })),
        field('Role', role),
        field('Profile', profile)),
      notes,
      h('div', { class: 'form-grid' },
        field('Bus', select('bus_id', draft.buses.map((b) => ({ value: b.id, label: b.name })), d.bus_id)),
        field('Unit ID', numberInput('unit_id', d.unit_id, { min: 1, max: 247, step: 1 })),
        field('Rating', numberInput('rated_kw', d.rated_w ? d.rated_w / 1000 : '', { min: 0, step: 0.1, required: false }), { unit: 'kW' }),
        field('Poll interval', numberInput('poll_interval_ms', d.poll_interval_ms || 1000, { min: 200, max: 60000, step: 100, required: false }), { unit: 'ms' })),
      ratingHint,
      gensetFields,
      h('div', { class: 'row' },
        checkbox('enabled', d.enabled, 'Enabled'),
        h('span', { 'data-tip': 'Flip the sign if the meter reports export as positive (or genset output as negative).' }, checkbox('invert_power', d.invert_power, 'Invert power sign'))),
      h('div', { class: 'row' },
        h('button', {
          class: 'btn', type: 'button', onclick: async (e) => {
            const f = e.target.closest('form');
            const dev = readDevice(f);
            const bus = draft.buses.find((b) => b.id === dev.bus_id);
            replace(testResult, h('span', { class: 'muted' }, 'Reading…'));
            try {
              const res = await post('/commissioning/probe', { bus, device: dev });
              const r = res.reading;
              replace(testResult, h('div', { class: 'stack', style: { gap: '4px' } },
                h('span', {}, pill('Device answered', 'ok'), ' ', res.controllable ? pill('Power limit available', 'info') : null),
                h('code', {}, Object.entries(r).map(([k, v]) => `${k}=${fmt.number(v, 2)}`).join('  '))));
            } catch (err) {
              replace(testResult, h('span', { class: 'error-text' }, err.message));
            }
          },
        }, icon('refresh'), 'Test connection'), testResult));
    syncRole();

    await formDialog({
      title: i >= 0 ? `Edit ${d.name}` : 'Add device',
      body,
      submitLabel: i >= 0 ? 'Update' : 'Add',
      onSubmit: (f) => {
        const dev = readDevice(f);
        if (draft.devices.some((x, j) => x.id === dev.id && j !== i)) throw new Error('Another device already uses this ID.');
        if (dev.role === 'genset_meter' && !dev.rated_w) throw new Error('Enter the genset rating.');
        if (i >= 0) draft.devices[i] = dev; else draft.devices.push(dev);
        markDirty();
        renderBuses();
        renderDevices();
      },
    });
  }

  function renderReader() {
    const out = h('div');
    const form = h('form', { class: 'form-grid' },
      field('Bus', select('bus', draft.buses.map((b) => ({ value: b.id, label: b.name })), draft.buses[0]?.id)),
      field('Unit ID', numberInput('unit', 1, { min: 1, max: 247, step: 1 })),
      field('Table', select('table', [{ value: 'holding', label: 'Holding (function 3)' }, { value: 'input', label: 'Input (function 4)' }], 'holding')),
      field('Address', numberInput('address', 0, { min: 0, max: 65535, step: 1 }), { hint: 'Zero-based: register 40001 or 30001 is address 0' }),
      field('Count', numberInput('quantity', 10, { min: 1, max: 125, step: 1 })),
      h('div', { style: { alignSelf: 'end' } }, h('button', { class: 'btn primary', type: 'submit', disabled: !draft.buses.length }, 'Read registers')));
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const bus = draft.buses.find((b) => b.id === form.bus.value);
      try {
        const res = await post('/commissioning/read', { bus, unit_id: Number(form.unit.value), table: form.table.value, address: Number(form.address.value), quantity: Number(form.quantity.value) });
        const start = Number(form.address.value);
        replace(out, h('div', { class: 'table-wrap' }, h('table', {},
          h('thead', {}, h('tr', {}, h('th', {}, 'Address'), h('th', { class: 'num' }, 'Unsigned'), h('th', { class: 'num' }, 'Signed'), h('th', { class: 'num' }, 'Hex'))),
          h('tbody', {}, res.values.map((v, k) => h('tr', {},
            h('td', { class: 'mono' }, String(start + k)),
            h('td', { class: 'num mono' }, String(v)),
            h('td', { class: 'num mono' }, String(v > 32767 ? v - 65536 : v)),
            h('td', { class: 'num mono' }, `0x${v.toString(16).padStart(4, '0').toUpperCase()}`)))))));
      } catch (err) {
        replace(out, h('p', { class: 'error-text' }, err.message));
      }
    });
    replace(reader, card('Register reader', 'Read raw registers from any device, for commissioning a new model. Uses saved buses.', form, out));
  }

  function renderAll() {
    renderSaveBar();
    renderDetails();
    renderBuses();
    renderDevices();
    renderReader();
  }
  renderAll();

  const guard = (e) => { if (dirty) { e.preventDefault(); e.returnValue = ''; } };
  window.addEventListener('beforeunload', guard);
  return () => window.removeEventListener('beforeunload', guard);
}
