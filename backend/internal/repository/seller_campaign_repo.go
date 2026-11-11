package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/drup/platform/internal/adguard"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var ErrCampaignForbidden = errors.New("campaign is not owned by the authenticated seller")
var ErrCampaignInput = errors.New("invalid seller campaign input")
var ErrCampaignInactive = errors.New("campaign is not active")
var ErrCampaignWidgetManaged = errors.New("campaign status is managed from the widgets section")

type SellerCampaign struct {
	ID            string             `json:"id"`
	StoreID       string             `json:"storeId"`
	Name          string             `json:"name"`
	Type          string             `json:"type"`
	Status        string             `json:"status"`
	ProductIDs    CampaignProductIDs `json:"productIds"`
	Configuration json.RawMessage    `json:"configuration"`
	StartsAt      *time.Time         `json:"startsAt,omitempty"`
	EndsAt        *time.Time         `json:"endsAt,omitempty"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

type CampaignProductIDs []string

func (ids *CampaignProductIDs) Scan(value interface{}) error {
	raw, ok := value.([]byte)
	if !ok {
		return ErrCampaignInput
	}
	return json.Unmarshal(raw, ids)
}

type SellerCampaignAnalytics struct {
	CampaignID  string  `json:"campaignId"`
	Impressions int     `json:"impressions"`
	Clicks      int     `json:"clicks"`
	CTR         float64 `json:"ctr"`
}

func validateCampaignInput(name, kind string, productIDs []string) error {
	if strings.TrimSpace(name) == "" || len(name) > 200 {
		return ErrCampaignInput
	}
	if kind != "widget" && kind != "banner" && kind != "link" {
		return ErrCampaignInput
	}
	if len(productIDs) > 100 {
		return ErrCampaignInput
	}
	seen := map[string]bool{}
	for _, id := range productIDs {
		if id == "" || seen[id] {
			return ErrCampaignInput
		}
		seen[id] = true
	}
	return nil
}

func (r *StoreRepository) listOwnedCampaigns(ctx context.Context, ownerID, slug string) (string, []SellerCampaign, error) {
	store, err := r.OwnedStore(ctx, ownerID, slug)
	if err != nil {
		return "", nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,store_id,name,campaign_type,status,product_ids,configuration,starts_at,ends_at,created_at,updated_at FROM seller_campaigns WHERE store_id=$1 ORDER BY created_at DESC`, store.ID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	result := []SellerCampaign{}
	for rows.Next() {
		var c SellerCampaign
		if err := rows.Scan(&c.ID, &c.StoreID, &c.Name, &c.Type, &c.Status, &c.ProductIDs, &c.Configuration, &c.StartsAt, &c.EndsAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return "", nil, err
		}
		result = append(result, c)
	}
	return store.ID, result, rows.Err()
}

func (r *StoreRepository) GetSellerCampaigns(ctx context.Context, ownerID, slug string) ([]SellerCampaign, error) {
	_, result, err := r.listOwnedCampaigns(ctx, ownerID, slug)
	return result, err
}

