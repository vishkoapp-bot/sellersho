package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/bank"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
)

type SupplierHandlers struct {
	supplierRepo                  *repository.SupplierRepository
	capabilityRepo                *repository.CapabilityRepository
	pricingRepo                   *repository.PricingRepository
	tapinRepo                     *repository.TapinRepository
	jwtService                    *auth.JWTService
	networkVolumeIncentiveEnabled bool
}

func NewSupplierHandlers(supplierRepo *repository.SupplierRepository, jwtService *auth.JWTService, networkVolumeIncentiveEnabled bool, capabilityRepos ...*repository.CapabilityRepository) *SupplierHandlers {
	var capabilityRepo *repository.CapabilityRepository
	if len(capabilityRepos) > 0 {
		capabilityRepo = capabilityRepos[0]
	}
	return &SupplierHandlers{
		supplierRepo:                  supplierRepo,
		capabilityRepo:                capabilityRepo,
		jwtService:                    jwtService,
		networkVolumeIncentiveEnabled: networkVolumeIncentiveEnabled,
	}
}

// WithPricingRepository attaches the pricing repository used by the
// governed price-increase path (gap 11) and the admin price-increase
// queue. It is a setter rather than a constructor argument so existing
// call sites (and tests) keep compiling unchanged.
func (h *SupplierHandlers) WithPricingRepository(pricingRepo *repository.PricingRepository) *SupplierHandlers {
	h.pricingRepo = pricingRepo
	return h
}

// WithTapinRepository attaches the Tapin repository used by the
// supplier Tapin boxes endpoint (gaps 1-3). Setter rather than a
// constructor argument, same rationale as WithPricingRepository.
func (h *SupplierHandlers) WithTapinRepository(tapinRepo *repository.TapinRepository) *SupplierHandlers {
	h.tapinRepo = tapinRepo
	return h
}

// 1. POST /api/v1/auth/supplier/register
func (h *SupplierHandlers) HandleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		CompanyName   string `json:"company_name"`
		ContactPerson string `json:"contact_person"`
		Username      string `json:"username"`
		Password      string `json:"password"`
		Phone         string `json:"phone"`
		Email         string `json:"email"`
		WarehouseCity string `json:"warehouse_city"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error":"Username and password required"}`, http.StatusBadRequest)
		return
	}

	user, supplier, err := h.supplierRepo.RegisterSupplier(
		r.Context(), req.CompanyName, req.ContactPerson, req.Username, req.Password, req.Phone, req.Email, req.WarehouseCity,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	capabilities := []string{"supplier"}
	if h.capabilityRepo != nil {
		if err := h.capabilityRepo.Grant(r.Context(), user.ID, "supplier", "supplier_registration", supplier.ID, nil); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		var capabilityErr error
		capabilities, capabilityErr = h.capabilityRepo.ListActive(r.Context(), user.ID)
		if capabilityErr != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, capabilityErr.Error()), http.StatusInternalServerError)
			return
		}
	}
	selectedRole := "supplier"
	token, tokenErr := h.jwtService.GenerateTokenWithCapabilities(user.ID, user.Mobile, user.FullName, selectedRole, capabilities, 7*24*time.Hour)
	if tokenErr != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, tokenErr.Error()), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"token":        token,
		"user":         user,
		"supplier":     supplier,
		"capabilities": capabilities,
	})
}

func getSupplierIDFromContext(r *http.Request, repo *repository.SupplierRepository) string {
	if supplierID, ok := r.Context().Value(supplierIDKey).(string); ok {
		return supplierID
	}
	claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if ok && claims != nil && claims.UserID != "" && repo != nil {
		sup, err := repo.GetSupplierByUserID(r.Context(), claims.UserID)
		if err == nil && sup != nil {
			return sup.ID
		}
	}
	return ""
}

