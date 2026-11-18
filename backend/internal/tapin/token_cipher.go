// Package tapin — token encryption (SEC-01).
//
// Supplier (and platform) Tapin tokens are stored AES-256-GCM encrypted
// under TAPIN_TOKEN_KEY, in the format the migration 202610280007 comment
// always claimed but no code ever implemented:
//
//	enc:v1:<base64(nonce || ciphertext)>
//
// Format rules:
//   - "enc:v1:" is the version marker. Only v1 exists today; a future
//     version gets a new marker and a new decrypt path — never a silent
//     reinterpretation of v1 bytes.
//   - The AES-256 key is SHA-256(key material). Key material of any
//     non-empty length is accepted; the derivation is what fixes the
//     32-byte key length, so a short operator-chosen key still yields a
//     full-strength AES-256 key (its strength is then the strength of
//     the material — generate it with a CSPRNG, ≥32 chars).
//   - The nonce is 12 fresh random bytes per encryption, prepended to
//     the ciphertext; the GCM tag authenticates the whole value, so a
//     wrong key or any tampering fails decryption instead of returning
//     wrong bytes.
//   - Plaintext (legacy) values — anything without the "enc:v1:"
//     prefix — are NOT ciphertext. DecodeStoredToken returns them
//     unchanged with encrypted=false so rows written before SEC-01
//     keep working until the in-place migration re-encrypts them.
//     New writes always produce ciphertext; a plaintext write through
//     the repository is an error, never a silent fallback.
//
// Rotation (gate G-10): a TokenCipher holds a current key plus an
// optional previous key. Decrypt tries current, then previous;
// encrypt always uses current. To rotate: deploy with the new
// TAPIN_TOKEN_KEY and the old one as TAPIN_TOKEN_PREVIOUS_KEY, let the
// startup in-place migration re-encrypt every row to the new key, then
// drop TAPIN_TOKEN_PREVIOUS_KEY. Rollback is pointing readers back at
// the old key (restore it as TAPIN_TOKEN_KEY). The old key must stay in
// the restricted store until re-encryption is proven per row.
//
// This package never logs a token, a key, or a ciphertext.
package tapin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// TokenCipherPrefix marks AES-256-GCM ciphertext in the token columns.
// The "v1" is the key/ciphertext format version, not the key id: a key
// rotation keeps v1 and changes the key material (see TokenCipher).
const TokenCipherPrefix = "enc:v1:"

// ErrTokenCipherNotConfigured is returned when token encryption is
// attempted without a key. It is a fail-closed error: callers must
// never fall back to plaintext storage on this path.
var ErrTokenCipherNotConfigured = errors.New("tapin token cipher is not configured (TAPIN_TOKEN_KEY missing)")

// ErrTokenDecryptFailed is returned when a ciphertext value cannot be
// decrypted with any configured key (wrong key, tampered bytes, or a
// truncated value). The error carries no value material.
var ErrTokenDecryptFailed = errors.New("tapin token decryption failed (wrong key or corrupted value)")

// TokenCipher encrypts/decrypts Tapin tokens with AES-256-GCM.
type TokenCipher struct {
	current  []byte // 32-byte AES key derived from the current key material
	previous []byte // 32-byte AES key derived from the previous material, or nil
}

// DeriveTokenKey derives the 32-byte AES-256 key for key material.
// It is exported so the rotation drill and tests can prove that two
// materials yield different keys without handling a TokenCipher.
func DeriveTokenKey(keyMaterial string) []byte {
	sum := sha256.Sum256([]byte(keyMaterial))
	key := make([]byte, 32)
	copy(key, sum[:])
	return key
}

// NewTokenCipher builds a cipher from key material. Empty (or
// whitespace-only) material is an error — an empty key must never
// silently mean "store plaintext".
func NewTokenCipher(keyMaterial string) (*TokenCipher, error) {
	if strings.TrimSpace(keyMaterial) == "" {
		return nil, ErrTokenCipherNotConfigured
	}
	return &TokenCipher{current: DeriveTokenKey(keyMaterial)}, nil
}

// WithPreviousKey adds the previous key material as a decryption
// fallback (rotation window). Encrypt still uses the current key.
// An empty previous material is a no-op. It returns the receiver so
// call sites can chain it on a constructor.
func (c *TokenCipher) WithPreviousKey(previousMaterial string) *TokenCipher {
	if c == nil || strings.TrimSpace(previousMaterial) == "" {
		return c
	}
	c.previous = DeriveTokenKey(previousMaterial)
	return c
}

