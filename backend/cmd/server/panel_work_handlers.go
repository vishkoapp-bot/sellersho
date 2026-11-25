package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/repository"
)

func (h *SupplierHandlers) HandleSupplierStatement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	month := r.URL.Query().Get("month")
	if month == "" {
		month = defaultStatementMonth(time.Now())
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "اندازهٔ صفحه معتبر نیست."})
			return
		}
	}
	stmt, err := h.supplierRepo.GetSupplierStatement(r.Context(), supplierID, month, repository.StatementPageOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")})
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "invalid month"), strings.Contains(err.Error(), "invalid statement cursor"):
			t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "دوره یا صفحهٔ گردش حساب معتبر نیست."})
		case errors.Is(err, repository.ErrFinancePrincipalUnresolved):
			t01WriteJSON(w, http.StatusConflict, map[string]string{"error": "بخشی از حساب نیازمند بررسی تیم مالی است."})
		default:
			writeInternalError(w, err)
		}
		return
	}
	t01WriteJSON(w, http.StatusOK, stmt)
}

func (h *SupplierHandlers) HandleSupplierWorkCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	counts, err := h.supplierRepo.GetSupplierWorkCounts(r.Context(), supplierID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, counts)
}

func handleSellerWorkCounts(w http.ResponseWriter, r *http.Request, orderRepo *repository.OrderRepository, storeID string) {
	count, err := orderRepo.CountStoreOrderFollowUp(r.Context(), storeID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]int{"orderFollowUp": count})
}
