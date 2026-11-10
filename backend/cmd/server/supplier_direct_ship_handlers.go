package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

// handleGetPendingShipments handles GET /api/v1/supplier/shipments/pending
func handleGetPendingShipments(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	shipmentRepo *repository.SupplierShipmentRepository,
	supplierRepo *repository.SupplierRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if !claims.HasDirectCapability("supplier") && !claims.HasDirectCapability("admin") {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	supplierID := ""
	if claims.HasCapability("supplier") {
		supplierID = getSupplierIDFromContext(r, supplierRepo)
	} else if claims.HasCapability("admin") {
		supplierID = r.URL.Query().Get("supplierId")
	}

	shipments, err := shipmentRepo.GetPendingShipments(ctx, supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	for _, s := range shipments {
		_ = shipmentRepo.LogShipmentAudit(ctx, s.ID, claims.UserID, "viewed_pending_shipments")
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"shipments": shipments,
	})
}

// handleGetSupplierShipments handles GET /api/v1/supplier/shipments
func handleGetSupplierShipments(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	shipmentRepo *repository.SupplierShipmentRepository,
	supplierRepo *repository.SupplierRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if !claims.HasCapability("supplier") && !claims.HasCapability("admin") {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	supplierID := ""
	if claims.HasCapability("supplier") {
		supplierID = getSupplierIDFromContext(r, supplierRepo)
	} else if claims.HasCapability("admin") {
		supplierID = r.URL.Query().Get("supplierId")
	}

	status := r.URL.Query().Get("status")
	shipments, err := shipmentRepo.GetShipments(ctx, supplierID, status)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"shipments": shipments,
	})
}