// EncryptToken encrypts a plaintext token. An empty plaintext is an
// error: clearing a token is a NULL write, not an encryption of "".
func (c *TokenCipher) EncryptToken(plaintext string) (string, error) {
	if c == nil || len(c.current) == 0 {
		return "", ErrTokenCipherNotConfigured
	}
	if plaintext == "" {
		return "", errors.New("tapin token plaintext is empty")
	}
	block, err := aes.NewCipher(c.current)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return TokenCipherPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptToken decrypts an "enc:v1:" value. A value without the prefix
// is NOT decrypted (see DecodeStoredToken); a value with the prefix
// that fails against every configured key returns
// ErrTokenDecryptFailed. The returned plaintext is the token.
func (c *TokenCipher) DecryptToken(stored string) (string, error) {
	if c == nil || len(c.current) == 0 {
		return "", ErrTokenCipherNotConfigured
	}
	if !strings.HasPrefix(stored, TokenCipherPrefix) {
		return "", errors.New("tapin token value is not ciphertext (missing enc:v1: prefix)")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, TokenCipherPrefix))
	if err != nil {
		return "", ErrTokenDecryptFailed
	}
	for _, key := range c.keys() {
		plain, err := decryptWithKey(key, raw)
		if err == nil {
			return plain, nil
		}
	}
	return "", ErrTokenDecryptFailed
}

// DecodeStoredToken decodes a stored column value of either shape:
//   - "enc:v1:..." → decrypted with the cipher (current, then
//     previous key); encrypted=true.
//   - anything else → returned unchanged as legacy plaintext;
//     encrypted=false. Callers must treat it as a row the in-place
//     migration has not re-encrypted yet, never as ciphertext.
//
// A NULL/empty stored value decodes to "" with encrypted=false and no
// error. A ciphertext value that cannot be decrypted is an error.
func (c *TokenCipher) DecodeStoredToken(stored string) (plaintext string, encrypted bool, err error) {
	if stored == "" {
		return "", false, nil
	}
	if !strings.HasPrefix(stored, TokenCipherPrefix) {
		return stored, false, nil
	}
	plain, err := c.DecryptToken(stored)
	if err != nil {
		return "", true, err
	}
	return plain, true, nil
}

// ReencryptToken decodes a stored value with this cipher (current or
// previous key, or legacy plaintext) and re-encrypts it under the
// current key. It is the rotation primitive: after a key rotation the
// caller re-encrypts each row through a cipher whose current key is
// the NEW key and whose previous key is the OLD key. A value already
// encrypted under the current key is returned unchanged.
func (c *TokenCipher) ReencryptToken(stored string) (string, error) {
	if c == nil || len(c.current) == 0 {
		return "", ErrTokenCipherNotConfigured
	}
	plain, encrypted, err := c.DecodeStoredToken(stored)
	if err != nil {
		return "", err
	}
	if !encrypted {
		return c.EncryptToken(plain)
	}
	// Already ciphertext: re-encrypt only if the previous key was the
	// one that decrypted it (i.e. it is still under the old key). A
	// value that decrypts under the current key is already current.
	if _, err := decryptWithKey(c.current, mustDecode(stored)); err == nil {
		return stored, nil
	}
	return c.EncryptToken(plain)
}

// keys returns the decryption key order: current first, then previous.
func (c *TokenCipher) keys() [][]byte {
	if len(c.previous) == 0 {
		return [][]byte{c.current}
	}
	return [][]byte{c.current, c.previous}
}

func decryptWithKey(key, raw []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", ErrTokenDecryptFailed
	}
	nonce, sealed := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", ErrTokenDecryptFailed
	}
	return string(plain), nil
}

// mustDecode base64-decodes a value that starts with the ciphertext
// prefix. It is only called on values already proven to carry the
// prefix and (in ReencryptToken) already proven decryptable, so a
// decode failure there means the value is corrupt; callers treat a
// nil return as a failed decrypt.
func mustDecode(stored string) []byte {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, TokenCipherPrefix))
	if err != nil {
		return nil
	}
	return raw
}
