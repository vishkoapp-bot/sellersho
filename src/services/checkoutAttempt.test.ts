import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  CHECKOUT_FINGERPRINT_CONFLICT_MESSAGE,
  claimCheckoutAttempt,
  type CheckoutAttempt,
} from './checkoutAttempt';
import type { CreateOrderPayload } from './orderApi';

test('checkout claims a canonical UUID and detects changed payloads without secure-context APIs', async (t) => {
  const attempts = new Map<string, CheckoutAttempt>();
  const database = {
    close() {},
    transaction() {
      const tx = {
        oncomplete: () => {},
        onabort: () => {},
        aborted: false,
        abort() {
          tx.aborted = true;
          tx.onabort();
        },
        objectStore() {
          return {
            add(attempt: CheckoutAttempt) {
              assert.ok(!attempts.has(attempt.scope));
              attempts.set(attempt.scope, attempt);
            },
            get(scope: string) {
              const read = { result: attempts.get(scope), onsuccess: () => {} };
              queueMicrotask(() => {
                read.onsuccess();
                if (!tx.aborted) tx.oncomplete();
              });
              return read;
            },
          };
        },
      };
      return tx;
    },
  };
  const indexedDBBefore = Object.getOwnPropertyDescriptor(globalThis, 'indexedDB');
  Object.defineProperty(globalThis, 'indexedDB', {
    configurable: true,
    value: {
      open() {
        const request = { result: database, onsuccess: () => {} };
        queueMicrotask(() => request.onsuccess());
        return request;
      },
    },
  });
  t.after(() => {
    if (indexedDBBefore) Object.defineProperty(globalThis, 'indexedDB', indexedDBBefore);
    else Reflect.deleteProperty(globalThis, 'indexedDB');
  });
  t.mock.getter(globalThis, 'crypto', () => ({ randomUUID: undefined, subtle: undefined }) as unknown as Crypto);

  const payload: CreateOrderPayload = {
    storeId: 'store',
    paymentMethod: 'cash',
    customerInfo: {
      fullName: 'Fixture',
      phone: '09000000000',
      province: 'Fixture',
      city: 'Fixture',
      address: 'Fixture',
      postalCode: '0000000000',
    },
    items: [{ productId: 'product', quantity: 1 }],
  };
  const claim = (value: CreateOrderPayload) => claimCheckoutAttempt('customer:store', value, () => true);
  const first = await claim(payload);
  assert.match(first.id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  assert.match(first.fingerprint, /^[0-9a-f]{16}$/);
  assert.deepEqual(await claim(payload), first);
  await assert.rejects(claim({ ...payload, items: [{ productId: 'product', quantity: 2 }] }), {
    message: CHECKOUT_FINGERPRINT_CONFLICT_MESSAGE,
  });
  assert.equal(attempts.size, 1);
});
