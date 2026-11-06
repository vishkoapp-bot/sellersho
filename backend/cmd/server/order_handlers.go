package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/privacy"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/routing"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type CreateOrderItemReq struct {
	ProductID string `json:"productId"`
	VariantID string `json:"variantId,omitempty"`
	Quantity  int    `json:"quantity"`
	// FIX 14: live order payload carries addonIds per item (live cart
	// key has the ::addonIds suffix); fees resolve from product_addons.
	AddonIDs []string `json:"addonIds,omitempty"`
}

type CreateOrderReq struct {
	StoreID         string               `json:"storeId"`
	CustomerInfo    privacy.CustomerInfo `json:"customerInfo"`
	PaymentMethod   string               `json:"paymentMethod"`
	Items           []CreateOrderItemReq `json:"items"`
	DiscountCode    string               `json:"discountCode,omitempty"`
	ShippingQuoteID string               `json:"shippingQuoteId,omitempty"`
	// FIX 14: live order payload fields (live binary, sweep H4).
	// IdempotencyKey dedupes replays for the same customer; UseWallet
	// is accepted but rejected while wallet checkout is disabled
	// (live UI: "wallet payment is not active"); AddressID loads a
	// saved user_addresses row; SaveAddress stores the order address.
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	UseWallet      bool   `json:"useWallet,omitempty"`
	WalletAmount   int64  `json:"walletAmount,omitempty"`
	AddressID      string `json:"addressId,omitempty"`
	SaveAddress    bool   `json:"saveAddress,omitempty"`
}

const maxOrderItemQuantity = 1000

// firstSuspendedSupplierProduct (gap 12) returns the first cart
// product — in REQUEST order — whose supplier is currently suspended,
// or "" when the cart is clean. One batch query over the cart's
// product ids; products with no suppliers row (legacy) and unknown
// product ids are not flagged here (the item loop reports unknown
// products itself).
func firstSuspendedSupplierProduct(ctx context.Context, db *sql.DB, items []CreateOrderItemReq) (string, error) {
	if db == nil || len(items) == 0 {
		return "", nil
	}
	ids := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		if it.ProductID != "" && !seen[it.ProductID] {
			seen[it.ProductID] = true
			ids = append(ids, it.ProductID)
		}
	}
	if len(ids) == 0 {
		return "", nil
	}
	var hasSuppliers bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.suppliers') IS NOT NULL`).Scan(&hasSuppliers); err != nil || !hasSuppliers {
		return "", err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT p.id FROM products p
		JOIN suppliers s ON s.id = p.supplier_id AND s.approval_status = 'suspended'
		WHERE p.id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	suspended := make(map[string]bool)
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
			return "", err
		}
		suspended[pid] = true
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, it := range items {
		if suspended[it.ProductID] {
			return it.ProductID, nil
		}
	}
	return "", nil
}

func validateOrderItemQuantity(quantity int) error {
	if quantity <= 0 {
		return fmt.Errorf("quantity must be greater than zero")
	}
	if quantity > maxOrderItemQuantity {
		return fmt.Errorf("quantity exceeds the maximum allowed")
	}
	return nil
}

