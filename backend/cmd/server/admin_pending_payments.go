package main

import (
	"database/sql"
	"net/http"
	"slices"
	"strings"

	"github.com/drup/platform/internal/repository"
)

func handleAdminPendingPayments(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	opts, err := parseListPage(r)
	if err != nil {
		http.Error(w, `{"error":"Invalid limit"}`, http.StatusBadRequest)
		return
	}
	page, err := repository.NewOrderRepository(db).ListAdminPendingPayments(r.Context(), opts)
	if err != nil {
		if isInvalidCursorError(err) {
			http.Error(w, `{"error":"cursor is invalid"}`, http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]any{
		"success": true, "orders": page.Items, "nextCursor": page.NextCursor,
		"hasMore": page.HasMore, "limit": page.Limit,
	})
}

const manualConfirmRecordingChosen = false

// Empty until the owner specifies the accepted evidence vocabulary.
var manualConfirmEvidenceKinds = []string{}

func handleAdminConfirmManualPayment(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireFinanceRequestKey(w, r); !ok {
		return
	}
	var req struct {
		EvidenceKind      string `json:"evidenceKind"`
		EvidenceReference string `json:"evidenceReference"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	if !slices.Contains(manualConfirmEvidenceKinds, req.EvidenceKind) || strings.TrimSpace(req.EvidenceReference) == "" {
		http.Error(w, `{"error":"Valid payment evidence is required"}`, http.StatusBadRequest)
		return
	}
	// Activation requires a transaction that records evidence and the keyed
	// result together with the payment transition.
	http.NotFound(w, r)
}