// derivedSupportsDirectShipping (gap 13) is the ONLY value DTOs
// emit for supportsDirectShipping: the capability is DERIVED from
// the supplier's shipping-method toggles (direct == enabled), the
// same source routing.ResolveItemRouting reads. The stored
// suppliers.supports_direct_shipping column is inert legacy — its
// write path (HandleUpdateShippingSettings +
// UpdateDirectShippingSetting) is removed — and is used here only
// as the fallback when the toggles cannot be loaded.
func derivedSupportsDirectShipping(ctx context.Context, repo *repository.SupplierRepository, supplierID string, stored bool) bool {
	if repo == nil || supplierID == "" {
		return stored
	}
	methods, err := repo.ListSupplierShippingMethods(ctx, supplierID)
	if err != nil {
		return stored
	}
	for _, m := range methods {
		if m.Method == "direct" {
			return m.Status == "enabled"
		}
	}
	return false
}

// 3. GET /api/v1/supplier/profile
func (h *SupplierHandlers) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var userID string
	if claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
		userID = claims.UserID
	}
	var sup *domain.Supplier
	var err error
	if scopedID, ok := r.Context().Value(supplierIDKey).(string); ok {
		sup, err = h.supplierRepo.GetSupplierByID(r.Context(), scopedID)
	} else if userID != "" {
		sup, err = h.supplierRepo.GetSupplierByUserID(r.Context(), userID)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sup == nil {
		http.Error(w, `{"error":"Supplier profile not found"}`, http.StatusNotFound)
		return
	}

	_, permissions, _, accessErr := h.supplierRepo.GetSupplierAccess(r.Context(), userID)
	if accessErr != nil {
		writeInternalError(w, accessErr)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"permissions":              permissions,
		"id":                       sup.ID,
		"name":                     sup.Name,
		"company_name":             sup.Name,
		"rating":                   sup.Rating,
		"total_products":           sup.TotalProducts,
		"delivery_score":           sup.DeliveryScore,
		"warehouse_city":           sup.WarehouseCity,
		"warehouse_city_confirmed": sup.WarehouseCityConfirmed,
		"logo_url":                 sup.LogoURL,
		"description":              sup.Description,
		"badge":                    sup.Badge,
		"supportsDirectShipping":   derivedSupportsDirectShipping(r.Context(), h.supplierRepo, sup.ID, sup.SupportsDirectShipping),
		"approvalStatus":           sup.ApprovalStatus,
		"signedContractVersion":    sup.SignedContractVersion,
		"defaultMarkupType":        sup.DefaultMarkupType,
		"defaultMarkupValue":       sup.DefaultMarkupValue,
		"warehouseCityConfirmed":   sup.WarehouseCityConfirmed,
		"warehouseCitySource":      sup.WarehouseCitySource,
		"tapinShopId":              sup.TapinShopID,
		"tapinConnected":           sup.TapinShopID != nil && *sup.TapinShopID != "",
	})
}

// proposalMeasurementInt reads one measurement field from a propose
// payload. JSON numbers decode as float64; plain ints are accepted too
// (dossier tooling). Missing, non-numeric or non-positive values report
// as absent — the form's rule is "present and > 0" for all four fields.
func proposalMeasurementInt(payload map[string]interface{}, keys ...string) (int, bool) {
	for _, key := range keys {
		switch v := payload[key].(type) {
		case float64:
			if v > 0 {
				return int(v), true
			}
		case int:
			if v > 0 {
				return v, true
			}
		case int64:
			if v > 0 {
				return int(v), true
			}
		case json.Number:
			if n, err := v.Int64(); err == nil && n > 0 {
				return int(n), true
			}
		}
	}
	return 0, false
}

// missingProposalMeasurements returns the Persian names of the required
// proposal measurements that are absent or non-positive (gap 8).
func missingProposalMeasurements(payload map[string]interface{}) []string {
	var missing []string
	if _, ok := proposalMeasurementInt(payload, "weightGrams", "weight_grams"); !ok {
		missing = append(missing, "وزن")
	}
	if _, ok := proposalMeasurementInt(payload, "lengthMm", "length_mm"); !ok {
		missing = append(missing, "طول")
	}
	if _, ok := proposalMeasurementInt(payload, "widthMm", "width_mm"); !ok {
		missing = append(missing, "عرض")
	}
	if _, ok := proposalMeasurementInt(payload, "heightMm", "height_mm"); !ok {
		missing = append(missing, "ارتفاع")
	}
	return missing
}

