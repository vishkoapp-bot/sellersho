// FIN-014 dry-run finance reconciliation report (read-only).
//
// This is the finance analogue of historical_reconciliation_report.go
// (inventory only): a per-record, cursor-paged, classification-carrying
// report over historical money data. It NEVER writes, NEVER proposes a
// cash movement for anything but verified gateway cash, and NEVER
// "resolves" an unknown by guessing.
//
// Detection composes the existing detectors instead of rewriting them:
// the FIN-013 oracle checks (scripts/fin013-oracle.sql), the five audit
// scripts (scripts/audit-*.sql), and the inventory report's bounding
// pattern (cursor paging, repeatable-read total+page, evidence caps of
// 50 entries x 256 bytes with EvidenceTotal/EvidenceTruncated).
//
// Defect classes (audit §2): payout_oversubscription,
// paid_cancellation_no_refund, duplicate_parcel_charge,
// resolved_but_held_dispute, storage_history_gap,
// coupon_oversubscription, statement_mismatch, uncertain_psp_outcome.
//
// cash_basis (audit §6 cross-cutting): verified_gateway / simulated /
// unverified / unknown. A simulated receipt is NEVER real cash; an
// unknown outcome is NEVER resolved by guessing.
//
// Cutover switch (audit §8 Q3, unanswered): there is deliberately NO
// writer switch in this file. The report/proposal/apply tooling is
// built with the switch point marked as a named gate (see
// fin014_repair_batch.go: FIN014CutoverGate); the flag is wired only
// when Q3 lands.
package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/lib/pq"
)

// FIN-014 defect classes (audit §2). The string values are stable:
// they are stored in fin014_repair_operations and in release evidence.
const (
	FIN014ClassPayoutOversubscription = "payout_oversubscription"
	FIN014ClassPaidCancellation       = "paid_cancellation_no_refund"
	FIN014ClassDuplicateParcelCharge  = "duplicate_parcel_charge"
	FIN014ClassResolvedButHeldDispute = "resolved_but_held_dispute"
	FIN014ClassStorageHistoryGap      = "storage_history_gap"
	FIN014ClassCouponOversubscription = "coupon_oversubscription"
	FIN014ClassStatementMismatch      = "statement_mismatch"
	FIN014ClassUncertainPSPOutcome    = "uncertain_psp_outcome"
)

// FIN014Classes lists every defect class in deterministic order.
var FIN014Classes = []string{
	FIN014ClassPayoutOversubscription,
	FIN014ClassPaidCancellation,
	FIN014ClassDuplicateParcelCharge,
	FIN014ClassResolvedButHeldDispute,
	FIN014ClassStorageHistoryGap,
	FIN014ClassCouponOversubscription,
	FIN014ClassStatementMismatch,
	FIN014ClassUncertainPSPOutcome,
}

// FIN-014 cash basis (audit §6 cross-cutting). Only verified_gateway
// is real cash; everything else must never fund a payout/refund.
const (
	FIN014CashVerifiedGateway = "verified_gateway"
	FIN014CashSimulated       = "simulated"
	FIN014CashUnverified      = "unverified"
	FIN014CashUnknown         = "unknown"
)

// FIN-014 confidence: certain defects are repair-eligible; ambiguous
// and unresolved cases are held for a reviewer and excluded from any
// automatic payout/refund decision (see TestFIN014UnresolvedExcluded).
const (
	FIN014ConfidenceCertain    = "certain"
	FIN014ConfidenceAmbiguous  = "ambiguous"
	FIN014ConfidenceUnresolved = "unresolved"
)

// FIN-014 report bounding (mirrors the inventory report, PERF-02):
// default/max page limits and the nested-evidence cap.
const (
	FIN014DefaultReportLimit = 50
	FIN014MaxReportLimit     = 100
	FIN014MaxRowEvidence     = 50
	FIN014MaxEvidenceLength  = 256
)

// FIN014ReportRow is one defect/ambiguity found by the dry-run report.
// It is intentionally read-only: it never assigns ownership, releases
// profit, or moves money.
type FIN014ReportRow struct {
	Class             string   `json:"class"`
	RecordID          string   `json:"recordId"`
	RecordKind        string   `json:"recordKind"`
	StoreID           string   `json:"storeId,omitempty"`
	OrderID           string   `json:"orderId,omitempty"`
	AmountToman       int64    `json:"amountToman"`
	CashBasis         string   `json:"cashBasis"`
	Confidence        string   `json:"confidence"`
	Explanation       string   `json:"explanation"`
	Evidence          []string `json:"evidence"`
	EvidenceTotal     int      `json:"evidenceTotal"`
	EvidenceTruncated bool     `json:"evidenceTruncated"`
	// Proposal fields (filled by the W5 proposal step, empty in a
	// bare W1 report): the content-derived operation ID, the
	// expected-before predicate and the proposed effect.
	OperationID    string          `json:"operationId,omitempty"`
	ExpectedBefore json.RawMessage `json:"expectedBefore,omitempty"`
	ProposedEffect json.RawMessage `json:"proposedEffect,omitempty"`
}

