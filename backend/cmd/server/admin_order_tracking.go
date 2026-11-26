package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/repository"
)

func handleAdminOrderTracking(w http.ResponseWriter, r *http.Request, orderRepo *repository.OrderRepository) {
	if !t01MethodOnly(w, r, http.MethodGet) {
		return
	}
	opts, err := parseListPage(r)
	if err != nil {
		t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "تعداد سفارش‌های هر صفحه معتبر نیست."})
		return
	}
	var references []string
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		normalized := normalizePersianDigits(search)
		references = []string{search, normalized, strings.ToUpper(search), strings.ToUpper(normalized)}
	}
	page, err := orderRepo.ListAdminOrderTracking(r.Context(), opts, r.URL.Query().Get("status"), references)
	switch {
	case isInvalidCursorError(err):
		t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "صفحهٔ سفارش‌ها معتبر نیست."})
	case errors.Is(err, repository.ErrInvalidAdminOrderStatus):
		t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "فیلتر وضعیت سفارش معتبر نیست."})
	case err != nil:
		writeInternalError(w, err)
	default:
		w.Header().Set("Cache-Control", "no-store")
		t01WriteJSON(w, http.StatusOK, page)
	}
}
