// SVG charts: a time-series line chart and a stacked bar chart, both
// responsive and with a hover tooltip.
import { h, s, clear } from './dom.js';

function niceStep(range, ticks) {
  const raw = range / ticks;
  const mag = 10 ** Math.floor(Math.log10(raw || 1));
  const norm = raw / mag;
  return (norm < 1.5 ? 1 : norm < 3 ? 2 : norm < 7 ? 5 : 10) * mag;
}

function niceScale(min, max, ticks = 4) {
  if (min === max) { min -= 1; max += 1; }
  const step = niceStep(max - min, ticks);
  const lo = Math.floor(min / step) * step;
  const hi = Math.ceil(max / step) * step;
  const values = [];
  for (let v = lo; v <= hi + step / 2; v += step) values.push(Math.abs(v) < step / 1e6 ? 0 : v);
  return { lo, hi, values };
}

/** Monotone cubic interpolation (like d3.curveMonotoneX): smooth, no overshoot. */
function monotonePath(pts) {
  if (pts.length < 3) return pts.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(1)},${p[1].toFixed(1)}`).join('');
  const n = pts.length;
  const dx = [], slope = [], tangent = new Array(n);
  for (let i = 0; i < n - 1; i++) {
    dx[i] = pts[i + 1][0] - pts[i][0];
    slope[i] = dx[i] ? (pts[i + 1][1] - pts[i][1]) / dx[i] : 0;
  }
  tangent[0] = slope[0];
  tangent[n - 1] = slope[n - 2];
  for (let i = 1; i < n - 1; i++) {
    tangent[i] = slope[i - 1] * slope[i] <= 0 ? 0 : (3 * (dx[i - 1] + dx[i])) / ((2 * dx[i] + dx[i - 1]) / slope[i - 1] + (dx[i] + 2 * dx[i - 1]) / slope[i]);
  }
  let d = `M${pts[0][0].toFixed(1)},${pts[0][1].toFixed(1)}`;
  for (let i = 0; i < n - 1; i++) {
    const h3 = dx[i] / 3;
    d += `C${(pts[i][0] + h3).toFixed(1)},${(pts[i][1] + tangent[i] * h3).toFixed(1)} ${(pts[i + 1][0] - h3).toFixed(1)},${(pts[i + 1][1] - tangent[i + 1] * h3).toFixed(1)} ${pts[i + 1][0].toFixed(1)},${pts[i + 1][1].toFixed(1)}`;
  }
  return d;
}

/** A rectangle with the two corners on the outer end rounded. */
function barPath(x, top, w, hgt, r, down) {
  r = Math.max(0, Math.min(r, w / 2, hgt));
  if (!down) return `M${x},${top + hgt}V${top + r}Q${x},${top} ${x + r},${top}H${x + w - r}Q${x + w},${top} ${x + w},${top + r}V${top + hgt}Z`;
  const b = top + hgt;
  return `M${x},${top}V${b - r}Q${x},${b} ${x + r},${b}H${x + w - r}Q${x + w},${b} ${x + w},${b - r}V${top}Z`;
}

/** Draws at the container's real pixel width, and again when it resizes. */
function responsive(container, draw) {
  let last = 0;
  const observer = new ResizeObserver(([entry]) => {
    if (!container.isConnected) { observer.disconnect(); return; }
    const width = Math.round(entry.contentRect.width);
    if (width && width !== last) {
      last = width;
      draw(width);
    }
  });
  observer.observe(container);
}

function tooltip(container) {
  const tip = h('div', { class: 'chart-tip', hidden: true });
  container.append(tip);
  return {
    show(x, y, title, rows) {
      clear(tip).append(h('b', {}, title), ...rows.map((r) => h('div', {}, h('i', { style: { background: r.color } }), h('span', {}, r.label), h('strong', {}, r.value))));
      tip.hidden = false;
      const box = container.getBoundingClientRect();
      const left = x + 14 + tip.offsetWidth > box.width ? x - tip.offsetWidth - 14 : x + 14;
      tip.style.left = `${Math.max(0, left)}px`;
      tip.style.top = `${Math.max(0, y - tip.offsetHeight / 2)}px`;
    },
    hide() { tip.hidden = true; },
  };
}

/**
 * Line chart over time.
 * series: [{ key, label, color, area? }]; points: [{ t, [key]: value }]
 */
export function lineChart({ series, points, height = 240, format, timeFormat, xMin, xMax, maxGap = 10000 }) {
  const container = h('div', { class: 'chart', style: { minHeight: `${height}px` } });
  const tip = tooltip(container);
  let svg = null;
  responsive(container, (width) => {
  const pad = { l: 64, r: 12, t: 10, b: 26 };
  svg?.remove();
  svg = s('svg', { viewBox: `0 0 ${width} ${height}`, width, height, role: 'img', 'aria-label': 'Power over time' });
  container.prepend(svg);

  const t0 = xMin ?? (points[0]?.t ?? Date.now() - 60000);
  const t1 = xMax ?? (points.at(-1)?.t ?? Date.now());
  let lo = 0, hi = 0;
  for (const p of points) for (const sr of series) {
    const v = p[sr.key];
    if (v != null) { lo = Math.min(lo, v); hi = Math.max(hi, v); }
  }
  const scale = niceScale(lo, hi);
  const x = (t) => pad.l + ((t - t0) / Math.max(1, t1 - t0)) * (width - pad.l - pad.r);
  const y = (v) => pad.t + (1 - (v - scale.lo) / (scale.hi - scale.lo)) * (height - pad.t - pad.b);

  const axis = s('g', { class: 'axis' });
  for (const v of scale.values) {
    axis.append(s('line', { class: v === 0 ? 'zero' : 'gridline', x1: pad.l, x2: width - pad.r, y1: y(v), y2: y(v), 'vector-effect': 'non-scaling-stroke' }));
    axis.append(s('text', { x: pad.l - 8, y: y(v) + 4, 'text-anchor': 'end' }, format(v)));
  }
  const ticks = Math.max(2, Math.min(6, Math.floor(width / 110)));
  for (let i = 0; i <= ticks; i++) {
    const t = t0 + ((t1 - t0) * i) / ticks;
    axis.append(s('text', { x: x(t), y: height - 6, 'text-anchor': i === 0 ? 'start' : i === ticks ? 'end' : 'middle' }, timeFormat(t)));
  }
  svg.append(axis);

  const defs = s('defs');
  svg.append(defs);
  for (const sr of series) {
    // Contiguous runs of points, each drawn as a smooth monotone curve.
    const runs = [];
    let run = [];
    let prevT = null;
    for (const p of points) {
      const v = p[sr.key];
      // Break the line at missing values and at gaps in time, rather than
      // drawing a misleading straight segment across them.
      if (v == null || (prevT != null && p.t - prevT > maxGap)) { if (run.length) runs.push(run); run = []; }
      prevT = p.t;
      if (v != null) run.push([x(p.t), y(v)]);
    }
    if (run.length) runs.push(run);
    if (!runs.length) continue;
    let fillId = null;
    if (sr.area) {
      fillId = `fill-${sr.key}-${Math.random().toString(36).slice(2, 7)}`;
      defs.append(s('linearGradient', { id: fillId, x1: 0, y1: 0, x2: 0, y2: 1 },
        s('stop', { offset: '5%', 'stop-color': sr.color, 'stop-opacity': 0.6 }),
        s('stop', { offset: '95%', 'stop-color': sr.color, 'stop-opacity': 0.04 })));
    }
    for (const r of runs) {
      const d = monotonePath(r);
      if (fillId) svg.append(s('path', { d: `${d}L${r.at(-1)[0]},${y(0)}L${r[0][0]},${y(0)}Z`, fill: `url(#${fillId})` }));
      svg.append(s('path', { d, fill: 'none', stroke: sr.color, 'stroke-width': 2, 'stroke-linejoin': 'round', 'vector-effect': 'non-scaling-stroke' }));
    }
  }

  const cursor = s('line', { class: 'cursor', y1: pad.t, y2: height - pad.b, 'vector-effect': 'non-scaling-stroke', visibility: 'hidden' });
  svg.append(cursor);
  const onPoint = (e) => {
    if (!points.length) return;
    const box = svg.getBoundingClientRect();
    const t = t0 + ((e.clientX - box.left) - pad.l) / (width - pad.l - pad.r) * (t1 - t0);
    let nearest = points[0];
    for (const p of points) if (Math.abs(p.t - t) < Math.abs(nearest.t - t)) nearest = p;
    cursor.setAttribute('x1', x(nearest.t));
    cursor.setAttribute('x2', x(nearest.t));
    cursor.setAttribute('visibility', 'visible');
    tip.show(e.clientX - box.left, e.clientY - box.top, timeFormat(nearest.t, true),
      series.filter((sr) => nearest[sr.key] != null).map((sr) => ({ label: sr.label, color: sr.color, value: format(nearest[sr.key]) })));
  };
  svg.addEventListener('pointermove', onPoint);
  svg.addEventListener('pointerdown', onPoint);
  svg.addEventListener('pointerleave', () => { cursor.setAttribute('visibility', 'hidden'); tip.hide(); });
  });
  return container;
}

