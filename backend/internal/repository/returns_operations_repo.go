package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ReturnParcel struct {
	ID                string    `json:"id"`
	ReturnRequestID   string    `json:"returnRequestId"`
	ParcelNumber      int       `json:"parcelNumber"`
	CarrierCode       string    `json:"carrierCode"`
	PaymentMethod     string    `json:"paymentMethod"`
	Origin            string    `json:"origin"`
	Destination       string    `json:"destination"`
	LengthMM          int       `json:"lengthMm"`
	WidthMM           int       `json:"widthMm"`
	HeightMM          int       `json:"heightMm"`
	WeightGrams       int       `json:"weightGrams"`
	EstimatedCharge   *int64    `json:"estimatedCharge,omitempty"`
	EstimateSource    string    `json:"estimateSource,omitempty"`
	EstimateReference string    `json:"estimateReference,omitempty"`
	FinalCharge       *int64    `json:"finalCharge,omitempty"`
	FinalChargeSource string    `json:"finalChargeSource,omitempty"`
	FinalChargeRef    string    `json:"finalChargeReference,omitempty"`
	Status            string    `json:"status"`
	TrackingNumber    string    `json:"trackingNumber,omitempty"`
	CreatedBy         string    `json:"createdBy"`
	ConfirmedBy       string    `json:"confirmedBy,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
}

type ReturnParcelInput struct {
	Quantity          int    `json:"quantity"`
	CarrierCode       string `json:"carrierCode"`
	PaymentMethod     string `json:"paymentMethod"`
	Origin            string `json:"origin"`
	Destination       string `json:"destination"`
	LengthMM          int    `json:"lengthMm"`
	WidthMM           int    `json:"widthMm"`
	HeightMM          int    `json:"heightMm"`
	WeightGrams       int    `json:"weightGrams"`
	EstimatedCharge   *int64 `json:"estimatedCharge"`
	EstimateSource    string `json:"estimateSource"`
	EstimateReference string `json:"estimateReference"`
	FinalCharge       *int64 `json:"finalCharge"`
	FinalChargeSource string `json:"finalChargeSource"`
	FinalChargeRef    string `json:"finalChargeReference"`
	TrackingNumber    string `json:"trackingNumber"`
	Notes             string `json:"notes"`
}

type CarrierTariffInput struct {
	CarrierCode       string `json:"carrierCode"`
	OriginRegion      string `json:"originRegion"`
	DestinationRegion string `json:"destinationRegion"`
	SizeClass         string `json:"sizeClass"`
	WeightGrams       int    `json:"weightGrams"`
}

type CarrierTariffResult struct {
	CarrierCode     string `json:"carrierCode"`
	Source          string `json:"source"`
	TariffVersion   int    `json:"tariffVersion"`
	BaseWeightGrams int    `json:"baseWeightGrams"`
	Charge          int64  `json:"charge"`
	RoundingRule    string `json:"roundingRule"`
}

// CarrierAdapter is the internal contract for a real carrier integration. An
// adapter must return an explicit unsupported error instead of fabricating data.
type CarrierAdapter interface {
	Quote(ctx context.Context, parcel ReturnParcelInput) (CarrierTariffResult, error)
	SupportedPaymentMethods(ctx context.Context, carrierCode string) ([]string, error)
	Book(ctx context.Context, parcel ReturnParcelInput) (trackingNumber string, carrierReference string, err error)
	Track(ctx context.Context, trackingNumber string) (status string, err error)
}

func CalculateCarrierTariff(ctx context.Context, db *sql.DB, input CarrierTariffInput) (*CarrierTariffResult, error) {
	if input.WeightGrams <= 0 || strings.TrimSpace(input.CarrierCode) == "" || strings.TrimSpace(input.SizeClass) == "" {
		return nil, fmt.Errorf("carrier, size class and positive weight are required")
	}
	var result CarrierTariffResult
	var extraPerKG int64
	if err := db.QueryRowContext(ctx, `
		SELECT ct.version,ct.base_weight_grams,ct.base_charge,ct.extra_kg_charge
		FROM carrier_tariffs ct
		JOIN carrier_configs cc ON cc.code=ct.carrier_code AND cc.enabled
		WHERE ct.carrier_code=$1 AND ct.origin_region=$2 AND ct.destination_region=$3
		  AND ct.size_class=$4 AND ct.valid_from <= NOW()
		  AND (ct.valid_until IS NULL OR ct.valid_until > NOW())
		ORDER BY ct.version DESC LIMIT 1`, input.CarrierCode, input.OriginRegion, input.DestinationRegion, input.SizeClass).Scan(
		&result.TariffVersion, &result.BaseWeightGrams, &result.Charge, &extraPerKG); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("active tariff not configured")
		}
		return nil, err
	}
	if input.WeightGrams > result.BaseWeightGrams {
		extraKG := int64(math.Ceil(float64(input.WeightGrams-result.BaseWeightGrams) / 1000))
		result.Charge += extraKG * extraPerKG
	}
	result.CarrierCode = input.CarrierCode
	result.Source = "tariff"
	result.RoundingRule = "extra kilograms rounded up"
	return &result, nil
}

func (r *SupplierRepository) CreateReturnParcel(ctx context.Context, returnID, actorUserID string, input ReturnParcelInput, idempotencyKey string) (*ReturnParcel, error) {
	if input.PaymentMethod != "sender_pays" && input.PaymentMethod != "receiver_pays" {
		return nil, fmt.Errorf("unsupported payment method")
	}
	if input.LengthMM <= 0 || input.WidthMM <= 0 || input.HeightMM <= 0 || input.WeightGrams <= 0 || strings.TrimSpace(input.Origin) == "" || strings.TrimSpace(input.Destination) == "" {
		return nil, fmt.Errorf("complete positive parcel measurements and route are required")
	}
	if input.Quantity <= 0 {
		return nil, fmt.Errorf("positive parcel quantity is required")
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, fmt.Errorf("idempotency key required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existing ReturnParcel
	if err := tx.QueryRowContext(ctx, `SELECT id,return_request_id,parcel_number,carrier_code,payment_method,origin,destination,length_mm,width_mm,height_mm,weight_grams,estimated_charge,COALESCE(estimate_source,''),COALESCE(estimate_reference,''),final_charge,COALESCE(final_charge_source,''),COALESCE(final_charge_reference,''),status,COALESCE(tracking_number,''),created_by,COALESCE(confirmed_by,''),created_at FROM supplier_return_parcels WHERE idempotency_key=$1`, idempotencyKey).Scan(&existing.ID, &existing.ReturnRequestID, &existing.ParcelNumber, &existing.CarrierCode, &existing.PaymentMethod, &existing.Origin, &existing.Destination, &existing.LengthMM, &existing.WidthMM, &existing.HeightMM, &existing.WeightGrams, &existing.EstimatedCharge, &existing.EstimateSource, &existing.EstimateReference, &existing.FinalCharge, &existing.FinalChargeSource, &existing.FinalChargeRef, &existing.Status, &existing.TrackingNumber, &existing.CreatedBy, &existing.ConfirmedBy, &existing.CreatedAt); err == nil {
		return &existing, tx.Commit()
	}
	var owner, returnStatus, productID string
	var requestedQuantity int
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id,status,product_id,requested_quantity FROM supplier_return_requests WHERE id=$1 FOR UPDATE`, returnID).Scan(&owner, &returnStatus, &productID, &requestedQuantity); err != nil {
		return nil, err
	}
	if returnStatus != "approved" && returnStatus != "packed" {
		return nil, fmt.Errorf("return must be approved before parcel preparation")
	}
	if input.PaymentMethod == "sender_pays" {
		var supported bool
		if err := tx.QueryRowContext(ctx, `SELECT supports_sender_pays FROM carrier_configs WHERE code=$1 AND enabled`, input.CarrierCode).Scan(&supported); err != nil {
			return nil, err
		}
		if !supported {
			return nil, fmt.Errorf("carrier does not support sender-paid returns")
		}
	} else {
		var supported bool
		if err := tx.QueryRowContext(ctx, `SELECT supports_receiver_pays FROM carrier_configs WHERE code=$1 AND enabled`, input.CarrierCode).Scan(&supported); err != nil {
			return nil, err
		}
		if !supported {
			return nil, fmt.Errorf("carrier does not support receiver-paid returns")
		}
	}
	var allocated int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(pri.quantity),0) FROM supplier_return_parcels p JOIN supplier_return_parcel_items pri ON pri.parcel_id=p.id WHERE p.return_request_id=$1 AND p.status <> 'cancelled'`, returnID).Scan(&allocated); err != nil {
		return nil, err
	}
	if allocated+input.Quantity > requestedQuantity {
		return nil, fmt.Errorf("parcel quantity exceeds return quantity")
	}
	var parcelNumber int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(parcel_number),0)+1 FROM supplier_return_parcels WHERE return_request_id=$1`, returnID).Scan(&parcelNumber); err != nil {
		return nil, err
	}
	id := "PAR-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_return_parcels (id,return_request_id,parcel_number,carrier_code,payment_method,origin,destination,length_mm,width_mm,height_mm,weight_grams,estimated_charge,estimate_source,estimate_reference,final_charge,final_charge_source,final_charge_reference,status,tracking_number,notes,created_by,idempotency_key) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''),NULLIF($14,''),$15,NULLIF($16,''),NULLIF($17,''),'draft',NULLIF($18,''),$19,$20,$21)`, id, returnID, parcelNumber, input.CarrierCode, input.PaymentMethod, input.Origin, input.Destination, input.LengthMM, input.WidthMM, input.HeightMM, input.WeightGrams, input.EstimatedCharge, input.EstimateSource, input.EstimateReference, input.FinalCharge, input.FinalChargeSource, input.FinalChargeRef, input.TrackingNumber, input.Notes, actorUserID, idempotencyKey); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_return_parcel_items (parcel_id,product_id,quantity) VALUES ($1,$2,$3)`, id, productID, input.Quantity); err != nil {
		return nil, err
	}
	if returnStatus == "approved" {
		if _, err := tx.ExecContext(ctx, `UPDATE supplier_return_requests SET status='packed',updated_at=NOW() WHERE id=$1 AND status='approved'`, returnID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ReturnParcel{ID: id, ReturnRequestID: returnID, ParcelNumber: parcelNumber, CarrierCode: input.CarrierCode, PaymentMethod: input.PaymentMethod, Origin: input.Origin, Destination: input.Destination, LengthMM: input.LengthMM, WidthMM: input.WidthMM, HeightMM: input.HeightMM, WeightGrams: input.WeightGrams, EstimatedCharge: input.EstimatedCharge, EstimateSource: input.EstimateSource, EstimateReference: input.EstimateReference, FinalCharge: input.FinalCharge, FinalChargeSource: input.FinalChargeSource, FinalChargeRef: input.FinalChargeRef, Status: "draft", TrackingNumber: input.TrackingNumber, CreatedBy: actorUserID}, nil
}

// ConfirmReturnParcelCharge records the carrier charge and supplier debit in
// one transaction. It intentionally permits a negative supplier balance.
func (r *SupplierRepository) ConfirmReturnParcelCharge(ctx context.Context, returnID, parcelID, actorUserID, idempotencyKey string) error {
	if strings.TrimSpace(idempotencyKey) == "" {
		return fmt.Errorf("idempotency key required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var chargeSupplierID string
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id FROM supplier_return_requests WHERE id=$1`, returnID).Scan(&chargeSupplierID); err != nil {
		return err
	}
	var accountLocks TransactionLockSet
	if err := LockAccountsTx(ctx, tx, &accountLocks, FinancialAccount{Type: "supplier", ID: chargeSupplierID}); err != nil {
		return err
	}
	bounded, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	if err := LockTransactionRowsTx(bounded, tx, &accountLocks, TransactionRowLock{Table: "supplier_return_requests", ID: returnID}, TransactionRowLock{Table: "supplier_return_parcels", ID: parcelID}); err != nil {
		return err
	}
	var amount int64
	var method, supplierID string
	var parcelStatus string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(final_charge,0),payment_method,status FROM supplier_return_parcels WHERE id=$1 AND return_request_id=$2 FOR UPDATE`, parcelID, returnID).Scan(&amount, &method, &parcelStatus); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id FROM supplier_return_requests WHERE id=$1 FOR UPDATE`, returnID).Scan(&supplierID); err != nil {
		return err
	}
	var oldParcel, oldMethod string
	var oldAmount int64
	duplicateErr := tx.QueryRowContext(ctx, `SELECT parcel_id,payment_method,amount FROM supplier_return_financial_events WHERE idempotency_key=$1 OR (parcel_id=$2 AND operation='debit') LIMIT 1`, idempotencyKey, parcelID).Scan(&oldParcel, &oldMethod, &oldAmount)
	if duplicateErr == nil {
		if oldParcel != parcelID || oldMethod != method || oldAmount != amount {
			return ErrFinanceCommandConflict
		}
		return tx.Commit()
	}
	if duplicateErr != sql.ErrNoRows {
		return duplicateErr
	}
	if method == "receiver_pays" {
		if _, err := tx.ExecContext(ctx, `UPDATE supplier_return_parcels SET status='confirmed',confirmed_by=$1,updated_at=NOW() WHERE id=$2 AND status IN ('draft','confirmed')`, actorUserID, parcelID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if method != "sender_pays" || amount <= 0 {
		return fmt.Errorf("sender-paid parcel requires a positive final carrier charge")
	}
	now := time.Now()
	ledgerID := "RET-LED-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries (id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES ($1,$2,$3,'supplier','debit',$4,$5,$6,$7)`, ledgerID, now, supplierID, amount, fmt.Sprintf("هزینه حمل مرجوعی %s / مرسوله %s", returnID, parcelID), actorUserID, returnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_return_financial_events (id,return_request_id,parcel_id,supplier_id,operation,amount,payment_method,ledger_entry_id,idempotency_key,reason,actor_user_id,event_identity) VALUES ($1,$2,$3,$4,'debit',$5,$6,$7,$8,$9,$10,$11)`, "RET-FIN-"+uuid.NewString(), returnID, parcelID, supplierID, amount, method, ledgerID, idempotencyKey, fmt.Sprintf("carrier charge for %s", parcelID), actorUserID, "return-parcel-charge:"+parcelID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_return_parcels SET status='confirmed',confirmed_by=$1,updated_at=$2 WHERE id=$3 AND status IN ('draft','confirmed')`, actorUserID, now, parcelID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// P1-8 (best-effort): sender-paid parcel charge confirmed - supplier
	// (amount debited). Receiver-pays confirmations carry no debit and
	// return earlier, so they notify nothing here.
	notifySupplierParcelCharge(ctx, r.db, returnID, parcelID, supplierID, amount)
	return nil
}

func (r *SupplierRepository) RecordProductMeasurement(ctx context.Context, productID, actorUserID string, lengthMM, widthMM, heightMM, weightGrams int, reason string) error {
	_, err := r.RecordSKUPhysicalMeasurement(ctx, productID, "", actorUserID, lengthMM, widthMM, heightMM, weightGrams, reason)
	return err
}

func nullInt(v sql.NullInt64) interface{} {
	if v.Valid {
		return int(v.Int64)
	}
	return nil
}
