package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func TestHistoricalReportDeterministicAndReviewReadOnly(t *testing.T) {
	db := openReturnsIntegrationDB(t)
	defer db.Close()
	ctx := context.Background()
	suffix := uuid.NewString()[:12]
	productID := "hist-report-product-" + suffix
	reservationID := "RES-hist-" + suffix
	if _, err := db.ExecContext(ctx, `INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,fulfillment_method) VALUES ($1,$2,$3,'digital','sup-technolife',100000,50000,1000,TRUE,2,'warehouse')`, productID, "Historical report fixture", productID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,'sup-technolife','supplier','bin','hist-bin','sellable',2,TRUE),($3,$2,'sup-technolife','supplier','warehouse','hist-quarantine','quarantine',3,TRUE)`, "INV-hist-sellable-"+suffix, productID, "INV-hist-quarantine-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO inventory_reservations (id,product_id,supplier_id,location_key,quantity,reference_type,reference_id,status,idempotency_key) VALUES ($1,$2,'sup-technolife','hist-bin',1,'test',$3,'active',$4)`, reservationID, productID, "hist-ref-"+suffix, "hist-idem-"+suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM historical_reconciliation_reviews WHERE product_id=$1`, productID)
		_, _ = db.ExecContext(ctx, `DELETE FROM audit_logs WHERE action='historical_reconciliation_review' AND details LIKE '%' || $1 || '%'`, productID)
		_, _ = db.ExecContext(ctx, `DELETE FROM inventory_reservations WHERE product_id=$1`, productID)
		_, _ = db.ExecContext(ctx, `DELETE FROM inventory_balances WHERE product_id=$1`, productID)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE id=$1`, productID)
	})
	repo := NewSupplierRepository(db)
	first, err := repo.GetHistoricalInventoryReport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.GetHistoricalInventoryReport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	find := func(rows []HistoricalInventoryReportRow) HistoricalInventoryReportRow {
		for _, row := range rows {
			if row.ProductID == productID {
				return row
			}
		}
		t.Fatalf("fixture missing from report")
		return HistoricalInventoryReportRow{}
	}
	firstRow, secondRow := find(first), find(second)
	firstJSON, _ := json.Marshal(firstRow)
	secondJSON, _ := json.Marshal(secondRow)
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("historical report is not deterministic")
	}
	if firstRow.CanonicalQuantity != 5 || firstRow.CanonicalSellableQuantity != 2 || len(firstRow.LocationBreakdown) != 2 || len(firstRow.ActiveReservations) != 1 {
		t.Fatalf("unexpected report detail: %+v", firstRow)
	}
	if !firstRow.PhysicalInventory || firstRow.MeasurementClassification != "PHYSICAL_MEASUREMENT_MISSING" || !firstRow.Unresolved {
		t.Fatalf("physical measurement classification is not explicit: %+v", firstRow)
	}
	var beforeStock, beforeBalances, beforeMovements int
	if err := db.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=$1`, productID).Scan(&beforeStock); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_balances WHERE product_id=$1`, productID).Scan(&beforeBalances); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE product_id=$1`, productID).Scan(&beforeMovements); err != nil {
		t.Fatal(err)
	}
	reviewCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	submit := func() (*HistoricalReview, error) {
		return repo.ReviewHistoricalInventory(reviewCtx, productID, "in_review", "evidence review only", "integration-reviewer", []string{"hist-bin", "second-reference"})
	}
	type result struct {
		review *HistoricalReview
		err    error
	}
	const callers = 6
	start := make(chan struct{})
	results := make(chan result, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			review, err := submit()
			results <- result{review, err}
		}()
	}
	close(start)
	var recorded []byte
	var savedReviewID string
	checkReview := func(review *HistoricalReview, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if review.ID == "" || review.ReviewedAt == nil || review.UpdatedAt.IsZero() {
			t.Fatalf("missing persisted review metadata: %+v", review)
		}
		data, err := json.Marshal(review)
		if err != nil {
			t.Fatal(err)
		}
		if recorded == nil {
			recorded, savedReviewID = data, review.ID
		} else if string(data) != string(recorded) {
			t.Fatalf("retry did not return persisted review: got %s, want %s", data, recorded)
		}
	}
	for i := 0; i < callers; i++ {
		outcome := <-results
		checkReview(outcome.review, outcome.err)
	}
	checkReview(submit())
	var reviewAuditCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='historical_reconciliation_review' AND details LIKE '%' || $1 || '%'`, productID).Scan(&reviewAuditCount); err != nil {
		t.Fatal(err)
	}
	if reviewAuditCount != 1 {
		t.Fatalf("review retry created duplicate audit events: %d", reviewAuditCount)
	}
	changed, err := repo.ReviewHistoricalInventory(reviewCtx, productID, "awaiting_evidence", "a distinct decision", "integration-reviewer", []string{"second-reference"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID != savedReviewID || changed.ReviewStatus != "awaiting_evidence" {
		t.Fatalf("changed decision must update the current review: %+v", changed)
	}
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='historical_reconciliation_review' AND details LIKE '%' || $1 || '%'`, productID).Scan(&reviewAuditCount); err != nil {
		t.Fatal(err)
	}
	if reviewAuditCount != 2 {
		t.Fatalf("changed decision did not create exactly one audit event: %d", reviewAuditCount)
	}
	var afterStock, afterBalances, afterMovements int
	if err := db.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=$1`, productID).Scan(&afterStock); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_balances WHERE product_id=$1`, productID).Scan(&afterBalances); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE product_id=$1`, productID).Scan(&afterMovements); err != nil {
		t.Fatal(err)
	}
	if beforeStock != afterStock || beforeBalances != afterBalances || beforeMovements != afterMovements {
		t.Fatalf("review mutated inventory: before=%d/%d/%d after=%d/%d/%d", beforeStock, beforeBalances, beforeMovements, afterStock, afterBalances, afterMovements)
	}
}