// 4. POST /api/v1/supplier/products/propose
func (h *SupplierHandlers) HandleProposeProduct(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if raw, exists := payload["variants"]; exists {
		list, ok := raw.([]interface{})
		if !ok || len(list) == 0 || len(list) > 24 {
			http.Error(w, `{"error":"Invalid variants"}`, http.StatusBadRequest)
			return
		}
		seen := map[string]bool{}
		for _, item := range list {
			v, ok := item.(map[string]interface{})
			if !ok {
				http.Error(w, `{"error":"Invalid variant"}`, 400)
				return
			}
			name, _ := v["variant_name"].(string)
			price, pok := v["supplier_base_cost"].(float64)
			stock, sok := v["stock"].(float64)
			if strings.TrimSpace(name) == "" || seen[strings.TrimSpace(name)] || !pok || price <= 0 || price != float64(int64(price)) || !sok || stock < 0 || stock != float64(int64(stock)) {
				http.Error(w, `{"error":"Each variant requires a unique name, positive integer price and nonnegative integer stock"}`, 400)
				return
			}
			seen[strings.TrimSpace(name)] = true
		}
	}

	payload["supplierId"] = supplierID
	payload["supplier_id"] = supplierID

	prodID := fmt.Sprintf("prod-%d", time.Now().UnixNano()/1e6)
	title, _ := payload["title"].(string)
	slug, _ := payload["slug"].(string)
	if slug == "" {
		slug = prodID
	}
	catSlug, _ := payload["categorySlug"].(string)
	// FIX 51 — live's category picker (Rl) can select any category in the
	// tree, not just five top-level slugs; accept any non-empty slug.
	if strings.TrimSpace(catSlug) == "" {
		catSlug = "digital"
	}
	if supplierID == "" {
		http.Error(w, `{"error":"Supplier required"}`, http.StatusForbidden)
		return
	}
	var cost int64
	switch v := payload["supplierBaseCost"].(type) {
	case float64:
		cost = int64(v)
	case int64:
		cost = v
	}
	var stock int
	switch v := payload["stock"].(type) {
	case float64:
		stock = int(v)
	case int:
		stock = v
	}
	fulfillmentMethod, _ := payload["fulfillmentMethod"].(string)
	if fulfillmentMethod == "" {
		// FIX 51 — live's form defaults to central-warehouse fulfillment.
		fulfillmentMethod = "warehouse"
	}
	payload["fulfillmentMethod"] = fulfillmentMethod
	if strings.TrimSpace(title) == "" || len(title) > 500 || cost <= 0 || stock < 0 {
		http.Error(w, `{"error":"Title, positive cost and nonnegative stock required"}`, http.StatusBadRequest)
		return
	}

	// Gap 8 (supplier-flow audit): the measurements gate used to be
	// frontend-only — the form refuses to submit without weight + all
	// three dimensions («بدون آن‌ها پیشنهاد تأیید نمی‌شود»), but this
	// handler never checked, so non-form proposals reached approval with
	// no measurements and were born unmeasurable (putaway then blocks
	// until measurement_status='verified'). Enforce the same rule here,
	// naming what is missing. Values arrive exactly as the form sends
	// them: weightGrams (grams), lengthMm/widthMm/heightMm (millimetres).
	if missing := missingProposalMeasurements(payload); len(missing) > 0 {
		msg := "وزن و ابعاد کالا ناقص است؛ این موارد الزامی‌اند و وارد نشده‌اند: " + strings.Join(missing, "، ") +
			". وزن (گرم) و هر سه بُعد کالا (طول، عرض و ارتفاع) الزامی است؛ بدون آن‌ها پیشنهاد تأیید نمی‌شود."
		body, _ := json.Marshal(map[string]string{"error": msg})
		http.Error(w, string(body), http.StatusBadRequest)
		return
	}

	imageUrl, _ := payload["imageUrl"].(string)
	if imageUrl == "" {
		imageUrl, _ = payload["image_url"].(string)
	}
	if imageUrl == "" {
		imageUrl, _ = payload["image"].(string)
	}
	if imageUrl != "" {
		trimmed := strings.TrimSpace(imageUrl)
		if !strings.HasPrefix(trimmed, "http://") &&
			!strings.HasPrefix(trimmed, "https://") &&
			!strings.HasPrefix(trimmed, "/static/") &&
			!strings.HasPrefix(trimmed, "/") &&
			!strings.HasPrefix(trimmed, "data:image/") {
			http.Error(w, `{"error":"Invalid image URL format"}`, http.StatusBadRequest)
			return
		}
		imageUrl = trimmed
		payload["imageUrl"] = imageUrl
		payload["image_url"] = imageUrl
	}

	payload["product_id"] = prodID
	proposalID, err := h.supplierRepo.ProposeProduct(r.Context(), supplierID, payload)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	description, _ := payload["description"].(string)

	var specsMap map[string]interface{}
	if rawSpecs, ok := payload["specs"].(map[string]interface{}); ok {
		specsMap = rawSpecs
	} else {
		specsMap = make(map[string]interface{})
	}

	warehouseName, _ := payload["warehouse"].(string)
	if warehouseName == "" {
		warehouseName, _ = payload["warehouseName"].(string)
	}
	if warehouseName == "" {
		warehouseName = "انبار مرکزی تهران"
	}

	prepDays := 1
	switch v := payload["preparationDays"].(type) {
	case float64:
		prepDays = int(v)
	case int:
		prepDays = v
	}

	isOriginal := true
	if orig, ok := payload["isOriginal"].(bool); ok {
		isOriginal = orig
	}

	prod := domain.Product{
		ID:                prodID,
		Title:             title,
		Slug:              slug,
		CategorySlug:      catSlug,
		SupplierID:        supplierID,
		SupplierBaseCost:  cost,
		OriginalPrice:     cost + 500000,
		PlatformMargin:    100000,
		Stock:             stock,
		IsOriginal:        isOriginal,
		FulfillmentMethod: fulfillmentMethod,
		ImageURL:          imageUrl,
		Description:       description,
		Specs:             specsMap,
		WarehouseName:     warehouseName,
		PreparationDays:   prepDays,
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"proposal_id": proposalID,
		"product_id":  prodID,
		"image_url":   imageUrl,
		"id":          proposalID,
		"status":      "pending",
		"product": map[string]interface{}{
			"id":                prod.ID,
			"title":             prod.Title,
			"slug":              prod.Slug,
			"categorySlug":      prod.CategorySlug,
			"supplierId":        prod.SupplierID,
			"supplierBaseCost":  prod.SupplierBaseCost,
			"stock":             prod.Stock,
			"fulfillmentMethod": prod.FulfillmentMethod,
			"imageUrl":          imageUrl,
			"description":       description,
			"specs":             specsMap,
			"warehouse":         warehouseName,
			"preparationDays":   prepDays,
			"isOriginal":        isOriginal,
		},
	})
}

