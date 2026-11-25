package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

// Use the same migrated, disposable checkout fixture as the FIN007 HTTP test.
// Each test owns a different store/product from the compact capacity seed.
func variantEffectiveMarginFixture(t *testing.T) *gap13Repos {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SUPPLIER_FLOW_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile("../capacity-fixture/seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	compact := strings.NewReplacer(
		"generate_series(1,1000)", "generate_series(1,1)",
		"generate_series(1,100)", "generate_series(1,3)",
		"generate_series(1,10000)", "generate_series(1,2)",
		"generate_series(1,50000)", "generate_series(1,10)",
		"generate_series(1,100000)", "generate_series(1,0)",
	).Replace(string(seed))
	// The suspension fixture can leave a text primary key and synthetic IDs
	// such as user-owner-1. Preserve UUIDs and map those legacy IDs to UUIDs.
	if _, err := db.Exec(`DO $$
	BEGIN
		IF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'users'::regclass
			AND attname = 'id' AND atttypid <> 'uuid'::regtype) THEN
			ALTER TABLE users ALTER COLUMN id DROP DEFAULT;
			ALTER TABLE users ALTER COLUMN id TYPE uuid USING CASE
				WHEN id ~* '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' THEN id::uuid
				ELSE md5(id)::uuid
			END;
			ALTER TABLE users ALTER COLUMN id SET DEFAULT gen_random_uuid();
		END IF;
	END $$;`); err != nil {
		t.Fatalf("restore capacity fixture users.id: %v", err)
	}
	if _, err := db.Exec(compact); err != nil {
		t.Fatal(err)
	}
	return &gap13Repos{
		db: db, orderRepo: repository.NewOrderRepository(db), catalogRepo: repository.NewCatalogRepository(db),
		storeRepo: repository.NewStoreRepository(db), shipmentRepo: repository.NewSupplierShipmentRepository(db),
		pricingRepo: repository.NewPricingRepository(db), supplierRepo: repository.NewSupplierRepository(db),
	}
}

func capacityFixtureUUID(key string) string {
	return uuid.UUID(md5.Sum([]byte(key))).String()
}

type effectiveMarginProjection struct {
	ProductID     string  `json:"productId"`
	VariantID     string  `json:"variantId"`
	MarginPct     float64 `json:"marginPct"`
	CeilingPct    float64 `json:"ceilingPct"`
	Tier          string  `json:"tier"`
	Locked        bool    `json:"locked"`
	ReferenceCost int64   `json:"referenceCost"`
	FinalPrice    int64   `json:"finalPrice"`
}

