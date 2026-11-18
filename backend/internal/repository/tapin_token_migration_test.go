package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/tapin"
	"github.com/google/uuid"
)

// SEC-01 Tapin token encryption tests. All token values and key
// material below are synthetic test-only values. The disposable
// database comes from operationsDB (OPERATIONS_TEST_DATABASE_URL,
// set by the isolated test harness); tests skip without it.

const (
	testTapinCipherKey     = "test-only-tapin-key-material-sec01-synthetic"
	testTapinCipherKeyNew  = "test-only-tapin-key-material-sec01-synthetic-new"
	testTapinPlaintextOne  = "test-only-supplier-tapin-token-sec01-aaa"
	testTapinPlaintextTwo  = "test-only-supplier-tapin-token-sec01-bbb"
	testTapinPlaintextTree = "test-only-supplier-tapin-token-sec01-ccc"
)

func sec01Supplier(t *testing.T, db *sql.DB, ownerMobileSuffix int) string {
	t.Helper()
	userID := uuid.NewString()
	supplierID := "sec01-" + uuid.NewString()[:12]
	mobile := fmt.Sprintf("09%09d", (time.Now().UnixNano()%1e9+int64(ownerMobileSuffix)*7919)%1e9)
	opsExec(t, db, `INSERT INTO users(id,mobile,full_name,role) VALUES($1,$2,'sec01 test','supplier')`, userID, mobile)
	opsExec(t, db, `INSERT INTO suppliers(id,name,owner_user_id,approval_status) VALUES($1,'sec01 supplier',$2,'approved')`, supplierID, userID)
	return supplierID
}

func sec01SetRawToken(t *testing.T, db *sql.DB, supplierID, token string) {
	t.Helper()
	opsExec(t, db, `UPDATE suppliers SET tapin_token = $2 WHERE id = $1`, supplierID, token)
}

func sec01ReadRawToken(t *testing.T, db *sql.DB, supplierID string) sql.NullString {
	t.Helper()
	var tok sql.NullString
	if err := db.QueryRow(`SELECT tapin_token FROM suppliers WHERE id = $1`, supplierID).Scan(&tok); err != nil {
		t.Fatalf("read raw tapin token: %v", err)
	}
	return tok
}

