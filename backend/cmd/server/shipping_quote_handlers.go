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
	"net/http"
	"sort"
	"strings"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/routing"
	"github.com/drup/platform/internal/shippingquote"
)

// shippingRates are the admin tariff inputs for the quote formula.
// The type (and the pricing primitives below) live in
// internal/shippingquote so the customer-return shipping settlement
// prices with the SAME engine (migration 202610250202 policy).
type shippingRates = shippingquote.Rates

// loadShippingRates reads the admin tariff from platform_settings
// id = 1. A missing/unreadable row is shipping_unavailable, never a
// silent zero quote: a zero rate must be an admin choice, not a read
// failure.
func loadShippingRates(ctx context.Context, db *sql.DB) (shippingRates, error) {
	if db == nil {
		return shippingRates{}, fmt.Errorf("database not initialized")
	}
	return shippingquote.LoadRates(ctx, db)
}

// shippingQuoteLine is one cart line resolved against the catalog:
// everything the pure quote computation needs, nothing about HTTP.
type shippingQuoteLine struct {
	ProductID           string
	VariantID           string
	Quantity            int
	Fulfillment         string
	SupplierID          string
	ItemIndex           int
	WarehouseID         string
	SupplierWarehouseID string
	Warehouse           string
	WeightG             int // 0 = unknown, caller applies the default
	LengthMM            int
	WidthMM             int
	HeightMM            int
	// UnitPriceToman is the catalog unit price (products.original_price)
	// at resolution time. It is NOT part of the formula quote; the
	// Tapin price inquiry (tapin_quote.go) uses it as the declared
	// goods value. Both line builders (resolveQuoteLines and order
	// creation's item loop) fill it from the same product read, so a
	// quote and its order-time recompute feed the inquiry identically.
	UnitPriceToman int64
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
	PricingError  error                     `json:"-"`
	QuoteID       string                    `json:"quoteId"`
	Legs          []shippingQuoteLeg        `json:"legs"`
	TotalShipping int64                     `json:"totalShipping"`
	Allocations   []shippingQuoteAllocation `json:"allocations"`
}

func billableWeightGrams(line shippingQuoteLine, rates shippingRates) int64 {
	return shippingquote.BillableWeightGrams(line.WeightG, line.LengthMM, line.WidthMM, line.HeightMM, rates)
}

