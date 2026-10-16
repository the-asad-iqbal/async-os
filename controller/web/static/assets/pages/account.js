// Account: profile, password, sign out, and system information.
import { h, replace } from '../lib/dom.js';
import { get, post } from '../lib/api.js';
import { icon } from '../lib/icons.js';
import * as live from '../lib/live.js';
import * as fmt from '../lib/format.js';
import { card, confirmDialog, field, pageHead, toast } from '../lib/ui.js';

export async function render(main, { can, user }) {
  const pw = h('form', { class: 'stack', style: { maxWidth: '420px' } },
    field('Current password', h('input', { type: 'password', name: 'current', required: true, autocomplete: 'current-password' })),
    field('New password', h('input', { type: 'password', name: 'next', required: true, minlength: 10, autocomplete: 'new-password' }), { hint: 'At least 10 characters. Your other signed-in devices will be signed out.' }),
    h('div', {}, h('button', { class: 'btn primary', type: 'submit' }, 'Change password')));
  pw.addEventListener('submit', async (e) => {
    e.preventDefault();
    try {
      await post('/auth/password', { current_password: pw.current.value, new_password: pw.next.value });
      pw.reset();
      toast('Password changed');
    } catch (err) {
      toast(err.message, 'error');
    }
  });

  const system = h('dl', { class: 'facts' });
  main.append(
    pageHead('Account', `Signed in as ${user.username}`,
      h('button', { class: 'btn', onclick: async () => { await post('/auth/logout'); live.disconnect(); location.href = '/'; } }, icon('logout'), 'Sign out')),
    h('div', { class: 'grid cols-2' },
      card('Profile', 'Your role decides what you can see and change.',
        h('dl', { class: 'facts' },
          h('div', {}, h('dt', {}, 'Name'), h('dd', {}, user.display_name)),
          h('div', {}, h('dt', {}, 'Username'), h('dd', {}, user.username)),
          h('div', {}, h('dt', {}, 'Role'), h('dd', { style: { textTransform: 'capitalize' } }, user.role)),
          h('div', {}, h('dt', {}, 'Last sign-in'), h('dd', {}, fmt.dateTime(user.last_login_at))))),
      card('Password', null, pw),
      card('System', 'This controller', system,
        can('system.reboot') && h('div', { class: 'card-foot' }, h('button', {
          class: 'btn danger', onclick: async () => {
            if (!await confirmDialog('Restart the controller?', 'Control pauses for about a minute. Inverters keep their last limit while it restarts.', { label: 'Restart', danger: true })) return;
            try { await post('/system/reboot'); toast('Restarting…'); } catch (err) { toast(err.message, 'error'); }
          },
        }, 'Restart controller')))));

  try {
    const info = await get('/system');
    replace(system,
      h('div', {}, h('dt', {}, 'Operating system'), h('dd', {}, info.os)),
      h('div', {}, h('dt', {}, 'Controller'), h('dd', {}, info.version)),
      h('div', {}, h('dt', {}, 'Hostname'), h('dd', {}, info.hostname)),
      h('div', {}, h('dt', {}, 'Uptime'), h('dd', {}, fmt.duration(info.uptime_s))));
  } catch { /* not critical */ }
}
