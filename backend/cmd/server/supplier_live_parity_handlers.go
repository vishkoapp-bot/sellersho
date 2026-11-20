package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/tapin"
	"github.com/google/uuid"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Live-parity supplier handlers (Team 06, Phase 2; MASTER-AUDIT D11/D12/D15).
//
// Existence of these routes in the live build is confirmed by the audit;
// per-route bodies/responses are largely unverified (U07). Routes whose
// behaviour cannot be grounded in source migrations, existing repo
// functions, or DB tables are registered as explicit 501 TODO stubs, never
// as guessed implementations. Tapin handlers here may accept (PUT settings)
// and select (connectivity test) a token, but NEVER return one in any API
// response (U63/Fix61); the token is used only for the server-side call.

func supplierLiveParityStub(w http.ResponseWriter, r *http.Request, todo, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  "not_implemented",
		"todo":   todo,
		"route":  r.URL.Path,
		"detail": detail,
	})
}

func supplierLiveParitySupplierID(w http.ResponseWriter, r *http.Request, h *SupplierHandlers) (string, bool) {
	id := getSupplierIDFromContext(r, h.supplierRepo)
	if id == "" {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return "", false
	}
	return id, true
}

func supplierLiveParityClaimsUserID(r *http.Request) string {
	if claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
		return claims.UserID
	}
	return ""
}

// POST /api/v1/supplier/contract/accept (D11).
// Records the platform's current supplier contract version as the
// supplier's signed version, using the existing repo functions whose
// comments describe exactly this re-confirmation flow.
func (h *SupplierHandlers) HandleSupplierContractAccept(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	platformVersion, err := h.supplierRepo.GetSupplierContractVersion(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.supplierRepo.UpdateSignedContractVersion(r.Context(), supplierID, platformVersion); err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":                 true,
		"supplierId":              supplierID,
		"signedContractVersion":   platformVersion,
		"platformContractVersion": platformVersion,
	})
}

// POST /api/v1/supplier/contract/reconfirm (D11).
// Same recording as accept, but a no-op success when the supplier already
// signed the current platform version (re-confirmation has nothing to do).
func (h *SupplierHandlers) HandleSupplierContractReconfirm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	sup, err := h.supplierRepo.GetSupplierByID(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sup == nil {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return
	}
	platformVersion, err := h.supplierRepo.GetSupplierContractVersion(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sup.SignedContractVersion >= platformVersion {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":                 true,
			"supplierId":              supplierID,
			"signedContractVersion":   sup.SignedContractVersion,
			"platformContractVersion": platformVersion,
			"alreadyCurrent":          true,
		})
		return
	}
	if err := h.supplierRepo.UpdateSignedContractVersion(r.Context(), supplierID, platformVersion); err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":                 true,
		"supplierId":              supplierID,
		"signedContractVersion":   platformVersion,
		"platformContractVersion": platformVersion,
		"alreadyCurrent":          false,
	})
}

// GET /api/v1/supplier/tapin/settings (D11).
// Returns only the non-secret part: the Tapin shop id and whether a shop
// is connected. No token field is ever emitted, masked or otherwise (U63).
func (h *SupplierHandlers) HandleGetTapinSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	sup, err := h.supplierRepo.GetSupplierByID(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sup == nil {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return
	}
	connected := sup.TapinShopID != nil && strings.TrimSpace(*sup.TapinShopID) != ""
	json.NewEncoder(w).Encode(map[string]interface{}{
		"supplierId":     supplierID,
		"tapinShopId":    sup.TapinShopID,
		"tapinConnected": connected,
	})
}

// PUT/POST /api/v1/supplier/tapin/settings (D11, Fix61).
// Stores the trimmed shop id and, when a non-empty token is supplied, the
// Tapin token. Accepts the live client's {shopId, token?} shape as well as
// the older {tapinShopId} shape. A token that is absent or empty leaves
// the stored token untouched (live placeholder: keep current token when
// empty); a shop id that is absent leaves the stored shop id untouched,
// while an explicitly empty shop id clears it. The token is never
// included in the response.
func (h *SupplierHandlers) HandleUpdateTapinSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var req struct {
		ShopID      *string `json:"shopId"`
		TapinShopID *string `json:"tapinShopId"`
		Token       *string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if req.ShopID != nil || req.TapinShopID != nil {
		shopID := ""
		if req.ShopID != nil {
			shopID = strings.TrimSpace(*req.ShopID)
		} else {
			shopID = strings.TrimSpace(*req.TapinShopID)
		}
		if err := h.supplierRepo.UpdateTapinShopID(r.Context(), supplierID, shopID); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	if req.Token != nil && strings.TrimSpace(*req.Token) != "" {
		if err := h.supplierRepo.UpdateTapinToken(r.Context(), supplierID, strings.TrimSpace(*req.Token)); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	sup, err := h.supplierRepo.GetSupplierByID(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sup == nil {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return
	}
	connected := sup.TapinShopID != nil && strings.TrimSpace(*sup.TapinShopID) != ""
	json.NewEncoder(w).Encode(map[string]interface{}{
		"supplierId":     supplierID,
		"tapinShopId":    sup.TapinShopID,
		"tapinConnected": connected,
	})
}

// DELETE /api/v1/supplier/tapin/settings (D11, Fix61).
// Disconnect: clears BOTH the stored shop id and the stored token, then
// returns the GET shape with connected=false. The token is never emitted.
func (h *SupplierHandlers) HandleDeleteTapinSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if err := h.supplierRepo.UpdateTapinShopID(r.Context(), supplierID, ""); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.supplierRepo.UpdateTapinToken(r.Context(), supplierID, ""); err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"supplierId":     supplierID,
		"tapinShopId":    nil,
		"tapinConnected": false,
	})
}

// GET /api/v1/supplier/tapin/boxes (D11, gaps 1-3).
// Tapin's API has NO boxes endpoint (verified against Tapin's
// official Tipax guide — the capture in ~/workspace/tapin-spec), so
// the box catalogue is platform-managed (tapin_boxes, migration
// 202611050008): this returns the active catalogue plus the
// platform default box id the client pre-selects.
func (h *SupplierHandlers) HandleTapinBoxes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, ok := supplierLiveParitySupplierID(w, r, h); !ok {
		return
	}
	if h.tapinRepo == nil {
		supplierLiveParityStub(w, r, "U07/U63", "Tapin repository not wired; box catalogue unavailable.")
		return
	}
	boxes, err := h.tapinRepo.ListActiveBoxes(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"boxes":        boxes,
		"defaultBoxId": h.tapinRepo.DefaultBoxID(r.Context()),
	})
}

// POST /api/v1/supplier/tapin/test (D11, gaps 1-3).
// Envelope-aware connectivity test: the stored token + shop id are
// sent to Tapin's Tipax options method (the shop id in the BODY —
// an empty body is answered 338 "no shop selected", live-verified)
// through internal/tapin, and the verdict reads the ENVELOPE, not
// the HTTP status class (Tapin wraps business failures in HTTP 200;
// the old status-class test called a dead token "connected").
// Verdicts (live-verified codes, 2026-11-05):
//   - envelope success (20/21): connected, Tipax service active.
//   - 30129: token + shop are VALID but the Tipax service is not
//     activated for this shop (activation is a Tapin support ticket)
//     -> ok:true with serviceActive:false, upstream message shown.
//   - 332 (token not found) and every other envelope failure:
//     ok:false with the upstream Persian message verbatim.
//   - transport failure: 502, unreachable.
//
// The token and shop id never appear in any response or log line.
func (h *SupplierHandlers) HandleTapinTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	sup, err := h.supplierRepo.GetSupplierByID(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sup == nil {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return
	}
	token := strings.TrimSpace(sup.TapinToken)
	shopID := ""
	if sup.TapinShopID != nil {
		shopID = strings.TrimSpace(*sup.TapinShopID)
	}
	if token == "" || shopID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"error":   "اتصال تاپین تنظیم نشده است. ابتدا شناسه فروشگاه و توکن تاپین را ذخیره کنید.",
			"message": "اتصال تاپین تنظیم نشده است. ابتدا شناسه فروشگاه و توکن تاپین را ذخیره کنید.",
		})
		return
	}
	callErr := newTapinClient().Options(r.Context(), token, shopID)
	if writeDependencyOverload(w, callErr) {
		return
	}
	if callErr == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":            true,
			"serviceActive": true,
			"message":       "اتصال به تاپین برقرار است و سرویس تیپاکس برای فروشگاه شما فعال است.",
		})
		return
	}
	var ue *tapin.UpstreamError
	if errors.As(callErr, &ue) {
		if ue.Status == tapin.StatusServiceNotActivated {
			// Credentials proven valid; only the Tipax activation is
			// missing on Tapin's side.
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":            true,
				"serviceActive": false,
				"message":       ue.Message,
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"error":   ue.Message,
			"message": ue.Message,
		})
		return
	}
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      false,
		"error":   "برقراری ارتباط با تاپین ممکن نشد. دوباره تلاش کنید.",
		"message": "برقراری ارتباط با تاپین ممکن نشد. دوباره تلاش کنید.",
	})
}