func legOrigin(line shippingQuoteLine) (key, label string) {
	if line.Fulfillment == "supplier_direct" {
		if line.SupplierWarehouseID != "" {
			return "supplier-warehouse:" + line.SupplierWarehouseID, line.Warehouse
		}
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
	if line.WarehouseID != "" {
		return "warehouse:" + line.WarehouseID, line.Warehouse
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
		if err := validateOrderItemQuantity(line.Quantity); err != nil {
			return shippingQuote{PricingError: err}
		}
		key, label := legOrigin(line)
		idx, ok := legOf[key]
		if !ok {
			idx = len(legs)
			legOf[key] = idx
			legs = append(legs, legAgg{key: key, label: label})
		}
		if int64(line.LengthMM) > pricing.MaxTransportMoney || int64(line.WidthMM) > pricing.MaxTransportMoney || int64(line.HeightMM) > pricing.MaxTransportMoney {
			return shippingQuote{PricingError: shippingquote.ErrInvalidShippingAmount}
		}
		unitWeight, err := shippingquote.CheckedBillableWeightGrams(line.WeightG, line.LengthMM, line.WidthMM, line.HeightMM, rates)
		if err != nil || unitWeight > pricing.MaxTransportMoney {
			return shippingQuote{PricingError: shippingquote.ErrInvalidShippingAmount}
		}
		weight, err := pricing.CheckedMultiply(unitWeight, int64(line.Quantity))
		if err != nil {
			return shippingQuote{PricingError: err}
		}
		legs[idx].weight, err = pricing.CheckedAdd(legs[idx].weight, weight)
		if err != nil || legs[idx].weight > pricing.MaxTransportMoney {
			return shippingQuote{PricingError: shippingquote.ErrInvalidShippingAmount}
		}
		allocs = append(allocs, shippingQuoteAllocation{
			ProductID: line.ProductID, VariantID: line.VariantID,
			Quantity: line.Quantity, LegIndex: idx,
		})
	}
	out := shippingQuote{Legs: []shippingQuoteLeg{}, Allocations: allocs}
	var total int64
	for i, lg := range legs {
		fare, err := shippingquote.CheckedLegFareToman(lg.weight, rates)
		if err != nil {
			return shippingQuote{PricingError: err}
		}
		if err = pricing.AccumulateMoney(&total, fare, 1); err != nil {
			return shippingQuote{PricingError: err}
		}
		out.Legs = append(out.Legs, shippingQuoteLeg{
			LegIndex: i, Index: i, OriginID: lg.key, OriginLabel: lg.label,
			Fare: fare, WeightGrams: lg.weight,
			BoxLabel: fmt.Sprintf("بسته %d", i+1),
			Source:   "weight_formula", Estimated: false,
		})
	}
	out.TotalShipping = total
	out.QuoteID = shippingQuoteIDFor(out.Legs, total, destination)
	return out
}

// shippingQuoteIDFor is the deterministic quote id over the priced
// quote + destination. computeShippingQuote uses it for formula
// quotes; the Tapin repricing step (tapin_quote.go) re-derives it
// after replacing leg fares, so a Tapin-priced quote and its
// order-time recompute hash identically.
func shippingQuoteIDFor(legs []shippingQuoteLeg, total int64, destination string) string {
	canonical, _ := json.Marshal(struct {
		Legs  []shippingQuoteLeg `json:"legs"`
		Total int64              `json:"totalShipping"`
		Dest  string             `json:"destination"`
	}{Legs: legs, Total: total, Dest: destination})
	sum := sha256.Sum256(canonical)
	return "sq_" + fmt.Sprintf("%x", sum)[:24]
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
//
// Gap 13: every line's Fulfillment is the RESOLVED routing from
// routing.ResolveItemRouting — the same resolver (and the same
// batched toggle load) order creation uses — so a quote groups its
// legs exactly as the order built from it will, and the order-time
// recomputation can only differ when the cart or the toggles
// actually changed.
func resolveQuoteLines(ctx context.Context, catalogRepo interface {
	GetProductByID(ctx context.Context, idOrSlug string) (*domain.Product, error)
}, supplierRepo *repository.SupplierRepository, items []shippingQuoteItemReq) ([]shippingQuoteLine, error) {
	products := make([]*domain.Product, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.ProductID) == "" || it.Quantity <= 0 {
			return nil, fmt.Errorf("invalid shipping quote")
		}
		p, err := catalogRepo.GetProductByID(ctx, it.ProductID)
		if err != nil || p == nil {
			return nil, fmt.Errorf("invalid shipping quote")
		}
		products = append(products, p)
	}
	// One batched toggle load for every distinct supplier in the
	// cart. A missing repo or a load error leaves the map empty,
	// which the resolver reads as "no methods enabled" (warehouse
	// fail-safe) — identical to order creation's methodsFor.
	methodsBySupplier := map[string]map[string]string{}
	if supplierRepo != nil {
		seen := map[string]bool{}
		var supplierIDs []string
		for _, prod := range products {
			if prod.SupplierID != "" && !seen[prod.SupplierID] {
				seen[prod.SupplierID] = true
				supplierIDs = append(supplierIDs, prod.SupplierID)
			}
		}
		if len(supplierIDs) > 0 {
			if batch, merr := supplierRepo.ListShippingMethodsBySupplierIDs(ctx, supplierIDs); merr == nil {
				methodsBySupplier = batch
			}
		}
	}
	// P0: one batched load of variant physicals for the whole quote.
	// A loader error / unknown variant keeps the product values — a
	// quote must never fail because a variant row is unreadable.
	variantPhysicals := map[string]domain.ProductVariant{}
	if supplierRepo != nil {
		var variantIDs []string
		for _, it := range items {
			if strings.TrimSpace(it.VariantID) != "" {
				variantIDs = append(variantIDs, it.VariantID)
			}
		}
		if len(variantIDs) > 0 {
			if phys, perr := supplierRepo.GetVariantPhysicalsByIDs(ctx, variantIDs); perr == nil {
				variantPhysicals = phys
			}
		}
	}
	lines := make([]shippingQuoteLine, 0, len(items))
	for i, it := range items {
		prod := products[i]
		itemRouting := routing.ResolveItemRouting(prod.FulfillmentMethod, methodsBySupplier[prod.SupplierID])
		// ISO-PURCHASE: a supplier-direct line with no supplier has no
		// fulfillment origin to quote from. Fail the quote instead of
		// pricing it under a substituted supplier's leg (the old
		// legOrigin fallback) or under the central warehouse. The
		// check keys on the RESOLVED routing, not the raw flag.
		if itemRouting == "supplier_direct" && strings.TrimSpace(prod.SupplierID) == "" {
			return nil, fmt.Errorf("invalid shipping quote")
		}
		line := shippingQuoteLine{
			ProductID: it.ProductID, VariantID: it.VariantID, Quantity: it.Quantity,
			Fulfillment: itemRouting, SupplierID: prod.SupplierID, Warehouse: prod.WarehouseName,
			UnitPriceToman: prod.OriginalPrice,
		}
		if prod.WeightGrams != nil {
			line.WeightG = *prod.WeightGrams
		}
		if prod.LengthMM != nil {
			line.LengthMM = *prod.LengthMM
		}
		if prod.WidthMM != nil {
			line.WidthMM = *prod.WidthMM
		}
		if prod.HeightMM != nil {
			line.HeightMM = *prod.HeightMM
		}
		// P0: variant physicals override the product's — weight field
		// by field, dims as an all-or-none triple (validation
		// guarantees a stored triple is complete; a partial triple
		// falls back wholesale to the product dims). M2/F3: the row
		// is applied ONLY when it belongs to this line's product —
		// a cross-product variant ID keeps the product values (the
		// spec's "row belongs to another product: ignore" rule).
		// The order-time recompute needs no such check: it loads the
		// variant via GetProductVariants(p.ID), which is scoped by
		// construction.
		if phys, ok := variantPhysicals[it.VariantID]; ok && phys.ProductID == it.ProductID {
			if phys.WeightGrams != nil {
				line.WeightG = *phys.WeightGrams
			}
			if phys.LengthMM != nil && phys.WidthMM != nil && phys.HeightMM != nil {
				line.LengthMM, line.WidthMM, line.HeightMM = *phys.LengthMM, *phys.WidthMM, *phys.HeightMM
			}
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
}, supplierRepo *repository.SupplierRepository, db *sql.DB) {
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
	lines, err := resolveQuoteLines(ctx, catalogRepo, supplierRepo, req.Items)
	if err != nil {
		http.Error(w, `{"error":"invalid shipping quote"}`, http.StatusBadRequest)
		return
	}
	lines, err = resolveQuoteOrigins(ctx, db, lines)
	if err != nil {
		http.Error(w, `{"error":"inventory_origin_unavailable"}`, http.StatusConflict)
		return
	}
	sortQuoteLines(lines)
	quote := computeShippingQuote(lines, rates, quoteDestinationKey(*req.Destination))
	if quote.PricingError != nil {
		http.Error(w, `{"error":"shipping_amount_out_of_range"}`, http.StatusBadRequest)
		return
	}
	// Tapin-connected supplier legs are priced by Tapin's live price
	// inquiry, not the formula (owner directive, gaps 1-3). The step
	// fails closed: an inquiry error fails the whole quote with the
	// upstream message — a connected leg is never silently repriced
	// by the formula.
	if err := priceTapinQuoteLegs(ctx, db, &quote, lines, *req.Destination); err != nil {
		if writeDependencyOverload(w, err) {
			return
		}
		http.Error(w, tapinQuoteErrorBody(err), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true, "quoteId": quote.QuoteID, "legs": quote.Legs,
		"totalShipping": quote.TotalShipping, "allocations": quote.Allocations,
	})
}

func resolveQuoteOrigins(ctx context.Context, db *sql.DB, lines []shippingQuoteLine) ([]shippingQuoteLine, error) {
	totals := map[string]int{}
	for _, line := range lines {
		key := line.ProductID + "/" + line.VariantID
		if line.Quantity <= 0 || totals[key] > int(^uint(0)>>1)-line.Quantity {
			return nil, repository.ErrInsufficientInventory
		}
		totals[key] += line.Quantity
	}
	plans := map[string][]repository.FulfillmentOrigin{}
	for _, line := range lines {
		key := line.ProductID + "/" + line.VariantID
		if _, ok := plans[key]; ok {
			continue
		}
		origins, err := repository.ResolveInventoryOrigins(ctx, db, line.ProductID, line.VariantID, line.Fulfillment, totals[key])
		if err != nil {
			return nil, err
		}
		plans[key] = origins
	}
	var result []shippingQuoteLine
	for i, line := range lines {
		key := line.ProductID + "/" + line.VariantID
		remaining := line.Quantity
		plan := plans[key]
		for j := range plan {
			if remaining == 0 {
				break
			}
			if plan[j].Quantity == 0 {
				continue
			}
			q := plan[j].Quantity
			if q > remaining {
				q = remaining
			}
			split := line
			split.ItemIndex = i
			split.Quantity = q
			split.WarehouseID = plan[j].WarehouseID
			split.SupplierWarehouseID = plan[j].SupplierWarehouseID
			split.Warehouse = plan[j].Label
			result = append(result, split)
			plan[j].Quantity -= q
			remaining -= q
		}
		plans[key] = plan
	}
	// The supplier shipment model/provider registration currently has one
	// parcel per supplier. Reject unsupported multiple pickup origins before
	// payment, rather than pricing several pickups then choosing one later.
	pickupBySupplier := map[string][]repository.SupplierOrderLine{}
	for _, line := range result {
		if line.Fulfillment == "supplier_direct" {
			pickupBySupplier[line.SupplierID] = append(pickupBySupplier[line.SupplierID], repository.SupplierOrderLine{OriginWarehouseID: line.SupplierWarehouseID, Quantity: line.Quantity})
		}
	}
	for _, lines := range pickupBySupplier {
		if _, err := repository.SingleSupplierPickupOrigin(lines); err != nil {
			return nil, err
		}
	}
	return result, nil
}
