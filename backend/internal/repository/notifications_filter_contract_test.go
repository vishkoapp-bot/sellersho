package repository

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestNotifKindRegistryCoversDeclarations(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "inventory_unify.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	registry := make(map[string]bool, len(AllNotifKinds))
	for _, kind := range AllNotifKinds {
		if registry[kind] {
			t.Errorf("duplicate registry entry %q", kind)
		}
		registry[kind] = true
	}

	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			values := spec.(*ast.ValueSpec)
			for i, name := range values.Names {
				if !strings.HasPrefix(name.Name, "NotifKind") {
					continue
				}
				if i >= len(values.Values) {
					t.Fatalf("%s needs an explicit string value", name.Name)
				}
				literal, ok := values.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s needs a string literal", name.Name)
				}
				kind, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				if !registry[kind] {
					t.Errorf("%s = %q is missing from AllNotifKinds or duplicates another constant", name.Name, kind)
				}
				delete(registry, kind)
			}
		}
	}
	for kind := range registry {
		t.Errorf("AllNotifKinds entry %q has no NotifKind constant", kind)
	}
}

func TestNotificationWriterKindBodyMatchesFilter(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customer, seller, supplier, admin, warehouse, storeID, supplierID, productID := seedNotifyOrderWorld(t, db)
	const (
		orderID     = "ord-filter"
		orderNumber = "ORD-FILTER"
		returnID    = "ret-1"
		amount      = 100000
	)
	seedNotifyOrder(t, db, orderID, orderNumber, customer, "processing", "paid")
	if _, err := db.Exec("UPDATE products SET stock = 5 WHERE id = $1", productID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO support_tickets (id, requester_user_id, requester_role, subject) VALUES ('tick-1', $1, 'supplier', 'کالا')", supplier); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE supplier_warehouses (id TEXT PRIMARY KEY, supplier_id TEXT NOT NULL);
		INSERT INTO supplier_warehouses VALUES ('wh-1', 'sup-1');
		CREATE TABLE customer_return_shipping_charges (return_id TEXT, supplier_id TEXT, bearer TEXT, amount BIGINT);
		INSERT INTO customer_return_shipping_charges VALUES ('ret-1', 'sup-1', 'supplier', 100000);
		INSERT INTO supplier_shipments (id, order_id, supplier_id, status) VALUES ('ship-1', 'ord-filter', 'sup-1', 'pending');
	`); err != nil {
		t.Fatal(err)
	}

	// Kind literals mirror the handler seeds. The three supplier substrings
	// must be present only in the supplier copy, even when kinds coincide.
	tests := []struct {
		name, kind, supplierBody string
		recipients               []string
		write                    func(*testing.T)
	}{
		{"direct shipping", "order_status", "", []string{customer}, func(t *testing.T) {
			if err := (&SupplierShipmentRepository{db: db}).SubmitShipping(ctx, supplierID, "ship-1", "پست", "TRK123", customer); err != nil {
				t.Fatal(err)
			}
		}},
		{"proposal rejected", "proposal_rejected", "", []string{supplier}, func(t *testing.T) {
			NewSupplierRepository(db).notifyProposalRejected(ctx, "prop-1", "کالا", supplierID, "")
		}},
		{"order placed", "order_placed", "", []string{supplier, seller}, func(t *testing.T) {
			notifyOrderPlaced(ctx, db, orderID)
		}},
		{"payment confirmed", "payment_confirmed", "", []string{supplier, seller}, func(t *testing.T) {
			notifyPaymentConfirmed(ctx, db, orderID)
		}},
		{"payment failed", "payment_failed", "", []string{customer}, func(t *testing.T) {
			notifyPaymentFailed(ctx, db, orderID)
		}},
		{"order cancelled", "order_cancelled", "آماده‌سازی آن را متوقف کنید", []string{supplier, seller}, func(t *testing.T) {
			notifyOrderCancelled(ctx, db, orderID)
		}},
		{"order expired", "order_cancelled", "آماده‌سازی آن را متوقف کنید", []string{customer}, func(t *testing.T) {
			notifyExpiredOrder(ctx, db, orderID, orderNumber, "")
		}},
		{"delivery confirmed", "delivery_confirmed", "سود فروشنده پس از پایان مهلت مرجوعی آزاد می‌شود", []string{supplier, customer, seller}, func(t *testing.T) {
			NotifyDeliveryConfirmed(ctx, db, orderID, false)
		}},
		{"delivery auto confirmed", "delivery_confirmed", "سود فروشنده پس از پایان مهلت مرجوعی آزاد می‌شود", []string{supplier, customer, seller}, func(t *testing.T) {
			NotifyDeliveryConfirmed(ctx, db, orderID, true)
		}},
		{"profit released", "profit_released", "", []string{supplier, seller}, func(t *testing.T) {
			notifyProfitReleased(ctx, db, orderID, amount)
		}},
		{"carrier failed", "carrier_failed", "", []string{customer}, func(t *testing.T) {
			notifyCarrierFailed(ctx, db, orderID)
		}},
		{"supplier accepted", "supplier_order_accepted", "", []string{customer}, func(t *testing.T) {
			notifySupplierAccepted(ctx, db, orderID, "کالا")
		}},
		{"low stock", "low_stock", "", []string{supplier, seller}, func(t *testing.T) {
			notifyLowStock(ctx, db, orderID)
		}},
		{"dispute filed", "dispute_filed", "", []string{admin}, func(t *testing.T) {
			notifyDisputeFiled(ctx, db, "disp-1", orderNumber, "damaged", amount)
		}},
		{"dispute supplier liable", "dispute_resolved", "مسئولیت با تأمین‌کننده اعلام شد", []string{supplier, customer}, func(t *testing.T) {
			notifyDisputeResolved(ctx, db, "disp-1", orderID, "refund", "supplier", amount)
		}},
		{"dispute seller liable", "dispute_resolved", "مسئولیت با تأمین‌کننده اعلام شد", []string{seller, customer}, func(t *testing.T) {
			notifyDisputeResolved(ctx, db, "disp-1", orderID, "release_to_seller", "seller", amount)
		}},
		{"refund issued", "refund_issued", "", []string{customer}, func(t *testing.T) {
			notifyRefundIssued(ctx, db, orderID, orderNumber, "", amount, true, "")
		}},
		{"finance adjustment", "finance_adjustment", "", []string{supplier}, func(t *testing.T) {
			NotifyFinanceAdjustment(ctx, db, "supplier", supplierID, "debit", amount, "تعدیل تست", "ADJ-1")
		}},
		{"payout reviewed", "payout_reviewed", "", []string{supplier}, func(t *testing.T) {
			notifyPayoutReviewed(ctx, db, "pay-1", "approved", "", amount, "", supplierID)
		}},
		{"payout transfer confirmed", "payout_transfer_confirmed", "", []string{supplier}, func(t *testing.T) {
			notifyPayoutTransferConfirmed(ctx, db, "pay-1", "REF-1", amount, "", supplierID)
		}},
		{"payout requested", "payout_requested", "", []string{admin}, func(t *testing.T) {
			notifyPayoutRequested(ctx, db, "pay-1", "تسویه", amount, storeID)
		}},
		{"settlement skipped", "settlement_skipped", "", []string{seller}, func(t *testing.T) {
			notifySettlementSkipped(ctx, db, storeID, "1405-08", amount)
		}},
		{"settlement created", "settlement_created", "", []string{seller}, func(t *testing.T) {
			notifySettlementCreated(ctx, db, storeID, "1405-08", "pay-1", amount)
		}},
		{"proposal approved", "proposal_approved", "", []string{supplier}, func(t *testing.T) {
			notifyProposalApproved(ctx, db, "prop-1", "کالا", supplierID, productID, "")
		}},
		{"approval rejected", "approval_rejected", "", []string{supplier}, func(t *testing.T) {
			notifyApprovalRejected(ctx, db, "approval-1", "store", "کالا", supplierID, "")
		}},
		{"supplier registered", "supplier_registered", "", []string{admin}, func(t *testing.T) {
			notifySupplierRegistered(ctx, db, supplierID, "کالا", supplier)
		}},
		{"supplier approved", "supplier_approved", "", []string{supplier}, func(t *testing.T) {
			notifyLegacySupplierDecision(ctx, db, supplierID, "", true, "")
		}},
		{"supplier rejected", "supplier_rejected", "", []string{supplier}, func(t *testing.T) {
			notifyLegacySupplierDecision(ctx, db, supplierID, "", false, "")
		}},
		{"ticket reply", "ticket_reply", "", []string{supplier}, func(t *testing.T) {
			NotifyTicketReply(ctx, db, "tick-1", admin, false, "پاسخ تست")
		}},
		{"return requested", "return_requested", "", []string{warehouse, admin}, func(t *testing.T) {
			notifyReturnRequested(ctx, db, returnID, orderNumber, "آسیب‌دیده")
		}},
		{"return approved", "return_approved", "", []string{customer}, func(t *testing.T) {
			notifyReturnDecision(ctx, db, returnID, orderNumber, customer, "approved", "")
		}},
		{"return rejected", "return_rejected", "", []string{customer}, func(t *testing.T) {
			notifyReturnDecision(ctx, db, returnID, orderNumber, customer, "rejected", "")
		}},
		{"return shipped", "return_shipped", "", []string{supplier}, func(t *testing.T) {
			notifyReturnShipped(ctx, db, returnID, orderNumber, "TRK123", "wh-1")
		}},
		{"return QC charged", "return_qc_charged", "", []string{supplier}, func(t *testing.T) {
			notifyReturnQCCharged(ctx, db, returnID, orderNumber, "RET-1")
		}},
		{"return COD refused", "return_cod_refused", "", []string{customer}, func(t *testing.T) {
			notifyReturnCODRefused(ctx, db, returnID, orderNumber, customer)
		}},
		{"inbound created", "inbound_created", "", []string{warehouse}, func(t *testing.T) {
			notifyInbound(ctx, db, "in-1", "کالا", false)
		}},
		{"inbound dispatched", "inbound_dispatched", "", []string{warehouse}, func(t *testing.T) {
			notifyInbound(ctx, db, "in-1", "کالا", true)
		}},
		{"supplier return reviewed", "supplier_return_reviewed", "", []string{supplier}, func(t *testing.T) {
			notifySupplierReturnReview(ctx, db, returnID, supplierID, "approved", "")
		}},
		{"supplier parcel charge", "supplier_parcel_charge", "", []string{supplier}, func(t *testing.T) {
			notifySupplierParcelCharge(ctx, db, returnID, "parcel-1", supplierID, amount)
		}},
		{"supplier return transit", "supplier_return_in_transit", "", []string{warehouse}, func(t *testing.T) {
			notifySupplierReturnTransit(ctx, db, returnID, supplierID)
		}},
		{"storage fee charged", "storage_fee_charged", "", []string{supplier}, func(t *testing.T) {
			notifyStorageFeeCharged(ctx, db, supplierID, amount, 1)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := db.Exec("DELETE FROM notification_outbox"); err != nil {
				t.Fatal(err)
			}
			tt.write(t)
			rows, err := db.Query("SELECT recipient_user_id, kind, body FROM notification_outbox")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			want := make(map[string]bool, len(tt.recipients))
			for _, recipient := range tt.recipients {
				want[recipient] = true
			}
			for rows.Next() {
				var recipient, kind, body string
				if err := rows.Scan(&recipient, &kind, &body); err != nil {
					t.Fatal(err)
				}
				if !want[recipient] || kind != tt.kind {
					t.Errorf("unexpected row for %s: kind = %q, want %q", recipient, kind, tt.kind)
				}
				if tt.supplierBody != "" && strings.Contains(body, tt.supplierBody) != (recipient == supplier) {
					t.Errorf("recipient %s has unexpected supplier body match for %q: %q", recipient, tt.supplierBody, body)
				}
				delete(want, recipient)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			for recipient := range want {
				t.Errorf("missing %q row for %s", tt.kind, recipient)
			}
		})
	}
}
