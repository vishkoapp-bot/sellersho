import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  buildQuoteSignature,
  QuoteSession,
  isQuoteChangedError,
} from '../services/shippingApi';
import type { QuoteSignatureInput, ShippingQuote } from '../services/shippingApi';
import { resubmitAfterMismatch } from './CartDrawer';
import { addressPatchClearsSavedSelection } from './CartCheckoutForm';

// CHECKOUT-02 regression file (G-15). Every test is named after its
// gate. Quote fetching is mocked at the shippingApi.quote layer with
// controllable deferred promises; no browser is launched.
//
// SCOPE NOTE: this repo has no jsdom / testing-library and Puppeteer
// cannot run as root on this VM, so the drawer cannot be rendered
// with live effects here. The tests therefore drive QuoteSession —
// the exact state machine the drawer binds to 1:1 (CartDrawer holds
// no quote state of its own: its submit gate, submit quoteId, retry,
// stale display and mismatch panel all read this session) — plus the
// drawer's exported resubmitAfterMismatch and the form's exported
// addressPatchClearsSavedSelection. A live-browser pass is still the
// right final check for the rendered drawer.

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
const destB = { province: 'اصفهان', city: 'اصفهان', address: 'خیابان دوم ۲' };
const line = (over: Record<string, unknown> = {}) => ({
  productId: 'p1', variantId: 'v1', quantity: 1, storeId: 'store-central', addonIds: [] as string[], ...over,
});
const inputA: QuoteSignatureInput = { storeId: 'store-central', items: [line()], destination: destA };
const inputB: QuoteSignatureInput = { storeId: 'store-central', items: [line()], destination: destB };
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

test('G-01 deferred A/B, B requested second and slower: B wins', async () => {
  const { session, calls, pendings } = makeSession();
  session.request(inputA, GOODS);
  session.request(inputB, GOODS);
  assert.equal(calls.length, 2);
  // A resolves first while B is still pending: it must not become
  // the displayed quote and submit must stay blocked.
  pendings[0].resolve(quote('sq-A', 40000));
  await tick();
  assert.equal(session.status, 'loading');
  assert.equal(session.quote === null, true);
  assert.equal(session.canSubmit(), false);
  assert.equal(session.getSubmittableQuoteId(), null);
  pendings[1].resolve(quote('sq-B', 50000));
  await tick();
  assert.equal(session.status, 'quoted');
  assert.equal(session.quote?.quoteId, 'sq-B');
  assert.equal(session.quote?.totalShipping, 50000);
  assert.equal(session.canSubmit(), true);
  assert.equal(session.getSubmittableQuoteId(), 'sq-B');
});

test('G-02 deferred A/B, A requested second and slower: A wins', async () => {
  const { session, pendings } = makeSession();
  session.request(inputB, GOODS);
  session.request(inputA, GOODS);
  pendings[0].resolve(quote('sq-B', 50000));
  await tick();
  assert.equal(session.status, 'loading');
  assert.equal(session.quote === null, true);
  assert.equal(session.canSubmit(), false);
  pendings[1].resolve(quote('sq-A', 40000));
  await tick();
  assert.equal(session.status, 'quoted');
  assert.equal(session.quote?.quoteId, 'sq-A');
  assert.equal(session.quote?.totalShipping, 40000);
  assert.equal(session.getSubmittableQuoteId(), 'sq-A');
});

