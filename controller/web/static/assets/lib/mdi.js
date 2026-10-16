// Material Design Icons paths used by the Home Assistant "Power Flow Card Plus"
// card, vendored so the card can render its own icons outside Home Assistant.
// Icons are filled (MDI style), unlike the AsyncOS line icons in icons.js.

const paths = new Map([
  ["mdi:solar-power", ["M11.45,2V5.55L15,3.77L11.45,2M10.45,8L8,10.46L11.75,11.71L10.45,8M2,11.45L3.77,15L5.55,11.45H2M10,2H2V10C2.57,10.17 3.17,10.25 3.77,10.25C7.35,10.26 10.26,7.35 10.27,3.75C10.26,3.16 10.17,2.57 10,2M17,22V16H14L19,7V13H22L17,22Z"]],
  ["mdi:transmission-tower", ["M8.28,5.45L6.5,4.55L7.76,2H16.23L17.5,4.55L15.72,5.44L15,4H9L8.28,5.45M18.62,8H14.09L13.3,5H10.7L9.91,8H5.38L4.1,10.55L5.89,11.44L6.62,10H17.38L18.1,11.45L19.89,10.56L18.62,8M17.77,22H15.7L15.46,21.1L12,15.9L8.53,21.1L8.3,22H6.23L9.12,11H11.19L10.83,12.35L12,14.1L13.16,12.35L12.81,11H14.88L17.77,22M11.4,15L10.5,13.65L9.32,18.13L11.4,15M14.68,18.12L13.5,13.64L12.6,15L14.68,18.12Z"]],
  ["mdi:transmission-tower-off", ["M22.1 21.5L2.4 1.7L1.1 3L6.1 8H5.4L4.1 10.5L5.9 11.4L6.6 10H8.1L9.1 11L6.2 22H8.3L8.5 21.1L12 15.9L15.5 21.1L15.7 22H17.8L17 18.8L20.9 22.7L22.1 21.5M9.3 18.1L10.5 13.6L11.4 14.9L9.3 18.1M14.7 18.1L12.6 15L12.8 14.7L14.1 16L14.7 18.1M14.2 11H14.9L15.1 11.9L14.2 11M14.1 8H18.6L19.9 10.6L18.1 11.5L17.4 10H13.2L10.2 7L10.7 5H13.3L14.1 8M8.4 5.2L6.9 3.7L7.8 2H16.3L17.6 4.5L15.8 5.4L15 4H9L8.4 5.2Z"]],
  ["mdi:home", ["M10,20V14H14V20H19V12H22L12,3L2,12H5V20H10Z"]],
  ["mdi:battery-high", ["M16 20H8V6H16M16.67 4H15V2H9V4H7.33C6.6 4 6 4.6 6 5.33V20.67C6 21.4 6.6 22 7.33 22H16.67C17.41 22 18 21.41 18 20.67V5.33C18 4.6 17.4 4 16.67 4M15 16H9V19H15V16M15 7H9V10H15V7M15 11.5H9V14.5H15V11.5Z"]],
  ["mdi:battery-charging", ["M16.67,4H15V2H9V4H7.33A1.33,1.33 0 0,0 6,5.33V20.66C6,21.4 6.6,22 7.33,22H16.66C17.4,22 18,21.4 18,20.67V5.33C18,4.6 17.4,4 16.67,4M11,20V14.5H9L13,7V12.5H15"]],
  ["mdi:help-circle", ["M12,2A10,10 0 0,0 2,12A10,10 0 0,0 12,22A10,10 0 0,0 22,12A10,10 0 0,0 12,2M12,18A1.5,1.5 0 0,1 10.5,16.5A1.5,1.5 0 0,1 12,15A1.5,1.5 0 0,1 13.5,16.5A1.5,1.5 0 0,1 12,18M12.87,12.78L12,16H11L10.55,11.53C10.5,10.92 10.95,10.36 11.53,10.15C13.5,9.4 15.5,9.75 15.5,12C15.5,12.5 15.27,13.5 14.5,13.8C13.4,14.17 12.9,13.9 12.87,12.78Z"]],
]);

/** mdiIcon(name, size) builds a filled 24x24 MDI icon element. */
export function mdiIcon(name, size = '24px') {
  const d = paths.get(name);
  const NS = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('fill', 'currentColor');
  svg.setAttribute('aria-hidden', 'true');
  svg.style.display = 'block';
  for (const one of d || paths.get('mdi:help-circle') || []) {
    const p = document.createElementNS(NS, 'path');
    p.setAttribute('d', one);
    svg.append(p);
  }
  return svg;
}
