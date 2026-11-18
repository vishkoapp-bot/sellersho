import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  QuoteSession,
} from '../services/shippingApi';
import type { QuoteSignatureInput, ShippingQuote } from '../services/shippingApi';
import {
  selectionForOrder,
  stageAddressSelection,
  stageSelectionForOrder,
  takeStagedAddressSelection,
} from '../services/addressApi';
import type { SavedAddress } from '../services/addressApi';
import { resubmitAfterMismatch, guardedReclaimSubmit, applyReclaimOutcome } from './CartDrawer';
import {
  CHECKOUT_FINGERPRINT_CONFLICT_MESSAGE,
  isCheckoutFingerprintConflictError,
} from '../services/checkoutAttempt';
import type { Order } from '../types';

// CHECKOUT-02 fix regressions (critic blockers B1/B2, should-fix S1).
// Round 2 (fix-critic B3/S2/S3, note N1) is appended below.
// Round 3 (fix-2-critic blocker B4) is appended at the end.
// Same harness rules as CartDrawer.quote.test.tsx: no browser, no
// jsdom — the session, the staging store and the exported reclaim
// helpers are driven directly. The drawer submit wiring that routes
// a fingerprint-conflict error into guardedReclaimSubmit (S1) and
// the drawer effects that clear the staged selection on direct-buy
// entry / drawer close (B3) are verified by code read, not by a test
// here: handleCheckoutSubmit and the effects are not exported and
// the drawer cannot be rendered in this environment (see the SCOPE
// NOTE in CartDrawer.quote.test.tsx); the guarded core, the reclaim
// outcome helper and the staging store they call are fully covered
// below. The N1 inner-catch routing (reclaim-time
// shipping_quote_changed → beginMismatch) is likewise code-read
// only: its detector is already covered in CartDrawer.quote.test.tsx
// and the branch itself lives in the unrenderable drawer catch.

