package repository

// Customer-return shipping settlement (migration 202610250202, owner
// policy Oct 25 2026). Until now the schema existed but no Go code
// wrote it: ReceiveCustomerReturn records the per-item QC verdict and
// this settlement prices the return parcel with the SAME engine as
// the outbound fare (internal/shippingquote), splits the fare across
// the received items, and charges each item's share by its verdict:
//
//   - defective -> the SUPPLIER bears the share: a
//     customer_return_shipping_charges row (bearer='supplier') plus a
//     platform ledger debit are written at RECEIVE time; the amount
//     feeds SupplierBalance.ReturnCharges, so the supplier's
//     available balance drops (and may go negative, per policy). At
//     REFUND time the same total is credited to the customer wallet
//     (the customer prepaid the courier and is made whole).
//   - ok (taste / wrong choice) -> the CUSTOMER bears the share: no
//     money moves at receive, but the share IS persisted at receive
//     as a bearer='customer' charge row at the receive-time amount
//     (Fix #2 — the shares are computed once, so a later tariff edit
//     cannot re-price the return); at refund time the product refund
//     is credited FIRST and the share is then debited from the
//     wallet, clamped so the balance never goes below zero
//     (customer_wallets.balance has a CHECK (balance >= 0)), and the
//     persisted rows are finalized to the ACTUALLY debited amounts;
//     a clamped shortfall is written off and surfaced in the refund
//     response, never carried as debt.
//
// A parcel that arrives post-paid (customer_returns.prepaid = FALSE)
// is REFUSED: the warehouse keeper cannot pay the postman, so no
// receipt, no QC, no money — cod_refused is set and the caller gets
// ErrReturnCODRefused.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/drup/platform/internal/shippingquote"
	"github.com/google/uuid"
)

// ErrReturnCODRefused rejects a warehouse receive of a post-paid
// (COD) return parcel. The Persian text is the customer/keeper-facing
// message; the handler surfaces it verbatim.
var ErrReturnCODRefused = errors.New("این مرسوله پس‌کرایه است؛ انباردار نمی‌تواند هزینه پست را پرداخت کند و بسته به مشتری برگردانده می‌شود")

// ErrReturnIncompleteQC rejects a warehouse receive whose QC verdict
// list does not cover every item of the return (critic fix F2): a
// partial verdict list must not flip the return to 'received',
// because the refund and the money reversals pay over the received
// set — an un-verdicted item would be paid for without ever being
// QC'd. The Persian text is the keeper-facing message.
var ErrReturnIncompleteQC = errors.New("برای ثبت دریافت مرجوعی، نتیجه کنترل کیفیت همه اقلام باید مشخص باشد")

// Per-item charge reasons recorded on customer_return_shipping_charges.
const (
	returnShippingReasonDefective = "سهم هزینه ارسال مرجوعی — کالای معیوب، به حساب تأمین‌کننده"
	returnShippingReasonTaste     = "سهم هزینه ارسال مرجوعی — انتخاب/پسند مشتری، از کیف پول مشتری"
)

// ReturnShippingSettlement is the money summary of one return's
// shipping split, returned by ReceiveCustomerReturn and
// RefundCustomerReturn so the receive/refund responses can show the
// breakdown without new endpoints. Amounts are integer Toman.
// CustomerDebited/CustomerWrittenOff are refund-stage only (zero at
// receive): CustomerBorneTotal is the customer's full liability,
// CustomerDebited what was actually collected after clamping, and
// CustomerWrittenOff the un-collectable remainder (written off per
// policy, never recorded as collected, never carried as debt).
type ReturnShippingSettlement struct {
	ReturnShippingAmount int64  `json:"returnShippingAmount"`
	ReturnShippingBearer string `json:"returnShippingBearer"`
	SupplierBorneTotal   int64  `json:"supplierBorneTotal"`
	CustomerBorneTotal   int64  `json:"customerBorneTotal"`
	CustomerDebited      int64  `json:"customerDebited,omitempty"`
	CustomerWrittenOff   int64  `json:"customerWrittenOff,omitempty"`
}

