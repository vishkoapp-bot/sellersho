// CAP-01: workload.json is the SINGLE source of truth for the
// 100-RPS mix. Drivers must import CLASSES/WEIGHTS from here, never
// re-declare the [30,20,15,15,8,4,4,4] literal. The weights-sum
// assertion (sum === 100) lives here and in the Python runner's
// validate_mix(); both must pass before any run starts.
import fs from 'node:fs';
import {workloadPath} from './target-config.mjs';

export const WORKLOAD = JSON.parse(fs.readFileSync(workloadPath(), 'utf8'));
export const CLASSES = WORKLOAD.classes;
export const WEIGHTS = WORKLOAD.weights;

export function validateWeights(weights = WEIGHTS) {
  if (!Array.isArray(weights) || weights.length !== CLASSES.length) {
    throw Error('workload mix must list one weight per scenario class');
  }
  if (!weights.every((w) => Number.isInteger(w) && w >= 0)) {
    throw Error('workload weights must be non-negative integers');
  }
  const sum = weights.reduce((a, b) => a + b, 0);
  if (sum !== 100) throw Error(`workload weights must sum to 100, got ${sum}`);
  return sum;
}

// Exact per-class arrival counts for an open-loop run. Throws unless
// every class count is an integer (10s/30s/90s at 100 RPS all split
// exactly; other durations must be chosen so the split stays exact).
export function countsFor(seconds, rps = WORKLOAD.total_api_rps) {
  validateWeights();
  const counts = WEIGHTS.map((w) => (w * seconds * rps) / 100);
  if (!counts.every(Number.isInteger)) {
    throw Error(`workload split is not exact for ${seconds}s at ${rps} RPS`);
  }
  return counts;
}

validateWeights();
