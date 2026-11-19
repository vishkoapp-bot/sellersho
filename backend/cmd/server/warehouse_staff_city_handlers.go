package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// Some legacy workflows currently carry no authoritative site. Preserve
// existing non-staff paths while keeping those workflows outside staff scope.
func requireUnscopedWarehouseWork(w http.ResponseWriter, r *http.Request) bool {
	if repository.WarehouseStaffID(r.Context()) == "" {
		return true
	}
	http.Error(w, "Warehouse site cannot be established for this action", http.StatusForbidden)
	return false
}

// warehouseCityContext derives staff scope only from refreshed claims. An
// active warehouse_staff capability always carries city scope on these paths,
// including when the same principal holds another capability.
func requireWarehouseCityRoles(jwt *auth.JWTService, roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return RequireRoles(jwt, roles...)(warehouseCityContext(next))
	}
}

func requireGlobalWarehouseRoles(jwt *auth.JWTService, roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return requireWarehouseCityRoles(jwt, roles...)(func(w http.ResponseWriter, r *http.Request) {
			if requireUnscopedWarehouseWork(w, r) {
				next(w, r)
			}
		})
	}
}

func warehouseCityContext(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(userClaimsKey).(*auth.UserClaims)
		if claims.HasCapability("warehouse_staff") {
			if claims.UserID == "" {
				http.Error(w, "Warehouse identity required", http.StatusForbidden)
				return
			}
			r = r.WithContext(repository.WithWarehouseStaffID(r.Context(), claims.UserID))
		}
		next(w, r)
	}
}

func handleWarehouseStaffCity(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPut) || t01DBNil(w, db) {
		return
	}
	const prefix = "/api/v1/admin/staff/"
	suffix := strings.TrimPrefix(r.URL.Path, prefix)
	rawID, ending, ok := strings.Cut(suffix, "/")
	id, err := uuid.Parse(rawID)
	if !ok || ending != "warehouse-city" || err != nil {
		http.NotFound(w, r)
		return
	}
	var request struct {
		City *string `json:"city"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.City == nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		http.Error(w, "An explicit city is required (empty to revoke)", http.StatusBadRequest)
		return
	}
	city := strings.TrimSpace(*request.City)
	if utf8.RuneCountInString(city) > 50 {
		http.Error(w, "Invalid city", http.StatusBadRequest)
		return
	}
	// An empty value revokes confirmation. Otherwise only an existing city
	// from the central warehouse catalog is eligible for staff assignment.
	actor := t01ClaimsUserID(r)
	if actor == "" {
		http.Error(w, "Authenticated supervisor required", http.StatusForbidden)
		return
	}
	result, err := db.ExecContext(r.Context(), `WITH assignment AS (
		UPDATE users AS u
		SET warehouse_city=$2, warehouse_city_confirmed=($2 <> ''), updated_at=NOW()
		WHERE u.id=$1 AND u.is_active AND EXISTS (SELECT 1 FROM user_capabilities c
			WHERE c.user_id=u.id AND c.capability='warehouse_staff' AND c.status='active')
		AND ($2='' OR EXISTS (SELECT 1 FROM warehouses w WHERE BTRIM(w.city_name) COLLATE "C"=$2))
		RETURNING u.id
	)
	INSERT INTO audit_logs (id, operator, action, category, details)
	SELECT $3,$4,'set_staff_warehouse_city','warehouse',
	       jsonb_build_object('userId',id,'city',$2::text,'confirmed',$2<>'')::text
	FROM assignment`, id.String(), city, uuid.NewString(), actor)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if n != 1 {
		http.Error(w, "Warehouse staff or city not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
