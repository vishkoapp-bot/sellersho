# Frontend test baseline

Run `npm test` from the repository root. The command discovers every
`src/**/*.test.ts` and `src/**/*.test.tsx` file and runs it through the existing
`tsx` and Node test runner. It fails on empty discovery and preserves a failing
child exit status. No dependency was added. Browser suites require an installed
Chrome/Edge, `CHROME_PATH`, or Puppeteer's installed browser; an unavailable
browser is a failure, not a skipped acceptance gate.

Browser suites serve synthetic fixtures on a loopback-only Vite listener. The
catalog fixture substitutes API responses while retaining the actual provider
stack and view. Its layout recorder observes the first committed DOM before the
navigation effect: a dropshipper never renders the central assortment, then
navigates to its storefront; a central store renders both fixture products.
The navigation assertion exercises actual store state instead of inspecting
source text. The obsolete SSR module-hook replacement was ineffective with the
current runner and failed before reaching the intended guard.

The order-identifier smoke test accepts a deterministic valid UUID beginning
`179` alongside random samples. It checks RFC4122 v4 format, rather than rejecting
legitimate random bytes that resemble a timestamp. This checks format and sampled
uniqueness; it does not prove randomness or test the full order endpoint.

Local validation on Windows, 2026-11-14:

- Workspace `npm test`: 277 passed, zero failures/skips, including the concurrent
  financial task's additional tests. Output: `logs/test02-frontend-all.log`.
- Root-only archived tree `775651fdef7092a725acd2cba659c6dabe384dcd`:
  `npm test` passed all 271 tests with zero failures/skips; `npm run build`
  passed. Vite reports the existing large output chunk warning. Output:
  `logs/test02-frontend-exact-test.log`, `logs/test02-frontend-exact-build.log`.
- `go test ./cmd/server -run
  'TestOrderIdentifiers_NonMonotonicity|TestOrderItemQuantityValidation' -count=20`
  passed in both the workspace and the root-only archive. Output:
  `logs/test02-order-id.log`, `logs/test02-order-id-exact.log`.
- Mutation check in the disposable source archive: disabling the catalog's
  render guard made the first-commit assertion fail (`2` central rows instead
  of `0`); exit was nonzero. The original bytes were restored in `finally`.
  Output: `logs/test02-catalog-mutation.log`.

Raw logs and archived build trees are local ignored evidence. TEST-02 remains
open: integrated disposable database regressions, CI isolation/failure injection,
dependency severity triage, and the other domain-task dependencies still apply.
This checkpoint does not certify production or capacity.
