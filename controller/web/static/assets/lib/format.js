// Number and time formatting.

const nf = (digits) => new Intl.NumberFormat(undefined, { maximumFractionDigits: digits, minimumFractionDigits: digits });

/** Power in W -> "12.4 kW" (split into value and unit). */
export function power(w) {
  if (w == null || Number.isNaN(w)) return { value: '–', unit: '' };
  const abs = Math.abs(w);
  if (abs >= 1e6) return { value: nf(2).format(w / 1e6), unit: 'MW' };
  if (abs >= 1e3) return { value: nf(abs >= 1e5 ? 0 : 1).format(w / 1e3), unit: 'kW' };
  return { value: nf(0).format(w), unit: 'W' };
}

export function powerText(w) {
  const p = power(w);
  return p.unit ? `${p.value} ${p.unit}` : p.value;
}

/** Energy in Wh -> "12.4 kWh". */
export function energy(wh) {
  if (wh == null || Number.isNaN(wh)) return { value: '–', unit: '' };
  const abs = Math.abs(wh);
  if (abs >= 1e6) return { value: nf(2).format(wh / 1e6), unit: 'MWh' };
  if (abs >= 1e3) return { value: nf(abs >= 1e5 ? 0 : 1).format(wh / 1e3), unit: 'kWh' };
  return { value: nf(0).format(wh), unit: 'Wh' };
}

export function energyText(wh) {
  const e = energy(wh);
  return e.unit ? `${e.value} ${e.unit}` : e.value;
}

export function percent(ratio, digits = 0) {
  if (ratio == null || Number.isNaN(ratio)) return '–';
  return `${nf(digits).format(ratio * 100)} %`;
}

export function number(v, digits = 1) {
  if (v == null || Number.isNaN(v)) return '–';
  return nf(digits).format(v);
}

export function dateTime(iso) {
  if (!iso) return '–';
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

export function time(iso) {
  return new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function relative(iso) {
  if (!iso) return 'never';
  const seconds = Math.round((Date.now() - new Date(iso)) / 1000);
  if (seconds < 5) return 'just now';
  if (seconds < 60) return `${seconds} s ago`;
  if (seconds < 3600) return `${Math.round(seconds / 60)} min ago`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)} h ago`;
  return dateTime(iso);
}

export function duration(seconds) {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return d ? `${d} d ${h} h` : h ? `${h} h ${m} min` : `${m} min`;
}

export function titleCase(text) {
  return String(text).replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase());
}