// FIN014Report is one dry-run execution: totals plus one page of rows.
// The total is read in the same repeatable-read transaction as the
// page, so it agrees with the page snapshot (inventory-report pattern).
// The report is a pure function of DB state: it carries NO wall-clock
// timestamp, so two runs over the same state are byte-identical (the
// W1 read-only gate). Provenance (when the report ran) belongs on the
// fin014_report_runs row (W4), not in the report bytes.
type FIN014Report struct {
	SchemaVersion   int               `json:"schemaVersion"`
	Totals          map[string]int    `json:"totals"`
	CertainCount    int               `json:"certainCount"`
	AmbiguousCount  int               `json:"ambiguousCount"`
	UnresolvedCount int               `json:"unresolvedCount"`
	TotalRows       int               `json:"totalRows"`
	Rows            []FIN014ReportRow `json:"rows"`
	NextCursor      string            `json:"nextCursor,omitempty"`
	// FIN011Pinned records the known-open FIN-011 referral asymmetry
	// (oracle check 7): reversal 5000 vs conserved 7500, 2500
	// Toman/case undercount. It is PINNED, not fixed — the report
	// carries it as a known-open item and never edits it away.
	FIN011Pinned bool   `json:"fin011Pinned"`
	FIN011Note   string `json:"fin011Note,omitempty"`
}

// FIN014ReportOptions bounds one report page. IDPrefix scopes every
// detector to record IDs with that prefix (the W2 fixture uses
// "fin014-"); production runs pass "" for the whole history. Cursor is
// the opaque page token from a previous report.
type FIN014ReportOptions struct {
	Limit    int
	Cursor   string
	IDPrefix string
}

func fin014ReportLimit(limit int) int {
	if limit <= 0 {
		return FIN014DefaultReportLimit
	}
	if limit > FIN014MaxReportLimit {
		return FIN014MaxReportLimit
	}
	return limit
}

func fin014EncodeCursor(class, recordID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(class + "\x00" + recordID))
}

func fin014DecodeCursor(cursor string) (class, recordID string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", fmt.Errorf("invalid fin014 report cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), "\x00", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid fin014 report cursor")
	}
	return parts[0], parts[1], nil
}

// fin014CashBasisSQL is the shared per-order cash-basis expression:
// a gateway receipt is verified cash; a simulated receipt is not real
// cash; a payment without a receipt is unverified; no payment at all
// is unknown. orders alias "o" must be in scope.
const fin014CashBasisSQL = `CASE
	WHEN EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id=o.id AND r.funding_kind='gateway') THEN 'verified_gateway'
	WHEN EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id=o.id AND r.funding_kind='simulated') THEN 'simulated'
	WHEN EXISTS (SELECT 1 FROM payment_transactions p WHERE p.order_id=o.id) THEN 'unverified'
	ELSE 'unknown' END`

