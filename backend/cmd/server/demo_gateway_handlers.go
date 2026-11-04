package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

// Team 01 (Phase 2, live parity): demo gateway APIs (MASTER-AUDIT D18).
//
// *** DEMO / TEST GATEWAY ONLY — NEVER A REAL PAYMENT GATEWAY ***
// These routes are kept strictly separate from Shaparak, whose
// behaviour is untouched. Nothing here moves money, marks an order
// paid, or writes a payment transaction: the callback only simulates
// a gateway round-trip. In a real production deployment this demo
// gateway must be disabled or restricted to test environments
// (live evidence, U63, notes a DEMO_GATEWAY_ENABLED-style gate whose
// exact semantics are unverified, so no gate is invented here — the
// restriction is documented, not silently assumed).
//
// Confirmed probe contract: GET /payments/demo-gateway/info without a
// token returns 400 "token required". The routes are therefore public
// (no auth middleware: the unauthenticated probe reached validation,
// not 401).

func t01DemoToken(r *http.Request) string {
	if t := strings.TrimSpace(r.URL.Query().Get("token")); t != "" {
		return t
	}
	return ""
}

// handleT01DemoGatewayInfo serves GET /api/v1/payments/demo-gateway/info.
func handleT01DemoGatewayInfo(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	if t01DemoToken(r) == "" {
		http.Error(w, `{"error":"token required"}`, http.StatusBadRequest)
		return
	}
	// TODO (U63): the live demo-gateway info payload for a valid token
	// is unverified; it is not guessed here.
	t01Stub(w, r, "D18", "Demo-gateway info payload for a valid token is unverified.")
}

// handleT01DemoGatewayStatus serves GET
// /api/v1/payments/demo-gateway/status.
// FIX 14: live returns 200 WITHOUT a token (probed 2026-11-03):
// {"enabled":true,"gateway":"demo","testOnly":true}. The token
// requirement applies to /info only (confirmed probe), not status.
func handleT01DemoGatewayStatus(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":  true,
		"gateway":  "demo",
		"testOnly": true,
	})
}

// handleT01DemoGatewayCallback serves POST
// /api/v1/payments/demo-gateway/callback. It simulates a gateway
// callback for the given token and deliberately changes NO order or
// payment state.
func handleT01DemoGatewayCallback(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	// The body is optional (the token may arrive as a query
	// parameter); a decode failure must not mask the query token.
	_ = json.NewDecoder(r.Body).Decode(&req)
	token := req.Token
	if token == "" {
		token = t01DemoToken(r)
	}
	if token == "" {
		http.Error(w, `{"error":"token required"}`, http.StatusBadRequest)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"demo":    true,
		"status":  "simulated",
	})
}