// 5. GET /api/v1/supplier/products/proposals
func (h *SupplierHandlers) HandleGetProposals(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)

	proposals, err := h.supplierRepo.GetSupplierProposals(r.Context(), supplierID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if proposals == nil {
		proposals = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(proposals)
}

// 6. POST /api/v1/admin/products/approve/:proposal_id
//
// GAP 20: the {approved} body field is honoured. An explicit
// {"approved": false} (with an optional "reason", falling back to
// "notes") runs the proposal rejection path instead of approving;
// {"approved": true} — and every pre-existing caller that sends no
// field at all — keeps the original approve behaviour, so the field
// is a *bool to keep "absent" distinct from an explicit false.
func (h *SupplierHandlers) HandleApproveProposal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(r.URL.Path, "/")
	proposalID := parts[len(parts)-1]

	var req struct {
		Approved *bool  `json:"approved"`
		Notes    string `json:"notes"`
		Reason   string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Approved != nil && !*req.Approved {
		reason := req.Reason
		if reason == "" {
			reason = req.Notes
		}
		if err := h.supplierRepo.RejectProposal(r.Context(), proposalID, reason); err != nil {
			if errors.Is(err, repository.ErrProposalNotPending) {
				http.Error(w, `{"error":"Proposal is not pending"}`, http.StatusConflict)
				return
			}
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"status":  "rejected",
		})
		return
	}

	productID, err := h.supplierRepo.ApproveProposal(r.Context(), proposalID, req.Notes)
	if err != nil {
		if errors.Is(err, repository.ErrProposalNotPending) {
			http.Error(w, `{"error":"Proposal is not pending"}`, http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"product_id": productID,
		"status":     "approved",
	})
}

// 7. GET /api/v1/supplier/products
func (h *SupplierHandlers) HandleGetProducts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)

	products, err := h.supplierRepo.GetSupplierProducts(r.Context(), supplierID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if products == nil {
		products = []domain.Product{}
	}
	json.NewEncoder(w).Encode(products)
}