// GET /api/v1/supplier/shipping-methods (D11).
func (h *SupplierHandlers) HandleGetSupplierShippingMethods(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	methods, err := h.supplierRepo.ListSupplierShippingMethods(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(methods)
}

// POST /api/v1/supplier/shipping-methods/request (D11).
// Suppliers may only REQUEST a method (migration 202610170012); an admin
// decides. Body: {"method":"direct"|"warehouse"}.
func (h *SupplierHandlers) HandleRequestSupplierShippingMethod(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var req struct {
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if err := h.supplierRepo.RequestSupplierShippingMethod(r.Context(), supplierID, strings.TrimSpace(req.Method)); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"supplierId": supplierID,
		"method":     strings.TrimSpace(req.Method),
		"status":     "requested",
	})
}

// GET /api/v1/supplier/pricing-settings (D12, read-only).
// Returns the supplier's agreement markup as stored (D53/D55 fields) and
// the tiered-pricing flag. TODO (U07): whether the live route also
// accepts writes, and its exact response shape, are unverified.
func (h *SupplierHandlers) HandleGetSupplierPricingSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	sup, err := h.supplierRepo.GetSupplierByID(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sup == nil {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"supplierId":         supplierID,
		"defaultMarkupType":  sup.DefaultMarkupType,
		"defaultMarkupValue": sup.DefaultMarkupValue,
	})
}

// PUT /api/v1/supplier/pricing-settings (D12, stub).
// TODO (U07): whether a supplier may self-set the agreement markup is
// unverified (the markup is the admin pricing tab's data in source), and
// no discount-campaign price application is implemented here (U04).
func (h *SupplierHandlers) HandleUpdateSupplierPricingSettings(w http.ResponseWriter, r *http.Request) {
	supplierLiveParityStub(w, r, "U07", "Supplier self-setting of agreement markup is unverified; not guessed.")
}

