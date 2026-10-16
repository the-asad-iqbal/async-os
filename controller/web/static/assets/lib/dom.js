// Tiny DOM builder. Text is always inserted as text nodes, never parsed as
// HTML, so values from devices or users cannot inject markup.

const SVG_NS = 'http://www.w3.org/2000/svg';

function build(el, props, children, svg) {
  for (const [key, value] of Object.entries(props || {})) {
    if (value == null || value === false) continue;
    if (key === 'class') {
      el.setAttribute('class', Array.isArray(value) ? value.filter(Boolean).join(' ') : value);
    } else if (key === 'style') {
      Object.assign(el.style, value);
    } else if (key === 'dataset') {
      Object.assign(el.dataset, value);
    } else if (key.startsWith('on') && typeof value === 'function') {
      el.addEventListener(key.slice(2).toLowerCase(), value);
    } else if (!svg && (key === 'value' || key === 'checked' || key === 'selected' || key === 'disabled')) {
      el[key] = value;
    } else {
      el.setAttribute(key, value === true ? '' : String(value));
    }
  }
  append(el, children);
  return el;
}

export function append(el, children) {
  for (const child of children.flat(Infinity)) {
    if (child == null || child === false) continue;
    el.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return el;
}

/** h('div', {class: 'card'}, 'text', child) */
export function h(tag, props, ...children) {
  return build(document.createElement(tag), props, children, false);
}

/** s('path', {d: '...'}) for SVG elements. */
export function s(tag, props, ...children) {
  return build(document.createElementNS(SVG_NS, tag), props, children, true);
}

export function clear(el) {
  while (el.firstChild) el.firstChild.remove();
  return el;
}

export function replace(el, ...children) {
  clear(el);
  return append(el, children);
}
