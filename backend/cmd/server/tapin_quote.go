package main

// Tapin leg pricing for the shipping quote (owner directive,
// 2026-11-05: «هزینه ارسال ها از تاپین خونده بشه»).
//
// Rule: a supplier-direct leg whose supplier has connected Tapin
// (stored shop id + token) is priced by Tapin's LIVE price inquiry
// (internal/tapin TipaxCheckPrice — the only inquiry contract
// Tapin's official guide publishes; capture file in
// ~/workspace/tapin-spec) using the supplier's OWN credentials.
// Every other leg — unconnected suppliers, and the platform /
// central warehouse leg — keeps the existing weight-formula price,
// untouched.
//
// FAIL-CLOSED (deliberate, recorded): when a connected supplier's
// inquiry answers with an envelope failure or cannot be reached,
// the quote FAILS with the upstream Persian message surfaced. The
// formula price is never silently substituted for a Tapin-connected
// leg. The same applies when the leg cannot even be inquired
// (origin warehouse has no Tapin geo ids, or the destination is not
// in the stored Tapin geography): the quote fails with an explicit
// Persian message instead of guessing a price.
//
// ONE EXCEPTION (critic fix F4, 2026-11-05): envelope 30129 — the
// Tipax service is not activated for the supplier's shop (their
// activation ticket is pending in the Tapin panel). Tapin cannot
// price the leg AT ALL in that state, so the leg keeps its formula
// fare (Source stays "weight_formula") instead of killing the
// supplier's checkout until activation lands. Every other envelope
// error (token, shop, geo, ...) still fails closed as above.
//
// PARITY: both quote call sites — the quote endpoint
// (handleShippingQuote) and order creation's recompute
// (handleCreateOrder) — run this step over lines built from the same
// product reads, and the step re-derives the quoteId over the final
// legs, so a Tapin-priced quote re-derives identically at order
// time (no shipping_quote_changed divergence). A short in-memory
// cache (2 minutes) additionally pins a leg's fare across the
// checkout -> order window; it is keyed by every input the fare
// depends on, and only successful fares are cached.
//
// Named origin legs use their selected supplier warehouse geography in both
// quote paths. Origin-less historical/unit constructions retain the old default
// lookup; production planning requires a named eligible origin.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/drup/platform/internal/dependencybudget"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/shippingquote"
	"github.com/drup/platform/internal/tapin"
)

// tapinAPIBaseURL is the single upstream host override point; tests
// point it at an httptest server.
var tapinAPIBaseURL = tapin.DefaultBaseURL

func newTapinClient() *tapin.Client {
	return tapin.NewClient(tapinAPIBaseURL)
}

// errTapinServiceNotActivated marks the 30129 envelope outcome
// inside the quote step: priceTapinQuoteLegs treats it as "keep the
// leg's formula fare" rather than a failure (see the header's F4
// note). It never reaches a response body.
var errTapinServiceNotActivated = errors.New("tapin: service not activated for shop (30129)")

// tapinQuoteError is a user-facing quote failure: its message is
// shown to the customer verbatim (an upstream Tapin message, or one
// of the explicit Persian explanations below).
type tapinQuoteError struct{ Message string }

func (e *tapinQuoteError) Error() string { return e.Message }

const (
	tapinMsgUnreachable    = "برقراری ارتباط با تاپین ممکن نشد. دوباره تلاش کنید."
	tapinMsgOriginUnmapped = "مختصات تاپین انبار تأمین‌کننده ثبت نشده است؛ هزینه ارسال این سفارش از تاپین قابل استعلام نیست."
	tapinMsgDestUnmapped   = "استان/شهر مقصد در داده‌های جغرافیایی تاپین یافت نشد؛ هزینه ارسال قابل استعلام نیست."
)

