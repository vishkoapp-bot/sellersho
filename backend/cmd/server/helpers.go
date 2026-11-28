package main

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
)

// writeInternalError reports a correlation ID while keeping internal details
// out of the response and the general application log.
func writeInternalError(w http.ResponseWriter, err error) {
	requestID := w.Header().Get("X-Request-ID")
	if err != nil {
		slog.Error("internal request error", "request_id", requestID, "error_type", fmt.Sprintf("%T", err))
	}
	payload := map[string]string{"error": "خطای داخلی سرور", "code": "internal_error"}
	if requestID != "" {
		payload["requestId"] = requestID
	}
	t01WriteJSON(w, http.StatusInternalServerError, payload)
}

// secureRandomInt64 generates a cryptographically secure pseudo-random int64 in [min, max]
func secureRandomInt64(min, max int64) int64 {
	diff := max - min + 1
	if diff <= 0 {
		return min
	}
	nBig, err := rand.Int(rand.Reader, big.NewInt(diff))
	if err != nil {
		return min
	}
	return nBig.Int64() + min
}
