// UUID-shaped identifier for contexts without crypto.randomUUID. The
// Math.random fallback provides uniqueness only. Money operations rely
// on JWT authentication and server-side account and payload binding.
export const newUuid = (): string => {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  } catch { /* fall through to the fallback */ }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
  });
};

// G1 (Batch 3): SHA-256 when WebCrypto is available, a documented
// non-cryptographic fallback when it is not (plain-HTTP origin).
// Purpose limitation: the result feeds only a fingerprint /
// idempotency partition, never authentication. The fallback output is
// a 16-hex-char FNV-1a hash, NOT a SHA-256 digest, and must never be
// presented or stored as one.
export const fingerprintHash = async (input: string): Promise<string> => {
  try {
    if (typeof crypto !== 'undefined' && crypto.subtle && typeof crypto.subtle.digest === 'function') {
      const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(input));
      return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
    }
  } catch { /* fall through to the non-cryptographic fallback */ }
  let hash = 0xcbf29ce484222325n;
  const bytes = new TextEncoder().encode(input);
  for (const byte of bytes) {
    hash ^= BigInt(byte);
    hash = (hash * 0x100000001b3n) & 0xffffffffffffffffn;
  }
  return hash.toString(16).padStart(16, '0');
};