/**
 * Stacked bars. Positive stacks above zero, negative below.
 * stacks: [{ key, label, color, negative? }]; buckets: [{ label, title, [key]: value }]
 */
export function barChart({ stacks, buckets, height = 280, format }) {
  const container = h('div', { class: 'chart', style: { minHeight: `${height}px` } });
  const tip = tooltip(container);
  let svg = null;
  responsive(container, (width) => {
  const pad = { l: 64, r: 8, t: 10, b: 26 };
  svg?.remove();
  svg = s('svg', { viewBox: `0 0 ${width} ${height}`, width, height, role: 'img', 'aria-label': 'Energy per period' });
  container.prepend(svg);

  let hi = 0, lo = 0;
  for (const b of buckets) {
    hi = Math.max(hi, stacks.filter((st) => !st.negative).reduce((sum, st) => sum + (b[st.key] || 0), 0));
    lo = Math.min(lo, -stacks.filter((st) => st.negative).reduce((sum, st) => sum + (b[st.key] || 0), 0));
  }
  const scale = niceScale(lo, hi);
  const y = (v) => pad.t + (1 - (v - scale.lo) / (scale.hi - scale.lo)) * (height - pad.t - pad.b);
  const slot = (width - pad.l - pad.r) / Math.max(1, buckets.length);
  const barW = Math.max(2, Math.min(48, slot * 0.7));

  const axis = s('g', { class: 'axis' });
  for (const v of scale.values) {
    axis.append(s('line', { class: v === 0 ? 'zero' : 'gridline', x1: pad.l, x2: width - pad.r, y1: y(v), y2: y(v), 'vector-effect': 'non-scaling-stroke' }));
    axis.append(s('text', { x: pad.l - 8, y: y(v) + 4, 'text-anchor': 'end' }, format(v)));
  }
  const every = Math.ceil(buckets.length / Math.max(3, Math.floor(width / 56)));
  buckets.forEach((b, i) => {
    if (i % every === 0) axis.append(s('text', { x: pad.l + slot * (i + 0.5), y: height - 6, 'text-anchor': 'middle' }, b.label));
  });
  svg.append(axis);

  buckets.forEach((b, i) => {
    const x = pad.l + slot * i + (slot - barW) / 2;
    const group = s('g', {});
    const highlight = s('rect', { class: 'bar-hover', x: pad.l + slot * i, y: pad.t, width: slot, height: height - pad.t - pad.b, rx: 4, visibility: 'hidden' });
    group.append(highlight);
    // Only the outermost segment of each stack gets rounded corners.
    const up = stacks.filter((st) => !st.negative && (b[st.key] || 0) > 0);
    const down = stacks.filter((st) => st.negative && (b[st.key] || 0) > 0);
    let acc = 0;
    up.forEach((st, k) => {
      const v = b[st.key];
      const top = y(acc + v), bottom = y(acc);
      group.append(s('path', { d: barPath(x, top, barW, bottom - top, k === up.length - 1 ? 4 : 0, false), fill: st.color }));
      acc += v;
    });
    acc = 0;
    down.forEach((st, k) => {
      const v = b[st.key];
      const top = y(-acc), bottom = y(-(acc + v));
      group.append(s('path', { d: barPath(x, top, barW, bottom - top, k === down.length - 1 ? 4 : 0, true), fill: st.color }));
      acc += v;
    });
    const hit = s('rect', { x: pad.l + slot * i, y: pad.t, width: slot, height: height - pad.t - pad.b, fill: 'transparent' });
    const onPoint = (e) => {
      const box = svg.getBoundingClientRect();
      for (const other of svg.querySelectorAll('.bar-hover')) other.setAttribute('visibility', 'hidden');
      highlight.setAttribute('visibility', 'visible');
      tip.show(e.clientX - box.left, e.clientY - box.top, b.title,
        stacks.map((st) => ({ label: st.label, color: st.color, value: format((st.negative ? -1 : 1) * (b[st.key] || 0)) })));
    };
    hit.addEventListener('pointermove', onPoint);
    hit.addEventListener('pointerdown', onPoint);
    hit.addEventListener('pointerleave', () => { highlight.setAttribute('visibility', 'hidden'); tip.hide(); });
    group.append(hit);
    svg.append(group);
  });
  });
  return container;
}

export function legend(items) {
  return h('div', { class: 'legend' }, items.map((it) => h('span', {}, h('i', { style: { background: it.color } }), it.label)));
}