func requestEffectiveMargins(t *testing.T, rp *gap13Repos, storeID, userID, role string) *httptest.ResponseRecorder {
	t.Helper()
	jwt := auth.NewJWTService(strings.Repeat("s", 32))
	token, err := jwt.GenerateToken(userID, "09800000003", "Synthetic seller", role, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/seller/variant-effective-margins?storeId="+storeID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	out := httptest.NewRecorder()
	RequireRoles(jwt, "seller")(func(w http.ResponseWriter, r *http.Request) {
		handleSellerVariantEffectiveMargins(w, r, rp.pricingRepo, rp.storeRepo, rp.catalogRepo)
	})(out, req)
	return out
}

func readEffectiveMargins(t *testing.T, rp *gap13Repos, storeID, ownerID string) map[string]effectiveMarginProjection {
	t.Helper()
	out := requestEffectiveMargins(t, rp, storeID, ownerID, "seller")
	if out.Code != http.StatusOK {
		t.Fatalf("effective margins: status %d, body %s", out.Code, out.Body.String())
	}
	var page struct {
		Items []effectiveMarginProjection `json:"items"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	margins := make(map[string]effectiveMarginProjection, len(page.Items))
	for _, item := range page.Items {
		if _, duplicate := margins[item.VariantID]; duplicate {
			t.Fatalf("duplicate variant %q in response", item.VariantID)
		}
		margins[item.VariantID] = item
	}
	return margins
}

func TestSellerVariantEffectiveMargins(t *testing.T) {
	rp := variantEffectiveMarginFixture(t)
	const store, product = "cap-store-3", "cap-product-2"
	owner := capacityFixtureUUID("cap-seller-3")
	unset := capacityFixtureUUID("cap-sku-6")
	overridden := capacityFixtureUUID("cap-sku-7")
	locked := capacityFixtureUUID("cap-sku-8")
	t.Cleanup(func() {
		execEffectiveMarginsSQL(t, rp, `DELETE FROM store_product_overrides WHERE store_id=$1 AND product_id=$2`, store, product)
		execEffectiveMarginsSQL(t, rp, `DELETE FROM store_variant_overrides WHERE store_id=$1 AND product_id=$2`, store, product)
		execEffectiveMarginsSQL(t, rp, `DELETE FROM store_margin_locks WHERE store_id=$1 AND scope='variant' AND scope_id=$2`, store, locked)
		execEffectiveMarginsSQL(t, rp, `DELETE FROM product_margin_ceilings WHERE product_id=$1`, product)
	})
	execEffectiveMarginsSQL(t, rp, `INSERT INTO store_product_overrides (store_id,product_id,override_value,mode) VALUES ($1,$2,15,'percent')`, store, product)
	execEffectiveMarginsSQL(t, rp, `INSERT INTO store_variant_overrides (store_id,product_id,variant_id,override_value,mode) VALUES ($1,$2,$3,15,'percent'),($1,$2,$4,15,'percent')`, store, product, overridden, locked)
	execEffectiveMarginsSQL(t, rp, `INSERT INTO store_margin_locks (store_id,scope,scope_id,lock_mode,lock_value) VALUES ($1,'variant',$2,'admin_set',29)`, store, locked)

	assertProjection := func(t *testing.T, wantCeiling float64) {
		t.Helper()
		got := readEffectiveMargins(t, rp, store, owner)
		for _, want := range []effectiveMarginProjection{
			{ProductID: product, VariantID: unset, MarginPct: wantCeiling, CeilingPct: wantCeiling, Tier: "max-default"},
			{ProductID: product, VariantID: overridden, MarginPct: 15, CeilingPct: wantCeiling, Tier: "variant"},
			{ProductID: product, VariantID: locked, MarginPct: 29, CeilingPct: wantCeiling, Tier: "variant", Locked: true},
		} {
			actual := got[want.VariantID]
			if actual.ProductID != want.ProductID || actual.MarginPct != want.MarginPct || actual.CeilingPct != want.CeilingPct ||
				actual.Tier != want.Tier || actual.Locked != want.Locked {
				t.Errorf("variant %s: got %+v, want %+v", want.VariantID, actual, want)
			}
		}
		if _, leaked := got[capacityFixtureUUID("cap-sku-1")]; leaked {
			t.Error("response included a variant published only in another store")
		}
	}
	t.Run("unset override and admin lock", func(t *testing.T) {
		settings, err := rp.pricingRepo.GetPlatformPricingSettings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		assertProjection(t, settings.MaxProfitMargin)
	})
	t.Run("product ceiling applies to unset variant and never replaces the lock", func(t *testing.T) {
		execEffectiveMarginsSQL(t, rp, `INSERT INTO product_margin_ceilings (product_id,max_margin_pct) VALUES ($1,25)`, product)
		assertProjection(t, 25)
	})
	t.Run("non-owner seller receives 403", func(t *testing.T) {
		out := requestEffectiveMargins(t, rp, store, capacityFixtureUUID("cap-seller-2"), "seller")
		if out.Code != http.StatusForbidden {
			t.Fatalf("non-owner: status %d, body %s", out.Code, out.Body.String())
		}
	})
	t.Run("owner without seller role receives 403", func(t *testing.T) {
		out := requestEffectiveMargins(t, rp, store, owner, "customer")
		if out.Code != http.StatusForbidden {
			t.Fatalf("customer role: status %d, body %s", out.Code, out.Body.String())
		}
	})
}

func execEffectiveMarginsSQL(t *testing.T, rp *gap13Repos, query string, args ...any) {
	t.Helper()
	if _, err := rp.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestSellerVariantEffectiveMarginsMatchDropshipperCharge(t *testing.T) {
	rp := variantEffectiveMarginFixture(t)
	const store, product = "cap-store-2", "cap-product-1"
	const wholesale, platformFee = 1_500_000, 150_000
	const reference = wholesale + platformFee
	owner := capacityFixtureUUID("cap-seller-2")
	variant := capacityFixtureUUID("cap-sku-1")
	customer := capacityFixtureUUID("cap-customer-1")
	campaignID := "drp33-charge-parity"
	t.Cleanup(func() {
		execEffectiveMarginsSQL(t, rp, `DELETE FROM seller_discount_campaigns WHERE id=$1`, campaignID)
		execEffectiveMarginsSQL(t, rp, `UPDATE stores SET seller_deals_widget_config=NULL WHERE id=$1`, store)
		execEffectiveMarginsSQL(t, rp, `DELETE FROM store_product_overrides WHERE store_id=$1 AND product_id=$2`, store, product)
		execEffectiveMarginsSQL(t, rp, `DELETE FROM product_margin_ceilings WHERE product_id=$1`, product)
		execEffectiveMarginsSQL(t, rp, `DELETE FROM platform_pct_products WHERE product_id=$1`, product)
		execEffectiveMarginsSQL(t, rp, `UPDATE product_variants SET supplier_base_cost=NULL WHERE id=$1`, variant)
	})
	execEffectiveMarginsSQL(t, rp, `UPDATE product_variants SET supplier_base_cost=$1 WHERE id=$2`, wholesale, variant)
	execEffectiveMarginsSQL(t, rp, `INSERT INTO platform_pct_products (product_id,pct) VALUES ($1,10)`, product)
	execEffectiveMarginsSQL(t, rp, `INSERT INTO product_margin_ceilings (product_id,max_margin_pct) VALUES ($1,35)`, product)
	execEffectiveMarginsSQL(t, rp, `INSERT INTO store_product_overrides (store_id,product_id,override_value,mode) VALUES ($1,$2,15,'percent')`, store, product)

	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{
		UserID: customer, Mobile: "09900000001", Role: "customer", Capabilities: []string{"customer"},
	})
	items := fmt.Sprintf(`[{"productId":%q,"variantId":%q,"quantity":1}]`, product, variant)
	const destination = `{"province":"Tehran","city":"Tehran","address":"Synthetic DRP-33 address","postalCode":"1111111111"}`
	const startingPrice = reference * 135 / 100
	for _, tc := range []struct {
		name        string
		wantPayable int64
		setup       func(t *testing.T)
	}{
		{name: "without discounts", wantPayable: startingPrice},
		{name: "widget subtracts ten margin points", wantPayable: reference * 125 / 100, setup: func(t *testing.T) {
			execEffectiveMarginsSQL(t, rp, `UPDATE stores SET seller_deals_widget_config=$2 WHERE id=$1`, store,
				fmt.Sprintf(`{"productIds":[%q],"discounts":{%q:10},"endsAt":%q}`, product, product, time.Now().Add(time.Hour).Format(time.RFC3339)))
			t.Cleanup(func() {
				execEffectiveMarginsSQL(t, rp, `UPDATE stores SET seller_deals_widget_config=NULL WHERE id=$1`, store)
			})
		}},
		{name: "campaign is capped at the variant margin", wantPayable: reference, setup: func(t *testing.T) {
			execEffectiveMarginsSQL(t, rp, `INSERT INTO seller_discount_campaigns (id,store_id,name,scope,target_id,mode,value,starts_at,ends_at)
				VALUES($1,$2,'DRP-33 campaign','variant',$3,'percent',50,NOW()-INTERVAL '1 hour',NOW()+INTERVAL '1 hour')`, campaignID, store, variant)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			projection := readEffectiveMargins(t, rp, store, owner)[variant]
			// The projection always reports the independently calculated
			// pre-discount starting price, even with a live seller promotion.
			if projection.ProductID != product || projection.Tier != "max-default" || projection.Locked || projection.MarginPct != 35 ||
				projection.ReferenceCost != reference || projection.FinalPrice != startingPrice {
				t.Fatalf("projection = %+v, want reference cost %d and final price %d", projection, reference, startingPrice)
			}

			quote := httptest.NewRecorder()
			handleDiscountQuote(quote, httptest.NewRequest(http.MethodPost, "/api/v1/stores/"+store+"/discount-quote", strings.NewReader(`{"items":`+items+`}`)),
				rp.catalogRepo, rp.storeRepo, rp.pricingRepo, store)
			var totals struct{ PayableAmount, OriginalPayable, SellerDiscount int64 }
			wantDiscount := startingPrice - tc.wantPayable
			if quote.Code != http.StatusOK || json.Unmarshal(quote.Body.Bytes(), &totals) != nil ||
				totals.PayableAmount != tc.wantPayable || totals.OriginalPayable != startingPrice || totals.SellerDiscount != wantDiscount {
				t.Fatalf("discount quote: status %d body %s, want %d from %d (discount %d)", quote.Code, quote.Body.String(), tc.wantPayable, startingPrice, wantDiscount)
			}

			shipping := httptest.NewRecorder()
			shippingReq := httptest.NewRequest(http.MethodPost, "/api/v1/shipping/quote", strings.NewReader(`{"items":`+items+`,"destination":`+destination+`}`)).WithContext(ctx)
			handleShippingQuote(shipping, shippingReq, ctx, rp.catalogRepo, rp.supplierRepo, rp.db)
			var shippingQuote struct{ QuoteID string }
			if shipping.Code != http.StatusOK || json.Unmarshal(shipping.Body.Bytes(), &shippingQuote) != nil || shippingQuote.QuoteID == "" {
				t.Fatalf("shipping quote: status %d body %s", shipping.Code, shipping.Body.String())
			}
			checkoutBody := fmt.Sprintf(`{"storeId":%q,"items":%s,"customerInfo":%s,"paymentMethod":"cash","shippingQuoteId":%q}`,
				store, items, strings.Replace(destination, "{", `{"fullName":"Synthetic DRP-33 customer",`, 1), shippingQuote.QuoteID)
			checkout := httptest.NewRecorder()
			checkoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(checkoutBody)).WithContext(ctx)
			handleCreateOrder(checkout, checkoutReq, ctx, rp.orderRepo, rp.catalogRepo, rp.storeRepo, rp.shipmentRepo, rp.pricingRepo, rp.supplierRepo, rp.db)
			orderID := gap13OrderID(t, checkout)

			var gotVariant string
			var gotPrice, gotDiscount, gotWholesale, gotFee, gotMargin int64
			if err := rp.db.QueryRow(`SELECT variant_id,final_unit_price,discount_amount,wholesale_cost,platform_fee,seller_margin FROM order_items WHERE order_id=$1`, orderID).
				Scan(&gotVariant, &gotPrice, &gotDiscount, &gotWholesale, &gotFee, &gotMargin); err != nil {
				t.Fatal(err)
			}
			if gotVariant != variant || gotPrice != tc.wantPayable || gotDiscount != wantDiscount || gotWholesale != wholesale || gotFee != platformFee || gotMargin != tc.wantPayable-reference {
				t.Fatalf("checkout snapshot variant %s: price %d discount %d wholesale %d platform fee %d seller margin %d", gotVariant, gotPrice, gotDiscount, gotWholesale, gotFee, gotMargin)
			}
		})
	}
}
