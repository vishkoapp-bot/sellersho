package main

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Fix69 (W-ROUTES) R5: POST /finance/adjustments.
//
// Live evidence (live bundles, /opt/drup/dist/assets, read-only):
//   AdminDashboardView-MERGED.js:
//     createAdjustment: POST /finance/adjustments, body passed
//     through verbatim from the adjustments tab form:
//       {targetKind, targetId, direction, amount, reason}
//     and the client reads X.docId from the response.
//   The same tab's form state proves the value domains:
//     targetKind "supplier" (default) or "seller"; the seller list
//     is the stores list and the supplier list comes from
//     /admin/suppliers/list, so targetId is a supplier id or a
//     store id; targetId is validated client-side against
//     /^[A-Za-z0-9][A-Za-z0-9_-]{1,63}$/; direction "debit"
//     (default, "بدهکار کردن (کاهش مانده)") or "credit"
//     ("بستانکار کردن (افزایش مانده)"); amount is a positive
//     integer; reason is required ("علت و سند عطف حسابداری").
//   The tab lists adjustments by reading GET /finance/ledger and
//     grouping entries whose reason starts with "سند تعدیل" by
//     relatedOrderOrDispute, taking the entry whose targetAccount
//     starts with "store_payable:" or "supplier_payable:" as the
//     document row, and its toast calls the document "سند دوبل،
//     غیرقابل ویرایش" (double-entry, immutable).
// Implementation therefore writes TWO ledger_entries sharing one
// docId as related_order_or_dispute, both with reason
// "سند تعدیل " + reason: the payable leg (target_account
// supplier_payable:<id> / store_payable:<id>, type = direction)
// and a platform counterpart leg (type = opposite direction).
// The counterpart account name ("platform") is a candidate-local
// choice, NOT proven live, and is flagged as such. W2 bundle check
// (AdminDashboardView-MERGED.js adjustments tab): the payable leg
// (store_payable:/supplier_payable:), the shared-docId grouping by
// relatedOrderOrDispute, the "سند تعدیل" reason prefix and the
// response docId field ARE live-proven; the counterpart leg's account
// and opposite-direction type are NOT (the live list collapses each
// document to its payable leg and never displays the counterpart). Ledger rows
// are append-only by migration 202610160003, which is what makes
// the document immutable. A finance_adjustments audit row is also
// written (migration 202611040012) when the table exists.
// GET keeps the existing deliberate 501 stub in
// handleT01FinanceAdjustments (finance_parity_handlers.go, NOT
// edited here): the stub stays the GET behaviour; only POST is
// newly implemented, via a dispatch-target swap in main.go
// (required because Go mux forbids registering the same pattern
// twice).

var wr69TargetIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,63}$`)

func wr69FinanceAdjustments(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	switch r.Method {
	case http.MethodGet:
		handleT01FinanceAdjustments(w, r, db)
	case http.MethodPost:
		wr69FinanceAdjustmentCreate(w, r, db)
	default:
		writeMethodNotAllowed(w)
	}
}

func wr69FinanceAdjustmentCreate(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	var req struct {
		TargetKind string `json:"targetKind"`
		TargetID   string `json:"targetId"`
		Direction  string `json:"direction"`
		Amount     int64  `json:"amount"`
		Reason     string `json:"reason"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	if req.TargetKind != "supplier" && req.TargetKind != "seller" {
		http.Error(w, `{"error":"targetKind must be supplier or seller"}`, http.StatusBadRequest)
		return
	}
	targetID := strings.TrimSpace(req.TargetID)
	if !wr69TargetIDRe.MatchString(targetID) {
		http.Error(w, `{"error":"invalid targetId"}`, http.StatusBadRequest)
		return
	}
	if req.Direction != "debit" && req.Direction != "credit" {
		http.Error(w, `{"error":"direction must be debit or credit"}`, http.StatusBadRequest)
		return
	}
	if req.Amount <= 0 {
		http.Error(w, `{"error":"amount must be greater than 0"}`, http.StatusBadRequest)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		http.Error(w, `{"error":"reason is required"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	// Fail closed: the target must be a real supplier / store id.
	var exists int
	var qerr error
	if req.TargetKind == "supplier" {
		qerr = db.QueryRowContext(ctx, `SELECT 1 FROM suppliers WHERE id = $1`, targetID).Scan(&exists)
	} else {
		qerr = db.QueryRowContext(ctx, `SELECT 1 FROM stores WHERE id = $1`, targetID).Scan(&exists)
	}
	if qerr == sql.ErrNoRows {
		http.Error(w, `{"error":"target not found"}`, http.StatusNotFound)
		return
	}
	if qerr != nil {
		writeInternalError(w, qerr)
		return
	}
	// M3 (round 2): server-side duplicate guard — an identical
	// adjustment (same target + direction + amount + reason, same
	// operator) within 10 minutes is rejected with a 409 naming the
	// existing docId. The UI single-flight does not cover
	// response-loss retries or two tabs; this covers the sequential
	// case. Truly concurrent identical POSTs can still both pass this
	// check (check-then-insert, no unique index — no new migrations
	// in this mission); the window + operator match keep the residual
	// narrow and admin-only.
	operator := wr69Operator(r)
	const wr69DuplicateWindow = 10 * time.Minute
	if t01TableExists(ctx, db, "finance_adjustments") {
		var existingDocID string
		dupErr := db.QueryRowContext(ctx, `SELECT id FROM finance_adjustments WHERE target_kind = $1 AND target_id = $2 AND direction = $3 AND amount = $4 AND reason = $5 AND operator = $6 AND created_at > $7 ORDER BY created_at DESC LIMIT 1`,
			req.TargetKind, targetID, req.Direction, req.Amount, reason, operator, time.Now().Add(-wr69DuplicateWindow)).Scan(&existingDocID)
		if dupErr == nil {
			t01WriteJSON(w, http.StatusConflict, map[string]string{"error": "duplicate adjustment within 10 minutes", "docId": existingDocID})
			return
		}
		if dupErr != sql.ErrNoRows {
			writeInternalError(w, dupErr)
			return
		}
	}
	docID := "ADJ-" + uuid.NewString()
	now := time.Now()
	payableAccount := "supplier_payable:" + targetID
	payableType := "supplier"
	if req.TargetKind == "seller" {
		payableAccount = "store_payable:" + targetID
		payableType = "seller"
	}
	counterDirection := "credit"
	if req.Direction == "credit" {
		counterDirection = "debit"
	}
	ledgerReason := "سند تعدیل " + reason
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer tx.Rollback()
	if t01TableExists(ctx, db, "finance_adjustments") {
		if _, err := tx.ExecContext(ctx, `INSERT INTO finance_adjustments (id, target_kind, target_id, direction, amount, reason, operator)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, docID, req.TargetKind, targetID, req.Direction, req.Amount, reason, operator); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	legs := []struct{ account, typ, dir string }{
		{payableAccount, payableType, req.Direction},
		{"platform", "platform", counterDirection},
	}
	for _, leg := range legs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, "ADJ-LED-"+uuid.NewString(), now, leg.account, leg.typ, leg.dir, req.Amount, ledgerReason, operator, docID); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "docId": docID})
}