func TestTapinTokenMigrationEncryptsPlaintextInPlace(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	plain := sec01Supplier(t, db, 1)
	plain2 := sec01Supplier(t, db, 2)
	empty := sec01Supplier(t, db, 3)

	cipher, err := tapin.NewTokenCipher(testTapinCipherKey)
	if err != nil {
		t.Fatal(err)
	}
	// One row is already encrypted (pre-migration state for the
	// idempotency path); two rows are legacy plaintext; one is NULL.
	encAlready, err := cipher.EncryptToken(testTapinPlaintextTree)
	if err != nil {
		t.Fatal(err)
	}
	sec01SetRawToken(t, db, plain, testTapinPlaintextOne)
	sec01SetRawToken(t, db, plain2, testTapinPlaintextTwo)
	sec01SetRawToken(t, db, empty, encAlready)
	// NULL-token supplier: a fourth fixture supplier with no token.
	nullSupplier := sec01Supplier(t, db, 4)

	repo := NewTapinRepository(db).WithTokenCipher(cipher)
	rep, err := repo.ReencryptSupplierTapinTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Counts are lower bounds, not exact: the disposable DB is shared
	// with the other tests in this package run, which may leave their
	// own token rows behind. The per-row assertions below are scoped
	// to this test's own suppliers and are the exact checks.
	if rep.Encrypted < 2 {
		t.Fatalf("encrypted=%d, want at least 2 (the two plaintext rows)", rep.Encrypted)
	}
	if rep.AlreadyEncrypted < 1 {
		t.Fatalf("alreadyEncrypted=%d, want at least 1", rep.AlreadyEncrypted)
	}
	if rep.Failed != 0 {
		t.Fatalf("failed=%d (ids %v), want 0", rep.Failed, rep.FailedIDs)
	}

	// Plaintext must be absent from THIS test's stored values.
	var remaining int
	if err := db.QueryRow(`SELECT count(*) FROM suppliers WHERE id IN ($1,$2,$3) AND tapin_token IS NOT NULL AND tapin_token NOT LIKE 'enc:v1:%'`, plain, plain2, empty).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("%d plaintext token rows remain after migration", remaining)
	}
	for _, tc := range []struct {
		id, want string
	}{
		{plain, testTapinPlaintextOne},
		{plain2, testTapinPlaintextTwo},
		{empty, testTapinPlaintextTree},
	} {
		raw := sec01ReadRawToken(t, db, tc.id)
		if !raw.Valid || !strings.HasPrefix(raw.String, tapin.TokenCipherPrefix) {
			t.Fatalf("supplier %s stored value is not ciphertext", tc.id)
		}
		if strings.Contains(raw.String, tc.want) {
			t.Fatalf("supplier %s ciphertext contains the plaintext", tc.id)
		}
		creds, err := repo.GetSupplierCredentials(ctx, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if creds.Token != tc.want {
			t.Fatalf("supplier %s decrypted token mismatch", tc.id)
		}
	}
	// NULL row untouched.
	if tok := sec01ReadRawToken(t, db, nullSupplier); tok.Valid {
		t.Fatal("NULL token row was modified by the migration")
	}

	// Second pass is a no-op for this test's rows: nothing left to
	// encrypt (global counts may include other tests' rows).
	rep2, err := repo.ReencryptSupplierTapinTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Failed != 0 {
		t.Fatalf("second pass failed=%d, want 0", rep2.Failed)
	}
	for _, tc := range []struct {
		id, want string
	}{
		{plain, testTapinPlaintextOne},
		{plain2, testTapinPlaintextTwo},
		{empty, testTapinPlaintextTree},
	} {
		raw := sec01ReadRawToken(t, db, tc.id)
		if !raw.Valid || !strings.HasPrefix(raw.String, tapin.TokenCipherPrefix) {
			t.Fatalf("supplier %s stored value is not ciphertext after second pass", tc.id)
		}
		creds, err := repo.GetSupplierCredentials(ctx, tc.id)
		if err != nil || creds.Token != tc.want {
			t.Fatalf("supplier %s token changed across passes: %v", tc.id, err)
		}
	}
}

func TestTapinTokenUpdateEncryptsOnWrite(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	supplierID := sec01Supplier(t, db, 11)

	cipher, err := tapin.NewTokenCipher(testTapinCipherKey)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSupplierRepository(db).WithTapinCipher(cipher)

	// The supplier needs a shop id too: Connected() requires both.
	if err := repo.UpdateTapinShopID(ctx, supplierID, "test-only-shop-sec01"); err != nil {
		t.Fatal(err)
	}

	// Write encrypts on write.
	if err := repo.UpdateTapinToken(ctx, supplierID, testTapinPlaintextOne); err != nil {
		t.Fatal(err)
	}
	raw := sec01ReadRawToken(t, db, supplierID)
	if !raw.Valid || !strings.HasPrefix(raw.String, tapin.TokenCipherPrefix) {
		t.Fatal("UpdateTapinToken stored plaintext")
	}
	// Read decrypts on read, through both selectors.
	sup, err := repo.GetSupplierByID(ctx, supplierID)
	if err != nil {
		t.Fatal(err)
	}
	if sup == nil || sup.TapinToken != testTapinPlaintextOne {
		t.Fatalf("GetSupplierByID token mismatch: %+v", sup)
	}
	creds, err := NewTapinRepository(db).WithTokenCipher(cipher).GetSupplierCredentials(ctx, supplierID)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Token != testTapinPlaintextOne || !creds.Connected() {
		t.Fatalf("GetSupplierCredentials mismatch: connected=%v", creds.Connected())
	}

	// Overwrite re-encrypts (new nonce: stored value changes).
	firstStored := raw.String
	if err := repo.UpdateTapinToken(ctx, supplierID, testTapinPlaintextTwo); err != nil {
		t.Fatal(err)
	}
	if second := sec01ReadRawToken(t, db, supplierID); !second.Valid || second.String == firstStored {
		t.Fatal("token overwrite did not produce a fresh ciphertext")
	}

	// Clear writes NULL.
	if err := repo.UpdateTapinToken(ctx, supplierID, ""); err != nil {
		t.Fatal(err)
	}
	if tok := sec01ReadRawToken(t, db, supplierID); tok.Valid {
		t.Fatal("clearing the token did not NULL the column")
	}

	// Fail-closed without a cipher: no silent plaintext write.
	bare := NewSupplierRepository(db)
	if err := bare.UpdateTapinToken(ctx, supplierID, testTapinPlaintextOne); err == nil {
		t.Fatal("UpdateTapinToken without a cipher succeeded (plaintext write)")
	}
	if tok := sec01ReadRawToken(t, db, supplierID); tok.Valid {
		t.Fatal("failed write still stored a token")
	}

	// Without a cipher, an encrypted value fails closed on read
	// instead of being returned as a token.
	if err := repo.UpdateTapinToken(ctx, supplierID, testTapinPlaintextOne); err != nil {
		t.Fatal(err)
	}
	if _, err := bare.GetSupplierByID(ctx, supplierID); err == nil {
		t.Fatal("ciphertext read without a cipher succeeded")
	}
	// ...while a legacy plaintext row still reads through (migration
	// has not reached it yet).
	sec01SetRawToken(t, db, supplierID, testTapinPlaintextTwo)
	sup, err = bare.GetSupplierByID(ctx, supplierID)
	if err != nil || sup == nil || sup.TapinToken != testTapinPlaintextTwo {
		t.Fatalf("legacy plaintext did not read through without a cipher: %v", err)
	}
}

