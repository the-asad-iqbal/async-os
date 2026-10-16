// Shared UI pieces: toasts, dialogs, form fields, status pills.
import { h, s } from './dom.js';
import { icon } from './icons.js';

export function toast(message, kind = 'info') {
  const el = h('div', { class: ['toast', kind === 'error' && 'error'], role: kind === 'error' ? 'alert' : 'status' }, message);
  document.getElementById('toasts').append(el);
  setTimeout(() => el.remove(), kind === 'error' ? 6000 : 3500);
}

/**
 * Opens a modal form. `body` builds the fields; `onSubmit(form)` may throw
 * to show an error and keep the dialog open. Resolves true when submitted.
 */
export function formDialog({ title, description, body, submitLabel = 'Save', danger = false, onSubmit }) {
  return new Promise((resolve) => {
    const error = h('p', { class: 'error-text', role: 'alert' });
    const submit = h('button', { class: ['btn', danger ? 'danger solid' : 'primary'], type: 'submit' }, submitLabel);
    const cancel = h('button', { class: 'btn', type: 'button', onclick: () => close(false) }, 'Cancel');
    const form = h('form', { method: 'dialog', novalidate: true },
      h('div', {}, h('h2', {}, title), description && h('p', { class: 'muted small' }, description)),
      body,
      error,
      h('div', { class: 'actions' }, cancel, submit));
    const dialog = h('dialog', {}, form);
    let done = false;
    const close = (result) => {
      if (done) return;
      done = true;
      dialog.close();
      dialog.remove();
      resolve(result);
    };
    dialog.addEventListener('cancel', (e) => { e.preventDefault(); close(false); });
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      if (!form.reportValidity()) return;
      submit.disabled = true;
      error.textContent = '';
      try {
        await onSubmit?.(form);
        close(true);
      } catch (err) {
        error.textContent = err.message;
      } finally {
        submit.disabled = false;
      }
    });
    document.body.append(dialog);
    dialog.showModal();
    form.querySelector('input, select, textarea')?.focus();
  });
}

export function confirmDialog(title, description, { label = 'Confirm', danger = false } = {}) {
  return formDialog({ title, description, body: null, submitLabel: label, danger });
}

/** A labeled input. `unit` adds a suffix box. */
export function field(label, input, { hint, unit } = {}) {
  return h('label', { class: 'field' },
    h('span', {}, label),
    unit ? h('div', { class: 'input-unit' }, input, h('span', {}, unit)) : input,
    hint && h('small', {}, hint));
}

export function numberInput(name, value, { min, max, step = 'any', required = true } = {}) {
  return h('input', { type: 'number', name, value: value ?? '', min, max, step, required, inputmode: 'decimal' });
}

export function textInput(name, value, attrs = {}) {
  return h('input', { type: 'text', name, value: value ?? '', ...attrs });
}

export function select(name, options, value, attrs = {}) {
  return h('select', { name, ...attrs },
    options.map((o) => {
      if (o.group) return h('optgroup', { label: o.group }, o.options.map((x) => h('option', { value: x.value, selected: x.value === value }, x.label)));
      return h('option', { value: o.value, selected: o.value === value }, o.label);
    }));
}

export function checkbox(name, checked, label) {
  return h('label', { class: 'check' }, h('input', { type: 'checkbox', name, checked: !!checked }), label);
}

/** shadcn-style switch (a checkbox underneath). */
export function switchInput(name, checked, label) {
  return h('label', { class: 'switch' }, h('input', { type: 'checkbox', role: 'switch', name, checked: !!checked }), label);
}

export function pill(text, kind = '') {
  return h('span', { class: ['pill', kind] }, text);
}

export const statusInfo = {
  curtailing: { label: 'Curtailing', kind: 'info' },
  unlimited: { label: 'Full output', kind: 'ok' },
  failsafe: { label: 'Fail-safe', kind: 'danger' },
  monitoring: { label: 'Monitoring only', kind: 'warn' },
  no_pv: { label: 'No controllable PV', kind: 'warn' },
};

export function statusPill(status) {
  const info = statusInfo[status] || { label: 'Starting', kind: '' };
  return pill(info.label, info.kind);
}

