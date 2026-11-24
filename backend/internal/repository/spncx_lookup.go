package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SPNCX lookup backend (§7.1): staff-only single-unit lookup + staff list.
// Unit rows never enter any public serializer — this file is the only read
// path for unit detail, and it is reachable only from the staff-only
// /api/v1/units handlers. No public repo function references product_units,
// product_unit_events or order_item_units.

var (
	ErrUnitNotFound      = errors.New("unit_not_found")
	ErrUnitInvalidLimit  = errors.New("invalid_limit")
	ErrUnitInvalidCursor = errors.New("invalid_cursor")
)

// UnitLookupVariant is the variant block of the §7.1 lookup response.
type UnitLookupVariant struct {
	ID   string `json:"id"`
	SPNC string `json:"spnc"`
	Name string `json:"name"`
}

// UnitLookupProduct is the product block of the §7.1 lookup response.
type UnitLookupProduct struct {
	ID    string `json:"id"`
	SPN   string `json:"spn"`
	Title string `json:"title"`
}

// UnitLookupLocation is the unit's current lot. It is nil for removed units
// (a removed unit holds no lot by the §1.2 CHECK constraint).
type UnitLookupLocation struct {
	BalanceID     string  `json:"balance_id"`
	LocationKey   string  `json:"location_key"`
	WarehouseName *string `json:"warehouse_name"`
	BinCode       *string `json:"bin_code"`
	LotStatus     string  `json:"lot_status"`
}

// UnitLookupPrinted is the print state block of the §7.1 lookup response.
type UnitLookupPrinted struct {
	PrintedAt     *time.Time `json:"printed_at"`
	PrintCount    int        `json:"print_count"`
	LastPrintedAt *time.Time `json:"last_printed_at"`
}

// UnitLookupOrder is the order link, populated only when the unit is sold or
// reserved for an order (from order_item_units).
type UnitLookupOrder struct {
	OrderID     string     `json:"order_id"`
	OrderNumber string     `json:"order_number"`
	CapturedAt  time.Time  `json:"captured_at"`
	RestoredAt  *time.Time `json:"restored_at"`
}

// UnitLookupEvent is one product_unit_events row, as returned in history.
type UnitLookupEvent struct {
	EventType     string    `json:"event_type"`
	FromStatus    *string   `json:"from_status"`
	ToStatus      *string   `json:"to_status"`
	ReferenceType string    `json:"reference_type"`
	ReferenceID   *string   `json:"reference_id"`
	ActorUserID   *string   `json:"actor_user_id"`
	CreatedAt     time.Time `json:"created_at"`
}

// UnitLookupResponse is the exact §7.1 GET /api/v1/units/{code} body.
type UnitLookupResponse struct {
	Code     string              `json:"code"`
	Status   string              `json:"status"`
	Variant  UnitLookupVariant   `json:"variant"`
	Product  UnitLookupProduct   `json:"product"`
	Location *UnitLookupLocation `json:"location"`
	Printed  UnitLookupPrinted   `json:"printed"`
	Order    *UnitLookupOrder    `json:"order"`
	History  []UnitLookupEvent   `json:"history"`
}

