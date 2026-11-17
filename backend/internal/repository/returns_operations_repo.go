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
	if err = lockSupplierReturnSKUTx(ctx, tx, returnID); err != nil {
		return nil, err
	}
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