export function banner(kind, children) {
  return h('div', { class: ['banner', kind], role: kind === 'danger' ? 'alert' : 'status' }, icon(kind === 'info' ? 'info' : 'alarm'), h('div', {}, children));
}

export function emptyState(text) {
  return h('div', { class: 'empty' }, text);
}

/** Circular percentage. */
export function ring(ratio, color) {
  const r = 30;
  const c = 2 * Math.PI * r;
  const v = ratio == null ? 0 : Math.max(0, Math.min(1, ratio));
  return s('svg', { class: 'ring', viewBox: '0 0 76 76' },
    s('circle', { class: 'track', cx: 38, cy: 38, r }),
    s('circle', { cx: 38, cy: 38, r, stroke: color, 'stroke-dasharray': `${c * v} ${c}`, 'stroke-linecap': 'round', transform: 'rotate(-90 38 38)' }),
    s('text', { x: 38, y: 38 }, ratio == null ? '–' : `${Math.round(v * 100)}%`));
}

/** Reads a form into an object, converting number inputs. */
export function formValues(form) {
  const out = {};
  for (const el of form.elements) {
    if (!el.name) continue;
    if (el.type === 'checkbox') out[el.name] = el.checked;
    else if (el.type === 'number') out[el.name] = el.value === '' ? null : Number(el.value);
    else out[el.name] = el.value;
  }
  return out;
}

/** Resolves a CSS custom property to its current value (for SVG and charts). */
export function cssVar(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

export const colors = () => ({
  solar: cssVar('--chart-solar'),
  grid: cssVar('--chart-grid'),
  export: cssVar('--chart-export'),
  genset: cssVar('--chart-genset'),
  load: cssVar('--chart-load'),
});

export function pageHead(title, description, ...actions) {
  return h('div', { class: 'page-head' },
    h('div', {}, h('h1', {}, title), description && h('p', {}, description)),
    actions.length ? h('div', { class: 'row' }, actions) : null);
}

export function card(title, description, ...children) {
  return h('section', { class: 'card' },
    (title || description) && h('div', { class: 'card-head' }, h('div', {}, title && h('h2', {}, title), description && h('p', {}, description))),
    children);
}

/** A KPI tile. */
export function kpi(label, valueParts, color) {
  return h('div', { class: 'kpi' },
    h('div', { class: 'label' }, color && h('span', { class: 'swatch', style: { background: color } }), label),
    h('div', { class: 'value' }, valueParts.value, valueParts.unit && h('small', {}, valueParts.unit)));
}

// Tooltips: any element with data-tip="text" shows a tooltip on hover or
// keyboard focus, and on tap for touch screens.
let tipEl = null;
function showTip(target) {
  const text = target.getAttribute('data-tip');
  if (!text) return;
  hideTip();
  tipEl = h('div', { class: 'tooltip', role: 'tooltip' }, text);
  document.body.append(tipEl);
  const r = target.getBoundingClientRect();
  const t = tipEl.getBoundingClientRect();
  let top = r.top - t.height - 8;
  if (top < 8) top = r.bottom + 8;
  const left = Math.min(window.innerWidth - t.width - 8, Math.max(8, r.left + r.width / 2 - t.width / 2));
  tipEl.style.top = `${top}px`;
  tipEl.style.left = `${left}px`;
}
function hideTip() {
  tipEl?.remove();
  tipEl = null;
}
document.addEventListener('pointerover', (e) => {
  if (e.pointerType === 'touch') return;
  const target = e.target.closest?.('[data-tip]');
  if (target) showTip(target); else hideTip();
});
document.addEventListener('focusin', (e) => {
  const target = e.target.closest?.('[data-tip]');
  if (target) showTip(target);
});
document.addEventListener('focusout', hideTip);
document.addEventListener('pointerdown', (e) => {
  const target = e.target.closest?.('[data-tip]');
  if (e.pointerType === 'touch' && target) {
    showTip(target);
    setTimeout(hideTip, 2500);
  } else if (!target) {
    hideTip();
  }
});
window.addEventListener('scroll', hideTip, { passive: true });

/** A small "i" with a tooltip, for explaining a label. */
export function info(text) {
  return h('span', { class: 'info-dot', 'data-tip': text, tabindex: 0, 'aria-label': text }, icon('info'));
}
