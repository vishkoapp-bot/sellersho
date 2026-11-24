// Code 128 symbol patterns, indexed by codeword. Widths alternate bar and space.
export const CODE128_PATTERNS = [
  '212222', '222122', '222221', '121223', '121322', '131222', '122213', '122312', '132212', '221213',
  '221312', '231212', '112232', '122132', '122231', '113222', '123122', '123221', '223211', '221132',
  '221231', '213212', '223112', '312131', '311222', '321122', '321221', '312212', '322112', '322211',
  '212123', '212321', '232121', '111323', '131123', '131321', '112313', '132113', '132311', '211313',
  '231113', '231311', '112133', '112331', '132131', '113123', '113321', '133121', '313121', '211331',
  '231131', '213113', '213311', '213131', '311123', '311321', '331121', '312113', '312311', '332111',
  '314111', '221411', '431111', '111224', '111422', '121124', '121421', '141122', '141221', '112214',
  '112412', '122114', '122411', '142112', '142211', '241211', '221114', '413111', '241112', '134111',
  '111242', '121142', '121241', '114212', '124112', '124211', '411212', '421112', '421211', '212141',
  '214121', '412121', '111143', '111341', '131141', '114113', '114311', '411113', '411311', '113141',
  '114131', '311141', '411131', '211412', '211214', '211232', '2331112',
] as const;

export const UNIT_CODE_PATTERN = /^[0-9A-Z-]+-X[0-9]{4}$/;

export function encodeCode128B(value: string): number[] {
  const startB = 104;
  const data = Array.from(value, character => {
    const ascii = character.charCodeAt(0);
    if (ascii < 32 || ascii > 127) throw new RangeError('Code 128-B requires ASCII 32–127.');
    return ascii - 32;
  });
  const weightedSum = data.reduce((sum, word, index) => sum + word * (index + 1), startB);
  return [startB, ...data, weightedSum % 103, 106];
}

export function renderCode128Svg(
  value: string,
  { moduleWidthPx = 2, heightPx = 48 }: { moduleWidthPx?: number; heightPx?: number } = {},
): string {
  if (!UNIT_CODE_PATTERN.test(value)) throw new RangeError('Invalid unit code.');
  if (!Number.isSafeInteger(moduleWidthPx) || moduleWidthPx < 1 || !Number.isFinite(heightPx) || heightPx <= 0) {
    throw new RangeError('Invalid barcode dimensions.');
  }

  const words = encodeCode128B(value);
  const quietZone = 10;
  let offset = quietZone;
  const bars: string[] = [];
  for (const word of words) {
    [...CODE128_PATTERNS[word]].forEach((width, index) => {
      const modules = Number(width);
      if (index % 2 === 0) {
        bars.push(`<rect x="${offset * moduleWidthPx}" y="0" width="${modules * moduleWidthPx}" height="${heightPx}"/>`);
      }
      offset += modules;
    });
  }
  const width = (offset + quietZone) * moduleWidthPx;
  if (!Number.isSafeInteger(width)) throw new RangeError('Invalid barcode width.');
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${heightPx}" width="${width}" height="${heightPx}" style="width: 100%; height: auto" fill="#000" aria-hidden="true"><rect width="100%" height="100%" fill="#fff"/>${bars.join('')}</svg>`;
}
