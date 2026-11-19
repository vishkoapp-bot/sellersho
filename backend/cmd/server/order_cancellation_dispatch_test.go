package main

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestOrderCancellationDispatchHTTPConflict(t *testing.T) {
	raw := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	base, err := testdb.Open(t, raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := "inv01_http_" + uuid.NewString()[:8]
	if _, err := base.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	db, err := testdb.Open(t, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close(); base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); base.Close() }()
	for _, ddl := range []string{
		`CREATE TABLE orders(id VARCHAR(50) PRIMARY KEY,order_number VARCHAR(30),customer_user_id uuid,status text DEFAULT 'processing'::character varying ,inventory_captured bool DEFAULT false ,shipped_at timestamptz,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			payable_amount BIGINT NOT NULL DEFAULT 0,
			payment_status VARCHAR(20) NOT NULL DEFAULT 'pending',
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			box_size character varying(80),
			carrier character varying(100),
			confirmed_at timestamp with time zone,
			coupon_discount_amount bigint,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			customer_address text,
			customer_city character varying(50),
			customer_full_name character varying(100),
			customer_phone character varying(20),
			customer_postal_code character varying(20),
			customer_province character varying(50),
			discount_amount bigint NOT NULL DEFAULT 0,
			discount_code text,
			dispute_reason text,
			disputed_at timestamp with time zone,
			dropshipper_profit bigint NOT NULL DEFAULT 0,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
			gateway_amount bigint NOT NULL DEFAULT 0,
			idempotency_key text,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			packed_at timestamp with time zone,
			payment_expires_at timestamp with time zone DEFAULT (now() + '00:15:00'::interval),
			payment_method character varying(20) NOT NULL DEFAULT 'cash'::character varying,
			platform_addon_amount bigint NOT NULL DEFAULT 0,
			platform_profit bigint NOT NULL DEFAULT 0,
			profit_released boolean DEFAULT false,
			profit_released_at timestamp with time zone,
			referral_commission bigint DEFAULT 0,
			referral_commission_released boolean DEFAULT false,
			referrer_seller_id character varying(50),
			return_window_ends_at timestamp with time zone,
			seller_discount_amount bigint NOT NULL DEFAULT 0,
			shipping_cost bigint NOT NULL DEFAULT 0,
			shipping_label text,
			shipping_quote_id text,
			store_id character varying(50),
			store_name character varying(100),
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			total_amount bigint,
			tracking_code character varying(50),
			tracking_number character varying(100),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_amount bigint NOT NULL DEFAULT 0,
			weight_grams integer)`,
		`CREATE TABLE supplier_shipments(id text PRIMARY KEY,order_id text,status text DEFAULT 'pending'::character varying ,shipped_at timestamptz,delivered_at timestamptz,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			tracking_number VARCHAR(255),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			carrier character varying(100),
			created_at timestamp with time zone DEFAULT now(),
			customer_name character varying(255),
			customer_phone character varying(20),
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			send_price bigint,
			shipping_address jsonb,
			supplier_id character varying(255),
			supplier_notes text,
			tapin_box_id text,
			tapin_last_status text,
			tapin_register_attempted_at timestamp with time zone,
			tapin_registered_at timestamp with time zone,
			tapin_service text,
			updated_at timestamp with time zone DEFAULT now())`,
		`INSERT INTO orders VALUES('dispatch','ORDER1','00000000-0000-0000-0000-000000000001','processing',true,NULL)`,
		`INSERT INTO supplier_shipments VALUES('leg1','dispatch','shipped',now(),NULL),('leg2','dispatch','pending',NULL,NULL)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"customer", "admin"} {
		ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: "00000000-0000-0000-0000-000000000001", Role: role})
		req := httptest.NewRequest("POST", "/api/v1/orders/ORDER1/cancel", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		handleOrderCancel(w, req, ctx, "ORDER1", repository.NewOrderRepository(db))
		if w.Code != 409 {
			t.Fatalf("%s status=%d body=%s", role, w.Code, w.Body.String())
		}
	}
}
