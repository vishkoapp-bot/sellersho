package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/bank"
	"github.com/drup/platform/internal/database/seeder"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/payment"
	"github.com/drup/platform/internal/repository"
)

func handleShaparakToken(w http.ResponseWriter, r *http.Request, ctx context.Context, orderRepo *repository.OrderRepository, catalogRepo *repository.CatalogRepository, redisClient *repository.RedisClient) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		OrderID     string `json:"orderId"`
		OrderNumber string `json:"orderNumber"`
		Amount      int64  `json:"amount"`
		RedirectURL string `json:"redirectUrl"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if orderRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	orderID := req.OrderID
	orderRecord, err := orderRepo.GetOrderByID(ctx, req.OrderID)
	if (err != nil || orderRecord == nil) && req.OrderNumber != "" {
		orderRecord, err = orderRepo.GetOrderByNumber(ctx, req.OrderNumber)
	}
	if err != nil || orderRecord == nil {
		http.Error(w, `{"error":"Order not found"}`, http.StatusNotFound)
		return
	}
	if !claims.HasDirectCapability("admin") && (orderRecord.CustomerUserID == nil || *orderRecord.CustomerUserID != claims.UserID) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	orderID = orderRecord.ID
	var expiresAt *time.Time
	if err := orderRepo.PaymentDeadline(ctx, orderID).Scan(&expiresAt); err != nil {
		writeInternalError(w, err)
		return
	}
	existing, existingErr := orderRepo.GetPaymentTransactionByOrderID(ctx, orderID)
	if existingErr != nil {
		http.Error(w, existingErr.Error(), http.StatusInternalServerError)
		return
	}
	if existing != nil && existing.Status == "paid" {
		writeTokenResponse(w, existing.GatewayToken, existing.OrderNumber, existing.Amount, "توکن پرداخت موجود بازگردانده شد")
		return
	}
	if orderRecord.PaymentStatus != "paid" && (orderRecord.Status == "cancelled" || (expiresAt != nil && !time.Now().Before(*expiresAt))) {
		writePaymentError(w, http.StatusConflict, "payment_expired", "مهلت پرداخت سفارش تمام شده است")
		return
	}
	if expiresAt == nil {
		writePaymentError(w, http.StatusConflict, "payment_deadline_review_required", "مهلت پرداخت ثبت نشده است؛ سفارش نیاز به بررسی دارد")
		return
	}
	if catalogRepo == nil || redisClient == nil {
		writePaymentError(w, http.StatusServiceUnavailable, "inventory_lock_unavailable", "رزرو موجودی برای شروع پرداخت در دسترس نیست")
		return
	}

	reservedProducts, err := reserveOrderInventory(ctx, orderRecord, catalogRepo, redisClient, *expiresAt)
	if err != nil {
		if errors.Is(err, repository.ErrPaymentExpired) {
			writePaymentError(w, http.StatusConflict, "payment_expired", "مهلت پرداخت سفارش تمام شده است")
			return
		}
		if errors.Is(err, repository.ErrInsufficientInventory) {
			writePaymentError(w, http.StatusConflict, "insufficient_stock", err.Error())
			return
		}
		writeInternalError(w, err)
		return
	}
	if existing != nil {
		writeTokenResponse(w, existing.GatewayToken, existing.OrderNumber, existing.Amount, "توکن پرداخت موجود بازگردانده شد")
		return
	}

	token := fmt.Sprintf("SHP-TKN-%d", secureRandomInt64(100000, 999999))
	if err := orderRepo.CreatePaymentTransaction(ctx, repository.PaymentTransaction{
		ID: fmt.Sprintf("pay-%d", time.Now().UnixNano()), OrderID: orderRecord.ID,
		OrderNumber: orderRecord.OrderNumber, GatewayToken: token, Amount: orderRecord.PayableAmount,
	}); err != nil {
		_ = redisClient.ReleaseOrderInventory(ctx, orderID, reservedProducts...)
		writeInternalError(w, err)
		return
	}

	persisted, err := orderRepo.GetPaymentTransactionByOrderID(ctx, orderID)
	if err != nil || persisted == nil {
		_ = redisClient.ReleaseOrderInventory(ctx, orderID, reservedProducts...)
		if err != nil {
			writeInternalError(w, err)
		} else {
			http.Error(w, "payment transaction was not persisted", http.StatusInternalServerError)
		}
		return
	}
	writeTokenResponse(w, persisted.GatewayToken, persisted.OrderNumber, persisted.Amount, "توکن پرداخت اینترنتی شاپرک صادر شد")
}

type PaymentOrderRepository interface {
	RejectVerifiedPaymentAdmission(ctx context.Context, token string) (bool, error)
	BeginPaymentVerification(ctx context.Context, token, fundingKind string) (string, error)
	RecordPaymentVerification(ctx context.Context, id string, result payment.VerificationResult) error
	RecordPaymentVerificationUnknown(ctx context.Context, id, reason string) error
	GetPaymentTransaction(ctx context.Context, gatewayToken string) (*repository.PaymentTransaction, error)
	GetOrderByID(ctx context.Context, id string) (*domain.Order, error)
	MarkPaymentPaid(ctx context.Context, gatewayToken, rrn, cardMask string) (*repository.PaymentTransaction, error)
	// RecordPaymentFailure records a gateway verification failure on
	// the payment transaction/order and notifies the customer (P1-9).
	RecordPaymentFailure(ctx context.Context, gatewayToken string) error
}

// IsCallbackSimulatorAllowed evaluates if the callback simulator route is allowed in the current environment (fail-closed allowlist)
func IsCallbackSimulatorAllowed(env string) bool {
	return seeder.IsDevEnvironment(env)
}

// HandleShaparakCallbackHTTP is the HTTP handler entrypoint for /api/v1/payments/shaparak/callback.
// It enforces the environment gate (fail-closed) before executing the callback handler.
func HandleShaparakCallbackHTTP(
	w http.ResponseWriter,
	r *http.Request,
	env string,
	orderRepo PaymentOrderRepository,
	redisClient *repository.RedisClient,
	gatewayClient payment.PaymentGatewayClient,
) {
	if !IsCallbackSimulatorAllowed(env) {
		http.Error(w, `{"error":"Shaparak callback simulator is strictly restricted to development/test environments (fail-closed)"}`, http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	handleShaparakCallback(w, r, r.Context(), orderRepo, redisClient, gatewayClient)
}

func handleShaparakCallback(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	orderRepo PaymentOrderRepository,
	redisClient *repository.RedisClient,
	gatewayClient payment.PaymentGatewayClient,
) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Token       string `json:"token"`
		OrderNumber string `json:"orderNumber"`
		CardMask    string `json:"cardMask"`
		Amount      int64  `json:"amount,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid callback payload"}`, http.StatusBadRequest)
		return
	}

	cardMask := req.CardMask
	if cardMask == "" {
		cardMask = "6037-99**-****-1234"
	}

	if orderRepo == nil || req.Token == "" {
		http.Error(w, `{"error":"Invalid payment token"}`, http.StatusBadRequest)
		return
	}
	existing, err := orderRepo.GetPaymentTransaction(ctx, req.Token)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if existing == nil || (req.OrderNumber != "" && existing.OrderNumber != req.OrderNumber) {
		http.Error(w, `{"error":"Payment transaction not found"}`, http.StatusNotFound)
		return
	}

	// 1. Verify transaction amount matches existing transaction record
	if req.Amount > 0 && req.Amount != existing.Amount {
		writePaymentError(w, http.StatusBadRequest, "amount_mismatch", "مبلغ تراکنش با رکورد پرداخت همخوانی ندارد")
		return
	}

	// 2. Gateway Client Verification (Strict Fail-Closed)
	if gatewayClient == nil {
		writePaymentError(w, http.StatusServiceUnavailable, "gateway_unavailable", "ارتباط با درگاه پرداخت شاپرک برقرار نیست")
		return
	}
	// This endpoint is a gated simulator. Never label its receipts real cash.
	verificationID, err := orderRepo.BeginPaymentVerification(ctx, req.Token, "simulated")
	if err != nil {
		writeInternalError(w, err)
		return
	}
	verifyRes, verifyErr := gatewayClient.VerifyTransaction(ctx, req.Token, existing.Amount)
	if verifyErr != nil || verifyRes == nil || !verifyRes.Success {
		if err := orderRepo.RecordPaymentVerificationUnknown(ctx, verificationID, "provider_unavailable"); err != nil {
			writeInternalError(w, err)
			return
		}
		writePaymentError(w, http.StatusPaymentRequired, "gateway_verification_failed", "تأییدیه تراکنش توسط درگاه شاپرک صادر نشد")
		return
	}

	if err := orderRepo.RecordPaymentVerification(ctx, verificationID, *verifyRes); err != nil {
		if errors.Is(err, payment.ErrAmountMismatch) {
			writePaymentError(w, http.StatusConflict, "amount_mismatch", "مبلغ تأییدشده نیاز به بررسی دارد")
			return
		}
		writeInternalError(w, err)
		return
	}
	rrn := existing.RRN.String
	if existing.Status != "paid" {
		if redisClient == nil {
			writePaymentError(w, http.StatusServiceUnavailable, "inventory_lock_unavailable", "اعتبارسنجی رزرو موجودی در دسترس نیست")
			return
		}
		orderRecord, orderErr := orderRepo.GetOrderByID(ctx, existing.OrderID)
		if orderErr != nil || orderRecord == nil {
			http.Error(w, "Order not found", http.StatusNotFound)
			return
		}
		productQuantities := make(map[repository.InventoryHoldRef]int)
		for _, item := range orderRecord.Items {
			ref := repository.InventoryHoldRef{ProductID: item.ProductID}
			if item.VariantID != nil {
				ref.VariantID = *item.VariantID
			}
			productQuantities[ref] += item.Quantity
		}
		productIDs := make([]repository.InventoryHoldRef, 0, len(productQuantities))
		for ref, quantity := range productQuantities {
			productIDs = append(productIDs, ref)
			held, holdErr := redisClient.HasInventoryHold(ctx, existing.OrderID, ref.ProductID, ref.VariantID, quantity)
			if holdErr != nil {
				writeInternalError(w, holdErr)
				return
			}
			if !held {
				cancelled, compensationErr := orderRepo.RejectVerifiedPaymentAdmission(ctx, req.Token)
				if compensationErr != nil {
					if errors.Is(compensationErr, repository.ErrCapturePendingManualResolution) {
						writePaymentError(w, http.StatusConflict, "capture_pending_manual_resolution", "تکمیل پرداخت نیاز به بررسی دارد")
						return
					}
					writeInternalError(w, compensationErr)
					return
				}
				if cancelled {
					_ = redisClient.ReleaseOrderInventory(ctx, existing.OrderID, productIDs...)
					writePaymentError(w, http.StatusConflict, "inventory_hold_expired", "رزرو پایان یافته؛ وضعیت بازپرداخت در سفارش ثبت شد")
					return
				}
				// Another callback already captured stock and released this Redis hold.
				break
			}
		}

		rrn = verifyRes.RRN
		if verifyRes.CardMask != "" {
			cardMask = verifyRes.CardMask
		}
		existing, err = orderRepo.MarkPaymentPaid(ctx, req.Token, rrn, cardMask)
		if err != nil {
			if errors.Is(err, repository.ErrCapturePendingManualResolution) {
				writePaymentError(w, http.StatusConflict, "capture_pending_manual_resolution", "تکمیل پرداخت نیاز به بررسی دارد")
				return
			}
			if errors.Is(err, repository.ErrPaymentExpired) {
				writePaymentError(w, http.StatusConflict, "payment_expired", "مهلت پرداخت سفارش تمام شده است")
				return
			}
			if errors.Is(err, repository.ErrUnitCoverageGap) {
				writePaymentError(w, http.StatusConflict, "unit_coverage_gap", "موجودی کالا نیاز به بررسی دارد")
				return
			}
			if errors.Is(err, repository.ErrInsufficientInventory) {
				writePaymentError(w, http.StatusConflict, "insufficient_stock", "موجودی کالا پیش از تکمیل پرداخت مصرف شده است")
				return
			}
			writeInternalError(w, err)
			return
		}
		if redisClient != nil {
			_ = redisClient.ReleaseOrderInventory(ctx, existing.OrderID, productIDs...)
		}
	} else {
		// Paid records can belong to a cancelled/extra capture. The ordered repository
		// guard still decides fulfillment admission on every replay.
		existing, err = orderRepo.MarkPaymentPaid(ctx, req.Token, verifyRes.RRN, cardMask)
		if err != nil {
			if errors.Is(err, repository.ErrPaymentExpired) {
				writePaymentError(w, http.StatusConflict, "payment_expired", "وضعیت بازپرداخت را در سفارش پیگیری کنید")
				return
			}
			writeInternalError(w, err)
			return
		}
		if existing.CardMask.Valid {
			cardMask = existing.CardMask.String
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"status":      "paid",
		"rrn":         rrn,
		"cardMask":    cardMask,
		"orderNumber": existing.OrderNumber,
		"paymentDate": time.Now().Format("2006-01-02 15:04:05"),
		"message":     "تراکنش با موفقیت در شاپرک ثبت و تسویه گردید",
	})
}

func reserveOrderInventory(ctx context.Context, order *domain.Order, catalogRepo *repository.CatalogRepository, redisClient *repository.RedisClient, deadline time.Time) ([]repository.InventoryHoldRef, error) {
	// PERF-04 D-06: holds are per-SKU, not per-product. Lines for different
	// variants of one product hold against different pools, each bounded by
	// its own stock; the PG capture in captureOrderInventoryTx stays the
	// authoritative backstop.
	quantities := make(map[repository.InventoryHoldRef]int)
	for _, item := range order.Items {
		ref := repository.InventoryHoldRef{ProductID: item.ProductID}
		if item.VariantID != nil {
			ref.VariantID = *item.VariantID
		}
		quantities[ref] += item.Quantity
	}

	reservedProducts := make([]repository.InventoryHoldRef, 0, len(quantities))
	for ref, qty := range quantities {
		product, err := catalogRepo.GetProductByID(ctx, ref.ProductID)
		if err != nil || product == nil {
			_ = redisClient.ReleaseOrderInventory(ctx, order.ID, reservedProducts...)
			return nil, fmt.Errorf("%w: محصول %s دیگر موجود نیست", repository.ErrInsufficientInventory, ref.ProductID)
		}
		availableStock := product.Stock
		if ref.VariantID != "" {
			// A tracked variant's own stock binds admission; legacy
			// variants fall back to the product stock, matching the PG
			// capture (which only enforces variant stock for tracked SKUs).
			variantStock, mode, active, verr := catalogRepo.VariantInventory(ctx, ref.ProductID, ref.VariantID)
			if verr != nil {
				_ = redisClient.ReleaseOrderInventory(ctx, order.ID, reservedProducts...)
				if errors.Is(verr, sql.ErrNoRows) {
					return nil, fmt.Errorf("%w: محصول %s دیگر موجود نیست", repository.ErrInsufficientInventory, ref.ProductID)
				}
				return nil, verr
			}
			if mode == "tracked" {
				if !active {
					_ = redisClient.ReleaseOrderInventory(ctx, order.ID, reservedProducts...)
					return nil, fmt.Errorf("%w: موجودی محصول %s برای پرداخت کافی نیست", repository.ErrInsufficientInventory, ref.ProductID)
				}
				availableStock = variantStock
			}
		}
		if !time.Now().Before(deadline) {
			_ = redisClient.ReleaseOrderInventory(ctx, order.ID, reservedProducts...)
			return nil, repository.ErrPaymentExpired
		}
		holdDeadline := deadline
		if maximum := time.Now().Add(15 * time.Minute); maximum.Before(holdDeadline) {
			holdDeadline = maximum
		}
		if err := redisClient.HoldInventoryUntil(ctx, order.ID, ref.ProductID, ref.VariantID, qty, availableStock, holdDeadline); err != nil {
			_ = redisClient.ReleaseOrderInventory(ctx, order.ID, reservedProducts...)
			if errors.Is(err, repository.ErrInsufficientInventory) {
				return nil, fmt.Errorf("%w: موجودی محصول %s برای پرداخت کافی نیست", repository.ErrInsufficientInventory, ref.ProductID)
			}
			return nil, err
		}
		reservedProducts = append(reservedProducts, ref)
	}
	return reservedProducts, nil
}

func writeTokenResponse(w http.ResponseWriter, token, orderNumber string, amount int64, message string) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"token":       token,
		"paymentUrl":  fmt.Sprintf("https://gateway.shaparak.ir/payment/%s", token),
		"orderNumber": orderNumber,
		"amount":      amount,
		"message":     message,
	})
}

func writePaymentError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message, "code": code})
}

func handleVerifySheba(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Sheba string `json:"sheba"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}

	res := bank.VerifySheba(req.Sheba)
	json.NewEncoder(w).Encode(res)
}
