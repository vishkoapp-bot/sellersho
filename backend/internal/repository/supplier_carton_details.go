package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type SupplierCartonUnit struct {
	Code       string `json:"code"`
	PrintCount int    `json:"print_count"`
}

type SupplierCartonLine struct {
	ID           string               `json:"id,omitempty"`
	VariantID    string               `json:"variant_id"`
	VariantName  string               `json:"variant_name"`
	ProductTitle string               `json:"product_title"`
	SPNC         string               `json:"spnc"`
	SourceKind   string               `json:"source_kind,omitempty"`
	ExpectedQty  int                  `json:"expected_qty"`
	ReceivedQty  int                  `json:"received_qty"`
	PutawayQty   *int                 `json:"putaway_qty"`
	CancelledQty int                  `json:"cancelled_qty"`
	QCStatus     string               `json:"qc_status"`
	QCPassedQty  *int                 `json:"qc_passed_qty"`
	QCFailedQty  *int                 `json:"qc_failed_qty"`
	QCShortage   *int                 `json:"qc_shortage"`
	QCReason     string               `json:"qc_reason,omitempty"`
	Units        []SupplierCartonUnit `json:"units"`
}

type SupplierCartonDetails struct {
	CartonShipment
	WarehouseName string               `json:"warehouse_name,omitempty"`
	ReceivedAt    *time.Time           `json:"received_at"`
	Lines         []SupplierCartonLine `json:"lines"`
}

// This predicate is shared by the supplier's preview and the print job. An
// exact putaway event is only usable while the unit still occupies its recorded
// bin and the latest putaway still points to this receipt. Reservations and
// quarantine use the current exact receipt lot directly.
const supplierCartonUnitScope = `
	JOIN inventory_balances b ON b.id=u.balance_id AND b.supplier_id=l.supplier_id AND b.owner_type='supplier'
		AND b.product_id=l.product_id AND b.variant_id=u.variant_id AND b.warehouse_id=l.warehouse_id
	WHERE u.product_id=l.product_id AND u.variant_id::text=l.variant_id
	AND u.` + printableStatusesSQL + `
	AND (
		(b.location_key='inbound-line:'||l.id AND b.status IN ('inbound_reserved','in_transit','quarantine'))
		OR EXISTS (
			SELECT 1 FROM inbound_putaway_allocations a
			WHERE a.receipt_line_id=l.id AND a.shipment_id=l.shipment_id
				AND a.supplier_id=l.supplier_id AND a.variant_id=l.variant_id
				AND b.status='qc_accepted' AND a.source_location_key=b.location_key
		)
		OR (b.location_type='bin' AND b.status IN ('sellable','sales_reserved') AND EXISTS (
			SELECT 1 FROM inbound_putaway_allocations a
			JOIN inventory_movements m ON m.idempotency_key='putaway:'||a.task_id
				AND m.to_location_key=b.location_key
			WHERE a.receipt_line_id=l.id AND a.shipment_id=l.shipment_id
				AND a.supplier_id=l.supplier_id AND a.variant_id=l.variant_id
				AND a.task_id=(
					SELECT e.reference_id FROM product_unit_events e
					WHERE e.unit_id=u.id AND e.reference_type='putaway_confirmed'
					ORDER BY e.created_at DESC,e.id DESC LIMIT 1
				)
		))
	)`

