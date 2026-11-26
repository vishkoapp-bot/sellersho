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

func supplierPermissionForPath(method, path string) string {
	path = strings.TrimSuffix(path, "/")
	// Product identifiers are path data. Their text cannot select a role.
	const products = "/api/v1/supplier/products"
	if path == products || strings.HasPrefix(path, products+"/") {
		_, action, _ := strings.Cut(strings.TrimPrefix(path, products+"/"), "/")
		if action == "stock" {
			return "inventory.manage"
		}
		return "products.manage"
	}
	switch {
	case path == "/api/v1/supplier/notifications", path == "/api/v1/supplier/notifications/read":
		return "profile.view"
	case strings.Contains(path, "/team"):
		return "supplier.manage"
	case strings.Contains(path, "/settlement"), strings.Contains(path, "/sales-report"):
		return "finance.manage"
	case strings.Contains(path, "/campaigns"):
		return "marketing.manage"
	case strings.HasPrefix(path, "/api/v1/supplier/cartons/"):
		return "inventory.manage"
	case strings.Contains(path, "/orders"), strings.Contains(path, "/shipments"), strings.Contains(path, "/warehouse-orders"), strings.Contains(path, "/customer-returns"), strings.Contains(path, "/return-shipping-charges"), strings.Contains(path, "/shipping-methods"), strings.Contains(path, "/shipping-sla"), strings.HasSuffix(path, "/tapin/boxes"):
		return "orders.manage"
	case strings.Contains(path, "/inbounds"), strings.Contains(path, "/discrepancies"), strings.Contains(path, "/inventory"), strings.Contains(path, "/returns"), strings.Contains(path, "/warehouses"), strings.HasSuffix(path, "/stock-share"), strings.HasSuffix(path, "/stock"):
		return "inventory.manage"
	case strings.Contains(path, "/upload/"), strings.Contains(path, "/addons"), strings.Contains(path, "/catalog/review"), strings.Contains(path, "/price-increases"), strings.Contains(path, "/warranties"), strings.Contains(path, "/exclusivity-challenges"):
		return "products.manage"
	case method == http.MethodGet && path == "/api/v1/supplier/profile":
		return "profile.view"
	case method == http.MethodGet && path == "/api/v1/supplier/tapin/settings":
		// Dispatchers need connection metadata, while updating credentials
		// still requires management permission.
		return "orders.manage"
	default:
		return "supplier.manage"
	}
}

func supplierProductReadPermissions(method, path string) []string {
	if method != http.MethodGet {
		return nil
	}
	const products = "/api/v1/supplier/products"
	path = strings.TrimSuffix(path, "/")
	if path == products {
		return []string{"inventory.manage", "marketing.manage"}
	}
	if path == products+"/variant-workspace" {
		return []string{"inventory.manage"}
	}
	if sub, ok := strings.CutPrefix(path, products+"/"); ok {
		productID, action, _ := strings.Cut(sub, "/")
		if productID != "" && action == "variants" {
			return []string{"inventory.manage"}
		}
	}
	return nil
}

// GetSupplierAccess checks account and membership activity and supplier
// suspension on every request before applying the route's permission.
func RequireSupplierAccess(jwtService *auth.JWTService, repo *repository.SupplierRepository) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return RequireRoles(jwtService, "supplier", "admin")(func(w http.ResponseWriter, r *http.Request) {
			claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
			if claims.HasDirectCapability("admin") {
				next(w, r)
				return
			}
			permission := supplierPermissionForPath(r.Method, r.URL.Path)
			supplierID, ok := supplierAccess(r, repo, permission)
			if !ok && r.URL.Path == "/api/v1/supplier/shipments" {
				// The exact collection is the carton workflow opened from inventory.
				supplierID, ok = supplierAccess(r, repo, "inventory.manage")
			}
			if !ok && permission == "products.manage" {
				for _, readPermission := range supplierProductReadPermissions(r.Method, r.URL.Path) {
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
				if sub, ok := strings.CutPrefix(r.URL.Path, "/api/v1/supplier/products/"); ok {
					if _, action := supplierUnitLabelRoute(sub); action != "" {
						writeSPNCXError(w, repository.ErrPrintProductNotFound)
						return
					}
				}
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
	if claims.HasDirectCapability("admin") {
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