// tapinQuoteErrorBody renders the quote failure in the quote
// endpoints' {"error": ...} shape, flagged so clients can tell a
// Tapin verdict from a local validation error.
func tapinQuoteErrorBody(err error) string {
	msg := tapinMsgUnreachable
	var qe *tapinQuoteError
	if errors.As(err, &qe) {
		msg = qe.Message
	}
	body, _ := json.Marshal(map[string]interface{}{"error": msg, "tapinError": true})
	return string(body)
}

// --- fare cache -----------------------------------------------------

type tapinFareCacheEntry struct {
	fareToman int64
	expires   time.Time
}

var tapinFareCache = struct {
	sync.Mutex
	entries map[string]tapinFareCacheEntry
}{entries: map[string]tapinFareCacheEntry{}}

const tapinFareCacheTTL = 2 * time.Minute

func tapinFareCacheGet(key string) (int64, bool) {
	tapinFareCache.Lock()
	defer tapinFareCache.Unlock()
	e, ok := tapinFareCache.entries[key]
	if !ok || time.Now().After(e.expires) {
		return 0, false
	}
	return e.fareToman, true
}

func tapinFareCachePut(key string, fareToman int64) {
	tapinFareCache.Lock()
	defer tapinFareCache.Unlock()
	tapinFareCache.entries[key] = tapinFareCacheEntry{fareToman: fareToman, expires: time.Now().Add(tapinFareCacheTTL)}
}

// tapinLineWeightGrams is the goods weight fed to the inquiry: the
// recorded weight, or the platform's 500g fallback for an unweighed
// item (the same fallback the formula quote documents).
func tapinLineWeightGrams(line shippingQuoteLine) int {
	if line.WeightG > 0 {
		return line.WeightG
	}
	return 500
}

// rialToToman converts Rials to Tomans rounded half-up — the same
// conversion shippingquote.LegFareToman applies to formula fares.
func rialToToman(rial int64) int64 {
	if rial < 0 {
		return 0
	}
	fare := rial / 10
	if rial%10 >= 5 {
		fare++
	}
	return fare
}

func checkedTapinGoods(lines []shippingQuoteLine) ([]tapin.TipaxProduct, int64, int64, error) {
	products := make([]tapin.TipaxProduct, 0, len(lines))
	var weight, value int64
	for _, line := range lines {
		if err := validateOrderItemQuantity(line.Quantity); err != nil || line.WeightG < 0 {
			return nil, 0, 0, pricing.ErrInvalidMoney
		}
		w := tapinLineWeightGrams(line)
		partWeight, err := pricing.CheckedMultiply(int64(w), int64(line.Quantity))
		if err != nil {
			return nil, 0, 0, err
		}
		weight, err = pricing.CheckedAdd(weight, partWeight)
		if err != nil || weight > pricing.MaxTransportMoney {
			return nil, 0, 0, pricing.ErrInvalidMoney
		}
		// Tapin's existing adapter contract uses integer rials. Convert once here.
		amount, err := pricing.CheckedMultiply(line.UnitPriceToman, 10)
		if err != nil {
			return nil, 0, 0, err
		}
		partValue, err := pricing.CheckedMultiply(amount, int64(line.Quantity))
		if err != nil {
			return nil, 0, 0, err
		}
		value, err = pricing.CheckedAdd(value, partValue)
		if err != nil {
			return nil, 0, 0, err
		}
		products = append(products, tapin.TipaxProduct{AmountPerCount: amount, DiscountPerCount: 0, WeightPerCount: w, Count: line.Quantity})
	}
	return products, weight, value, nil
}

// priceTapinQuoteLegs reprices the connected suppliers' legs of an
// already-computed formula quote, in place, and re-derives the
// total + quoteId when at least one leg changed. See the file
// header for the fail-closed and parity rules.
//
// tapinRepo must carry the Tapin token cipher (SEC-01): it is built
// by tapinRepositoryFor from the supplier repo's cipher, so stored
// "enc:v1:" tokens decrypt on read. A nil repo means "no Tapin
// pricing" (same as the old nil-db case) and leaves the formula
// quote untouched.
// tapinRepositoryFor builds the Tapin repository for the quote step,
// carrying the supplier Tapin-token cipher when the supplier repo has
// one (SEC-01). A nil db yields a nil repo, which priceTapinQuoteLegs
// treats as "no Tapin pricing".
func tapinRepositoryFor(db *sql.DB, supplierRepo *repository.SupplierRepository) *repository.TapinRepository {
	if db == nil {
		return nil
	}
	repo := repository.NewTapinRepository(db)
	if supplierRepo != nil {
		repo.WithTokenCipher(supplierRepo.TapinCipher())
	}
	return repo
}