// returnItemShare is one received return item's share of the return
// shipping fare, after leg pricing and pro-rata allocation.
type returnItemShare struct {
	ReturnItemID string
	SupplierID   string
	Bearer       string // 'supplier' (defective) | 'customer' (ok)
	Amount       int64
}

// allocateProRata splits total across parts in proportion to their
// weights. The last part with a positive weight absorbs the rounding
// remainder, so the shares sum EXACTLY to total. All-zero weights
// leave all-zero shares unless total is positive, in which case the
// last part takes it (defensive; billable weights are never zero in
// practice because unknown weights fall back to the platform default).
func allocateProRata(total int64, weights []int64) []int64 {
	out := make([]int64, len(weights))
	if total <= 0 || len(weights) == 0 {
		return out
	}
	var sum int64
	last := -1
	for i, w := range weights {
		if w > 0 {
			sum += w
			last = i
		}
	}
	if sum == 0 {
		out[len(weights)-1] = total
		return out
	}
	var allocated int64
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		if i == last {
			out[i] = total - allocated
		} else {
			out[i] = total * w / sum
			allocated += out[i]
		}
	}
	return out
}

// originLegPart is one (item x origin leg) slice of a quantity: the
// leg grouping key, a customer-facing label for the leg, and the
// part's billable weight (per-unit billable grams x part quantity).
type originLegPart struct {
	legKey string
	label  string
	weight int64
}

// spreadItemOverOriginLegs spreads qty over the item's persisted
// order_item_origins rows in leg order (each part capped at the
// row's shipped quantity); a remainder with no (or insufficient)
// origin coverage falls back to one leg derived from the product's
// fulfillment origin — the settlement's legacy fallback. It is the
// SHARED grouping of the receive-time settlement
// (computeReturnShippingShares) and the pre-filing return quote
// (OrderRepository.ComputeReturnQuoteLegs), so a fare quoted before
// filing groups into exactly the legs the settlement will price.
func spreadItemOverOriginLegs(ctx context.Context, tx *sql.Tx, orderItemID string, qty int, billablePerUnit int64, fulfillment, supplierID, originSupplierWH, warehouseName string) ([]originLegPart, error) {
	originRows, err := tx.QueryContext(ctx, `
		SELECT origin_type, COALESCE(warehouse_id, ''), COALESCE(supplier_warehouse_id, ''),
			COALESCE(supplier_id, ''), COALESCE(origin_label, ''), quantity
		FROM order_item_origins
		WHERE order_item_id::text = $1
		ORDER BY leg_index ASC
	`, orderItemID)
	if err != nil {
		return nil, err
	}
	var parts []originLegPart
	remaining := qty
	for originRows.Next() {
		if remaining <= 0 {
			break
		}
		var originType, warehouseID, supplierWarehouseID, originSupplierID, originLabel string
		var originQty int
		if err := originRows.Scan(&originType, &warehouseID, &supplierWarehouseID, &originSupplierID, &originLabel, &originQty); err != nil {
			originRows.Close()
			return nil, err
		}
		partQty := originQty
		if partQty > remaining {
			partQty = remaining
		}
		if partQty <= 0 {
			continue
		}
		remaining -= partQty
		key := "origin:" + originType + ":" + warehouseID + ":" + supplierWarehouseID + ":" + originSupplierID
		parts = append(parts, originLegPart{legKey: key, label: originLabel, weight: billablePerUnit * int64(partQty)})
	}
	if err := originRows.Err(); err != nil {
		originRows.Close()
		return nil, err
	}
	originRows.Close()
	if remaining > 0 {
		// No (or insufficient) persisted origins: fall back to the
		// product's fulfillment origin, mirroring the outbound
		// legOrigin grouping.
		key := "warehouse:" + warehouseName
		label := warehouseName
		if fulfillment == "supplier_direct" {
			key = "supplier:" + supplierID + ":" + originSupplierWH
			if label == "" {
				label = "ارسال مستقیم تأمین‌کننده"
			}
		} else if label == "" {
			label = "انبار مرکزی"
		}
		parts = append(parts, originLegPart{legKey: "fallback:" + key, label: label, weight: billablePerUnit * int64(remaining)})
	}
	return parts, nil
}