func (r *StoreRepository) CreateSellerCampaign(ctx context.Context, ownerID, slug, name, kind string, productIDs []string, config map[string]interface{}, status string) (*SellerCampaign, error) {
	store, err := r.OwnedStore(ctx, ownerID, slug)
	if err != nil {
		return nil, err
	}
	if err = validateCampaignInput(name, kind, productIDs); err != nil {
		return nil, err
	}
	if status != "draft" && status != "active" {
		return nil, ErrCampaignInput
	}
	if kind == "widget" {
		return nil, ErrCampaignInput
	} // Widgets use the transactional existing configuration endpoint.
	if len(config) > 0 {
		return nil, ErrCampaignInput
	} // No unimplemented advertising channels or client prices.
	if len(productIDs) > 0 {
		var count int
		if err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_products sp JOIN products p ON p.id=sp.product_id WHERE sp.store_id=$2 AND p.id=ANY($1::varchar[]) AND p.supplier_base_cost>0`, pq.Array(productIDs), store.ID).Scan(&count); err != nil {
			return nil, err
		}
		if count != len(productIDs) {
			return nil, ErrCampaignInput
		}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, ErrCampaignInput
	}
	id := uuid.New()
	var c SellerCampaign
	err = r.db.QueryRowContext(ctx, `INSERT INTO seller_campaigns(id,store_id,name,campaign_type,status,product_ids,configuration) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,store_id,name,campaign_type,status,product_ids,configuration,starts_at,ends_at,created_at,updated_at`, id, store.ID, name, kind, status, pqArray(productIDs), raw).Scan(&c.ID, &c.StoreID, &c.Name, &c.Type, &c.Status, &c.ProductIDs, &c.Configuration, &c.StartsAt, &c.EndsAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// pqArray is kept local to avoid exposing SQL details to handlers.
func pqArray(ids []string) interface{} {
	if ids == nil {
		ids = []string{}
	}
	raw, _ := json.Marshal(ids)
	return string(raw)
}

func (r *StoreRepository) SaveOwnedWidgetConfig(ctx context.Context, ownerID, slug string, config []byte) error {
	store, err := r.OwnedStore(ctx, ownerID, slug)
	if err != nil {
		return err
	}
	var parsed struct {
		Title      string    `json:"title"`
		ProductIDs []string  `json:"productIds"`
		EndsAt     time.Time `json:"endsAt"`
	}
	if json.Unmarshal(config, &parsed) != nil || validateCampaignInput(parsed.Title, "widget", parsed.ProductIDs) != nil {
		return ErrCampaignInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_products WHERE store_id=$2 AND product_id=ANY($1)`, pq.Array(parsed.ProductIDs), store.ID).Scan(&count); err != nil {
		return err
	}
	if count != len(parsed.ProductIDs) {
		return ErrCampaignInput
	}
	_, err = tx.ExecContext(ctx, `UPDATE stores SET seller_deals_widget_config=$1,updated_at=NOW() WHERE id=$2 AND owner_user_id=$3`, string(config), store.ID, ownerID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO seller_campaigns(store_id,name,campaign_type,status,product_ids,configuration,ends_at,updated_at) VALUES($1,$2,'widget','active',$3,$4,$5,NOW()) ON CONFLICT(store_id,campaign_type) WHERE campaign_type='widget' DO UPDATE SET name=EXCLUDED.name,status='active',product_ids=EXCLUDED.product_ids,configuration=EXCLUDED.configuration,ends_at=EXCLUDED.ends_at,updated_at=NOW()`, store.ID, parsed.Title, pqArray(parsed.ProductIDs), string(config), parsed.EndsAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *StoreRepository) SetSellerCampaignStatus(ctx context.Context, ownerID, slug, campaignID, status string) error {
	if _, err := uuid.Parse(campaignID); err != nil {
		return ErrCampaignInput
	}
	if status != "draft" && status != "active" && status != "paused" && status != "archived" {
		return ErrCampaignInput
	}
	store, err := r.OwnedStore(ctx, ownerID, slug)
	if err != nil {
		return err
	}
	// F2: a 'widget' campaign row is only a MIRROR of
	// stores.seller_deals_widget_config (SaveOwnedWidgetConfig
	// upserts it on every widget save); pricing
	// (loadDealsWidgetConfig) and the storefront sections read
	// the config, never this row's status. Changing the status
	// here would hide the public campaign listing while prices
	// stay discounted, so refuse: the deal is managed from the
	// deals-widget section itself.
	var campaignType string
	if err := r.db.QueryRowContext(ctx, `SELECT campaign_type FROM seller_campaigns WHERE id=$1 AND store_id=$2`, campaignID, store.ID).Scan(&campaignType); err != nil {
		if err == sql.ErrNoRows {
			return ErrCampaignForbidden
		}
		return err
	}
	if campaignType == "widget" {
		return ErrCampaignWidgetManaged
	}
	res, err := r.db.ExecContext(ctx, `UPDATE seller_campaigns SET status=$1,updated_at=NOW() WHERE id=$2 AND store_id=$3 AND status<>'archived'`, status, campaignID, store.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCampaignForbidden
	}
	return nil
}

func (r *StoreRepository) RecordSellerCampaignEvent(ctx context.Context, campaignID, eventType, token, productID string, evidence ...adguard.Evidence) (bool, error) {
	if _, err := uuid.Parse(campaignID); err != nil {
		return false, ErrCampaignInput
	}
	if strings.TrimSpace(token) == "" || len(token) > 200 {
		return false, ErrCampaignInput
	}
	if eventType != "impression" && eventType != "click" {
		return false, ErrCampaignInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var storeID, status string
	var starts, ends *time.Time
	var ids CampaignProductIDs
	if err := tx.QueryRowContext(ctx, `SELECT c.store_id,c.status,c.starts_at,c.ends_at,c.product_ids FROM seller_campaigns c JOIN stores s ON s.id=c.store_id WHERE c.id=$1 AND s.status='active' FOR SHARE OF c`, campaignID).Scan(&storeID, &status, &starts, &ends, &ids); err != nil {
		if err == sql.ErrNoRows {
			return false, ErrCampaignForbidden
		}
		return false, err
	}
	now := time.Now()
	if status != "active" || (starts != nil && now.Before(*starts)) || (ends != nil && now.After(*ends)) {
		return false, ErrCampaignInactive
	}
	if productID != "" {
		found := false
		for _, id := range ids {
			if id == productID {
				found = true
			}
		}
		if !found {
			return false, ErrCampaignInput
		}
	}
	if len(evidence) > 0 {
		for _, key := range []string{"ip:" + evidence[0].IPHash, "fp:" + evidence[0].FingerprintHash} {
			res, e := tx.ExecContext(ctx, `INSERT INTO ad_fraud_keys(campaign_id,event_type,identity_key,expires_at) VALUES($1,$2,$3,NOW()+INTERVAL '24 hours') ON CONFLICT(campaign_id,event_type,identity_key) DO UPDATE SET expires_at=EXCLUDED.expires_at WHERE ad_fraud_keys.expires_at<=NOW()`, campaignID, eventType, key)
			if e != nil {
				return false, e
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return false, nil
			}
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO seller_campaign_events(campaign_id,store_id,event_type,ad_token,product_id) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,'')) ON CONFLICT DO NOTHING`, campaignID, storeID, eventType, token, productID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return n == 1, nil
}

func (r *StoreRepository) RejectAdEvent(ctx context.Context, campaign, event, token, reason, ipHash string) error {
	if len(token) > 12 {
		token = token[:12]
	}
	_, e := r.db.ExecContext(ctx, `INSERT INTO ad_event_rejections(campaign_id,event_type,token_prefix,reason,ip_address) VALUES($1,$2,$3,$4,$5)`, campaign, event, token, reason, ipHash)
	return e
}

func (r *StoreRepository) SellerCampaignAnalytics(ctx context.Context, ownerID, slug, campaignID string) (*SellerCampaignAnalytics, error) {
	if _, err := uuid.Parse(campaignID); err != nil {
		return nil, ErrCampaignInput
	}
	store, err := r.OwnedStore(ctx, ownerID, slug)
	if err != nil {
		return nil, err
	}
	var owned bool
	if err = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM seller_campaigns WHERE id=$1 AND store_id=$2)`, campaignID, store.ID).Scan(&owned); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrCampaignForbidden
	}
	var a SellerCampaignAnalytics
	err = r.db.QueryRowContext(ctx, `SELECT campaign_id,COUNT(*) FILTER(WHERE event_type='impression'),COUNT(*) FILTER(WHERE event_type='click') FROM seller_campaign_events WHERE campaign_id=$1 AND store_id=$2 GROUP BY campaign_id`, campaignID, store.ID).Scan(&a.CampaignID, &a.Impressions, &a.Clicks)
	if err == sql.ErrNoRows {
		return &SellerCampaignAnalytics{CampaignID: campaignID}, nil
	}
	if err != nil {
		return nil, err
	}
	if a.Impressions > 0 {
		a.CTR = float64(a.Clicks) / float64(a.Impressions) * 100
	}
	return &a, nil
}

func (r *StoreRepository) PublicSellerCampaigns(ctx context.Context, slug string) ([]SellerCampaign, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT c.id,c.store_id,c.name,c.campaign_type,c.status,c.product_ids,c.configuration,c.starts_at,c.ends_at,c.created_at,c.updated_at FROM seller_campaigns c JOIN stores s ON s.id=c.store_id WHERE s.slug=$1 AND s.status='active' AND c.status='active' AND (c.starts_at IS NULL OR c.starts_at<=NOW()) AND (c.ends_at IS NULL OR c.ends_at>NOW()) ORDER BY c.created_at DESC`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []SellerCampaign{}
	for rows.Next() {
		var c SellerCampaign
		if err = rows.Scan(&c.ID, &c.StoreID, &c.Name, &c.Type, &c.Status, &c.ProductIDs, &c.Configuration, &c.StartsAt, &c.EndsAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}
