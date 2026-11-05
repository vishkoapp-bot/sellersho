// Package shippingquote holds the single implementation of the admin
// shipping tariff: rates loading, billable-weight computation and the
// per-leg fare formula.
//
// It exists so the outbound quote (cmd/server) and the customer-return
// shipping settlement (internal/repository, migration 202610250202)
// price with the SAME engine, as the Oct 25 2026 returns policy
// requires ("the return shipping amount is computed with the SAME
// engine as the outbound fare"). Grouping lines into legs stays with
// each caller — outbound groups by catalog fulfillment, returns group
// by the persisted order_item_origins — but the pricing primitives
// below are the only implementation of the formula.
//
// Units: platform_settings shipping_base_rate / shipping_per_kg are
// stored in Rials; a leg fare computed in Rials is converted to
// Tomans (rounded half-up, /10), exactly like the outbound quote.
package shippingquote

import (
	"context"
	"database/sql"
	"fmt"
	"math"
)

// Rates are the admin tariff inputs for the quote formula.
type Rates struct {
	BaseRateRial   int64
	PerKgRial      int64
	VolumetricDiv  int64
	DefaultWeightG int64
}

// Querier is satisfied by both *sql.DB and *sql.Tx, so rates can be
// loaded inside a caller's transaction.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// LoadRates reads the admin tariff from platform_settings id = 1. A
// missing/unreadable row is an error, never a silent zero quote: a
// zero rate must be an admin choice, not a read failure.
func LoadRates(ctx context.Context, q Querier) (Rates, error) {
	var r Rates
	if q == nil {
		return r, fmt.Errorf("database not initialized")
	}
	var div, def sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT COALESCE(shipping_base_rate, 0),
		COALESCE(shipping_per_kg, 0), volumetric_divisor, default_item_weight_grams
		FROM platform_settings ORDER BY id LIMIT 1`).Scan(
		&r.BaseRateRial, &r.PerKgRial, &div, &def)
	if err != nil {
		return r, err
	}
	if div.Valid {
		r.VolumetricDiv = div.Int64
	} else {
		r.VolumetricDiv = 5000
	}
	if def.Valid && def.Int64 > 0 {
		r.DefaultWeightG = def.Int64
	} else {
		r.DefaultWeightG = 500
	}
	return r, nil
}

// BillableWeightGrams is one unit's billable weight: max(actual
// weight, volumetric weight), where an unknown actual weight falls
// back to the platform default (500g unless the admin set one).
func BillableWeightGrams(weightG, lengthMM, widthMM, heightMM int, rates Rates) int64 {
	actual := int64(weightG)
	if actual <= 0 {
		actual = rates.DefaultWeightG
	}
	billable := actual
	if rates.VolumetricDiv > 0 && lengthMM > 0 && widthMM > 0 && heightMM > 0 {
		// Volumetric grams = volume(mm^3) / divisor: volume in cm^3
		// is mm^3/1000 and kg is cm^3/divisor, so grams is mm^3/divisor.
		vol := (int64(lengthMM) * int64(widthMM) * int64(heightMM)) / rates.VolumetricDiv
		if vol > billable {
			billable = vol
		}
	}
	return billable
}

// LegFareToman prices one leg: base rate + per-kilo rate x ceil of
// the leg's total billable kilograms, converted Rials -> Tomans
// (rounded half-up) and clamped at zero.
func LegFareToman(totalBillableGrams int64, rates Rates) int64 {
	billableKg := int64(math.Ceil(float64(totalBillableGrams) / 1000.0))
	fareRial := rates.BaseRateRial + rates.PerKgRial*billableKg
	// Rials -> Tomans, rounded half-up.
	fare := (fareRial + 5) / 10
	if fare < 0 {
		fare = 0
	}
	return fare
}