func priceTapinQuoteLegs(ctx context.Context, tapinRepo *repository.TapinRepository, quote *shippingQuote, lines []shippingQuoteLine, dest shippingQuoteDestination) error {
	if quote != nil && quote.PricingError != nil {
		return quote.PricingError
	}
	if tapinRepo == nil || quote == nil {
		return nil
	}
	// Group lines by leg origin key, exactly as computeShippingQuote
	// grouped them into quote.Legs.
	linesByLeg := map[string][]shippingQuoteLine{}
	for _, line := range lines {
		if line.Quantity <= 0 {
			continue
		}
		key, _ := legOrigin(line)
		linesByLeg[key] = append(linesByLeg[key], line)
	}
	repo := tapinRepo
	client := newTapinClient()
	changed := false
	for i := range quote.Legs {
		leg := &quote.Legs[i]
		legLines := linesByLeg[leg.OriginID]
		if len(legLines) == 0 || legLines[0].Fulfillment != "supplier_direct" {
			continue
		}
		supplierID := legLines[0].SupplierID
		creds, err := repo.GetSupplierCredentials(ctx, supplierID)
		if err != nil {
			return &tapinQuoteError{Message: tapinMsgUnreachable}
		}
		if !creds.Connected() {
			continue // not connected: formula price stands
		}
		fare, err := tapinLegFare(ctx, repo, client, creds, linesByLeg[leg.OriginID], leg.WeightGrams, dest)
		if err != nil {
			if errors.Is(err, errTapinServiceNotActivated) {
				// F4: the shop's Tipax service is not activated —
				// Tapin cannot price this leg at all, so its formula
				// fare + Source stand (changed stays as it was).
				continue
			}
			return err
		}
		leg.Fare = fare
		leg.Source = "tapin"
		leg.Estimated = false
		changed = true
	}
	if !changed {
		return nil
	}
	var total int64
	for _, leg := range quote.Legs {
		if err := pricing.AccumulateMoney(&total, leg.Fare, 1); err != nil {
			return err
		}
	}
	quote.TotalShipping = total
	quote.QuoteID = shippingQuoteIDFor(quote.Legs, total, quoteDestinationKey(dest))
	return nil
}

