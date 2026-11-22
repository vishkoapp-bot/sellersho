import { apiRequest, tokenStore } from './apiClient';
import { fingerprintHash, newUuid } from './uuid';

const pending = new Map<string, string>();

// Store only an opaque identity and hashes in session storage, never account
// destinations, money payloads or credentials. An uncertain response retains its
// identity across component remounts and reloads. Acknowledged success rearms it.
export async function financeOperation<T>(endpoint: string, payload: object, accountOperation: string, accepts: (value: any) => boolean): Promise<T> {
  const generation = tokenStore.getGeneration();
  // This partition is not authorization: the server verifies the JWT and binds
  // ownership. Keeping the immutable user ID avoids rotating an unresolved key
  // when a token is refreshed for the same account.
  let principal = `anonymous:${generation}`;
  try {
    const token = tokenStore.getToken();
    if (token) {
      const claims = JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')));
      if (typeof claims.userId === 'string' && claims.userId) principal = claims.userId;
    }
  } catch { /* unauthenticated requests fail server-side */ }
  // The FNV fallback stays in the client-side storage identity. A collision
  // reuses a UUID, and the server rejects a different payload for that account.
  const identity = `sellersho.finance.pending.${await fingerprintHash(JSON.stringify([
    principal, accountOperation, payload,
  ]))}`;
  let key = pending.get(identity);
  try { key ??= sessionStorage.getItem(identity) ?? undefined; } catch { /* memory fallback */ }
  key ??= newUuid();
  pending.set(identity, key);
  try { sessionStorage.setItem(identity, key); } catch { /* memory fallback */ }
  if (tokenStore.getGeneration() !== generation) throw new Error('Session changed during financial request');
  const result = await apiRequest<T>(endpoint, {
    method: 'POST', auth: true, body: JSON.stringify(payload), headers: { 'Idempotency-Key': key },
  });
  if (tokenStore.getGeneration() !== generation) throw new Error('Session changed during financial request');
  if (result === null || typeof result !== 'object' || !accepts(result)) throw new Error('Financial request outcome could not be confirmed');
  if (pending.get(identity) === key) {
    pending.delete(identity);
    try { if (sessionStorage.getItem(identity) === key) sessionStorage.removeItem(identity); } catch { /* memory fallback */ }
  }
  return result;
}
