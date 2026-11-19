package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func (r *StoreRepository) UpdateSellerCampaign(ctx context.Context, owner, slug, id, name string, ids []string, start, end *time.Time) error {
	return r.patchSellerCampaign(ctx, owner, slug, id, &campaignMetadata{name, ids, start, end}, "")
}

type campaignMetadata struct {
	name       string
	ids        []string
	start, end *time.Time
}

// PatchSellerCampaign applies all requested changes and their history together.
func (r *StoreRepository) PatchSellerCampaign(ctx context.Context, owner, slug, id, name string, ids []string, status string) error {
	var metadata *campaignMetadata
	if name != "" || ids != nil {
		metadata = &campaignMetadata{name: name, ids: ids}
	}
	return r.patchSellerCampaign(ctx, owner, slug, id, metadata, status)
}

func (r *StoreRepository) patchSellerCampaign(ctx context.Context, owner, slug, id string, metadata *campaignMetadata, status string) error {
	if metadata == nil && status == "" {
		return nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return ErrCampaignInput
	}
	if metadata == nil && !validSellerCampaignStatus(status) {
		return ErrCampaignInput
	}
	store, err := r.OwnedStore(ctx, owner, slug)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if metadata != nil {
		if err := updateSellerCampaignMetadata(ctx, tx, owner, store.ID, id, *metadata); err != nil {
			return err
		}
	}
	if status != "" {
		if err := setSellerCampaignStatus(ctx, tx, store.ID, id, status, metadata != nil); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func updateSellerCampaignMetadata(ctx context.Context, tx *sql.Tx, owner, storeID, id string, metadata campaignMetadata) error {
	name, ids, start, end := metadata.name, metadata.ids, metadata.start, metadata.end
	if validateCampaignInput(name, "banner", ids) != nil || (start != nil && end != nil && !end.After(*start)) {
		return ErrCampaignInput
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_products WHERE store_id=$1 AND product_id=ANY($2)`, storeID, pq.Array(ids)).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return ErrCampaignInput
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.campaign_actor',$1,true)`, owner); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE seller_campaigns SET name=$1,product_ids=$2,starts_at=$3,ends_at=$4,updated_at=NOW() WHERE id=$5 AND store_id=$6 AND campaign_type<>'widget' AND status<>'archived'`, name, pqArray(ids), start, end, id, storeID)
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
	return nil
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
