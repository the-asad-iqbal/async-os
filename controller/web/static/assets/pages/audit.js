// Audit log: who did what, when and from where.
import { h, replace, append } from '../lib/dom.js';
import { get } from '../lib/api.js';
import * as fmt from '../lib/format.js';
import { emptyState, pageHead, pill } from '../lib/ui.js';

export async function render(main) {
  const tbody = h('tbody');
  const more = h('button', { class: 'btn', onclick: () => load() }, 'Load older entries');
  const wrap = h('section', { class: 'card' },
    h('div', { class: 'table-wrap', style: { marginTop: '-24px', borderTop: '0' } }, h('table', {},
      h('thead', {}, h('tr', {}, h('th', {}, 'Time'), h('th', {}, 'User'), h('th', {}, 'Action'), h('th', {}, 'Target'), h('th', {}, 'Result'), h('th', {}, 'Address'))),
      tbody)));
  main.append(pageHead('Audit log', 'Every sign-in and change, newest first'), wrap, h('div', { class: 'row', style: { justifyContent: 'center', marginTop: '16px' } }, more));

  let before = 0;
  async function load() {
    more.disabled = true;
    const entries = await get(`/audit?limit=50${before ? `&before=${before}` : ''}`);
    if (!entries.length && !before) replace(tbody, h('tr', {}, h('td', { colspan: 6 }, emptyState('Nothing recorded yet.'))));
    append(tbody, entries.map((e) => h('tr', {},
      h('td', { class: 'muted' }, fmt.dateTime(e.time)),
      h('td', {}, e.username || '–', e.role && h('span', { class: 'muted small' }, ` · ${e.role}`)),
      h('td', {}, h('code', {}, e.action)),
      h('td', { 'data-tip': e.details ? JSON.stringify(e.details) : null }, e.target || '–'),
      h('td', {}, e.success ? pill('OK', 'ok') : h('span', { class: 'pill danger', 'data-tip': e.error || '' }, 'Failed')),
      h('td', { class: 'muted mono' }, e.ip || '–'))));
    if (entries.length) before = entries.at(-1).id;
    more.disabled = entries.length < 50;
    more.textContent = entries.length < 50 ? 'No older entries' : 'Load older entries';
  }
  load();
}
