package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/drup/platform/internal/shippingquote"
	"github.com/google/uuid"
)

// Critic-fix wave tests (F2 partial-receive money hole, Fix #2
// receive-time share persistence, F3 origin-priced return quote).
// They reuse the return-shipping settlement fixture
// (openReturnSettlementDB / rsSeedReturn).

// F2(a): a verdict list covering only SOME items of a return must be
// rejected wholesale — the return stays untouched.
func TestReceiveRejectsPartialVerdictList(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)

	weight := 1000
	returnID, _, itemIDs := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-f2-a", supplierID: "sup-1", weightG: &weight, qty: 1, returnQty: 1, unitPrice: 10000},
		{productID: "p-f2-b", supplierID: "sup-1", weightG: &weight, qty: 1, returnQty: 1, unitPrice: 20000},
	})

	_, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{
		{ReturnItemID: itemIDs[0], QCStatus: "ok"},
	}, "keeper-1")
	if !errors.Is(err, ErrReturnIncompleteQC) {
		t.Fatalf("partial receive err = %v, want ErrReturnIncompleteQC", err)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM customer_returns WHERE id = $1::uuid`, returnID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "approved" {
		t.Fatalf("return status = %q, want still approved", status)
	}
	var receivedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM return_items WHERE return_id = $1::uuid AND received_at IS NOT NULL`, returnID).Scan(&receivedCount); err != nil {
		t.Fatal(err)
	}
	if receivedCount != 0 {
		t.Fatalf("%d items marked received by a rejected partial receive", receivedCount)
	}
	if charges := rsCharges(t, db, returnID); len(charges) != 0 {
		t.Fatalf("charges after rejected receive = %+v, want none", charges)
	}

	// Control: the full verdict list still receives cleanly.
	if _, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{
		{ReturnItemID: itemIDs[0], QCStatus: "ok"},
		{ReturnItemID: itemIDs[1], QCStatus: "ok"},
	}, "keeper-1"); err != nil {
		t.Fatalf("full receive after partial rejection: %v", err)
	}
}

