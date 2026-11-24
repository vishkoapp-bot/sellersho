// Code 128 symbol widths from ISO/IEC 15417, in symbol-value order.
// SPNCX fits Code Set B: printable ASCII letters, digits, and hyphens.
const WIDTHS = `212222 222122 222221 121223 121322 131222 122213 122312 132212 221213 221312 231212 112232 122132 122231 113222 123122 123221 223211 221132 221231 213212 223112 312131 311222 321122 321221 312212 322112 322211 212123 212321 232121 111323 131123 131321 112313 132113 132311 211313 231113 231311 112133 112331 132131 113123 113321 133121 313121 211331 231131 213113 213311 213131 311123 311321 331121 312113 312311 332111 314111 221411 431111 111224 111422 121124 121421 141122 141221 112214 112412 122114 122411 142112 142211 241211 221114 413111 241112 134111 111242 121142 121241 114212 124112 124211 411212 421112 421211 212141 214121 412121 111143 111341 131141 114113 114311 411113 411311 113141 114131 311141 411131 211412 211214 211232 2331112`.split(' ');

export const isSPNCX = (code: unknown): code is string =>
  typeof code === 'string' && code.length <= 20 && /^[0-9A-Z-]+-X[0-9]{4}$/.test(code);

export function spncxBarcode(code: string) {
  if (!isSPNCX(code)) throw new Error('Invalid SPNCX');
  const data = Array.from(code, char => char.charCodeAt(0) - 32);
  const startB = 104;
  const checksum = data.reduce((sum, value, index) => sum + value * (index + 1), startB) % 103;
  const symbols = [startB, ...data, checksum, 106];
  let x = 10; // include the required quiet zones in the vector itself
  const path: string[] = [];
  for (const symbol of symbols) {
    Array.from(WIDTHS[symbol], Number).forEach((width, index) => {
      if (index % 2 === 0) path.push(`M${x} 0h${width}v60h-${width}z`);
      x += width;
    });
  }
  return { viewBox: `0 0 ${x + 10} 60`, path: path.join('') };
}

const SVG_NS = 'http://www.w3.org/2000/svg';
const PRINT_STYLE = `
  @page { size: 80mm 30mm; margin: 0 }
  * { box-sizing: border-box }
  body { margin: 0; background: #fff; color: #000; font: 14px sans-serif }
  .spncx-sheet { width: 80mm; height: 30mm; margin: 0 auto; padding: 3mm;
    display: flex; align-items: center; justify-content: center; flex-direction: column;
    gap: 2mm; break-after: page; break-inside: avoid }
  .spncx-sheet:last-child { break-after: auto }
  .spncx-sheet svg { display: block; width: 100%; height: 16mm; fill: #000 }
  .spncx-sheet code { white-space: nowrap; font: bold 11pt monospace; direction: ltr }
  .spncx-print-help { margin: 1rem auto; text-align: center; direction: rtl }
  @media print { .spncx-print-help { display: none } }
`;

// A separate, script-free document prints the label sheets, never the staff
// dashboard. This is called by a fresh button click after the server job exists.
export function openSPNCXPrint(codes: readonly string[]): Window | null {
  const popup = window.open('', '_blank', 'popup,width=560,height=640');
  if (!popup) return null;
  try {
    popup.opener = null;
    const doc = popup.document;
    doc.title = 'چاپ برچسب SPNCX';
    doc.documentElement.lang = 'fa';
    const style = doc.createElement('style');
    style.textContent = PRINT_STYLE;
    doc.head.append(style);
    const help = doc.createElement('div');
    help.className = 'spncx-print-help';
    const button = doc.createElement('button');
    button.textContent = 'باز کردن تنظیمات چاپ';
    button.addEventListener('click', () => popup.print());
    help.append(button);
    doc.body.append(help);
    const pages = doc.createElement('main');
    for (const code of codes) {
      const barcode = spncxBarcode(code);
      const sheet = doc.createElement('section');
      sheet.className = 'spncx-sheet';
      const svg = doc.createElementNS(SVG_NS, 'svg');
      svg.setAttribute('viewBox', barcode.viewBox);
      svg.setAttribute('preserveAspectRatio', 'none');
      svg.setAttribute('shape-rendering', 'crispEdges');
      svg.setAttribute('aria-hidden', 'true');
      const path = doc.createElementNS(SVG_NS, 'path');
      path.setAttribute('d', barcode.path);
      svg.append(path);
      const text = doc.createElement('code');
      text.textContent = code;
      sheet.append(svg, text);
      pages.append(sheet);
    }
    doc.body.append(pages);
    popup.focus();
    popup.print();
    return popup;
  } catch (error) {
    popup.close();
    throw error;
  }
}
