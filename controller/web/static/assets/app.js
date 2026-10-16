// AsyncOS controller UI: routing, session and the app shell.
import { h, replace } from './lib/dom.js';
import { get, post, setUnauthorizedHandler } from './lib/api.js';
import { icon, logoMark } from './lib/icons.js';
import * as live from './lib/live.js';
import { field, toast } from './lib/ui.js';

const routes = [
  { path: '/', title: 'Overview', icon: 'overview', perm: 'dashboard.view', load: () => import('./pages/overview.js') },
  { path: '/energy', title: 'Energy', icon: 'energy', perm: 'energy.view', load: () => import('./pages/energy.js') },
  { path: '/devices', title: 'Devices', icon: 'devices', perm: 'devices.view', load: () => import('./pages/devices.js') },
  { path: '/alarms', title: 'Alarms', icon: 'alarm', perm: 'alarms.view', load: () => import('./pages/alarms.js') },
  { path: '/control', title: 'Control', icon: 'control', perm: 'devices.view', section: 'Configuration', load: () => import('./pages/control.js') },
  { path: '/site', title: 'Site setup', icon: 'site', perm: 'site.configure', load: () => import('./pages/site.js') },
  { path: '/users', title: 'Users', icon: 'users', perm: 'users.manage', section: 'Administration', load: () => import('./pages/users.js') },
  { path: '/audit', title: 'Audit log', icon: 'audit', perm: 'audit.view', load: () => import('./pages/audit.js') },
  { path: '/account', title: 'Account', icon: 'account', perm: null, hidden: true, load: () => import('./pages/account.js') },
];

const root = document.getElementById('app');
const state = { user: null, cleanup: null };

export function can(permission) {
  return !permission || !!state.user?.permissions.includes(permission);
}

export function currentUser() { return state.user; }

export function navigate(path) {
  if (location.pathname !== path) history.pushState({}, '', path);
  render();
}

document.addEventListener('click', (e) => {
  const a = e.target.closest('a[data-link]');
  if (!a || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
  e.preventDefault();
  navigate(a.getAttribute('href'));
});
window.addEventListener('popstate', () => render());

setUnauthorizedHandler(() => {
  if (!state.user) return;
  state.user = null;
  live.disconnect();
  toast('Your session ended. Please sign in again.', 'error');
  start();
});

/** /kiosk: claim a display link from the URL fragment, then show the wall display. */
async function startKiosk() {
  const token = new URLSearchParams(location.hash.slice(1)).get('token');
  if (token) {
    history.replaceState({}, '', '/kiosk');
    try { await post('/kiosk/claim', { token }); } catch (err) { return kioskUnlinked(err.message); }
  }
  try {
    state.user = await get('/auth/me');
  } catch {
    return kioskUnlinked('This screen is not linked yet.');
  }
  document.title = 'AsyncOS';
  const page = await import('./pages/kiosk.js');
  await page.render(root, { can, user: state.user, navigate });
}

function kioskUnlinked(message) {
  authScreen('Display', message, h('div', { class: 'stack' },
    h('p', { class: 'muted small' }, 'An owner can create a display link under Users → Display links, or you can sign in to use this screen as a kiosk.'),
    h('button', { class: 'btn primary', onclick: () => { location.href = '/'; } }, 'Sign in')));
}

async function start() {
  if (location.pathname === '/kiosk') return startKiosk();
  try {
    const setup = await get('/setup');
    if (setup.needed) return showSetup();
  } catch (err) {
    return showFatal(err.message);
  }
  try {
    state.user = await get('/auth/me');
  } catch {
    return showLogin();
  }
  live.connect(() => {
    state.user = null;
    toast('Your session ended. Please sign in again.', 'error');
    showLogin();
  });
  render();
}

function authScreen(title, lead, form) {
  replace(root, h('div', { class: 'auth' },
    h('div', { class: 'card auth-card' },
      h('div', { class: 'brand' }, logoMark(), h('span', { class: 'brand-name' }, 'Async', h('span', {}, 'OS'))),
      h('p', { class: 'lead' }, lead),
      h('h1', { class: 'sr-only', hidden: true }, title),
      form)));
  form.querySelector('input')?.focus();
}

function showFatal(message) {
  authScreen('Error', 'The controller is not responding.', h('div', { class: 'stack' },
    h('p', { class: 'error-text' }, message),
    h('button', { class: 'btn primary', onclick: () => start() }, 'Try again')));
}

function showLogin() {
  const error = h('p', { class: 'error-text', role: 'alert' });
  const button = h('button', { class: 'btn primary', type: 'submit' }, 'Sign in');
  const form = h('form', {},
    field('Username', h('input', { name: 'username', autocomplete: 'username', required: true, autocapitalize: 'none' })),
    field('Password', h('input', { name: 'password', type: 'password', autocomplete: 'current-password', required: true })),
    error, button);
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    button.disabled = true;
    error.textContent = '';
    try {
      await post('/auth/login', { username: form.username.value, password: form.password.value });
      await start();
    } catch (err) {
      error.textContent = err.message;
      form.password.value = '';
      form.password.focus();
    } finally {
      button.disabled = false;
    }
  });
  authScreen('Sign in', 'Sign in to manage this controller.', form);
}