// tapinLegFare prices ONE connected supplier's leg via the inquiry.
// Any failure is a *tapinQuoteError carrying the message to surface.
func tapinLegFare(ctx context.Context, repo *repository.TapinRepository, client *tapin.Client, creds repository.SupplierTapinCredentials, legLines []shippingQuoteLine, legWeightGrams int64, dest shippingQuoteDestination) (int64, error) {
	origin, err := repo.GetSupplierDefaultWarehouseGeo(ctx, creds.SupplierID)
	if len(legLines) > 0 && legLines[0].SupplierWarehouseID != "" {
		origin, err = repo.GetSupplierWarehouseGeo(ctx, legLines[0].SupplierWarehouseID)
	}
	if err != nil {
		return 0, &tapinQuoteError{Message: tapinMsgUnreachable}
	}
	if origin == nil || origin.TapinCityID <= 0 || origin.TapinProvinceID <= 0 {
		return 0, &tapinQuoteError{Message: tapinMsgOriginUnmapped}
	}
	destProvinceID, destCityID, ok, err := repo.ResolveDestinationGeo(ctx, dest.Province, dest.City)
	if err != nil {
		return 0, &tapinQuoteError{Message: tapinMsgUnreachable}
	}
	if !ok {
		return 0, &tapinQuoteError{Message: tapinMsgDestUnmapped}
	}
	serviceType := 2 // inter-city (guide p.8)
	if origin.TapinCityID == destCityID {
		serviceType = 7 // intra-city
	}
	products, goodsWeight, goodsValueRial, goodsErr := checkedTapinGoods(legLines)
	if goodsErr != nil {
		return 0, goodsErr
	}

	// Guide rule (pp.3, 20): a Tipax parcel's total weight is at
	// least 1kg — lighter parcels are entered as 1kg. The padding is
	// carried in package_weight so the goods lines stay truthful.
	packageWeight := 0
	if goodsWeight < 1000 {
		packageWeight = int(1000 - goodsWeight)
	}
	// Dims: the platform box auto-picked for this leg. P0: the leg's
	// dims are the per-dimension MAX over its lines (mm -> cm, ceil),
	// so the auto-picked box fits the largest item on the leg. Zero
	// dims (unmeasured product) keep the old behaviour: PickBox falls
	// back to the configured default box (size 4) per the existing
	// rule. A catalogue with no dimensions falls back to the guide's
	// Tipax simplification (5x5x5, p.17 note).
	maxL, maxW, maxH := 0, 0, 0
	for _, line := range legLines {
		if line.LengthMM > maxL {
			maxL = line.LengthMM
		}
		if line.WidthMM > maxW {
			maxW = line.WidthMM
		}
		if line.HeightMM > maxH {
			maxH = line.HeightMM
		}
	}
	// ceil(mm / 10); 0 stays 0.
	legL, legW, legH := (maxL+9)/10, (maxW+9)/10, (maxH+9)/10
	length, width, height := 5, 5, 5
	if box, _, berr := repo.PickBoxForParcel(ctx, legL, legW, legH, int(legWeightGrams)); berr == nil &&
		box.LengthCm > 0 && box.WidthCm > 0 && box.HeightCm > 0 {
		length, width, height = box.LengthCm, box.WidthCm, box.HeightCm
	}
	cacheKey := fmt.Sprintf("%s|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d",
		creds.SupplierID, origin.TapinCityID, destProvinceID, destCityID,
		goodsWeight, goodsValueRial, serviceType, packageWeight, length, width, height)
	if fare, hit := tapinFareCacheGet(cacheKey); hit {
		return fare, nil
	}
	res, err := client.TipaxCheckPrice(ctx, creds.Token, tapin.TipaxCheckPriceRequest{
		ShopID:             creds.ShopID,
		ReceiverProvinceID: destProvinceID,
		ReceiverCityID:     destCityID,
		ProductTypeID:      1,  // general/package (guide p.8)
		PackingTypeID:      2,  // «نیاز به بسته بندی ندارد» — seller packs (guide p.2)
		PaymentType:        10, // online/prepaid (guide p.7)
		ServiceType:        serviceType,
		DeliveryType:       10, // deliver at customer address (guide p.7)
		PickupType:         20, // drop at branch — the guide's rule under 10 orders (p.3)
		Products:           products,
		Length:             length,
		Width:              width,
		Height:             height,
		PackageWeight:      packageWeight,
	})
	if err != nil {
		if errors.Is(err, dependencybudget.ErrOverloaded) {
			return 0, err
		}
		if tapin.IsServiceNotActivated(err) {
			return 0, errTapinServiceNotActivated
		}
		var ue *tapin.UpstreamError
		if errors.As(err, &ue) {
			return 0, &tapinQuoteError{Message: ue.Message}
		}
		return 0, &tapinQuoteError{Message: tapinMsgUnreachable}
	}
	fare, err := shippingquote.CheckedLegFareToman(0, shippingquote.Rates{BaseRateRial: res.TotalSendPrice})
	if err != nil || fare > pricing.MaxTransportMoney {
		return 0, pricing.ErrInvalidMoney
	}
	tapinFareCachePut(cacheKey, fare)
	return fare, nil
}
