package repository

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"time"
)

func (r *StoreRepository) UpdateSellerCampaign(ctx context.Context, owner, slug, id, name string, ids []string, start, end *time.Time) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrCampaignInput
	}
	store, err := r.OwnedStore(ctx, owner, slug)
	if err != nil {
		return err
	}
	if validateCampaignInput(name, "banner", ids) != nil || (start != nil && end != nil && !end.After(*start)) {
		return ErrCampaignInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_products WHERE store_id=$1 AND product_id=ANY($2)`, store.ID, pq.Array(ids)).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return ErrCampaignInput
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.campaign_actor',$1,true)`, owner); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE seller_campaigns SET name=$1,product_ids=$2,starts_at=$3,ends_at=$4,updated_at=NOW() WHERE id=$5 AND store_id=$6 AND campaign_type<>'widget' AND status<>'archived'`, name, pqArray(ids), start, end, id, store.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCampaignForbidden
	}
	return tx.Commit()
}

type SellerCampaignHistory struct {
	Action    string          `json:"action"`
	Snapshot  json.RawMessage `json:"snapshot"`
	CreatedAt time.Time       `json:"createdAt"`
}

func (r *StoreRepository) SellerCampaignHistory(ctx context.Context, owner, slug, id string) ([]SellerCampaignHistory, error) {
	if _, err := r.SellerCampaignAnalytics(ctx, owner, slug, id); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT action,snapshot,created_at FROM seller_campaign_history WHERE campaign_id=$1 ORDER BY created_at DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SellerCampaignHistory{}
	for rows.Next() {
		var item SellerCampaignHistory
		if err = rows.Scan(&item.Action, &item.Snapshot, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
