package repository

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"
)

func TestGranularReviewDatabaseLifecycle(t *testing.T) {
	dsn := os.Getenv("GRANULAR_REVIEW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated granular_review_test database required")
	}
	db, e := testdb.Open(t, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`CREATE TEMP TABLE pending_approvals(id TEXT PRIMARY KEY,entity_type TEXT,target_id TEXT,title TEXT,requested_data JSONB,requested_by TEXT,status TEXT,rejection_reason TEXT,created_at TIMESTAMPTZ DEFAULT NOW(),reviewed_at TIMESTAMPTZ);
 CREATE TEMP TABLE proposal_field_reviews(proposal_id TEXT,field_key TEXT,decision TEXT,reason TEXT,reviewer_id TEXT,value_hash TEXT,reviewed_at TIMESTAMPTZ,UNIQUE(proposal_id,field_key));
 CREATE TEMP TABLE proposal_review_history(proposal_id TEXT,action TEXT,field_key TEXT,reason TEXT,actor_id TEXT,actor_role TEXT,created_at TIMESTAMPTZ DEFAULT NOW());`)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	r := NewSupplierRepository(db)
	data := map[string]interface{}{"title": "shirt", "categorySlug": "digital", "productType": "variable", "supplierBaseCost": float64(200), "stock": float64(3), "variants": []interface{}{map[string]interface{}{"variant_name": "color: blue", "stock": float64(3), "supplier_base_cost": float64(200), "attributes": map[string]interface{}{"color": "blue"}}}}
	raw, _ := json.Marshal(data)
	_, e = db.Exec(`INSERT INTO pending_approvals(id,entity_type,target_id,title,requested_data,requested_by,status) VALUES('review-test','proposed_product','review-test','shirt',$1,'owner','pending')`, string(raw))
	if e != nil {
		t.Fatal(e)
	}
	decisions := []FieldDecision{}
	for key, value := range ReviewLeaves(data) {
		decisions = append(decisions, FieldDecision{Key: key, Decision: "approved", ValueHash: ReviewValueHash(value)})
	}
	for i := range decisions {
		if decisions[i].Key == "/variants/0/stock" {
			decisions[i].Decision = "rejected"
			decisions[i].Reason = "correct count"
		}
	}
	bad := append([]FieldDecision{}, decisions...)
	bad[0].ValueHash = "stale"
	if r.SaveFieldReview(ctx, "review-test", "admin", bad) == nil {
		t.Fatal("stale save accepted")
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM proposal_field_reviews`).Scan(&count)
	if count != 0 {
		t.Fatal("partial decision write")
	}
	if e = r.SaveFieldReview(ctx, "review-test", "admin", decisions); e != nil {
		t.Fatal(e)
	}
	var reviewedAt, beforeReplay time.Time
	if err := db.QueryRow(`SELECT reviewed_at FROM proposal_field_reviews WHERE proposal_id='review-test' AND field_key='/title'`).Scan(&reviewedAt); err != nil {
		t.Fatal(err)
	}
	if e = r.SaveFieldReview(ctx, "review-test", "different-admin", decisions); e != nil {
		t.Fatal(e)
	}
	if err := db.QueryRow(`SELECT reviewed_at FROM proposal_field_reviews WHERE proposal_id='review-test' AND field_key='/title'`).Scan(&beforeReplay); err != nil || !beforeReplay.Equal(reviewedAt) {
		t.Fatal("unchanged approval rewritten by repeated save", err)
	}
	db.Exec(`INSERT INTO proposal_field_reviews(proposal_id,field_key,decision,reason) VALUES('review-test','price_c','rejected','old reason')`)
	if r.ResubmitCatalogReview(ctx, "owner", "review-test", map[string]interface{}{"supplierBaseCost": float64(999)}) == nil {
		t.Fatal("legacy path bypassed granular review")
	}

	if _, e = r.ApproveProposal(ctx, "review-test", ""); e == nil || !strings.Contains(e.Error(), "field requires approval") {
		t.Fatal("publication gate", e)
	}
	if e = r.ResubmitGranularReview(ctx, "intruder", "review-test", map[string]interface{}{"/variants/0/stock": float64(7)}); e == nil {
		t.Fatal("cross-supplier correction")
	}
	if e = r.ResubmitGranularReview(ctx, "owner", "review-test", map[string]interface{}{"/title": "hacked"}); e == nil {
		t.Fatal("approved field correction")
	}
	if e = r.SaveFieldReview(ctx, "review-test", "admin", []FieldDecision{{Key: "/categorySlug", Decision: "rejected", Reason: "correct category", ValueHash: ReviewValueHash("digital")}}); e != nil {
		t.Fatal(e)
	}
	if e = r.ResubmitGranularReview(ctx, "owner", "review-test", map[string]interface{}{"/categorySlug": "fashion"}); e != nil {
		t.Fatal(e)
	}
	var titleDecision, stockDecision string
	db.QueryRow(`SELECT decision FROM proposal_field_reviews WHERE proposal_id='review-test' AND field_key='/title'`).Scan(&titleDecision)
	db.QueryRow(`SELECT decision FROM proposal_field_reviews WHERE proposal_id='review-test' AND field_key='/variants/0/stock'`).Scan(&stockDecision)
	if titleDecision != "approved" || stockDecision != "rejected" {
		t.Fatal("unrelated decisions were reset", titleDecision, stockDecision)
	}
	if e = r.ResubmitGranularReview(ctx, "owner", "review-test", map[string]interface{}{"/variants/0/stock": float64(7)}); e != nil {
		t.Fatal(e)
	}
	dossier, e := r.GetAdminFieldReview(ctx, "review-test")
	if e != nil {
		t.Fatal(e)
	}
	proposal := dossier["proposal"].(map[string]interface{})
	if ReviewLeaves(proposal)["/variants/0/stock"] != float64(7) {
		t.Fatal("stock not saved")
	}
	if e = r.SaveFieldReview(ctx, "review-test", "admin", decisions); e == nil {
		t.Fatal("old decisions replay accepted")
	}
	updated := []FieldDecision{}
	for key, value := range ReviewLeaves(proposal) {
		updated = append(updated, FieldDecision{Key: key, Decision: "approved", ValueHash: ReviewValueHash(value)})
	}
	if e = r.SaveFieldReview(ctx, "review-test", "admin", updated); e != nil {
		t.Fatal(e)
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = requireReviewed(ctx, tx, "review-test", proposal); e != nil {
		t.Fatal(e)
	}
	tx.Rollback()
	db.Exec(`UPDATE pending_approvals SET status='approved' WHERE id='review-test'`)
	if r.ResubmitGranularReview(ctx, "owner", "review-test", map[string]interface{}{"/variants/0/stock": float64(9)}) == nil {
		t.Fatal("published edit")
	}
}