func (r *SupplierRepository) GetSupplierCartonDetails(ctx context.Context, supplierID, shipmentID string) (*SupplierCartonDetails, error) {
	id, err := uuid.Parse(shipmentID)
	if err != nil || supplierID == "" {
		return nil, sql.ErrNoRows
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &SupplierCartonDetails{Lines: []SupplierCartonLine{}}
	err = tx.QueryRowContext(ctx, `
		SELECT s.id,s.shipment_code,s.status,s.created_at,s.received_at,COALESCE(w.name,'')
		FROM shipments s LEFT JOIN warehouses w ON w.id=s.warehouse_id
		WHERE s.id=$1 AND s.supplier_id=$2`, id, supplierID).
		Scan(&out.ID, &out.ShipmentCode, &out.Status, &out.CreatedAt, &out.ReceivedAt, &out.WarehouseName)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `
		WITH legacy_putaway AS MATERIALIZED (
			SELECT t.product_id,t.variant_id FROM putaway_tasks t
			WHERE t.inbound_id=$1::uuid::text AND t.status='completed'
				AND NOT EXISTS (SELECT 1 FROM inbound_putaway_allocations a WHERE a.task_id=t.id)
		)
		SELECT COALESCE(l.id,''),v.id,COALESCE(v.variant_name,''),COALESCE(p.title,''),
			COALESCE(v.spnc,''),COALESCE(l.source_kind,''),i.expected_qty,
			COALESCE(l.received_quantity,i.received_qty),i.qc_status,COALESCE(l.cancelled_quantity,0),
			CASE WHEN l.id IS NOT NULL AND NOT EXISTS (
				SELECT 1 FROM legacy_putaway old WHERE old.product_id=p.id AND (old.variant_id IS NULL OR old.variant_id=v.id)
			) THEN putaway.quantity END,
			qc.quantity_passed,qc.quantity_failed,qc.quantity_shortage,COALESCE(qc.reason,'')
		FROM shipment_items i
		JOIN product_variants v ON v.id=i.variant_id JOIN products p ON p.id=v.product_id
		LEFT JOIN inbound_inventory_lines l ON l.shipment_id=i.shipment_id::text
			AND l.supplier_id=$2 AND l.product_id=p.id AND l.variant_id=v.id::text
		LEFT JOIN LATERAL (
			SELECT d.quantity_passed,d.quantity_failed,d.quantity_shortage,d.reason
			FROM inbound_qc_dispositions d WHERE d.receipt_line_id=l.id
			ORDER BY d.cumulative_disposed DESC LIMIT 1
		) qc ON TRUE
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(a.quantity),0) AS quantity FROM inbound_putaway_allocations a
			JOIN putaway_tasks t ON t.id=a.task_id AND t.status='completed'
			JOIN inventory_movements m ON m.idempotency_key='putaway:'||a.task_id
				AND m.reference_type='putaway_confirmed' AND m.reference_id=a.task_id
				AND m.product_id=a.product_id AND m.supplier_id=a.supplier_id
				AND m.variant_id::text=a.variant_id AND m.quantity=a.quantity
				AND m.from_location_key=a.source_location_key
				AND m.from_status='qc_accepted' AND m.to_status='sellable'
			WHERE a.receipt_line_id=l.id AND a.shipment_id=l.shipment_id AND a.supplier_id=l.supplier_id
				AND a.product_id=l.product_id AND a.variant_id=l.variant_id AND a.warehouse_id=l.warehouse_id
		) putaway ON TRUE
		WHERE i.shipment_id=$1 AND p.supplier_id=$2
		ORDER BY p.title,v.variant_name,v.id`, id, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byLine := map[string]int{}
	for rows.Next() {
		line := SupplierCartonLine{Units: []SupplierCartonUnit{}}
		if err := rows.Scan(&line.ID, &line.VariantID, &line.VariantName, &line.ProductTitle, &line.SPNC,
			&line.SourceKind, &line.ExpectedQty, &line.ReceivedQty, &line.QCStatus, &line.CancelledQty,
			&line.PutawayQty, &line.QCPassedQty, &line.QCFailedQty, &line.QCShortage, &line.QCReason); err != nil {
			return nil, err
		}
		if line.ID != "" {
			byLine[line.ID] = len(out.Lines)
		}
		out.Lines = append(out.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	out.ItemCount = len(out.Lines)
	if len(byLine) > 0 && out.Status != "cancelled" {
		unitRows, err := tx.QueryContext(ctx, `
			SELECT l.id,u.code,u.print_count
			FROM inbound_inventory_lines l JOIN product_units u ON u.variant_id::text=l.variant_id`+
			supplierCartonUnitScope+` AND l.shipment_id=$1 AND l.supplier_id=$2
			ORDER BY l.id,u.sequence`, out.ID, supplierID)
		if err != nil {
			return nil, err
		}
		defer unitRows.Close()
		for unitRows.Next() {
			var lineID string
			var u SupplierCartonUnit
			if err := unitRows.Scan(&lineID, &u.Code, &u.PrintCount); err != nil {
				return nil, err
			}
			if i, ok := byLine[lineID]; ok {
				out.Lines[i].Units = append(out.Lines[i].Units, u)
			}
		}
		if err := unitRows.Err(); err != nil {
			return nil, err
		}
		unitRows.Close()
	}
	return out, tx.Commit()
}

func resolveSupplierCartonPrintLine(ctx context.Context, tx *sql.Tx, req *MarkUnitsPrintedRequest) error {
	id, err := uuid.Parse(req.CartonShipmentID)
	if err != nil {
		return ErrPrintReceiptLineNotFound
	}
	err = tx.QueryRowContext(ctx, `
		SELECT l.product_id,v.id FROM shipments s
		JOIN shipment_items i ON i.shipment_id=s.id
		JOIN product_variants v ON v.id=i.variant_id
		JOIN inbound_inventory_lines l ON l.shipment_id=s.id::text AND l.supplier_id=s.supplier_id
			AND l.product_id=v.product_id AND l.variant_id=v.id::text
		WHERE s.id=$1 AND s.supplier_id=$2 AND s.status<>'cancelled' AND l.id=$3`,
		id, req.SupplierID, *req.ReceiptLineID).Scan(&req.ProductID, &req.VariantID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPrintReceiptLineNotFound
	}
	req.CartonShipmentID = id.String()
	return err
}

func validateSupplierCartonPrintUnits(ctx context.Context, tx *sql.Tx, req MarkUnitsPrintedRequest) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT u.code FROM inbound_inventory_lines l
		JOIN shipments s ON s.id::text=l.shipment_id AND s.supplier_id=l.supplier_id
		JOIN product_units u ON u.code=ANY($1)`+supplierCartonUnitScope+`
		AND l.id=$2 AND l.supplier_id=$3 AND l.shipment_id=$4 AND s.status<>'cancelled'`,
		pq.Array(req.UnitCodes), *req.ReceiptLineID, req.SupplierID, req.CartonShipmentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	visible := make(map[string]bool, len(req.UnitCodes))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return err
		}
		visible[code] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var missing []string
	for _, code := range req.UnitCodes {
		if !visible[code] {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		return missingPrintUnits(missing)
	}
	return nil
}