// GET /api/v1/supplier/warehouses (D12).
func (h *SupplierHandlers) HandleGetSupplierWarehouses(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListSupplierWarehouses(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if list == nil {
		list = []repository.SupplierWarehouse{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// POST /api/v1/supplier/warehouses (Fix 37, live parity: createMyWarehouse).
// Live validates client-side with the same two messages; the server mirrors
// them so a direct API call cannot store an invalid warehouse. Duplicate
// names map to a readable 400 (live's exact duplicate message is unproven).
func (h *SupplierHandlers) HandleCreateSupplierWarehouse(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var req repository.SupplierWarehouse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	validateWarehouseInput(&req)
	if req.Name == "" || req.ProvinceName == "" || req.CityName == "" {
		http.Error(w, `{"error":"نام، استان و شهر انبار الزامی است."}`, http.StatusBadRequest)
		return
	}
	if req.PostalCode != "" && !isValidPostalCode(req.PostalCode) {
		http.Error(w, `{"error":"کد پستی باید دقیقاً ۱۰ رقم باشد."}`, http.StatusBadRequest)
		return
	}
	created, err := h.supplierRepo.CreateSupplierWarehouse(r.Context(), supplierID, req)
	if err != nil {
		if err == repository.ErrWarehouseNameTaken {
			http.Error(w, `{"error":"انباری با این نام قبلاً ثبت شده است."}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

// PUT /api/v1/supplier/warehouses/{id} (Fix 37, live parity: updateMyWarehouse).
func (h *SupplierHandlers) HandleUpdateSupplierWarehouse(w http.ResponseWriter, r *http.Request, id string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var req repository.SupplierWarehouse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	validateWarehouseInput(&req)
	if req.Name == "" || req.ProvinceName == "" || req.CityName == "" {
		http.Error(w, `{"error":"نام، استان و شهر انبار الزامی است."}`, http.StatusBadRequest)
		return
	}
	if req.PostalCode != "" && !isValidPostalCode(req.PostalCode) {
		http.Error(w, `{"error":"کد پستی باید دقیقاً ۱۰ رقم باشد."}`, http.StatusBadRequest)
		return
	}
	updated, err := h.supplierRepo.UpdateSupplierWarehouse(r.Context(), supplierID, id, req)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Supplier warehouse not found"}`, http.StatusNotFound)
			return
		}
		if err == repository.ErrWarehouseNameTaken {
			http.Error(w, `{"error":"انباری با این نام قبلاً ثبت شده است."}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// validateWarehouseInput trims the supplier-warehouse form fields in place.
func validateWarehouseInput(req *repository.SupplierWarehouse) {
	req.Name = strings.TrimSpace(req.Name)
	req.ProvinceName = strings.TrimSpace(req.ProvinceName)
	req.CityName = strings.TrimSpace(req.CityName)
	req.Address = strings.TrimSpace(req.Address)
	req.PostalCode = strings.TrimSpace(req.PostalCode)
	req.Phone = strings.TrimSpace(req.Phone)
}

// isValidPostalCode mirrors live's client rule: exactly 10 ASCII digits.
func isValidPostalCode(code string) bool {
	if len(code) != 10 {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// GET /api/v1/supplier/orders/{orderId}/addons (Fix 38, live parity:
// getOrderAddons). Response shape is live's: {addons, total}.
func (h *SupplierHandlers) HandleGetSupplierOrderAddons(w http.ResponseWriter, r *http.Request, orderID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	addons, err := h.supplierRepo.GetSupplierOrderAddons(r.Context(), supplierID, orderID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var total int64
	for _, a := range addons {
		total += a.Total
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"addons": addons,
		"total":  total,
	})
}

// PUT /api/v1/supplier/products/{id}/origin (Fix 39, live parity:
// updateProductOrigin). Body is live's shape: {supplierWarehouseId} where
// an empty string means "profile city" (no explicit origin).
func (h *SupplierHandlers) HandleUpdateProductOrigin(w http.ResponseWriter, r *http.Request, productID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var req struct {
		SupplierWarehouseID string `json:"supplierWarehouseId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if err := h.supplierRepo.UpdateProductOrigin(r.Context(), supplierID, productID, strings.TrimSpace(req.SupplierWarehouseID)); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Supplier product not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// GET /api/v1/supplier/warehouses/central (D12).
// TODO (U07): see ListCentralWarehouses for the "central" reading.
func (h *SupplierHandlers) HandleGetCentralWarehouses(w http.ResponseWriter, r *http.Request) {
	list, err := h.supplierRepo.ListCentralWarehouses(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// GET /api/v1/supplier/exclusivity-challenges (D12).
func (h *SupplierHandlers) HandleGetSupplierExclusivityChallenges(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListSupplierExclusivityChallenges(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// GET /api/v1/supplier/price-increases (D12, read-only).
func (h *SupplierHandlers) HandleGetSupplierPriceIncreases(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListSupplierPriceIncreases(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// POST /api/v1/supplier/price-increases/exception-ticket (D12; gap 11).
// Files the supplier's exception request as a finance-section support
// ticket (migration 202610310003 routes exception tickets by
// section = 'finance'); an admin later turns it into a per-product grant
// via POST /api/v1/admin/products/price-exception/{productId}. Body:
// {productId, newReference, message?}. newReference and message are
// validated here but not persisted: the sanctioned ticket columns carry
// no body field (see CreatePriceIncreaseExceptionTicket).
func (h *SupplierHandlers) HandleSupplierPriceIncreaseExceptionTicket(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var req struct {
		ProductID    string `json:"productId"`
		NewReference int64  `json:"newReference"`
		Message      string `json:"message"`
	}
	if err := decodeMoneyJSON(r.Body, &req, true); err != nil || strings.TrimSpace(req.ProductID) == "" || req.NewReference <= 0 {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	// requester_user_id is a UUID column: the claims user id is the
	// requester; when absent, the repository falls back to the supplier's
	// owner_user_id (never the supplier record id).
	requesterUserID := supplierLiveParityClaimsUserID(r)
	ticketID, err := h.supplierRepo.CreatePriceIncreaseExceptionTicket(r.Context(), supplierID, requesterUserID, strings.TrimSpace(req.ProductID))
	if err != nil {
		if errors.Is(err, repository.ErrExceptionTicketProductNotFound) {
			http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "ticketId": ticketID})
}

// GET /api/v1/supplier/stock-share (D12, stub).
// TODO (U07): no stock-share table, column, or body shape exists in
// source; a share percentage or quantity is not guessed.
func (h *SupplierHandlers) HandleGetSupplierStockShare(w http.ResponseWriter, r *http.Request) {
	supplierLiveParityStub(w, r, "U07", "No stock-share data source exists in source; not guessed.")
}

// GET /api/v1/supplier/customer-returns (D12).
func (h *SupplierHandlers) HandleGetSupplierCustomerReturns(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListSupplierCustomerReturns(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// GET /api/v1/supplier/return-shipping-charges (D12).
func (h *SupplierHandlers) HandleGetSupplierReturnShippingCharges(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListSupplierReturnShippingCharges(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if list == nil {
		list = []repository.SupplierReturnShippingCharge{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"charges": list})
}

// POST /api/v1/supplier/customer-returns/{returnItemId}/request-back
// (FIX 44, live's An submit handler S): supplier requests received return
// units back from the returns warehouse. Idempotency-Key header is
// honoured; a replay returns the original request.
func (h *SupplierHandlers) HandleRequestSupplierReturnBack(w http.ResponseWriter, r *http.Request, returnItemID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var body struct {
		Quantity int    `json:"quantity"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	back, err := h.supplierRepo.CreateSupplierReturnBack(r.Context(), supplierID, returnItemID, body.Quantity, body.Reason, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeSupplierReturnBackError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(back)
}

// POST /api/v1/supplier/customer-returns/back-requests/{id}/cancel
// (FIX 44, live's An handler R): supplier withdraws a requested
// back-request before warehouse review.
func (h *SupplierHandlers) HandleCancelSupplierReturnBack(w http.ResponseWriter, r *http.Request, backID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	back, err := h.supplierRepo.CancelSupplierReturnBack(r.Context(), supplierID, backID, body.Reason, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeSupplierReturnBackError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(back)
}

// POST /api/v1/supplier/customer-returns/back-requests/{id}/complete
// (FIX 44, live's An handler $): supplier confirms receipt of units the
// warehouse shipped back.
func (h *SupplierHandlers) HandleCompleteSupplierReturnBack(w http.ResponseWriter, r *http.Request, backID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	back, err := h.supplierRepo.CompleteSupplierReturnBack(r.Context(), supplierID, backID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeSupplierReturnBackError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(back)
}

func writeSupplierReturnBackError(w http.ResponseWriter, err error) {
	switch {
	case err == repository.ErrSupplierReturnBackActive:
		http.Error(w, `{"error":"an active back request already exists for this item"}`, http.StatusConflict)
	case err == repository.ErrSupplierReturnBackNotReceived:
		http.Error(w, `{"error":"item has not been received at the warehouse yet"}`, http.StatusBadRequest)
	case err == repository.ErrSupplierReturnBackBadQuantity:
		http.Error(w, `{"error":"quantity is out of range for this item"}`, http.StatusBadRequest)
	case err == repository.ErrSupplierReturnBackBadState:
		http.Error(w, `{"error":"back request is not in a state that allows this action"}`, http.StatusConflict)
	case err == sql.ErrNoRows:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	default:
		writeInternalError(w, err)
	}
}

// GET /api/v1/supplier/shipments (FIX 46, live's ln getShipments):
// carton shipments for this supplier, newest first.
func (h *SupplierHandlers) HandleGetCartonShipments(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListCartonShipments(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if list == nil {
		list = []repository.CartonShipment{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"shipments": list})
}

// POST /api/v1/supplier/shipments (FIX 46, live's ln createShipment):
// creates a carton shipment with variant lines, returns {shipment}.
func (h *SupplierHandlers) HandleCreateCartonShipment(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var body struct {
		WarehouseID string                          `json:"warehouse_id"`
		CreationKey string                          `json:"creation_key"`
		Action      string                          `json:"action"`
		ShipmentID  string                          `json:"shipment_id"`
		Source      repository.CartonInboundSource  `json:"source"`
		Items       []repository.CartonShipmentItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if body.Action != "" {
		if (body.Action != "dispatch" && body.Action != "cancel") || body.ShipmentID == "" {
			http.Error(w, `{"error":"invalid shipment action"}`, 400)
			return
		}
		if err := h.supplierRepo.ChangeCartonDispatch(r.Context(), supplierID, body.ShipmentID, body.Action == "cancel"); err != nil {
			http.Error(w, `{"error":"inbound provenance or physical state conflict"}`, 409)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}
	if strings.TrimSpace(body.CreationKey) == "" {
		http.Error(w, `{"error":"creation_key is required"}`, http.StatusBadRequest)
		return
	}
	out, err := h.supplierRepo.CreateCartonShipmentFromSource(r.Context(), supplierID, body.Items, body.Source, body.WarehouseID, body.CreationKey)
	if err != nil {
		if errors.Is(err, repository.ErrCartonCreationConflict) || errors.Is(err, repository.ErrInboundProvenance) || errors.Is(err, repository.ErrInsufficientInventory) {
			http.Error(w, `{"error":"creation_key belongs to a different shipment payload"}`, http.StatusConflict)
			return
		}
		var state interface{ SQLState() string }
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
			(errors.As(err, &state) && (state.SQLState() == "40001" || state.SQLState() == "40P01" || state.SQLState() == "55P03")) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":"shipment creation is busy; retry with the same creation_key"}`, http.StatusServiceUnavailable)
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"variant not found or not yours"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"invalid shipment lines"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"shipment": out})
}

// GET /api/v1/supplier/products/{id}/variants (FIX 46, live's ln
// getVariants; reused later by the products-tab variants modal Fl).
func (h *SupplierHandlers) HandleGetProductVariants(w http.ResponseWriter, r *http.Request, productID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListProductVariants(r.Context(), supplierID, productID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if list == nil {
		list = []repository.SupplierProductVariant{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"variants": list})
}

// FIX 47 — live gallery modal (Ll) + variants modal (Fl) handlers.
// Gallery cap is live's ft=8; the first image (sort_order 0) is the cover.
// Upload validation mirrors HandleProductImageUpload (extension whitelist,
// 5MB per file, magic bytes) and returns live's client-side strings so
// toasts match whether the check fires client- or server-side.
func fix47ImageExt(buf []byte, filename string) (string, bool) {
	ext := ""
	if i := strings.LastIndex(filename, "."); i >= 0 {
		ext = strings.ToLower(filename[i:])
	}
	switch ext {
	case ".jpg", ".jpeg":
		if len(buf) >= 3 && buf[0] == 0xFF && buf[1] == 0xD8 && buf[2] == 0xFF {
			return ".jpg", true
		}
	case ".png":
		if len(buf) >= 8 && buf[0] == 0x89 && buf[1] == 0x50 && buf[2] == 0x4E && buf[3] == 0x47 {
			return ".png", true
		}
	case ".webp":
		if len(buf) >= 12 && string(buf[0:4]) == "RIFF" && string(buf[8:12]) == "WEBP" {
			return ".webp", true
		}
	}
	return "", false
}

// GET /api/v1/supplier/products/{id}/images
func (h *SupplierHandlers) HandleGetProductImages(w http.ResponseWriter, r *http.Request, productID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListProductImages(r.Context(), supplierID, productID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if list == nil {
		list = []repository.SupplierProductImage{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"images": list})
}

// POST /api/v1/supplier/products/{id}/images (multipart field "images")
func (h *SupplierHandlers) HandleUploadProductImages(w http.ResponseWriter, r *http.Request, productID, uploadDir string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if uploadDir == "" {
		uploadDir = "./static/uploads/products"
	}
	r.Body = http.MaxBytesReader(w, r.Body, 48*1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, `{"error":"خطا در پردازش فرم یا حجم بیش از حد مجاز"}`, http.StatusBadRequest)
		return
	}
	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		http.Error(w, `{"error":"فایل تصویر یافت نشد"}`, http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		writeInternalError(w, err)
		return
	}
	urls := make([]string, 0, len(files))
	for _, fh := range files {
		if fh.Size > 5*1024*1024 {
			http.Error(w, `{"error":"حجم هر فایل نباید بیشتر از ۵ مگابایت باشد."}`, http.StatusBadRequest)
			return
		}
		f, err := fh.Open()
		if err != nil {
			writeInternalError(w, err)
			return
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		head = head[:n]
		safeExt, valid := fix47ImageExt(head, fh.Filename)
		if !valid {
			f.Close()
			http.Error(w, `{"error":"فرمت نامعتبر. فقط JPG، PNG و WEBP مجاز است."}`, http.StatusBadRequest)
			return
		}
		if seeker, ok := f.(io.Seeker); ok {
			_, _ = seeker.Seek(0, io.SeekStart)
		}
		filename := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().Unix(), safeExt)
		dst, err := os.Create(filepath.Join(uploadDir, filename))
		if err != nil {
			f.Close()
			writeInternalError(w, err)
			return
		}
		if _, err := io.Copy(dst, f); err != nil {
			dst.Close()
			f.Close()
			writeInternalError(w, err)
			return
		}
		dst.Close()
		f.Close()
		urls = append(urls, "/static/uploads/products/"+filename)
	}
	list, err := h.supplierRepo.AddProductImages(r.Context(), supplierID, productID, urls)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
			return
		}
		if err == repository.ErrGalleryFull {
			http.Error(w, `{"error":"حداکثر ۸ تصویر برای هر محصول مجاز است."}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"images": list})
}

// DELETE /api/v1/supplier/products/{id}/images/{imageId}
func (h *SupplierHandlers) HandleDeleteProductImage(w http.ResponseWriter, r *http.Request, productID, imageID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if err := h.supplierRepo.DeleteProductImage(r.Context(), supplierID, productID, imageID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"image not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// PUT /api/v1/supplier/products/{id}/images/{imageId}/cover
func (h *SupplierHandlers) HandleSetProductCoverImage(w http.ResponseWriter, r *http.Request, productID, imageID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if err := h.supplierRepo.SetProductCoverImage(r.Context(), supplierID, productID, imageID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"image not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// PUT /api/v1/supplier/products/{id}/images/reorder {orderedIds}
func (h *SupplierHandlers) HandleReorderProductImages(w http.ResponseWriter, r *http.Request, productID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		OrderedIDs []string `json:"orderedIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if err := h.supplierRepo.ReorderProductImages(r.Context(), supplierID, productID, body.OrderedIDs); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"خطا در ذخیره ترتیب."}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// P0 variant images (2026-11-13): mirrors the product gallery handlers
// above, scoped to product_variant_images (cap 4/variant). Same upload
// validation: 5 MB/file, magic bytes via fix47ImageExt, UUID filenames.

// GET /api/v1/supplier/products/{id}/variants/{vid}/images
func (h *SupplierHandlers) HandleGetVariantImages(w http.ResponseWriter, r *http.Request, productID, variantID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListVariantImages(r.Context(), supplierID, productID, variantID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if list == nil {
		list = []repository.SupplierProductImage{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"images": list})
}

// POST /api/v1/supplier/products/{id}/variants/{vid}/images (multipart field "images")
func (h *SupplierHandlers) HandleUploadVariantImages(w http.ResponseWriter, r *http.Request, productID, variantID, uploadDir string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if uploadDir == "" {
		uploadDir = "./static/uploads/products"
	}
	r.Body = http.MaxBytesReader(w, r.Body, 48*1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, `{"error":"خطا در پردازش فرم یا حجم بیش از حد مجاز"}`, http.StatusBadRequest)
		return
	}
	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		http.Error(w, `{"error":"فایل تصویر یافت نشد"}`, http.StatusBadRequest)
		return
	}
	// Cap + ownership are checked BEFORE any file is written to disk:
	// a rejected upload (full gallery, or a variant the supplier does
	// not own) must leave no orphan files behind.
	if err := h.supplierRepo.CheckVariantGalleryRoom(r.Context(), supplierID, productID, variantID, len(files)); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		if err == repository.ErrVariantGalleryFull {
			http.Error(w, `{"error":"حداکثر ۴ تصویر برای هر تنوع مجاز است."}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		writeInternalError(w, err)
		return
	}
	urls := make([]string, 0, len(files))
	for _, fh := range files {
		if fh.Size > 5*1024*1024 {
			http.Error(w, `{"error":"حجم هر فایل نباید بیشتر از ۵ مگابایت باشد."}`, http.StatusBadRequest)
			return
		}
		f, err := fh.Open()
		if err != nil {
			writeInternalError(w, err)
			return
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		head = head[:n]
		safeExt, valid := fix47ImageExt(head, fh.Filename)
		if !valid {
			f.Close()
			http.Error(w, `{"error":"فرمت نامعتبر. فقط JPG، PNG و WEBP مجاز است."}`, http.StatusBadRequest)
			return
		}
		if seeker, ok := f.(io.Seeker); ok {
			_, _ = seeker.Seek(0, io.SeekStart)
		}
		filename := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().Unix(), safeExt)
		dst, err := os.Create(filepath.Join(uploadDir, filename))
		if err != nil {
			f.Close()
			writeInternalError(w, err)
			return
		}
		if _, err := io.Copy(dst, f); err != nil {
			dst.Close()
			f.Close()
			writeInternalError(w, err)
			return
		}
		dst.Close()
		f.Close()
		urls = append(urls, "/static/uploads/products/"+filename)
	}
	list, err := h.supplierRepo.AddVariantImages(r.Context(), supplierID, productID, variantID, urls)
	if err != nil {
		// Best-effort cleanup: the pre-check above normally prevents
		// this, but a concurrent upload can still fill the gallery
		// between the check and the insert — never leave orphans.
		for _, u := range urls {
			_ = os.Remove(filepath.Join(uploadDir, filepath.Base(u)))
		}
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		if err == repository.ErrVariantGalleryFull {
			http.Error(w, `{"error":"حداکثر ۴ تصویر برای هر تنوع مجاز است."}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"images": list})
}

// DELETE /api/v1/supplier/products/{id}/variants/{vid}/images/{imageId}
func (h *SupplierHandlers) HandleDeleteVariantImage(w http.ResponseWriter, r *http.Request, productID, variantID, imageID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	if err := h.supplierRepo.DeleteVariantImage(r.Context(), supplierID, productID, variantID, imageID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"image not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// PUT /api/v1/supplier/products/{id}/variants/{vid}/images/reorder {orderedIds}
func (h *SupplierHandlers) HandleReorderVariantImages(w http.ResponseWriter, r *http.Request, productID, variantID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		OrderedIDs []string `json:"orderedIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if err := h.supplierRepo.ReorderVariantImages(r.Context(), supplierID, productID, variantID, body.OrderedIDs); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"خطا در ذخیره ترتیب."}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// PUT /api/v1/supplier/products/{id}/variants/{vid}/physical
// Body: {"weightGrams": 1200, "lengthMm": 300, "widthMm": 200,
// "heightMm": 150}. All fields nullable; an explicit null clears the
// field back to inherit-from-product. A field absent from the body
// keeps its stored value. Dims are validated on the RESULTING triple
// (all-or-none), so a single-dim update is fine when the other two
// are already stored, and a lone dimension on an unmeasured variant
// is rejected. No admin re-approval (P0 decision D6).
func (h *SupplierHandlers) HandleUpdateVariantPhysical(w http.ResponseWriter, r *http.Request, productID, variantID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	current, err := h.supplierRepo.GetSupplierVariant(r.Context(), supplierID, productID, variantID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	// Merge body over stored values; each key accepts a JSON integer
	// or explicit null (clear). Anything else is a 400.
	weight, length, width, height := current.WeightGrams, current.LengthMM, current.WidthMM, current.HeightMM
	for _, f := range []struct {
		key string
		dst **int
	}{
		{"weightGrams", &weight},
		{"lengthMm", &length},
		{"widthMm", &width},
		{"heightMm", &height},
	} {
		msg, present := raw[f.key]
		if !present {
			continue
		}
		if string(msg) == "null" {
			*f.dst = nil
			continue
		}
		var n int
		if err := json.Unmarshal(msg, &n); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		// Reject non-integers (e.g. 1.5) that Unmarshal into int
		// would otherwise silently accept via truncation in older
		// decoders: re-encode and compare the raw text.
		var f64 float64
		if err := json.Unmarshal(msg, &f64); err != nil || f64 != float64(n) {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		*f.dst = &n
	}
	if err := repository.ValidateVariantPhysicalValues(weight, length, width, height); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	out, err := h.supplierRepo.UpdateVariantPhysical(r.Context(), supplierID, productID, variantID, weight, length, width, height)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// PUT /api/v1/supplier/products/{id}/variants/{vid}/sale (P1(c)).
// Sets (or clears) a per-variant sale price with an optional window.
// Body fields are partial: omitted fields keep their values, except
// clearing salePrice clears from/to as well. salePrice null clears
// the whole sale. Validation (all server-side, before write):
//   - S > 0, multiple of 100 (displayed prices sit on the 100 grid)
//   - S > R, where R is the reference cost from the variant's current
//     effective cost C and the platform-pct ladder (floor = R, not C:
//     the platform fee R-C is not discountable; R is a derived
//     platform number, safe to reveal to the variant's own supplier)
//   - S < P_central, the central-store resolved price at write time
//   - saleFrom < saleTo when both set; saleTo strictly in the future
//   - admin-locked variants (variant/product admin_set lock) => 409
//
// A sale may be set on an inactive variant (inert until activation).
// Setting a sale needs no supplier_price_increases row: a sale is a
// final-price DECREASE. If a later governed cost increase pushes R
// above S, the sale auto-suspends at display/charge time.
func (h *SupplierHandlers) HandleUpdateVariantSale(w http.ResponseWriter, r *http.Request, productID, variantID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if err := decodeMoneyJSON(r.Body, &raw, true); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	current, err := h.supplierRepo.GetSupplierVariant(r.Context(), supplierID, productID, variantID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	// Merge body over stored values. salePrice accepts an integer or
	// explicit null (clear); saleFrom/saleTo accept RFC3339 strings
	// or explicit null (clear). Anything else is a 400.
	salePrice := current.SalePrice
	saleFrom := current.SaleFrom
	saleTo := current.SaleTo
	if msg, present := raw["salePrice"]; present {
		if string(msg) == "null" {
			salePrice, saleFrom, saleTo = nil, nil, nil
		} else {
			var n int64
			var f float64
			if err := json.Unmarshal(msg, &f); err != nil || f != float64(int64(f)) {
				http.Error(w, `{"error":"invalid salePrice"}`, http.StatusBadRequest)
				return
			}
			n = int64(f)
			salePrice = &n
		}
	}
	for _, f := range []struct {
		key string
		dst **time.Time
	}{
		{"saleFrom", &saleFrom},
		{"saleTo", &saleTo},
	} {
		msg, present := raw[f.key]
		if !present {
			continue
		}
		if string(msg) == "null" {
			*f.dst = nil
			continue
		}
		var s string
		if err := json.Unmarshal(msg, &s); err != nil {
			http.Error(w, `{"error":"invalid sale window"}`, http.StatusBadRequest)
			return
		}
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
		if err != nil {
			http.Error(w, `{"error":"invalid sale window"}`, http.StatusBadRequest)
			return
		}
		*f.dst = &parsed
	}
	// Clearing salePrice clears the window too.
	if salePrice == nil {
		saleFrom, saleTo = nil, nil
	}
	if salePrice != nil {
		if *salePrice <= 0 || *salePrice%100 != 0 {
			http.Error(w, `{"error":"salePrice must be a positive multiple of 100"}`, http.StatusBadRequest)
			return
		}
		if saleFrom != nil && saleTo != nil && !saleFrom.Before(*saleTo) {
			http.Error(w, `{"error":"saleFrom must be before saleTo"}`, http.StatusBadRequest)
			return
		}
		if saleTo != nil && !saleTo.After(time.Now()) {
			http.Error(w, `{"error":"saleTo must be in the future"}`, http.StatusBadRequest)
			return
		}
		// Floor R / ceiling P_central from the CURRENT effective cost
		// and the platform-pct ladder, resolved as the central store.
		categorySlug, costC, err := h.supplierRepo.GetVariantSaleBasis(r.Context(), supplierID, productID, variantID)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
				return
			}
			writeInternalError(w, err)
			return
		}
		settings, _ := loadPricingSettings(r.Context(), h.pricingRepo)
		central := domain.Store{ID: "store-central", Slug: "central", OwnerType: "central"}
		prod := domain.Product{ID: productID, CategorySlug: categorySlug, SupplierBaseCost: costC}
		centralRes := pricing.ResolveReferencePricingForStoreProductVariantWithCost(prod, costC, variantID, central, settings, nil, nil)
		if centralRes.PricingError != "" {
			http.Error(w, `{"error":"Product pricing is unavailable"}`, http.StatusConflict)
			return
		}
		refCost := centralRes.BaseCost
		if *salePrice <= refCost {
			http.Error(w, fmt.Sprintf(`{"error":"salePrice must be above the reference cost %d"}`, refCost), http.StatusBadRequest)
			return
		}
		if *salePrice >= centralRes.FinalPrice {
			http.Error(w, fmt.Sprintf(`{"error":"salePrice must be below the central price %d"}`, centralRes.FinalPrice), http.StatusBadRequest)
			return
		}
		// Admin-locked variants cannot go on sale: the lock fixes
		// the margin and a sale would undercut it (409).
		if h.pricingRepo != nil {
			locked, lerr := h.pricingRepo.VariantAdminLocked(r.Context(), productID, variantID)
			if lerr != nil {
				writeInternalError(w, lerr)
				return
			}
			if locked {
				http.Error(w, `{"error":"variant margin is locked by admin"}`, http.StatusConflict)
				return
			}
		}
	}
	out, err := h.supplierRepo.UpdateVariantSale(r.Context(), supplierID, productID, variantID, salePrice, saleFrom, saleTo)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// POST /api/v1/supplier/products/{id}/variants {variant_name, attributes}
func (h *SupplierHandlers) HandleCreateProductVariant(w http.ResponseWriter, r *http.Request, productID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		ID               string            `json:"id"`
		Selection        map[string]string `json:"selection"`
		IsActive         *bool             `json:"is_active"`
		Stock            int               `json:"stock"`
		SupplierBaseCost *int64            `json:"supplier_base_cost"`
		MinCartQty       *int              `json:"min_cart_qty"`
		MaxCartQty       *int              `json:"max_cart_qty"`
		VariantName      string            `json:"variant_name"`
		Attributes       map[string]string `json:"attributes"`
	}
	if err := decodeMoneyJSON(r.Body, &body, true); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if body.Selection != nil {
		if body.IsActive == nil {
			http.Error(w, `{"error":"SKU active state required"}`, http.StatusBadRequest)
			return
		}
		out, err := h.supplierRepo.CreateProductVariantV2(r.Context(), supplierID, productID, repository.SupplierProductVariant{ID: body.ID, Selection: body.Selection, IsActive: *body.IsActive, Stock: body.Stock, SupplierBaseCost: body.SupplierBaseCost, MinCartQty: body.MinCartQty, MaxCartQty: body.MaxCartQty})
		if err != nil {
			status := http.StatusBadRequest
			if err == sql.ErrNoRows {
				status = http.StatusNotFound
			}
			http.Error(w, `{"error":"Invalid canonical SKU configuration"}`, status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
		return
	}
	if strings.TrimSpace(body.VariantName) == "" {
		http.Error(w, `{"error":"نام تنوع را وارد کنید."}`, http.StatusBadRequest)
		return
	}
	if body.Attributes == nil {
		body.Attributes = map[string]string{}
	}
	out, err := h.supplierRepo.CreateProductVariant(r.Context(), supplierID, productID, strings.TrimSpace(body.VariantName), body.Attributes)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// POST /api/v1/supplier/products/{id}/variants/bulk {variants}
func (h *SupplierHandlers) HandleCreateProductVariantsBulk(w http.ResponseWriter, r *http.Request, productID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		Variants []json.RawMessage `json:"variants"`
	}
	if err := decodeMoneyJSON(r.Body, &body, true); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	variants := make([]repository.SupplierProductVariant, 0, len(body.Variants))
	for _, raw := range body.Variants {
		var v repository.SupplierProductVariant
		var fields map[string]interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			http.Error(w, `{"error":"invalid SKU"}`, 400)
			return
		}
		if err := json.Unmarshal(raw, &fields); err != nil {
			http.Error(w, `{"error":"invalid SKU"}`, 400)
			return
		}
		if v.Selection != nil {
			if _, ok := fields["is_active"].(bool); !ok {
				http.Error(w, `{"error":"SKU active state required"}`, 400)
				return
			}
		}
		variants = append(variants, v)
	}
	count, err := h.supplierRepo.CreateProductVariantsBulk(r.Context(), supplierID, productID, variants)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"ثبت گروهی تنوع‌ها انجام نشد."}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"count": count})
}

// PUT /api/v1/supplier/products/{id}/variants/{variantId}/cart-limits
func (h *SupplierHandlers) HandleUpdateVariantCartLimits(w http.ResponseWriter, r *http.Request, productID, variantID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		MinCartQty *int `json:"min_cart_qty"`
		MaxCartQty *int `json:"max_cart_qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if body.MinCartQty == nil || *body.MinCartQty < 1 {
		http.Error(w, `{"error":"حداقل تعداد خرید باید عددی حداقل ۱ باشد."}`, http.StatusBadRequest)
		return
	}
	if body.MaxCartQty != nil && *body.MaxCartQty < *body.MinCartQty {
		http.Error(w, `{"error":"حداکثر تعداد خرید نمی‌تواند کمتر از حداقل تعداد خرید باشد."}`, http.StatusBadRequest)
		return
	}
	out, err := h.supplierRepo.UpdateVariantCartLimits(r.Context(), supplierID, productID, variantID, body.MinCartQty, body.MaxCartQty)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// PUT /api/v1/supplier/products/{id}/variants/{vid}/pricing (gap 10) —
// sets or clears the variant's own supplier base cost, the value the
// pricing resolver has always read (product_variants.supplier_base_cost,
// NULL = inherit the product's cost) but could never be written.
// Mirrors the product pricing PUT: cost must be positive; an explicit
// {"clear": true} restores inheritance. Both "supplierBaseCost" and the
// product endpoint's "new_supplier_base_cost" field names are accepted.
//
// Governance (critic fix F1): a cost at or below the variant's
// EFFECTIVE cost (its own cost, else the product's — inheritance)
// applies directly, and so does a clear-to-inherit that lowers or
// matches the effective cost (own cost at/above the product's, or
// nothing to clear). An INCREASE goes through the same recorded
// policy matrix as a product increase
// (PricingRepository.ApplyGovernedVariantPriceIncrease): it either
// auto-applies under the effective cap / an exception grant, or is
// queued as a pending review row and answered 202. A clear whose own
// cost sits BELOW the product's is an increase in disguise — clearing
// would raise the effective cost to the product's — so it takes the
// same governed path with the product's cost as the target
// (PricingRepository.ApplyGovernedVariantClear, migration
// 202611050010), and approval applies as a clear.
func (h *SupplierHandlers) HandleUpdateVariantPricing(w http.ResponseWriter, r *http.Request, productID, variantID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		SupplierBaseCost    *int64 `json:"supplierBaseCost"`
		NewSupplierBaseCost *int64 `json:"new_supplier_base_cost"`
		Clear               bool   `json:"clear"`
		Reason              string `json:"reason"`
	}
	if err := decodeMoneyJSON(r.Body, &body, true); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	cost := body.SupplierBaseCost
	if cost == nil {
		cost = body.NewSupplierBaseCost
	}
	if cost == nil && !body.Clear {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if cost != nil && *cost <= 0 {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if cost != nil {
		_, effective, err := h.supplierRepo.GetVariantEffectiveBaseCost(r.Context(), supplierID, productID, variantID)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
				return
			}
			writeInternalError(w, err)
			return
		}
		if *cost > effective {
			h.applyGovernedVariantIncrease(w, r, supplierID, productID, variantID, *cost, body.Reason)
			return
		}
	}
	if cost == nil && body.Clear {
		own, productCost, err := h.supplierRepo.GetVariantOwnAndProductCost(r.Context(), supplierID, productID, variantID)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
				return
			}
			writeInternalError(w, err)
			return
		}
		// A clear only escapes governance when it cannot raise the
		// effective cost: nothing to clear, or the own cost already
		// meets/exceeds the product's (clearing lowers or matches).
		// Below the product's cost, clearing is a governed increase.
		if own != nil && *own < productCost {
			h.applyGovernedVariantClear(w, r, supplierID, productID, variantID, body.Reason)
			return
		}
	}
	out, err := h.supplierRepo.UpdateVariantPricing(r.Context(), supplierID, productID, variantID, cost, body.Clear, body.Reason)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// applyGovernedVariantIncrease answers a variant cost INCREASE the
// way HandleUpdatePricing answers a product one: applied -> the
// variant's post-write shape plus applied/autoApproved/requestId;
// queued -> 202 pending_review with the Persian explanation; a
// duplicate pending row for the same variant -> 409.
func (h *SupplierHandlers) applyGovernedVariantIncrease(w http.ResponseWriter, r *http.Request, supplierID, productID, variantID string, newCost int64, reason string) {
	// Without a pricing repository the increase cannot be evaluated,
	// so it is refused rather than applied ungated (mirrors the
	// product pricing handler).
	if h.pricingRepo == nil {
		http.Error(w, `{"error":"Pricing governance unavailable"}`, http.StatusInternalServerError)
		return
	}
	result, err := h.pricingRepo.ApplyGovernedVariantPriceIncrease(r.Context(), supplierID, productID, variantID, newCost, reason)
	h.writeGovernedVariantResult(w, r, supplierID, productID, variantID, result, err)
}

// applyGovernedVariantClear answers a clear-to-inherit that would
// RAISE the variant's effective cost (own cost below the product's)
// with the same governed responses as an increase; approval applies
// as a clear (see PricingRepository.ApplyGovernedVariantClear).
func (h *SupplierHandlers) applyGovernedVariantClear(w http.ResponseWriter, r *http.Request, supplierID, productID, variantID string, reason string) {
	if h.pricingRepo == nil {
		http.Error(w, `{"error":"Pricing governance unavailable"}`, http.StatusInternalServerError)
		return
	}
	result, err := h.pricingRepo.ApplyGovernedVariantClear(r.Context(), supplierID, productID, variantID, reason)
	h.writeGovernedVariantResult(w, r, supplierID, productID, variantID, result, err)
}

// writeGovernedVariantResult renders one governed variant-pricing
// outcome (an increase or a governed clear) in HandleUpdatePricing's
// shapes: applied -> the variant's post-write shape plus
// applied/autoApproved/requestId; queued -> 202 pending_review with
// the Persian explanation; duplicate pending -> 409.
func (h *SupplierHandlers) writeGovernedVariantResult(w http.ResponseWriter, r *http.Request, supplierID, productID, variantID string, result *repository.GovernedIncreaseResult, err error) {
	if err != nil {
		if errors.Is(err, repository.ErrPriceIncreaseDuplicatePending) {
			http.Error(w, `{"error":"برای این کالا یک درخواست افزایش قیمت در حال بررسی وجود دارد."}`, http.StatusConflict)
			return
		}
		if errors.Is(err, repository.ErrGovernedProductNotFound) {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if result.Applied {
		out, err := h.supplierRepo.GetSupplierVariant(r.Context(), supplierID, productID, variantID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		raw, _ := json.Marshal(out)
		resp := map[string]interface{}{}
		_ = json.Unmarshal(raw, &resp)
		resp["applied"] = true
		resp["autoApproved"] = result.AutoApproved
		if result.RequestID != "" {
			resp["requestId"] = result.RequestID
		}
		json.NewEncoder(w).Encode(resp)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   false,
		"status":    "pending_review",
		"requestId": result.RequestID,
		"pct":       result.Pct,
		"capPct":    result.CapPct,
		"reason":    result.Reason,
		"message":   priceIncreasePendingMessage(result.Reason),
	})
}

// GET /api/v1/supplier/products/{id}/labels
func (h *SupplierHandlers) HandleGetProductLabels(w http.ResponseWriter, r *http.Request, productID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	spn, labels, err := h.supplierRepo.GetProductLabels(r.Context(), supplierID, productID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if labels == nil {
		labels = []repository.SupplierVariantLabel{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"spn": spn, "labels": labels})
}

// FIX 48 — live manager-feedback modal (zl) handlers.
// GET /api/v1/supplier/catalog/review/{id}
func (h *SupplierHandlers) HandleGetCatalogReviewDetail(w http.ResponseWriter, r *http.Request, proposalID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	proposal, fields, history, err := h.supplierRepo.GetCatalogReviewDetail(r.Context(), supplierID, proposalID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"proposal not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if fields == nil {
		fields = []map[string]interface{}{}
	}
	if history == nil {
		history = []map[string]interface{}{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"proposal": proposal, "fields": fields, "history": history})
}

// POST /api/v1/supplier/catalog/review/{id}/resubmit {data}
func (h *SupplierHandlers) HandleResubmitCatalogReview(w http.ResponseWriter, r *http.Request, proposalID string) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	var body struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := decodeMoneyJSON(r.Body, &body, true); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	if len(body.Data) == 0 {
		http.Error(w, `{"error":"حداقل یک فیلد ردشده را اصلاح کنید تا ارسال مجدد انجام شود."}`, http.StatusBadRequest)
		return
	}
	if err := h.supplierRepo.ResubmitCatalogReview(r.Context(), supplierID, proposalID, body.Data); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"proposal not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"خطا در ارسال مجدد."}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// GET /api/v1/supplier/settlement/storage-fees (D12, read-only).
// This is a read of assessed storage-fee charges only; it is NOT an
// automatic settlement run (no auto settlement is implemented here).
func (h *SupplierHandlers) HandleGetSupplierStorageFees(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListSupplierStorageFees(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if list == nil {
		list = []repository.SupplierStorageFee{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"charges": list})
}

// GET /api/v1/supplier/analytics (D12).
// Shape matches the live supplier analytics tab fields
// (totalBaseAmount, totalQuantity, orderCount, returns, daily,
// products). Returns aggregation and days-left rounding are
// reconstructed and provisional; see GetSupplierAnalytics.
func (h *SupplierHandlers) HandleGetSupplierAnalytics(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	summary, err := h.supplierRepo.GetSupplierAnalytics(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// GET /api/v1/supplier/addons (D12).
func (h *SupplierHandlers) HandleGetSupplierAddons(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListSupplierAddons(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if list == nil {
		list = []repository.SupplierAddon{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"addons": list})
}

// GET /api/v1/supplier/catalog/review (D12, stub).
// TODO (U07): the live catalog-review dossier shape is unverified, and
// the supplier's own proposals are already served by the existing
// GET /supplier/products/proposals route; no duplicate guessed shape.
func (h *SupplierHandlers) HandleSupplierCatalogReview(w http.ResponseWriter, r *http.Request) {
	supplierLiveParityStub(w, r, "U07", "Catalog-review dossier shape is unverified; proposals already have their own route.")
}

// GET /api/v1/supplier/products/implicit-agreements (D12, read-only).
func (h *SupplierHandlers) HandleGetSupplierImplicitAgreements(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	list, err := h.supplierRepo.ListSupplierImplicitAgreementProducts(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// GET /api/v1/supplier/search-insight (D12).
//
// search_queries has no supplier key (migration 202610220003): a row is
// a normalized term, a store slug and result counts only. The insight
// is therefore FOOTPRINT-SCOPED in the repository: platform-wide search
// aggregates are shown to a supplier only for terms matching their own
// catalog footprint (their product titles, brands and category titles),
// and matched products are hard-filtered to the caller's own products.
// No store slug, per-store split, platform total or other supplier's
// product ever leaves this endpoint. Window: ?days=7|30|90 (else 30).
func (h *SupplierHandlers) HandleSupplierSearchInsight(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	days := 30
	switch strings.TrimSpace(r.URL.Query().Get("days")) {
	case "7":
		days = 7
	case "90":
		days = 90
	}
	insight, err := h.supplierRepo.GetSupplierSearchInsight(r.Context(), supplierID, days)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(insight)
}

// --- Admin supplier routes (D54/D15) ------------------------------------
// The exact live approve/reject route is unverified (U06): the audit
// found no /admin/suppliers/approve-style string in the live binary, so
// these routes are placed under the existing admin/suppliers family by
// reconstruction choice, documented here and in the final report. They
// call the existing repo functions, which are the confirmed behaviour.
// No response below ever includes a Tapin token: the model field is
// json:"-" and no query here selects the token column.

func supplierAdminSummaryRow(s domain.Supplier) map[string]interface{} {
	return map[string]interface{}{
		"id":                     s.ID,
		"name":                   s.Name,
		"logoUrl":                s.LogoURL,
		"rating":                 s.Rating,
		"totalProducts":          s.TotalProducts,
		"deliveryScore":          s.DeliveryScore,
		"warehouseCity":          s.WarehouseCity,
		"warehouseCityConfirmed": s.WarehouseCityConfirmed,
		"warehouseCitySource":    s.WarehouseCitySource,
		"approvalStatus":         s.ApprovalStatus,
		"signedContractVersion":  s.SignedContractVersion,
		"defaultMarkupType":      s.DefaultMarkupType,
		"defaultMarkupValue":     s.DefaultMarkupValue,
		"supportsDirectShipping": s.SupportsDirectShipping,
		"createdAt":              s.CreatedAt,
	}
}

func supplierAdminRecordRow(s domain.Supplier) map[string]interface{} {
	row := supplierAdminSummaryRow(s)
	row["approvedBy"] = s.ApprovedBy
	row["approvedAt"] = s.ApprovedAt
	row["rejectionReason"] = s.RejectionReason
	row["tapinShopId"] = s.TapinShopID
	row["tapinConnected"] = s.TapinShopID != nil && strings.TrimSpace(*s.TapinShopID) != ""
	row["description"] = s.Description
	row["badge"] = s.Badge
	// Gap 12: suspension bookkeeping, admin records surface only.
	row["suspendedAt"] = s.SuspendedAt
	row["suspendedBy"] = s.SuspendedBy
	row["suspensionReason"] = s.SuspensionReason
	return row
}

func (h *SupplierHandlers) handleAdminSuppliersDetail(w http.ResponseWriter, r *http.Request, records bool) {
	w.Header().Set("Content-Type", "application/json")
	// PERF-02 Step 7: the ?status= filter now applies IN SQL (filtering in
	// Go after loading the whole list would break paging).
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	opts, perr := parseListPage(r)
	if perr != nil {
		http.Error(w, `{"error":"Invalid limit"}`, http.StatusBadRequest)
		return
	}
	page, err := h.supplierRepo.ListSuppliersForAdminDetailPage(r.Context(), status, opts)
	if err != nil {
		if isInvalidCursorError(err) {
			http.Error(w, `{"error":"cursor is invalid"}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	list := page.Items
	// Gap 13: supportsDirectShipping is emitted DERIVED from the
	// shipping-method toggles, loaded here in ONE batched query for
	// the whole list (the row builders are pure functions of the
	// stored record, so the handler overrides their stored value).
	// A failed load leaves the stored value in place.
	derivedBySupplier := map[string]bool{}
	{
		ids := make([]string, 0, len(list))
		for _, sup := range list {
			ids = append(ids, sup.ID)
		}
		if batch, merr := h.supplierRepo.ListShippingMethodsBySupplierIDs(r.Context(), ids); merr == nil {
			for id, methods := range batch {
				derivedBySupplier[id] = methods["direct"] == "enabled"
			}
			for _, sup := range list {
				if _, ok := batch[sup.ID]; !ok {
					derivedBySupplier[sup.ID] = false
				}
			}
		}
	}
	rows := []map[string]interface{}{}
	for _, s := range list {
		var row map[string]interface{}
		if records {
			row = supplierAdminRecordRow(s)
		} else {
			row = supplierAdminSummaryRow(s)
		}
		if derived, ok := derivedBySupplier[s.ID]; ok {
			row["supportsDirectShipping"] = derived
		}
		rows = append(rows, row)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"suppliers": rows, "nextCursor": page.NextCursor, "hasMore": page.HasMore, "limit": page.Limit})
}

// GET /api/v1/admin/suppliers/list (D15). Optional ?status= filter, e.g.
// pending_approval, approved, rejected.
func (h *SupplierHandlers) HandleAdminSuppliersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	h.handleAdminSuppliersDetail(w, r, false)
}

// GET /api/v1/admin/suppliers/records (D15). Same real data as /list, plus
// the record fields (approvedBy/approvedAt/rejectionReason/tapinShopId).
// TODO (U07): the live distinction between /list and /records is
// unverified; this split (summary vs full record) is a reconstruction
// choice, not a claimed live shape.
func (h *SupplierHandlers) HandleAdminSuppliersRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	h.handleAdminSuppliersDetail(w, r, true)
}

// GET /api/v1/admin/suppliers/pending (D54). The existing repo function,
// previously unreachable from HTTP.
func (h *SupplierHandlers) HandleAdminSuppliersPending(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	opts, perr := parseListPage(r)
	if perr != nil {
		http.Error(w, `{"error":"Invalid limit"}`, http.StatusBadRequest)
		return
	}
	page, err := h.supplierRepo.ListPendingSuppliersPage(r.Context(), opts)
	if err != nil {
		if isInvalidCursorError(err) {
			http.Error(w, `{"error":"cursor is invalid"}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	list := page.Items
	rows := []map[string]interface{}{}
	for _, s := range list {
		rows = append(rows, map[string]interface{}{
			"id":             s.ID,
			"name":           s.Name,
			"warehouseCity":  s.WarehouseCity,
			"approvalStatus": s.ApprovalStatus,
			"createdAt":      s.CreatedAt,
		})
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"suppliers": rows, "nextCursor": page.NextCursor, "hasMore": page.HasMore, "limit": page.Limit})
}

func supplierAdminReviewError(w http.ResponseWriter, err error) {
	if err != nil && strings.Contains(err.Error(), "already processed") {
		http.Error(w, `{"error":"Supplier not found or already processed"}`, http.StatusConflict)
		return
	}
	writeInternalError(w, err)
}

// Item 6 (admin supplier detail, 2026-11-13): GET
// /api/v1/admin/suppliers/:id — one supplier, fully enriched. The
// route is a bare-segment branch INSIDE the existing sub-router
// closure in main.go (no new mux pattern, so no route-table conflict
// risk); the RequireRoles(admin) gate is inherited from the closure.
//
// Every figure comes from a real query. Supplementary sections are
// best-effort: a failed load renders as an empty array / null and is
// named in "gaps", never as fabricated data. Stored
// totalProducts/rating/deliveryScore are returned as stored (the
// record row); the live-computed figures are under "counts".
// Finance is in TOMAN: Earned sums wholesale_cost (Toman prices),
// storage fees read amount_toman, and the payout gate compares
// Available against payout_requests.amount in the same unit.
// tapin_token is never selected or returned: connected is a strict
// boolean (shop id AND token both stored).
func (h *SupplierHandlers) HandleAdminSupplierDetail(w http.ResponseWriter, r *http.Request, supplierID string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if strings.TrimSpace(supplierID) == "" {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	sup, err := h.supplierRepo.GetSupplierDetailByID(ctx, supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sup == nil {
		http.NotFound(w, r)
		return
	}

	// GAP (honest, unrecoverable): ApproveSupplier and RejectSupplier
	// write NO audit_logs row, so approve/reject history cannot be
	// reconstructed. Only suspend/reactivate appear in "history".
	gaps := []string{
		"سابقهٔ تأیید/رد در دسترس نیست: تأیید و رد تأمین‌کننده در سوابق ثبت نمی‌شود و قابل بازیابی نیست؛ فقط تعلیق و فعال‌سازی مجدد در تاریخچه دیده می‌شود.",
	}

	supplierRow := supplierAdminRecordRow(*sup)

	owner, err := h.supplierRepo.GetSupplierOwnerContact(ctx, supplierID)
	if err != nil {
		gaps = append(gaps, "owner")
	} else if owner != nil {
		supplierRow["owner"] = owner
	} else {
		supplierRow["owner"] = nil
	}

	warehouses, err := h.supplierRepo.ListSupplierWarehouses(ctx, supplierID)
	if err != nil {
		gaps = append(gaps, "warehouses")
		warehouses = []repository.SupplierWarehouse{}
	}
	members, err := h.supplierRepo.ListMembers(ctx, supplierID)
	if err != nil {
		gaps = append(gaps, "members")
		members = []domain.SupplierMember{}
	}
	methods, err := h.supplierRepo.ListSupplierShippingMethods(ctx, supplierID)
	if err != nil {
		gaps = append(gaps, "shippingMethods")
		methods = []repository.SupplierShippingMethod{}
	}
	// supportsDirectShipping is DERIVED from the direct method toggle,
	// same as the list handler — the stored column is not trusted.
	for _, m := range methods {
		if m.Method == "direct" {
			supplierRow["supportsDirectShipping"] = m.Status == "enabled"
		}
	}

	counts, err := h.supplierRepo.GetSupplierDetailCounts(ctx, supplierID)
	if err != nil {
		gaps = append(gaps, "counts")
		counts = &repository.SupplierDetailCounts{Orders: repository.SupplierDetailOrderCounts{DirectShipmentsByStatus: map[string]int{}}}
	}

	history, err := h.supplierRepo.GetSupplierAuditHistory(ctx, supplierID)
	if err != nil {
		gaps = append(gaps, "history")
		history = []repository.SupplierAuditHistoryRow{}
	}

	// Tapin: strict boolean, shop AND token. Prefer the attached
	// TapinRepository (wired in main); fall back to the supplier-repo
	// check (same rule, token never leaves the DB) when it is nil,
	// e.g. in handler tests that construct SupplierHandlers directly.
	tapinShopID := ""
	tapinConnected := false
	if h.tapinRepo != nil {
		if creds, cerr := h.tapinRepo.GetSupplierCredentials(ctx, supplierID); cerr == nil {
			tapinShopID = creds.ShopID
			tapinConnected = creds.Connected()
		} else {
			gaps = append(gaps, "tapin")
		}
	} else if shop, connected, serr := h.supplierRepo.GetSupplierTapinStatus(ctx, supplierID); serr == nil {
		tapinShopID = shop
		tapinConnected = connected
	} else {
		gaps = append(gaps, "tapin")
	}
	// The record row's tapinConnected is shop-id-only; the strict
	// (shop AND token) value wins here.
	supplierRow["tapinConnected"] = tapinConnected

	// Finance, in TOMAN (see header comment). Balance errors leave a
	// null balance rather than a fabricated zero.
	var balance interface{}
	if bal, berr := h.supplierRepo.GetSettlementBalance(ctx, supplierID); berr == nil {
		balance = bal
	} else {
		gaps = append(gaps, "financeBalance")
	}
	settlements, err := h.supplierRepo.GetSettlementHistory(ctx, supplierID)
	if err != nil {
		gaps = append(gaps, "settlements")
		settlements = []map[string]interface{}{}
	}

	// Contract: the supplier's signed version vs the platform-current
	// version. No per-supplier version history exists — the response
	// carries only the two current numbers.
	var platformContractVersion interface{}
	if v, verr := h.supplierRepo.GetSupplierContractVersion(ctx); verr == nil {
		platformContractVersion = v
	} else {
		gaps = append(gaps, "platformContractVersion")
	}
	supplierRow["platformContractVersion"] = platformContractVersion

	json.NewEncoder(w).Encode(map[string]interface{}{
		"supplier":        supplierRow,
		"counts":          counts,
		"warehouses":      warehouses,
		"members":         members,
		"shippingMethods": methods,
		"tapin": map[string]interface{}{
			"shopId":    tapinShopID,
			"connected": tapinConnected,
		},
		"finance": map[string]interface{}{
			"currencyUnit": "TOMAN",
			"balance":      balance,
			"settlements":  settlements,
		},
		"history": history,
		"gaps":    gaps,
	})
}

// POST /api/v1/admin/suppliers/:id/approve (D54, route choice per U06).
// The approver is the admin's own user id from the JWT claims.
func (h *SupplierHandlers) HandleAdminSupplierApprove(w http.ResponseWriter, r *http.Request, supplierID string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	userID, err := h.supplierRepo.ApproveSupplier(r.Context(), supplierID, supplierLiveParityClaimsUserID(r))
	if err != nil {
		supplierAdminReviewError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"supplierId": supplierID,
		"userId":     userID,
		"status":     "approved",
	})
}

// POST /api/v1/admin/suppliers/:id/reject (D54, route choice per U06).
// Body: {"reason": "..."} (also accepts "rejectionReason").
func (h *SupplierHandlers) HandleAdminSupplierReject(w http.ResponseWriter, r *http.Request, supplierID string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var req struct {
		Reason          string `json:"reason"`
		RejectionReason string `json:"rejectionReason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = strings.TrimSpace(req.RejectionReason)
	}
	if err := h.supplierRepo.RejectSupplier(r.Context(), supplierID, supplierLiveParityClaimsUserID(r), reason); err != nil {
		supplierAdminReviewError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"supplierId": supplierID,
		"status":     "rejected",
		"reason":     reason,
	})
}

// supplierSuspensionError maps the gap-12 repo sentinels to 409:
// a wrong-state transition and the protected central record are both
// conflicts, never 500s.
func supplierSuspensionError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrSupplierStateConflict) || errors.Is(err, repository.ErrCentralSupplierProtected) {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusConflict)
		return
	}
	writeInternalError(w, err)
}

// POST /api/v1/admin/suppliers/:id/suspend (gap 12).
// Body: {"reason": "..."} (also accepts "suspensionReason").
func (h *SupplierHandlers) HandleAdminSupplierSuspend(w http.ResponseWriter, r *http.Request, supplierID string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var req struct {
		Reason           string `json:"reason"`
		SuspensionReason string `json:"suspensionReason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = strings.TrimSpace(req.SuspensionReason)
	}
	if err := h.supplierRepo.SuspendSupplier(r.Context(), supplierID, supplierLiveParityClaimsUserID(r), reason); err != nil {
		supplierSuspensionError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"supplierId": supplierID,
		"status":     "suspended",
		"reason":     reason,
	})
}

// POST /api/v1/admin/suppliers/:id/reactivate (gap 12).
func (h *SupplierHandlers) HandleAdminSupplierReactivate(w http.ResponseWriter, r *http.Request, supplierID string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if err := h.supplierRepo.ReactivateSupplier(r.Context(), supplierID, supplierLiveParityClaimsUserID(r)); err != nil {
		supplierSuspensionError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"supplierId": supplierID,
		"status":     "approved",
	})
}

// GET/POST /api/v1/admin/suppliers/shipping-methods (D15).
// GET returns all method rows. POST decides a method:
// {"supplierId": "...", "method": "direct"|"warehouse",
//
//	"status": "enabled"|"disabled"} — admin-only per migration 202610170012.
//
// TODO (U07): the live decide body/route shape is unverified.
func (h *SupplierHandlers) HandleAdminSuppliersShippingMethods(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		list, err := h.supplierRepo.ListAllSupplierShippingMethods(r.Context())
		if err != nil {
			writeInternalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(list)
	case http.MethodPost:
		var req struct {
			SupplierID string `json:"supplierId"`
			Method     string `json:"method"`
			Status     string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
			return
		}
		if err := h.supplierRepo.DecideSupplierShippingMethod(r.Context(), strings.TrimSpace(req.SupplierID), strings.TrimSpace(req.Method), strings.TrimSpace(req.Status), supplierLiveParityClaimsUserID(r)); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"supplierId": strings.TrimSpace(req.SupplierID),
			"method":     strings.TrimSpace(req.Method),
			"status":     strings.TrimSpace(req.Status),
		})
	default:
		writeMethodNotAllowed(w)
	}
}

// GET /api/v1/admin/store-intel/suppliers (D15, supplier part only).
// TODO (U07): the live store-intel shape is unverified; this returns the
// same real supplier summary rows as /admin/suppliers/list rather than a
// guessed intel shape.
func (h *SupplierHandlers) HandleAdminStoreIntelSuppliers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	h.handleAdminSuppliersDetail(w, r, false)
}

// --- Admin price-increase governance (gap 11) ---------------------------
// Review queue + per-product exception grants for the policy recorded in
// migrations 202610310003 + 202610310006. All state changes run through
// PricingRepository in single transactions with audit_logs rows
// (operator "admin:<userID>", category 'finance').

func adminClaimsUserID(r *http.Request) string {
	if claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
		return claims.UserID
	}
	return ""
}

// GET /api/v1/admin/price-increases?status=pending — the review queue.
// status may be pending | approved | rejected | all (default pending).
func (h *SupplierHandlers) HandleAdminPriceIncreases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if h.pricingRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "pending"
	}
	list, err := h.pricingRepo.ListAdminPriceIncreases(r.Context(), status)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// POST /api/v1/admin/price-increases/{id}/review {approved} — approve
// applies the queued reference (with the stale-cost guard) or rejects.
// Both decisions stamp reviewed_at and write an audit row.
func (h *SupplierHandlers) HandleAdminReviewPriceIncrease(w http.ResponseWriter, r *http.Request, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if h.pricingRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		Approved bool `json:"approved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	row, err := h.pricingRepo.ReviewPriceIncrease(r.Context(), requestID, req.Approved, adminClaimsUserID(r))
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrPriceIncreaseNotFound):
			http.Error(w, `{"error":"Price increase request not found"}`, http.StatusNotFound)
		case errors.Is(err, repository.ErrPriceIncreaseNotPending):
			http.Error(w, `{"error":"Price increase request already reviewed"}`, http.StatusConflict)
		case errors.Is(err, repository.ErrPriceIncreaseStaleCost):
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusConflict)
		default:
			writeInternalError(w, err)
		}
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      row.ID,
		"status":  row.Status,
	})
}

// POST /api/v1/admin/products/price-exception/{productId}
// {exceptionCapPct, ticketId?} — create or replace the product's
// exception grant. DELETE on the same path revokes it.
func (h *SupplierHandlers) HandleAdminProductPriceException(w http.ResponseWriter, r *http.Request, productID string) {
	w.Header().Set("Content-Type", "application/json")
	if h.pricingRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	adminUserID := adminClaimsUserID(r)
	switch r.Method {
	case http.MethodPost:
		var req struct {
			ExceptionCapPct float64 `json:"exceptionCapPct"`
			TicketID        *string `json:"ticketId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ExceptionCapPct <= 0 {
			http.Error(w, `{"error":"exceptionCapPct must be positive"}`, http.StatusBadRequest)
			return
		}
		if err := h.pricingRepo.UpsertProductPriceException(r.Context(), productID, req.ExceptionCapPct, req.TicketID, adminUserID); err != nil {
			if errors.Is(err, repository.ErrProductNotFound) {
				http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
				return
			}
			writeInternalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "productId": productID, "exceptionCapPct": req.ExceptionCapPct})
	case http.MethodDelete:
		if err := h.pricingRepo.DeleteProductPriceException(r.Context(), productID, adminUserID); err != nil {
			if errors.Is(err, repository.ErrPriceExceptionNotFound) {
				http.Error(w, `{"error":"No price exception grant exists for this product"}`, http.StatusNotFound)
				return
			}
			writeInternalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "productId": productID})
	default:
		writeMethodNotAllowed(w)
	}
}