const tick = async () => {
  await new Promise((resolve) => setImmediate(resolve));
  await new Promise((resolve) => setImmediate(resolve));
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

const quote = (quoteId: string, totalShipping: number): ShippingQuote => ({
  success: true,
  quoteId,
  legs: [{ legIndex: 0, originId: 'origin-1', originLabel: 'انبار مرکزی', fare: totalShipping, weightGrams: 500, boxLabel: 'سایز ۴' }],
  totalShipping,
  allocations: [{ productId: 'p1', variantId: 'v1', quantity: 1, legIndex: 0 }],
});

const destA = { province: 'تهران', city: 'تهران', address: 'خیابان اول ۱' };
const line = (over: Record<string, unknown> = {}) => ({
  productId: 'p1', variantId: 'v1', quantity: 1, storeId: 'store-central', addonIds: [] as string[], ...over,
});
const inputA: QuoteSignatureInput = { storeId: 'store-central', items: [line()], destination: destA };
const GOODS = 100000;

interface CallRecord {
  items: unknown;
  destination: unknown;
  signal: AbortSignal;
}

function makeSession() {
  const calls: CallRecord[] = [];
  const pendings: ReturnType<typeof deferred<ShippingQuote>>[] = [];
  const fetch = (items: Parameters<ConstructorParameters<typeof QuoteSession>[0]>[0], destination: Parameters<ConstructorParameters<typeof QuoteSession>[0]>[1], opts: { signal: AbortSignal }) => {
    const d = deferred<ShippingQuote>();
    calls.push({ items, destination, signal: opts.signal });
    pendings.push(d);
    return d.promise;
  };
  const session = new QuoteSession(fetch as never);
  return { session, calls, pendings };
}

async function makeAcknowledgedSession() {
  const { session, pendings } = makeSession();
  session.request(inputA, GOODS);
  pendings[0].resolve(quote('sq-A', 40000));
  await tick();
  session.beginMismatch();
  pendings[1].resolve(quote('sq-B', 55000));
  await tick();
  session.acknowledgeMismatch();
  assert.equal(session.getSubmittableQuoteId(), 'sq-B');
  return session;
}

// --- B1: the Q8 deselection must stick in staging -------------------------

const savedAddress: SavedAddress = {
  id: 'addr-S',
  fullName: 'مشتری تست',
  phone: '09123456789',
  province: 'تهران',
  city: 'تهران',
  postalCode: '1234567890',
  address: 'خیابان اول ۱',
};
const book = [savedAddress];
const savedFields = { province: 'تهران', city: 'تهران', address: 'خیابان اول ۱', postalCode: '1234567890' };

test('B1(i) postal-code-only edit: staged selection carries NO addressId', () => {
  // Defence in depth alone: the edited postal code no longer matches
  // the saved address, so even without the deselection flag the
  // field-match branch must not re-attach it (postalCode is now part
  // of the match key). The staged value is what placeOrder consumes.
  const staged = stageSelectionForOrder({ ...savedFields, postalCode: '0987654321' }, book, null, true);
  assert.equal(staged?.addressId, undefined);
  assert.deepEqual(takeStagedAddressSelection(), { saveAddress: true });
  // And with the explicit deselection the form records on a
  // postal-code patch (updateCustomerInfo), the same must hold.
  const staged2 = stageSelectionForOrder({ ...savedFields, postalCode: '0987654321' }, book, null, true, true);
  assert.equal(staged2?.addressId, undefined);
  assert.deepEqual(takeStagedAddressSelection(), { saveAddress: true });
});

test('B1(ii) explicit new-address click: staged selection carries NO addressId', () => {
  // Fields exactly equal to the saved address, no selection, and the
  // deselection flag the form sets in selectNewAddress: the old code
  // staged { addressId: 'addr-S' } here, defeating the choice.
  const staged = stageSelectionForOrder(savedFields, book, null, true, true);
  assert.equal(staged?.addressId, undefined);
  assert.deepEqual(takeStagedAddressSelection(), { saveAddress: true });
});

test('B1(iii) picking a saved address still stages it (no regression)', () => {
  // Explicit pick: selectedId set, deselection flag cleared by
  // applySavedAddress — the saved address must stage as before.
  // (The fields also match the saved address, so the field-match
  // branch fires first and adds saveAddress — same id either way.)
  const staged = stageSelectionForOrder(savedFields, book, 'addr-S', true, false);
  assert.equal(staged?.addressId, 'addr-S');
  assert.deepEqual(takeStagedAddressSelection(), { addressId: 'addr-S', saveAddress: true });
  // Untouched fields matching a saved address still field-match when
  // nothing was ever deselected (pre-existing live behaviour).
  const matched = stageSelectionForOrder(savedFields, book, null, true, false);
  assert.equal(matched?.addressId, 'addr-S');
  assert.deepEqual(takeStagedAddressSelection(), { addressId: 'addr-S', saveAddress: true });
  // The flag blocks the field-match branch only: an explicitly picked
  // id still wins even if a stale flag were present.
  assert.equal(selectionForOrder(savedFields, book, 'addr-S', true, true)?.addressId, 'addr-S');
});

// --- B2: recover-before-abandon in the reclaim ----------------------------

test('B2 lost response on the reclaim submit, then retry: exactly one order, the recovered one', async () => {
  const session = await makeAcknowledgedSession();
  const orderO1 = { id: 'O1', orderNumber: 'ORD-O1' } as unknown as Order;
  const serverOrders: string[] = [];
  const calls: string[] = [];
  let localAttempt: string | null = 'att-1';
  let serverAttempt2: { state: 'open' | 'completed'; orderId?: string } | null = null;
  let abandonCalls = 0;
  let submitCalls = 0;
  // Mocks replicate startNewCheckout's documented semantics
  // (OrderContext.tsx): abandon returns the server attempt — a
  // completed attempt keeps its order — but the frontend discards
  // that order and clears the local record.
  const recoverCheckout = async (): Promise<Order | null> => {
    calls.push('recover');
    if (localAttempt === 'att-2' && serverAttempt2?.state === 'completed') return orderO1;
    return null;
  };
  const abandonCheckout = async (): Promise<void> => {
    calls.push('abandon');
    abandonCalls += 1;
    localAttempt = null;
  };
  const submitOrder = async (qid: string): Promise<Order> => {
    calls.push(`submit:${qid}`);
    submitCalls += 1;
    assert.equal(qid, 'sq-B');
    if (submitCalls === 1) {
      // Reclaim submit #1: the server creates O1, the response is lost.
      localAttempt = 'att-2';
      serverAttempt2 = { state: 'completed', orderId: 'O1' };
      serverOrders.push('O1');
      throw new Error('network dropped');
    }
    localAttempt = 'att-3';
    serverOrders.push('O2');
    return { id: 'O2', orderNumber: 'ORD-O2' } as unknown as Order;
  };
  const args = { session, abandonCheckout, submitOrder, recoverCheckout };

  // Resubmit #1 (the reclaim): no order exists yet, so recovery is
  // null and abandon + submit run; the submit creates O1 but its
  // response is lost.
  await assert.rejects(resubmitAfterMismatch(args), /network dropped/);
  assert.deepEqual(calls, ['recover', 'abandon', 'submit:sq-B'], 'recovery is checked before abandoning');
  assert.equal(abandonCalls, 1);
  assert.equal(submitCalls, 1);
  assert.deepEqual(serverOrders, ['O1']);
  assert.equal(session.canSubmit(), true, 'retry stays possible after the lost response');

  // Retry via the submit button: recover-before-abandon must return
  // the recovered O1 and abandon/submit NOTHING.
  const retried = await resubmitAfterMismatch(args);
  assert.equal(retried.order.id, 'O1');
  assert.equal(retried.recovered, true, 'the retry returns the recovered order, flagged as recovered');
  assert.deepEqual(calls, ['recover', 'abandon', 'submit:sq-B', 'recover'], 'the retry recovers and stops there');
  assert.equal(abandonCalls, 1, 'the retry must not abandon the completed attempt');
  assert.equal(submitCalls, 1, 'the retry must not submit a second order');
  assert.deepEqual(serverOrders, ['O1'], 'exactly one order exists for the purchase');
});

test('B2 recover returns an order on the first resubmit: abandon/submit never run', async () => {
  const session = await makeAcknowledgedSession();
  const orderO0 = { id: 'O0', orderNumber: 'ORD-O0' } as unknown as Order;
  let abandonCalls = 0;
  let submitCalls = 0;
  const out = await resubmitAfterMismatch({
    session,
    abandonCheckout: async () => { abandonCalls += 1; },
    submitOrder: async () => { submitCalls += 1; return { id: 'OX' } as unknown as Order; },
    recoverCheckout: async () => orderO0,
  });
  assert.equal(out.order.id, 'O0');
  assert.equal(out.recovered, true);
  assert.equal(abandonCalls, 0);
  assert.equal(submitCalls, 0);
});

test('B2 recover returns null: abandon + submit still run (no behaviour change)', async () => {
  const session = await makeAcknowledgedSession();
  const calls: string[] = [];
  const out = await resubmitAfterMismatch({
    session,
    abandonCheckout: async () => { calls.push('abandon'); },
    submitOrder: async (qid: string) => { calls.push(`submit:${qid}`); return { id: 'O9', orderNumber: 'ORD-O9' } as unknown as Order; },
    recoverCheckout: async () => null,
  });
  assert.equal(out.order.id, 'O9');
  assert.equal(out.recovered, false, 'a fresh abandon+submit is not a recovery');
  assert.deepEqual(calls, ['abandon', 'submit:sq-B']);
});

test('B2 resubmitAfterMismatch without recoverCheckout still abandons + submits (old call shape)', async () => {
  const session = await makeAcknowledgedSession();
  const calls: string[] = [];
  const out = await resubmitAfterMismatch({
    session,
    abandonCheckout: async () => { calls.push('abandon'); },
    submitOrder: async (qid: string) => { calls.push(`submit:${qid}`); return { id: 'O9', orderNumber: 'ORD-O9' } as unknown as Order; },
  });
  assert.equal(out.order.id, 'O9');
  assert.equal(out.recovered, false);
  assert.deepEqual(calls, ['abandon', 'submit:sq-B']);
});

// --- S1: fingerprint-conflict submits go through the guarded reclaim ------

test('S1 fingerprint-conflict error is detected from the real claim message', () => {
  assert.equal(isCheckoutFingerprintConflictError(new Error(CHECKOUT_FINGERPRINT_CONFLICT_MESSAGE)), true);
  assert.equal(isCheckoutFingerprintConflictError(new Error('shipping_quote_changed')), false);
  assert.equal(isCheckoutFingerprintConflictError(new Error('هزینه ارسال تغییر کرده است؛ لطفاً هزینه به‌روز را بررسی و دوباره تلاش کنید.')), false);
  assert.equal(isCheckoutFingerprintConflictError(new Error('some other failure')), false);
  assert.equal(isCheckoutFingerprintConflictError(null), false);
});

test('S1 guarded reclaim: recovered order short-circuits abandon + submit', async () => {
  const orderO1 = { id: 'O1', orderNumber: 'ORD-O1' } as unknown as Order;
  let abandonCalls = 0;
  let submitCalls = 0;
  const out = await guardedReclaimSubmit({
    quoteId: 'sq-B',
    abandonCheckout: async () => { abandonCalls += 1; },
    submitOrder: async () => { submitCalls += 1; return { id: 'O2' } as unknown as Order; },
    recoverCheckout: async () => orderO1,
  });
  assert.equal(out.order.id, 'O1');
  assert.equal(out.recovered, true);
  assert.equal(abandonCalls, 0);
  assert.equal(submitCalls, 0);
});

test('S1 guarded reclaim: null recovery abandons then submits, in that order', async () => {
  const calls: string[] = [];
  const out = await guardedReclaimSubmit({
    quoteId: 'sq-B',
    abandonCheckout: async () => { calls.push('abandon'); },
    submitOrder: async (qid: string) => { calls.push(`submit:${qid}`); return { id: 'O2', orderNumber: 'ORD-O2' } as unknown as Order; },
    recoverCheckout: async () => null,
  });
  assert.equal(out.order.id, 'O2');
  assert.equal(out.recovered, false);
  assert.deepEqual(calls, ['abandon', 'submit:sq-B']);
});

// --- Round 2: B3 (stale staged address leaks into direct buy),
// S2 (recovered flag), S3 (cart clear on B2 recovery only) ---------

// The drawer effects that perform the clear (direct-buy prefill
// effect, mode-switch effect, handleClose) are not exported and the
// drawer cannot be rendered here, so the tests below drive the exact
// call those effects make — stageAddressSelection(null) — against
// the real staging store, plus the real effective-address expression
// placeOrder evaluates (OrderContext.tsx:327-330:
// effectiveAddressId = addressId ?? stagedAddress?.addressId).
// The three call sites themselves are code-read verified.

// placeOrder's effective-address expression (OrderContext.tsx:327-330),
// as a function so the test drives the same logic without TS
// complaining about literal-undefined ?? operands.
const effectiveAddressId = (
  addressId: string | undefined,
  staged: { addressId?: string } | null,
): string | undefined => addressId ?? staged?.addressId;

test('B3 entering direct-buy mode clears the staged cart address; an edited direct-buy submit consumes no addressId', () => {
  // 1. Cart checkout stages the saved address (explicit pick).
  stageSelectionForOrder(savedFields, book, 'addr-S', true, false);
  // Without the clear, the staged value is exactly what an edited
  // direct-buy submit would fall back to (the fix-critic's proof):
  // the edited direct buy passes address=undefined, so placeOrder
  // would send the stale cart addressId.
  const leaked = takeStagedAddressSelection();
  assert.deepEqual(leaked, { addressId: 'addr-S', saveAddress: true });
  const leakedEffectiveAddressId = effectiveAddressId(undefined, leaked);
  assert.equal(leakedEffectiveAddressId, 'addr-S', 'pre-fix behaviour: the stale cart address leaks');

  // 2. Stage it again, then simulate entering direct-buy mode: the
  // drawer clears the staged selection on mode entry.
  stageSelectionForOrder(savedFields, book, 'addr-S', true, false);
  stageAddressSelection(null);
  assert.equal(takeStagedAddressSelection(), null, 'entering direct-buy mode clears the staged selection');

  // 3. An edited direct-buy submit now consumes no addressId: it
  // passes address=undefined and nothing staged survives, so the
  // order is computed from the typed fields.
  stageSelectionForOrder(savedFields, book, 'addr-S', true, false);
  stageAddressSelection(null);
  const stagedForSubmit = takeStagedAddressSelection();
  const editedEffectiveAddressId = effectiveAddressId(undefined, stagedForSubmit);
  assert.equal(editedEffectiveAddressId, undefined);

  // 4. The direct-buy prefill address itself still works: an
  // UNEDITED direct buy passes its explicit addressId, which wins
  // over any staged value (fields identical to the saved address
  // make the overwrite a no-op).
  stageSelectionForOrder(savedFields, book, 'addr-S', true, false);
  const uneditedStaged = takeStagedAddressSelection();
  const uneditedEffectiveAddressId = effectiveAddressId('addr-D', uneditedStaged);
  assert.equal(uneditedEffectiveAddressId, 'addr-D');
});

test('B3 drawer close clears the staged selection (defence in depth)', () => {
  stageSelectionForOrder(savedFields, book, 'addr-S', true, false);
  // handleClose's clear: the same stageAddressSelection(null) call.
  stageAddressSelection(null);
  assert.equal(takeStagedAddressSelection(), null);
});

// --- S2: the recovered flag threads out of the reclaim --------------

test('S2 recovered flag: true on recover-hit, false on fresh abandon+submit', async () => {
  const orderO1 = { id: 'O1', orderNumber: 'ORD-O1' } as unknown as Order;
  const hit = await guardedReclaimSubmit({
    quoteId: 'sq-B',
    abandonCheckout: async () => { throw new Error('must not abandon on a recover-hit'); },
    submitOrder: async () => { throw new Error('must not submit on a recover-hit'); },
    recoverCheckout: async () => orderO1,
  });
  assert.equal(hit.recovered, true);
  assert.equal(hit.order.id, 'O1');

  const calls: string[] = [];
  const fresh = await guardedReclaimSubmit({
    quoteId: 'sq-B',
    abandonCheckout: async () => { calls.push('abandon'); },
    submitOrder: async (qid: string) => { calls.push(`submit:${qid}`); return { id: 'O2', orderNumber: 'ORD-O2' } as unknown as Order; },
    recoverCheckout: async () => null,
  });
  assert.equal(fresh.recovered, false);
  assert.equal(fresh.order.id, 'O2');
  assert.deepEqual(calls, ['abandon', 'submit:sq-B']);
});

// --- S3: cart clear on B2 recovery return, never on S1 recovery ----

test('S3 B2 recovery return clears the cart; a fresh resubmit does not clear it here', async () => {
  const session = await makeAcknowledgedSession();
  const orderO1 = { id: 'O1', orderNumber: 'ORD-O1' } as unknown as Order;
  const outcome = await resubmitAfterMismatch({
    session,
    abandonCheckout: async () => { throw new Error('must not abandon on a recover-hit'); },
    submitOrder: async () => { throw new Error('must not submit on a recover-hit'); },
    recoverCheckout: async () => orderO1,
  });
  assert.equal(outcome.recovered, true);
  let clearCalls = 0;
  const placed: { order: Order | null; recovered: boolean | null } = { order: null, recovered: null };
  applyReclaimOutcome(outcome, {
    clearCartOnRecovery: true,
    clearCart: () => { clearCalls += 1; },
    onPlaced: (order, recovered) => { placed.order = order; placed.recovered = recovered; },
  });
  assert.equal(clearCalls, 1, 'B2 recovery clears the cart like a normal success');
  assert.equal(placed.order?.id, 'O1');
  assert.equal(placed.recovered, true);

  // A fresh (non-recovery) resubmit: the cart is cleared by the
  // StoreContext placeOrder wrapper on its .then(), not here.
  const session2 = await makeAcknowledgedSession();
  const fresh = await resubmitAfterMismatch({
    session: session2,
    abandonCheckout: async () => undefined,
    submitOrder: async () => ({ id: 'O9', orderNumber: 'ORD-O9' }) as unknown as Order,
    recoverCheckout: async () => null,
  });
  assert.equal(fresh.recovered, false);
  applyReclaimOutcome(fresh, {
    clearCartOnRecovery: true,
    clearCart: () => { clearCalls += 1; },
    onPlaced: (order) => { placed.order = order; },
  });
  assert.equal(clearCalls, 1, 'a fresh submit must not clear the cart a second time here');
});

test('S3 S1 recovery does NOT clear the cart (new payload still unpurchased)', async () => {
  const orderO1 = { id: 'O1', orderNumber: 'ORD-O1' } as unknown as Order;
  const outcome = await guardedReclaimSubmit({
    quoteId: 'sq-B',
    abandonCheckout: async () => { throw new Error('must not abandon on a recover-hit'); },
    submitOrder: async () => { throw new Error('must not submit on a recover-hit'); },
    recoverCheckout: async () => orderO1,
  });
  assert.equal(outcome.recovered, true);
  let clearCalls = 0;
  const placed: { order: Order | null; recovered: boolean | null } = { order: null, recovered: null };
  applyReclaimOutcome(outcome, {
    clearCartOnRecovery: false,
    clearCart: () => { clearCalls += 1; },
    onPlaced: (order, recovered) => { placed.order = order; placed.recovered = recovered; },
  });
  assert.equal(clearCalls, 0, 'S1 recovery keeps the cart: the new payload is still unpurchased');
  assert.equal(placed.order?.id, 'O1');
  assert.equal(placed.recovered, true, 'the recovered flag still reaches the success path for the distinct copy');
});

// --- Round 3: B4 (direct-buy B2 recovery must NOT clear the cart) --
//
// The mismatch branch (CartDrawer.tsx) is mode-agnostic: it runs for
// cart checkout AND for direct buy. Its flag is now
// `clearCartOnRecovery: !isDirectBuy`, so the flag value the branch
// computes is false in direct mode and true in cart mode. The branch
// wiring itself is code-read only (the drawer cannot be rendered
// here — same scope note as B3 above); the tests below drive a real
// resubmitAfterMismatch recover-hit through applyReclaimOutcome with
// exactly the flag value the branch computes for each mode, and
// assert the cart-clear count in both.

test('B4 direct-mode B2 recover-hit clears the cart ZERO times; cart-mode recover-hit still clears', async () => {
  const orderO1 = { id: 'O1', orderNumber: 'ORD-O1' } as unknown as Order;
  const recoverHit = async (session: QuoteSession) => resubmitAfterMismatch({
    session,
    abandonCheckout: async () => { throw new Error('must not abandon on a recover-hit'); },
    submitOrder: async () => { throw new Error('must not submit on a recover-hit'); },
    recoverCheckout: async () => orderO1,
  });

  // Direct mode: the mismatch branch computes clearCartOnRecovery
  // as !isDirectBuy = false — the user's unrelated background cart
  // must survive the recovery (StoreContext invariant: "A direct
  // order must NOT clear the cart").
  const directSession = await makeAcknowledgedSession();
  const directOutcome = await recoverHit(directSession);
  assert.equal(directOutcome.recovered, true);
  let clearCalls = 0;
  const placedDirect: { order: Order | null } = { order: null };
  applyReclaimOutcome(directOutcome, {
    clearCartOnRecovery: !true, // the branch's flag in direct mode (!isDirectBuy)
    clearCart: () => { clearCalls += 1; },
    onPlaced: (order) => { placedDirect.order = order; },
  });
  assert.equal(clearCalls, 0, 'direct-mode B2 recovery must NOT clear the cart');
  assert.equal(placedDirect.order?.id, 'O1', 'the recovered order is still placed');

  // Cart mode: the mismatch branch computes clearCartOnRecovery as
  // !isDirectBuy = true — the purchased cart clears like a normal
  // success (S3 behaviour, unchanged).
  const cartSession = await makeAcknowledgedSession();
  const cartOutcome = await recoverHit(cartSession);
  assert.equal(cartOutcome.recovered, true);
  const placedCart: { order: Order | null } = { order: null };
  applyReclaimOutcome(cartOutcome, {
    clearCartOnRecovery: !false, // the branch's flag in cart mode (!isDirectBuy)
    clearCart: () => { clearCalls += 1; },
    onPlaced: (order) => { placedCart.order = order; },
  });
  assert.equal(clearCalls, 1, 'cart-mode B2 recovery still clears the cart once');
  assert.equal(placedCart.order?.id, 'O1');
});
