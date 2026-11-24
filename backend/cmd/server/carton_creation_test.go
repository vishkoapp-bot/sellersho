package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestCartonCreationAPIAtomicAndRetrySafe(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	root, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "carton_api_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := root.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid disposable URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := testdb.Open(t, u.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE suppliers(id TEXT PRIMARY KEY,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			approval_status VARCHAR(20) NOT NULL DEFAULT 'approved',
			name VARCHAR(200) NOT NULL DEFAULT '',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			warehouse_city VARCHAR(255) NOT NULL DEFAULT 'تهران',
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			approved_at timestamp with time zone,
			approved_by character varying(50),
			badge character varying(50),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			default_markup_type character varying(10),
			default_markup_value numeric(10,2),
			delivery_score integer DEFAULT 100,
			description text,
			logo_url text,
			owner_user_id uuid,
			rating numeric(3,2) DEFAULT 5.00,
			rejection_reason text,
			signed_contract_version integer NOT NULL DEFAULT 1,
			supports_direct_shipping boolean DEFAULT false,
			suspended_at timestamp with time zone,
			suspended_by text,
			suspension_reason text,
			tapin_shop_id text,
			tapin_token text,
			total_products integer DEFAULT 0,
			warehouse_city_confirmed boolean NOT NULL DEFAULT false,
			warehouse_city_source character varying(20))`,
		`CREATE TABLE warehouses(id TEXT PRIMARY KEY,is_active BOOL DEFAULT true ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			tapin_province_id INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			capacity_m3_total numeric(10,3),
			capacity_units_total integer,
			city_name text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			is_receiving_open boolean NOT NULL DEFAULT true,
			name text,
			priority integer NOT NULL DEFAULT 100,
			province_name text,
			tapin_city_id integer,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE supplier_warehouses(id TEXT PRIMARY KEY,supplier_id TEXT,is_active BOOL DEFAULT true ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			address text,
			city_name text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			name text,
			phone character varying(20),
			postal_code character varying(10),
			province_name text,
			tapin_city_id integer,
			tapin_province_id integer,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE products(id VARCHAR(50) PRIMARY KEY,supplier_id TEXT,origin_supplier_warehouse_id TEXT,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_status VARCHAR(255) NOT NULL DEFAULT 'unverified',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_version INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribute_schema jsonb,
			barcode character varying(64),
			base_price_override bigint,
			brand text NOT NULL DEFAULT ''::text,
			category_slug character varying(100),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			description text,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
			height_mm integer,
			image_url text,
			implicit_agreement boolean NOT NULL DEFAULT false,
			implicit_agreement_until timestamp with time zone,
			is_active boolean NOT NULL DEFAULT true,
			is_featured boolean DEFAULT false,
			is_original boolean NOT NULL DEFAULT true,
			is_special_deal boolean DEFAULT false,
			length_mm integer,
			markup_type character varying(10),
			markup_value numeric(10,2),
			measurement_revision_id text,
			measurement_source character varying(20),
			original_price bigint,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			platform_margin bigint,
			preparation_days integer DEFAULT 1,
			product_type text NOT NULL DEFAULT 'legacy'::text,
			rating numeric(3,2) NOT NULL DEFAULT 0,
			reviews_count integer NOT NULL DEFAULT 0,
			schema_version integer NOT NULL DEFAULT 0,
			shipping_class text NOT NULL DEFAULT 'standard'::text,
			slug character varying(255),
			specs jsonb DEFAULT '{}'::jsonb,
			spn character varying(20),
			spn_number bigint,
			stock integer NOT NULL DEFAULT 0,
			supplier_base_cost bigint,
			supplier_warranty character varying(100),
			supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
			tax_class text NOT NULL DEFAULT 'standard'::text,
			title character varying(255),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying,
			warranty_property_id character varying(60),
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE product_variants(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,product_id TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
			combination_key text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			height_mm integer,
			inventory_mode text NOT NULL DEFAULT 'legacy'::text,
			is_active boolean NOT NULL DEFAULT true,
			is_default boolean NOT NULL DEFAULT false,
			length_mm integer,
			max_cart_qty integer,
			measurement_revision_id text,
			measurement_source text,
			measurement_status text NOT NULL DEFAULT 'unverified'::text,
			measurement_version integer NOT NULL DEFAULT 0,
			min_cart_qty integer NOT NULL DEFAULT 1,
			sale_from timestamp with time zone,
			sale_price bigint,
			sale_to timestamp with time zone,
			selection jsonb NOT NULL DEFAULT '{}'::jsonb,
			shipping_class text,
			spnc character varying(12),
			spnc_number integer,
			stock integer NOT NULL DEFAULT 0,
			supplier_base_cost bigint,
			tax_class text,
			variant_name text NOT NULL DEFAULT 'پیش‌فرض'::text,
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE shipments(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,shipment_code TEXT UNIQUE,supplier_id TEXT,status TEXT DEFAULT 'draft'::character varying ,warehouse_id TEXT,supplier_warehouse_id TEXT,created_at TIMESTAMPTZ DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			creation_key character varying(128),
			received_at timestamp with time zone,
			request_fingerprint character varying(64))`,
		`CREATE TABLE shipment_items(id UUID PRIMARY KEY DEFAULT gen_random_uuid(),shipment_id UUID REFERENCES shipments(id),variant_id UUID REFERENCES product_variants(id),expected_qty INT CHECK(expected_qty>0),received_qty INT DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			inventory_received_qty integer NOT NULL DEFAULT 0,
			qc_status character varying(20) NOT NULL DEFAULT 'pending'::character varying)`,
		`CREATE TABLE inventory_reservations(id TEXT PRIMARY KEY,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			idempotency_key character varying(150),
			location_key character varying(150),
			product_id character varying(50),
			quantity integer,
			reference_id character varying(80),
			reference_type character varying(40),
			released_at timestamp with time zone,
			status character varying(20) NOT NULL DEFAULT 'active'::character varying,
			supplier_id character varying(50),
			variant_id uuid)`,
		`CREATE TABLE inbound_shipments(id TEXT PRIMARY KEY,status TEXT DEFAULT 'in_transit'::character varying ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			carrier character varying(100),
			cartons_count integer,
			completed_at timestamp with time zone,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			dispatch_at timestamp with time zone,
			dispatched_at timestamp with time zone,
			expected_arrival timestamp with time zone,
			items jsonb DEFAULT '[]'::jsonb,
			items_count integer,
			notes text,
			received_at timestamp with time zone,
			scanned_items jsonb DEFAULT '[]'::jsonb,
			started_receiving_at timestamp with time zone,
			supplier_id character varying(50),
			supplier_name character varying(100),
			supplier_warehouse_id text,
			truck_plate character varying(50),
			warehouse_id text,
			warehouse_name character varying(100) NOT NULL DEFAULT 'انبار مرکزی تهران'::character varying)`,
		`CREATE TABLE inventory_balances(LIKE public.inventory_balances INCLUDING ALL)`,
		`CREATE TABLE inventory_movements(LIKE public.inventory_movements INCLUDING ALL)`,
		// The scoped connection must not fall back to public for SPNCX writes.
		`CREATE TABLE product_units (LIKE public.product_units INCLUDING ALL)`,
		`ALTER TABLE product_units
			ADD FOREIGN KEY (variant_id) REFERENCES product_variants(id) ON DELETE RESTRICT,
			ADD FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT,
			ADD FOREIGN KEY (balance_id) REFERENCES inventory_balances(id) ON DELETE RESTRICT`,
		`CREATE TABLE product_unit_events (LIKE public.product_unit_events INCLUDING ALL)`,
		`ALTER TABLE product_unit_events ADD FOREIGN KEY (unit_id) REFERENCES product_units(id) ON DELETE RESTRICT`,
		`INSERT INTO suppliers VALUES('s'); INSERT INTO warehouses VALUES('w',TRUE); INSERT INTO supplier_warehouses VALUES('sw','s',TRUE); INSERT INTO products VALUES('p','s','sw')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../migrations/202611140005_carton_business_idempotency.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	provenance, err := os.ReadFile("../../migrations/202611140006_inventory_transfer_provenance.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(provenance)); err != nil {
		t.Fatal(err)
	}
	red, blue := uuid.NewString(), uuid.NewString()
	for _, variant := range []struct{ id, spnc string }{{red, "TESTRED"}, {blue, "TESTBLUE"}} {
		if _, err := db.Exec(`INSERT INTO product_variants(id,product_id,spnc) VALUES ($1,'p',$2)`, variant.id, variant.spnc); err != nil {
			t.Fatal(err)
		}
		balanceID := uuid.NewString()
		if _, err := db.Exec(`INSERT INTO inventory_balances
			(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled,supplier_warehouse_id,unit_tracked)
			VALUES($1,'p','s',$2,'supplier','supplier','supplier-warehouse:sw','sellable',1,TRUE,'sw',TRUE)`, balanceID, variant.id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO product_units (code, variant_id, product_id, sequence, status, balance_id)
			VALUES ($1, $2, 'p', 1, 'in_stock', $3)`, variant.spnc+"-X0001", variant.id, balanceID); err != nil {
			t.Fatal(err)
		}
	}
	h := &SupplierHandlers{supplierRepo: repository.NewSupplierRepository(db)}
	call := func(key string, items []repository.CartonShipmentItem) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]any{"warehouse_id": "w", "creation_key": key, "items": items})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/shipments", strings.NewReader(string(payload)))
		req = req.WithContext(context.WithValue(req.Context(), supplierIDKey, "s"))
		rec := httptest.NewRecorder()
		h.HandleCreateCartonShipment(rec, req)
		return rec
	}
	counts := func(headers, lines int) {
		t.Helper()
		var h, l, r, inStock, reserved, events int
		if err := db.QueryRow(`SELECT
			(SELECT COUNT(*) FROM shipments), (SELECT COUNT(*) FROM shipment_items), (SELECT COUNT(*) FROM inventory_reservations),
			(SELECT COUNT(*) FROM product_units WHERE status='in_stock'),
			(SELECT COUNT(*) FROM product_units WHERE status='reserved'),
			(SELECT COUNT(*) FROM product_unit_events)`).Scan(&h, &l, &r, &inStock, &reserved, &events); err != nil {
			t.Fatal(err)
		}
		if h != headers || l != lines || r != 0 || inStock != 2-lines || reserved != lines || events != lines {
			t.Fatalf("atomic counts: headers=%d lines=%d reservations=%d in_stock=%d reserved=%d events=%d; want %d/%d/0/%d/%d/%d",
				h, l, r, inStock, reserved, events, headers, lines, 2-lines, lines, lines)
		}
	}
	items := []repository.CartonShipmentItem{{VariantID: red, ExpectedQty: 1}}
	if rec := call("", items); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing creation key: %d", rec.Code)
	}
	if rec := call("overflow", []repository.CartonShipmentItem{{VariantID: red, ExpectedQty: 1}, {VariantID: blue, ExpectedQty: 2147483648}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("overflow: %d", rec.Code)
	}
	counts(0, 0)
	if _, err := db.Exec(`CREATE FUNCTION fail_carton_line() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF EXISTS(SELECT 1 FROM shipment_items WHERE shipment_id=NEW.shipment_id) THEN RAISE EXCEPTION 'injected line failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_line BEFORE INSERT ON shipment_items FOR EACH ROW EXECUTE FUNCTION fail_carton_line()`); err != nil {
		t.Fatal(err)
	}
	if rec := call("fail", []repository.CartonShipmentItem{{VariantID: red, ExpectedQty: 1}, {VariantID: blue, ExpectedQty: 1}}); rec.Code == http.StatusOK {
		t.Fatal("injected failure succeeded")
	}
	counts(0, 0)
	first := call("replay", items)
	if first.Code != http.StatusOK {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	replay := call("replay", items)
	if replay.Code != http.StatusOK || first.Body.String() != replay.Body.String() {
		t.Fatalf("replay differs: %d", replay.Code)
	}
	if rec := call("replay", []repository.CartonShipmentItem{{VariantID: red, ExpectedQty: 2}}); rec.Code != http.StatusConflict {
		t.Fatalf("key payload conflict: %d want409", rec.Code)
	}
	missingSKU := uuid.NewString()
	if rec := call("replay", []repository.CartonShipmentItem{{VariantID: missingSKU, ExpectedQty: 1}}); rec.Code != http.StatusConflict {
		t.Fatalf("existing key with nonexistent changed SKU: %d want409", rec.Code)
	}
	if rec := call("fresh-missing", []repository.CartonShipmentItem{{VariantID: missingSKU, ExpectedQty: 1}}); rec.Code != http.StatusNotFound {
		t.Fatalf("fresh key with nonexistent SKU: %d want404", rec.Code)
	}
	counts(1, 1)
}
