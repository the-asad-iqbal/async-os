// Users and display links (owner only).
import { h, replace } from '../lib/dom.js';
import { del, get, patch, post } from '../lib/api.js';
import { icon } from '../lib/icons.js';
import * as fmt from '../lib/format.js';
import { card, checkbox, confirmDialog, emptyState, field, formDialog, pageHead, pill, select, textInput, toast } from '../lib/ui.js';

const roles = [
  { value: 'viewer', label: 'Viewer', help: 'Sees the dashboard, energy, devices and alarms.' },
  { value: 'operator', label: 'Operator', help: 'Viewer, plus acknowledging alarms and moving the export limit within the installer’s bound.' },
  { value: 'installer', label: 'Installer', help: 'Operator, plus devices, control settings, commissioning tools, the audit log and restarts.' },
  { value: 'owner', label: 'Owner', help: 'Everything, including users, display links and updates.' },
];
const roleKind = { owner: 'info', installer: 'ok', operator: 'warn', viewer: 'plain' };

export async function render(main, { user: me }) {
  const usersBody = h('div');
  const displaysBody = h('div');
  main.append(
    pageHead('Users', 'Who can access this controller, and what they can do',
      h('button', { class: 'btn primary', onclick: () => createUser() }, icon('plus'), 'Add user')),
    h('div', { class: 'stack' },
      h('section', { class: 'card' }, usersBody),
      h('section', { class: 'card' },
        h('div', { class: 'card-head' },
          h('div', {}, h('h2', {}, 'Display links'), h('p', {}, 'Read-only kiosk access for wall screens, without a user account. Revoke a link to cut the screen off.')),
          h('button', { class: 'btn', onclick: () => createDisplay() }, icon('plus'), 'New display link')),
        displaysBody)));

  async function loadUsers() {
    const users = await get('/users');
    replace(usersBody, h('div', { class: 'table-wrap', style: { marginTop: '-24px', borderTop: '0' } }, h('table', {},
      h('thead', {}, h('tr', {}, h('th', {}, 'User'), h('th', {}, 'Role'), h('th', {}, 'Status'), h('th', {}, 'Last sign-in'), h('th', {}))),
      h('tbody', {}, users.map((u) => h('tr', {},
        h('td', {}, h('b', {}, u.display_name), h('div', { class: 'muted small' }, u.username, u.id === me.id ? ' (you)' : '')),
        h('td', {}, h('span', { class: ['pill', roleKind[u.role], 'plain'], 'data-tip': roles.find((r) => r.value === u.role)?.help }, u.role)),
        h('td', {}, u.disabled ? pill('Disabled', 'danger') : pill('Active', 'ok')),
        h('td', { class: 'muted' }, u.last_login_at ? fmt.relative(u.last_login_at) : 'never'),
        h('td', { class: 'num' }, h('div', { class: 'row', style: { justifyContent: 'flex-end', gap: '4px' } },
          h('button', { class: 'btn sm ghost icon-btn', 'aria-label': 'Edit', 'data-tip': 'Edit role or status', onclick: () => editUser(u) }, icon('edit')),
          h('button', { class: 'btn sm ghost', 'data-tip': 'Set a new password; ends their sessions', onclick: () => resetPassword(u) }, 'Reset password'),
          u.id !== me.id && h('button', { class: 'btn sm ghost icon-btn danger', 'aria-label': 'Delete', 'data-tip': 'Delete user', onclick: () => deleteUser(u) }, icon('trash')))))))));
  }

  async function loadDisplays() {
    const displays = await get('/displays');
    if (!displays.length) {
      replace(displaysBody, emptyState('No display links yet.'));
      return;
    }
    replace(displaysBody, h('div', { class: 'table-wrap' }, h('table', {},
      h('thead', {}, h('tr', {}, h('th', {}, 'Name'), h('th', {}, 'Created'), h('th', {}, 'Last seen'), h('th', {}))),
      h('tbody', {}, displays.map((d) => h('tr', {},
        h('td', {}, h('b', {}, d.name)),
        h('td', { class: 'muted' }, `${fmt.dateTime(d.created_at)} by ${d.created_by}`),
        h('td', { class: 'muted' }, d.last_seen ? fmt.relative(d.last_seen) : 'never'),
        h('td', { class: 'num' }, h('button', {
          class: 'btn sm danger', onclick: async () => {
            if (!await confirmDialog(`Revoke “${d.name}”?`, 'The screen loses access within a minute.', { label: 'Revoke', danger: true })) return;
            await del(`/displays/${d.id}`);
            toast('Display link revoked');
            loadDisplays();
          },
        }, 'Revoke'))))))));
  }

  function roleSelect(value) {
    return field('Role', select('role', roles.map((r) => ({ value: r.value, label: `${r.label} — ${r.help}` })), value));
  }

  async function createUser() {
    if (await formDialog({
      title: 'Add user',
      body: h('div', { class: 'stack' },
        field('Name', textInput('display_name', '', { autocomplete: 'off' })),
        field('Username', textInput('username', '', { required: true, pattern: '[a-z0-9._\\-]{3,32}', autocapitalize: 'none' }), { hint: '3 to 32 lowercase letters, digits, dots, dashes or underscores' }),
        roleSelect('viewer'),
        field('Initial password', h('input', { type: 'password', name: 'password', required: true, minlength: 10, autocomplete: 'new-password' }), { hint: 'At least 10 characters. Ask them to change it after signing in.' })),
      submitLabel: 'Add user',
      onSubmit: (f) => post('/users', { display_name: f.display_name.value, username: f.username.value.trim().toLowerCase(), role: f.role.value, password: f.password.value }),
    })) { toast('User added'); loadUsers(); }
  }

  async function editUser(u) {
    if (await formDialog({
      title: `Edit ${u.display_name}`,
      description: 'Changing the role or disabling the account signs the user out everywhere.',
      body: h('div', { class: 'stack' },
        field('Name', textInput('display_name', u.display_name, { required: true })),
        roleSelect(u.role),
        checkbox('disabled', u.disabled, 'Account disabled')),
      onSubmit: (f) => patch(`/users/${u.id}`, { display_name: f.display_name.value, role: f.role.value, disabled: f.disabled.checked }),
    })) { toast('User updated'); loadUsers(); }
  }

  async function resetPassword(u) {
    if (await formDialog({
      title: `New password for ${u.display_name}`,
      description: 'They are signed out everywhere and must use the new password.',
      body: field('New password', h('input', { type: 'password', name: 'password', required: true, minlength: 10, autocomplete: 'new-password' })),
      submitLabel: 'Set password',
      onSubmit: (f) => post(`/users/${u.id}/password`, { password: f.password.value }),
    })) toast('Password set');
  }

  async function deleteUser(u) {
    if (!await confirmDialog(`Delete ${u.display_name}?`, 'This cannot be undone. Their audit entries are kept.', { label: 'Delete', danger: true })) return;
    try { await del(`/users/${u.id}`); toast('User deleted'); loadUsers(); } catch (err) { toast(err.message, 'error'); }
  }

  async function createDisplay() {
    let link = '';
    if (!await formDialog({
      title: 'New display link',
      description: 'Creates a read-only link for a wall screen. The link is shown once.',
      body: field('Screen name', textInput('name', '', { required: true, placeholder: 'Control room TV' })),
      submitLabel: 'Create link',
      onSubmit: async (f) => {
        const res = await post('/displays', { name: f.name.value });
        link = `${location.origin}/kiosk#token=${res.token}`;
      },
    })) return;
    loadDisplays();
    const input = h('input', { type: 'text', value: link, readonly: true, style: { fontFamily: 'var(--mono)', fontSize: '12px' } });
    await formDialog({
      title: 'Display link created',
      description: 'Open this link on the screen once. Anyone with it can view the dashboard until you revoke it, so treat it like a password.',
      body: h('div', { class: 'stack' }, input,
        h('button', { class: 'btn', type: 'button', onclick: async () => { try { await navigator.clipboard.writeText(link); toast('Link copied'); } catch { input.select(); } } }, 'Copy link')),
      submitLabel: 'Done',
    });
  }

  loadUsers();
  loadDisplays();
}