func (h *SupplierHandlers) HandleGetReturns(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	list, err := h.supplierRepo.GetReturnRequests(r.Context(), getSupplierIDFromContext(r, h.supplierRepo))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(list)
}
func (h *SupplierHandlers) HandleCreateReturn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		ProductID        string `json:"productId"`
		Quantity         int    `json:"quantity"`
		CollectionMethod string `json:"collectionMethod"`
		Reason           string `json:"reason"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, `{"error":"Invalid payload"}`, 400)
		return
	}
	item, err := h.supplierRepo.CreateReturnRequestWithIdempotency(r.Context(), getSupplierIDFromContext(r, h.supplierRepo), req.ProductID, req.Quantity, req.CollectionMethod, req.Reason, r.Header.Get("Idempotency-Key"), callerUserID(r))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

func callerUserID(r *http.Request) string {
	if claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
		return claims.UserID
	}
	return ""
}

func (h *SupplierHandlers) HandleCancelReturn(w http.ResponseWriter, r *http.Request) {
	returnID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/returns/"), "/cancel")
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	if err := h.supplierRepo.CancelReturn(r.Context(), getSupplierIDFromContext(r, h.supplierRepo), returnID, req.Reason, callerUserID(r)); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "returnId": returnID, "status": "cancelled"})
}
func (h *SupplierHandlers) HandleGetWarranties(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	list, err := h.supplierRepo.GetWarrantyProperties(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	json.NewEncoder(w).Encode(list)
}
func (h *SupplierHandlers) HandleRequestWarranty(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		ProductID, Name, Description string
		Images                       []string `json:"images"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, `{"error":"Invalid payload"}`, 400)
		return
	}
	id, err := h.supplierRepo.RequestWarrantyProperty(r.Context(), getSupplierIDFromContext(r, h.supplierRepo), req.ProductID, req.Name, req.Description, req.Images)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), 400)
		return
	}
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": id, "status": "pending"})
}

