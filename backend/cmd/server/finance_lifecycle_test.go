package main

import (
	"context"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestSettlementApprovalIsIdempotent(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewFinanceRepository(db)
	requestID := "audit-settlement-" + uuid.NewString()[:24]
	_, err := db.ExecContext(ctx, `
		INSERT INTO payout_requests (id, supplier_id, recipient_type, recipient_name, shaba, bank_name, amount, status)
		VALUES ($1, 'sup-ali', 'supplier', 'Audit Supplier', 'IR060170000000000000000000', 'بانک ملی', 1000000, 'pending')
	`, requestID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM ledger_entries WHERE related_order_or_dispute = $1`, requestID)
		_, _ = db.ExecContext(ctx, `DELETE FROM payout_requests WHERE id = $1`, requestID)
	})

	if err := repo.ReviewSettlement(ctx, requestID, "approved", "audit", "finance-test"); err != nil {
		t.Fatalf("first approval failed: %v", err)
	}
	if err := repo.ReviewSettlement(ctx, requestID, "approved", "duplicate", "finance-test"); err == nil {
		t.Fatal("duplicate settlement approval should fail")
	}

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM payout_requests WHERE id = $1`, requestID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "approved" {
		t.Fatalf("settlement status = %q, want approved", status)
	}
	// D32: approval is only a manual-transfer marker — it writes NO
	// ledger entry. The debit is recorded by ConfirmManualTransfer.
	var ledgerCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute = $1`, requestID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 0 {
		t.Fatalf("ledger entries after approval = %d, want 0 (D32)", ledgerCount)
	}
	if err := repo.ConfirmManualTransfer(ctx, requestID, "audit-transfer-ref", "finance-test"); err != nil {
		t.Fatalf("transfer confirmation failed: %v", err)
	}
	if err := repo.ConfirmManualTransfer(ctx, requestID, "audit-transfer-ref", "finance-test"); err == nil {
		t.Fatal("duplicate transfer confirmation should fail")
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM payout_requests WHERE id = $1`, requestID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "transfer_confirmed" {
		t.Fatalf("settlement status after transfer = %q, want transfer_confirmed", status)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute = $1`, requestID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 {
		t.Fatalf("ledger entries after transfer = %d, want 1", ledgerCount)
	}
}
