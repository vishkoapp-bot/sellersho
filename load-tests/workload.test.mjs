import test from 'node:test';
import assert from 'node:assert/strict';
import {WORKLOAD, CLASSES, WEIGHTS, validateWeights, countsFor} from './workload.mjs';
import {arrivalPlan} from './arrival-plan.mjs';

test('weights sum to exactly 100 and match the recorded contract', () => {
  assert.equal(validateWeights(), 100);
  assert.deepEqual(WEIGHTS, [30, 20, 15, 15, 8, 4, 4, 4]);
  assert.deepEqual(CLASSES, ['list', 'detail', 'store', 'account', 'quote', 'create', 'initiate', 'callback']);
  assert.equal(WORKLOAD.total_api_rps, 100);
});

test('weights-sum assertion rejects any other total', () => {
  assert.throws(() => validateWeights([30, 20, 15, 15, 8, 4, 4, 3]), /sum to 100, got 99/);
  assert.throws(() => validateWeights([30, 20, 15, 15, 8, 4, 4]), /one weight per scenario/);
  assert.throws(() => validateWeights([30, 20, 15, 15, 8, 4, 4, 4.5]), /non-negative integers/);
});

test('baseline durations split exactly (D3: 10s/30s/90s)', () => {
  assert.deepEqual(countsFor(10, 100), [300, 200, 150, 150, 80, 40, 40, 40]);
  assert.deepEqual(countsFor(30, 100), [900, 600, 450, 450, 240, 120, 120, 120]);
  assert.deepEqual(countsFor(90, 100), [2700, 1800, 1350, 1350, 720, 360, 360, 360]);
  assert.deepEqual(countsFor(300, 200), [18000, 12000, 9000, 9000, 4800, 2400, 2400, 2400]);
  assert.deepEqual(arrivalPlan(30, 100).counts, countsFor(30, 100));
  assert.deepEqual(arrivalPlan(10, 100).counts, countsFor(10, 100));
});

test('unread workload fields carry the contract values', () => {
  assert.equal(WORKLOAD.cpu_max_percent, 70);
  assert.equal(WORKLOAD.unexpected_error_rate_lt, 0.001);
  assert.equal(WORKLOAD.headroom_rps, 200);
  assert.equal(WORKLOAD.headroom_seconds, 300);
  assert.equal(WORKLOAD.recovery_seconds, 120);
  assert.equal(WORKLOAD.dropped_iterations, 0);
});