// ReturnQuoteLeg is one origin leg of a pre-filing return fare
// quote: the grouping key (the settlement's leg key), a
// customer-facing label, the leg's total billable weight, and its
// fare at the given tariff.
type ReturnQuoteLeg struct {
	Key         string
	Label       string
	WeightGrams int64
	Fare        int64
}

// ComputeReturnQuoteLegs prices the return fare for the given order
// items (order item id -> quantity the customer wants to return)
// from the order's PERSISTED origins — the same leg grouping the
// receive-time settlement applies (spreadItemOverOriginLegs) — so
// the fare quoted before filing equals the fare the warehouse
// settlement will charge for the same returned set (critic fix F3:
// the quote previously grouped by the products' CURRENT fulfillment
// flag while the settlement groups by order_item_origins, and the
// two diverged whenever they disagreed). Origin-less legacy items
// fall back per item to the product's fulfillment origin, exactly
// as the settlement does. Legs come back in first-appearance order
// over the deterministically ordered items.
func (r *OrderRepository) ComputeReturnQuoteLegs(ctx context.Context, orderID string, quantities map[string]int, rates shippingquote.Rates) ([]ReturnQuoteLeg, int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	itemIDs := make([]string, 0, len(quantities))
	for id := range quantities {
		itemIDs = append(itemIDs, id)
	}
	sort.Strings(itemIDs)

	legIndex := map[string]int{}
	var legs []ReturnQuoteLeg
	for _, itemID := range itemIDs {
		qty := quantities[itemID]
		if qty <= 0 {
			continue
		}
		var supplierID, fulfillment, originSupplierWH, warehouseName string
		var weight, lengthMM, widthMM, heightMM int64
		err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(p.supplier_id, ''), COALESCE(p.weight_grams, 0),
				COALESCE(p.length_mm, 0), COALESCE(p.width_mm, 0), COALESCE(p.height_mm, 0),
				COALESCE(p.fulfillment_method, ''), COALESCE(p.origin_supplier_warehouse_id, ''),
				COALESCE(p.warehouse_name, '')
			FROM order_items oi
			JOIN products p ON p.id = oi.product_id
			WHERE oi.id::text = $1 AND oi.order_id = $2
		`, itemID, orderID).Scan(&supplierID, &weight, &lengthMM, &widthMM, &heightMM, &fulfillment, &originSupplierWH, &warehouseName)
		if err != nil {
			return nil, 0, err
		}
		billable := shippingquote.BillableWeightGrams(int(weight), int(lengthMM), int(widthMM), int(heightMM), rates)
		itemParts, err := spreadItemOverOriginLegs(ctx, tx, itemID, qty, billable, fulfillment, supplierID, originSupplierWH, warehouseName)
		if err != nil {
			return nil, 0, err
		}
		for _, ip := range itemParts {
			if idx, ok := legIndex[ip.legKey]; ok {
				legs[idx].WeightGrams += ip.weight
				continue
			}
			legIndex[ip.legKey] = len(legs)
			legs = append(legs, ReturnQuoteLeg{Key: ip.legKey, Label: ip.label, WeightGrams: ip.weight})
		}
	}
	var total int64
	for i := range legs {
		legs[i].Fare = shippingquote.LegFareToman(legs[i].WeightGrams, rates)
		total += legs[i].Fare
	}
	return legs, total, nil
}

// computeReturnShippingShares prices the return parcel for the items
// of returnID that have been QC-received inside tx (received_at set,
// qc_status 'ok' or 'defective') and returns each item's fare share
// plus the total fare.
//
// Lines are built from the RETURN's own items (return_items.qty,
// received only) — never the full ordered quantity. Legs group by the
// persisted order_item_origins: the returned quantity is spread over
// the item's origin rows in leg order (capped at each row's shipped
// quantity); a remainder with no origin coverage (legacy orders
// without origin rows) falls back to one leg derived from the
// product's fulfillment origin. Each leg is priced with
// shippingquote.LegFareToman and the leg fare is allocated across the
// leg's parts pro-rata to billable weight x qty, the last part
// absorbing the rounding remainder.
func computeReturnShippingShares(ctx context.Context, tx *sql.Tx, returnID string, rates shippingquote.Rates) ([]returnItemShare, int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT ri.id, ri.order_item_id, ri.qty, ri.qc_status,
			COALESCE(p.supplier_id, ''), COALESCE(p.weight_grams, 0),
			COALESCE(p.length_mm, 0), COALESCE(p.width_mm, 0), COALESCE(p.height_mm, 0),
			COALESCE(p.fulfillment_method, ''), COALESCE(p.origin_supplier_warehouse_id, ''),
			COALESCE(p.warehouse_name, '')
		FROM return_items ri
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		JOIN products p ON p.id = oi.product_id
		WHERE ri.return_id = $1 AND ri.received_at IS NOT NULL AND ri.qc_status IN ('ok', 'defective')
		ORDER BY ri.id ASC
	`, returnID)
	if err != nil {
		return nil, 0, err
	}
	type receivedItem struct {
		returnItemID, orderItemID     string
		qty                           int
		qcStatus, supplierID          string
		billable                      int64 // per-unit billable grams
		fulfillment, originSupplierWH string
		warehouseName                 string
	}
	var items []receivedItem
	for rows.Next() {
		var it receivedItem
		var weightG, lengthMM, widthMM, heightMM int
		if err := rows.Scan(&it.returnItemID, &it.orderItemID, &it.qty, &it.qcStatus,
			&it.supplierID, &weightG, &lengthMM, &widthMM, &heightMM,
			&it.fulfillment, &it.originSupplierWH, &it.warehouseName); err != nil {
			rows.Close()
			return nil, 0, err
		}
		it.billable = shippingquote.BillableWeightGrams(weightG, lengthMM, widthMM, heightMM, rates)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	if len(items) == 0 {
		return nil, 0, nil
	}

	// A part is one (return item x origin leg) slice of the returned
	// quantity; legs price parts, parts roll up into item shares.
	type part struct {
		itemIdx int
		legKey  string
		weight  int64 // billable grams x part qty
	}
	var parts []part
	for idx, it := range items {
		itemParts, err := spreadItemOverOriginLegs(ctx, tx, it.orderItemID, it.qty, it.billable,
			it.fulfillment, it.supplierID, it.originSupplierWH, it.warehouseName)
		if err != nil {
			return nil, 0, err
		}
		for _, ip := range itemParts {
			parts = append(parts, part{itemIdx: idx, legKey: ip.legKey, weight: ip.weight})
		}
	}

	// Group parts into legs (deterministic order) and price each leg.
	legIndexOf := map[string]int{}
	var legKeys []string
	var legWeights []int64
	var legParts [][]int
	for pi, pt := range parts {
		li, ok := legIndexOf[pt.legKey]
		if !ok {
			li = len(legKeys)
			legIndexOf[pt.legKey] = li
			legKeys = append(legKeys, pt.legKey)
			legWeights = append(legWeights, 0)
			legParts = append(legParts, nil)
		}
		legWeights[li] += pt.weight
		legParts[li] = append(legParts[li], pi)
	}
	itemAmounts := make([]int64, len(items))
	var totalFare int64
	for li := range legKeys {
		fare := shippingquote.LegFareToman(legWeights[li], rates)
		totalFare += fare
		weights := make([]int64, len(legParts[li]))
		for k, pi := range legParts[li] {
			weights[k] = parts[pi].weight
		}
		shares := allocateProRata(fare, weights)
		for k, pi := range legParts[li] {
			itemAmounts[parts[pi].itemIdx] += shares[k]
		}
	}

	out := make([]returnItemShare, 0, len(items))
	for i, it := range items {
		bearer := "customer"
		if it.qcStatus == "defective" {
			bearer = "supplier"
		}
		out = append(out, returnItemShare{
			ReturnItemID: it.returnItemID,
			SupplierID:   it.supplierID,
			Bearer:       bearer,
			Amount:       itemAmounts[i],
		})
	}
	return out, totalFare, nil
}

// returnShippingBearerOf rolls the per-item shares up into the
// return-level bearer ('supplier' | 'customer' | 'mixed'). With a
// zero fare nobody bore a cost, so the verdict mix decides.
func returnShippingBearerOf(shares []returnItemShare) string {
	var supplierTotal, customerTotal int64
	var hasSupplier, hasCustomer bool
	for _, sh := range shares {
		if sh.Bearer == "supplier" {
			hasSupplier = true
			supplierTotal += sh.Amount
		} else {
			hasCustomer = true
			customerTotal += sh.Amount
		}
	}
	switch {
	case supplierTotal > 0 && customerTotal > 0:
		return "mixed"
	case supplierTotal > 0:
		return "supplier"
	case customerTotal > 0:
		return "customer"
	case hasSupplier && hasCustomer:
		return "mixed"
	case hasSupplier:
		return "supplier"
	default:
		return "customer"
	}
}

// insertSupplierReturnShippingCharge writes one bearer='supplier'
// charge row plus its platform ledger debit (mirroring
// ConfirmReturnParcelCharge's ledger shape) and returns the ledger
// entry id stored on the charge row.
func insertSupplierReturnShippingCharge(ctx context.Context, tx *sql.Tx, returnID string, share returnItemShare, actorUserID string) error {
	ledgerID := "RET-LED-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
		VALUES ($1, NOW(), $2, 'supplier', 'debit', $3, $4, $5, $6)
	`, ledgerID, share.SupplierID, share.Amount,
		fmt.Sprintf("هزینه حمل مرجوعی %s / قلم %s", returnID, share.ReturnItemID),
		actorUserID, returnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO customer_return_shipping_charges (id, return_id, return_item_id, supplier_id, bearer, amount, ledger_entry_id, idempotency_key, reason, actor_user_id)
		VALUES ($1, $2, $3, $4, 'supplier', $5, $6, $7, $8, $9)
	`, uuid.NewString(), returnID, share.ReturnItemID, share.SupplierID, share.Amount,
		ledgerID, "crsc:"+returnID+":"+share.ReturnItemID,
		returnShippingReasonDefective, actorUserID); err != nil {
		return err
	}
	return nil
}

