package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/drup/platform/internal/accesspolicy"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/dependencybudget"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/phone"
	"github.com/drup/platform/internal/repository"
)

type contextKey string

const userClaimsKey contextKey = "userClaims"
const legacyProfileName = "کاربر جدید"

type accountNameStore interface {
	GetProfileName(context.Context, string) (string, error)
	SetProfileName(context.Context, string, string) (string, error)
}

func handleAccountProfile(users accountNameStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		claims, _ := r.Context().Value(userClaimsKey).(*auth.UserClaims)
		if claims == nil {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var name string
		var err error
		switch r.Method {
		case http.MethodGet:
			name, err = users.GetProfileName(r.Context(), claims.UserID)
		case http.MethodPatch:
			var body struct {
				FullName string `json:"fullName"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
				http.Error(w, `{"error":"اطلاعات نام معتبر نیست."}`, http.StatusBadRequest)
				return
			}
			name = strings.TrimSpace(body.FullName)
			length := utf8.RuneCountInString(name)
			if length < 2 || length > 100 || name == legacyProfileName || strings.ContainsFunc(name, unicode.IsControl) {
				http.Error(w, `{"error":"نام و نام خانوادگی را بین ۲ تا ۱۰۰ نویسه وارد کنید."}`, http.StatusBadRequest)
				return
			}
			name, err = users.SetProfileName(r.Context(), claims.UserID, name)
		default:
			w.Header().Set("Allow", "GET, PATCH")
			writeMethodNotAllowed(w)
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"Account is disabled"}`, http.StatusForbidden)
			return
		}
		if err != nil {
			log.Printf("load or update account name: %v", err)
			http.Error(w, `{"error":"دریافت یا ذخیرهٔ نام حساب ممکن نشد."}`, http.StatusServiceUnavailable)
			return
		}
		updated := *claims
		updated.FullName = strings.TrimSpace(name)
		if updated.FullName == legacyProfileName {
			updated.FullName = ""
		}
		if updated.Capabilities != nil {
			updated.Role = sessionRole(updated.Role, updated.Capabilities)
		}
		json.NewEncoder(w).Encode(updated)
	}
}

