package main

// FIX 5 (full live parity, Team 03 G1/G2/G12): pre-checkout shipping
// quote, live route POST /api/v1/shipping/quote.
//
// Live evidence (read-only, 2026-11-03):
//   - Live frontend posts {items, destination} (destination optional)
//     to /shipping/quote (under /api/v1) while the checkout step is
//     active, with a 450ms debounce, and sends shippingQuoteId with
//     the order when a quote exists. The live binary contains exactly
//     one /api/v1/shipping/quote string and one totalShipping string.
//   - The response carries legs, totalShipping and allocations. A leg
//     is one shipment per origin: legIndex, weightGrams, boxLabel; the
//     order-detail form of a leg is {index, originLabel, fare, source,
//     estimated}. Both key spellings are emitted so both consumers
//     read the same quote.
//   - The live admin tariff text states the server formula: base rate
//     + (per-kilo rate x billable-weight ceiling in kilograms), where
//     a line's billable weight is max(actual weight, volumetric
//     weight), and an item with no recorded weight defaults to 500g.
//   - Live quote error codes surfaced to the client:
//     shipping_quote_changed, shipping_unavailable,
//     shipping_address_required (plus the invalid-quote and
//     store-unavailable messages in the live error tables).
//
// This is NOT the warehouse carrier tariff: /api/v1/warehouse/
// carriers/quote (warehouse roles, carrier/size-class tariff tables)
// exists in live too and is left untouched.
//
// Units (documented assumption, see report): platform_settings
// shipping_base_rate / shipping_per_kg are stored in Rials (migration
// 202610160003 comment), while every other candidate money amount is
// in Tomans, so a leg fare computed in Rials is converted to Tomans
// (rounded half-up, /10) before it is returned or added to an order.
// If the admin rates turn out to be Tomans, the quote will read 10x
// too low; that is the one number a user screenshot of a live quote
// against the live admin rates would settle.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/drup/platform/internal/domain"
)

// shippingRates are the admin tariff inputs for the quote formula.
type shippingRates struct {
	BaseRateRial   int64
	PerKgRial      int64
	VolumetricDiv  int64
	DefaultWeightG int64
}

