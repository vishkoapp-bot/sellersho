package main

import (
	"context"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGranularFullPublication(t *testing.T) {
	db := supplierFlowDB(t)
	owner := supplierFlowSeed(t, db)
	repo := repository.NewSupplierRepository(db)
	ctx := context.Background()
	variants := []interface{}{}
	for i, color := range []string{"red", "blue", "green", "black", "white"} {
		variants = append(variants, map[string]interface{}{"variant_name": "color: " + color, "attributes": map[string]interface{}{"color": color}, "stock": float64(i + 1), "supplier_base_cost": float64(1000 + i*100)})
	}
	data := map[string]interface{}{"title": "Granular publication test", "productType": "variable", "categorySlug": "digital", "description": "Synthetic product for isolated review tests only", "supplierBaseCost": float64(1000), "stock": float64(15), "variants": variants, "imageUrls": []interface{}{"/static/test-a.png", "/static/test-b.png"}, "imageUrl": "/static/test-a.png", "weightGrams": float64(100), "lengthMm": float64(100), "widthMm": float64(100), "heightMm": float64(100)}
	id, e := repo.ProposeProduct(ctx, owner.supplier, data)
	if e != nil {
		t.Fatal(e)
	}
	jwt := auth.NewJWTService(strings.Repeat("r", 32))
	endpoint := RequireRoles(jwt, "admin")(func(w http.ResponseWriter, r *http.Request) { handleProductFieldReview(w, r, repo) })
	for _, role := range []string{"", "supplier", "super_admin"} {
		req := httptest.NewRequest("GET", "/api/v1/admin/product-field-reviews/"+id, nil)
		if role != "" {
			token, err := jwt.GenerateToken(owner.user, "09123456789", "Audit", role, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		result := httptest.NewRecorder()
		endpoint(result, req)
		want := 200
		if role == "" {
			want = 401
		} else if role == "supplier" {
			want = 403
		}
		if result.Code != want {
			t.Fatalf("review authorization %s: %d %s", role, result.Code, result.Body.String())
		}
	}
	if _, e = repo.ApproveProposal(ctx, id, ""); e == nil {
		t.Fatal("unreviewed publication")
	}
	decisions := []repository.FieldDecision{}
	for key, value := range repository.ReviewLeaves(data) {
		d := repository.FieldDecision{Key: key, Decision: "approved", ValueHash: repository.ReviewValueHash(value)}
		if key == "/variants/0/stock" || key == "/imageUrl" || key == "/variants/2/supplier_base_cost" || key == "/variants/1/attributes/color" {
			d.Decision = "rejected"
			d.Reason = "correct count"
		}
		decisions = append(decisions, d)
	}
	if e = repo.SaveFieldReview(ctx, id, "test-admin", decisions); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.ApproveProposal(ctx, id, ""); e == nil {
		t.Fatal("rejected publication")
	}
	if e = repo.ResubmitGranularReview(ctx, owner.supplier, id, map[string]interface{}{"/variants/0/stock": float64(7), "/imageUrl": "/static/corrected-primary.png", "/variants/2/supplier_base_cost": float64(3333), "/variants/1/attributes/color": "orange"}); e != nil {
		t.Fatal(e)
	}
	dossier, e := repo.GetAdminFieldReview(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	updated := dossier["proposal"].(map[string]interface{})
	decisions = nil
	for key, value := range repository.ReviewLeaves(updated) {
		decisions = append(decisions, repository.FieldDecision{Key: key, Decision: "approved", ValueHash: repository.ReviewValueHash(value)})
	}
	if e = repo.SaveFieldReview(ctx, id, "test-admin", decisions); e != nil {
		t.Fatal(e)
	}
	product, e := repo.ApproveProposal(ctx, id, "")
	if e != nil {
		t.Fatal(e)
	}
	var primary string
	if err := db.QueryRow(`SELECT image_url FROM products WHERE id=$1`, product).Scan(&primary); err != nil || primary != "/static/corrected-primary.png" {
		t.Fatal("primary correction lost", primary, err)
	}
	var count, defaults, total, images int
	if e = db.QueryRow(`SELECT COUNT(*),COUNT(*) FILTER(WHERE is_default),SUM(stock) FROM product_variants WHERE product_id=$1`, product).Scan(&count, &defaults, &total); e != nil {
		t.Fatal(e)
	}
	if count != 5 || defaults != 0 || total != 21 {
		t.Fatalf("SKUs %d default %d stock %d", count, defaults, total)
	}
	var minimum, maximum int
	if err := db.QueryRow(`SELECT MIN(supplier_base_cost),MAX(supplier_base_cost) FROM product_variants WHERE product_id=$1`, product).Scan(&minimum, &maximum); err != nil || minimum != 1000 || maximum != 3333 {
		t.Fatal("independent SKU prices lost", minimum, maximum, err)
	}
	var corrected int
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_variants WHERE product_id=$1 AND attributes->>'color'='orange' AND variant_name LIKE '%orange%'`, product).Scan(&corrected); err != nil || corrected != 1 {
		t.Fatal("attribute correction lost", corrected, err)
	}
	db.QueryRow(`SELECT COUNT(*) FROM product_images WHERE product_id=$1`, product).Scan(&images)
	if images != 2 {
		t.Fatal("gallery dropped", images)
	}
	if _, e = repo.ApproveProposal(ctx, id, ""); e == nil {
		t.Fatal("duplicate approval")
	}
	if repo.ResubmitGranularReview(ctx, owner.supplier, id, map[string]interface{}{"/variants/0/stock": float64(9)}) == nil {
		t.Fatal("published correction")
	}
}