// 8. PUT /api/v1/supplier/products/:id/stock
func (h *SupplierHandlers) HandleUpdateStock(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/stock"), "/")
	productID := parts[len(parts)-1]

	var req struct {
		NewStock int    `json:"new_stock"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewStock < 0 {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	err := h.supplierRepo.UpdateProductStock(r.Context(), getSupplierIDFromContext(r, h.supplierRepo), productID, req.NewStock, req.Reason)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"stock":   req.NewStock,
	})
}

// 9. PUT /api/v1/supplier/products/:id/pricing
// Gap 11: price DECREASES (and unchanged prices) take the direct path
// exactly as before. An INCREASE is governed by the recorded policy
// (migrations 202610310003 + 202610310006): it either auto-applies under
// the effective cap / an exception grant, or is queued as a pending
// review row and answered 202 with the Persian explanation.
func (h *SupplierHandlers) HandleUpdatePricing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/pricing"), "/")
	productID := parts[len(parts)-1]

	var req struct {
		NewSupplierBaseCost int64  `json:"new_supplier_base_cost"`
		Reason              string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewSupplierBaseCost <= 0 {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	supplierID := getSupplierIDFromContext(r, h.supplierRepo)
	currentCost, err := h.supplierRepo.GetSupplierProductBaseCost(r.Context(), supplierID, productID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	if req.NewSupplierBaseCost <= currentCost {
		err := h.supplierRepo.UpdateProductPricing(r.Context(), supplierID, productID, req.NewSupplierBaseCost, req.Reason)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":            true,
			"supplier_base_cost": req.NewSupplierBaseCost,
		})
		return
	}

	// Governed increase path. Without a pricing repository the increase
	// cannot be evaluated, so it is refused rather than applied ungated.
	if h.pricingRepo == nil {
		http.Error(w, `{"error":"Pricing governance unavailable"}`, http.StatusInternalServerError)
		return
	}
	result, err := h.pricingRepo.ApplyGovernedPriceIncrease(r.Context(), supplierID, productID, req.NewSupplierBaseCost, req.Reason)
	if err != nil {
		if errors.Is(err, repository.ErrPriceIncreaseDuplicatePending) {
			http.Error(w, `{"error":"برای این کالا یک درخواست افزایش قیمت در حال بررسی وجود دارد."}`, http.StatusConflict)
			return
		}
		if errors.Is(err, repository.ErrGovernedProductNotFound) {
			http.Error(w, fmt.Sprintf(`{"error":"product %s not found"}`, productID), http.StatusInternalServerError)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	if result.Applied {
		resp := map[string]interface{}{
			"success":            true,
			"supplier_base_cost": req.NewSupplierBaseCost,
			"applied":            true,
			"autoApproved":       result.AutoApproved,
		}
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

// priceIncreasePendingMessage maps a governance reason to the supplier-
// facing Persian explanation. exception_cap deliberately reuses the
// above_cap message: from the supplier's side both mean "above the cap
// that applies to this product, queued for management review".
func priceIncreasePendingMessage(reason string) string {
	switch reason {
	case "locked":
		return "افزایش قیمت در بازهٔ قفل‌شده نیازمند تأیید مدیریت است؛ درخواست شما ثبت شد و پس از بررسی اعمال می‌شود."
	case "agreement":
		return "این کالا تحت توافق ثبات قیمت است؛ درخواست افزایش شما برای بررسی مدیریت ثبت شد."
	default: // above_cap, exception_cap
		return "افزایش بیش از سقف مجاز نیازمند بررسی مدیریت است؛ درخواست شما ثبت شد."
	}
}

// 10. POST /api/v1/supplier/inbounds/create
func (h *SupplierHandlers) HandleCreateInbound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	inboundID, err := h.supplierRepo.CreateInbound(r.Context(), supplierID, payload)
	if err != nil {
		if errors.Is(err, repository.ErrInboundProductForbidden) {
			t01WriteJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, repository.ErrInboundInput) {
			t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeInternalError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"inbound_id": inboundID,
		"id":         inboundID,
	})
}

// 11. GET /api/v1/supplier/inbounds
func (h *SupplierHandlers) HandleGetInbounds(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)

	list, err := h.supplierRepo.GetSupplierInbounds(r.Context(), supplierID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []repository.InboundShipment{}
	}
	json.NewEncoder(w).Encode(list)
}

// 12. GET /api/v1/supplier/inbounds/:id
func (h *SupplierHandlers) HandleGetInboundByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(r.URL.Path, "/")
	inboundID := parts[len(parts)-1]

	inb, err := h.supplierRepo.GetInboundByID(r.Context(), inboundID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(inb)
}

// POST /api/v1/supplier/inbounds/:id/dispatch (FIX 45, live tn
// «ارسال شد» button): scheduled -> in_transit + dispatched_at.
func (h *SupplierHandlers) HandleDispatchInbound(w http.ResponseWriter, r *http.Request, inboundID string) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)
	if err := h.supplierRepo.DispatchInbound(r.Context(), supplierID, inboundID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"inbound not found or not scheduled"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// POST /api/v1/supplier/inbounds/:id/cancel (FIX 45, live tn «لغو»
// button): scheduled -> cancelled.
func (h *SupplierHandlers) HandleCancelInbound(w http.ResponseWriter, r *http.Request, inboundID string) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)
	if err := h.supplierRepo.CancelInbound(r.Context(), supplierID, inboundID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"inbound not found or not scheduled"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// PUT /api/v1/supplier/products/:id/supply-option
func (h *SupplierHandlers) HandleUpdateSupplyOption(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/supply-option"), "/")
	productID := parts[len(parts)-1]
	var req struct {
		SupplyOption string `json:"supply_option"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	if err := h.supplierRepo.UpdateProductSupplyOption(r.Context(), getSupplierIDFromContext(r, h.supplierRepo), productID, req.SupplyOption); err != nil {
		status := http.StatusInternalServerError
		if strings.HasPrefix(err.Error(), "invalid supply_option") {
			status = http.StatusBadRequest
		}
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), status)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "supply_option": req.SupplyOption})
}

