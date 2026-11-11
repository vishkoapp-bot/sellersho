package main

import (
	"context"
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
	if orderRecord.PaymentStatus != "paid" && (orderRecord.Status == "cancelled" || (expiresAt != nil && !time.Now().Before(*expiresAt))) {
		writePaymentError(w, http.StatusConflict, "payment_expired", "مهلت پرداخت سفارش تمام شده است")
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
	if catalogRepo == nil || redisClient == nil {
		writePaymentError(w, http.StatusServiceUnavailable, "inventory_lock_unavailable", "رزرو موجودی برای شروع پرداخت در دسترس نیست")
		return
	}

	reservedProducts, err := reserveOrderInventory(ctx, orderRecord, catalogRepo, redisClient)
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientInventory) {
			writePaymentError(w, http.StatusConflict, "insufficient_stock", err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			http.Error(w, "payment transaction was not persisted", http.StatusInternalServerError)
		}
		return
	}
	writeTokenResponse(w, persisted.GatewayToken, persisted.OrderNumber, persisted.Amount, "توکن پرداخت اینترنتی شاپرک صادر شد")
}

type PaymentOrderRepository interface {
	GetPaymentTransaction(ctx context.Context, gatewayToken string) (*repository.PaymentTransaction, error)
	GetOrderByID(ctx context.Context, id string) (*domain.Order, error)
	MarkPaymentPaid(ctx context.Context, gatewayToken, rrn, cardMask string) (*repository.PaymentTransaction, error)
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
	verifyRes, verifyErr := gatewayClient.VerifyTransaction(ctx, req.Token, existing.Amount)
	if verifyErr != nil || verifyRes == nil || !verifyRes.Success {
		writePaymentError(w, http.StatusPaymentRequired, "gateway_verification_failed", "تأییدیه تراکنش توسط درگاه شاپرک صادر نشد")
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
		productQuantities := make(map[string]int)
		for _, item := range orderRecord.Items {
			productQuantities[item.ProductID] += item.Quantity
		}
		productIDs := make([]string, 0, len(productQuantities))
		for productID, quantity := range productQuantities {
			productIDs = append(productIDs, productID)
			held, holdErr := redisClient.HasInventoryHold(ctx, existing.OrderID, productID, quantity)
			if holdErr != nil {
				writeInternalError(w, holdErr)
				return
			}
			if !held {
				writePaymentError(w, http.StatusConflict, "inventory_hold_expired", "مهلت رزرو موجودی برای این پرداخت به پایان رسیده است")
				return
			}
		}

		rrn = verifyRes.RRN
		if verifyRes.CardMask != "" {
			cardMask = verifyRes.CardMask
		}
		paymentOrderID := existing.OrderID
		existing, err = orderRepo.MarkPaymentPaid(ctx, req.Token, rrn, cardMask)
		if err != nil {
			if errors.Is(err, repository.ErrPaymentExpired) {
				writePaymentError(w, http.StatusConflict, "payment_expired", "مهلت پرداخت سفارش تمام شده است")
				return
			}
			if errors.Is(err, repository.ErrInsufficientInventory) {
				_ = redisClient.ReleaseOrderInventory(ctx, paymentOrderID, productIDs...)
				writePaymentError(w, http.StatusConflict, "insufficient_stock", "موجودی کالا پیش از تکمیل پرداخت مصرف شده است")
				return
			}
			writeInternalError(w, err)
			return
		}
		if redisClient != nil {
			_ = redisClient.ReleaseOrderInventory(ctx, existing.OrderID, productIDs...)
		}
	} else if existing.CardMask.Valid {
		cardMask = existing.CardMask.String
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

func reserveOrderInventory(ctx context.Context, order *domain.Order, catalogRepo *repository.CatalogRepository, redisClient *repository.RedisClient) ([]string, error) {
	quantities := make(map[string]int)
	for _, item := range order.Items {
		quantities[item.ProductID] += item.Quantity
	}

	reservedProducts := make([]string, 0, len(quantities))
	for productID, qty := range quantities {
		product, err := catalogRepo.GetProductByID(ctx, productID)
		if err != nil || product == nil {
			_ = redisClient.ReleaseOrderInventory(ctx, order.ID, reservedProducts...)
			return nil, fmt.Errorf("%w: محصول %s دیگر موجود نیست", repository.ErrInsufficientInventory, productID)
		}
		if err := redisClient.HoldInventory(ctx, order.ID, productID, qty, product.Stock, 15*time.Minute); err != nil {
			_ = redisClient.ReleaseOrderInventory(ctx, order.ID, reservedProducts...)
			if errors.Is(err, repository.ErrInsufficientInventory) {
				return nil, fmt.Errorf("%w: موجودی محصول %s برای پرداخت کافی نیست", repository.ErrInsufficientInventory, productID)
			}
			return nil, err
		}
		reservedProducts = append(reservedProducts, productID)
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