// handleCreateOrder prices every line with the live percent engine
// (pricing.ResolveReferencePricing; a line with a chosen variant is
// priced with the variant resolver instead, Fix69 M1). Snapshots
// are written from that resolution and are never recomputed later:
// history keeps
// its own snapshot even though today's formula differs from past
// regimes. The live order-path profit split when platform_pct = 0 is
// unverified (TODO U14); the split used here is the arithmetic
// decomposition of the storefront price (ReferenceProfitSplit), not a
// proven live order-accounting rule.
func handleCreateOrder(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	orderRepo *repository.OrderRepository,
	catalogRepo *repository.CatalogRepository,
	storeRepo *repository.StoreRepository,
	shipmentRepo *repository.SupplierShipmentRepository,
	pricingRepo *repository.PricingRepository,
	supplierRepo *repository.SupplierRepository,
	db *sql.DB,
) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil || catalogRepo == nil || storeRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	var req CreateOrderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	// FIX 14: shared order-response writer (create = 201, idempotency
	// replay = 200 with the same shape, role-masked like live).
	respondOrder := func(o domain.Order, status int) {
		callerRole := "customer"
		if claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil {
			callerRole = claims.Role
			if claims.HasDirectCapability("admin") {
				callerRole = "admin"
			}
		}
		maskedCustomer := privacy.MaskCustomerForRole(privacy.CustomerInfo{
			FullName: o.CustomerFullName, Phone: o.CustomerPhone, Province: o.CustomerProvince,
			City: o.CustomerCity, Address: o.CustomerAddress, PostalCode: o.CustomerPostalCode,
		}, callerRole)
		o.CustomerFullName = maskedCustomer.FullName
		o.CustomerPhone = maskedCustomer.Phone
		o.CustomerAddress = maskedCustomer.Address
		o.CustomerPostalCode = maskedCustomer.PostalCode
		resp := map[string]interface{}{
			"id":                         o.ID,
			"orderId":                    o.ID,
			"order_id":                   o.ID,
			"orderNumber":                o.OrderNumber,
			"order_number":               o.OrderNumber,
			"storeId":                    o.StoreID,
			"storeName":                  o.StoreName,
			"totalAmount":                o.TotalAmount,
			"payableAmount":              o.PayableAmount,
			"payable_amount":             o.PayableAmount,
			"paymentMethod":              o.PaymentMethod,
			"paymentStatus":              o.PaymentStatus,
			"customerFullName":           o.CustomerFullName,
			"customerPhone":              o.CustomerPhone,
			"customerProvince":           o.CustomerProvince,
			"customerCity":               o.CustomerCity,
			"customerAddress":            o.CustomerAddress,
			"customerPostalCode":         o.CustomerPostalCode,
			"status":                     o.Status,
			"trackingCode":               o.TrackingCode,
			"shippingCost":               o.ShippingCost,
			"shippingQuoteId":            o.ShippingQuoteID,
			"tracking_code":              o.TrackingCode,
			"dropshipperProfit":          o.DropshipperProfit,
			"platformProfit":             o.PlatformProfit,
			"referralCommission":         o.ReferralCommission,
			"referralCommissionReleased": o.ReferralCommissionReleased,
			"profitReleased":             o.ProfitReleased,
			"fulfillmentMethod":          o.FulfillmentMethod,
			"fulfillment_method":         o.FulfillmentMethod,
			"items":                      o.Items,
			"createdAt":                  o.CreatedAt,
			"updatedAt":                  o.UpdatedAt,
			"order":                      o,
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(resp)
	}
	// Authenticated identity is authoritative; delivery recipient details remain request data.
	req.CustomerInfo.Phone = claims.Mobile
	// FIX 14: addressId loads a saved address (user_addresses) into the
	// order's recipient fields; it runs before the required-info check
	// because a live client may send addressId instead of full info.
	if strings.TrimSpace(req.AddressID) != "" {
		var a struct {
			ReceiverName, ReceiverPhone, Province, City, PostalCode, Detail string
		}
		var phone sql.NullString
		var postal sql.NullString
		err := db.QueryRowContext(ctx,
			`SELECT receiver_full_name, receiver_phone, province_name, city_name, postal_code, address_detail
			FROM user_addresses WHERE id = $1 AND user_id = $2::uuid`,
			req.AddressID, claims.UserID).Scan(&a.ReceiverName, &phone, &a.Province, &a.City, &postal, &a.Detail)
		if err != nil {
			http.Error(w, `{"error":"Address not found"}`, http.StatusBadRequest)
			return
		}
		req.CustomerInfo.FullName = a.ReceiverName
		if phone.Valid && phone.String != "" {
			req.CustomerInfo.Phone = phone.String
		}
		req.CustomerInfo.Province = a.Province
		req.CustomerInfo.City = a.City
		if postal.Valid {
			req.CustomerInfo.PostalCode = postal.String
		}
		req.CustomerInfo.Address = a.Detail
	}
	// FIX 14: wallet checkout is DISABLED on live (UI badge + text);
	// a wallet-payment request is rejected, never silently converted.
	if req.UseWallet {
		http.Error(w, `{"error":"Wallet payment is not available"}`, http.StatusBadRequest)
		return
	}
	// FIX 14: idempotency replay -- the same customer + key returns the
	// original order instead of creating a duplicate. (No unique DB
	// constraint is added here; concurrent same-key races are flagged
	// in the Fix 14 report, same class as the settlement race.)
	if strings.TrimSpace(req.IdempotencyKey) != "" {
		var existingID string
		if err := db.QueryRowContext(ctx,
			`SELECT id FROM orders WHERE customer_user_id = $1 AND idempotency_key = $2`,
			claims.UserID, req.IdempotencyKey).Scan(&existingID); err == nil && existingID != "" {
			if existing, err := orderRepo.GetOrderByID(ctx, existingID); err == nil && existing != nil {
				respondOrder(*existing, http.StatusOK)
				return
			}
		}
	}

	if len(req.Items) == 0 || req.CustomerInfo.FullName == "" || req.CustomerInfo.Phone == "" {
		http.Error(w, `{"error":"Missing required order information"}`, http.StatusBadRequest)
		return
	}

	// ISO-PURCHASE: store attribution is fail-closed. The requested
	// slug IS the order's store: an unknown, stale, or empty slug must
	// never silently fall back to central. That fallback misattributed
	// a dropshipper checkout to the platform store and, because the
	// per-line publication gate below is skipped for central, let
	// unpublished lines through. A client shopping central sends slug
	// "central" explicitly; anything else that does not resolve is a
	// 404, never a different store's order.
	store, err := storeRepo.GetStoreBySlug(ctx, req.StoreID)
	if err != nil {
		http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
		return
	}
	if store == nil {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return
	}

	// Discount code validation happens BEFORE pricing and BEFORE any
	// persistence: an invalid/expired code fails the whole order with
	// 400 here, never silently charging the full price. The use is
	// consumed only after the order persists (below).
	var sellerDiscountTotal int64 = 0
	// Fix69 follow-up (W-MONEY M2 residual): true once the validated
	// code actually survives the admin margin lock on at least one
	// line (see the per-line check below). A code dropped by the
	// lock on every line must neither be recorded on the order nor
	// consume a use: the customer paid full price and did not use
	// the code.
	var discountCodeApplied bool
	var discountCodeObj *repository.DiscountCode
	var discountCodeForOrder string
	if req.DiscountCode != "" {
		c, err := storeRepo.ValidateDiscountCode(ctx, store.ID, req.DiscountCode)
		if err != nil {
			http.Error(w, `{"error":"Discount code is invalid or expired"}`, http.StatusBadRequest)
			return
		}
		discountCodeObj = c
		discountCodeForOrder = c.Code
	}

	settings, _ := loadPricingSettings(ctx, pricingRepo)
	locks := loadStoreMarginLocks(ctx, pricingRepo, store.ID)
	// Fix69 M1: variant overrides once per order (variant pricing below).
	variantOverrides := loadVariantOverridesForStore(ctx, pricingRepo, store.ID)
	// The store's timed-deals widget config, parsed once for every
	// order line (see applyDealsWidgetDiscount).
	widgetCfg := loadDealsWidgetConfig(store)

	// Cryptographically secure non-monotonic IDs using crypto/rand UUIDv4
	orderUUID := uuid.New()
	orderID := fmt.Sprintf("ord-%s", orderUUID.String())
	orderNumber := fmt.Sprintf("ORD-%s", strings.ToUpper(orderUUID.String()[:8]))
	trackingUUID := uuid.New()
	trackingCode := fmt.Sprintf("TRK-%s", strings.ToUpper(trackingUUID.String()[:12]))

	var totalAmount int64 = 0
	var payableAmount int64 = 0
	var dropshipperProfit int64 = 0
	var platformProfit int64 = 0
	// FIX 14: addon money streams (live: buy-now price =
	// (price + sum addon feePerUnit) x qty). platform vs supplier
	// addons land in their respective order columns.
	var platformAddonTotal int64 = 0
	var supplierAddonTotal int64 = 0
	// Gap 12: suspended-supplier gate, BEFORE the item loop and
	// independent of its per-line routing resolution: one item from a
	// suspended supplier rejects the WHOLE order, mirroring the
	// per-line publication gate's whole-order semantics below.
	if suspendedProductID, serr := firstSuspendedSupplierProduct(ctx, db, req.Items); serr != nil {
		http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
		return
	} else if suspendedProductID != "" {
		http.Error(w, fmt.Sprintf(`{"error":"Product %s is unavailable because its supplier is suspended"}`, suspendedProductID), http.StatusBadRequest)
		return
	}

	var orderItems []domain.OrderItem
	var quoteLines []shippingQuoteLine
	fulfillmentMethod := "warehouse"
	// Gap 13: the supplier shipping-method toggles behind the single
	// routing resolver are loaded lazily, once per distinct supplier,
	// through the batched repo method (memoised here so a cart with
	// many lines from one supplier costs one query). A missing
	// supplierRepo or a load error yields an empty map, which the
	// resolver treats as "no methods enabled" — the warehouse
	// fail-safe, never a guessed direct route.
	routingMethodsMemo := map[string]map[string]string{}
	methodsFor := func(supplierID string) map[string]string {
		if supplierID == "" {
			return nil
		}
		if m, ok := routingMethodsMemo[supplierID]; ok {
			return m
		}
		m := map[string]string{}
		if supplierRepo != nil {
			if batch, merr := supplierRepo.ListShippingMethodsBySupplierIDs(ctx, []string{supplierID}); merr == nil {
				if mm, ok := batch[supplierID]; ok {
					m = mm
				}
			}
		}
		routingMethodsMemo[supplierID] = m
		return m
	}
	directSupplierIDs := make(map[string]struct{})
	hasWarehouseItems := false

	for _, itemReq := range req.Items {
		qty := itemReq.Quantity
		if err := validateOrderItemQuantity(qty); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"Quantity for product %s %s"}`, itemReq.ProductID, err.Error()), http.StatusBadRequest)
			return
		}

		p, err := catalogRepo.GetProductByID(ctx, itemReq.ProductID)
		if err != nil || p == nil {
			http.Error(w, fmt.Sprintf(`{"error":"Product %s not found"}`, itemReq.ProductID), http.StatusBadRequest)
			return
		}
		if p.Stock < qty {
			http.Error(w, fmt.Sprintf(`{"error":"Insufficient stock for product %s"}`, itemReq.ProductID), http.StatusBadRequest)
			return
		}
		if store.OwnerType == "dropshipper" {
			active, activeErr := storeRepo.StoreProductActive(ctx, store.ID, p.ID)
			if activeErr != nil {
				http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
				return
			}
			if !active {
				http.Error(w, fmt.Sprintf(`{"error":"Product %s is not published by this store"}`, p.ID), http.StatusBadRequest)
				return
			}
		}

		// Fix69 M1+O4: resolve the chosen variant BEFORE pricing.
		// M1: the order line is priced with the variant resolver
		// (ResolveReferencePricingForStoreProductVariantWithCost),
		// the same resolver the storefront variant-price response
		// uses, so the charged price matches the displayed variant
		// price instead of the product-level price.
		// O4: variant min/max/stock gate the order here, before any
		// persistence. domain.ProductVariant carries no min/max, so
		// they are read from product_variants directly. Variant
		// stock 0 is treated as untracked, NOT as out of stock:
		// supplier variant creation seeds stock 0 (supplier_repo
		// AddVariant/CreateProductVariantsBulk) and no order,
		// warehouse, or proposal path ever raises variant stock,
		// so a hard gate on 0-stock variants would make those
		// products unorderable. Only a positive variant stock is
		// enforced as an allocation cap. Product-level stock
		// (checked above) stays the authoritative gate.
		var chosenVariant *domain.ProductVariant
		if strings.TrimSpace(itemReq.VariantID) != "" {
			variants, verr := catalogRepo.GetProductVariants(ctx, p.ID)
			if verr != nil {
				http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
				return
			}
			for _, v := range variants {
				if v.ID == itemReq.VariantID {
					vv := v
					chosenVariant = &vv
					break
				}
			}
			if chosenVariant == nil {
				http.Error(w, fmt.Sprintf(`{"error":"Variant %s not found for product %s"}`, itemReq.VariantID, itemReq.ProductID), http.StatusBadRequest)
				return
			}
			var vstock, vmin int
			var vmax sql.NullInt32
			if qerr := db.QueryRowContext(ctx, `SELECT stock, min_cart_qty, max_cart_qty FROM product_variants WHERE id = $1`, itemReq.VariantID).Scan(&vstock, &vmin, &vmax); qerr != nil {
				http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
				return
			}
			if qty < vmin {
				http.Error(w, fmt.Sprintf(`{"error":"Quantity below minimum for variant %s"}`, itemReq.VariantID), http.StatusBadRequest)
				return
			}
			if vmax.Valid && qty > int(vmax.Int32) {
				http.Error(w, fmt.Sprintf(`{"error":"Quantity exceeds maximum for variant %s"}`, itemReq.VariantID), http.StatusBadRequest)
				return
			}
			if vstock > 0 && qty > vstock {
				http.Error(w, fmt.Sprintf(`{"error":"Insufficient stock for variant %s"}`, itemReq.VariantID), http.StatusBadRequest)
				return
			}
		}

		// Gap 13: ONE routing truth — the resolver combines the
		// product flag with the supplier's shipping-method toggles
		// (the warehouse queue's documented semantics, adopted as
		// the platform rule). Creation used to read the flag alone,
		// so a direct-only supplier's warehouse-flagged lines got no
		// shipment here yet routed direct in the queue.
		itemRouting := routing.ResolveItemRouting(p.FulfillmentMethod, methodsFor(p.SupplierID))
		if itemRouting == "supplier_direct" {
			// ISO-PURCHASE: a direct-fulfillment product with no supplier
			// must fail, never be attributed to a hardcoded supplier:
			// that substitution created a shipment (and a shipping leg)
			// for a supplier that does not own the product. The check
			// keys on the RESOLVED routing, not the raw flag.
			if strings.TrimSpace(p.SupplierID) == "" {
				http.Error(w, fmt.Sprintf(`{"error":"Product %s has no supplier for direct fulfillment"}`, p.ID), http.StatusBadRequest)
				return
			}
			directSupplierIDs[p.SupplierID] = struct{}{}
		} else {
			hasWarehouseItems = true
		}

		var res pricing.ReferenceResolution
		lineVariantID := ""
		if chosenVariant != nil {
			// Fix69 M1: variant price, not product price. Variant
			// cost wins when set, else the product cost (same rule
			// as priceVariantsForResponse); a store variant override
			// wins over the product resolution inside the resolver.
			cost := p.SupplierBaseCost
			if chosenVariant.SupplierBaseCost != nil {
				cost = *chosenVariant.SupplierBaseCost
			}
			var ov *pricing.StoreVariantOverride
			if o, ok := variantOverrides[p.ID+"/"+chosenVariant.ID]; ok {
				oc := o
				ov = &oc
			}
			res = pricing.ResolveReferencePricingForStoreProductVariantWithCost(*p, cost, chosenVariant.ID, *store, settings, locks, ov)
			lineVariantID = chosenVariant.ID
		} else {
			res = pricing.ResolveReferencePricing(*p, *store, settings, locks)
		}
		// The timed-deals widget discount (margin percentage points
		// from the store's widget config) is the first discount
		// layer: it is the advertised campaign price, so it is what
		// the customer is charged. Its amount folds into the line's
		// DiscountAmount alongside the DB layers below.
		res, widgetDiscountAmount := applyDealsWidgetDiscount(store, widgetCfg, p.ID, res)
		// Dropshipper discounts (campaign, then code) layer on the
		// percent-engine resolution: wholesale cost and platform fee
		// in res are untouched, only FinalPrice/SellerMargin shrink.
		discounted := applyStoreDiscounts(ctx, storeRepo, store, p, lineVariantID, res, discountCodeObj)
		discounted.DiscountAmount += widgetDiscountAmount
		// Fix69 follow-up: DiscountResult.Code is set only by
		// ApplyCodeDiscount, which runs only when the code passed
		// the lock gate in applyStoreDiscounts (dropshipper store,
		// margin not locked, lock readable). A campaign-only result
		// never sets Code, so this is the exact per-line signal
		// that the code applied — no second lock query, no gate
		// logic duplicated here. The lock is scoped per
		// product/niche, so the code counts as applied if it
		// survived on ANY line.
		if discounted.Code != "" {
			discountCodeApplied = true
		}
		sellerDiscountTotal += discounted.DiscountAmount * int64(qty)
		res = discounted.Resolution
		// TODO U14: order-path split unverified in live; see function doc.
		split := pricing.ReferenceProfitSplit(res, store.OwnerType != "dropshipper")

		lineItemSellingPrice := res.FinalPrice * int64(qty)
		lineItemPlatformFee := split.PlatformFeeAmount * int64(qty)
		lineItemSellerMargin := split.DropshipperMargin * int64(qty)

		totalAmount += res.OriginalPrice * int64(qty)
		payableAmount += lineItemSellingPrice

		if store.OwnerType == "dropshipper" {
			dropshipperProfit += lineItemSellerMargin
			platformProfit += lineItemPlatformFee
		} else {
			platformProfit += split.PlatformTotal * int64(qty)
		}

		// Fix69 M1: the variant snapshot travels to the order line
		// (the variant itself was resolved before pricing above, so
		// res already carries the variant price). This replaces the
		// Fix 5 rule that variant prices were a frontend-only
		// display override.
		// Gap 13: the origin snapshot persisted as this line's
		// order_item_origins row. The label mirrors legOrigin
		// (shipping_quote_handlers.go): a supplier leg is labelled
		// with the product's warehouse name when one is recorded,
		// else the direct-shipping label; a warehouse leg carries
		// the warehouse name (central fallback when unnamed).
		// WarehouseID / SupplierWarehouseID / LegIndex are resolved
		// in batch after the loop.
		originLabel := "انبار مرکزی"
		if strings.TrimSpace(p.WarehouseName) != "" {
			originLabel = p.WarehouseName
		} else if itemRouting == "supplier_direct" {
			originLabel = "ارسال مستقیم تأمین‌کننده"
		}
		item := domain.OrderItem{
			ProductID:      p.ID,
			ProductTitle:   p.Title,
			Quantity:       qty,
			WholesaleCost:  res.WholesaleCost,
			PlatformFee:    res.PlatformFee,
			SellerMargin:   res.SellerMargin,
			FinalUnitPrice: res.FinalPrice,
			DiscountAmount: discounted.DiscountAmount,
			Routing:        itemRouting,
			OriginLabel:    originLabel,
			SupplierID:     p.SupplierID,
		}
		// ISO-PURCHASE: resolve addonIds against product_addons
		// (active + approved only) AND scoped to this (store, product),
		// mirroring the PDP resolver (GetPDPAddons): a dropshipper
		// store may only charge addons it explicitly selected for this
		// product (store_product_addons); central may only charge the
		// product supplier's defined attachments, minus central
		// exclusions. The old unscoped query let ANY approved addon
		// from any product/store be charged on any line. Linked-product
		// addons are not fee cards (their charge is the linked
		// product's price, a separate complementary display), so they
		// never resolve here. Unknown/inactive/unselected ids fail the
		// order, like an invalid discount code: never silently drop a
		// paid extra, and never charge an unselected one.
		// price_mode fixed -> price_value per unit; percent -> that
		// percent of the unit price, capped by cap_toman when > 0.
		// (Percent/cap details are inferred from the addon table;
		// live's exact feePerUnit formula for percent addons is unprobed.)
		if len(itemReq.AddonIDs) > 0 {
			seen := map[string]bool{}
			uniq := []string{}
			for _, aid := range itemReq.AddonIDs {
				if aid = strings.TrimSpace(aid); aid != "" && !seen[aid] {
					seen[aid] = true
					uniq = append(uniq, aid)
				}
			}
			if len(uniq) > 0 {
				var rows *sql.Rows
				var qerr error
				if store.OwnerType == "central" {
					rows, qerr = db.QueryContext(ctx,
						`SELECT DISTINCT pa.id, pa.owner_type, pa.price_mode, pa.price_value, pa.cap_toman
						FROM product_addons pa
						JOIN product_addon_attachments att ON att.addon_id = pa.id
						WHERE pa.id = ANY($1) AND pa.is_active AND pa.approval_status = 'approved'
						  AND pa.linked_product_id IS NULL AND pa.owner_supplier_id = $2
						  AND ((att.scope_type = 'product' AND att.product_id = $3)
						    OR (att.scope_type = 'category' AND att.category_slug = $4)
						    OR att.scope_type = 'global')
						  AND NOT EXISTS (
							SELECT 1 FROM store_product_addon_exclusions ex
							WHERE ex.store_id = $5 AND ex.product_id = $3 AND ex.addon_id = pa.id
						  )`,
						uniq, p.SupplierID, p.ID, p.CategorySlug, store.ID)
				} else {
					rows, qerr = db.QueryContext(ctx,
						`SELECT pa.id, pa.owner_type, pa.price_mode, pa.price_value, pa.cap_toman
						FROM product_addons pa
						JOIN store_product_addons spa ON spa.addon_id = pa.id
						WHERE pa.id = ANY($1) AND pa.is_active AND pa.approval_status = 'approved'
						  AND pa.linked_product_id IS NULL
						  AND spa.store_id = $2 AND spa.product_id = $3`,
						uniq, store.ID, p.ID)
				}
				if qerr != nil {
					http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
					return
				}
				found := map[string]bool{}
				var addonLineTotal int64 = 0
				for rows.Next() {
					var aid, owner, mode string
					var priceVal float64
					var cap int64
					if err := rows.Scan(&aid, &owner, &mode, &priceVal, &cap); err != nil {
						rows.Close()
						http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
						return
					}
					found[aid] = true
					var fee int64
					if mode == "percent" {
						fee = int64(float64(res.FinalPrice) * priceVal / 100.0)
					} else {
						fee = int64(priceVal)
					}
					if cap > 0 && fee > cap {
						fee = cap
					}
					if owner == "supplier" {
						supplierAddonTotal += fee * int64(qty)
					} else {
						platformAddonTotal += fee * int64(qty)
					}
					addonLineTotal += fee * int64(qty)
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
					return
				}
				if len(found) != len(uniq) {
					http.Error(w, `{"error":"Addon not available for this store or product"}`, http.StatusBadRequest)
					return
				}
				payableAmount += addonLineTotal
			}
		}
		if chosenVariant != nil {
			vid, vname, vspnc := chosenVariant.ID, chosenVariant.Name, chosenVariant.Spnc
			item.VariantID, item.VariantName, item.VariantSPNC = &vid, &vname, &vspnc
		}
		orderItems = append(orderItems, item)
		quoteLine := shippingQuoteLine{
			ProductID: p.ID, VariantID: itemReq.VariantID, Quantity: qty,
			Fulfillment: itemRouting, SupplierID: p.SupplierID, Warehouse: p.WarehouseName,
			UnitPriceToman: p.OriginalPrice,
		}
		if p.WeightGrams != nil {
			quoteLine.WeightG = *p.WeightGrams
		}
		if p.LengthMM != nil {
			quoteLine.LengthMM = *p.LengthMM
		}
		if p.WidthMM != nil {
			quoteLine.WidthMM = *p.WidthMM
		}
		if p.HeightMM != nil {
			quoteLine.HeightMM = *p.HeightMM
		}
		quoteLines = append(quoteLines, quoteLine)

	}

	// Gap 13: resolve the origin identities the origins rows carry,
	// in batch. A supplier leg's supplier_warehouse_id is the
	// product's recorded origin supplier warehouse (catalogRepo's
	// product read does not select that column, so it is fetched
	// here with the warehouses lookup); a warehouse leg's
	// warehouse_id is the warehouses row whose name matches the
	// product's warehouse_name (NULL when unmatched — the origin
	// label still records the name). Lookup failures degrade to
	// NULL identities rather than failing a priced order.
	if len(orderItems) > 0 {
		if db != nil {
			productIDs := []string{}
			seenProduct := map[string]bool{}
			warehouseNames := []string{}
			seenName := map[string]bool{}
			for i := range orderItems {
				if !seenProduct[orderItems[i].ProductID] {
					seenProduct[orderItems[i].ProductID] = true
					productIDs = append(productIDs, orderItems[i].ProductID)
				}
				if orderItems[i].Routing != "supplier_direct" {
					if name := quoteLines[i].Warehouse; name != "" && !seenName[name] {
						seenName[name] = true
						warehouseNames = append(warehouseNames, name)
					}
				}
			}
			originSwhByProduct := map[string]string{}
			if rows, qerr := db.QueryContext(ctx, `SELECT id, COALESCE(origin_supplier_warehouse_id, '') FROM products WHERE id = ANY($1::text[])`, pq.Array(productIDs)); qerr == nil {
				for rows.Next() {
					var pid, swh string
					if err := rows.Scan(&pid, &swh); err == nil && swh != "" {
						originSwhByProduct[pid] = swh
					}
				}
				rows.Close()
			}
			warehouseIDByName := map[string]string{}
			if len(warehouseNames) > 0 {
				if rows, qerr := db.QueryContext(ctx, `SELECT id, name FROM warehouses WHERE name = ANY($1::text[])`, pq.Array(warehouseNames)); qerr == nil {
					for rows.Next() {
						var wid, wname string
						if err := rows.Scan(&wid, &wname); err == nil {
							warehouseIDByName[wname] = wid
						}
					}
					rows.Close()
				}
			}
			for i := range orderItems {
				if orderItems[i].Routing == "supplier_direct" {
					if swh, ok := originSwhByProduct[orderItems[i].ProductID]; ok {
						v := swh
						orderItems[i].SupplierWarehouseID = &v
					}
				} else if wid, ok := warehouseIDByName[quoteLines[i].Warehouse]; ok {
					v := wid
					orderItems[i].WarehouseID = &v
				}
			}
		}
		// leg_index: first-appearance index of the origin key over
		// the quote-sorted lines — the same grouping and order
		// computeShippingQuote prices, so an origin row's leg_index
		// is the quote leg the line was priced into.
		sortedIdx := make([]int, len(quoteLines))
		for i := range sortedIdx {
			sortedIdx[i] = i
		}
		sort.SliceStable(sortedIdx, func(a, b int) bool {
			ka, _ := legOrigin(quoteLines[sortedIdx[a]])
			kb, _ := legOrigin(quoteLines[sortedIdx[b]])
			return ka < kb
		})
		legOf := map[string]int{}
		for _, idx := range sortedIdx {
			key, _ := legOrigin(quoteLines[idx])
			li, ok := legOf[key]
			if !ok {
				li = len(legOf)
				legOf[key] = li
			}
			orderItems[idx].LegIndex = li
		}
	}

	// Referral commission (D33): respected only when the flag is on.
	// Live has referral_is_enabled = false, so this is normally zero.
	// The central base (fee + margin, via PlatformTotal) is the SOURCE
	// formula and is NOT proven for live re-enablement; if referral is
	// ever re-enabled, the base must be re-proven before relying on it.
	var referralCommission int64 = 0
	if store.ReferrerSellerID != nil && settings != nil && settings.ReferralIsEnabled {
		percent := settings.ReferralCommissionPercent
		if percent <= 0 {
			percent = 2.0
		}
		referralCommission = int64(float64(platformProfit) * (percent / 100.0))
	}

	if len(directSupplierIDs) > 0 {
		fulfillmentMethod = "supplier_direct"
	}
	if hasWarehouseItems && len(directSupplierIDs) > 0 {
		fulfillmentMethod = "mixed"
	}

	// Installments are deleted entirely (user decision): no
	// installment field, price, or logic may remain in this file.
	// Every order is a cash order, whatever the request asked for;
	// the old "installments" value is not honoured or stored.
	paymentMethod := "cash"

	// FIX 5 (Team 03 G1): a quoted order records its quote instead
	// of ShippingCost: 0. The quote is never trusted from the client:
	// it is recomputed from the same cart + destination, and a
	// mismatch is shipping_quote_changed, exactly like live.
	var shippingCost int64 = 0
	var shippingQuoteID string
	if strings.TrimSpace(req.ShippingQuoteID) != "" {
		rates, rerr := loadShippingRates(ctx, db)
		if rerr != nil {
			http.Error(w, `{"error":"shipping_unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		sortQuoteLines(quoteLines)
		quoteDest := shippingQuoteDestination{
			Province: req.CustomerInfo.Province, City: req.CustomerInfo.City,
			Address: req.CustomerInfo.Address, PostalCode: req.CustomerInfo.PostalCode,
		}
		quote := computeShippingQuote(quoteLines, rates, quoteDestinationKey(quoteDest))
		// Tapin repricing (tapin_quote.go): the identical step the
		// quote endpoint ran, over lines built from the same product
		// reads — a Tapin-priced quote re-derives the same quoteId
		// here; a Tapin failure fails the order with the upstream
		// message instead of silently repricing by formula.
		if terr := priceTapinQuoteLegs(ctx, db, &quote, quoteLines, quoteDest); terr != nil {
			http.Error(w, tapinQuoteErrorBody(terr), http.StatusBadRequest)
			return
		}
		if quote.QuoteID != req.ShippingQuoteID {
			http.Error(w, `{"error":"shipping_quote_changed"}`, http.StatusBadRequest)
			return
		}
		shippingCost = quote.TotalShipping
		shippingQuoteID = quote.QuoteID
		payableAmount += shippingCost
	}

	// FIX 14: DiscountAmount is original-minus-final merchandise
	// value ONLY. The old totalAmount-payableAmount subtracted the
	// shipping cost (and now addon totals) too, understating the
	// discount and even going negative (sweep H6).
	merchandisePayable := payableAmount - shippingCost - platformAddonTotal - supplierAddonTotal
	var idempotencyPtr *string
	if strings.TrimSpace(req.IdempotencyKey) != "" {
		k := strings.TrimSpace(req.IdempotencyKey)
		idempotencyPtr = &k
	}
	// Fix69 follow-up: record the code on the order only if it
	// actually applied to at least one line. A lock-dropped code
	// leaves DiscountCode empty, so the order never claims a code
	// the customer did not benefit from.
	if !discountCodeApplied {
		discountCodeForOrder = ""
	}
	order := domain.Order{
		ID:                         orderID,
		CustomerUserID:             &claims.UserID,
		OrderNumber:                orderNumber,
		StoreID:                    store.ID,
		StoreName:                  store.Name,
		TotalAmount:                totalAmount,
		DiscountAmount:             totalAmount - merchandisePayable,
		PayableAmount:              payableAmount,
		PaymentMethod:              paymentMethod,
		PaymentStatus:              "pending",
		CustomerFullName:           req.CustomerInfo.FullName,
		CustomerPhone:              req.CustomerInfo.Phone,
		CustomerProvince:           req.CustomerInfo.Province,
		CustomerCity:               req.CustomerInfo.City,
		CustomerAddress:            req.CustomerInfo.Address,
		CustomerPostalCode:         req.CustomerInfo.PostalCode,
		Status:                     "processing",
		TrackingCode:               trackingCode,
		ShippingCost:               shippingCost,
		ShippingQuoteID:            shippingQuoteID,
		DropshipperProfit:          dropshipperProfit,
		SellerDiscountAmount:       sellerDiscountTotal,
		DiscountCode:               discountCodeForOrder,
		PlatformProfit:             platformProfit,
		ReferralCommission:         referralCommission,
		ReferralCommissionReleased: false,
		ReferrerSellerID:           store.ReferrerSellerID,
		ProfitReleased:             false,
		FulfillmentMethod:          fulfillmentMethod,
		Items:                      orderItems,
		// FIX 14: live payload fields persisted (columns already exist).
		// Wallet stays 0: wallet checkout is disabled, so the gateway
		// covers the full payable amount (CHECK wallet+gateway<=payable).
		WalletAmount:        0,
		GatewayAmount:       payableAmount,
		PlatformAddonAmount: platformAddonTotal,
		SupplierAddonAmount: supplierAddonTotal,
		IdempotencyKey:      idempotencyPtr,
	}

	shipments := make([]domain.SupplierShipment, 0, len(directSupplierIDs))
	if len(directSupplierIDs) > 0 && shipmentRepo != nil {
		for directShipSupplierID := range directSupplierIDs {
			shipments = append(shipments, domain.SupplierShipment{
				ID:         fmt.Sprintf("ship-%s-%s", uuid.NewString(), directShipSupplierID),
				OrderID:    order.ID,
				SupplierID: directShipSupplierID,
				ShippingAddress: domain.ShippingAddress{
					Province:   req.CustomerInfo.Province,
					City:       req.CustomerInfo.City,
					PostalCode: req.CustomerInfo.PostalCode,
					Address:    req.CustomerInfo.Address,
				},
				CustomerPhone: req.CustomerInfo.Phone,
				CustomerName:  req.CustomerInfo.FullName,
				Status:        "pending",
			})
		}
	}

	if err := orderRepo.CreateOrderWithShipments(ctx, order, shipments); err != nil {
		http.Error(w, `{"error":"Order could not be created"}`, http.StatusInternalServerError)
		return
	}
	// FIX 14: saveAddress stores the order address in the address book
	// (best-effort: an address-book failure must not fail the order).
	if req.SaveAddress {
		var existingCount int
		_ = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM user_addresses WHERE user_id = $1::uuid`, claims.UserID).Scan(&existingCount)
		_, _ = db.ExecContext(ctx,
			`INSERT INTO user_addresses (id, user_id, label, receiver_full_name, receiver_phone,
			province_name, city_name, postal_code, address_detail, is_default)
			VALUES ($1, $2::uuid, 'order', $3, $4, $5, $6, $7, $8, $9)`,
			"addr-"+uuid.NewString(), claims.UserID, req.CustomerInfo.FullName, req.CustomerInfo.Phone,
			req.CustomerInfo.Province, req.CustomerInfo.City, req.CustomerInfo.PostalCode,
			req.CustomerInfo.Address, existingCount == 0)
	}
	// Code use is consumed ONLY after order persistence succeeds. A
	// consumption failure must not fail an already-created order
	// (old-branch semantics); the atomic guard in
	// IncrementDiscountCodeUse bounds the next use instead.
	if discountCodeObj != nil && discountCodeApplied {
		_ = storeRepo.IncrementDiscountCodeUse(ctx, discountCodeObj.ID)
	}

	respondOrder(order, http.StatusCreated)
}

func handleTrackOrder(w http.ResponseWriter, r *http.Request, ctx context.Context, code string, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	order, err := orderRepo.GetOrderByTrackingCode(ctx, strings.TrimSpace(code))
	if err != nil || order == nil {
		http.Error(w, `{"error":"Order not found"}`, http.StatusNotFound)
		return
	}

	callerRole := "public"
	if claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil && claims.Role != "" {
		callerRole = claims.Role
	}

	rawCust := privacy.CustomerInfo{
		FullName:   order.CustomerFullName,
		Phone:      order.CustomerPhone,
		Province:   order.CustomerProvince,
		City:       order.CustomerCity,
		Address:    order.CustomerAddress,
		PostalCode: order.CustomerPostalCode,
	}
	maskedCust := privacy.MaskCustomerForRole(rawCust, callerRole)

	order.CustomerFullName = maskedCust.FullName
	order.CustomerPhone = maskedCust.Phone
	order.CustomerAddress = maskedCust.Address
	order.CustomerPostalCode = maskedCust.PostalCode
	if callerRole != "admin" && callerRole != "super_admin" {
		order.DropshipperProfit = 0
		order.PlatformProfit = 0
		order.ReferralCommission = 0
		order.ReferrerSellerID = nil
		order.WalletAmount = 0
		order.GatewayAmount = 0
		order.PlatformAddonAmount = 0
		order.SupplierAddonAmount = 0
		order.DiscountCode = ""
		order.SellerDiscountAmount = 0
	}

	json.NewEncoder(w).Encode(order)
}

func handleConfirmDelivery(w http.ResponseWriter, r *http.Request, ctx context.Context, idOrNumber string, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil || !claims.HasCapability("customer") {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	if err := orderRepo.ConfirmOrderDelivery(ctx, idOrNumber, claims.UserID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Order not found or not owned by user"}`, http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"status":  "delivered_confirmed",
		"message": "Order delivery confirmed; profit stays locked until the return window closes",
	})
}

