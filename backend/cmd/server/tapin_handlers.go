package main

// Tapin completion handlers (gaps 1, 2, 3): the admin box-catalogue
// editor endpoints, the supplier parcel-label proxy, and the
// ship-with-Tapin registration core used by the supplier shipping
// submission. All upstream calls go through internal/tapin, whose
// contracts are captured verbatim in
// ~/workspace/tapin-spec/tapin-contract-capture.md.
//
// Only the TIPAX service is implemented: it is the one service whose
// full contract the official guide publishes. The POST service stays
// behind this file's explicit stub responses until its contract is
// captured from the Tapin panel (یکپارچه‌سازی ← وب سرویس).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/tapin"
)

// --- Admin: box catalogue ------------------------------------------

// handleT01AdminSettingsTapinBoxes serves GET/PUT
// /api/v1/admin/settings/tapin-boxes (admin role, enforced by the
// route). GET returns the full catalogue (inactive included) plus
// the platform default box id; PUT upserts the edited list. Boxes
// are never hard-deleted here: deactivation is the removal path, so
// the tapin_box_id recorded on past shipments always resolves.
func handleT01AdminSettingsTapinBoxes(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	w.Header().Set("Content-Type", "application/json")
	if db == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	repo := repository.NewTapinRepository(db)
	switch r.Method {
	case http.MethodGet:
		boxes, err := repo.ListBoxes(r.Context())
		if err != nil {
			writeInternalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      true,
			"boxes":        boxes,
			"defaultBoxId": repo.DefaultBoxID(r.Context()),
		})
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Boxes []tapin.Box `json:"boxes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
			return
		}
		for _, b := range req.Boxes {
			if strings.TrimSpace(b.ID) == "" || strings.TrimSpace(b.Title) == "" ||
				b.LengthCm <= 0 || b.WidthCm <= 0 || b.HeightCm <= 0 ||
				(b.MaxWeightGrams != nil && *b.MaxWeightGrams <= 0) {
				http.Error(w, `{"error":"Invalid box entry: id, title and positive dimensions are required"}`, http.StatusBadRequest)
				return
			}
		}
		if err := repo.ReplaceBoxes(r.Context(), req.Boxes); err != nil {
			writeInternalError(w, err)
			return
		}
		boxes, err := repo.ListBoxes(r.Context())
		if err != nil {
			writeInternalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      true,
			"boxes":        boxes,
			"defaultBoxId": repo.DefaultBoxID(r.Context()),
		})
	default:
		writeMethodNotAllowed(w)
	}
}

// --- Supplier: parcel label -----------------------------------------

// handleSupplierTapinLabel serves GET
// /api/v1/supplier/shipments/:id/tapin-label. The label itself comes
// from Tapin: the shipment's contract_code resolves to a hash_raw
// via the filter method, and the label GET is then proxied +
// streamed untouched (the guide documents the URL and the hash
// lookup but not the response format — capture file, NOT-CAPTURED
// item 2). Only Tipax-registered shipments have a label path; a
// Post registration (not yet buildable) would answer the explicit
// stub below.
func handleSupplierTapinLabel(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	shipmentID string,
	supplierRepo *repository.SupplierRepository,
	tapinRepo *repository.TapinRepository,
) {
	supplierID := getSupplierIDFromContext(r, supplierRepo)
	if claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil && claims.HasDirectCapability("admin") {
		if q := r.URL.Query().Get("supplierId"); q != "" {
			supplierID = q
		}
	}
	if supplierID == "" {
		http.Error(w, `{"error":"Supplier scope required"}`, http.StatusForbidden)
		return
	}
	state, err := tapinRepo.GetShipmentTapinStateByID(ctx, supplierID, shipmentID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if state == nil {
		http.Error(w, `{"error":"Shipment not found"}`, http.StatusNotFound)
		return
	}
	if state.TapinService == "" || state.TrackingNumber == "" {
		http.Error(w, `{"error":"این مرسوله با تاپین ثبت نشده است."}`, http.StatusBadRequest)
		return
	}
	if state.TapinService != "tipax" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  "not_implemented",
			"detail": "برچسب سرویس پست تاپین هنوز در دسترس نیست؛ قرارداد آن در پنل تاپین (یکپارچه‌سازی ← وب سرویس) است و هنوز دریافت نشده است.",
		})
		return
	}
	creds, err := tapinRepo.GetSupplierCredentials(ctx, supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !creds.Connected() {
		http.Error(w, `{"error":"اتصال تاپین تنظیم نشده است."}`, http.StatusBadRequest)
		return
	}
	client := newTapinClient()
	hash, err := client.TipaxFilterHashRaw(ctx, creds.Token, creds.ShopID, state.TrackingNumber)
	if err != nil {
		writeTapinUpstreamError(w, err)
		return
	}
	if hash == "" {
		http.Error(w, `{"error":"برچسب این مرسوله از تاپین دریافت نشد."}`, http.StatusBadGateway)
		return
	}
	status, contentType, body, err := client.GetRaw(ctx, creds.Token, tapin.TipaxLabelPath(hash))
	if err != nil {
		http.Error(w, `{"error":"`+tapinMsgUnreachable+`"}`, http.StatusBadGateway)
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeTapinUpstreamError renders a Tapin call failure: an envelope
// failure surfaces its upstream Persian message verbatim (502); a
// transport failure surfaces the unreachable message (502).
func writeTapinUpstreamError(w http.ResponseWriter, err error) {
	var ue *tapin.UpstreamError
	if errors.As(err, &ue) {
		body, _ := json.Marshal(map[string]interface{}{"error": ue.Message, "tapinError": true})
		http.Error(w, string(body), http.StatusBadGateway)
		return
	}
	http.Error(w, `{"error":"`+tapinMsgUnreachable+`"}`, http.StatusBadGateway)
}

// --- Supplier: ship-with-Tapin registration --------------------------

// tapinSubmitOutcome is what the shipping-submission handler writes:
// an HTTP status plus the JSON body.
type tapinSubmitOutcome struct {
	Status int
	Body   map[string]interface{}
}

func tapinOutcome(status int, body map[string]interface{}) tapinSubmitOutcome {
	return tapinSubmitOutcome{Status: status, Body: body}
}

// splitReceiverName splits the order's single full-name string into
// Tapin's first/last fields. Tapin's own payloads render empty name
// parts as "-" (guide p.24), mirrored here for a single-name
// receiver.
func splitReceiverName(full string) (first, last string) {
	parts := strings.Fields(strings.TrimSpace(full))
	if len(parts) == 0 {
		return "-", "-"
	}
	if len(parts) == 1 {
		return parts[0], "-"
	}
	return parts[0], strings.Join(parts[1:], " ")
}

// submitShippingViaTapin registers the supplier's shipment for an
// order with Tapin and, on success, persists it through the SAME
// SubmitShipping state machine the manual path uses (carrier
// 'tapin-post' / 'tapin-tipax', tracking = the upstream barcode).
//
// Failure discipline:
//   - Everything is validated BEFORE the upstream call; the Tipax
//     1-6kg total-weight guard (guide pp.3, 20) answers 422 locally.
//   - An envelope failure is a DEFINITE rejection: 502 with the
//     upstream message verbatim, zero state change, and the
//     unknown-outcome guard (tapin_register_attempted_at, migration
//     202610250012) is cleared so a retry is safe.
//   - A transport failure leaves the outcome UNKNOWN: the guard
//     stays stamped, so a blind retry answers 409 instead of
//     double-registering upstream.
//   - A shipment already carrying a Tapin tracking code answers 409.
func submitShippingViaTapin(
	ctx context.Context,
	supplierID, orderID, service, requestedBoxID, actorUserID string,
	shipmentRepo *repository.SupplierShipmentRepository,
	orderRepo *repository.OrderRepository,
	tapinRepo *repository.TapinRepository,
) tapinSubmitOutcome {
	if service == "post" {
		// Explicit stub, not a guess: the Post (irpost) contract is
		// not published in Tapin's official guide; it lives in the
		// panel's یکپارچه‌سازی ← وب سرویس section (owner-ask list).
		return tapinOutcome(http.StatusNotImplemented, map[string]interface{}{
			"error":  "not_implemented",
			"detail": "ثبت مرسوله با پست تاپین هنوز فعال نیست؛ قرارداد وب‌سرویس پست در راهنمای رسمی تاپین منتشر نشده و در پنل تاپین (یکپارچه‌سازی ← وب سرویس) قرار دارد.",
		})
	}
	if service != "tipax" {
		return tapinOutcome(http.StatusBadRequest, map[string]interface{}{
			"error": "سرویس تاپین نامعتبر است.",
		})
	}
	creds, err := tapinRepo.GetSupplierCredentials(ctx, supplierID)
	if err != nil {
		return tapinOutcome(http.StatusInternalServerError, map[string]interface{}{"error": "Internal server error"})
	}
	if !creds.Connected() {
		return tapinOutcome(http.StatusBadRequest, map[string]interface{}{
			"error": "اتصال تاپین تنظیم نشده است. ابتدا شناسه فروشگاه و توکن تاپین را ذخیره کنید.",
		})
	}
	order, err := orderRepo.GetOrderByID(ctx, orderID)
	if err != nil || order == nil {
		return tapinOutcome(http.StatusNotFound, map[string]interface{}{"error": "Order not found"})
	}
	state, err := tapinRepo.GetShipmentTapinState(ctx, supplierID, orderID)
	if err != nil {
		return tapinOutcome(http.StatusInternalServerError, map[string]interface{}{"error": "Internal server error"})
	}
	if state == nil {
		return tapinOutcome(http.StatusNotFound, map[string]interface{}{"error": "Shipment not found"})
	}
	if state.TrackingNumber != "" && strings.HasPrefix(state.Carrier, "tapin-") {
		return tapinOutcome(http.StatusConflict, map[string]interface{}{
			"error": "این مرسوله قبلاً با تاپین ثبت شده است.",
			"tapin": map[string]interface{}{
				"service": state.TapinService, "boxId": state.TapinBoxID,
				"trackingNumber": state.TrackingNumber,
			},
		})
	}
	if state.TapinRegisterAttempted {
		return tapinOutcome(http.StatusConflict, map[string]interface{}{
			"error": "ثبت این مرسوله در تاپین قبلاً تلاش شده و نتیجه‌اش نامشخص است؛ برای جلوگیری از ثبت دوباره، ابتدا وضعیت مرسوله را در پنل تاپین بررسی کنید.",
		})
	}
	if state.Status != "pending" && state.Status != "processing" {
		return tapinOutcome(http.StatusConflict, map[string]interface{}{
			"error": "Shipment is not ready for shipping",
		})
	}
	lines, err := tapinRepo.ListSupplierOrderLines(ctx, orderID, supplierID)
	if err != nil {
		return tapinOutcome(http.StatusInternalServerError, map[string]interface{}{"error": "Internal server error"})
	}
	if len(lines) == 0 {
		return tapinOutcome(http.StatusBadRequest, map[string]interface{}{
			"error": "برای این سفارش کالایی از شما یافت نشد.",
		})
	}
	destProvinceID, destCityID, ok, err := tapinRepo.ResolveDestinationGeo(ctx, order.CustomerProvince, order.CustomerCity)
	if err != nil {
		return tapinOutcome(http.StatusInternalServerError, map[string]interface{}{"error": "Internal server error"})
	}
	if !ok {
		return tapinOutcome(http.StatusBadRequest, map[string]interface{}{"error": tapinMsgDestUnmapped})
	}
	// Origin: the items' own origin warehouse when it carries Tapin
	// geo ids, else the supplier's default warehouse.
	var origin *repository.SupplierOriginWarehouse
	for _, l := range lines {
		if l.OriginWarehouseID == "" {
			continue
		}
		if w, werr := tapinRepo.GetSupplierWarehouseGeo(ctx, l.OriginWarehouseID); werr == nil && w != nil && w.TapinCityID > 0 && w.TapinProvinceID > 0 {
			origin = w
			break
		}
	}
	if origin == nil {
		w, werr := tapinRepo.GetSupplierDefaultWarehouseGeo(ctx, supplierID)
		if werr != nil {
			return tapinOutcome(http.StatusInternalServerError, map[string]interface{}{"error": "Internal server error"})
		}
		if w != nil && w.TapinCityID > 0 && w.TapinProvinceID > 0 {
			origin = w
		}
	}
	if origin == nil {
		return tapinOutcome(http.StatusBadRequest, map[string]interface{}{"error": tapinMsgOriginUnmapped})
	}
	serviceType := 2 // inter-city
	if origin.TapinCityID == destCityID {
		serviceType = 7 // intra-city
	}
	products := make([]tapin.TipaxProduct, 0, len(lines))
	var goodsWeight int64
	for _, l := range lines {
		w := l.WeightGrams
		if w <= 0 {
			w = 500 // platform fallback for an unweighed item
		}
		goodsWeight += int64(w) * int64(l.Quantity)
		products = append(products, tapin.TipaxProduct{
			Title:            strings.TrimSpace(l.Title + " " + l.VariantName + " " + l.VariantSPNC),
			AmountPerCount:   l.FinalUnitPrice * 10, // Toman -> Rial
			DiscountPerCount: 0,
			WeightPerCount:   w,
			Count:            l.Quantity,
		})
	}
	// Guide pp.3/20: total Tipax weight must be 1000-6000 g. Under
	// 1kg the guide says to enter 1kg — the padding rides in
	// package_weight; over 6kg is rejected BEFORE any upstream call.
	packageWeight := 0
	if goodsWeight < 1000 {
		packageWeight = int(1000 - goodsWeight)
	}
	if goodsWeight+int64(packageWeight) > 6000 {
		return tapinOutcome(http.StatusUnprocessableEntity, map[string]interface{}{
			"error": "وزن مجموع مرسوله برای تیپاکس باید بین ۱ تا ۶ کیلوگرم باشد.",
		})
	}
	// Box: the supplier's explicit choice (validated against the
	// active catalogue) or the system pick from the packed parcel's
	// recorded dims and the total weight.
	boxID := strings.TrimSpace(requestedBoxID)
	if boxID != "" {
		active, aerr := tapinRepo.ListActiveBoxes(ctx)
		if aerr != nil {
			return tapinOutcome(http.StatusInternalServerError, map[string]interface{}{"error": "Internal server error"})
		}
		found := false
		for _, b := range active {
			if b.ID == boxID {
				found = true
				break
			}
		}
		if !found {
			return tapinOutcome(http.StatusBadRequest, map[string]interface{}{
				"error": "جعبه انتخاب‌شده در کاتالوگ جعبه‌های تاپین نیست.",
			})
		}
	} else {
		pick, _, perr := tapinRepo.PickBoxForParcel(ctx,
			state.PackagingLengthMM/10, state.PackagingWidthMM/10, state.PackagingHeightMM/10,
			int(goodsWeight)+packageWeight)
		if perr != nil {
			return tapinOutcome(http.StatusInternalServerError, map[string]interface{}{"error": "Internal server error"})
		}
		boxID = pick.ID
	}
	firstName, lastName := splitReceiverName(order.CustomerFullName)
	req := tapin.TipaxRegisterRequest{
		ShopID:             creds.ShopID,
		ReceiverProvinceID: destProvinceID,
		ReceiverCityID:     destCityID,
		ReceiverFirstName:  firstName,
		ReceiverLastName:   lastName,
		ReceiverMobile:     order.CustomerPhone,
		ReceiverAddress:    order.CustomerAddress,
		ReceiverPlaque:     "-",
		ReceiverUnit:       "-",
		ReceiverFloor:      "-",
		ReceiverPostalCode: order.CustomerPostalCode,
		ProductTypeID:      1, // general/package
		PackingTypeID:      2, // «نیاز به بسته بندی ندارد» — the seller packs
		PaymentType:        10,
		ServiceType:        serviceType,
		DeliveryType:       10,
		PickupType:         20,
		EnableLabelPrivacy: false,
		Products:           products,
		RegisterType:       2, // ready-to-send: auto price inquiry + final (guide p.20)
		ManualID:           order.OrderNumber,
		PackageWeight:      packageWeight,
		Length:             5, // Tipax simplification (guide p.20)
		Width:              5,
		Height:             5,
	}
	// Stamp the unknown-outcome guard BEFORE the call (migration
	// 202610250012 semantics).
	if err := tapinRepo.MarkTapinRegisterAttempted(ctx, state.ID); err != nil {
		return tapinOutcome(http.StatusInternalServerError, map[string]interface{}{"error": "Internal server error"})
	}
	res, callErr := newTapinClient().TipaxRegisterOrder(ctx, creds.Token, req)
	if callErr != nil {
		var ue *tapin.UpstreamError
		if errors.As(callErr, &ue) {
			// Definite rejection: nothing was registered; clear the
			// guard and surface the upstream message. Zero state change.
			_ = tapinRepo.ClearTapinRegisterAttempted(ctx, state.ID)
			return tapinOutcome(http.StatusBadGateway, map[string]interface{}{
				"error": ue.Message, "tapinError": true,
			})
		}
		// Unknown outcome: the guard stays stamped; a retry answers
		// 409 (above) instead of double-registering.
		return tapinOutcome(http.StatusBadGateway, map[string]interface{}{
			"error": tapinMsgUnreachable, "tapinError": true,
		})
	}
	carrier := "tapin-tipax"
	if err := shipmentRepo.SubmitShipping(ctx, supplierID, orderID, carrier, res.ContractCode, actorUserID); err != nil {
		// The Tapin order EXISTS upstream but the local state machine
		// refused the transition. Surface the barcode so nothing is
		// lost; the guard stays stamped for manual reconciliation.
		return tapinOutcome(http.StatusConflict, map[string]interface{}{
			"error": "مرسوله در تاپین ثبت شد اما ثبت محلی آن کامل نشد؛ کد رهگیری را نگه دارید و با پشتیبانی در میان بگذارید.",
			"tapin": map[string]interface{}{
				"service": service, "boxId": boxID, "trackingNumber": res.ContractCode,
			},
		})
	}
	_ = tapinRepo.RecordTapinRegistration(ctx, state.ID, service, boxID, res.Status)
	return tapinOutcome(http.StatusOK, map[string]interface{}{
		"success":        true,
		"message":        "مرسوله در تاپین ثبت شد و کد رهگیری صادر شد.",
		"carrier":        carrier,
		"trackingNumber": res.ContractCode,
		"status":         "shipped",
		"tapin": map[string]interface{}{
			"service": service, "boxId": boxID, "trackingNumber": res.ContractCode,
		},
	})
}