// handleMarkShipmentShipped handles POST /api/v1/supplier/shipments/:id/ship
func handleMarkShipmentShipped(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	shipmentID string,
	shipmentRepo *repository.SupplierShipmentRepository,
	orderRepo *repository.OrderRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	_ = orderRepo // aggregate order transitions are handled transactionally by SubmitShipping.
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if !claims.HasCapability("supplier") && !claims.HasCapability("admin") {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	var req struct {
		Carrier        string  `json:"carrier"`
		TrackingNumber string  `json:"trackingNumber"`
		Notes          *string `json:"notes,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Carrier) == "" || strings.TrimSpace(req.TrackingNumber) == "" {
		http.Error(w, `{"error":"Carrier and trackingNumber are required"}`, http.StatusBadRequest)
		return
	}

	supplierID := getSupplierIDFromContext(r, nil)
	if claims.HasDirectCapability("admin") {
		supplierID = r.URL.Query().Get("supplierId")
	}
	if supplierID == "" {
		http.Error(w, `{"error":"Supplier scope required"}`, http.StatusForbidden)
		return
	}

	shipment, err := shipmentRepo.GetShipmentByIDSupplier(ctx, shipmentID, supplierID)
	if err != nil || shipment == nil {
		http.Error(w, `{"error":"Shipment not found"}`, http.StatusNotFound)
		return
	}

	err = shipmentRepo.SubmitShipping(ctx, supplierID, shipmentID, req.Carrier, req.TrackingNumber, claims.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrTrackingNumberInUse) {
			http.Error(w, `{"error":"کد رهگیری قبلاً برای مرسولهٔ دیگری ثبت شده است"}`, http.StatusConflict)
			return
		}
		if errors.Is(err, repository.ErrShipmentState) {
			http.Error(w, `{"error":"Shipment is not ready for shipping"}`, http.StatusConflict)
			return
		}
		if repository.IsMissingShipment(err) {
			http.Error(w, `{"error":"Shipment not found"}`, http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Shipment marked as shipped successfully",
		"shipment": map[string]interface{}{
			"id":             shipmentID,
			"orderId":        shipment.OrderID,
			"carrier":        req.Carrier,
			"trackingNumber": req.TrackingNumber,
			"status":         "shipped",
		},
	})
}

// handleGetOrderTracking handles GET /api/v1/orders/:orderId/tracking
func handleGetOrderTracking(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	orderID string,
	orderRepo *repository.OrderRepository,
	shipmentRepo *repository.SupplierShipmentRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	order, err := orderRepo.GetOrderByID(ctx, orderID)
	if err != nil || order == nil {
		order, err = orderRepo.GetOrderByNumber(ctx, orderID)
	}
	if err != nil || order == nil {
		order, err = orderRepo.GetOrderByTrackingCode(ctx, orderID)
	}
	if err != nil || order == nil {
		http.Error(w, `{"error":"Order not found"}`, http.StatusNotFound)
		return
	}

	claims, authenticated := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !authenticated || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	supplierScoped := false
	if claims.HasCapability("admin") {
		// Admin access remains governed by the route capability middleware.
	} else {
		operationalAccess := false
		for _, capability := range []string{"seller", "supplier"} {
			if !claims.HasCapability(capability) {
				continue
			}
			allowed, err := orderRepo.CanReadOperationalTracking(ctx, order.ID, claims.UserID, capability)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			if allowed {
				operationalAccess = true
				if capability == "supplier" {
					supplierScoped = true
				}
				break
			}
		}
		customerAccess := claims.HasCapability("customer") && order.CustomerUserID != nil && *order.CustomerUserID == claims.UserID
		if !operationalAccess && !customerAccess {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
	}
	response := map[string]interface{}{
		"success":           true,
		"orderId":           order.ID,
		"orderNumber":       order.OrderNumber,
		"fulfillmentMethod": order.FulfillmentMethod,
		"status":            order.Status,
	}

	if supplierScoped && shipmentRepo != nil {
		// A supplier party sees ONLY its own leg. The previous code used the
		// unscoped latest-shipment lookup here, so on a multi-supplier order
		// supplier A was shown supplier B's carrier/tracking once B shipped
		// later (or instead). For mixed orders this also replaces the
		// warehouse leg's code with the supplier's own leg.
		shipment, err := shipmentRepo.GetShipmentByOrderIDForUser(ctx, order.ID, claims.UserID)
		if err == nil && shipment != nil {
			response["tracking"] = map[string]interface{}{
				"carrier":        shipment.Carrier,
				"trackingNumber": shipment.TrackingNumber,
				"shippedAt":      shipment.ShippedAt,
				"deliveredAt":    shipment.DeliveredAt,
				"status":         shipment.Status,
			}
		} else {
			// No own shipment (unreachable after CanReadOperationalTracking
			// passed, but fail closed): never fall back to another leg.
			response["tracking"] = map[string]interface{}{
				"carrier":        "در انتظار ارسال تأمین‌کننده",
				"trackingNumber": "",
				"status":         "pending",
			}
		}
	} else if order.FulfillmentMethod == "supplier_direct" && shipmentRepo != nil {
		shipment, err := shipmentRepo.GetShipmentByOrderID(ctx, order.ID)
		if err == nil && shipment != nil {
			response["tracking"] = map[string]interface{}{
				"carrier":        shipment.Carrier,
				"trackingNumber": shipment.TrackingNumber,
				"shippedAt":      shipment.ShippedAt,
				"deliveredAt":    shipment.DeliveredAt,
				"status":         shipment.Status,
			}
		} else {
			response["tracking"] = map[string]interface{}{
				"carrier":        "در انتظار ارسال تأمین‌کننده",
				"trackingNumber": order.TrackingCode,
				"status":         "pending",
			}
		}
	} else {
		response["message"] = "Warehouse fulfillment - tracking from WMS"
		response["tracking"] = map[string]interface{}{
			"carrier":        "پست پیشتاز (انبار مرکزی پلتفرم)",
			"trackingNumber": order.TrackingCode,
			"shippedAt":      order.ShippedAt,
			"status":         order.Status,
		}
	}

	json.NewEncoder(w).Encode(response)
}

// handleWebhookTrackingUpdate handles POST /api/v1/webhooks/tracking/update
// It authenticates via the shared secret header X-Webhook-Secret (fail-closed).
// Carrier updates only update shipment & order transit status, NEVER releasing seller profit or referral commission.
func handleWebhookTrackingUpdate(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	shipmentRepo *repository.SupplierShipmentRepository,
	orderRepo *repository.OrderRepository,
	expectedSecret string,
) {
	w.Header().Set("Content-Type", "application/json")

	// 1. Authenticate webhook request via X-Webhook-Secret header (Strict Fail-Closed)
	providedSecret := r.Header.Get("X-Webhook-Secret")
	if expectedSecret == "" || subtle.ConstantTimeCompare([]byte(providedSecret), []byte(expectedSecret)) != 1 {
		http.Error(w, `{"error":"Unauthorized: invalid or missing X-Webhook-Secret"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		TrackingNumber string `json:"trackingNumber"`
		Status         string `json:"status"`
		Carrier        string `json:"carrier,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if req.TrackingNumber == "" || req.Status == "" {
		http.Error(w, `{"error":"TrackingNumber and Status are required"}`, http.StatusBadRequest)
		return
	}

	// 2. Validate allowed carrier webhook transitions
	validStatuses := map[string]bool{
		"shipped":    true,
		"in_transit": true,
		"delivered":  true,
		"failed":     true,
	}
	if !validStatuses[req.Status] {
		http.Error(w, `{"error":"Invalid carrier status. Allowed values: shipped, in_transit, delivered, failed"}`, http.StatusBadRequest)
		return
	}

	shipment, err := shipmentRepo.UpdateShipmentStatus(ctx, req.TrackingNumber, req.Status)
	if err != nil || shipment == nil {
		// Warehouse shipments are stored on orders rather than supplier_shipments.
		// Preserve the supplier path above and only fall back for warehouse orders.
		if orderRepo == nil {
			http.Error(w, `{"error":"Shipment not found for tracking number"}`, http.StatusNotFound)
			return
		}
		order, orderErr := orderRepo.UpdateWarehouseOrderStatusFromCarrier(ctx, req.TrackingNumber, req.Status)
		if orderErr != nil || order == nil {
			http.Error(w, `{"error":"Shipment not found for tracking number"}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":        true,
			"message":        fmt.Sprintf("Delivery status updated to %s", req.Status),
			"trackingNumber": req.TrackingNumber,
			"status":         req.Status,
		})
		return
	}

	// 3. Carrier delivery updates order status WITHOUT releasing profit.
	// Profit release is strictly gated to customer confirmation or the 20-day system dispute lock.
	if req.Status == "delivered" && orderRepo != nil && shipment.OrderID != "" {
		_ = orderRepo.UpdateOrderStatusFromCarrier(ctx, shipment.OrderID, "delivered")
	}

	_ = shipmentRepo.LogShipmentAudit(ctx, shipment.ID, "carrier_webhook", "status_"+req.Status)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"message":        fmt.Sprintf("Delivery status updated to %s", req.Status),
		"trackingNumber": req.TrackingNumber,
		"status":         req.Status,
	})
}

