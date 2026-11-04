package main

import (
	"crypto/rand"
	"log"
	"math/big"
	"net/http"
)

// writeInternalError logs detailed internal server errors on the server side
// while returning an opaque sanitized error response to the client (fail-closed, no leak).
func writeInternalError(w http.ResponseWriter, err error) {
	if err != nil {
		log.Printf("[ERROR] Internal Server Error: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{"error":"خطای داخلی سرور"}`))
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

