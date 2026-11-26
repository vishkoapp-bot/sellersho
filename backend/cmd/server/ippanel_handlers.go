package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/drup/platform/internal/dependencybudget"
	"github.com/drup/platform/internal/ippanel"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"time"

	"database/sql"
)

func handleIPPanelCredit(w http.ResponseWriter, r *http.Request, client *ippanel.Client) {
	w.Header().Set("Content-Type", "application/json")
	if client == nil {
		http.Error(w, `{"error":"IPPanel is disabled"}`, http.StatusServiceUnavailable)
		return
	}
	data, err := client.Credit(r.Context())
	if err != nil {
		if writeDependencyOverload(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	json.NewEncoder(w).Encode(data)
}

func handleIPPanelRecipients(w http.ResponseWriter, r *http.Request, client *ippanel.Client) {
	w.Header().Set("Content-Type", "application/json")
	bulkID := r.URL.Query().Get("bulk_id")
	if bulkID == "" {
		http.Error(w, `{"error":"bulk_id is required"}`, http.StatusBadRequest)
		return
	}
	if client == nil {
		http.Error(w, `{"error":"IPPanel is disabled"}`, http.StatusServiceUnavailable)
		return
	}
	data, err := client.Recipients(r.Context(), bulkID)
	if err != nil {
		if writeDependencyOverload(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	json.NewEncoder(w).Encode(data)
}

func loadIPPanelClient(ctx context.Context, db *sql.DB) (*ippanel.Client, bool, error) {
	var enabled bool
	var baseURL, key, sender, pattern string
	err := db.QueryRowContext(ctx, `SELECT ippanel_enabled, ippanel_base_url, ippanel_api_key, ippanel_sender, ippanel_pattern_code FROM platform_settings WHERE id=1`).Scan(&enabled, &baseURL, &key, &sender, &pattern)
	if err != nil {
		return nil, false, err
	}
	if !enabled || strings.TrimSpace(key) == "" || strings.TrimSpace(pattern) == "" {
		return nil, false, nil
	}
	return ippanel.New(baseURL, key, sender), true, nil
}

func handleIPPanelSettings(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	w.Header().Set("Content-Type", "application/json")
	if db == nil {
		http.Error(w, `{"error":"Database unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet {
		var enabled bool
		var baseURL, key, sender, pattern string
		err := db.QueryRowContext(r.Context(), `SELECT ippanel_enabled, ippanel_base_url, ippanel_api_key, ippanel_sender, ippanel_pattern_code FROM platform_settings WHERE id=1`).Scan(&enabled, &baseURL, &key, &sender, &pattern)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		masked := ""
		if len(key) > 8 {
			masked = key[:4] + "..." + key[len(key)-4:]
		} else if key != "" {
			masked = "configured"
		}
		json.NewEncoder(w).Encode(map[string]any{"enabled": enabled, "baseUrl": baseURL, "apiKeyMasked": masked, "sender": sender, "patternCode": pattern})
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var req struct {
		Enabled     bool   `json:"enabled"`
		BaseURL     string `json:"baseUrl"`
		APIKey      string `json:"apiKey"`
		Sender      string `json:"sender"`
		PatternCode string `json:"patternCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://edge.ippanel.com/v1"
	}
	_, err := db.ExecContext(r.Context(), `UPDATE platform_settings SET ippanel_enabled=$1, ippanel_base_url=$2,
	  ippanel_api_key=COALESCE(NULLIF($3,''), ippanel_api_key), ippanel_sender=$4, ippanel_pattern_code=$5,
	  updated_at=NOW() WHERE id=1`, req.Enabled, baseURL, strings.TrimSpace(req.APIKey), strings.TrimSpace(req.Sender), strings.TrimSpace(req.PatternCode))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func handleIPPanelTest(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	client, enabled, loadErr := loadIPPanelClient(r.Context(), db)
	if loadErr != nil {
		http.Error(w, loadErr.Error(), http.StatusInternalServerError)
		return
	}
	if !enabled {
		http.Error(w, `{"error":"IPPanel is disabled or incomplete"}`, http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Recipient string `json:"recipient"`
		Code      string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Recipient) == "" {
		http.Error(w, `{"error":"recipient is required"}`, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		req.Code = "123456"
	}
	var pattern string
	_ = db.QueryRowContext(r.Context(), `SELECT ippanel_pattern_code FROM platform_settings WHERE id=1`).Scan(&pattern)
	if pattern == "" {
		http.Error(w, `{"error":"pattern code is not configured"}`, http.StatusBadRequest)
		return
	}
	ids, err := client.SendPattern(r.Context(), req.Recipient, pattern, map[string]string{"code": req.Code})
	if err != nil {
		// F8: log the failed test send in sms_logs, like the OTP path
		// (main.go OTP logSMS callback) logs its failures — best effort,
		// never masks the send error.
		status := "failed"
		if dependencybudget.IsAmbiguous(err) {
			status = "unknown"
		}
		logIPPanelTestSMS(r.Context(), db, req.Recipient, status, "", err.Error())
		if writeDependencyOverload(w, err) {
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}
	// F8: the admin test SMS is a real send — write the same sms_logs
	// row the OTP path writes (same columns, same masking, same
	// provider), so it shows up in the admin SMS log (GET
	// /api/v1/admin/sms/logs). message_type "test" distinguishes it
	// from OTP ("otp") rows.
	logIPPanelTestSMS(r.Context(), db, req.Recipient, "sent", strings.Join(ids, ","), "")
	json.NewEncoder(w).Encode(map[string]any{"success": true, "messageOutboxIds": ids, "sentAt": time.Now()})
}

// logIPPanelTestSMS mirrors the OTP sms_logs insert in main.go: same
// table/columns, same mobile masking (first 4 + **** + last 3), same
// provider ("ippanel"). Best effort — a logging failure is swallowed
// so it can never turn a send result into a handler error.
func logIPPanelTestSMS(ctx context.Context, db *sql.DB, mobile, status, providerID, message string) {
	if db == nil {
		return
	}
	masked := mobile
	if len(masked) > 7 {
		masked = masked[:4] + "****" + masked[len(masked)-3:]
	}
	_, _ = db.ExecContext(ctx, `INSERT INTO sms_logs (id,mobile_masked,message_type,provider,provider_message_id,status,error) VALUES ($1,$2,$3,$4,$5,$6,$7)`, uuid.New(), masked, "test", "ippanel", providerID, status, message)
}