func handleListOrders(w http.ResponseWriter, r *http.Request, ctx context.Context, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	orders, err := orderRepo.GetOrdersByCustomerUserID(ctx, claims.UserID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if orders == nil {
		orders = []domain.Order{}
	}

	json.NewEncoder(w).Encode(orders)
}

// ---- Team 03 (live parity, D19/D36/D64/D10) ----
// Route patterns: only /orders/<id>/shipping is a confirmed live
// pattern (MASTER-AUDIT D64, live frontend getOrderShipping). The
// cancel / return / return-quote / dispute patterns below follow the
// probed /api/v1/orders/<id>/cancel shape and the warehouse
// /api/v1/warehouse/orders/<id>/<action> shape, but the exact live
// URL for the order-pattern actions is unverified (TODO U01): a
// no-token probe returns 401 for any registered subtree because the
// auth middleware runs first, so 401 never proved these patterns.
// They are registered under the most-evidenced pattern with that
// TODO, never presented as confirmed live URLs.

func orderHandlerClaims(ctx context.Context) *auth.UserClaims {
	claims, _ := ctx.Value(userClaimsKey).(*auth.UserClaims)
	return claims
}

func handleOrderCancel(w http.ResponseWriter, r *http.Request, ctx context.Context, idOrNumber string, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	claims := orderHandlerClaims(ctx)
	if claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	isAdmin := claims.HasCapability("admin")
	if !isAdmin && !claims.HasCapability("customer") {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	if err := orderRepo.CancelOrder(ctx, idOrNumber, claims.UserID, isAdmin); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Order not found or not owned by user"}`, http.StatusNotFound)
			return
		}
		if strings.Contains(err.Error(), "cannot be cancelled") {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"orderId": idOrNumber,
		"status":  "cancelled",
	})
}

