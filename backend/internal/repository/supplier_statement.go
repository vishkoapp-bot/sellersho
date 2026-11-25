package repository

import (
	"context"
	"database/sql"
	"time"
)

type SupplierStatementEntry struct {
	ID        string     `json:"id"`
	Date      *time.Time `json:"date"`
	Amount    int64      `json:"amount"`
	Kind      string     `json:"kind"`
	Reference string     `json:"reference"`
	Status    string     `json:"status,omitempty"`
}

type SupplierStatement struct {
	Month        string                   `json:"month"`
	Balance      SupplierBalance          `json:"balance"`
	Credits      int64                    `json:"credits"`
	Debits       int64                    `json:"debits"`
	TotalEntries int                      `json:"totalEntries"`
	UndatedCount int                      `json:"undatedCount"`
	Entries      []SupplierStatementEntry `json:"entries"`
	NextCursor   string                   `json:"nextCursor,omitempty"`
}

// Each signed row describes one of the canonical payable balance terms. A
// pending payout represents a reservation, and does not create a second row
// when it later moves into the Paid term.
const supplierStatementSQL = `
	SELECT o.id::text id, coalesce(o.confirmed_at, (SELECT max(ss.delivered_at) FROM supplier_shipments ss WHERE ss.order_id=o.id AND ss.supplier_id=$1)) occurred,
		sum(oi.wholesale_cost::numeric*oi.quantity) amount, 'order' kind, o.order_number::text ref, o.status::text status
	FROM order_items oi JOIN orders o ON o.id=oi.order_id
	WHERE ` + supplierOriginAuthority + `=$1 AND o.payment_status='paid' AND o.status IN ('delivered','delivered_confirmed') GROUP BY o.id
	UNION ALL SELECT id::text,created_at,-amount,'payout',id::text,status::text FROM payout_requests WHERE supplier_id=$1 AND status IN ('pending','approved','paid','processing','completed','transfer_confirmed')
	UNION ALL SELECT id,created_at,CASE WHEN operation='debit' THEN -amount ELSE amount END,'return_event',return_request_id,'' FROM supplier_return_financial_events WHERE supplier_id=$1
	UNION ALL SELECT id,created_at,-amount,'return_shipping',return_id::text,'' FROM customer_return_shipping_charges WHERE supplier_id=$1 AND bearer='supplier'
	UNION ALL SELECT e.id,e.created_at,CASE WHEN e.operation='debit' THEN -e.amount ELSE e.amount END,'return_clawback',e.return_id::text,'' FROM customer_return_financial_events e JOIN orders o ON o.id=e.order_id WHERE e.supplier_id=$1 AND e.component='product_cost' AND o.status<>'cancelled'
	UNION ALL SELECT id::text,date,CASE WHEN type='credit' THEN amount ELSE -amount END,'adjustment',id::text,'' FROM ledger_entries WHERE target_account='supplier_payable:' || $1 AND target_type='supplier' AND reason LIKE 'سند تعدیل%'
	UNION ALL SELECT id,created_at,-amount_toman,'storage',fee_date::text,'' FROM warehouse_storage_fee_charges WHERE supplier_id=$1`

func (r *SupplierRepository) GetSupplierStatement(ctx context.Context, supplierID, month string, opts StatementPageOptions) (*SupplierStatement, error) {
	start, end, err := statementMonthWindow(month)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &SupplierStatement{Month: month, Entries: []SupplierStatementEntry{}}
	if err := scanSupplierBalance(ctx, tx, supplierID, &out.Balance); err != nil {
		return nil, err
	}
	period := `occurred >= $2 AND occurred < $3`
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER(WHERE `+period+`), COUNT(*) FILTER(WHERE occurred IS NULL),
		COALESCE(SUM(amount) FILTER(WHERE amount > 0 AND `+period+`),0), COALESCE(SUM(-amount) FILTER(WHERE amount < 0 AND `+period+`),0)
		FROM (`+supplierStatementSQL+`) e WHERE amount<>0`, supplierID, start, end).
		Scan(&out.TotalEntries, &out.UndatedCount, &out.Credits, &out.Debits); err != nil {
		return nil, err
	}
	limit := statementPageLimit(opts.Limit)
	args := []any{supplierID, start, end, limit + 1}
	where := period
	if opts.Cursor != "" {
		at, id, kind, err := decodeStatementCursor(opts.Cursor)
		if err != nil {
			return nil, err
		}
		where += ` AND (occurred < $5 OR (occurred = $5 AND (id, kind) > ($6, $7)))`
		args = append(args, at, id, kind)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, occurred, amount, kind, ref, status FROM (`+supplierStatementSQL+`) e WHERE amount<>0 AND `+where+` ORDER BY occurred DESC,id,kind LIMIT $4`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry SupplierStatementEntry
		if err := rows.Scan(&entry.ID, &entry.Date, &entry.Amount, &entry.Kind, &entry.Reference, &entry.Status); err != nil {
			return nil, err
		}
		if len(out.Entries) == limit {
			last := out.Entries[limit-1]
			out.NextCursor = encodeStatementCursor(*last.Date, last.ID, last.Kind)
			break
		}
		out.Entries = append(out.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
