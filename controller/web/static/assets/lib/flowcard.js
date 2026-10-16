// Power flow, drawn by the Home Assistant "Power Flow Card Plus" (MIT,
// vendored as ../power-flow-card-plus.js) running outside Home Assistant.
//
// The card expects a Home Assistant frontend around it: a `hass` object with
// entity states, the ha-card / ha-icon elements and the HA theme variables.
// This module provides small stand-ins for all of them, maps a controller
// snapshot onto four fake sensors, and falls back to the built-in diagram if
// the card cannot load.
import { replace } from './dom.js';
import { mdiIcon } from './mdi.js';
import { icon } from './icons.js';

let cardModule = null;
async function loadCard() {
  cardModule ??= import('../power-flow-card-plus.js').then((m) => m.PowerFlowCardPlus || null, (err) => {
    console.error('power flow card failed to load', err);
    return null;
  });
  return cardModule;
}

// Stand-ins for the Home Assistant elements the card renders --------------------

// The card asks for MDI names; the four nodes get the AsyncOS line icons so
// the diagram matches the rest of the UI, anything else the MDI glyph. The
// card swaps its battery icon by charge state: all of them are the genset.
function iconFor(name, size) {
  const own = { 'mdi:solar-power': 'sun', 'mdi:solar-panel': 'sun', 'mdi:transmission-tower': 'grid', 'mdi:home': 'load' };
  const key = own[name] || (String(name).startsWith('mdi:battery') ? 'genset' : null);
  return key ? icon(key, { width: size, height: size, style: { display: 'block' } }) : mdiIcon(name, size);
}

function defineHAElements() {
  if (!customElements.get('ha-card')) {
    customElements.define('ha-card', class extends HTMLElement {
      connectedCallback() { Object.assign(this.style, { display: 'block', overflow: 'hidden', position: 'relative' }); }
    });
  }
  if (!customElements.get('ha-ripple')) {
    customElements.define('ha-ripple', class extends HTMLElement {
      connectedCallback() { this.style.display = 'none'; }
    });
  }
  if (!customElements.get('ha-icon')) {
    customElements.define('ha-icon', class extends HTMLElement {
      static get observedAttributes() { return ['icon']; }
      set icon(name) { this.setAttribute('icon', name || ''); }
      get icon() { return this.getAttribute('icon'); }
      connectedCallback() {
        Object.assign(this.style, {
          display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
          width: 'var(--mdc-icon-size, 24px)', height: 'var(--mdc-icon-size, 24px)',
        });
        this.render();
      }
      attributeChangedCallback() { this.render(); }
      render() {
        // The card styles `.circle svg` as a stretched, unfilled flow layer;
        // a shadow root keeps the icon out of reach of that rule.
        const size = getComputedStyle(this).getPropertyValue('--mdc-icon-size').trim() || '24px';
        if (!this.shadowRoot) this.attachShadow({ mode: 'open' });
        this.shadowRoot.replaceChildren(iconFor(this.getAttribute('icon'), size));
      }
    });
  }
}

// Theme ---------------------------------------------------------------------------

/**
 * The card reads its colours from HA theme variables in its own :host rule,
 * which beats any document rule, so they are set inline on the element from
 * the AsyncOS tokens. Re-run when the colour scheme changes.
 */
function applyTheme(el) {
  const cs = getComputedStyle(document.documentElement);
  const v = (name) => cs.getPropertyValue(name).trim();
  const text = v('--foreground');
  const solar = v('--chart-solar');
  const grid = v('--chart-grid');
  const ret = v('--chart-export');
  const genset = v('--chart-genset');
  const load = v('--chart-load');
  const theme = {
    '--primary-text-color': text,
    '--secondary-text-color': v('--muted-foreground'),
    '--primary-color': v('--primary'),
    '--card-background-color': v('--card'),
    '--divider-color': v('--border'),
    '--energy-solar-color': solar,
    '--energy-grid-consumption-color': grid,
    '--energy-grid-return-color': ret,
    '--energy-battery-in-color': genset,
    '--energy-battery-out-color': genset,
    '--energy-non-fossil-color': v('--success'),
    '--icon-solar-color': solar,
    '--icon-grid-color': grid,
    '--icon-home-color': load,
    '--icon-battery-color': genset,
    '--cirlce-grid-color': grid, // sic: the card's own spelling
    '--circle-battery-color': genset,
    '--text-solar-color': text,
    '--text-home-color': text,
    '--text-grid-consumption-color': grid,
    '--text-grid-return-color': ret,
    '--text-battery-in-color': genset,
    '--text-battery-out-color': genset,
    '--clickable-cursor': 'default',
  };
  for (const [name, value] of Object.entries(theme)) if (value) el.style.setProperty(name, value);
}