// 14. POST /api/v1/supplier/campaigns/create
func (h *SupplierHandlers) HandleCreateCampaign(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	campaignID, err := h.supplierRepo.CreateCampaign(r.Context(), supplierID, payload)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"campaign_id": campaignID,
		"id":          campaignID,
	})
}

// 15. GET /api/v1/supplier/campaigns/:id/stats
func (h *SupplierHandlers) HandleGetCampaignStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/stats"), "/")
	campaignID := parts[len(parts)-1]

	stats, err := h.supplierRepo.GetCampaignStats(r.Context(), getSupplierIDFromContext(r, h.supplierRepo), campaignID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if stats == nil {
		http.NotFound(w, r)
		return
	}
	json.NewEncoder(w).Encode(stats)
}

// 16. GET /api/v1/supplier/sales-report
func (h *SupplierHandlers) HandleGetSalesReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	for _, value := range []string{startDate, endDate} {
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				http.Error(w, `{"error":"Invalid date"}`, http.StatusBadRequest)
				return
			}
		}
	}
	if startDate != "" && endDate != "" && startDate > endDate {
		http.Error(w, `{"error":"Invalid date range"}`, http.StatusBadRequest)
		return
	}

	report, err := h.supplierRepo.GetSalesReport(r.Context(), supplierID, startDate, endDate)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(report)
}

// 17. GET /api/v1/supplier/discrepancies
func (h *SupplierHandlers) HandleGetDiscrepancies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)

	list, err := h.supplierRepo.GetDiscrepancies(r.Context(), supplierID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// 18. POST /api/v1/supplier/discrepancies/:id/respond
func (h *SupplierHandlers) HandleRespondDiscrepancy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/respond"), "/")
	discrepancyID := parts[len(parts)-1]

	var req struct {
		Response        string `json:"response"`
		ResolutionOffer string `json:"resolution_offer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Response) == "" {
		http.Error(w, `{"error":"Response required"}`, http.StatusBadRequest)
		return
	}
	err := h.supplierRepo.RespondDiscrepancy(r.Context(), getSupplierIDFromContext(r, h.supplierRepo), discrepancyID, req.Response, req.ResolutionOffer)
	if err != nil {
		if errors.Is(err, repository.ErrQCResponseExpired) || errors.Is(err, repository.ErrStoreInput) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"status":  "resolved",
	})
}

// 19. POST /api/v1/supplier/settlement/request
func (h *SupplierHandlers) HandleRequestSettlement(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)

	var req struct {
		Amount        int64  `json:"amount"`
		Shaba         string `json:"shaba"`
		BankName      string `json:"bank_name"`
		AccountHolder string `json:"account_holder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	verification := bank.VerifySheba(req.Shaba)
	if req.Amount <= 0 || !verification.IsValid || strings.TrimSpace(req.AccountHolder) == "" {
		http.Error(w, `{"error":"Positive amount, valid IBAN and account holder required"}`, http.StatusBadRequest)
		return
	}
	req.Shaba = strings.ToUpper(strings.TrimSpace(req.Shaba))
	if !strings.HasPrefix(req.Shaba, "IR") {
		req.Shaba = "IR" + req.Shaba
	}
	reqID, err := h.supplierRepo.RequestSettlement(r.Context(), supplierID, req.Amount, req.Shaba, verification.BankName, req.AccountHolder)
	if err != nil {
		// Gap 12: new settlement requests are frozen while the
		// supplier is suspended (409, like the balance conflict).
		if errors.Is(err, repository.ErrSupplierSuspended) {
			http.Error(w, `{"error":"Supplier is suspended"}`, http.StatusConflict)
			return
		}
		if strings.Contains(err.Error(), "insufficient settlement balance") {
			http.Error(w, `{"error":"Insufficient settlement balance"}`, http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":           true,
		"payout_request_id": reqID,
		"id":                reqID,
	})
}

// 20. GET /api/v1/supplier/settlement/history
func (h *SupplierHandlers) HandleGetSettlementHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	supplierID := getSupplierIDFromContext(r, h.supplierRepo)

	list, err := h.supplierRepo.GetSettlementHistory(r.Context(), supplierID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}