func handleOrderReturn(w http.ResponseWriter, r *http.Request, ctx context.Context, idOrNumber string, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	claims := orderHandlerClaims(ctx)
	if claims == nil || !claims.HasCapability("customer") {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	// FIX 5 (Team 03 G5): live clients send order_item_id,
	// warehouse_id and prepaid; the old camelCase shape still decodes.
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Reason      string `json:"reason"`
		WarehouseID string `json:"warehouseId"`
		Prepaid     bool   `json:"prepaid"`
		Items       []struct {
			OrderItemID string `json:"orderItemId"`
			Qty         int    `json:"qty"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	var snake struct {
		WarehouseID string `json:"warehouse_id"`
		Items       []struct {
			OrderItemID string `json:"order_item_id"`
			Qty         int    `json:"qty"`
		} `json:"items"`
	}
	_ = json.Unmarshal(body, &snake)
	if req.WarehouseID == "" {
		req.WarehouseID = snake.WarehouseID
	}
	if len(req.Items) == 0 && len(snake.Items) > 0 {
		for _, it := range snake.Items {
			req.Items = append(req.Items, struct {
				OrderItemID string `json:"orderItemId"`
				Qty         int    `json:"qty"`
			}{OrderItemID: it.OrderItemID, Qty: it.Qty})
		}
	}
	items := make([]repository.CustomerReturnItemRequest, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, repository.CustomerReturnItemRequest{OrderItemID: it.OrderItemID, Qty: it.Qty})
	}
	ret, err := orderRepo.CreateCustomerReturn(ctx, claims.UserID, idOrNumber, req.Reason, items, repository.CustomerReturnOptions{WarehouseID: req.WarehouseID, Prepaid: req.Prepaid})
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Order not found or not owned by user"}`, http.StatusNotFound)
			return
		}
		if errors.Is(err, repository.ErrReturnWindowClosed) {
			http.Error(w, `{"error":"Return window has closed"}`, http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"return": ret})
}

