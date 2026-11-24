import assert from 'node:assert/strict';
import { test } from 'node:test';
import { CODE128_PATTERNS, encodeCode128B, renderCode128Svg } from './code128';

test('encodes the reference vector in Code Set B', () => {
  const data = [36, 16, 38, 25, 17, 33, 22, 16, 13, 56, 16, 16, 16, 17];
  assert.equal(data.reduce((sum, value, index) => sum + value * (index + 1), 0), 2338);
  const words = encodeCode128B('D0F91A60-X0001');
  assert.equal(words.at(-2), 73);
  assert.deepEqual(words, [104, ...data, 73, 106]);
  assert.equal(words.reduce((sum, word) => sum + patternWidth(CODE128_PATTERNS[word]), 0), 189);
});

test('encodes the SPNC-T19A reference vector', () => {
  // By hand: 51+96+138+140+65+312+119+200+297+130+616+192+208+224+255 = 3043.
  // (104 + 3043) - 30 × 103 = 57.
  assert.equal(encodeCode128B('SPNC-T19A-X0001').at(-2), 57);
});

function patternWidth(pattern: string): number {
  return [...pattern].reduce((sum, width) => sum + Number(width), 0);
}

test('all 107 Code 128 patterns have the standard module widths', () => {
  assert.equal(CODE128_PATTERNS.length, 107);
  CODE128_PATTERNS.forEach((pattern, word) => {
    assert.match(pattern, word === 106 ? /^[1-4]{7}$/ : /^[1-4]{6}$/);
    assert.equal(patternWidth(pattern), word === 106 ? 13 : 11, `codeword ${word}`);
  });
});

test('rejects malformed unit codes before generating SVG', () => {
  for (const value of ['d0f91a60-X0001', 'D0F91A60-X۰۰۰۱', 'D0F91A60-X001', 'D0F91A60', '', 'A\u001f-X0001', 'AÉ-X0001', 'D0F91A60-X0001\n']) {
    assert.throws(() => renderCode128Svg(value), RangeError);
  }
});

test('sequence fixtures sort lexically and encode differently', () => {
  const inSequenceOrder = ['D0F91A60-X0001', 'D0F91A60-X0010', 'D0F91A60-X0200'];
  assert.deepEqual([inSequenceOrder[2], inSequenceOrder[0], inSequenceOrder[1]].sort(), inSequenceOrder);
  assert.equal(new Set(inSequenceOrder.map(value => encodeCode128B(value).at(-2))).size, 3);
  assert.equal(new Set(inSequenceOrder.map(value => renderCode128Svg(value))).size, 3);
});

test('SVG is standalone and contains both quiet zones', () => {
  const svg = renderCode128Svg('D0F91A60-X0001');
  assert.doesNotMatch(svg, /<text|(?:href|src)=|url\(|preserveAspectRatio="none"/i);
  assert.match(svg, /style="width: 100%; height: auto"/);
  const viewBox = svg.match(/viewBox="0 0 (\d+) (\d+)"/);
  assert.ok(viewBox);
  assert.equal(Number(viewBox[2]), 48);
  const bars = [...svg.matchAll(/<rect x="(\d+)" y="0" width="(\d+)" height="48"\/>/g)];
  const first = bars[0];
  const last = bars.at(-1)!;
  assert.ok(Number(first[1]) >= 20);
  assert.equal(Number(last[1]) + Number(last[2]) - Number(first[1]), 189 * 2);
  assert.ok(Number(viewBox[1]) - Number(last[1]) - Number(last[2]) >= 20);
  assert.match(renderCode128Svg('D0F91A60-X0001', { moduleWidthPx: 3, heightPx: 30 }), /viewBox="0 0 627 30"/);
});