func TestMeasurementClassificationSeparatesUnknownPresenceFromCatalogOnly(t *testing.T) {
	cases := []struct {
		name string
		row  HistoricalInventoryReportRow
		want string
	}{
		{name: "catalog without stock or canonical evidence", row: HistoricalInventoryReportRow{}, want: "CATALOG_ONLY_MEASUREMENT_MISSING"},
		{name: "legacy stock without canonical evidence", row: HistoricalInventoryReportRow{LegacyStock: 3}, want: "PHYSICAL_PRESENCE_UNCONFIRMED"},
		{name: "canonical row with zero quantity", row: HistoricalInventoryReportRow{CanonicalRows: 1}, want: "PHYSICAL_PRESENCE_UNCONFIRMED"},
		{name: "physical inventory without verified measurement", row: HistoricalInventoryReportRow{PhysicalInventory: true, MeasurementStatus: "unverified"}, want: "PHYSICAL_MEASUREMENT_MISSING"},
		{name: "physical inventory with verified measurement", row: HistoricalInventoryReportRow{PhysicalInventory: true, HasMeasurement: true, MeasurementStatus: "verified"}, want: "VERIFIED"},
		{name: "quarantine or damaged physical inventory", row: HistoricalInventoryReportRow{PhysicalInventory: true, MeasurementStatus: "unverified"}, want: "PHYSICAL_MEASUREMENT_MISSING"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := measurementClassification(testCase.row); got != testCase.want {
				t.Fatalf("measurement classification=%s, want %s", got, testCase.want)
			}
		})
	}
}

func openReturnsIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Skipf("Postgres integration unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("Postgres integration unavailable: %v", err)
	}
	var migration string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(to_regclass('public.supplier_return_parcels')::text,'')`).Scan(&migration); err != nil || migration == "" {
		db.Close()
		t.Skip("000024 schema is not applied; run the migration before this integration test")
	}
	return db
}

func TestSupplierReturnInventoryFinancialAndTariffLifecycle(t *testing.T) {
	db := openReturnsIntegrationDB(t)
	defer db.Close()
	ctx := context.Background()
	suffix := uuid.NewString()[:12]
	productID := "ret-int-product-" + suffix
	tariffID := "TAR-INT-" + suffix
	originRegion := "INT-" + suffix
	destinationRegion := "DEST-" + suffix
	idem := "return-int:" + suffix
	parcelIdem := "parcel-int:" + suffix
	chargeIdem := "charge-int:" + suffix
	var firstReturnID, secondReturnID string

	_, err := db.ExecContext(ctx, `
		INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,fulfillment_method)
		VALUES ($1,$2,$3,'digital','sup-technolife',100000,50000,1000,TRUE,10,'warehouse')`,
		productID, "Return integration product", productID)
	if err != nil {
		t.Fatal(err)
	}
	// The SKU-linked return lots retain their real source bin FK.
	if _, err := db.ExecContext(ctx, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf) VALUES('integration-bin','return-fixture','1','1') ON CONFLICT (bin_code) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,'sup-technolife','supplier','bin','bin:integration-bin','sellable',10,TRUE)`, "INV-"+suffix, productID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_financial_events WHERE return_request_id IN ($1,$2)`, firstReturnID, secondReturnID)
		_, _ = db.ExecContext(ctx, `DELETE FROM ledger_entries WHERE related_order_or_dispute IN ($1,$2)`, firstReturnID, secondReturnID)
		_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_parcel_items WHERE parcel_id IN (SELECT id FROM supplier_return_parcels WHERE return_request_id IN ($1,$2))`, firstReturnID, secondReturnID)
		_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_parcels WHERE return_request_id IN ($1,$2)`, firstReturnID, secondReturnID)
		_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_events WHERE return_request_id IN ($1,$2)`, firstReturnID, secondReturnID)
		_, _ = db.ExecContext(ctx, `DELETE FROM inventory_movements WHERE reference_id IN ($1,$2)`, firstReturnID, secondReturnID)
		_, _ = db.ExecContext(ctx, `DELETE FROM inventory_reservations WHERE reference_id IN ($1,$2)`, firstReturnID, secondReturnID)
		_, _ = db.ExecContext(ctx, `DELETE FROM inventory_balances WHERE product_id=$1`, productID)
		_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_requests WHERE id IN ($1,$2)`, firstReturnID, secondReturnID)
		_, _ = db.ExecContext(ctx, `DELETE FROM carrier_tariffs WHERE id=$1`, tariffID)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE id=$1`, productID)
	})

	repo := NewSupplierRepository(db)
	// An unresolved legacy request must fail before changing quantity or creating
	// reservations, movements, requests, or unit coverage (§4.2 row 27).
	if _, err := repo.CreateReturnRequestWithIdempotency(ctx, "sup-technolife", productID, 3, "supplier_collect", "unresolved return", idem, "integration-user"); !errors.Is(err, ErrSupplierReturnVariantUnresolved) {
		t.Fatalf("unresolved return error = %v, want supplier_return_variant_unresolved", err)
	}
	var beforeStock, beforeQuantity, effects int
	if err := db.QueryRowContext(ctx, `SELECT p.stock,
		(SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=p.id AND status='sellable'),
		(SELECT COUNT(*) FROM supplier_return_requests WHERE product_id=p.id) +
		(SELECT COUNT(*) FROM inventory_reservations WHERE product_id=p.id) +
		(SELECT COUNT(*) FROM inventory_movements WHERE product_id=p.id) +
		(SELECT COUNT(*) FROM product_units WHERE product_id=p.id)
		FROM products p WHERE p.id=$1`, productID).Scan(&beforeStock, &beforeQuantity, &effects); err != nil {
		t.Fatal(err)
	}
	if beforeStock != 10 || beforeQuantity != 10 || effects != 0 {
		t.Fatalf("unresolved return changed state: stock=%d sellable=%d effects=%d", beforeStock, beforeQuantity, effects)
	}
	// Legacy requests may omit the SKU only when the sole default is resolvable.
	variant := seedSupplierReturnLegacyVariant(t, db, productID)
	first, err := repo.CreateReturnRequestWithIdempotency(ctx, "sup-technolife", productID, 3, "supplier_collect", "integration return", idem, "integration-user")
	if err != nil {
		t.Fatal(err)
	}
	if first.VariantID != variant {
		t.Fatalf("resolved variant=%q, want %q", first.VariantID, variant)
	}
	firstReturnID = first.ID
	replay, err := repo.CreateReturnRequestWithIdempotency(ctx, "sup-technolife", productID, 3, "supplier_collect", "integration return", idem, "integration-user")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != replay.ID {
		t.Fatalf("idempotency created two return requests: %s != %s", first.ID, replay.ID)
	}
	var stock int
	if err := db.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=$1`, productID).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 7 {
		t.Fatalf("stock after one idempotent request = %d, want 7", stock)
	}
	if err := repo.ReviewReturn(ctx, first.ID, "rejected", "integration rejection", "warehouse-user"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReviewReturn(ctx, first.ID, "rejected", "duplicate rejection", "warehouse-user"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=$1`, productID).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 10 {
		t.Fatalf("stock after repeated rejection = %d, want 10", stock)
	}
	if _, err := repo.CreateReturnRequestWithIdempotency(ctx, "sup-technolife", productID, 11, "supplier_collect", "must exceed canonical stock", "return-overflow:"+suffix, "integration-user"); err == nil {
		t.Fatal("return quantity above canonical sellable stock unexpectedly succeeded")
	}

	second, err := repo.CreateReturnRequestWithIdempotency(ctx, "sup-technolife", productID, 2, "supplier_collect", "integration approved return", "return-second:"+suffix, "integration-user")
	if err != nil {
		t.Fatal(err)
	}
	secondReturnID = second.ID
	if err := repo.ReviewReturn(ctx, second.ID, "approved", "approved", "warehouse-user"); err != nil {
		t.Fatal(err)
	}
	charge := int64(125000)
	parcel, err := repo.CreateReturnParcel(ctx, second.ID, "warehouse-user", ReturnParcelInput{
		Quantity: 2, CarrierCode: "tipax", PaymentMethod: "sender_pays", Origin: "Tehran", Destination: "Qom",
		LengthMM: 200, WidthMM: 150, HeightMM: 100, WeightGrams: 1800, FinalCharge: &charge, FinalChargeSource: "manual", FinalChargeRef: "carrier-ref-int",
	}, parcelIdem)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmReturnParcelCharge(ctx, second.ID, parcel.ID, "warehouse-user", chargeIdem); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmReturnParcelCharge(ctx, second.ID, parcel.ID, "warehouse-user", chargeIdem); err != nil {
		t.Fatal(err)
	}
	var ledgerCount, financialCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, second.ID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM supplier_return_financial_events WHERE return_request_id=$1`, second.ID).Scan(&financialCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 || financialCount != 1 {
		t.Fatalf("duplicate charge effects: ledger=%d financial=%d", ledgerCount, financialCount)
	}
	if err := repo.HandoverReturn(ctx, second.ID, "warehouse-user", "handover:"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := repo.HandoverReturn(ctx, second.ID, "warehouse-user", "handover:"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReceiveReturn(ctx, second.ID, "warehouse-user", "receive:"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReceiveReturn(ctx, second.ID, "warehouse-user", "receive:"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := repo.InspectReturn(ctx, second.ID, "warehouse-user", "inspect:"+suffix, 1, 1, "partial integration inspection"); err != nil {
		t.Fatal(err)
	}
	if err := repo.InspectReturn(ctx, second.ID, "warehouse-user", "inspect:"+suffix, 1, 1, "duplicate partial integration inspection"); err != nil {
		t.Fatal(err)
	}
	var status, inspection string
	if err := db.QueryRowContext(ctx, `SELECT status,inspection_status FROM supplier_return_requests WHERE id=$1`, second.ID).Scan(&status, &inspection); err != nil {
		t.Fatal(err)
	}
	if status != "qc_failed" || inspection != "partial" {
		t.Fatalf("inspection state = %s/%s, want qc_failed/partial", status, inspection)
	}
	var lifecycleMovements int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE reference_id=$1`, second.ID).Scan(&lifecycleMovements); err != nil {
		t.Fatal(err)
	}
	if lifecycleMovements != 5 {
		t.Fatalf("duplicate lifecycle effects: movements=%d, want 5", lifecycleMovements)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO carrier_tariffs (id,carrier_code,version,origin_region,destination_region,size_class,base_weight_grams,base_charge,extra_kg_charge,valid_from,created_by) VALUES ($1,'tipax',1,$2,$3,'size-4',1000,500000,30000,NOW(),'integration')`, tariffID, originRegion, destinationRegion); err != nil {
		t.Fatal(err)
	}
	quote, err := CalculateCarrierTariff(ctx, db, CarrierTariffInput{CarrierCode: "tipax", OriginRegion: originRegion, DestinationRegion: destinationRegion, SizeClass: "size-4", WeightGrams: 2800})
	if err != nil {
		t.Fatal(err)
	}
	if quote.Charge != 560000 || quote.Source != "tariff" || quote.TariffVersion != 1 {
		t.Fatalf("unexpected tariff quote: %+v", quote)
	}

	if err := repo.CancelReturn(ctx, "sup-technolife", second.ID, "must not cancel after handover", "supplier-user"); err == nil {
		t.Fatal("post-handover cancellation unexpectedly succeeded")
	}

}

func TestSupplierReturnNegativeBalanceAndChargeIdempotency(t *testing.T) {
	db := openReturnsIntegrationDB(t)
	defer db.Close()
	ctx := context.Background()
	suffix := uuid.NewString()[:12]
	supplierID := "sup-neg-" + suffix
	productID := "neg-product-" + suffix
	returnID := ""

	if _, err := db.ExecContext(ctx, `INSERT INTO suppliers (id,name,warehouse_city) VALUES ($1,$2,'Tehran')`, supplierID, "Negative balance integration supplier"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,fulfillment_method) VALUES ($1,$2,$3,'digital',$4,100000,50000,1000,TRUE,2,'warehouse')`, productID, "Negative balance product", productID, supplierID); err != nil {
		t.Fatal(err)
	}
	// Legacy requests may omit the SKU only when the sole default is resolvable.
	seedSupplierReturnLegacyVariant(t, db, productID)
	// The SKU-linked return lots retain their real source bin FK.
	if _, err := db.ExecContext(ctx, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf) VALUES('negative-bin','return-fixture','1','1') ON CONFLICT (bin_code) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,$3,'supplier','bin','bin:negative-bin','sellable',2,TRUE)`, "INV-"+suffix, productID, supplierID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if returnID != "" {
			_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_financial_events WHERE return_request_id=$1`, returnID)
			_, _ = db.ExecContext(ctx, `DELETE FROM ledger_entries WHERE related_order_or_dispute=$1`, returnID)
			_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_parcel_items WHERE parcel_id IN (SELECT id FROM supplier_return_parcels WHERE return_request_id=$1)`, returnID)
			_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_parcels WHERE return_request_id=$1`, returnID)
			_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_events WHERE return_request_id=$1`, returnID)
			_, _ = db.ExecContext(ctx, `DELETE FROM supplier_return_requests WHERE id=$1`, returnID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM inventory_movements WHERE product_id=$1`, productID)
		_, _ = db.ExecContext(ctx, `DELETE FROM inventory_reservations WHERE product_id=$1`, productID)
		_, _ = db.ExecContext(ctx, `DELETE FROM inventory_balances WHERE product_id=$1`, productID)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE id=$1`, productID)
		_, _ = db.ExecContext(ctx, `DELETE FROM suppliers WHERE id=$1`, supplierID)
	})

	repo := NewSupplierRepository(db)
	before, err := repo.GetSettlementBalance(ctx, supplierID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Available != 0 {
		t.Fatalf("initial available balance=%d, want 0", before.Available)
	}
	ret, err := repo.CreateReturnRequestWithIdempotency(ctx, supplierID, productID, 1, "supplier_collect", "negative balance test", "negative-return:"+suffix, "negative-test")
	if err != nil {
		t.Fatal(err)
	}
	returnID = ret.ID
	if err := repo.ReviewReturn(ctx, returnID, "approved", "approved", "negative-test"); err != nil {
		t.Fatal(err)
	}
	charge := int64(125000)
	parcel, err := repo.CreateReturnParcel(ctx, returnID, "negative-test", ReturnParcelInput{Quantity: 1, CarrierCode: "tipax", PaymentMethod: "sender_pays", Origin: "Tehran", Destination: "Qom", LengthMM: 100, WidthMM: 100, HeightMM: 100, WeightGrams: 500, FinalCharge: &charge, FinalChargeSource: "manual", FinalChargeRef: "negative-charge"}, "negative-parcel:"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmReturnParcelCharge(ctx, returnID, parcel.ID, "negative-test", "negative-charge:"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmReturnParcelCharge(ctx, returnID, parcel.ID, "negative-test", "negative-charge:"+suffix); err != nil {
		t.Fatal(err)
	}
	after, err := repo.GetSettlementBalance(ctx, supplierID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReturnCharges != charge || after.Available != -charge {
		t.Fatalf("after charge balance=%+v, want returnCharges=%d available=%d", after, charge, -charge)
	}
	if err := repo.ConfirmReturnParcelCharge(ctx, returnID, parcel.ID, "negative-test", "alternate-charge:"+suffix); err != nil {
		t.Fatal("same parcel replay under a new request key", err)
	}
	var identity string
	if err := db.QueryRowContext(ctx, `SELECT event_identity FROM supplier_return_financial_events WHERE parcel_id=$1`, parcel.ID).Scan(&identity); err != nil || identity != "return-parcel-charge:"+parcel.ID {
		t.Fatalf("parcel charge identity=%q error=%v", identity, err)
	}
	var financialCount, ledgerCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM supplier_return_financial_events WHERE return_request_id=$1`, returnID).Scan(&financialCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, returnID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if financialCount != 1 || ledgerCount != 1 {
		t.Fatalf("duplicate debit effects: financial=%d ledger=%d", financialCount, ledgerCount)
	}
}
