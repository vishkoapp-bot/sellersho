package repository

import (
	"context"
	"database/sql"
)

type SupplierInventoryLocation struct {
	ProductID           string `json:"productId"`
	ProductTitle        string `json:"productTitle"`
	VariantID           string `json:"variantId,omitempty"`
	VariantName         string `json:"variantName,omitempty"`
	VariantSPNC         string `json:"variantSpnc,omitempty"`
	Allocation          string `json:"allocation"`
	OwnerType           string `json:"ownerType"`
	LocationType        string `json:"locationType"`
	LocationKey         string `json:"locationKey"`
	SupplierWarehouseID string `json:"supplierWarehouseId,omitempty"`
	WarehouseName       string `json:"warehouseName,omitempty"`
	BinCode             string `json:"binCode,omitempty"`
	Status              string `json:"status"`
	Quantity            int    `json:"quantity"`
	Reconciled          bool   `json:"reconciled"`
}

type CarrierOption struct {
	Code                 string `json:"code"`
	DisplayName          string `json:"displayName"`
	SupportsSenderPays   bool   `json:"supportsSenderPays"`
	SupportsReceiverPays bool   `json:"supportsReceiverPays"`
	SupportsQuoteAPI     bool   `json:"supportsQuoteApi"`
	TariffVersion        int    `json:"tariffVersion"`
}

type ReturnFinancialEvent struct {
	ID              string `json:"id"`
	ReturnRequestID string `json:"returnRequestId"`
	ParcelID        string `json:"parcelId,omitempty"`
	Operation       string `json:"operation"`
	Amount          int64  `json:"amount"`
	PaymentMethod   string `json:"paymentMethod"`
	LedgerEntryID   string `json:"ledgerEntryId,omitempty"`
	Reason          string `json:"reason"`
	ActorUserID     string `json:"actorUserId"`
	CreatedAt       string `json:"createdAt"`
}

func (r *SupplierRepository) GetSupplierInventory(ctx context.Context, supplierID string) ([]SupplierInventoryLocation, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ib.product_id, p.title, ib.owner_type, ib.location_type, ib.location_key,
		       COALESCE(ib.warehouse_name,''), COALESCE(ib.bin_code,''), ib.status, ib.quantity, ib.reconciled,
		       COALESCE(ib.variant_id::text,''),COALESCE(pv.variant_name,''),COALESCE(pv.spnc,''),CASE WHEN ib.variant_id IS NULL THEN 'unallocated' ELSE 'sku' END, COALESCE(ib.supplier_warehouse_id,'')
		FROM inventory_balances ib JOIN products p ON p.id=ib.product_id
		LEFT JOIN product_variants pv ON pv.id=ib.variant_id
		WHERE ib.supplier_id=$1
		UNION ALL
		SELECT p.id, p.title, 'unreconciled', 'unknown', 'legacy:' || p.id, COALESCE(p.warehouse_name,''), '', 'unreconciled', p.stock, FALSE,'','','','unallocated',''
		FROM products p
		WHERE p.supplier_id=$1 AND p.schema_version<2 AND NOT EXISTS (SELECT 1 FROM inventory_balances ib WHERE ib.product_id=p.id AND ib.supplier_id=$1)
		ORDER BY 2, 8, 5`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SupplierInventoryLocation{}
	for rows.Next() {
		var item SupplierInventoryLocation
		if err := rows.Scan(&item.ProductID, &item.ProductTitle, &item.OwnerType, &item.LocationType, &item.LocationKey, &item.WarehouseName, &item.BinCode, &item.Status, &item.Quantity, &item.Reconciled, &item.VariantID, &item.VariantName, &item.VariantSPNC, &item.Allocation, &item.SupplierWarehouseID); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *SupplierRepository) GetReturnParcels(ctx context.Context, returnID string) ([]ReturnParcel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.return_request_id,p.parcel_number,p.carrier_code,p.payment_method,p.origin,p.destination,p.length_mm,p.width_mm,p.height_mm,p.weight_grams,p.estimated_charge,COALESCE(p.estimate_source,''),COALESCE(p.estimate_reference,''),p.final_charge,COALESCE(p.final_charge_source,''),COALESCE(p.final_charge_reference,''),p.status,COALESCE(p.tracking_number,''),p.created_by,COALESCE(p.confirmed_by,''),p.created_at FROM supplier_return_parcels p JOIN supplier_return_requests rr ON rr.id=p.return_request_id WHERE p.return_request_id=$1 ORDER BY p.parcel_number`, returnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ReturnParcel{}
	for rows.Next() {
		var item ReturnParcel
		if err := rows.Scan(&item.ID, &item.ReturnRequestID, &item.ParcelNumber, &item.CarrierCode, &item.PaymentMethod, &item.Origin, &item.Destination, &item.LengthMM, &item.WidthMM, &item.HeightMM, &item.WeightGrams, &item.EstimatedCharge, &item.EstimateSource, &item.EstimateReference, &item.FinalCharge, &item.FinalChargeSource, &item.FinalChargeRef, &item.Status, &item.TrackingNumber, &item.CreatedBy, &item.ConfirmedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *SupplierRepository) GetReturnFinancialEvents(ctx context.Context, returnID string) ([]ReturnFinancialEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,return_request_id,COALESCE(parcel_id,''),operation,amount,payment_method,COALESCE(ledger_entry_id,''),reason,actor_user_id,created_at::text FROM supplier_return_financial_events WHERE return_request_id=$1 ORDER BY created_at`, returnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ReturnFinancialEvent{}
	for rows.Next() {
		var item ReturnFinancialEvent
		if err := rows.Scan(&item.ID, &item.ReturnRequestID, &item.ParcelID, &item.Operation, &item.Amount, &item.PaymentMethod, &item.LedgerEntryID, &item.Reason, &item.ActorUserID, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *SupplierRepository) GetCarrierOptions(ctx context.Context) ([]CarrierOption, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT code,display_name,supports_sender_pays,supports_receiver_pays,supports_quote_api,tariff_version FROM carrier_configs WHERE enabled ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CarrierOption{}
	for rows.Next() {
		var item CarrierOption
		if err := rows.Scan(&item.Code, &item.DisplayName, &item.SupportsSenderPays, &item.SupportsReceiverPays, &item.SupportsQuoteAPI, &item.TariffVersion); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

var _ *sql.DB
