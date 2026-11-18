package tapin

import (
	"strings"
	"testing"
)

// All key material below is synthetic test-only material, generated
// for these tests. It is not, and never was, a production value.

const (
	testTapinKeyMaterial    = "test-only-tapin-key-material-0001-synthetic"
	testTapinKeyMaterialNew = "test-only-tapin-key-material-0002-synthetic-rotated"
	testTapinKeyMaterialOld = "test-only-tapin-key-material-0003-synthetic-previous"
	testTapinPlaintext      = "test-only-supplier-tapin-token-synthetic"
)

func TestTokenCipherRoundTrip(t *testing.T) {
	c, err := NewTokenCipher(testTapinKeyMaterial)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c.EncryptToken(testTapinPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, TokenCipherPrefix) {
		t.Fatalf("ciphertext missing %q prefix: %.12s", TokenCipherPrefix, enc)
	}
	if strings.Contains(enc, testTapinPlaintext) {
		t.Fatal("ciphertext contains the plaintext")
	}
	plain, err := c.DecryptToken(enc)
	if err != nil {
		t.Fatal(err)
	}
	if plain != testTapinPlaintext {
		t.Fatal("round-trip plaintext mismatch")
	}
	// Two encryptions of the same plaintext must differ (fresh nonce).
	enc2, err := c.EncryptToken(testTapinPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	if enc2 == enc {
		t.Fatal("re-encryption produced identical ciphertext (nonce not fresh)")
	}
	if p, err := c.DecryptToken(enc2); err != nil || p != testTapinPlaintext {
		t.Fatal("second ciphertext did not decrypt to the plaintext")
	}
}

func TestTokenCipherWrongKeyFails(t *testing.T) {
	c, err := NewTokenCipher(testTapinKeyMaterial)
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewTokenCipher(testTapinKeyMaterialNew)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c.EncryptToken(testTapinPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.DecryptToken(enc); err == nil {
		t.Fatal("decryption with the wrong key succeeded")
	}
	// Tampering with one ciphertext byte must fail authentication.
	tampered := enc[:len(enc)-2] + "AA"
	if _, err := c.DecryptToken(tampered); err == nil {
		t.Fatal("decryption of tampered ciphertext succeeded")
	}
	// A truncated value must fail, not return partial plaintext.
	if _, err := c.DecryptToken(enc[:len(TokenCipherPrefix)+8]); err == nil {
		t.Fatal("decryption of truncated ciphertext succeeded")
	}
}

func TestTokenCipherEmptyKeyIsFailClosed(t *testing.T) {
	for _, material := range []string{"", "   "} {
		if _, err := NewTokenCipher(material); err == nil {
			t.Fatalf("empty key material %q was accepted", material)
		}
	}
	var nilCipher *TokenCipher
	if _, err := nilCipher.EncryptToken(testTapinPlaintext); err == nil {
		t.Fatal("nil cipher encrypted a token")
	}
	if _, err := nilCipher.DecryptToken(TokenCipherPrefix + "AAAA"); err == nil {
		t.Fatal("nil cipher decrypted a token")
	}
}

func TestTokenCipherLegacyPlaintextPassthrough(t *testing.T) {
	c, err := NewTokenCipher(testTapinKeyMaterial)
	if err != nil {
		t.Fatal(err)
	}
	// Legacy plaintext decodes unchanged, flagged unencrypted.
	plain, encrypted, err := c.DecodeStoredToken(testTapinPlaintext)
	if err != nil || encrypted || plain != testTapinPlaintext {
		t.Fatalf("legacy passthrough wrong: plain=%q encrypted=%v err=%v", plain, encrypted, err)
	}
	// Empty stored value decodes to empty, no error.
	if plain, encrypted, err := c.DecodeStoredToken(""); err != nil || encrypted || plain != "" {
		t.Fatalf("empty decode wrong: %q %v %v", plain, encrypted, err)
	}
	// A non-prefixed value is never treated as ciphertext.
	if _, err := c.DecryptToken(testTapinPlaintext); err == nil {
		t.Fatal("plaintext was accepted as ciphertext")
	}
	// An encrypted value decodes to the plaintext, flagged encrypted.
	enc, err := c.EncryptToken(testTapinPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	if plain, encrypted, err := c.DecodeStoredToken(enc); err != nil || !encrypted || plain != testTapinPlaintext {
		t.Fatalf("ciphertext decode wrong: %q %v %v", plain, encrypted, err)
	}
}

func TestTokenCipherRotationReencrypt(t *testing.T) {
	// Old key encrypts the token (pre-rotation state).
	oldCipher, err := NewTokenCipher(testTapinKeyMaterialOld)
	if err != nil {
		t.Fatal(err)
	}
	encOld, err := oldCipher.EncryptToken(testTapinPlaintext)
	if err != nil {
		t.Fatal(err)
	}

	// Rotation cipher: current = new key, previous = old key. The old
	// token must still read (dual-read), and re-encryption must move
	// it to the new key.
	rotCipher, err := NewTokenCipher(testTapinKeyMaterialNew)
	if err != nil {
		t.Fatal(err)
	}
	rotCipher.WithPreviousKey(testTapinKeyMaterialOld)
	if plain, _, err := rotCipher.DecodeStoredToken(encOld); err != nil || plain != testTapinPlaintext {
		t.Fatalf("old token unreadable under rotation cipher: %v", err)
	}
	encNew, err := rotCipher.ReencryptToken(encOld)
	if err != nil {
		t.Fatal(err)
	}
	if encNew == encOld {
		t.Fatal("re-encryption returned the old ciphertext unchanged")
	}
	// The new ciphertext reads under the new key alone; the old key
	// alone must no longer read it.
	newOnly, err := NewTokenCipher(testTapinKeyMaterialNew)
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := newOnly.DecryptToken(encNew); err != nil || plain != testTapinPlaintext {
		t.Fatalf("re-encrypted token unreadable under new key: %v", err)
	}
	if _, err := oldCipher.DecryptToken(encNew); err == nil {
		t.Fatal("re-encrypted token still readable under the retired old key")
	}
	// A value already under the current key is returned unchanged.
	if same, err := rotCipher.ReencryptToken(encNew); err != nil || same != encNew {
		t.Fatalf("current-key value was re-encrypted: err=%v", err)
	}
	// Legacy plaintext re-encrypts to ciphertext under the new key.
	encFromPlain, err := rotCipher.ReencryptToken(testTapinPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encFromPlain, TokenCipherPrefix) {
		t.Fatal("plaintext was not re-encrypted to ciphertext")
	}
	if plain, err := newOnly.DecryptToken(encFromPlain); err != nil || plain != testTapinPlaintext {
		t.Fatalf("re-encrypted plaintext unreadable under new key: %v", err)
	}
	// Without the previous key, the old ciphertext is unreadable:
	// rotation without the old key destroys access, by design.
	if _, err := newOnly.DecryptToken(encOld); err == nil {
		t.Fatal("old ciphertext readable without the previous key")
	}
}

func TestTokenCipherKeyDerivation(t *testing.T) {
	a := DeriveTokenKey(testTapinKeyMaterial)
	b := DeriveTokenKey(testTapinKeyMaterial)
	c := DeriveTokenKey(testTapinKeyMaterialNew)
	if len(a) != 32 {
		t.Fatalf("derived key is %d bytes, want 32", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("same material derived different keys")
		}
	}
	same := true
	for i := range a {
		if a[i] != c[i] {
			same = false
		}
	}
	if same {
		t.Fatal("different materials derived the same key")
	}
}
