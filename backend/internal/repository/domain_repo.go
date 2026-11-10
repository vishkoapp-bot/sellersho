package repository

import (
	"context"
	"encoding/json"
	"github.com/drup/platform/internal/customdomain"
	"github.com/google/uuid"
	"time"
)

type StoreDomain struct {
	Domain     string     `json:"domain"`
	StoreID    string     `json:"storeId"`
	Token      string     `json:"verificationToken,omitempty"`
	Status     string     `json:"status"`
	Approved   bool       `json:"approved"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
}

func (r *StoreRepository) GetOwnedDomains(ctx context.Context, userID string) ([]StoreDomain, error) {
	rows, e := r.db.QueryContext(ctx, `SELECT d.domain,d.store_id,d.verification_token,d.status,d.verified_at,COALESCE((SELECT p.status='approved' FROM pending_approvals p WHERE p.target_id=d.store_id AND p.entity_type='custom_domain' AND p.requested_data->>'value'=d.domain ORDER BY p.created_at DESC,p.id DESC LIMIT 1),FALSE) FROM store_domains d JOIN stores s ON s.id=d.store_id WHERE s.owner_user_id=$1 ORDER BY d.created_at DESC`, userID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []StoreDomain{}
	for rows.Next() {
		var d StoreDomain
		if e = rows.Scan(&d.Domain, &d.StoreID, &d.Token, &d.Status, &d.VerifiedAt, &d.Approved); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (r *StoreRepository) RequestDomain(ctx context.Context, userID, slug, raw string) (*StoreDomain, error) {
	domain, e := customdomain.Normalize(raw)
	if e != nil {
		return nil, e
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var id string
	var limit int
	if e = tx.QueryRowContext(ctx, `SELECT s.id,COALESCE((SELECT allowed_custom_domains FROM subscription_plans p WHERE p.id=s.active_plan_id),0) FROM stores s WHERE s.slug=$1 AND s.owner_user_id=$2 AND s.owner_type='dropshipper' FOR UPDATE`, slug, userID).Scan(&id, &limit); e != nil {
		return nil, ErrStoreForbidden
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_domains WHERE store_id=$1 AND status<>'disabled'`, id).Scan(&count); e != nil {
		return nil, e
	}
	if count >= limit {
		return nil, ErrStoreInput
	}
	d := StoreDomain{Domain: domain, StoreID: id, Token: uuid.NewString(), Status: "pending_dns"}
	res, e := tx.ExecContext(ctx, `INSERT INTO store_domains(domain,store_id,verification_token) VALUES($1,$2,$3) ON CONFLICT(domain) DO UPDATE SET verification_token=EXCLUDED.verification_token,status='pending_dns',verified_at=NULL,created_at=NOW() WHERE store_domains.store_id=EXCLUDED.store_id AND store_domains.status='disabled'`, domain, id, d.Token)
	if e != nil {
		return nil, e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrStoreInput
	}
	data, _ := json.Marshal(map[string]string{"value": domain, "storeId": id})
	if _, e = tx.ExecContext(ctx, `INSERT INTO pending_approvals(id,entity_type,target_id,title,requested_data,requested_by,status) VALUES($1,'custom_domain',$2,$3,$4,$5,'pending')`, "req-"+uuid.NewString(), id, "اتصال دامنه: "+domain, string(data), userID); e != nil {
		return nil, e
	}
	return &d, tx.Commit()
}
func (r *StoreRepository) VerifyOwnedDomain(ctx context.Context, userID, domain, edge string, resolver customdomain.Resolver) error {
	domains, e := r.GetOwnedDomains(ctx, userID)
	if e != nil {
		return e
	}
	var selected *StoreDomain
	for _, d := range domains {
		if d.Domain == domain && d.Status != "disabled" {
			copy := d
			selected = &copy
		}
	}
	if selected == nil {
		return ErrStoreForbidden
	}
	if !selected.Approved {
		return ErrStoreInput
	}
	if e = customdomain.Verify(ctx, resolver, domain, selected.Token, edge); e != nil {
		return e
	}
	res, e := r.db.ExecContext(ctx, `UPDATE store_domains d SET status='active',verified_at=NOW() FROM stores s WHERE d.domain=$1 AND s.id=d.store_id AND s.owner_user_id=$2 AND s.status='active' AND d.verification_token=$3 AND d.status<>'disabled'`, domain, userID, selected.Token)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrStoreForbidden
	}
	return nil
}
func (r *StoreRepository) DisableOwnedDomain(ctx context.Context, userID, domain string) error {
	res, e := r.db.ExecContext(ctx, `UPDATE store_domains d SET status='disabled',verified_at=NULL,verification_token=$3 FROM stores s WHERE d.domain=$1 AND s.id=d.store_id AND s.owner_user_id=$2`, domain, userID, uuid.NewString())
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrStoreForbidden
	}
	return nil
}
func (r *StoreRepository) AllowedDomain(ctx context.Context, domain string) (string, error) {
	var slug string
	e := r.db.QueryRowContext(ctx, `SELECT s.slug FROM store_domains d JOIN stores s ON s.id=d.store_id WHERE d.domain=$1 AND d.status='active' AND s.status='active' AND COALESCE((SELECT p.status='approved' FROM pending_approvals p WHERE p.target_id=s.id AND p.entity_type='custom_domain' AND p.requested_data->>'value'=d.domain ORDER BY p.created_at DESC,p.id DESC LIMIT 1),FALSE)`, domain).Scan(&slug)
	return slug, e
}
