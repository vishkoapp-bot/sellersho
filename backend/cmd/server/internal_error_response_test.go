package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInternalErrorResponseIncludesRequestIDWithoutRawDetails(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("X-Request-ID", "0123456789abcdef0123456789abcdef")
	cause := "synthetic private database detail"
	writeInternalError(w, errors.New(cause))

	if w.Code != http.StatusInternalServerError || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected internal error response: %d %v", w.Code, w.Header())
	}
	if strings.Contains(w.Body.String(), cause) {
		t.Fatal("response exposed the internal cause")
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] == "" || body["code"] != "internal_error" || body["requestId"] != w.Header().Get("X-Request-ID") {
		t.Fatalf("unexpected error contract: %v", body)
	}
}
