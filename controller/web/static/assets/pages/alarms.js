// Alarms: active conditions with acknowledgement.
import { h, replace } from '../lib/dom.js';
import { get, post } from '../lib/api.js';
import { icon } from '../lib/icons.js';
import * as fmt from '../lib/format.js';
import { emptyState, pageHead, pill, toast } from '../lib/ui.js';

export async function render(main, { can }) {
  const body = h('div');
  main.append(pageHead('Alarms', 'Active conditions. An alarm clears itself when the condition ends.'),
    h('section', { class: 'card' }, body));

  const load = async () => {
    let alarms;
    try { alarms = await get('/alarms'); } catch (err) { replace(body, h('p', { class: 'muted' }, err.message)); return; }
    if (!alarms.length) {
      replace(body, emptyState('No active alarms. Everything is running normally.'));
      return;
    }
    replace(body, h('div', { class: 'table-wrap', style: { marginTop: '-24px', borderTop: '0' } }, h('table', {},
      h('thead', {}, h('tr', {}, h('th', {}, 'Severity'), h('th', {}, 'Alarm'), h('th', {}, 'Device'), h('th', {}, 'Acknowledged'), h('th', {}))),
      h('tbody', {}, alarms.map((a) => h('tr', {},
        h('td', {}, a.critical ? pill('Critical', 'danger') : pill('Warning', 'warn')),
        h('td', {}, a.message),
        h('td', {}, a.device || '–'),
        h('td', { class: 'muted' }, a.acked ? `${a.acked.by}, ${fmt.relative(a.acked.time)}` : '–'),
        h('td', { class: 'num' }, can('alarms.ack') && !a.acked && h('button', {
          class: 'btn sm', onclick: async () => {
            try { await post('/alarms/ack', { key: a.key }); toast('Alarm acknowledged'); load(); } catch (err) { toast(err.message, 'error'); }
          },
        }, icon('check'), 'Acknowledge'))))))));
  };
  load();
  const timer = setInterval(load, 5000);
  return () => clearInterval(timer);
}