test('G-03 address selection / edit refetches and submits the new quote', async () => {
  const { session, calls, pendings } = makeSession();
  // (a) A saved address is applied, then the city is edited: the new
  // request carries the edited destination and submit uses its quote.
  session.request(inputA, GOODS);
  pendings[0].resolve(quote('sq-A', 40000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-A');
  const edited: QuoteSignatureInput = { storeId: 'store-central', items: [line()], destination: { ...destA, city: 'کرج' } };
  session.request(edited, GOODS);
  assert.equal(calls.length, 2);
  assert.deepEqual(calls[1].destination, { ...destA, city: 'کرج' });
  pendings[1].resolve(quote('sq-edited', 45000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-edited');
  // (b) Transient pair: province picked from the list clears the
  // city — no request is issued for province + empty city; only the
  // final province+city pair is ever quoted.
  const before = calls.length;
  session.request({ storeId: 'store-central', items: [line()], destination: { province: 'فارس', city: '', address: 'خیابان سوم ۳' } }, GOODS);
  assert.equal(session.status, 'warning');
  assert.equal(calls.length, before, 'a transient province+empty-city pair must never be quoted');
  session.request({ storeId: 'store-central', items: [line()], destination: { province: 'فارس', city: 'شیراز', address: 'خیابان سوم ۳' } }, GOODS);
  assert.equal(calls.length, before + 1);
  assert.deepEqual(calls[before].destination, { province: 'فارس', city: 'شیراز', address: 'خیابان سوم ۳' });
  pendings[before].resolve(quote('sq-final', 60000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-final');
  // (c) Q8: an address-field edit clears the saved selection, a
  // name/phone edit does not.
  assert.equal(addressPatchClearsSavedSelection({ province: 'تهران' }), true);
  assert.equal(addressPatchClearsSavedSelection({ city: 'کرج' }), true);
  assert.equal(addressPatchClearsSavedSelection({ address: 'خیابان ۱' }), true);
  assert.equal(addressPatchClearsSavedSelection({ postalCode: '1234567890' }), true);
  assert.equal(addressPatchClearsSavedSelection({ fullName: 'علی رضایی' }), false);
  assert.equal(addressPatchClearsSavedSelection({ phone: '09120000000' }), false);
});

test('G-04 quantity, addon, and SKU changes each refetch / revalidate the quote', async () => {
  const { session, calls, pendings } = makeSession();
  session.request(inputA, GOODS);
  pendings[0].resolve(quote('sq-1', 40000));
  await tick();
  // (i) quantity 1 -> 2: a new request reflecting the change.
  session.request({ ...inputA, items: [line({ quantity: 2 })] }, GOODS);
  assert.equal(calls.length, 2);
  assert.deepEqual(calls[1].items, [{ productId: 'p1', variantId: 'v1', quantity: 2 }]);
  pendings[1].resolve(quote('sq-qty2', 50000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-qty2');
  // (ii) variant/SKU switch: a new request reflecting the change.
  session.request({ ...inputA, items: [line({ quantity: 2, variantId: 'v2' })] }, GOODS);
  assert.equal(calls.length, 3);
  assert.deepEqual(calls[2].items, [{ productId: 'p1', variantId: 'v2', quantity: 2 }]);
  pendings[2].resolve(quote('sq-v2', 52000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-v2');
  // (iii) addon added, then removed (Q3: fee-only — a signature bump
  // + revalidation request, but NO new lines in the quote request).
  const withAddon: QuoteSignatureInput = { ...inputA, items: [line({ quantity: 2, variantId: 'v2', addonIds: ['addon-1'] })] };
  session.request(withAddon, GOODS);
  assert.equal(calls.length, 4, 'adding an addon must issue a revalidation request');
  assert.deepEqual(calls[3].items, [{ productId: 'p1', variantId: 'v2', quantity: 2 }], 'addons add no shippable lines');
  pendings[3].resolve(quote('sq-addon', 52000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-addon');
  session.request({ ...inputA, items: [line({ quantity: 2, variantId: 'v2', addonIds: [] })] }, GOODS);
  assert.equal(calls.length, 5, 'removing an addon must issue a revalidation request');
  pendings[4].resolve(quote('sq-no-addon', 52000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-no-addon');
});

test('G-05 direct-purchase vs normal-cart switch: no cross-mode quote is displayed or submitted', async () => {
  // Cart -> direct.
  {
    const { session, calls, pendings } = makeSession();
    session.request(inputA, GOODS);
    session.cancelAndClear(); // drawer mode-switch effect
    assert.equal(session.quote === null, true);
    assert.equal(session.canSubmit(), false);
    assert.equal(calls[0].signal.aborted, true);
    pendings[0].resolve(quote('sq-cart', 40000));
    await tick();
    assert.equal(session.quote, null, 'a late cart response must never land in direct checkout');
    assert.equal(session.canSubmit(), false);
    const direct: QuoteSignatureInput = { storeId: 'store-direct', items: [line({ productId: 'p-direct' })], destination: destA };
    session.request(direct, 50000);
    pendings[1].resolve(quote('sq-direct', 30000));
    await tick();
    assert.equal(session.getSubmittableQuoteId(), 'sq-direct');
  }
  // Direct -> cart.
  {
    const { session, pendings } = makeSession();
    const direct: QuoteSignatureInput = { storeId: 'store-direct', items: [line({ productId: 'p-direct' })], destination: destA };
    session.request(direct, 50000);
    session.cancelAndClear();
    pendings[0].resolve(quote('sq-direct', 30000));
    await tick();
    assert.equal(session.quote, null, 'a late direct response must never land in cart checkout');
    session.request(inputA, GOODS);
    pendings[1].resolve(quote('sq-cart', 40000));
    await tick();
    assert.equal(session.getSubmittableQuoteId(), 'sq-cart');
  }
});

test('G-06 drawer close / reopen: stale responses are discarded, reopen refetches', async () => {
  const { session, calls, pendings } = makeSession();
  session.request(inputA, GOODS);
  session.cancelAndClear(); // drawer close
  assert.equal(session.quote === null, true);
  assert.equal(session.canSubmit(), false);
  pendings[0].resolve(quote('sq-pre-close', 40000));
  await tick();
  assert.equal(session.quote, null, 'a pre-close response must never be displayed or submittable on reopen');
  assert.equal(session.canSubmit(), false);
  // Reopen refetches with the same cart + typed address (the session
  // owns neither, so closing cannot lose them — the reopen request
  // proves they survive intact).
  session.request(inputA, GOODS);
  assert.equal(calls.length, 2);
  assert.deepEqual(calls[1].items, calls[0].items);
  assert.deepEqual(calls[1].destination, calls[0].destination);
  pendings[1].resolve(quote('sq-fresh', 42000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-fresh');
});

test('G-07 failed quote: error is visible, retry works without losing form or cart', async () => {
  // shipping_unavailable: visible error, retry issues exactly one
  // new request, inputs unchanged.
  {
    const { session, calls, pendings } = makeSession();
    session.request(inputA, GOODS);
    pendings[0].reject({ body: { error: 'shipping_unavailable' } });
    await tick();
    assert.equal(session.status, 'error');
    assert.equal(session.errorCode, 'shipping_unavailable');
    assert.match(session.message, /قیمت ارسال در دسترس نیست/);
    assert.doesNotMatch(session.message, /محاسبه در مرحله بعد/);
    assert.equal(session.canSubmit(), false, 'a failed request must never leave a submittable quote behind');
    assert.equal(session.getSubmittableQuoteId(), null);
    session.retry();
    assert.equal(calls.length, 2, 'retry issues exactly one new request');
    assert.deepEqual(calls[1].items, calls[0].items, 'form and cart are unchanged after failure and retry');
    assert.deepEqual(calls[1].destination, calls[0].destination);
    pendings[1].resolve(quote('sq-retry', 40000));
    await tick();
    assert.equal(session.getSubmittableQuoteId(), 'sq-retry');
  }
  // Network / timeout: distinct error state, not a silent fallback.
  {
    const { session, pendings } = makeSession();
    session.request(inputA, GOODS);
    pendings[0].reject({ status: 0, body: null });
    await tick();
    assert.equal(session.status, 'error');
    assert.equal(session.errorCode, 'quote_timeout_or_network');
    assert.match(session.message, /زمان محاسبه/);
    assert.equal(session.canSubmit(), false);
  }
  // shipping_address_required from the endpoint: warning, not error.
  {
    const { session, pendings } = makeSession();
    session.request(inputA, GOODS);
    pendings[0].reject({ body: { error: 'shipping_address_required' } });
    await tick();
    assert.equal(session.status, 'warning');
    assert.match(session.message, /نشانی کامل/);
    assert.equal(session.canSubmit(), false);
  }
  // A failed refetch retains the last good quote as visibly stale
  // (Q5) — and it is not submittable while stale.
  {
    const { session, pendings } = makeSession();
    session.request(inputA, GOODS);
    pendings[0].resolve(quote('sq-good', 40000));
    await tick();
    assert.equal(session.getSubmittableQuoteId(), 'sq-good');
    session.request(inputB, GOODS);
    pendings[1].reject({ body: { error: 'shipping_unavailable' } });
    await tick();
    assert.equal(session.status, 'error');
    assert.equal(session.quote?.quoteId, 'sq-good', 'last good quote is retained');
    assert.equal(session.displayStale, true, 'retained quote is marked stale, never current');
    assert.equal(session.canSubmit(), false);
  }
});

test('G-08 submits always use the latest matching quote', async () => {
  const { session, pendings } = makeSession();
  session.request(inputA, GOODS);
  pendings[0].resolve(quote('sq-A', 40000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-A');
  // Any input change blocks submit until the matching fresh quote resolves.
  session.request(inputB, GOODS);
  assert.equal(session.canSubmit(), false, 'submit is disabled while the matching fresh quote is pending');
  assert.equal(session.getSubmittableQuoteId(), null);
  pendings[1].resolve(quote('sq-B', 50000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-B', 'the submitted quoteId is the latest request for the current inputs');
  // Negative case: the cart is emptied mid-checkout.
  session.request({ ...inputB, items: [] }, GOODS);
  assert.equal(session.status, 'idle');
  assert.equal(session.quote === null, true);
  assert.equal(session.canSubmit(), false, 'no stale quote is submittable for an emptied cart');
});

test('G-09 generation / signature covers address/city, store, lines/SKUs/quantities, and all shipping-relevant options', () => {
  const base = buildQuoteSignature(inputA);
  const changed = (patch: Partial<QuoteSignatureInput>) =>
    buildQuoteSignature({ ...inputA, ...patch });
  const changedDest = (patch: Record<string, string>) =>
    buildQuoteSignature({ ...inputA, destination: { ...destA, ...patch } });
  assert.notEqual(changedDest({ province: 'فارس' }), base, 'province changes the signature');
  assert.notEqual(changedDest({ city: 'کرج' }), base, 'city changes the signature');
  assert.notEqual(changedDest({ address: 'خیابان دیگر ۹' }), base, 'address changes the signature');
  assert.notEqual(changedDest({ postalCode: '1234567890' }), base, 'postal code changes the signature');
  assert.notEqual(changed({ storeId: 'store-other' }), base, 'store changes the signature');
  assert.notEqual(changed({ items: [line({ productId: 'p2' })] }), base, 'product id changes the signature');
  assert.notEqual(changed({ items: [line({ variantId: 'v9' })] }), base, 'variant/SKU changes the signature');
  assert.notEqual(changed({ items: [line({ quantity: 3 })] }), base, 'quantity changes the signature');
  assert.notEqual(changed({ items: [line({ addonIds: ['addon-1'] })] }), base, 'addon selection changes the signature');
  assert.equal(buildQuoteSignature(inputA), base, 'an identical input has an identical signature');
  // A shipping-irrelevant change (recipient name / phone) is not a
  // signature input at all, so it issues no new request: an identical
  // re-request is a no-op.
  const { session, calls } = makeSession();
  session.request(inputA, GOODS);
  const afterFirst = calls.length;
  session.request(inputA, GOODS);
  assert.equal(calls.length, afterFirst, 're-requesting identical inputs must not issue a new quote request');
});

test('G-10 obsolete requests are cancelled where possible', async () => {
  const { session, calls, pendings } = makeSession();
  session.request(inputA, GOODS);
  assert.ok(calls[0].signal instanceof AbortSignal, 'an abort signal is passed through shippingApi.quote');
  session.request(inputB, GOODS);
  assert.equal(calls[0].signal.aborted, true, 'the superseded request is aborted');
  assert.equal(calls[1].signal.aborted, false);
  // The part that cannot be aborted once dispatched is still ignored
  // (covered by G-01/G-02); here the aborted request resolves late.
  pendings[0].resolve(quote('sq-A', 40000));
  await tick();
  assert.equal(session.quote === null, true);
  pendings[1].resolve(quote('sq-B', 50000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-B');
});

test('G-11 only the latest matching request updates success / error / loading', async () => {
  // (a) Latest succeeds, earlier fails afterwards: state stays quoted
  // with the latest quote — no error flash.
  {
    const { session, pendings } = makeSession();
    session.request(inputA, GOODS);
    session.request(inputB, GOODS);
    pendings[1].resolve(quote('sq-B', 50000));
    await tick();
    pendings[0].reject({ body: { error: 'shipping_unavailable' } });
    await tick();
    assert.equal(session.status, 'quoted');
    assert.equal(session.quote?.quoteId, 'sq-B');
    assert.equal(session.canSubmit(), true);
  }
  // (b) Latest fails, earlier succeeds afterwards: state stays error.
  {
    const { session, pendings } = makeSession();
    session.request(inputA, GOODS);
    session.request(inputB, GOODS);
    pendings[1].reject({ body: { error: 'shipping_unavailable' } });
    await tick();
    assert.equal(session.status, 'error');
    pendings[0].resolve(quote('sq-A', 40000));
    await tick();
    assert.equal(session.status, 'error', 'a stale success must never overwrite the latest failure');
    assert.equal(session.quote === null, true);
    assert.equal(session.canSubmit(), false);
  }
  // (c) While a refetch is pending, the previous charge is retained
  // as visibly stale — never presented as the current charge.
  {
    const { session, pendings } = makeSession();
    session.request(inputA, GOODS);
    pendings[0].resolve(quote('sq-A', 40000));
    await tick();
    session.request(inputB, GOODS);
    assert.equal(session.status, 'loading');
    assert.equal(session.displayQuote?.quoteId, 'sq-A', 'previous charge is retained while refetching');
    assert.equal(session.displayQuote?.totalShipping, 40000);
    assert.equal(session.displayStale, true);
    assert.equal(session.canSubmit(), false, 'a stale charge is never submittable as current');
    pendings[1].resolve(quote('sq-B', 50000));
    await tick();
    assert.equal(session.displayStale, false);
    assert.equal(session.getSubmittableQuoteId(), 'sq-B');
  }
});

test('G-12 server mismatch visibly refreshes the charge and is retryable without editing the address or losing the cart', async () => {
  const { session, calls, pendings } = makeSession();
  session.request(inputA, GOODS);
  pendings[0].resolve(quote('sq-A', 40000));
  await tick();
  assert.equal(session.getSubmittableQuoteId(), 'sq-A');
  assert.equal(isQuoteChangedError(new Error('shipping_quote_changed')), true);
  assert.equal(isQuoteChangedError(new Error('هزینه ارسال تغییر کرده است؛ لطفاً هزینه به‌روز را بررسی و دوباره تلاش کنید.')), true);

  // (i) On shipping_quote_changed the drawer auto-refetches with no
  // address edit and no drawer close.
  session.beginMismatch();
  assert.equal(calls.length, 2, 'a fresh quote request is issued automatically');
  assert.deepEqual(calls[1].items, calls[0].items, 'cart lines are unchanged');
  assert.deepEqual(calls[1].destination, calls[0].destination, 'form fields are unchanged');
  assert.equal(session.mismatch?.oldTotalShipping, 40000);
  assert.equal(session.mismatch?.oldPayable, 140000);
  assert.equal(session.mismatch?.newTotalShipping, null);
  // (ii) The new charge and new payable are displayed, visibly
  // different from the stale ones.
  pendings[1].resolve(quote('sq-B', 55000));
  await tick();
  assert.equal(session.mismatch?.newTotalShipping, 55000);
  assert.equal(session.mismatch?.newPayable, 155000);
  assert.notEqual(session.mismatch?.newTotalShipping, session.mismatch?.oldTotalShipping);
  // (iii)+(iv) Resubmit is blocked until acknowledgement, then
  // abandon-and-reclaim creates exactly one order with the new quote.
  const orderOfCalls: string[] = [];
  const orders: { id: string; quoteId: string }[] = [];
  let abandonCalls = 0;
  const abandon = async () => { abandonCalls += 1; orderOfCalls.push('abandon'); };
  const submitOrder = async (quoteId: string) => {
    orderOfCalls.push('submit');
    const order = { id: `order-${orders.length + 1}`, orderNumber: `N-${orders.length + 1}`, quoteId } as never;
    orders.push(order as never);
    return order;
  };
  assert.equal(session.canSubmit(), false, 'submit stays blocked until the new charge is acknowledged');
  await assert.rejects(
    resubmitAfterMismatch({ session, abandonCheckout: abandon, submitOrder }),
    /تأیید نشده/,
    'an unacknowledged resubmit must not abandon or submit anything',
  );
  assert.equal(abandonCalls, 0);
  assert.equal(orders.length, 0);
  session.acknowledgeMismatch();
  assert.equal(session.canSubmit(), true);
  const outcome = await resubmitAfterMismatch({ session, abandonCheckout: abandon, submitOrder });
  assert.equal((outcome.order as unknown as { quoteId: string }).quoteId, 'sq-B');
  assert.equal(outcome.recovered, false, 'a fresh abandon+submit is not a recovery');
  assert.deepEqual(orderOfCalls, ['abandon', 'submit'], 'abandon-and-reclaim runs as part of recovery, in order');
  assert.equal(abandonCalls, 1);
  assert.equal(orders.length, 1, 'exactly one order is created');
});

test('G-13 no duplicate order on recovery', async () => {
  // (b) The G-12 mismatch flow end-to-end creates exactly one order
  // for the attempt (see G-12 for the full drill). Here: an abandon
  // failure must not submit anything, and a fresh purchase after a
  // completed one starts from a clean session with a new quote.
  const { session, pendings } = makeSession();
  session.request(inputA, GOODS);
  pendings[0].resolve(quote('sq-A', 40000));
  await tick();
  session.beginMismatch();
  pendings[1].resolve(quote('sq-B', 55000));
  await tick();
  session.acknowledgeMismatch();
  let submitCalls = 0;
  await assert.rejects(
    resubmitAfterMismatch({
      session,
      abandonCheckout: async () => { throw new Error('abandon failed'); },
      submitOrder: async () => { submitCalls += 1; return { id: 'order-1' } as never; },
    }),
    /abandon failed/,
  );
  assert.equal(submitCalls, 0, 'a failed abandon must not create an order');
  // A genuinely new identical purchase starts clean: a new session
  // carries no quote, no mismatch, and no attempt state.
  const fresh = makeSession();
  assert.equal(fresh.session.quote, null);
  assert.equal(fresh.session.mismatch, null);
  assert.equal(fresh.session.canSubmit(), false);
  fresh.session.request(inputA, GOODS);
  fresh.pendings[0].resolve(quote('sq-new', 40000));
  await tick();
  assert.equal(fresh.session.getSubmittableQuoteId(), 'sq-new');
  // (a) + (c) — lost-response replay under the same attempt id and a
  // second order for a deliberate new purchase — are covered by the
  // CHECKOUT-01 suite (src/context/OrderContext.checkout.test.tsx),
  // which needs a browser and cannot run as root on this VM.
});

test('G-14 changed payable is explicitly reviewed before submit', async () => {
  const { session, pendings } = makeSession();
  session.request(inputA, GOODS);
  pendings[0].resolve(quote('sq-A', 40000));
  await tick();
  session.beginMismatch();
  pendings[1].resolve(quote('sq-B', 55000));
  await tick();
  // The new charge is displayed, but submit is blocked until the
  // dedicated acknowledgement control is used (Q7: display alone
  // does not suffice).
  assert.equal(session.mismatch?.oldPayable, 140000);
  assert.equal(session.mismatch?.newPayable, 155000);
  assert.equal(session.status, 'quoted');
  assert.equal(session.canSubmit(), false);
  assert.equal(session.getSubmittableQuoteId(), null, 'the pre-refresh payable is never the submitted payable');
  session.acknowledgeMismatch();
  assert.equal(session.canSubmit(), true);
  assert.equal(session.getSubmittableQuoteId(), 'sq-B');
});

test('G-15 regression coverage exists and passes', () => {
  // This file is the coverage: it imports the session, the signature
  // builder, the mismatch detector, the drawer's abandon-and-reclaim
  // helper and the form's Q8 helper, and names one test per gate
  // G-01..G-14. This smoke test pins those exports in place.
  assert.equal(typeof buildQuoteSignature, 'function');
  assert.equal(typeof QuoteSession, 'function');
  assert.equal(typeof isQuoteChangedError, 'function');
  assert.equal(typeof resubmitAfterMismatch, 'function');
  assert.equal(typeof addressPatchClearsSavedSelection, 'function');
});