// AuthMiddleware requires a valid JWT Bearer token and stores its claims in the request context.
func AuthMiddleware(jwtService *auth.JWTService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := jwtService.ValidateSession(r.Context(), tokenStr)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		// PERF-03 G1: one round trip refreshes capabilities AND the
		// blocked check (was RefreshCapabilities + IsUserBlocked, two
		// sequential seq-scanning queries). Behaviour is identical:
		// D57 — a blocked or inactive user must not keep using an old
		// token after admin block/unblock (handleSetUserActiveStatus
		// toggles users.is_active). The error string reuses the
		// existing login-time disabled-account message; no new status
		// value or error string is invented (U53).
		blocked, err := jwtService.RefreshAuthState(r.Context(), claims)
		if err != nil {
			http.Error(w, `{"error":"Authorization state unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		if blocked {
			http.Error(w, `{"error":"Account is disabled"}`, http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), userClaimsKey, claims)
		next(w, r.WithContext(ctx))
	}
}

// OptionalAuthMiddleware is the public-route counterpart of
// AuthMiddleware: it never rejects a request. When the caller
// presents a valid Bearer token it places the refreshed claims in
// the request context exactly like AuthMiddleware; absent, invalid
// or blocked credentials simply leave the request anonymous. It
// exists for surfaces that are public by design but key part of
// their payload to the caller's identity -- the product list/detail
// gate on supplier cost fields (see canSeeSupplierFields). Every
// failure mode degrades to anonymous, which can only hide gated
// fields, never expose them.
func OptionalAuthMiddleware(jwtService *auth.JWTService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			next(w, r)
			return
		}
		claims, err := jwtService.ValidateSession(r.Context(), strings.TrimPrefix(authHeader, "Bearer "))
		if err != nil {
			next(w, r)
			return
		}
		// PERF-03 G1: single round trip (see AuthMiddleware above).
		blocked, err := jwtService.RefreshAuthState(r.Context(), claims)
		if err != nil || blocked {
			next(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), userClaimsKey, claims)
		next(w, r.WithContext(ctx))
	}
}

func writeSessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrSessionStoreUnavailable) {
		t01WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Authorization state unavailable"})
		return
	}
	t01WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
}

func handleSessionLogout(jwtService *auth.JWTService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !t01MethodOnly(w, r, http.MethodPost) {
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeSessionError(w, nil)
			return
		}
		if err := jwtService.RevokeSession(r.Context(), token); err != nil {
			writeSessionError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// RequireCapabilities wraps an endpoint with JWT authentication and an
// explicit active-capability allowlist. Legacy role-only tokens (no
// capabilities claim at all) remain valid through HasCapability's
// nil-capabilities fallback in internal/auth. A token whose capabilities
// were REFRESHED to an empty list (every capability revoked) must NOT
// fall back to its token role: that was the previous len==0 fallback here,
// and it let a fully-revoked user keep role access until token expiry.
func RequireCapabilities(jwtService *auth.JWTService, capabilities ...string) func(http.HandlerFunc) http.HandlerFunc {
	allowed := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		allowed[capability] = struct{}{}
	}
	return func(next http.HandlerFunc) http.HandlerFunc {
		return AuthMiddleware(jwtService, func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims)
			if !ok || claims == nil {
				http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
				return
			}
			matched := false
			for capability := range allowed {
				if claims.HasCapability(capability) {
					matched = true
					break
				}
			}
			// No len==0 role fallback: see the function comment. HasCapability
			// already covers the legacy nil-capabilities token case.
			if !matched {
				http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
				return
			}
			next(w, r)
		})
	}
}

// RequireRoles preserves the existing route API while evaluating the same
// values as explicit capabilities.
func RequireRoles(jwtService *auth.JWTService, roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	allowed := make([]string, 0, len(roles)+1)
	for _, role := range roles {
		allowed = append(allowed, role)
		if role == "admin" {
			allowed = append(allowed, accesspolicy.SuperAdminCapability)
		}
		if role == "finance" {
			allowed = append(allowed, "finance_specialist")
		} else if role == "finance_specialist" {
			allowed = append(allowed, "finance")
		}
	}
	return RequireCapabilities(jwtService, allowed...)
}

func handleSendOTP(w http.ResponseWriter, r *http.Request, ctx context.Context, otpService *auth.OTPService) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Mobile string `json:"mobile"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !phone.IsValid(req.Mobile) {
		http.Error(w, `{"error":"Invalid mobile number"}`, http.StatusBadRequest)
		return
	}

	mobile := phone.Normalize(req.Mobile)
	code, err := otpService.GenerateAndStoreOTP(ctx, mobile)
	if err != nil {
		if errors.Is(err, dependencybudget.ErrOverloaded) || errors.Is(err, auth.ErrOTPStoreUnavailable) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "سرویس ورود موقتاً در دسترس نیست؛ کمی بعد تلاش کنید.", http.StatusServiceUnavailable)
			return
		}
		if errors.Is(err, auth.ErrOTPDeliveryUnknown) {
			http.Error(w, "نتیجه ارسال پیامک هنوز مشخص نیست؛ اگر کد را دریافت کرده‌اید از همان استفاده کنید و برای ارسال مجدد کمی صبر کنید.", http.StatusBadGateway)
			return
		}
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "cooldown") || strings.Contains(err.Error(), "invalid") {
			status = http.StatusTooManyRequests
		} else if strings.Contains(err.Error(), "SMS delivery failed") {
			status = http.StatusBadGateway
		}
		http.Error(w, err.Error(), status)
		return
	}

	response := map[string]interface{}{
		"success":   true,
		"mobile":    mobile,
		"expiresIn": 120,
		"message":   "کد یکبارمصرف ارسال شد",
	}
	_ = code
	json.NewEncoder(w).Encode(response)
}

