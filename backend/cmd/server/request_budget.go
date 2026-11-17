package main

import (
	"errors"
	"github.com/drup/platform/internal/dependencybudget"
	"net/http"
)

func writeDependencyOverload(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, dependencybudget.ErrOverloaded) {
		return false
	}
	w.Header().Set("Retry-After", "1")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte(`{"error":"سرویس موقتاً شلوغ است؛ کمی بعد دوباره تلاش کنید.","retryable":true}`))
	return true
}