// GetUnitByCode resolves one unit by its business code. supplierID scopes the
// lookup to that supplier's products; an empty supplierID means unscoped
// (admin / warehouse). A unit outside the caller's scope is indistinguishable
// from an unknown code: both return ErrUnitNotFound.
func GetUnitByCode(ctx context.Context, db *sql.DB, supplierID, code string) (UnitLookupResponse, error) {
	out := UnitLookupResponse{History: []UnitLookupEvent{}}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	var supplier any
	if supplierID != "" {
		supplier = supplierID
	}
	var unitID string
	var balanceID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.code,u.status,
		v.id,COALESCE(v.spnc,''),COALESCE(v.variant_name,''),
		u.product_id,COALESCE(p.spn,''),COALESCE(p.title,''),
		u.balance_id,u.printed_at,u.print_count,u.last_printed_at
		FROM product_units u
		JOIN product_variants v ON v.id=u.variant_id
		JOIN products p ON p.id=u.product_id
		WHERE u.code=$1 AND ($2::text IS NULL OR p.supplier_id=$2)`,
		code, supplier).Scan(&unitID, &out.Code, &out.Status,
		&out.Variant.ID, &out.Variant.SPNC, &out.Variant.Name,
		&out.Product.ID, &out.Product.SPN, &out.Product.Title,
		&balanceID, &out.Printed.PrintedAt, &out.Printed.PrintCount, &out.Printed.LastPrintedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrUnitNotFound
	}
	if err != nil {
		return out, err
	}

	if balanceID.Valid {
		loc := UnitLookupLocation{BalanceID: balanceID.String}
		err = tx.QueryRowContext(ctx, `SELECT location_key,warehouse_name,bin_code,status
			FROM inventory_balances WHERE id=$1`, balanceID.String).
			Scan(&loc.LocationKey, &loc.WarehouseName, &loc.BinCode, &loc.LotStatus)
		if err != nil {
			return out, err
		}
		out.Location = &loc
	}

	rows, err := tx.QueryContext(ctx, `SELECT event_type,from_status,to_status,reference_type,
		reference_id,actor_user_id,created_at FROM product_unit_events
		WHERE unit_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100`, unitID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var event UnitLookupEvent
		if err := rows.Scan(&event.EventType, &event.FromStatus, &event.ToStatus,
			&event.ReferenceType, &event.ReferenceID, &event.ActorUserID, &event.CreatedAt); err != nil {
			return out, err
		}
		out.History = append(out.History, event)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}

	// The order link exists only for a unit currently held by an order. A
	// restored unit's old capture row (restored_at set) must not surface
	// once the unit is back in stock, so the link is read only for
	// sold/reserved units. When several capture rows exist for the unit
	// (sold → restored → sold again), the latest capture wins.
	if out.Status == "sold" || out.Status == "reserved" {
		var link UnitLookupOrder
		err = tx.QueryRowContext(ctx, `SELECT oiu.order_id,COALESCE(o.order_number,''),
			oiu.captured_at,oiu.restored_at FROM order_item_units oiu
			LEFT JOIN orders o ON o.id=oiu.order_id
			WHERE oiu.unit_id=$1 ORDER BY oiu.captured_at DESC LIMIT 1`, unitID).
			Scan(&link.OrderID, &link.OrderNumber, &link.CapturedAt, &link.RestoredAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if err == nil {
			out.Order = &link
		}
	}
	return out, tx.Commit()
}

// ListUnitsOptions carries the §7.1 GET /api/v1/units filters.
type ListUnitsOptions struct {
	VariantID   string
	Status      string
	Q           string
	OrderID     string
	BalanceID   string
	OrderItemID string
	Limit       int
	Cursor      string
}

// ListUnitsResponse is the exact §7.1 GET /api/v1/units body: unit summaries
// as in §6.2 (UnitLabelRow) plus an opaque cursor.
type ListUnitsResponse struct {
	Units      []UnitLabelRow `json:"units"`
	NextCursor *string        `json:"next_cursor"`
}

// escapeUnitCodePrefix escapes LIKE wildcards so a q prefix matches literally.
func escapeUnitCodePrefix(prefix string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(prefix)
}

// ListUnits lists units with the §7.1 filters (combined with AND). supplierID
// scopes to that supplier's products; empty means unscoped (admin /
// warehouse). Unknown filter values return an empty list, never an error:
// an unknown variant/order/balance/order-item id, an unparsable UUID filter,
// or an unknown status value all yield {"units": [], "next_cursor": null}.
func ListUnits(ctx context.Context, db *sql.DB, supplierID string, opts ListUnitsOptions) (ListUnitsResponse, error) {
	out := ListUnitsResponse{Units: []UnitLabelRow{}}
	if opts.Limit < 0 {
		return out, ErrUnitInvalidLimit
	}
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	opts.Limit = min(opts.Limit, 200)
	if opts.Status != "" && !validUnitLabelStatus(opts.Status) {
		return out, nil
	}

	var variant any
	if opts.VariantID != "" {
		id, err := uuid.Parse(opts.VariantID)
		if err != nil {
			return out, nil
		}
		variant = id
	}
	var orderItem any
	if opts.OrderItemID != "" {
		id, err := uuid.Parse(opts.OrderItemID)
		if err != nil {
			return out, nil
		}
		orderItem = id
	}

	var cursor unitLabelCursor
	var cursorVariant any
	if opts.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(opts.Cursor)
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.CreatedAt.IsZero() ||
			cursor.Sequence < 1 || cursor.Sequence > unitSequenceCap {
			return out, ErrUnitInvalidCursor
		}
		id, err := uuid.Parse(cursor.VariantID)
		if err != nil {
			return out, ErrUnitInvalidCursor
		}
		cursorVariant = id
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	var supplier, status, q, orderID, balanceID any
	if supplierID != "" {
		supplier = supplierID
	}
	if opts.Status != "" {
		status = opts.Status
	}
	if opts.Q != "" {
		q = escapeUnitCodePrefix(opts.Q) + "%"
	}
	if opts.OrderID != "" {
		orderID = opts.OrderID
	}
	if opts.BalanceID != "" {
		balanceID = opts.BalanceID
	}
	args := []any{supplier, variant, status, q, orderID, balanceID, orderItem, cursorVariant,
		cursor.CreatedAt, cursor.Sequence, opts.Limit + 1}
	rows, err := tx.QueryContext(ctx, `SELECT u.code,COALESCE(v.spnc,''),v.id,COALESCE(v.variant_name,''),
		u.product_id,COALESCE(p.spn,''),COALESCE(p.title,''),u.status,u.printed_at,u.print_count,
		u.last_printed_at,v.created_at,u.sequence
		FROM product_units u
		JOIN product_variants v ON v.id=u.variant_id
		JOIN products p ON p.id=u.product_id
		WHERE ($1::text IS NULL OR p.supplier_id=$1)
		AND ($2::uuid IS NULL OR u.variant_id=$2)
		AND ($3::text IS NULL OR u.status=$3)
		AND ($4::text IS NULL OR u.code LIKE $4 ESCAPE '\')
		AND ($5::text IS NULL OR EXISTS (SELECT 1 FROM order_item_units oiu WHERE oiu.unit_id=u.id AND oiu.order_id=$5))
		AND ($6::text IS NULL OR u.balance_id=$6)
		AND ($7::uuid IS NULL OR EXISTS (SELECT 1 FROM order_item_units oiu WHERE oiu.unit_id=u.id AND oiu.order_item_id=$7))
		AND ($8::uuid IS NULL OR (v.created_at,v.id,u.sequence)>($9,$8,$10))
		ORDER BY v.created_at,v.id,u.sequence LIMIT $11`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Units) == opts.Limit {
			data, err := json.Marshal(cursor)
			if err != nil {
				return out, err
			}
			next := base64.RawURLEncoding.EncodeToString(data)
			out.NextCursor = &next
			break
		}
		u := UnitLabelRow{}
		if err := rows.Scan(&u.Code, &u.SPNC, &u.VariantID, &u.VariantName,
			&u.ProductID, &u.SPN, &u.Title, &u.Status, &u.PrintedAt, &u.PrintCount,
			&u.LastPrintedAt, &cursor.CreatedAt, &cursor.Sequence); err != nil {
			return out, err
		}
		cursor.VariantID = u.VariantID
		out.Units = append(out.Units, u)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	return out, tx.Commit()
}