// F2(b): defence in depth — a return hand-crafted into 'received'
// with one item never received must refund and claw back ONLY over
// the received set.
func TestRefundExcludesUnreceivedItems(t *testing.T) {
	db := openGapMoneyDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	gmProduct(t, db, "p-f2-c", "sup-f2-c", 5)
	gmBalance(t, db, "p-f2-c", "sup-f2-c", "warehouse:wh-1", 5)
	gmProduct(t, db, "p-f2-d", "sup-f2-d", 5)
	gmBalance(t, db, "p-f2-d", "sup-f2-d", "warehouse:wh-1", 5)
	gmOrder(t, db, "ord-f2", customerID, "", true, false, 0)
	itemA := gmItem(t, db, "ord-f2", "p-f2-c", 1, 4000, 0, 10000, "")
	itemB := gmItem(t, db, "ord-f2", "p-f2-d", 1, 9000, 0, 20000, "")

	returnID, ri := gmFileReturn(t, repo, customerID, "ord-f2", []CustomerReturnItemRequest{
		{OrderItemID: itemA, Qty: 1},
		{OrderItemID: itemB, Qty: 1},
	})
	// Hand-craft the legacy bad state: return 'received', item A
	// received+QC'd, item B never received.
	if _, err := db.Exec(`UPDATE customer_returns SET status = 'received' WHERE id = $1::uuid`, returnID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE return_items SET received_at = NOW(), qc_status = 'ok' WHERE id = $1::uuid`, ri[itemA]); err != nil {
		t.Fatal(err)
	}

	amount, _, err := repo.RefundCustomerReturn(ctx, returnID, "admin-1")
	if err != nil {
		t.Fatalf("RefundCustomerReturn: %v", err)
	}
	if amount != 10000 {
		t.Fatalf("refund amount = %d, want 10000 (received item only)", amount)
	}
	if got := gmWallet(t, db, customerID); got != 10000 {
		t.Fatalf("wallet = %d, want 10000", got)
	}
	events := map[string]int64{}
	rows, err := db.Query(`SELECT return_item_id::text, amount FROM customer_return_financial_events WHERE return_id = $1::uuid`, returnID)
	if err != nil {
		t.Fatalf("clawback events: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rid string
		var amt int64
		if err := rows.Scan(&rid, &amt); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		events[rid] = amt
	}
	if len(events) != 1 || events[ri[itemA]] != 4000 {
		t.Fatalf("clawback events = %v, want only item A's 4000 (unreceived item B excluded)", events)
	}
}

// Fix #2: a tariff edit between receive and refund must NOT re-price
// the customer's borne total — the refund consumes the shares
// persisted at receive.
func TestRefundUsesReceiveTimeSharesAfterTariffChange(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)

	weight := 1000
	returnID, customerID, itemIDs := rsSeedReturn(t, db, true, []rsItemSpec{
		{productID: "p-fix2-a", supplierID: "sup-1", weightG: &weight, qty: 1, returnQty: 1, unitPrice: 5000},
	})
	// Fixture rates are Rial-denominated (base 100000 + 20000/kg),
	// so the 1kg taste fare is 12000 Toman, borne by the customer.
	settlement, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{
		{ReturnItemID: itemIDs[0], QCStatus: "ok"},
	}, "keeper-1")
	if err != nil {
		t.Fatalf("ReceiveCustomerReturn: %v", err)
	}
	if settlement == nil || settlement.CustomerBorneTotal != 12000 {
		t.Fatalf("receive settlement = %+v, want customerBorne 12000", settlement)
	}

	// The tariff is edited between receive and refund.
	if _, err := db.Exec(`UPDATE platform_settings SET shipping_base_rate = 500000, shipping_per_kg = 20000 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	_, refundSettlement, err := repo.RefundCustomerReturn(ctx, returnID, "admin-1")
	if err != nil {
		t.Fatalf("RefundCustomerReturn: %v", err)
	}
	if refundSettlement == nil {
		t.Fatal("refund settlement is nil")
	}
	// Borne total stays the receive-time 12000 (a recompute at the
	// new tariff would say 52000). The wallet only holds the 5000
	// product refund, so the debit clamps to 5000 and 7000 is
	// written off.
	if refundSettlement.CustomerBorneTotal != 12000 {
		t.Fatalf("customer borne = %d, want receive-time 12000", refundSettlement.CustomerBorneTotal)
	}
	if refundSettlement.CustomerDebited != 5000 || refundSettlement.CustomerWrittenOff != 7000 {
		t.Fatalf("debited/writtenOff = %d/%d, want 5000/7000", refundSettlement.CustomerDebited, refundSettlement.CustomerWrittenOff)
	}
	if got := rsWallet(t, db, customerID); got != 0 {
		t.Fatalf("wallet = %d, want 0 after the clamped debit", got)
	}
	charges := rsCharges(t, db, returnID)
	if len(charges) != 1 || charges[0].Bearer != "customer" || charges[0].Amount != 5000 {
		t.Fatalf("charges after refund = %+v, want one customer row finalized to the debited 5000", charges)
	}
}

// F3: the pre-filing return quote groups by the PERSISTED origins —
// not the products' current fulfillment flags — and equals the
// receive-time settlement fare for the same returned set.
func TestReturnQuoteLegsMatchSettlementWithOriginFlagDisagreement(t *testing.T) {
	db := openReturnSettlementDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)

	weight := 1000
	returnID, _, itemIDs := rsSeedReturn(t, db, true, []rsItemSpec{
		// Flag says warehouse; the persisted origin says supplier.
		{productID: "p-f3-a", supplierID: "sup-1", weightG: &weight, qty: 1, returnQty: 1, unitPrice: 10000},
		// Flag and origin agree: warehouse.
		{productID: "p-f3-b", supplierID: "sup-1", weightG: &weight, qty: 1, returnQty: 1, unitPrice: 10000},
		// Flag says supplier_direct; the persisted origin says warehouse.
		{productID: "p-f3-c", supplierID: "sup-1", weightG: &weight, qty: 1, returnQty: 1, unitPrice: 10000},
	})
	if _, err := db.Exec(`UPDATE products SET fulfillment_method = 'supplier_direct' WHERE id = 'p-f3-c'`); err != nil {
		t.Fatal(err)
	}
	// Custom origins (rsSeedReturn only seeds warehouse origins).
	orderItemOf := func(returnItemID string) string {
		var oi string
		if err := db.QueryRow(`SELECT order_item_id FROM return_items WHERE id = $1::uuid`, returnItemID).Scan(&oi); err != nil {
			t.Fatal(err)
		}
		return oi
	}
	var orderID string
	if err := db.QueryRow(`SELECT order_id FROM customer_returns WHERE id = $1::uuid`, returnID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	origins := []struct {
		item        string
		typ, wh, swh string
		label       string
	}{
		{orderItemOf(itemIDs[0]), "supplier_direct", "", "swh-9", "انبار تأمین‌کننده"},
		{orderItemOf(itemIDs[1]), "warehouse", "wh-rs", "", "انبار مرکزی"},
		{orderItemOf(itemIDs[2]), "warehouse", "wh-rs", "", "انبار مرکزی"},
	}
	quantities := map[string]int{}
	for _, o := range origins {
		if _, err := db.Exec(`INSERT INTO order_item_origins (id, order_id, order_item_id, leg_index, origin_type, supplier_id, warehouse_id, supplier_warehouse_id, origin_label, quantity) VALUES ($1, $2, $3, 0, $4, 'sup-1', $5, $6, $7, 1)`,
			uuid.NewString(), orderID, o.item, o.typ, sql.NullString{String: o.wh, Valid: o.wh != ""}, sql.NullString{String: o.swh, Valid: o.swh != ""}, o.label); err != nil {
			t.Fatalf("seed origin: %v", err)
		}
		quantities[o.item] = 1
	}

	rates, err := shippingquote.LoadRates(ctx, db)
	if err != nil {
		t.Fatalf("LoadRates: %v", err)
	}
	legs, total, err := repo.ComputeReturnQuoteLegs(ctx, orderID, quantities, rates)
	if err != nil {
		t.Fatalf("ComputeReturnQuoteLegs: %v", err)
	}
	// Origin grouping: one supplier leg (1kg -> 12000 Toman) + one
	// warehouse leg (2kg -> 14000) = 26000. Flag grouping would have
	// produced a different membership split (flags say a,b warehouse
	// and c supplier), and for two same-flag/different-origin items
	// a single merged leg.
	if len(legs) != 2 {
		t.Fatalf("quote legs = %+v, want 2 origin legs", legs)
	}
	if total != 26000 {
		t.Fatalf("quote total = %d, want 26000", total)
	}
	byKey := map[string]int64{}
	for _, leg := range legs {
		byKey[leg.Key] = leg.Fare
	}
	if byKey["origin:supplier_direct::swh-9:sup-1"] != 12000 {
		t.Fatalf("supplier origin leg fare = %d, want 12000 (legs %+v)", byKey["origin:supplier_direct::swh-9:sup-1"], legs)
	}
	if byKey["origin:warehouse:wh-rs::sup-1"] != 14000 {
		t.Fatalf("warehouse origin leg fare = %d, want 14000 (legs %+v)", byKey["origin:warehouse:wh-rs::sup-1"], legs)
	}

	// The receive-time settlement prices the same returned set at
	// exactly the quoted total.
	settlement, err := repo.ReceiveCustomerReturn(ctx, returnID, []CustomerReturnItemQC{
		{ReturnItemID: itemIDs[0], QCStatus: "defective"},
		{ReturnItemID: itemIDs[1], QCStatus: "ok"},
		{ReturnItemID: itemIDs[2], QCStatus: "ok"},
	}, "keeper-1")
	if err != nil {
		t.Fatalf("ReceiveCustomerReturn: %v", err)
	}
	if settlement == nil || settlement.ReturnShippingAmount != total {
		t.Fatalf("settlement fare = %+v, want it to equal the quote total %d", settlement, total)
	}
}
