package repository

import (
	"context"
	"database/sql"
	"fmt"
)

type SupplierCampaign struct {
	ID          string `json:"id"`
	Name        string `json:"campaign_name"`
	Budget      int64  `json:"budget"`
	Bid         int64  `json:"cpc_bid"`
	Spent       int64  `json:"spent_budget"`
	Impressions int    `json:"impressions"`
	Clicks      int    `json:"clicks"`
	Status      string `json:"status"`
}

func (r *SupplierRepository) GetCampaigns(ctx context.Context, supplierID string) ([]SupplierCampaign, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, campaign_name, budget, cpc_bid, spent_budget, impressions, clicks, status FROM supplier_campaigns WHERE supplier_id = $1 ORDER BY created_at DESC`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SupplierCampaign{}
	for rows.Next() {
		var item SupplierCampaign
		if err := rows.Scan(&item.ID, &item.Name, &item.Budget, &item.Bid, &item.Spent, &item.Impressions, &item.Clicks, &item.Status); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *SupplierRepository) SetCampaignStatus(ctx context.Context, supplierID, id, status string) error {
	if status != "active" && status != "paused" {
		return fmt.Errorf("invalid campaign status")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE supplier_campaigns SET status = $1 WHERE id = $2 AND supplier_id = $3`, status, id, supplierID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type SupplierBalance struct {
	Earned        int64 `json:"earned"`
	Reserved      int64 `json:"reserved"`
	Paid          int64 `json:"paid"`
	ReturnCharges int64 `json:"returnCharges"`
	Available     int64 `json:"available"`
}

const supplierBalanceSQLLegacy = `SELECT
 COALESCE((SELECT SUM(oi.wholesale_cost * oi.quantity) FROM order_items oi JOIN products p ON p.id = oi.product_id JOIN orders o ON o.id = oi.order_id WHERE p.supplier_id = $1 AND o.payment_status = 'paid' AND o.status IN ('delivered','delivered_confirmed')), 0),
 COALESCE((SELECT SUM(amount) FROM payout_requests WHERE supplier_id = $1 AND status = 'pending'), 0),
 COALESCE((SELECT SUM(amount) FROM payout_requests WHERE supplier_id = $1 AND status IN ('approved','paid','processing','completed','transfer_confirmed')), 0)`

const supplierBalanceSQL = `SELECT
 COALESCE((SELECT SUM(oi.wholesale_cost * oi.quantity) FROM order_items oi JOIN products p ON p.id = oi.product_id JOIN orders o ON o.id = oi.order_id WHERE p.supplier_id = $1 AND o.payment_status = 'paid' AND o.status IN ('delivered','delivered_confirmed')), 0),
 COALESCE((SELECT SUM(amount) FROM payout_requests WHERE supplier_id = $1 AND status = 'pending'), 0),
 COALESCE((SELECT SUM(amount) FROM payout_requests WHERE supplier_id = $1 AND status IN ('approved','paid','processing','completed','transfer_confirmed')), 0),
 COALESCE((SELECT SUM(CASE WHEN operation = 'debit' THEN amount ELSE -amount END) FROM supplier_return_financial_events WHERE supplier_id = $1), 0)`

type queryRower interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func scanSupplierBalance(ctx context.Context, q queryRower, supplierID string, balance *SupplierBalance) error {
	var hasFinancialEvents bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass('public.supplier_return_financial_events') IS NOT NULL`).Scan(&hasFinancialEvents); err != nil {
		return err
	}
	if hasFinancialEvents {
		return q.QueryRowContext(ctx, supplierBalanceSQL, supplierID).Scan(&balance.Earned, &balance.Reserved, &balance.Paid, &balance.ReturnCharges)
	}
	return q.QueryRowContext(ctx, supplierBalanceSQLLegacy, supplierID).Scan(&balance.Earned, &balance.Reserved, &balance.Paid)
}

func (r *SupplierRepository) GetSettlementBalance(ctx context.Context, supplierID string) (*SupplierBalance, error) {
	var balance SupplierBalance
	if err := scanSupplierBalance(ctx, r.db, supplierID, &balance); err != nil {
		return nil, err
	}
	balance.Available = balance.Earned - balance.Reserved - balance.Paid - balance.ReturnCharges
	return &balance, nil
}
