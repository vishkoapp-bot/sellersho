import { it } from 'node:test';
import assert from 'node:assert/strict';
import { proposalStatusCounts, resolveProposalFocus } from './SupplierProductsTab';

// Deep-link decision tests (DESIGN.md §4.3-4.4): the bell badge → drawer
// click → Products / proposed_offers → SupplierFeedbackModal chain.
// The component applies resolveProposalFocus's outcome verbatim in its
// focus effect (setSubTab('proposed_offers'); scroll + open the modal
// when targetId is set; report the focus consumed), so these tests pin
// the whole decision surface of that chain: which outcome each state
// produces, including the consume-once guard. What they cannot cover —
// the effect actually running, the 50ms scroll/modal timeout, the 5s
// highlight ring, scrollIntoView — needs a DOM + act(), which this
// harness (tsx --test + renderToStaticMarkup, no jsdom) cannot run.
const proposals = [
  { id: 'prop-1', title: 'تیشرت' },
  { id: 'prop-2', title: 'شلوار' },
];

it('resets the handled guard when there is no focus id', () => {
  assert.deepEqual(resolveProposalFocus(null, proposals, 'prop-1', false), { kind: 'reset' });
  assert.deepEqual(resolveProposalFocus(undefined, proposals, 'prop-1', false), { kind: 'reset' });
  assert.deepEqual(resolveProposalFocus('', proposals, 'prop-1', false), { kind: 'reset' });
});

it('focuses a known proposal: switch tab and open the modal for it', () => {
  assert.deepEqual(resolveProposalFocus('prop-1', proposals, null, false), {
    kind: 'focus',
    focusId: 'prop-1',
    targetId: 'prop-1',
  });
});

it('consumes the focus exactly once: a handled id is skipped on re-render', () => {
  // Lifecycle: drawer hands prop-1 down → focus → dashboard clears it
  // (onFocusConsumed) → a later render with the same id must not re-open.
  assert.deepEqual(resolveProposalFocus('prop-1', proposals, null, false).kind, 'focus');
  assert.deepEqual(resolveProposalFocus('prop-1', proposals, 'prop-1', false), { kind: 'skip' });
  assert.deepEqual(resolveProposalFocus('prop-1', proposals, 'prop-1', true), { kind: 'skip' });
  // A different proposal id is still a fresh deep-link.
  assert.deepEqual(resolveProposalFocus('prop-2', proposals, 'prop-1', false), {
    kind: 'focus',
    focusId: 'prop-2',
    targetId: 'prop-2',
  });
});

// G6: the probe result for sup-supplier_test_0933 — 42 approved +
// 3 pending + 1 rejected = 46. The tab label must read pending 3
// with total 46 alongside it, never 46 as "awaiting review".
it('proposalStatusCounts splits the 46-row history into pending 3 / total 46', () => {
  const offers = [
    ...Array.from({ length: 42 }, (_, i) => ({ id: `approved-${i}`, status: 'approved' })),
    ...Array.from({ length: 3 }, (_, i) => ({ id: `pending-${i}`, status: 'pending' })),
    { id: 'rejected-0', status: 'rejected' },
  ];
  assert.deepEqual(proposalStatusCounts(offers), { total: 46, pending: 3, approved: 42, rejected: 1 });
  assert.deepEqual(proposalStatusCounts([]), { total: 0, pending: 0, approved: 0, rejected: 0 });
});

it('skips while proposals are still loading without consuming the id', () => {
  assert.deepEqual(resolveProposalFocus('prop-1', proposals, null, true), { kind: 'skip' });
  // The skip did not mark it handled: once loading finishes the same id
  // still lands (the effect re-runs on proposalsResource.loading).
  assert.deepEqual(resolveProposalFocus('prop-1', proposals, null, false), {
    kind: 'focus',
    focusId: 'prop-1',
    targetId: 'prop-1',
  });
});

it('matches a numeric proposal id against a string focus id', () => {
  assert.deepEqual(resolveProposalFocus('7', [{ id: 7 }], null, false), {
    kind: 'focus',
    focusId: '7',
    targetId: '7',
  });
});

it('still switches tab and consumes the focus when the id is not in the list', () => {
  // Stale id (proposal removed while the drawer was open): no modal
  // target, but the tab switch + consumption still happen — the effect
  // must not loop on an id it can never resolve.
  assert.deepEqual(resolveProposalFocus('prop-9', proposals, null, false), {
    kind: 'focus',
    focusId: 'prop-9',
    targetId: null,
  });
});