// loadShippingRates reads the admin tariff from platform_settings
// id = 1. A missing/unreadable row is shipping_unavailable, never a
// silent zero quote: a zero rate must be an admin choice, not a read
// failure.
func loadShippingRates(ctx context.Context, db *sql.DB) (shippingRates, error) {
	var r shippingRates
	var div, def sql.NullInt64
	if db == nil {
		return r, fmt.Errorf("database not initialized")
	}
	err := db.QueryRowContext(ctx, `SELECT COALESCE(shipping_base_rate, 0),
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

// shippingQuoteLine is one cart line resolved against the catalog:
// everything the pure quote computation needs, nothing about HTTP.
type shippingQuoteLine struct {
	ProductID   string
	VariantID   string
	Quantity    int
	Fulfillment string
	SupplierID  string
	Warehouse   string
	WeightG     int // 0 = unknown, caller applies the default
	LengthMM    int
	WidthMM     int
	HeightMM    int
}

// shippingQuoteLeg is one shipment-per-origin leg of a quote.
type shippingQuoteLeg struct {
	LegIndex    int    `json:"legIndex"`
	Index       int    `json:"index"`
	OriginID    string `json:"originId"`
	OriginLabel string `json:"originLabel"`
	Fare        int64  `json:"fare"`
	WeightGrams int64  `json:"weightGrams"`
	BoxLabel    string `json:"boxLabel"`
	Source      string `json:"source"`
	Estimated   bool   `json:"estimated"`
}

// shippingQuoteAllocation links one cart line to its leg.
type shippingQuoteAllocation struct {
	ProductID string `json:"productId"`
	VariantID string `json:"variantId,omitempty"`
	Quantity  int    `json:"quantity"`
	LegIndex  int    `json:"legIndex"`
}

// shippingQuote is the computed quote: deterministic quoteId, legs,
// allocations and the total in Tomans.
type shippingQuote struct {
	QuoteID       string                    `json:"quoteId"`
	Legs          []shippingQuoteLeg        `json:"legs"`
	TotalShipping int64                     `json:"totalShipping"`
	Allocations   []shippingQuoteAllocation `json:"allocations"`
}

func billableWeightGrams(line shippingQuoteLine, rates shippingRates) int64 {
	actual := int64(line.WeightG)
	if actual <= 0 {
		actual = rates.DefaultWeightG
	}
	billable := actual
	if rates.VolumetricDiv > 0 && line.LengthMM > 0 && line.WidthMM > 0 && line.HeightMM > 0 {
		// Volumetric grams = volume(mm^3) / divisor: volume in cm^3
		// is mm^3/1000 and kg is cm^3/divisor, so grams is mm^3/divisor.
		vol := (int64(line.LengthMM) * int64(line.WidthMM) * int64(line.HeightMM)) / rates.VolumetricDiv
		if vol > billable {
			billable = vol
		}
	}
	return billable
}

func legOrigin(line shippingQuoteLine) (key, label string) {
	if line.Fulfillment == "supplier_direct" {
		// ISO-PURCHASE: never substitute a real supplier for a missing
		// one. Callers that resolve from the catalog (resolveQuoteLines,
		// order-time quote rebuild) reject a supplier-less direct line
		// before it reaches here; this branch only keeps an unresolved
		// line honestly grouped instead of attributing it to technolife.
		sup := line.SupplierID
		if sup == "" {
			sup = "unknown"
		}
		if strings.TrimSpace(line.Warehouse) != "" {
			return "supplier:" + sup, line.Warehouse
		}
		return "supplier:" + sup, "ارسال مستقیم تأمین‌کننده"
	}
	if strings.TrimSpace(line.Warehouse) != "" {
		return "warehouse", line.Warehouse
	}
	return "warehouse", "انبار مرکزی"
}

// computeShippingQuote groups lines into one leg per origin and
// prices each leg with the admin formula: base + per-kg x ceil of
// the leg's total billable kilograms. The quoteId is a deterministic
// hash of the priced quote, so an order can re-derive it and reject
// a stale quote with shipping_quote_changed instead of trusting a
// client-supplied amount.
func computeShippingQuote(lines []shippingQuoteLine, rates shippingRates, destination string) shippingQuote {
	type legAgg struct {
		key, label string
		weight     int64
	}
	legs := []legAgg{}
	legOf := map[string]int{}
	allocs := []shippingQuoteAllocation{}
	for _, line := range lines {
		if line.Quantity <= 0 {
			continue
		}
		key, label := legOrigin(line)
		idx, ok := legOf[key]
		if !ok {
			idx = len(legs)
			legOf[key] = idx
			legs = append(legs, legAgg{key: key, label: label})
		}
		legs[idx].weight += billableWeightGrams(line, rates) * int64(line.Quantity)
		allocs = append(allocs, shippingQuoteAllocation{
			ProductID: line.ProductID, VariantID: line.VariantID,
			Quantity: line.Quantity, LegIndex: idx,
		})
	}
	out := shippingQuote{Legs: []shippingQuoteLeg{}, Allocations: allocs}
	var total int64
	for i, lg := range legs {
		billableKg := int64(math.Ceil(float64(lg.weight) / 1000.0))
		fareRial := rates.BaseRateRial + rates.PerKgRial*billableKg
		// Rials -> Tomans, rounded half-up.
		fare := (fareRial + 5) / 10
		if fare < 0 {
			fare = 0
		}
		total += fare
		out.Legs = append(out.Legs, shippingQuoteLeg{
			LegIndex: i, Index: i, OriginID: lg.key, OriginLabel: lg.label,
			Fare: fare, WeightGrams: lg.weight,
			BoxLabel: fmt.Sprintf("بسته %d", i+1),
			Source:   "weight_formula", Estimated: false,
		})
	}
	out.TotalShipping = total
	// Deterministic quote id over the priced quote + destination.
	canonical, _ := json.Marshal(struct {
		Legs  []shippingQuoteLeg `json:"legs"`
		Total int64              `json:"totalShipping"`
		Dest  string             `json:"destination"`
	}{Legs: out.Legs, Total: total, Dest: destination})
	sum := sha256.Sum256(canonical)
	out.QuoteID = "sq_" + fmt.Sprintf("%x", sum)[:24]
	return out
}

// shippingQuoteItemReq decodes a quote/order line tolerantly: live
// clients send camelCase, but snake_case is accepted too.
type shippingQuoteItemReq struct {
	ProductID string
	VariantID string
	Quantity  int
}

func (q *shippingQuoteItemReq) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				var s string
				if json.Unmarshal(v, &s) == nil {
					return s
				}
			}
		}
		return ""
	}
	num := func(keys ...string) int {
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				var n int
				if json.Unmarshal(v, &n) == nil {
					return n
				}
			}
		}
		return 0
	}
	q.ProductID = str("productId", "product_id")
	q.VariantID = str("variantId", "variant_id")
	q.Quantity = num("quantity", "qty")
	return nil
}

type shippingQuoteDestination struct {
	Province   string `json:"province"`
	City       string `json:"city"`
	Address    string `json:"address"`
	PostalCode string `json:"postalCode"`
}

func quoteDestinationKey(d shippingQuoteDestination) string {
	return strings.Join([]string{
		strings.TrimSpace(d.Province), strings.TrimSpace(d.City),
		strings.TrimSpace(d.Address), strings.TrimSpace(d.PostalCode),
	}, "|")
}

// resolveQuoteLines turns request lines into catalog-resolved lines.
// Unknown products are an invalid quote, not a zero-weight guess.
func resolveQuoteLines(ctx context.Context, catalogRepo interface {
	GetProductByID(ctx context.Context, idOrSlug string) (*domain.Product, error)
}, items []shippingQuoteItemReq) ([]shippingQuoteLine, error) {
	lines := make([]shippingQuoteLine, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.ProductID) == "" || it.Quantity <= 0 {
			return nil, fmt.Errorf("invalid shipping quote")
		}
		p, err := catalogRepo.GetProductByID(ctx, it.ProductID)
		if err != nil || p == nil {
			return nil, fmt.Errorf("invalid shipping quote")
		}
		// ISO-PURCHASE: a supplier-direct line with no supplier has no
		// fulfillment origin to quote from. Fail the quote instead of
		// pricing it under a substituted supplier's leg (the old
		// legOrigin fallback) or under the central warehouse.
		if p.FulfillmentMethod == "supplier_direct" && strings.TrimSpace(p.SupplierID) == "" {
			return nil, fmt.Errorf("invalid shipping quote")
		}
		line := shippingQuoteLine{
			ProductID: it.ProductID, VariantID: it.VariantID, Quantity: it.Quantity,
			Fulfillment: p.FulfillmentMethod, SupplierID: p.SupplierID, Warehouse: p.WarehouseName,
		}
		if p.WeightGrams != nil {
			line.WeightG = *p.WeightGrams
		}
		if p.LengthMM != nil {
			line.LengthMM = *p.LengthMM
		}
		if p.WidthMM != nil {
			line.WidthMM = *p.WidthMM
		}
		if p.HeightMM != nil {
			line.HeightMM = *p.HeightMM
		}
		lines = append(lines, line)
	}
	// Deterministic leg order (by origin key), so a quote recomputed
	// at order time yields the same quoteId as the quoted one.
	sortQuoteLines(lines)
	return lines, nil
}

// sortQuoteLines orders quote lines by origin key. The quote endpoint
// and the order-time revalidation must share this order, or the same
// cart would hash to two different quoteIds.
func sortQuoteLines(lines []shippingQuoteLine) {
	sort.SliceStable(lines, func(i, j int) bool {
		ki, _ := legOrigin(lines[i])
		kj, _ := legOrigin(lines[j])
		return ki < kj
	})
}

func handleShippingQuote(w http.ResponseWriter, r *http.Request, ctx context.Context, catalogRepo interface {
	GetProductByID(ctx context.Context, idOrSlug string) (*domain.Product, error)
}, db *sql.DB) {
	w.Header().Set("Content-Type", "application/json")
	if catalogRepo == nil || db == nil {
		http.Error(w, `{"error":"shipping_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var req struct {
		Items       []shippingQuoteItemReq    `json:"items"`
		Destination *shippingQuoteDestination `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Items) == 0 {
		http.Error(w, `{"error":"invalid shipping quote"}`, http.StatusBadRequest)
		return
	}
	// Live requires a complete destination for a quote; an incomplete
	// one is shipping_address_required, not a quote for a guessed zone.
	if req.Destination == nil || strings.TrimSpace(req.Destination.Province) == "" ||
		strings.TrimSpace(req.Destination.City) == "" || strings.TrimSpace(req.Destination.Address) == "" {
		http.Error(w, `{"error":"shipping_address_required"}`, http.StatusBadRequest)
		return
	}
	rates, err := loadShippingRates(ctx, db)
	if err != nil {
		http.Error(w, `{"error":"shipping_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	lines, err := resolveQuoteLines(ctx, catalogRepo, req.Items)
	if err != nil {
		http.Error(w, `{"error":"invalid shipping quote"}`, http.StatusBadRequest)
		return
	}
	quote := computeShippingQuote(lines, rates, quoteDestinationKey(*req.Destination))
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true, "quoteId": quote.QuoteID, "legs": quote.Legs,
		"totalShipping": quote.TotalShipping, "allocations": quote.Allocations,
	})
}
