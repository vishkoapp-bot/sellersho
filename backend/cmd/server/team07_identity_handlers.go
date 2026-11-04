package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/accesspolicy"
	"github.com/drup/platform/internal/repository"
)

// Team 07 (Phase 2, live parity): identity/admin gaps.
//
// D58: super_admin-guarded admin capability grant/revoke. The exact live
// endpoint is unverified (MASTER-AUDIT U55), so this is a minimal,
// explicitly documented mechanism, not a claim about the live route:
// POST /api/v1/admin/capabilities/grant and .../revoke, guarded by
// RequireCapabilities("super_admin") in main.go. Grants are recorded
// with source_type='admin_grant' via CapabilityRepository.Grant, which
// is also why the live DB holds 2 such rows; revokes use Revoke.
// super_admin itself is refused on both paths: Grant already refuses
// it ("super admin requires OTP verification") and revoking it here
// could lock out the only super admins, so both return Forbidden.
// D59 is DEFERRED: no staff users are created or seeded here (U56).
// D15 staff: GET /api/v1/admin/staff replaces the Team 01 501 stub
// (U54: live handler name/shape unverified). It lists users in staff
// roles using the same AdminUser shape as GET /admin/users; the exact
// live staff shape may differ and must be confirmed by an authenticated
// probe against the merged tree before frontend work relies on it.

var t07StaffRoles = []string{
	"admin",
	"finance",
	"finance_specialist",
	"support_specialist",
	"logistics_specialist",
	"warehouse_staff",
}

func handleT07AdminStaff(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	role := strings.TrimSpace(r.URL.Query().Get("role"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	query := `SELECT id, mobile, full_name, role, COALESCE(username, ''), is_active, created_at
		FROM users
		WHERE role IN ('admin', 'finance', 'finance_specialist', 'support_specialist', 'logistics_specialist', 'warehouse_staff')`
	args := []interface{}{}
	if role != "" {
		// Still scoped to staff roles: a non-staff role filter yields an
		// empty list rather than leaking non-staff users through /staff.
		args = append(args, role)
		query += fmt.Sprintf(" AND role = $%d", len(args))
	}
	if status == "active" {
		query += " AND is_active = TRUE"
	} else if status == "blocked" || status == "inactive" {
		query += " AND is_active = FALSE"
	}
	args = append(args, t01Limit(r))
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	staff := []repository.AdminUser{}
	for rows.Next() {
		var u repository.AdminUser
		if err := rows.Scan(&u.ID, &u.Mobile, &u.FullName, &u.Role, &u.Username, &u.IsActive, &u.CreatedAt); err != nil {
			writeInternalError(w, err)
			return
		}
		staff = append(staff, u)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "staff": staff})
}

func t07DecodeCapabilityRequest(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	var req struct {
		UserID     string `json:"userId"`
		Capability string `json:"capability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return "", "", false
	}
	userID := strings.TrimSpace(req.UserID)
	capability := strings.TrimSpace(req.Capability)
	if userID == "" || capability == "" {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return "", "", false
	}
	return userID, capability, true
}

// t07CapabilityTargetExists reports whether a users row exists for userID.
// Grant/Revoke themselves do not check this, so the endpoint does: a
// grant to a nonexistent user would otherwise surface only as a
// database error.
func t07CapabilityTargetExists(ctx context.Context, db *sql.DB, userID string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id::text = $1)`, userID).Scan(&exists)
	return exists, err
}

func handleT07AdminCapabilityGrant(w http.ResponseWriter, r *http.Request, db *sql.DB, capabilityRepo *repository.CapabilityRepository) {
	if !t01MethodOnly(w, r, http.MethodPost) {
		return
	}
	userID, capability, ok := t07DecodeCapabilityRequest(w, r)
	if !ok {
		return
	}
	if capability == accesspolicy.SuperAdminCapability {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	if db == nil || capabilityRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	exists, err := t07CapabilityTargetExists(r.Context(), db, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !exists {
		http.Error(w, `{"error":"User not found"}`, http.StatusNotFound)
		return
	}
	grantedBy := t01ClaimsUserID(r)
	var grantedByPtr *string
	if grantedBy != "" {
		grantedByPtr = &grantedBy
	}
	if err := capabilityRepo.Grant(r.Context(), userID, capability, "admin_grant", "", grantedByPtr); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "userId": userID, "capability": capability,
		"status": "active", "sourceType": "admin_grant",
	})
}

func handleT07AdminCapabilityRevoke(w http.ResponseWriter, r *http.Request, db *sql.DB, capabilityRepo *repository.CapabilityRepository) {
	if !t01MethodOnly(w, r, http.MethodPost) {
		return
	}
	userID, capability, ok := t07DecodeCapabilityRequest(w, r)
	if !ok {
		return
	}
	if capability == accesspolicy.SuperAdminCapability {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	if db == nil || capabilityRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	exists, err := t07CapabilityTargetExists(r.Context(), db, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !exists {
		http.Error(w, `{"error":"User not found"}`, http.StatusNotFound)
		return
	}
	if err := capabilityRepo.Revoke(r.Context(), userID, capability); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "userId": userID, "capability": capability,
		"status": "revoked",
	})
}