function showSetup() {
  const error = h('p', { class: 'error-text', role: 'alert' });
  const button = h('button', { class: 'btn primary', type: 'submit' }, 'Create owner account');
  const form = h('form', {},
    h('div', { class: 'banner info' }, icon('info'), h('div', {},
      'Enter the setup code shown on the controller’s screen. It proves you have physical access to this device.')),
    field('Setup code', h('input', { name: 'code', required: true, autocomplete: 'off', placeholder: 'XXXX-XXXX-XXXX', autocapitalize: 'characters', spellcheck: 'false' })),
    field('Your name', h('input', { name: 'display_name', autocomplete: 'name' })),
    field('Username', h('input', { name: 'username', required: true, autocomplete: 'username', autocapitalize: 'none', pattern: '[a-z0-9._\\-]{3,32}' }),
      { hint: '3 to 32 lowercase letters, digits, dots, dashes or underscores' }),
    field('Password', h('input', { name: 'password', type: 'password', required: true, minlength: 10, autocomplete: 'new-password' }),
      { hint: 'At least 10 characters' }),
    error, button);
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!form.reportValidity()) return;
    button.disabled = true;
    error.textContent = '';
    try {
      await post('/setup', {
        code: form.code.value, display_name: form.display_name.value,
        username: form.username.value.trim().toLowerCase(), password: form.password.value,
      });
      toast('Welcome to AsyncOS. Next, add your meters and inverters in Site setup.');
      history.replaceState({}, '', '/site');
      await start();
    } catch (err) {
      error.textContent = err.message;
    } finally {
      button.disabled = false;
    }
  });
  authScreen('Set up', 'Create the owner account for this controller.', form);
}

function shell(route) {
  const nav = h('nav', { class: 'nav', 'aria-label': 'Main' });
  for (const r of routes) {
    if (r.hidden || !can(r.perm)) continue;
    if (r.section) nav.append(h('div', { class: 'nav-section' }, r.section));
    nav.append(h('a', { href: r.path, 'data-link': true, 'aria-current': r === route ? 'page' : false }, icon(r.icon), r.title));
  }
  const u = state.user;
  const initials = (u.display_name || u.username).split(/\s+/).map((p) => p[0]).join('').slice(0, 2).toUpperCase();
  const sidebar = h('aside', { class: 'sidebar' },
    h('a', { class: 'brand', href: '/', 'data-link': true }, logoMark(), h('span', { class: 'brand-name' }, 'Async', h('span', {}, 'OS'))),
    nav,
    h('a', { class: 'nav-kiosk btn sm', href: '/kiosk', 'data-tip': 'Full-screen wall display for a TV or the device screen' }, icon('overview'), 'Kiosk mode'),
    h('a', { class: 'sidebar-foot', href: '/account', 'data-link': true },
      h('span', { class: 'avatar' }, initials),
      h('span', { class: 'who' }, h('b', {}, u.display_name || u.username), h('small', {}, u.role))));
  const main = h('main', { id: 'main' });
  const app = h('div', { class: 'shell' },
    h('header', { class: 'topbar' },
      h('button', { class: 'btn ghost icon-btn', 'aria-label': 'Menu', onclick: () => app.classList.toggle('nav-open') }, icon('menu')),
      h('span', { class: 'brand-name' }, 'Async', h('span', {}, 'OS'))),
    sidebar, main);
  sidebar.addEventListener('click', (e) => { if (e.target.closest('a')) app.classList.remove('nav-open'); });
  return { app, main };
}

async function render() {
  if (!state.user) return;
  state.cleanup?.();
  state.cleanup = null;
  const route = routes.find((r) => r.path === location.pathname) || routes[0];
  if (!can(route.perm)) {
    history.replaceState({}, '', '/');
    return render();
  }
  document.title = `${route.title} · AsyncOS`;
  const { app, main } = shell(route);
  replace(root, app);
  try {
    const page = await route.load();
    const cleanup = await page.render(main, { can, user: state.user, navigate });
    state.cleanup = typeof cleanup === 'function' ? cleanup : null;
  } catch (err) {
    replace(main, h('div', { class: 'banner' }, icon('alarm'), h('div', {}, `This page could not be loaded: ${err.message}`)));
  }
}

start();