// fin014DetectorQueries returns the eight class detectors (audit §2),
// each as (class, SQL). Every query takes $1 = the ID prefix filter
// ("" matches everything via LIKE '%') and returns columns:
// record_id, record_kind, store_id, order_id, amount_toman,
// cash_basis, confidence, explanation, evidence (text[]).
func fin014DetectorQueries() [][2]string {
	return [][2]string{
		{FIN014ClassPayoutOversubscription, `
			SELECT p.id, 'payout_request', p.store_id, NULL::text, p.amount,
				'unverified', 'certain',
				'payout request exceeds signed availability (seller ledger net minus other pending/approved payouts)',
				ARRAY['payout:'||p.id, 'store:'||p.store_id]
			FROM payout_requests p
			WHERE p.id LIKE $1||'%' AND p.status IN ('pending','approved')
			  AND p.amount > (
				SELECT COALESCE(SUM(CASE WHEN l.type='credit' THEN l.amount ELSE -l.amount END),0)
				FROM ledger_entries l WHERE l.target_account=p.store_id AND l.target_type='seller')
			  - (SELECT COALESCE(SUM(p2.amount),0) FROM payout_requests p2
				WHERE p2.store_id=p.store_id AND p2.id<>p.id AND p2.status IN ('pending','approved'))
			UNION ALL
			SELECT i.id, 'settlement_item', i.store_id, NULL::text, i.net_amount,
				'unverified', 'certain',
				'settlement item net exceeds the store released profit for the period',
				ARRAY['settlement_item:'||i.id, 'cycle:'||i.cycle_period]
			FROM monthly_settlement_items i
			WHERE i.id LIKE $1||'%' AND i.net_amount > (
				SELECT COALESCE(SUM(o.dropshipper_profit),0) FROM orders o
				WHERE o.store_id=i.store_id AND o.profit_released)`},
		{FIN014ClassPaidCancellation, `
			SELECT o.id, 'order', o.store_id, o.id, o.payable_amount,
				` + fin014CashBasisSQL + `, 'certain',
				'paid order cancelled with no cancellation refund obligation',
				ARRAY['order:'||o.id]
			FROM orders o WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND o.status='cancelled'
			  AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')
			UNION ALL
			SELECT o.id, 'order', o.store_id, o.id, o.payable_amount,
				` + fin014CashBasisSQL + `, 'certain',
				'paid order cancelled with duplicate cancellation refund obligations',
				ARRAY['order:'||o.id]
			FROM orders o WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND o.status='cancelled'
			  AND (SELECT COUNT(*) FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation') > 1
			UNION ALL
			SELECT o.id, 'order', o.store_id, o.id, o.payable_amount,
				` + fin014CashBasisSQL + `, 'unresolved',
				'cancellation refund obligation exists but its outcome is unknown',
				ARRAY['order:'||o.id]
			FROM orders o JOIN order_refund_obligations r ON r.order_id=o.id AND r.purpose='cancellation'
			WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND o.status='cancelled' AND r.status='unknown'`},
		{FIN014ClassDuplicateParcelCharge, `
			-- One row per anomalous parcel. A parcel is anomalous while
			-- any debit event is uncorrected: a same-amount duplicate
			-- debit with no reversal of that amount, a debit with no
			-- ledger leg and no compensating ledger entry referencing
			-- the event, or a ledger entry shared by two uncompensated
			-- events. A posted correction clears the row without ever
			-- rewriting the original events (compensating events only).
			SELECT pr.id, 'parcel', NULL::text, NULL::text,
				(SELECT COALESCE(SUM(e.amount),0) FROM supplier_return_financial_events e WHERE e.parcel_id=pr.id AND e.operation='debit'),
				'unknown', 'certain',
				'parcel charge anomaly: duplicate debit, missing ledger leg, or shared ledger entry — no correction recorded',
				ARRAY['parcel:'||pr.id]
			FROM supplier_return_parcels pr WHERE pr.id LIKE $1||'%'
			  AND EXISTS (
				SELECT 1 FROM supplier_return_financial_events e WHERE e.parcel_id=pr.id AND e.operation='debit'
				AND (
				  (SELECT COUNT(*) FROM supplier_return_financial_events e2 WHERE e2.parcel_id=pr.id AND e2.operation='debit' AND e2.amount=e.amount)
				  - (SELECT COUNT(*) FROM supplier_return_financial_events e3 WHERE e3.parcel_id=pr.id AND e3.operation='reversal' AND e3.amount=e.amount) > 1
				  OR (e.ledger_entry_id IS NULL AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.reason LIKE '%'||e.id||'%'))
				  OR (e.ledger_entry_id IS NOT NULL
					AND (SELECT COUNT(*) FROM supplier_return_financial_events e4 WHERE e4.ledger_entry_id=e.ledger_entry_id AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.reason LIKE '%'||e4.id||'%')) > 1
					AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.reason LIKE '%'||e.id||'%'))
				)
			  )`},
		{FIN014ClassResolvedButHeldDispute, `
			-- Certain: dispute resolved in the seller's favour
			-- (release_to_seller) with an audited FIN-005 review row,
			-- but the order's profit was never released. The
			-- refund-verdict-without-obligation shape is also detected
			-- (second branch) for pre-fence legacy data, but it cannot
			-- be seeded on the current schema: the FIN-005
			-- decision_consistency trigger requires a dispute
			-- obligation equal to the claim before a refund verdict
			-- can commit (see the W2 fixture notes).
			SELECT d.id, 'dispute', d.store_id, d.order_id, d.claim_amount,
				` + fin014CashBasisSQL + `, 'certain',
				'dispute resolved in the seller favour (release_to_seller) with an audited review row, but the order profit was never released',
				ARRAY['dispute:'||d.id, 'order:'||d.order_id]
			FROM disputes d JOIN orders o ON o.id=d.order_id
			WHERE d.id LIKE $1||'%' AND d.status='resolved' AND d.resolution_action='release_to_seller'
			  AND NOT o.profit_released
			  AND EXISTS (SELECT 1 FROM audit_logs a WHERE a.action='FIN005 reviewed legacy evidence' AND a.details LIKE '%'||d.id||'%')
			UNION ALL
			SELECT d.id, 'dispute', d.store_id, d.order_id, d.claim_amount,
				` + fin014CashBasisSQL + `, 'certain',
				'dispute resolved with a refund verdict but no refund obligation was recorded',
				ARRAY['dispute:'||d.id, 'order:'||d.order_id]
			FROM disputes d JOIN orders o ON o.id=d.order_id
			WHERE d.id LIKE $1||'%' AND d.status='resolved' AND d.resolution_action LIKE 'refund%'
			  AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=d.order_id AND r.purpose='dispute' AND r.business_id=d.id)
			UNION ALL
			SELECT d.id, 'dispute', d.store_id, d.order_id, d.claim_amount,
				` + fin014CashBasisSQL + `, 'ambiguous',
				'dispute resolved in the seller favour but no audited review row exists (review_provenance_missing) — held for reviewer',
				ARRAY['dispute:'||d.id, 'order:'||d.order_id]
			FROM disputes d JOIN orders o ON o.id=d.order_id
			WHERE d.id LIKE $1||'%' AND d.status='resolved' AND d.resolution_action='release_to_seller'
			  AND NOT o.profit_released
			  AND NOT EXISTS (SELECT 1 FROM audit_logs a WHERE a.action='FIN005 reviewed legacy evidence' AND a.details LIKE '%'||d.id||'%')`},
		{FIN014ClassStorageHistoryGap, `
			-- Unresolved: a receipt lot whose history cannot be
			-- re-derived from receipt facts (merged history — never
			-- guessed), or a position carrying fee charges with no
			-- receipt lot at all (the lot insert guard requires a
			-- full inbound-QC chain, so a missing lot is itself the
			-- gap: charges exist that no receipt facts support).
			SELECT l.id, 'receipt_lot', NULL::text, NULL::text, 0,
				'unknown', 'unresolved',
				'receipt lot history is unresolved ('||l.history_status||'): charges cannot be re-derived from receipt facts — never guessed',
				ARRAY['lot:'||l.id, 'status:'||l.history_status]
			FROM warehouse_storage_receipt_lots l WHERE l.id LIKE $1||'%' AND l.history_status<>'verified'
			UNION ALL
			SELECT p.id, 'storage_position', NULL::text, NULL::text,
				(SELECT COALESCE(SUM(c.amount_toman),0) FROM warehouse_storage_fee_charges c WHERE c.position_id=p.id),
				'unknown', 'unresolved',
				'storage position has fee charges but no receipt lot — charges cannot be re-derived from receipt facts, never guessed',
				ARRAY['position:'||p.id]
			FROM warehouse_storage_positions p WHERE p.id LIKE $1||'%'
			  AND EXISTS (SELECT 1 FROM warehouse_storage_fee_charges c WHERE c.position_id=p.id)
			  AND NOT EXISTS (SELECT 1 FROM warehouse_storage_receipt_lots l WHERE l.id=p.id)
			UNION ALL
			SELECT c.id, 'storage_charge', NULL::text, NULL::text, c.amount_toman,
				'unknown', 'certain',
				'storage charge violates the per-position-per-day uniqueness or positivity rule (FIN-013 oracle check 8)',
				ARRAY['charge:'||c.id, 'position:'||c.position_id]
			FROM warehouse_storage_fee_charges c WHERE c.id LIKE $1||'%'
			  AND (c.amount_toman<=0 OR c.qty<=0 OR EXISTS (SELECT 1 FROM warehouse_storage_fee_charges c2 WHERE c2.position_id=c.position_id AND c2.fee_date=c.fee_date AND c2.id<>c.id))`},
		{FIN014ClassCouponOversubscription, `
			SELECT c.id, 'coupon', c.store_id, NULL::text, NULL::bigint,
				'unknown', 'certain',
				'coupon uses exceed max_uses (used_count > max_uses)',
				ARRAY['coupon:'||c.id]
			FROM seller_discount_codes c
			WHERE c.id LIKE $1||'%' AND c.max_uses IS NOT NULL AND c.used_count > c.max_uses
			UNION ALL
			SELECT c.id, 'coupon', c.store_id, NULL::text, NULL::bigint,
				'unknown', 'certain',
				'coupon used_count disagrees with occupying reservations (reserved/consumed/review_hold)',
				ARRAY['coupon:'||c.id]
			FROM seller_discount_codes c
			WHERE c.id LIKE $1||'%'
			  AND c.used_count <> (SELECT COUNT(*) FROM order_coupon_reservations r WHERE r.coupon_id=c.id AND r.state IN ('reserved','consumed','review_hold'))`},
		{FIN014ClassStatementMismatch, `
			SELECT o.id, 'order', o.store_id, o.id, o.dropshipper_profit,
				` + fin014CashBasisSQL + `, 'certain',
				'released profit has a live dispute/admin refund obligation with no compensating seller-profit reversal',
				ARRAY['order:'||o.id]
			FROM orders o WHERE o.id LIKE $1||'%' AND o.profit_released AND o.status IN ('delivered','delivered_confirmed')
			  AND EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose IN ('dispute','admin_refund')
				AND COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=r.id ORDER BY e.event_no DESC LIMIT 1), r.status) IN ('pending','succeeded','unknown'))
			  AND NOT EXISTS (SELECT 1 FROM seller_profit_reversals s WHERE s.order_id=o.id)
			UNION ALL
			SELECT o.id, 'order', o.store_id, o.id, o.dropshipper_profit,
				` + fin014CashBasisSQL + `, 'ambiguous',
				'released profit is invisible to the statement builder (order status is outside the released_profit projection) — held for reviewer to confirm delivery',
				ARRAY['order:'||o.id]
			FROM orders o WHERE o.id LIKE $1||'%' AND o.profit_released AND o.status NOT IN ('delivered','delivered_confirmed')
			UNION ALL
			SELECT s.store_id||':2026-10', 'store_month', s.store_id, NULL::text, 0,
				'unknown', 'ambiguous',
				'statement opening for 2026-10 is unverified (no FIN-014 cutover record) — opening balance cannot be trusted',
				ARRAY['store:'||s.store_id, 'month:2026-10']
			FROM (SELECT DISTINCT target_account AS store_id FROM ledger_entries
				WHERE target_account LIKE $1||'%' AND target_type='seller' AND date < '2026-10-01'::timestamptz) s`},
		{FIN014ClassUncertainPSPOutcome, `
			SELECT o.id, 'order', o.store_id, o.id, o.payable_amount,
				'unknown', 'unresolved',
				'paid order has no payment record at all (legacy pre-payment-model shape)',
				ARRAY['order:'||o.id]
			FROM orders o WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND o.status<>'cancelled'
			  AND NOT EXISTS (SELECT 1 FROM payment_transactions p WHERE p.order_id=o.id)
			UNION ALL
			SELECT o.id, 'order', o.store_id, o.id, o.payable_amount,
				'unverified', 'unresolved',
				'payment verification exists but no receipt was ever recorded — provider outcome unknown',
				ARRAY['order:'||o.id]
			FROM orders o WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND o.status<>'cancelled'
			  AND EXISTS (SELECT 1 FROM order_payment_verifications v WHERE v.order_id=o.id)
			  AND NOT EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id=o.id)
			UNION ALL
			SELECT o.id, 'order', o.store_id, o.id, o.payable_amount,
				'simulated', 'ambiguous',
				'only a simulated receipt exists — simulated funding is not real cash',
				ARRAY['order:'||o.id]
			FROM orders o WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND o.status<>'cancelled'
			  AND EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id=o.id AND r.funding_kind='simulated')
			  AND NOT EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id=o.id AND r.funding_kind='gateway')
			UNION ALL
			SELECT o.id, 'order', o.store_id, o.id, o.payable_amount,
				'unverified', 'unresolved',
				'paid payment exists but no verification and no receipt — manual-bridge shape, outcome unverified',
				ARRAY['order:'||o.id]
			FROM orders o WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND o.status<>'cancelled'
			  AND EXISTS (SELECT 1 FROM payment_transactions p WHERE p.order_id=o.id)
			  AND NOT EXISTS (SELECT 1 FROM order_payment_verifications v WHERE v.order_id=o.id)
			  AND NOT EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id=o.id)`},
	}
}