// handleGetSupplierOrders handles GET /api/v1/supplier/orders
func handleGetSupplierOrders(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	shipmentRepo *repository.SupplierShipmentRepository,
	supplierRepo *repository.SupplierRepository,
	orderRepo *repository.OrderRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	supplierID := ""
	if claims.Role == "supplier" {
		supplierID = getSupplierIDFromContext(r, supplierRepo)
	} else if claims.Role == "admin" {
		supplierID = r.URL.Query().Get("supplierId")
	} else {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	shipments, err := shipmentRepo.GetShipments(ctx, supplierID, "")
	if err != nil {
		writeInternalError(w, err)
		return
	}

	type OrderItemView struct {
		ID             string `json:"id"`
		OrderID        string `json:"orderId"`
		OrderNumber    string `json:"orderNumber"`
		CustomerName   string `json:"customerName"`
		CustomerPhone  string `json:"customerPhone"`
		Status         string `json:"status"`
		StatusText     string `json:"statusText"`
		Carrier        string `json:"carrier,omitempty"`
		TrackingNumber string `json:"trackingNumber,omitempty"`
		ShippedAt      string `json:"shippedAt,omitempty"`
		CreatedAt      string `json:"createdAt"`
		ItemCount      int    `json:"itemCount"`
	}

	ordersView := make([]OrderItemView, 0)
	for _, s := range shipments {
		statusText := "آماده‌سازی"
		if s.Status == "shipped" || s.Status == "in_transit" {
			statusText = "ارسال شده"
		} else if s.Status == "delivered" {
			statusText = "تحویل داده شده"
		}
		shippedAtStr := ""
		if s.ShippedAt != nil {
			shippedAtStr = s.ShippedAt.Format("2006/01/02 15:04")
		}
		carrierStr := ""
		if s.Carrier != nil {
			carrierStr = *s.Carrier
		}
		trackingStr := ""
		if s.TrackingNumber != nil {
			trackingStr = *s.TrackingNumber
		}
		itemCount, err := shipmentRepo.SupplierItemCount(ctx, s.OrderID, s.SupplierID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		ordersView = append(ordersView, OrderItemView{
			ID:             s.ID,
			OrderID:        s.OrderID,
			OrderNumber:    s.OrderNumber,
			CustomerName:   s.CustomerName,
			CustomerPhone:  s.CustomerPhone,
			Status:         s.Status,
			StatusText:     statusText,
			Carrier:        carrierStr,
			TrackingNumber: trackingStr,
			ShippedAt:      shippedAtStr,
			CreatedAt:      s.CreatedAt.Format("2006/01/02 15:04"),
			ItemCount:      itemCount,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"orders":  ordersView,
	})
}

// handleSupplierSubmitShipping handles POST /api/v1/supplier/orders/:orderId/shipping
func handleSupplierSubmitShipping(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	orderID string,
	shipmentRepo *repository.SupplierShipmentRepository,
	orderRepo *repository.OrderRepository,
	supplierRepo *repository.SupplierRepository,
	tapinRepo *repository.TapinRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if claims.Role != "supplier" && claims.Role != "admin" {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	var req struct {
		Carrier        string `json:"carrier"`
		TrackingNumber string `json:"trackingNumber"`
		ShippingDate   string `json:"shippingDate,omitempty"`
		// Ship-with-Tapin (gaps 1-3): when ViaTapin is true the
		// carrier/tracking pair is produced by Tapin registration
		// (submitShippingViaTapin) and the manual fields are ignored.
		ViaTapin     bool   `json:"viaTapin,omitempty"`
		TapinService string `json:"tapinService,omitempty"` // "post" (default) | "tipax"
		TapinBoxID   string `json:"tapinBoxId,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	supplierID, _ := ctx.Value(supplierIDKey).(string)
	if claims.Role == "admin" {
		supplierID = r.URL.Query().Get("supplierId")
	}
	if supplierID == "" {
		http.Error(w, `{"error":"Supplier scope required"}`, http.StatusForbidden)
		return
	}

	if req.ViaTapin {
		service := strings.TrimSpace(req.TapinService)
		if service == "" {
			service = "post"
		}
		outcome := submitShippingViaTapin(ctx, supplierID, orderID, service, req.TapinBoxID, claims.UserID, shipmentRepo, orderRepo, tapinRepo)
		w.WriteHeader(outcome.Status)
		json.NewEncoder(w).Encode(outcome.Body)
		return
	}

	if strings.TrimSpace(req.Carrier) == "" || strings.TrimSpace(req.TrackingNumber) == "" {
		http.Error(w, `{"error":"Carrier and trackingNumber are required"}`, http.StatusBadRequest)
		return
	}
	if err := shipmentRepo.SubmitShipping(ctx, supplierID, orderID, strings.TrimSpace(req.Carrier), strings.TrimSpace(req.TrackingNumber), claims.UserID); err != nil {
		if errors.Is(err, repository.ErrTrackingNumberInUse) {
			http.Error(w, `{"error":"کد رهگیری قبلاً برای مرسولهٔ دیگری ثبت شده است"}`, http.StatusConflict)
			return
		}
		if repository.IsMissingShipment(err) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, repository.ErrShipmentState) {
			http.Error(w, `{"error":"Shipment is not ready for shipping"}`, http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"message":        "اطلاعات ارسال با موفقیت ثبت شد",
		"carrier":        req.Carrier,
		"trackingNumber": req.TrackingNumber,
		"status":         "shipped",
	})
}

// handleGetSupplierWarehouseOrders handles GET /api/v1/supplier/warehouse-orders
// Read-only visibility: paid orders fulfilled from the central warehouse
// that contain the caller's products. The supplier takes no action on
// these; the response carries only the caller's own items at wholesale
// cost plus the destination province/city.
func handleGetSupplierWarehouseOrders(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	shipmentRepo *repository.SupplierShipmentRepository,
	supplierRepo *repository.SupplierRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	supplierID := ""
	if claims.Role == "supplier" {
		supplierID = getSupplierIDFromContext(r, supplierRepo)
	} else if claims.Role == "admin" {
		supplierID = r.URL.Query().Get("supplierId")
	} else {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	if supplierID == "" {
		http.Error(w, `{"error":"Supplier scope required"}`, http.StatusForbidden)
		return
	}

	page := 1
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	pageSize := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("pageSize")); err == nil && v > 0 {
		pageSize = v
	}
	if pageSize > 100 {
		pageSize = 100
	}

	orders, total, err := shipmentRepo.GetSupplierWarehouseOrders(ctx, supplierID, pageSize, (page-1)*pageSize)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	type WarehouseOrderItemView struct {
		ProductID     string `json:"productId"`
		ProductTitle  string `json:"productTitle"`
		Quantity      int    `json:"quantity"`
		WholesaleCost int64  `json:"wholesaleCost"`
		LineTotal     int64  `json:"lineTotal"`
	}
	type WarehouseOrderView struct {
		OrderID     string                   `json:"orderId"`
		OrderNumber string                   `json:"orderNumber"`
		Status      string                   `json:"status"`
		Province    string                   `json:"province"`
		City        string                   `json:"city"`
		CreatedAt   string                   `json:"createdAt"`
		Items       []WarehouseOrderItemView `json:"items"`
		ItemsTotal  int64                    `json:"itemsTotal"`
	}

	views := make([]WarehouseOrderView, 0, len(orders))
	for _, o := range orders {
		view := WarehouseOrderView{
			OrderID:     o.OrderID,
			OrderNumber: o.OrderNumber,
			Status:      o.Status,
			Province:    o.Province,
			City:        o.City,
			CreatedAt:   o.CreatedAt.Format("2006/01/02 15:04"),
			Items:       make([]WarehouseOrderItemView, 0, len(o.Items)),
			ItemsTotal:  o.ItemsTotal,
		}
		for _, item := range o.Items {
			view.Items = append(view.Items, WarehouseOrderItemView{
				ProductID:     item.ProductID,
				ProductTitle:  item.ProductTitle,
				Quantity:      item.Quantity,
				WholesaleCost: item.WholesaleCost,
				LineTotal:     item.LineTotal,
			})
		}
		views = append(views, view)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"orders":   views,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// handleSupplierAcceptOrder handles POST /api/v1/supplier/orders/:orderId/accept
// Moves the caller's direct shipment from 'pending' to 'processing'.
// Accepting is an explicit acknowledgement step only: SubmitShipping keeps
// accepting 'pending' shipments exactly as before, so skipping the accept
// step never blocks shipping.
func handleSupplierAcceptOrder(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	orderID string,
	shipmentRepo *repository.SupplierShipmentRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if claims.Role != "supplier" && claims.Role != "admin" {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	supplierID, _ := ctx.Value(supplierIDKey).(string)
	if claims.Role == "admin" {
		supplierID = r.URL.Query().Get("supplierId")
	}
	if supplierID == "" {
		http.Error(w, `{"error":"Supplier scope required"}`, http.StatusForbidden)
		return
	}

	if err := shipmentRepo.AcceptShipment(ctx, supplierID, orderID, claims.UserID); err != nil {
		if repository.IsMissingShipment(err) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, repository.ErrShipmentNotPending) {
			http.Error(w, `{"error":"این سفارش دیگر در وضعیت انتظار نیست؛ قبلاً قبول شده یا ارسال شده است."}`, http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "سفارش قبول شد و در حال آماده‌سازی است.",
		"status":  "processing",
	})
}
