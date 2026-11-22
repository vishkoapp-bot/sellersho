package repository

import (
	"context"
	"time"
)

// The interim queue lists unpaid orders. An offline payment claim has no
// persisted membership criterion yet.
const pendingManualConfirmScope = `payment_status='pending' AND status<>'cancelled'`

type AdminPendingPayment struct {
	ID            string    `json:"id"`
	OrderNumber   string    `json:"orderNumber"`
	CustomerName  string    `json:"customerName"`
	AmountToman   int64     `json:"amountToman"`
	PaymentStatus string    `json:"paymentStatus"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	AgeHours      int64     `json:"ageHours"`
}

func (r *OrderRepository) ListAdminPendingPayments(ctx context.Context, opts ListPageOptions) (ListPage[AdminPendingPayment], error) {
	page := ListPage[AdminPendingPayment]{Items: []AdminPendingPayment{}, Limit: NormalizeListLimit(opts.Limit)}
	query := `SELECT id, order_number, customer_full_name, payable_amount, payment_status, status, created_at
		FROM orders WHERE ` + pendingManualConfirmScope
	args := []any{page.Limit + 1}
	if opts.Cursor != "" {
		at, id, err := DecodeCreatedAtCursor(opts.Cursor)
		if err != nil {
			return page, err
		}
		query += ` AND (created_at, id) > ($2, $3)`
		args = append(args, at, id)
	}
	query += ` ORDER BY created_at ASC, id ASC LIMIT $1`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var row AdminPendingPayment
		if err := rows.Scan(&row.ID, &row.OrderNumber, &row.CustomerName, &row.AmountToman, &row.PaymentStatus, &row.Status, &row.CreatedAt); err != nil {
			return page, err
		}
		row.AgeHours = max(0, int64(now.Sub(row.CreatedAt)/time.Hour))
		page.Items = append(page.Items, row)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > page.Limit {
		page.Items = page.Items[:page.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = EncodeCreatedAtCursor(last.CreatedAt, last.ID)
		page.HasMore = true
	}
	return page, nil
}