// fin014CapEvidence enforces the 50 x 256B evidence cap on one row.
func fin014CapEvidence(evidence []string) (capped []string, total int, truncated bool) {
	total = len(evidence)
	capped = make([]string, 0, len(evidence))
	for _, e := range evidence {
		if len(capped) >= FIN014MaxRowEvidence {
			truncated = true
			break
		}
		if len(e) > FIN014MaxEvidenceLength {
			e = e[:FIN014MaxEvidenceLength]
		}
		capped = append(capped, e)
	}
	if total > len(capped) {
		truncated = true
	}
	return capped, total, truncated
}

// fin014SchemaVersion returns the count of applied migrations (the
// schema version carried in release evidence).
func fin014SchemaVersion(ctx context.Context, tx *sql.Tx) (int, error) {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// GetFIN014Report runs the read-only dry-run report. It opens one
// repeatable-read read-only transaction, runs all eight detectors
// against the same snapshot, and pages the merged rows in deterministic
// (class, record_id) order. It writes nothing.
func GetFIN014Report(ctx context.Context, db *sql.DB, opts FIN014ReportOptions) (*FIN014Report, error) {
	limit := fin014ReportLimit(opts.Limit)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	schemaVersion, err := fin014SchemaVersion(ctx, tx)
	if err != nil {
		return nil, err
	}
	prefix := opts.IDPrefix
	var rows []FIN014ReportRow
	for _, det := range fin014DetectorQueries() {
		class, query := det[0], det[1]
		qrows, err := tx.QueryContext(ctx, query, prefix)
		if err != nil {
			return nil, fmt.Errorf("fin014 detector %s: %w", class, err)
		}
		for qrows.Next() {
			var r FIN014ReportRow
			var storeID, orderID sql.NullString
			var amount sql.NullInt64
			var evidence []string
			r.Class = class
			if err := qrows.Scan(&r.RecordID, &r.RecordKind, &storeID, &orderID, &amount, &r.CashBasis, &r.Confidence, &r.Explanation, pq.Array(&evidence)); err != nil {
				qrows.Close()
				return nil, fmt.Errorf("fin014 detector %s scan: %w", class, err)
			}
			if storeID.Valid {
				r.StoreID = storeID.String
			}
			if orderID.Valid {
				r.OrderID = orderID.String
			}
			if amount.Valid {
				r.AmountToman = amount.Int64
			}
			if evidence == nil {
				evidence = []string{}
			}
			r.Evidence, r.EvidenceTotal, r.EvidenceTruncated = fin014CapEvidence(evidence)
			rows = append(rows, r)
		}
		if err := qrows.Err(); err != nil {
			qrows.Close()
			return nil, fmt.Errorf("fin014 detector %s: %w", class, err)
		}
		qrows.Close()
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Class != rows[j].Class {
			return rows[i].Class < rows[j].Class
		}
		return rows[i].RecordID < rows[j].RecordID
	})
	report := &FIN014Report{
		SchemaVersion: schemaVersion,
		Totals:        map[string]int{},
		Rows:          []FIN014ReportRow{},
		FIN011Pinned:  true,
		FIN011Note:    "FIN-011 referral asymmetry pinned, not fixed: reversal 5000 vs conserved 7500, undercount 2500 Toman/case (oracle check 7 expected to fail until the FIN-011 follow-up lands; audit §8 Q6).",
	}
	for _, r := range rows {
		report.Totals[r.Class]++
		switch r.Confidence {
		case FIN014ConfidenceCertain:
			report.CertainCount++
		case FIN014ConfidenceAmbiguous:
			report.AmbiguousCount++
		case FIN014ConfidenceUnresolved:
			report.UnresolvedCount++
		}
	}
	report.TotalRows = len(rows)
	// Cursor paging over the merged, deterministically ordered rows.
	start := 0
	if opts.Cursor != "" {
		class, recordID, err := fin014DecodeCursor(opts.Cursor)
		if err != nil {
			return nil, err
		}
		for i, r := range rows {
			if r.Class > class || (r.Class == class && r.RecordID > recordID) {
				start = i
				break
			}
			start = i + 1
		}
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	report.Rows = rows[start:end]
	if end < len(rows) {
		report.NextCursor = fin014EncodeCursor(rows[end-1].Class, rows[end-1].RecordID)
	}
	return report, nil
}

// FIN014ReportJSON renders one report as indented JSON (the
// fin014-report CLI output format: one command, one JSON report).
func FIN014ReportJSON(report *FIN014Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

// FIN014ProposeBatch builds a concrete repair proposal from a report
// (audit §5.1, W5). Every certain defect gets exactly one operation;
// every ambiguous/unresolved case gets a held operation with a
// reason. Operation IDs are content-derived (FIN014OperationID), so
// re-proposing the same report regenerates the same IDs. The builder
// reads the live rows it needs (payment ids, obligation ids,
// occupying reservation counts, the anomalous parcel event) from the
// database in a read-only snapshot; if a row it needs is gone, it
// fails loudly instead of inventing an effect. Expected-before
// predicates capture the state the apply command must re-check
// in-transaction before running the effect.
//
// Cash gating: no cash-moving operation is proposed for anything but
// verified_gateway — except create_cancellation_obligation, which
// moves no cash itself: its guard parks an unverified source as
// 'unknown', and the money only moves at a succeeded refund event
// (which the guard requires a retained provider attempt + gateway
// receipt for). Class-8 rows are held lookup-only until FIN-010.
func FIN014ProposeBatch(ctx context.Context, db *sql.DB, report *FIN014Report, batchID, runID string) (*FIN014Proposal, error) {
	if report == nil {
		return nil, fmt.Errorf("fin014 proposal needs a report")
	}
	if batchID == "" || runID == "" {
		return nil, fmt.Errorf("fin014 proposal needs a batch id and a run id")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	proposal := &FIN014Proposal{BatchID: batchID, ReportRunID: runID}
	for _, row := range report.Rows {
		op, err := fin014ProposeOperation(ctx, tx, row)
		if err != nil {
			return nil, fmt.Errorf("fin014 propose %s/%s: %w", row.Class, row.RecordID, err)
		}
		proposal.Operations = append(proposal.Operations, op)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := ValidateFIN014Proposal(proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

// fin014Held builds a held operation (action none + reason) for a row
// that must not be auto-repaired.
func fin014Held(row FIN014ReportRow, reason string) FIN014Operation {
	effect, _ := json.Marshal(map[string]any{"action": "none", "reason": reason})
	return FIN014Operation{
		ID:             FIN014OperationID(row.Class, []string{row.RecordID}, effect),
		Class:          row.Class,
		RecordID:       row.RecordID,
		RecordKind:     row.RecordKind,
		StoreID:        row.StoreID,
		OrderID:        row.OrderID,
		Effect:         map[string]any{"action": "none", "reason": reason},
		ExpectedBefore: nil,
		Explanation:    row.Explanation,
		CashBasis:      row.CashBasis,
		Confidence:     row.Confidence,
		Status:         "held",
		HoldReason:     reason,
	}
}

func fin014Proposed(row FIN014ReportRow, recordIDs []string, effect map[string]any, preds []FIN014Predicate) (FIN014Operation, error) {
	effectJSON, err := json.Marshal(effect)
	if err != nil {
		return FIN014Operation{}, err
	}
	return FIN014Operation{
		ID:             FIN014OperationID(row.Class, recordIDs, effectJSON),
		Class:          row.Class,
		RecordID:       row.RecordID,
		RecordKind:     row.RecordKind,
		StoreID:        row.StoreID,
		OrderID:        row.OrderID,
		Effect:         effect,
		ExpectedBefore: preds,
		Explanation:    row.Explanation,
		CashBasis:      row.CashBasis,
		Confidence:     row.Confidence,
		Status:         "proposed",
	}, nil
}

// fin014ProposeOperation maps one report row to its operation.
func fin014ProposeOperation(ctx context.Context, tx *sql.Tx, row FIN014ReportRow) (FIN014Operation, error) {
	switch row.Class {
	case FIN014ClassPayoutOversubscription:
		if row.RecordKind == "settlement_item" {
			// Q4: no canonical settlement writer — report-only.
			return fin014Held(row, "no canonical settlement writer (audit §8 Q4) — report-only, never auto-applied"), nil
		}
		var status string
		var amount int64
		if err := tx.QueryRowContext(ctx, `SELECT status, amount FROM payout_requests WHERE id=$1`, row.RecordID).Scan(&status, &amount); err != nil {
			return FIN014Operation{}, fmt.Errorf("payout request %s not found: %w", row.RecordID, err)
		}
		return fin014Proposed(row, []string{row.RecordID},
			map[string]any{"action": "reject_payout_request", "payout_request_id": row.RecordID, "amount": amount},
			[]FIN014Predicate{
				{Kind: "equals", Table: "payout_requests", ID: row.RecordID, Column: "status", Expected: status},
				{Kind: "equals", Table: "payout_requests", ID: row.RecordID, Column: "amount", Expected: amount},
			})
	case FIN014ClassPaidCancellation:
		var n int
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(status),'') FROM order_refund_obligations WHERE order_id=$1 AND purpose='cancellation'`, row.OrderID).Scan(&n, &status); err != nil {
			return FIN014Operation{}, err
		}
		if row.Confidence == FIN014ConfidenceUnresolved {
			return fin014Held(row, "cancellation obligation outcome is unknown — held for reviewer, never resolved by guessing"), nil
		}
		if n > 0 {
			return fin014Held(row, "duplicate cancellation obligations — creating another cannot repair it; held for reviewer"), nil
		}
		var paymentID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM payment_transactions WHERE order_id=$1 AND status='paid' ORDER BY id LIMIT 1`, row.OrderID).Scan(&paymentID); err != nil {
			return FIN014Operation{}, fmt.Errorf("paid cancellation %s has no paid payment to source the obligation from: %w", row.OrderID, err)
		}
		return fin014Proposed(row, []string{row.OrderID},
			map[string]any{"action": "create_cancellation_obligation", "order_id": row.OrderID, "source_payment_id": paymentID, "amount": row.AmountToman},
			[]FIN014Predicate{
				{Kind: "absent", Table: "order_refund_obligations", Column: "order_id", Value: row.OrderID, Match: map[string]string{"purpose": "cancellation"}},
				{Kind: "equals", Table: "orders", ID: row.OrderID, Column: "status", Expected: "cancelled"},
				{Kind: "equals", Table: "orders", ID: row.OrderID, Column: "payment_status", Expected: "paid"},
			})
	case FIN014ClassDuplicateParcelCharge:
		// The anomalous event is the first debit the detector flags
		// (same condition text as the D3 detector); its amount is the
		// reversal the repair posts.
		var eventID string
		var amount int64
		err := tx.QueryRowContext(ctx, `SELECT e.id, e.amount FROM supplier_return_financial_events e WHERE e.parcel_id=$1 AND e.operation='debit'
			AND ((SELECT COUNT(*) FROM supplier_return_financial_events e2 WHERE e2.parcel_id=e.parcel_id AND e2.operation='debit' AND e2.amount=e.amount)
			  - (SELECT COUNT(*) FROM supplier_return_financial_events e3 WHERE e3.parcel_id=e.parcel_id AND e3.operation='reversal' AND e3.amount=e.amount) > 1
			  OR (e.ledger_entry_id IS NULL AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.reason LIKE '%'||e.id||'%'))
			  OR (e.ledger_entry_id IS NOT NULL
				AND (SELECT COUNT(*) FROM supplier_return_financial_events e4 WHERE e4.ledger_entry_id=e.ledger_entry_id AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.reason LIKE '%'||e4.id||'%')) > 1
				AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.reason LIKE '%'||e.id||'%')))
			ORDER BY e.id LIMIT 1`, row.RecordID).Scan(&eventID, &amount)
		if err != nil {
			return FIN014Operation{}, fmt.Errorf("parcel %s has no anomalous debit event anymore: %w", row.RecordID, err)
		}
		return fin014Proposed(row, []string{row.RecordID, eventID},
			map[string]any{"action": "post_parcel_reversal", "parcel_id": row.RecordID, "event_id": eventID, "amount": amount},
			[]FIN014Predicate{
				{Kind: "equals", Table: "supplier_return_financial_events", ID: eventID, Column: "operation", Expected: "debit"},
				{Kind: "equals", Table: "supplier_return_financial_events", ID: eventID, Column: "amount", Expected: amount},
			})
	case FIN014ClassResolvedButHeldDispute:
		if row.Confidence != FIN014ConfidenceCertain {
			return fin014Held(row, "review_provenance_missing — no audited FIN-005 review row; held for reviewer"), nil
		}
		return fin014Proposed(row, []string{row.RecordID, row.OrderID},
			map[string]any{"action": "release_dispute_profit", "dispute_id": row.RecordID, "order_id": row.OrderID},
			[]FIN014Predicate{
				{Kind: "equals", Table: "disputes", ID: row.RecordID, Column: "status", Expected: "resolved"},
				{Kind: "equals", Table: "disputes", ID: row.RecordID, Column: "resolution_action", Expected: "release_to_seller"},
				{Kind: "equals", Table: "orders", ID: row.OrderID, Column: "profit_released", Expected: false},
			})
	case FIN014ClassStorageHistoryGap:
		return fin014Held(row, "merged history — charges cannot be re-derived from receipt facts; never guessed (storage lots NOT applicable to apply)"), nil
	case FIN014ClassCouponOversubscription:
		var usedCount int64
		var maxUses sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT used_count, max_uses FROM seller_discount_codes WHERE id=$1`, row.RecordID).Scan(&usedCount, &maxUses); err != nil {
			return FIN014Operation{}, fmt.Errorf("coupon %s not found: %w", row.RecordID, err)
		}
		var occupying int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_coupon_reservations WHERE coupon_id=$1 AND state IN ('reserved','consumed','review_hold')`, row.RecordID).Scan(&occupying); err != nil {
			return FIN014Operation{}, err
		}
		if maxUses.Valid && usedCount > maxUses.Int64 {
			// The cap sits below the admitted uses. Correcting the
			// count alone cannot clear it, and raising the cap would
			// ratify the oversubscription — a reviewer decides.
			return fin014Held(row, "coupon cap below admitted uses — raising the cap would ratify the oversubscription; held for reviewer"), nil
		}
		return fin014Proposed(row, []string{row.RecordID},
			map[string]any{"action": "correct_coupon_used_count", "coupon_id": row.RecordID, "used_count": occupying},
			[]FIN014Predicate{
				{Kind: "equals", Table: "seller_discount_codes", ID: row.RecordID, Column: "used_count", Expected: usedCount},
			})
	case FIN014ClassStatementMismatch:
		if row.RecordKind == "store_month" {
			return fin014Held(row, "statement opening unverified — held for reviewer (FIN-014 cutover record required)"), nil
		}
		if row.Confidence != FIN014ConfidenceCertain {
			return fin014Held(row, "released profit invisible to the statement builder — held for reviewer to confirm delivery"), nil
		}
		// The seller-profit reversal is legitimate only for a
		// succeeded refund event on a dispute/admin obligation.
		var obligationID string
		err := tx.QueryRowContext(ctx, `SELECT r.id FROM order_refund_obligations r WHERE r.order_id=$1 AND r.purpose IN ('dispute','admin_refund')
			AND COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=r.id ORDER BY e.event_no DESC LIMIT 1), r.status)='succeeded'
			ORDER BY r.id LIMIT 1`, row.OrderID).Scan(&obligationID)
		if err != nil {
			if err == sql.ErrNoRows {
				return fin014Held(row, "no succeeded refund event — a seller-profit reversal requires one; held for reviewer"), nil
			}
			return FIN014Operation{}, err
		}
		return fin014Proposed(row, []string{row.OrderID, obligationID},
			map[string]any{"action": "record_seller_profit_reversal", "order_id": row.OrderID, "obligation_id": obligationID},
			[]FIN014Predicate{
				{Kind: "equals", Table: "orders", ID: row.OrderID, Column: "profit_released", Expected: true},
				{Kind: "absent", Table: "seller_profit_reversals", Column: "order_id", Value: row.OrderID},
				{Kind: "latest_event_status", Table: "order_refund_obligations", ID: obligationID, Expected: "succeeded"},
			})
	case FIN014ClassUncertainPSPOutcome:
		return fin014Held(row, "lookup-only until FIN-010 lands (no PSP adapter — never simulate PSP proof)"), nil
	default:
		return FIN014Operation{}, fmt.Errorf("unknown defect class %q", row.Class)
	}
}
func FIN014OperationID(class string, recordIDs []string, effectJSON []byte) string {
	h := sha256.New()
	h.Write([]byte(class))
	for _, id := range recordIDs {
		h.Write([]byte{0})
		h.Write([]byte(id))
	}
	h.Write([]byte{0})
	h.Write(effectJSON)
	return "fin014-op-" + hex.EncodeToString(h.Sum(nil))[:32]
}
