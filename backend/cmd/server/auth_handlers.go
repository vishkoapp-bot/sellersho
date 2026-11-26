package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/drup/platform/internal/accesspolicy"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/dependencybudget"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/phone"
	"github.com/drup/platform/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const userClaimsKey contextKey = "userClaims"

// AuthMiddleware requires a valid JWT Bearer token and stores its claims in the request context.
func AuthMiddleware(jwtService *auth.JWTService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := jwtService.ValidateToken(tokenStr)
		if err != nil {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
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
		claims, err := jwtService.ValidateToken(strings.TrimPrefix(authHeader, "Bearer "))
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	name := "کاربر جدید"
	if existingUser != nil && !existingUser.IsActive {
		// GAP 19b (OTP path): the code has verified by this point, so
		// account ownership is proven and a supplier owner may be told
		// WHY the account cannot log in -- the exact same state bodies
		// the password path returns (see handlePasswordLogin). Any
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
		user, err = userRepo.UpsertUser(ctx, mobile, name, role)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if accesspolicy.IsSuperAdminMobile(mobile) {
		if err := capabilityRepo.GrantSuperAdminAfterOTP(ctx, user.ID, mobile); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	capabilities, err := capabilityRepo.ListActive(ctx, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

// GAP 19b: supplier account states on the password-login path. The
// codes are part of the login contract; the frontend renders a
// dedicated pending/rejected screen from them.
const (
	supplierPendingApprovalCode = "supplier_pending_approval"
	supplierRejectedCode        = "supplier_rejected"
)

func handlePasswordLogin(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	userRepo *repository.UserRepository,
	jwtService *auth.JWTService,
	supplierRepo *repository.SupplierRepository,
) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Username) == "" || req.Password == "" {
		http.Error(w, `{"error":"Invalid login payload"}`, http.StatusBadRequest)
		return
	}

	user, passwordHash, _, lockedUntil, err := userRepo.FindByUsername(ctx, strings.TrimSpace(req.Username))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user == nil || passwordHash == "" {
		http.Error(w, `{"error":"Invalid username or password"}`, http.StatusUnauthorized)
		return
	}
	if lockedUntil != nil && lockedUntil.After(time.Now()) {
		http.Error(w, `{"error":"Account temporarily locked"}`, http.StatusTooManyRequests)
		return
	}
	// The password is verified BEFORE any account state is examined:
	// a wrong password always gets the generic failure, so nothing
	// about pending/rejected/blocked leaks to anyone who has not
	// proven ownership of the account.
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		_ = userRepo.RecordLoginFailure(ctx, user.ID, 30*time.Second)
		http.Error(w, `{"error":"Invalid username or password"}`, http.StatusUnauthorized)
		return
	}
	if err := userRepo.ResetLoginFailures(ctx, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !user.IsActive {
		// Ownership is proven at this point, so a supplier owner may
		// be told WHY the account cannot log in: still waiting for
		// admin approval, rejected (with the recorded reason), or
		// approved but blocked. Any other inactive account keeps the
		// pre-existing generic failure, byte for byte.
		if supplierRepo != nil && user.Role == "supplier" {
			if sup, serr := supplierRepo.GetSupplierByUserID(ctx, user.ID); serr == nil && sup != nil {
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
				case "approved":
					// Approved but blocked: the platform's existing
					// disabled-account response.
					http.Error(w, `{"error":"Account is disabled"}`, http.StatusForbidden)
					return
				}
			}
		}
		http.Error(w, `{"error":"Invalid username or password"}`, http.StatusUnauthorized)
		return
	}

	claims := &auth.UserClaims{UserID: user.ID}
	blocked, err := jwtService.RefreshAuthState(ctx, claims)
	if err != nil {
		http.Error(w, `{"error":"Authorization state unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if blocked {
		http.Error(w, `{"error":"Account is disabled"}`, http.StatusForbidden)
		return
	}
	writeAuthenticatedSession(w, jwtService, user, sessionRole(user.Role, claims.Capabilities), claims.Capabilities)
}
