package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

const supplierIDKey contextKey = "supplierID"

func supplierPermissionForPath(path string) string {
	switch {
	case strings.Contains(path, "/team"):
		return "supplier.manage"
	case strings.Contains(path, "/settlement"), strings.Contains(path, "/sales-report"):
		return "finance.manage"
	case strings.Contains(path, "/campaigns"):
		return "marketing.manage"
	case strings.Contains(path, "/orders"), strings.Contains(path, "/shipments"), strings.Contains(path, "/warehouse-orders"), strings.Contains(path, "/customer-returns"), strings.Contains(path, "/return-shipping-charges"), strings.Contains(path, "/shipping-methods"), strings.Contains(path, "/shipping-sla"), strings.HasSuffix(path, "/tapin/boxes"):
		return "orders.manage"
	case strings.Contains(path, "/inbounds"), strings.Contains(path, "/discrepancies"), strings.Contains(path, "/inventory"), strings.Contains(path, "/returns"), strings.Contains(path, "/warehouses"), strings.HasSuffix(path, "/stock-share"), strings.HasSuffix(path, "/stock"):
		return "inventory.manage"
	case strings.Contains(path, "/products"), strings.Contains(path, "/upload/"), strings.Contains(path, "/addons"), strings.Contains(path, "/catalog/review"), strings.Contains(path, "/price-increases"), strings.Contains(path, "/warranties"), strings.Contains(path, "/exclusivity-challenges"):
		return "products.manage"
	case strings.HasSuffix(path, "/profile"):
		return "profile.view"
	default:
		return "supplier.manage"
	}
}

// D54 pending_approval policy (Team 06, Phase 2) — ASSUMPTION, TODO (U06/D54):
// No explicit approval_status check is added here, deliberately. In
// practice a pending supplier is already gated: registration creates
// the user with is_active = FALSE, and GetSupplierAccess (owner path)
// requires users.is_active = TRUE, while ApproveSupplier flips both
// approval_status and is_active together. So a pending supplier's
// registration JWT does NOT open supplier routes today — it fails the
// access check — but that gate is is_active, not approval_status, and
// the exact live pending_approval policy is still a user decision.
// If live policy differs (e.g. pending suppliers may view their own
// profile), add an explicit approval_status check here instead of
// relying on is_active. Do NOT silently grant pending suppliers full
// panel access: any change here must be a conscious policy decision.
func RequireSupplierAccess(jwtService *auth.JWTService, repo *repository.SupplierRepository) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return RequireRoles(jwtService, "supplier", "admin")(func(w http.ResponseWriter, r *http.Request) {
			claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
			if claims.HasDirectCapability("admin") {
				next(w, r)
				return
			}
			permission := supplierPermissionForPath(r.URL.Path)
			// Dispatchers need connection metadata, never permission to replace
			// the business's write-only carrier credential.
			if r.Method == http.MethodGet && strings.TrimSuffix(r.URL.Path, "/") == "/api/v1/supplier/tapin/settings" {
				permission = "orders.manage"
			}
			supplierID, ok := supplierAccess(r, repo, permission)
			if !ok && r.Method == http.MethodGet && strings.TrimSuffix(r.URL.Path, "/") == "/api/v1/supplier/products" {
				for _, readPermission := range []string{"inventory.manage", "marketing.manage"} {
					if supplierID, ok = supplierAccess(r, repo, readPermission); ok {
						break
					}
				}
			}
			if !ok {
				http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
				return
			}
			if !repo.OwnsSupplierResource(r.Context(), supplierID, r.URL.Path) {
				http.Error(w, `{"error":"Resource does not belong to your supplier"}`, http.StatusForbidden)
				return
			}
			next(w, r.WithContext(context.WithValue(r.Context(), supplierIDKey, supplierID)))
		})
	}
}

func supplierAccess(r *http.Request, repo *repository.SupplierRepository, permission string) (string, bool) {
	claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		return "", false
	}
	if repo == nil {
		return "", false
	}
	if claims.HasCapability("admin") {
		return r.URL.Query().Get("supplierId"), true
	}
	supplierID, permissions, active, err := repo.GetSupplierAccess(r.Context(), claims.UserID)
	if err != nil || !active {
		return "", false
	}
	if permission == "profile.view" {
		return supplierID, true
	}
	for _, item := range permissions {
		if item == "supplier.manage" || item == permission {
			return supplierID, true
		}
	}
	return "", false
}

func handleSupplierMembersList(w http.ResponseWriter, r *http.Request, repo *repository.SupplierRepository) {
	supplierID, ok := supplierAccess(r, repo, "supplier.manage")
	if !ok {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	members, err := repo.ListMembers(r.Context(), supplierID)
	if err != nil {
		http.Error(w, `{"error":"Failed to load team"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(members)
}

func handleSupplierMemberCreate(w http.ResponseWriter, r *http.Request, repo *repository.SupplierRepository) {
	supplierID, ok := supplierAccess(r, repo, "supplier.manage")
	if !ok {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	var req struct {
		FullName string `json:"fullName"`
		Username string `json:"username"`
		Mobile   string `json:"mobile"`
		Password string `json:"password"`
		RoleKey  string `json:"roleKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.FullName) == "" || len(req.FullName) > 100 || !regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,50}$`).MatchString(req.Username) || len(req.Password) < 8 || len(req.Password) > 72 || !regexp.MustCompile(`^09[0-9]{9}$`).MatchString(req.Mobile) {
		http.Error(w, `{"error":"نام، نام کاربری، موبایل و رمز حداقل ۸ رقمی الزامی است"}`, http.StatusBadRequest)
		return
	}
	if _, valid := repository.SupplierRolePermissions[req.RoleKey]; !valid {
		http.Error(w, `{"error":"نقش انتخاب‌شده معتبر نیست"}`, http.StatusBadRequest)
		return
	}
	member, err := repo.CreateMember(r.Context(), supplierID, claims.UserID, strings.TrimSpace(req.FullName), strings.TrimSpace(req.Username), strings.TrimSpace(req.Mobile), req.Password, req.RoleKey)
	if err != nil {
		http.Error(w, `{"error":"Could not create supplier team member"}`, http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(member)
}

func handleSupplierMemberStatus(w http.ResponseWriter, r *http.Request, repo *repository.SupplierRepository) {
	supplierID, ok := supplierAccess(r, repo, "supplier.manage")
	if !ok {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	var req struct {
		Active  *bool   `json:"active"`
		RoleKey *string `json:"roleKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}
	memberID := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/team/")
	var err error
	if req.RoleKey != nil && req.Active == nil {
		if _, valid := repository.SupplierRolePermissions[*req.RoleKey]; !valid {
			http.Error(w, `{"error":"Invalid role"}`, http.StatusBadRequest)
			return
		}
		err = repo.UpdateMemberRole(r.Context(), supplierID, memberID, *req.RoleKey)
	} else if req.Active != nil && req.RoleKey == nil {
		err = repo.SetMemberActive(r.Context(), supplierID, memberID, *req.Active)
	} else {
		http.Error(w, `{"error":"Specify either active or roleKey"}`, http.StatusBadRequest)
		return
	}
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Member not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"Could not update team member"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
