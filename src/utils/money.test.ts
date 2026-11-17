import test from 'node:test';
import assert from 'node:assert/strict';
import { markedUpPrice, roundMoneyTo100, parsePercent, truncatedPercent } from './money';

test('toman nearest100 half up and nonnegative price floor', () => {
  for (const [input, expected] of [[0,0],[49,0],[50,100],[149,100],[150,200]]) {
    assert.equal(roundMoneyTo100(input), expected);
  }
  assert.equal(markedUpPrice(1, 0), 1);
  assert.equal(markedUpPrice(149, 0), 149);
  assert.equal(markedUpPrice(100000, .25), 100300);
  assert.equal(markedUpPrice(9007199254740000, 0), 9007199254740000);
  assert.equal(truncatedPercent(9007199254740991, 5.25), 472877960873902);
});

test('unsupported precision and unsafe money are rejected', () => {
  for (const pct of [NaN, Infinity, -1, .001, 1000.01]) assert.throws(() => parsePercent(pct));
  for (const pct of [0,.01,.29,5.25,1000]) assert.doesNotThrow(() => parsePercent(pct));
  for (const amount of [NaN,Infinity,-1,1.5,9007199254740992]) assert.throws(() => markedUpPrice(amount,0));
  assert.throws(() => markedUpPrice(Number.MAX_SAFE_INTEGER,1000));
});
