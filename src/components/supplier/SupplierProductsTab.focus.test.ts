import { it } from 'node:test';
import assert from 'node:assert/strict';
import {
  proposalStatusCounts,
  resolveProposalFocus,
  supplierProductStatusLabel,
  supplierStockNeedsOrigin,
  resolveSupplierOriginDisplay,
  isStockOriginRequiredError,
} from './SupplierProductsTab';
import { isSupplierProductActive } from '../../services/supplierApi';

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

// E1: ONE shared definition — active = isActive !== false. Only an explicit
// false is inactive; a missing isActive stays active (the backend always
// sends it, domain.Product.IsActive, but older rows must not flip).
it('isSupplierProductActive: only an explicit false is inactive', () => {
  assert.equal(isSupplierProductActive({ isActive: true }), true);
  assert.equal(isSupplierProductActive({ isActive: false }), false);
  assert.equal(isSupplierProductActive({}), true);
  assert.equal(isSupplierProductActive({ isActive: undefined }), true);
  assert.equal(isSupplierProductActive({ isActive: null }), true);
});

// E1: the probe fixture — 43 products, 2 inactive. Both headers must show
// ۴۱ (the shared predicate, not two different counts), and the 2 inactive
// rows carry a «غیرفعال» badge, never «تأییدشده».
it('E1 fixture 43 products / 2 inactive → 41 active, inactive rows labelled غیرفعال', () => {
  const products = [
    ...Array.from({ length: 41 }, (_, i) => ({ id: `active-${i}`, isActive: true })),
    { id: 'prod-tapin-test-20261028', isActive: false },
    { id: 'prod-1793281034601', isActive: false },
  ];
  assert.equal(products.filter(isSupplierProductActive).length, 41);
  assert.deepEqual(
    products.filter((p) => !isSupplierProductActive(p)).map((p) => supplierProductStatusLabel(p as any)),
    ['غیرفعال', 'غیرفعال'],
  );
  assert.equal(
    products.filter(isSupplierProductActive).every((p) => supplierProductStatusLabel(p as any) === 'تأییدشده'),
    true,
  );
});

// G4: the stock-edit pre-check keys off the product's own origin, and only
// for supplier_direct rows — warehouse-fulfilment stock is not declared
// through this path.
it('supplierStockNeedsOrigin: only supplier_direct rows without an origin', () => {
  assert.equal(supplierStockNeedsOrigin({ fulfillmentMethod: 'supplier_direct' }), true);
  assert.equal(supplierStockNeedsOrigin({ fulfillmentMethod: 'supplier_direct', originSupplierWarehouseId: '' }), true);
  assert.equal(supplierStockNeedsOrigin({ fulfillmentMethod: 'supplier_direct', originSupplierWarehouseId: 'wh-1' }), false);
  assert.equal(supplierStockNeedsOrigin({ fulfillmentMethod: 'warehouse' }), false);
  assert.equal(supplierStockNeedsOrigin({ fulfillmentMethod: 'warehouse', originSupplierWarehouseId: 'wh-1' }), false);
  assert.equal(supplierStockNeedsOrigin({}), false);
});

// G4: honest origin display — a NULL origin reads «مبدأ مشخص نشده» even
// when a profile city exists; the profile city is not an origin.
it('resolveSupplierOriginDisplay: NULL origin is مبدأ مشخص نشده, never the profile city', () => {
  assert.equal(resolveSupplierOriginDisplay({ originSupplierWarehouseId: undefined }, undefined), 'مبدأ مشخص نشده');
  assert.equal(resolveSupplierOriginDisplay({}, undefined), 'مبدأ مشخص نشده');
  assert.equal(resolveSupplierOriginDisplay({ originSupplierWarehouseId: 'wh-1' }, undefined), 'انبار غیرفعال‌شده');
  assert.equal(
    resolveSupplierOriginDisplay({ originSupplierWarehouseId: 'wh-1' }, { name: 'انبار اصفهان', cityName: 'اصفهان' }),
    'انبار اصفهان — اصفهان',
  );
});

// G4: the 409 detector matches only the origin-required failure — the
// inline guidance must not swallow other stock errors (below-held,
// invalid input) into the origin flow.
it('isStockOriginRequiredError: matches only the 409 origin message', () => {
  const origin409: any = new Error('برای اعلام موجودی جدید، انبار فعال متعلق به تأمین‌کننده را مشخص کنید');
  origin409.status = 409;
  assert.equal(isStockOriginRequiredError(origin409), true);
  const origin400: any = new Error('برای اعلام موجودی جدید، انبار فعال متعلق به تأمین‌کننده را مشخص کنید');
  origin400.status = 400;
  assert.equal(isStockOriginRequiredError(origin400), false);
  const other409: any = new Error('موجودی کل نباید از موجودی نگهداری‌شده در محل‌های دیگر کمتر باشد');
  other409.status = 409;
  assert.equal(isStockOriginRequiredError(other409), false);
  assert.equal(isStockOriginRequiredError(new Error('ذخیره موجودی انجام نشد.')), false);
  assert.equal(isStockOriginRequiredError(null), false);
  assert.equal(isStockOriginRequiredError('not an error'), false);
});