func TestTapinTokenRotationDrillViaPreviousKey(t *testing.T) {
	// Local rotation drill (disposable DB, synthetic values only):
	// token written under the OLD key; rotate to the NEW key with the
	// old key as the previous-key fallback; re-encrypt in place; prove
	// the new key alone reads it and the old key alone does not.
	db := operationsDB(t)
	ctx := context.Background()
	supplierID := sec01Supplier(t, db, 21)

	oldCipher, err := tapin.NewTokenCipher(testTapinCipherKey)
	if err != nil {
		t.Fatal(err)
	}
	oldRepo := NewSupplierRepository(db).WithTapinCipher(oldCipher)
	if err := oldRepo.UpdateTapinToken(ctx, supplierID, testTapinPlaintextOne); err != nil {
		t.Fatal(err)
	}

	newCipher, err := tapin.NewTokenCipher(testTapinCipherKeyNew)
	if err != nil {
		t.Fatal(err)
	}
	newCipher.WithPreviousKey(testTapinCipherKey)
	rotRepo := NewTapinRepository(db).WithTokenCipher(newCipher)

	// Old token still reads during the rotation window (dual-read).
	creds, err := rotRepo.GetSupplierCredentials(ctx, supplierID)
	if err != nil || creds.Token != testTapinPlaintextOne {
		t.Fatalf("old token unreadable during rotation window: %v", err)
	}
	// Re-encrypt in place under the new key. The count is a lower
	// bound: the disposable DB is shared with the other tests in this
	// package run, whose old-key rows are re-encrypted too.
	rep, err := rotRepo.ReencryptSupplierTapinTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Encrypted < 1 || rep.Failed != 0 {
		t.Fatalf("rotation re-encryption: encrypted=%d failed=%d, want >=1/0", rep.Encrypted, rep.Failed)
	}
	// New key alone reads it; old key alone must not.
	newKeyOnly, err := tapin.NewTokenCipher(testTapinCipherKeyNew)
	if err != nil {
		t.Fatal(err)
	}
	creds, err = NewTapinRepository(db).WithTokenCipher(newKeyOnly).GetSupplierCredentials(ctx, supplierID)
	if err != nil || creds.Token != testTapinPlaintextOne {
		t.Fatalf("re-encrypted token unreadable under new key alone: %v", err)
	}
	if _, err := NewTapinRepository(db).WithTokenCipher(oldCipher).GetSupplierCredentials(ctx, supplierID); err == nil {
		t.Fatal("re-encrypted token still readable under the retired old key")
	}
}