// Layout corrections inside the card's shadow root. A constructed stylesheet,
// because the strict CSP refuses <style> elements.
const PATCH = `
  ha-card { background: transparent; box-shadow: none; border: 0; }
  .card-content > .row, .circle-container { position: relative; z-index: 2; }
  .circle { background: var(--card-background-color); }
  .lines { z-index: 1; }
  .lines svg { max-width: none; }
  .lines svg.flat-line { width: calc(var(--lines-svg-flat-width) + 32px); margin-left: -16px; margin-right: -16px; }
  .lines svg:not(.flat-line) { width: calc(var(--lines-svg-not-flat-width) + 24px); margin-left: -12px; margin-right: -12px; height: calc(var(--lines-svg-not-flat-line-height) + 12px); }
  ha-icon { display: inline-flex; align-items: center; justify-content: center; }
`;
let patchSheet = null;
function patchShadow(el, tries = 0) {
  const root = el.shadowRoot;
  if (!root) {
    if (tries < 120) requestAnimationFrame(() => patchShadow(el, tries + 1));
    return;
  }
  if (!patchSheet) {
    patchSheet = new CSSStyleSheet();
    patchSheet.replaceSync(PATCH);
  }
  if (!root.adoptedStyleSheets.includes(patchSheet)) root.adoptedStyleSheets = [...root.adoptedStyleSheets, patchSheet];
}

// Data ------------------------------------------------------------------------------

const LABELS = { solar: 'Solar', grid: 'Grid', home: 'Home', battery: 'Genset', non_fossil: 'Non-fossil', consumption: 'Import', return: 'Export', production: 'Production' };

const sensor = (value, name) => ({ state: value == null ? 'unavailable' : String(value), attributes: { unit_of_measurement: 'W', friendly_name: name } });

/** A minimal `hass` object holding the snapshot as four power sensors. */
function hassFor(snap) {
  return {
    states: {
      'sensor.solar_power': sensor(snap.pv_w ?? 0, 'Solar'),
      'sensor.grid_power': sensor(snap.grid_w ?? 0, 'Grid'),
      'sensor.home_power': sensor(snap.load_w ?? 0, 'Home'),
      // The card's battery slot: positive discharges to the home, which is
      // what a running genset does; negative is reverse power.
      'sensor.genset_power': sensor(snap.genset_w ?? 0, 'Genset'),
    },
    locale: { language: 'en-US', number_format: 'language' },
    config: { components: [] },
    user: { name: 'AsyncOS' },
    themes: { darkMode: matchMedia('(prefers-color-scheme: dark)').matches },
    connection: { subscribeMessage: async () => () => {}, sendMessagePromise: async () => ({}) },
    localize: (key) => LABELS[String(key || '').split('.').pop()] || '',
    formatEntityState: (st) => `${st?.state ?? ''} ${st?.attributes?.unit_of_measurement || ''}`.trim(),
    formatEntityAttributeValue: (_st, _attr, value) => value,
  };
}

function configFor(snap) {
  const entities = {
    solar: { entity: 'sensor.solar_power', name: 'Solar', display_zero_state: true },
    home: { entity: 'sensor.home_power', name: 'Load' },
  };
  if (snap.grid_w != null || snap.devices?.some((d) => d.role === 'grid_meter' || d.role === 'source_meter')) {
    entities.grid = { entity: 'sensor.grid_power', name: 'Grid', display_state: 'one_way_no_zero' };
  }
  if (snap.genset_w != null || snap.devices?.some((d) => d.role === 'genset_meter' || d.role === 'source_meter')) {
    entities.battery = { entity: 'sensor.genset_power', name: 'Genset', icon: 'mdi:battery-high', display_state: 'one_way_no_zero' };
  }
  return {
    type: 'custom:power-flow-card-plus',
    entities,
    w_decimals: 0,
    kw_decimals: 1,
    watt_threshold: 1000,
    min_flow_rate: 0.75,
    max_flow_rate: 6,
    clickable_entities: false,
    disable_dots: false,
  };
}

/**
 * Mounts the power flow into el. Returns update(snap); call it with every
 * snapshot. fallback(el, snap) draws the built-in diagram if the card is
 * unavailable.
 */
export async function mountFlow(el, fallback) {
  const Card = await loadCard();
  if (!Card) return (snap) => fallback(el, snap);
  defineHAElements();
  const card = document.createElement('power-flow-card-plus');
  card.classList.add('flow-card');
  applyTheme(card);
  const dark = matchMedia('(prefers-color-scheme: dark)');
  const onScheme = () => applyTheme(card);
  dark.addEventListener('change', onScheme);
  let shape = '';
  let mounted = false;
  return (snap) => {
    if (!card.isConnected && mounted) {
      dark.removeEventListener('change', onScheme);
      return;
    }
    // Rebuild the config only when the set of sources changes.
    const config = configFor(snap);
    const key = Object.keys(config.entities).join();
    try {
      if (key !== shape) {
        shape = key;
        card.setConfig(config);
      }
      card.hass = hassFor(snap);
      if (!mounted) {
        mounted = true;
        replace(el, card);
        patchShadow(card);
      }
    } catch (err) {
      console.error('power flow card failed', err);
      fallback(el, snap);
    }
  };
}
