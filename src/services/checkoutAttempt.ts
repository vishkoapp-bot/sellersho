import type { CreateOrderPayload } from './orderApi';

export interface CheckoutAttempt {
  scope: string;
  id: string;
  fingerprint: string;
  state: 'open' | 'completed';
  orderId?: string;
}
const storageError = () => new Error('ذخیره امن تلاش خرید در دسترس نیست؛ تنظیمات مرورگر را بررسی کنید و دوباره تلاش کنید.');
const requireCurrent = (isCurrent: () => boolean) => { if (!isCurrent()) throw new Error('Session changed during order request'); };

// Persist only opaque identifiers and a hash, never recipient data or credentials.
function operate<T>(scope: string, isCurrent: () => boolean, action: (current: CheckoutAttempt | null, store: IDBObjectStore) => T): Promise<T> {
  return new Promise((resolve, reject) => {
    let request: IDBOpenDBRequest;
    try { requireCurrent(isCurrent); request = indexedDB.open('sellersho.checkout.v1', 1); } catch (err) { reject(isCurrent() ? storageError() : err); return; }
    request.onupgradeneeded = () => request.result.createObjectStore('attempts', { keyPath: 'scope' });
    request.onerror = () => reject(storageError());
    request.onblocked = () => reject(storageError());
    request.onsuccess = () => {
      const db = request.result; let result: T; let failure: unknown;
      try {
        requireCurrent(isCurrent);
        const tx = db.transaction('attempts', 'readwrite'); const store = tx.objectStore('attempts');
        tx.oncomplete = () => { db.close(); try { requireCurrent(isCurrent); resolve(result); } catch (err) { reject(err); } };
        tx.onabort = tx.onerror = () => { db.close(); reject(failure ?? storageError()); };
        const read = store.get(scope);
        read.onsuccess = () => { try { requireCurrent(isCurrent); result = action(read.result ?? null, store); } catch (err) { failure = err; tx.abort(); } };
      } catch (err) { db.close(); reject(err); }
    };
  });
}

const trim = (value?: string) => value?.trim() ?? '';
export function normalizeCheckoutPayload(payload: CreateOrderPayload): CreateOrderPayload {
  const c = payload.customerInfo;
  return {
    storeId: trim(payload.storeId),
    customerInfo: { fullName: trim(c.fullName), phone: trim(c.phone), province: trim(c.province), city: trim(c.city), address: trim(c.address), postalCode: trim(c.postalCode) },
    paymentMethod: trim(payload.paymentMethod) || 'cash',
    items: payload.items.map(item => ({ productId: trim(item.productId), variantId: trim(item.variantId), quantity: item.quantity, addonIds: (item.addonIds ?? []).map(trim).sort() })).sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b), 'en')),
    discountCode: trim(payload.discountCode), shippingQuoteId: trim(payload.shippingQuoteId), addressId: trim(payload.addressId), saveAddress: payload.saveAddress ?? false,
  };
}
export const readCheckoutAttempt = (scope: string, isCurrent: () => boolean) => operate(scope, isCurrent, current => current);

export async function claimCheckoutAttempt(scope: string, payload: CreateOrderPayload, isCurrent: () => boolean): Promise<CheckoutAttempt> {
  requireCurrent(isCurrent);
  const hash = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(normalizeCheckoutPayload(payload))));
  const fingerprint = Array.from(new Uint8Array(hash), b => b.toString(16).padStart(2, '0')).join('');
  requireCurrent(isCurrent);
  return operate(scope, isCurrent, (current, store) => {
    if (current) {
      if (current.fingerprint !== fingerprint) throw new Error('خرید قبلی هنوز بسته نشده است؛ ابتدا نتیجه آن را بازیابی کنید یا «شروع خرید تازه» را بزنید.');
      return current;
    }
    const attempt: CheckoutAttempt = { scope, id: crypto.randomUUID(), fingerprint, state: 'open' };
    store.add(attempt); return attempt;
  });
}
export function completeCheckoutAttempt(scope: string, id: string, orderId: string, isCurrent: () => boolean) {
  return operate(scope, isCurrent, (current, store) => {
    if (current?.id !== id) return current;
    const completed: CheckoutAttempt = { ...current, state: 'completed', orderId }; store.put(completed); return completed;
  });
}
export function clearCheckoutAttempt(scope: string, id: string, isCurrent: () => boolean) {
  return operate(scope, isCurrent, (current, store) => {
    if (current?.id !== id) return false;
    store.delete(scope); return true;
  });
}