// insertCustomerReturnShippingCharge persists one bearer='customer'
// charge row at RECEIVE time with the receive-time amount (Fix #2).
// No ledger entry is written: the customer's share is not a platform
// receivable — it is collected from the customer's wallet at refund,
// where the row is finalized to the actually debited amount. Same
// idempotency key shape as the supplier rows (one row per return
// item; an item's verdict fixes its single bearer).
func insertCustomerReturnShippingCharge(ctx context.Context, tx *sql.Tx, returnID string, share returnItemShare, actorUserID string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO customer_return_shipping_charges (id, return_id, return_item_id, supplier_id, bearer, amount, ledger_entry_id, idempotency_key, reason, actor_user_id)
		VALUES ($1, $2, $3, $4, 'customer', $5, NULL, $6, $7, $8)
	`, uuid.NewString(), returnID, share.ReturnItemID, share.SupplierID, share.Amount,
		"crsc:"+returnID+":"+share.ReturnItemID,
		returnShippingReasonTaste, actorUserID)
	return err
}

// sortedShares returns the shares in the deterministic order used
// for remainder absorption (by return item id).
func sortedShares(shares []returnItemShare) []returnItemShare {
	out := make([]returnItemShare, len(shares))
	copy(out, shares)
	sort.Slice(out, func(i, j int) bool { return out[i].ReturnItemID < out[j].ReturnItemID })
	return out
}
