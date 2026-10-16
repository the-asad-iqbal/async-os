// Line icons (24 px grid, stroke drawn).
import { s } from './dom.js';

const paths = {
  overview: ['M3 12l9-8 9 8', 'M5 10v10h14V10', 'M10 20v-6h4v6'],
  energy: ['M4 20V10', 'M10 20V4', 'M16 20v-8', 'M22 20H2'],
  devices: ['M4 5h16v10H4z', 'M8 19h8', 'M12 15v4'],
  control: ['M4 6h10', 'M18 6h2', 'M4 12h4', 'M12 12h8', 'M4 18h12', 'M20 18h0', 'M14 4v4', 'M8 10v4', 'M16 16v4'],
  site: ['M3 21h18', 'M5 21V8l7-5 7 5v13', 'M9 21v-6h6v6'],
  alarm: ['M12 3l10 18H2z', 'M12 10v5', 'M12 18h.01'],
  users: ['M16 20v-2a4 4 0 00-4-4H6a4 4 0 00-4 4v2', 'M9 10a4 4 0 100-8 4 4 0 000 8z', 'M22 20v-2a4 4 0 00-3-3.9', 'M16 2.1a4 4 0 010 7.8'],
  audit: ['M14 3H6a2 2 0 00-2 2v14a2 2 0 002 2h12a2 2 0 002-2V9z', 'M14 3v6h6', 'M8 13h8', 'M8 17h5'],
  account: ['M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2', 'M12 11a4 4 0 100-8 4 4 0 000 8z'],
  menu: ['M3 6h18', 'M3 12h18', 'M3 18h18'],
  sun: ['M12 16a4 4 0 100-8 4 4 0 000 8z', 'M12 2v2', 'M12 20v2', 'M4.9 4.9l1.4 1.4', 'M17.7 17.7l1.4 1.4', 'M2 12h2', 'M20 12h2', 'M4.9 19.1l1.4-1.4', 'M17.7 6.3l1.4-1.4'],
  grid: ['M7 22l3-18h4l3 18', 'M8.5 13h7', 'M5 6h14', 'M9.3 9h5.4'],
  genset: ['M3 8h14v10H3z', 'M17 11h3v4h-3', 'M7 8V5h6v3', 'M9 11l-2 3h4l-2 3'],
  load: ['M3 11l9-8 9 8', 'M5 9v12h14V9', 'M12 13l-2 4h4l-2 4'],
  plus: ['M12 5v14', 'M5 12h14'],
  edit: ['M12 20h9', 'M16.5 3.5a2.1 2.1 0 013 3L7 19l-4 1 1-4z'],
  trash: ['M3 6h18', 'M8 6V4h8v2', 'M19 6l-1 14H6L5 6'],
  check: ['M20 6L9 17l-5-5'],
  refresh: ['M21 12a9 9 0 11-3-6.7L21 8', 'M21 3v5h-5'],
  chevronLeft: ['M15 18l-6-6 6-6'],
  chevronRight: ['M9 18l6-6-6-6'],
  logout: ['M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4', 'M16 17l5-5-5-5', 'M21 12H9'],
  bolt: ['M13 2L3 14h9l-1 8 10-12h-9z'],
  info: ['M12 22a10 10 0 100-20 10 10 0 000 20z', 'M12 16v-4', 'M12 8h.01'],
};

export function icon(name, extra = {}) {
  return s('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.8, 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true', ...extra },
    (paths[name] || paths.info).map((d) => s('path', { d })));
}

/** The AsyncOS mark. */
export function logoMark() {
  const id = `g${Math.random().toString(36).slice(2, 8)}`;
  return s('svg', { viewBox: '0 0 64 64', 'aria-hidden': 'true' },
    s('defs', {}, s('linearGradient', { id, x1: 0, y1: 0, x2: 64, y2: 64, gradientUnits: 'userSpaceOnUse' },
      s('stop', { offset: 0, 'stop-color': '#6B6CF8' }), s('stop', { offset: 1, 'stop-color': '#3F3BD9' }))),
    s('rect', { width: 64, height: 64, rx: 16, fill: `url(#${id})` }),
    s('path', { d: 'M18 47 L29.5 17 M34.5 17 L46 47', fill: 'none', stroke: '#fff', 'stroke-width': 6, 'stroke-linecap': 'round' }),
    s('circle', { cx: 32, cy: 37, r: 3.75, fill: '#22D3EE' }));
}