// FIX 14: return-quote means RETURN-SHIPPING COST on live
// ({groups, totalFareToman}), NOT a refund estimate. Groups are the
// per-origin return legs priced with the same admin weight formula
// as the forward quote; the fare formula itself for returns is
// inferred from that engine (live return fares are unprobed without
// a logged-in return flow). When rates cannot load, live's
// shippingQuoteUnavailable shape is returned.
func handleOrderReturnQuote(w http.ResponseWriter, r *http.Request, ctx context.Context, idOrNumber string, orderRepo *repository.OrderRepository, catalogRepo *repository.CatalogRepository, db *sql.DB) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	// FIX 5: accept the live order_item_id shape as well as camelCase.
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Items []struct {
			OrderItemID string `json:"orderItemId"`
		} `json:"items"`
	}
	_ = json.Unmarshal(body, &req)
	if len(req.Items) == 0 {
		var snake struct {
			Items []struct {
				OrderItemID string `json:"order_item_id"`
			} `json:"items"`
		}
		if json.Unmarshal(body, &snake) == nil {
			for _, it := range snake.Items {
				req.Items = append(req.Items, struct {
					OrderItemID string `json:"orderItemId"`
				}{OrderItemID: it.OrderItemID})
			}
		}
	}
	var itemIDs []string
	for _, it := range req.Items {
		itemIDs = append(itemIDs, it.OrderItemID)
	}
	order, err := orderRepo.GetOrderByID(ctx, idOrNumber)
	if err != nil || order == nil {
		order, err = orderRepo.GetOrderByNumber(ctx, idOrNumber)
	}
	if err != nil || order == nil {
		http.Error(w, `{"error":"Order not found"}`, http.StatusNotFound)
		return
	}
	// ISO-1 (seller scoping): return-quote previously served any order to
	// any authenticated caller on the /orders/ route (customer, seller,
	// admin, supplier) with no ownership check. Apply the same rule as
	// handleOrderShipping: admin, owning customer, or the seller /
	// supplier operationally tied to the order (CanReadOperationalTracking).
	claims := orderHandlerClaims(ctx)
	if claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	allowed := claims.HasCapability("admin")
	if !allowed && claims.HasCapability("customer") && order.CustomerUserID != nil && *order.CustomerUserID == claims.UserID {
		allowed = true
	}
	if !allowed {
		for _, capability := range []string{"seller", "supplier"} {
			if !claims.HasCapability(capability) {
				continue
			}
			ok, err := orderRepo.CanReadOperationalTracking(ctx, order.ID, claims.UserID, capability)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if ok {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	wanted := map[string]bool{}
	for _, id := range itemIDs {
		wanted[id] = true
	}
	// F3 (critic): the selection still validates the catalog rows,
	// but the FARE no longer groups by the products' CURRENT
	// fulfillment flag — it prices from the order's persisted
	// order_item_origins, grouped exactly the way the receive-time
	// settlement groups them (ComputeReturnQuoteLegs), so the quoted
	// fare and the destination below describe the same legs and the
	// quote equals the settlement fare for the same returned set.
	quantities := map[string]int{}
	var pricedItemIDs []string
	for _, it := range order.Items {
		if len(wanted) > 0 && !wanted[it.ID] {
			continue
		}
		if catalogRepo == nil {
			http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
			return
		}
		p, perr := catalogRepo.GetProductByID(ctx, it.ProductID)
		if perr != nil || p == nil {
			http.Error(w, `{"error":"Order or item not found"}`, http.StatusNotFound)
			return
		}
		quantities[it.ID] = it.Quantity
		pricedItemIDs = append(pricedItemIDs, it.ID)
	}
	if len(pricedItemIDs) == 0 {
		http.Error(w, `{"error":"Order or item not found"}`, http.StatusNotFound)
		return
	}
	// Destination (owner rule, Oct 28 2026): derived for the SAME
	// selected item set the fare prices, so the customer sees where
	// the parcel goes before filing. Derivation is display-only here;
	// creation re-derives authoritatively, so a failure omits the
	// field instead of failing the fare quote.
	destination, derr := orderRepo.DeriveReturnDestination(ctx, order.ID, pricedItemIDs)
	rates, rerr := loadShippingRates(ctx, db)
	if rerr != nil {
		payload := map[string]interface{}{
			"shippingQuoteUnavailable": true,
			"groups":                   []shippingQuoteLeg{},
			"totalFareToman":           0,
		}
		if derr == nil {
			payload["destination"] = destination
		}
		json.NewEncoder(w).Encode(payload)
		return
	}
	quoteLegs, quoteTotal, qerr := orderRepo.ComputeReturnQuoteLegs(ctx, order.ID, quantities, rates)
	if qerr != nil {
		http.Error(w, qerr.Error(), http.StatusInternalServerError)
		return
	}
	groups := make([]shippingQuoteLeg, 0, len(quoteLegs))
	for i, leg := range quoteLegs {
		groups = append(groups, shippingQuoteLeg{
			LegIndex: i, Index: i, OriginID: leg.Key, OriginLabel: leg.Label,
			Fare: leg.Fare, WeightGrams: leg.WeightGrams,
			BoxLabel: fmt.Sprintf("بسته %d", i+1),
			Source:   "weight_formula", Estimated: false,
		})
	}
	payload := map[string]interface{}{
		"groups":         groups,
		"totalFareToman": quoteTotal,
		"orderId":        order.ID,
		"orderNumber":    order.OrderNumber,
	}
	if derr == nil {
		payload["destination"] = destination
	}
	json.NewEncoder(w).Encode(payload)
}

func handleOrderDispute(w http.ResponseWriter, r *http.Request, ctx context.Context, idOrNumber string, finRepo *repository.FinanceRepository, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if finRepo == nil || orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	claims := orderHandlerClaims(ctx)
	if claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	order, err := orderRepo.GetOrderByID(ctx, idOrNumber)
	if err != nil || order == nil {
		order, err = orderRepo.GetOrderByNumber(ctx, idOrNumber)
	}
	if err != nil || order == nil {
		http.Error(w, `{"error":"Order not found"}`, http.StatusNotFound)
		return
	}
	// ISO-1 (seller scoping): the old check only constrained callers WITH
	// the customer capability, so a pure seller/supplier token (route
	// /api/v1/orders/ admits seller and supplier roles) skipped ownership
	// entirely and could file a dispute on ANY store's order. Dispute
	// filing is a customer action (live client: customer orders view
	// only): admin bypass is kept, a customer must own the order, and
	// every other role is denied.
	if !claims.HasCapability("admin") {
		if claims.HasCapability("customer") {
			if order.CustomerUserID == nil || *order.CustomerUserID != claims.UserID {
				http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
				return
			}
		} else {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
	}
	var req struct {
		Type         string `json:"type"`
		ClaimAmount  int64  `json:"claimAmount"`
		Reason       string `json:"reason"`
		ProductTitle string `json:"productTitle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	// FIX 14: the live client sends {reason} only (no type). An
	// empty type is accepted and stored as-is -- live's stored value
	// for reason-only filings is unprobed, so none is invented.
	// A provided type must still be one of the two known values.
	if req.Type != "" && req.Type != "return_7day" && req.Type != "delivery_undelivered" {
		http.Error(w, `{"error":"Dispute type must be return_7day or delivery_undelivered"}`, http.StatusBadRequest)
		return
	}
	// TODO(U25): the 7-day window enforcement on filing is unproven
	// in live (no live return_7day dispute exists). The check below
	// reads the recorded window and rejects only a return_7day filing
	// after it; a NULL window never rejects (TODO U19).
	if req.Type == "return_7day" && order.ReturnWindowEndsAt != nil && order.ReturnWindowEndsAt.Before(time.Now()) {
		http.Error(w, `{"error":"Return window has closed"}`, http.StatusBadRequest)
		return
	}
	dispute := domain.Dispute{
		ID:           fmt.Sprintf("DISP-%d", time.Now().UnixMilli()%100000),
		OrderID:      order.ID,
		OrderNumber:  order.OrderNumber,
		Type:         req.Type,
		CustomerName: order.CustomerFullName,
		ProductTitle: req.ProductTitle,
		ClaimAmount:  req.ClaimAmount,
		Reason:       req.Reason,
		StoreID:      order.StoreID,
		Status:       "pending",
		CreatedAt:    time.Now(),
	}
	if err := finRepo.CreateDispute(ctx, dispute); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !claims.HasCapability("admin") && (order.CustomerUserID == nil || *order.CustomerUserID != claims.UserID) {
		dispute.CustomerName = ""
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dispute)
}

// handleOrderShipping serves GET /api/v1/orders/<id>/shipping (D64,
// live frontend getOrderShipping) plus the D19 handleOrderShipping
// name: carrier, tracking, pack/ship times and the direct supplier
// shipment, with the same ownership rules as order tracking.
func handleOrderShipping(w http.ResponseWriter, r *http.Request, ctx context.Context, idOrNumber string, orderRepo *repository.OrderRepository, shipmentRepo *repository.SupplierShipmentRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	order, err := orderRepo.GetOrderByID(ctx, idOrNumber)
	if err != nil || order == nil {
		order, err = orderRepo.GetOrderByNumber(ctx, idOrNumber)
	}
	if err != nil || order == nil {
		http.Error(w, `{"error":"Order not found"}`, http.StatusNotFound)
		return
	}
	claims := orderHandlerClaims(ctx)
	if claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	allowed := claims.HasCapability("admin")
	supplierScoped := false
	if !allowed && claims.HasCapability("customer") && order.CustomerUserID != nil && *order.CustomerUserID == claims.UserID {
		allowed = true
	}
	if !allowed {
		for _, capability := range []string{"seller", "supplier"} {
			if !claims.HasCapability(capability) {
				continue
			}
			ok, err := orderRepo.CanReadOperationalTracking(ctx, order.ID, claims.UserID, capability)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if ok {
				allowed = true
				if capability == "supplier" {
					supplierScoped = true
				}
				break
			}
		}
	}
	if !allowed {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	response := map[string]interface{}{
		"success":            true,
		"orderId":            order.ID,
		"orderNumber":        order.OrderNumber,
		"status":             order.Status,
		"fulfillmentMethod":  order.FulfillmentMethod,
		"carrier":            order.Carrier,
		"trackingNumber":     order.TrackingNumber,
		"trackingCode":       order.TrackingCode,
		"shippingLabel":      order.ShippingLabel,
		"packedAt":           order.PackedAt,
		"shippedAt":          order.ShippedAt,
		"boxSize":            order.BoxSize,
		"weightGrams":        order.WeightGrams,
		"returnWindowEndsAt": order.ReturnWindowEndsAt,
	}
	// Merge (f): the order-level warehouse label is not leg-scoped.
	// Supplier-only callers must not see it (same fail-closed rule
	// as the carrier/trackingCode clearing below), whether or not
	// their own leg resolves.
	if supplierScoped {
		response["shippingLabel"] = ""
	}
	if supplierScoped && shipmentRepo != nil {
		// Supplier-only callers see only their own leg's carrier/tracking:
		// the unscoped latest-shipment lookup previously exposed another
		// supplier's tracking on multi-supplier orders, and the order-level
		// carrier/tracking fields carry the last shipper's values. For
		// mixed orders this also surfaces the supplier's own leg, which
		// the supplier_direct-only branch below hid entirely.
		own, ownErr := shipmentRepo.GetShipmentByOrderIDForUser(ctx, order.ID, claims.UserID)
		if ownErr == nil && own != nil {
			if own.Carrier != nil {
				response["carrier"] = *own.Carrier
			}
			if own.TrackingNumber != nil {
				response["trackingNumber"] = *own.TrackingNumber
				response["trackingCode"] = *own.TrackingNumber
			}
			response["shipment"] = map[string]interface{}{
				"carrier":        own.Carrier,
				"trackingNumber": own.TrackingNumber,
				"shippedAt":      own.ShippedAt,
				"deliveredAt":    own.DeliveredAt,
				"status":         own.Status,
			}
		} else {
			// Fail closed: no own leg, so no other leg's carrier,
			// tracking number, or order-level tracking code.
			response["carrier"] = ""
			response["trackingNumber"] = ""
			response["trackingCode"] = ""
		}
	} else if order.FulfillmentMethod == "supplier_direct" && shipmentRepo != nil {
		shipment, err := shipmentRepo.GetShipmentByOrderID(ctx, order.ID)
		if err == nil && shipment != nil {
			response["shipment"] = map[string]interface{}{
				"carrier":        shipment.Carrier,
				"trackingNumber": shipment.TrackingNumber,
				"shippedAt":      shipment.ShippedAt,
				"deliveredAt":    shipment.DeliveredAt,
				"status":         shipment.Status,
			}
		}
	}
	json.NewEncoder(w).Encode(response)
}

// ---- Customer returns HTTP surface (D36, D10) ----

func handleListCustomerReturns(w http.ResponseWriter, r *http.Request, ctx context.Context, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	claims := orderHandlerClaims(ctx)
	if claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var returns []repository.CustomerReturn
	var err error
	if claims.HasCapability("admin") {
		customerID := strings.TrimSpace(r.URL.Query().Get("customerId"))
		returns, err = orderRepo.ListCustomerReturns(ctx, customerID)
	} else {
		returns, err = orderRepo.ListCustomerReturns(ctx, claims.UserID)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if returns == nil {
		returns = []repository.CustomerReturn{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"returns": returns})
}

func handleCreateCustomerReturnBody(w http.ResponseWriter, r *http.Request, ctx context.Context, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	claims := orderHandlerClaims(ctx)
	if claims == nil || !claims.HasCapability("customer") {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		OrderID     string `json:"orderId"`
		Reason      string `json:"reason"`
		WarehouseID string `json:"warehouseId"`
		Prepaid     bool   `json:"prepaid"`
		Items       []struct {
			OrderItemID string `json:"orderItemId"`
			Qty         int    `json:"qty"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.OrderID == "" {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}
	var snake struct {
		WarehouseID string `json:"warehouse_id"`
		Items       []struct {
			OrderItemID string `json:"order_item_id"`
			Qty         int    `json:"qty"`
		} `json:"items"`
	}
	_ = json.Unmarshal(body, &snake)
	if req.WarehouseID == "" {
		req.WarehouseID = snake.WarehouseID
	}
	if len(req.Items) == 0 && len(snake.Items) > 0 {
		for _, it := range snake.Items {
			req.Items = append(req.Items, struct {
				OrderItemID string `json:"orderItemId"`
				Qty         int    `json:"qty"`
			}{OrderItemID: it.OrderItemID, Qty: it.Qty})
		}
	}
	items := make([]repository.CustomerReturnItemRequest, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, repository.CustomerReturnItemRequest{OrderItemID: it.OrderItemID, Qty: it.Qty})
	}
	ret, err := orderRepo.CreateCustomerReturn(ctx, claims.UserID, req.OrderID, req.Reason, items, repository.CustomerReturnOptions{WarehouseID: req.WarehouseID, Prepaid: req.Prepaid})
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Order not found or not owned by user"}`, http.StatusNotFound)
			return
		}
		if errors.Is(err, repository.ErrReturnWindowClosed) {
			http.Error(w, `{"error":"Return window has closed"}`, http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"return": ret})
}

func handleGetCustomerReturn(w http.ResponseWriter, r *http.Request, ctx context.Context, id string, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	claims := orderHandlerClaims(ctx)
	if claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ret, err := orderRepo.GetCustomerReturn(ctx, id)
	if err != nil || ret == nil {
		http.Error(w, `{"error":"Return not found"}`, http.StatusNotFound)
		return
	}
	if !claims.HasCapability("admin") && !claims.HasCapability("warehouse_staff") && !claims.HasCapability("logistics_specialist") {
		if ret.CustomerID != claims.UserID {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"return": ret})
}

// handleCustomerReturnAction serves POST /api/v1/returns/<id>/<action>:
// approve / reject (admin), receive (warehouse QC), refund (admin or
// finance, wallet refund), cancel (customer owner or admin).
// The cancel pattern is confirmed by migration 202610190005, which
// names POST /api/v1/returns/:id/cancel; the other action patterns
// follow the same shape with TODO(U01) for their exact live URLs.
func handleCustomerReturnAction(w http.ResponseWriter, r *http.Request, ctx context.Context, id, action string, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	claims := orderHandlerClaims(ctx)
	if claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var noteReq struct {
		AdminNote string `json:"adminNote"`
		Items     []struct {
			ReturnItemID string `json:"returnItemId"`
			QCStatus     string `json:"qcStatus"`
		} `json:"items"`
	}
	_ = json.NewDecoder(r.Body).Decode(&noteReq)
	switch action {
	case "approve", "reject":
		if !claims.HasCapability("admin") {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		var err error
		if action == "approve" {
			err = orderRepo.ApproveCustomerReturn(ctx, id, noteReq.AdminNote)
		} else {
			err = orderRepo.RejectCustomerReturn(ctx, id, noteReq.AdminNote)
		}
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Return not found or not in requested status"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "receive":
		if !claims.HasCapability("warehouse_staff") && !claims.HasCapability("logistics_specialist") && !claims.HasCapability("admin") {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		qc := make([]repository.CustomerReturnItemQC, 0, len(noteReq.Items))
		for _, it := range noteReq.Items {
			qc = append(qc, repository.CustomerReturnItemQC{ReturnItemID: it.ReturnItemID, QCStatus: it.QCStatus})
		}
		settlement, err := orderRepo.ReceiveCustomerReturn(ctx, id, qc, claims.UserID)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error":"Return or item not found, or return not approved"}`, http.StatusNotFound)
				return
			}
			if errors.Is(err, repository.ErrReturnCODRefused) {
				http.Error(w, `{"error":"`+repository.ErrReturnCODRefused.Error()+`"}`, http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ret, err := orderRepo.GetCustomerReturn(ctx, id)
		if err != nil || ret == nil {
			http.Error(w, `{"error":"Return not found"}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"return": ret, "returnShipping": settlement})
		return
	case "refund":
		if !claims.HasCapability("admin") && !claims.HasCapability("finance_specialist") {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		amount, settlement, err := orderRepo.RefundCustomerReturn(ctx, id, claims.UserID)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error":"Return not found"}`, http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]interface{}{"success": true, "returnId": id, "status": "refunded", "refundAmount": amount}
		if settlement != nil {
			resp["returnShippingAmount"] = settlement.ReturnShippingAmount
			resp["returnShippingBearer"] = settlement.ReturnShippingBearer
			resp["returnShippingSupplierBorne"] = settlement.SupplierBorneTotal
			resp["returnShippingCustomerBorne"] = settlement.CustomerBorneTotal
			resp["returnShippingCustomerDebited"] = settlement.CustomerDebited
			resp["returnShippingWrittenOff"] = settlement.CustomerWrittenOff
		}
		json.NewEncoder(w).Encode(resp)
		return
	case "ship":
		// FIX 14: live's ship action -- the customer ships an approved
		// return back (optional tracking number). Owner or admin only.
		if !claims.HasCapability("admin") {
			ret0, err := orderRepo.GetCustomerReturn(ctx, id)
			if err != nil || ret0 == nil {
				http.Error(w, `{"error":"Return not found"}`, http.StatusNotFound)
				return
			}
			if ret0.CustomerID != claims.UserID {
				http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
				return
			}
		}
		// FIX 14b: live's client sends tracking_number (snake_case) AND
		// prepaid; the old code decoded only trackingNumber, so a live-
		// shaped ship request silently dropped both. Accept both
		// spellings (camelCase wins on conflict) and persist prepaid
		// only when the client actually sends it (nil = untouched).
		var shipReq struct {
			TrackingNumber  string `json:"trackingNumber"`
			TrackingNumber2 string `json:"tracking_number"`
			Prepaid         *bool  `json:"prepaid"`
		}
		_ = json.NewDecoder(r.Body).Decode(&shipReq)
		tracking := shipReq.TrackingNumber
		if tracking == "" {
			tracking = shipReq.TrackingNumber2
		}
		if err := orderRepo.ShipCustomerReturn(ctx, id, tracking, shipReq.Prepaid); err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error":"Return not found or not shippable"}`, http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "cancel":
		ret, err := orderRepo.GetCustomerReturn(ctx, id)
		if err != nil || ret == nil {
			http.Error(w, `{"error":"Return not found"}`, http.StatusNotFound)
			return
		}
		if !claims.HasCapability("admin") && ret.CustomerID != claims.UserID {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		if err := orderRepo.CancelCustomerReturn(ctx, id); err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, `{"error":"Return not found or not cancellable"}`, http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	ret, err := orderRepo.GetCustomerReturn(ctx, id)
	if err != nil || ret == nil {
		http.Error(w, `{"error":"Return not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"return": ret})
}

// handleStoreReturns serves stores/:slug/returns (D10) after the
// stores switch has authorised the store read.
func handleStoreReturns(w http.ResponseWriter, r *http.Request, ctx context.Context, storeID string, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	returns, err := orderRepo.ListCustomerReturnsByStore(ctx, storeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if returns == nil {
		returns = []repository.CustomerReturn{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"returns": returns})
}

// handleWarehousesReturns serves GET /api/v1/warehouses/returns (D10)
// from the returns engine. It is a different route from Team 01's
// /api/v1/warehouse/return-backs stub, which is not touched here.
func handleWarehousesReturns(w http.ResponseWriter, r *http.Request, ctx context.Context, orderRepo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	// Optional destination filter (?warehouseId= / ?warehouse_id=):
	// a central warehouse id or a supplier warehouse id narrows the
	// queue to that destination's returns only.
	destinationID := r.URL.Query().Get("warehouseId")
	if destinationID == "" {
		destinationID = r.URL.Query().Get("warehouse_id")
	}
	returns, err := orderRepo.ListCustomerReturnsForWarehouse(ctx, destinationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if returns == nil {
		returns = []repository.CustomerReturn{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"returns": returns})
}
