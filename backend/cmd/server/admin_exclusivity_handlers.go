package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
)

// Fix69 (W-ROUTES) R2: admin exclusivity-challenge review.
//
// Live evidence (live bundles, /opt/drup/dist/assets, read-only):
//   index-MERGED.js: getExclusivityChallenges: GET
//     /admin/exclusivity-challenges[?status=] -> {challenges: [...]}
//   index-MERGED.js: reviewExclusivityChallenge: POST
//     /admin/exclusivity-challenges/<id>/review
//     body {decision, reason}
//   AdminDashboardView-MERGED.js call site: decision is "approved" or
//     "rejected"; a reason is required client-side for "rejected".
//   AdminDashboardView-MERGED.js render: challenge items carry
//     id, supplierId, supplierName, branch, proposedPrice,
//     productTitle, productBrand, productCategory, barcode,
//     holderKind ("product"|"proposal"), holderTitle,
//     holderSupplierId, holderSupplierName, holderProductId,
//     variantName, variantNames[], variantAttributes,
//     challengerPayload {supplierBaseCost, stock}, status,
//     rejectionReason, reviewedBy/At, productConsumedAt,
//     variantConsumedAt, createdAt.
// The list below returns exactly those fields from the
// exclusivity_challenges table (migration 202610220004); the two
// supplier names come from LEFT JOINs on suppliers. The review
// response body {success, id, status} is a candidate shape: the live
// client only needs a 2xx (it toasts and reloads), so the live
// response shape itself is unproven and is not claimed here.
// Only rows in status "pending" can be reviewed; anything else is
// 409, a missing id is 404.

// wr69Operator returns the acting user's display name for audit
// columns, mirroring the finance handlers' fallback.
func wr69Operator(r *http.Request) string {
	if claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims); ok && claims != nil && claims.FullName != "" {
		return claims.FullName
	}
	return "admin"
}

func wr69AdminExclusivityChallenges(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/exclusivity-challenges")
	sub = strings.Trim(sub, "/")
	if sub == "" {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		wr69AdminChallengesList(w, r, db)
		return
	}
	if strings.HasSuffix(sub, "/review") {
		id := strings.TrimSuffix(sub, "/review")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		wr69AdminChallengeReview(w, r, db, id)
		return
	}
	http.NotFound(w, r)
}

func wr69AdminChallengesList(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	status := r.URL.Query().Get("status")
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		http.Error(w, `{"error":"invalid status"}`, http.StatusBadRequest)
		return
	}
	query := `SELECT c.id, c.supplier_id, COALESCE(s.name, ''), c.branch, c.proposed_price,
		c.product_title, c.product_brand, c.product_category, c.barcode,
		c.product_identity_key, c.holder_kind, c.holder_id, c.holder_title,
		c.holder_supplier_id, COALESCE(hs.name, ''), c.holder_product_id,
		c.variant_name, c.variant_attributes, c.variant_names, c.challenger_payload,
		c.status, c.rejection_reason, c.reviewed_by, c.reviewed_at,
		c.product_consumed_at, c.variant_consumed_at, c.created_at
		FROM exclusivity_challenges c
		LEFT JOIN suppliers s ON s.id = c.supplier_id
		LEFT JOIN suppliers hs ON hs.id = c.holder_supplier_id`
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = db.QueryContext(r.Context(), query+` WHERE c.status = $1 ORDER BY c.created_at DESC LIMIT $2`, status, t01Limit(r))
	} else {
		rows, err = db.QueryContext(r.Context(), query+` ORDER BY c.created_at DESC LIMIT $1`, t01Limit(r))
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, supplierID, supplierName, branch, productTitle, productBrand, productCategory, barcode string
		var identityKey, holderKind, holderID, holderTitle, holderSupplierID, holderSupplierName, holderProductID string
		var variantName, variantAttributes, statusVal string
		var proposedPrice int64
		var variantNamesRaw, challengerPayloadRaw []byte
		var rejection, reviewedBy sql.NullString
		var reviewedAt, productConsumedAt, variantConsumedAt sql.NullTime
		var createdAt time.Time
		if err := rows.Scan(&id, &supplierID, &supplierName, &branch, &proposedPrice,
			&productTitle, &productBrand, &productCategory, &barcode,
			&identityKey, &holderKind, &holderID, &holderTitle,
			&holderSupplierID, &holderSupplierName, &holderProductID,
			&variantName, &variantAttributes, &variantNamesRaw, &challengerPayloadRaw,
			&statusVal, &rejection, &reviewedBy, &reviewedAt,
			&productConsumedAt, &variantConsumedAt, &createdAt); err != nil {
			writeInternalError(w, err)
			return
		}
		variantNames := json.RawMessage("[]")
		if len(variantNamesRaw) > 0 {
			variantNames = json.RawMessage(variantNamesRaw)
		}
		challengerPayload := json.RawMessage("{}")
		if len(challengerPayloadRaw) > 0 {
			challengerPayload = json.RawMessage(challengerPayloadRaw)
		}
		item := map[string]interface{}{
			"id": id, "supplierId": supplierID, "supplierName": supplierName,
			"branch": branch, "proposedPrice": proposedPrice,
			"productTitle": productTitle, "productBrand": productBrand,
			"productCategory": productCategory, "barcode": barcode,
			"productIdentityKey": identityKey, "holderKind": holderKind,
			"holderId": holderID, "holderTitle": holderTitle,
			"holderSupplierId": holderSupplierID, "holderSupplierName": holderSupplierName,
			"holderProductId": holderProductID, "variantName": variantName,
			"variantAttributes": variantAttributes, "variantNames": variantNames,
			"challengerPayload": challengerPayload, "status": statusVal,
			"reviewedAt":        t01NullTime(reviewedAt),
			"productConsumedAt": t01NullTime(productConsumedAt),
			"variantConsumedAt": t01NullTime(variantConsumedAt),
			"createdAt":         createdAt.Format(time.RFC3339),
		}
		if rejection.Valid {
			item["rejectionReason"] = rejection.String
		}
		if reviewedBy.Valid {
			item["reviewedBy"] = reviewedBy.String
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"challenges": out})
}

func wr69AdminChallengeReview(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var req struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	if req.Decision != "approved" && req.Decision != "rejected" {
		http.Error(w, `{"error":"decision must be approved or rejected"}`, http.StatusBadRequest)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if req.Decision == "rejected" && reason == "" {
		http.Error(w, `{"error":"reason is required for rejection"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var current string
	err := db.QueryRowContext(ctx, `SELECT status FROM exclusivity_challenges WHERE id = $1`, id).Scan(&current)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Challenge not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if current != "pending" {
		http.Error(w, `{"error":"Challenge already reviewed"}`, http.StatusConflict)
		return
	}
	var res sql.Result
	if req.Decision == "rejected" {
		res, err = db.ExecContext(ctx, `UPDATE exclusivity_challenges
			SET status = 'rejected', rejection_reason = $1, reviewed_by = $2, reviewed_at = NOW()
			WHERE id = $3 AND status = 'pending'`, reason, wr69Operator(r), id)
	} else {
		res, err = db.ExecContext(ctx, `UPDATE exclusivity_challenges
			SET status = 'approved', rejection_reason = NULL, reviewed_by = $1, reviewed_at = NOW()
			WHERE id = $2 AND status = 'pending'`, wr69Operator(r), id)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, `{"error":"Challenge already reviewed"}`, http.StatusConflict)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id, "status": req.Decision})
}