func handleVerifyOTP(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	otpService *auth.OTPService,
	userRepo *repository.UserRepository,
	jwtService *auth.JWTService,
	supplierRepo *repository.SupplierRepository,
	capabilityRepo *repository.CapabilityRepository,
	storeRepo *repository.StoreRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Mobile    string `json:"mobile"`
		Code      string `json:"code"`
		Role      string `json:"role"`
		StoreSlug string `json:"storeSlug"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	mobile := phone.Normalize(req.Mobile)
	code := strings.TrimSpace(req.Code)

	if !otpService.VerifyOTP(ctx, mobile, code) {
		http.Error(w, `{"error":"کد یکبارمصرف نادرست یا منقضی شده است"}`, http.StatusUnauthorized)
		return
	}

	existingUser, err := userRepo.FindByMobile(ctx, mobile)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if supplierRepo != nil {
		supplierUser, supplierErr := supplierRepo.FindUserByMobile(ctx, mobile)
		if supplierErr != nil {
			http.Error(w, supplierErr.Error(), http.StatusInternalServerError)
			return
		}
		if supplierUser != nil {
			existingUser = supplierUser
		}
	}

	role := "customer"
	if existingUser != nil {
		role = existingUser.Role
	}
	if existingUser == nil && req.Role != "" {
		http.Error(w, `{"error":"Requested role must already be provisioned"}`, http.StatusForbidden)
		return
	}

	if existingUser != nil && !existingUser.IsActive {
		// GAP 19b (OTP path): the code has verified by this point, so
		// account ownership is proven and a supplier owner may be told
		// why the account cannot log in. Any
		// other inactive account (approved-but-blocked supplier, plain
		// customer, lookup failure) keeps the generic disabled failure
		// below, byte for byte.
		if supplierRepo != nil && existingUser.Role == "supplier" {
			if sup, serr := supplierRepo.GetSupplierByUserID(ctx, existingUser.ID); serr == nil && sup != nil {
				switch sup.ApprovalStatus {
				case "pending_approval":
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"error":         "حساب تأمین‌کننده شما در انتظار تأیید مدیریت است",
						"code":          supplierPendingApprovalCode,
						"supplierName":  sup.Name,
						"warehouseCity": sup.WarehouseCity,
					})
					return
				case "rejected":
					body := map[string]interface{}{
						"error":         "درخواست تأمین‌کننده شما رد شده است",
						"code":          supplierRejectedCode,
						"supplierName":  sup.Name,
						"warehouseCity": sup.WarehouseCity,
					}
					if sup.RejectionReason != nil && *sup.RejectionReason != "" {
						body["rejectionReason"] = *sup.RejectionReason
					}
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(body)
					return
				}
			}
		}
		http.Error(w, `{"error":"Account is disabled"}`, http.StatusForbidden)
		return
	}

	var user *domain.User
	if existingUser != nil {
		user = existingUser
	} else {
		user, err = userRepo.UpsertUser(ctx, mobile, "", role)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}

	if capabilityRepo == nil {
		http.Error(w, `{"error":"Capability authorization is unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	// No requested role is the explicit customer-access flow. A requested role
	// only selects an existing active capability; it never grants one.
	if req.Role == "" {
		if err := capabilityRepo.GrantCustomerAfterOTP(ctx, user.ID); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	if accesspolicy.IsSuperAdminMobile(mobile) {
		if err := capabilityRepo.GrantSuperAdminAfterOTP(ctx, user.ID, mobile); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	capabilities, err := capabilityRepo.ListActive(ctx, user.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	selectedRole := sessionRole(user.Role, capabilities)
	if req.Role == "admin" && slices.Contains(capabilities, accesspolicy.SuperAdminCapability) {
		selectedRole = accesspolicy.SuperAdminCapability
	} else if req.Role != "" {
		if !slices.Contains(capabilities, req.Role) {
			http.Error(w, `{"error":"Requested role is not an active capability"}`, http.StatusForbidden)
			return
		}
		selectedRole = req.Role
	}

	// Fix83: record buyer signup attribution (buyer -> store) idempotently.
	// Only customer accounts are attributed; an unknown slug is ignored
	// (no default store is ever invented). A recording failure never
	// breaks login: the next verified login with a store slug retries
	// the same idempotent insert.
	if user.Role == "customer" && storeRepo != nil {
		if slug := strings.TrimSpace(req.StoreSlug); slug != "" {
			if store, serr := storeRepo.GetStoreBySlug(ctx, slug); serr == nil && store != nil {
				if lerr := storeRepo.RecordCustomerStoreLink(ctx, user.ID, store.ID); lerr != nil {
					log.Printf("Fix83: failed to record customer_store_link user=%s store=%s: %v", user.ID, store.ID, lerr)
				}
			}
		}
	}
	writeAuthenticatedSession(w, jwtService, user, selectedRole, capabilities)
}

// Preserve the selected role while it remains active. Older clients can use
// the role for display, and the full membership list authorizes access.
func sessionRole(preferred string, capabilities []string) string {
	if slices.Contains(capabilities, preferred) {
		return preferred
	}
	if slices.Contains(capabilities, "customer") {
		return "customer"
	}
	if len(capabilities) > 0 {
		return capabilities[0]
	}
	return ""
}

func writeAuthenticatedSession(w http.ResponseWriter, jwtService *auth.JWTService, user *domain.User, role string, capabilities []string) {
	capabilities = append([]string{}, capabilities...)
	token, err := jwtService.GenerateTokenWithCapabilities(user.ID, user.Mobile, user.FullName, role, capabilities, 7*24*time.Hour)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"token":   token,
		"user": map[string]interface{}{
			"id":           user.ID,
			"mobile":       user.Mobile,
			"fullName":     user.FullName,
			"role":         role,
			"capabilities": capabilities,
		},
	})
}

// Supplier account states after OTP verification. The
// codes are part of the login contract; the frontend renders a
// dedicated pending/rejected screen from them.
const (
	supplierPendingApprovalCode = "supplier_pending_approval"
	supplierRejectedCode        = "supplier_rejected"
)
