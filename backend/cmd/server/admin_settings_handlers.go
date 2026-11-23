package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/drup/platform/internal/pricing"
	"net/http"
)

// Team 01 (Phase 2, live parity): admin settings family, non-pricing
// parts only (MASTER-AUDIT D14).
//
// Pricing/margins/platform-pct/fee-history are owned by Team 02 and
// are NOT duplicated here. admin/settings/base-fee (D20, deleted in
// live) is deliberately NOT brought back: there is no base-fee route
// in this file.
//
// Every route below reads/writes one or more platform_settings
// columns of the singleton row (id = 1). Column names are grounded in
// the migration chain; keys not in a route's allowlist are rejected
// (fail-closed) rather than silently ignored. settlement_day and
// early_withdrawal_fee_pct are SETTINGS DATA only: their presence
// does not prove any automatic behaviour (see finance handlers).

type t01SettingField struct {
	JSONKey   string
	Column    string
	Kind      string // "int", "float", "bool", "text", "json"
	Min       float64
	HasMin    bool
	Max       float64
	HasMax    bool // int: inclusive max; float: exclusive max (fee < 100)
	AllowNull bool
	Nullable  bool // when true, GET renders JSON null for a NULL column instead of the fallback
}

func t01SettingsInt(ctx context.Context, db *sql.DB, f t01SettingField) any {
	if !f.Nullable {
		return t01PlatformInt(ctx, db, f.Column, 0)
	}
	var value sql.NullInt64
	if err := db.QueryRowContext(ctx,
		"SELECT "+f.Column+" FROM platform_settings WHERE id = 1").Scan(&value); err != nil || !value.Valid {
		return nil
	}
	return value.Int64
}

func t01SettingsGet(w http.ResponseWriter, r *http.Request, db *sql.DB, fields []t01SettingField) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	out := map[string]interface{}{}
	for _, f := range fields {
		if !t01ColumnExists(ctx, db, "platform_settings", f.Column) {
			continue
		}
		switch f.Kind {
		case "int":
			out[f.JSONKey] = t01SettingsInt(ctx, db, f)
		case "float":
			out[f.JSONKey] = t01PlatformFloat(ctx, db, f.Column, 0)
		case "bool":
			var v sql.NullBool
			if err := db.QueryRowContext(ctx,
				"SELECT "+f.Column+" FROM platform_settings WHERE id = 1").Scan(&v); err == nil && v.Valid {
				out[f.JSONKey] = v.Bool
			}
		case "text":
			out[f.JSONKey] = t01PlatformText(ctx, db, f.Column, "")
		case "json":
			out[f.JSONKey] = t01PlatformJSON(ctx, db, f.Column)
		}
	}
	t01WriteJSON(w, http.StatusOK, out)
}

func t01SettingsPut(w http.ResponseWriter, r *http.Request, db *sql.DB, fields []t01SettingField) {
	if !t01MethodOnly(w, r, http.MethodPut, http.MethodPatch) || t01DBNil(w, db) {
		return
	}
	var body map[string]json.RawMessage
	if !t01DecodeJSON(w, r, &body) {
		return
	}
	rawBody, _ := json.Marshal(body)
	var exactBody any
	if err := decodeMoneyJSON(bytes.NewReader(rawBody), &exactBody); err != nil {
		http.Error(w, `{"error":"Unsupported monetary precision or bounds"}`, http.StatusBadRequest)
		return
	}
	byKey := map[string]t01SettingField{}
	for _, f := range fields {
		byKey[f.JSONKey] = f
	}
	for k := range body {
		if _, ok := byKey[k]; !ok {
			http.Error(w, fmt.Sprintf(`{"error":"Unknown setting: %s"}`, k), http.StatusBadRequest)
			return
		}
	}
	ctx := r.Context()
	for k, raw := range body {
		f := byKey[k]
		if string(raw) == "null" {
			if !f.AllowNull {
				http.Error(w, fmt.Sprintf(`{"error":"Setting %s may not be null"}`, k), http.StatusBadRequest)
				return
			}
			if err := t01SetPlatformColumn(ctx, db, f.Column, nil); err != nil {
				writeInternalError(w, err)
				return
			}
			continue
		}
		switch f.Kind {
		case "int":
			var v float64
			if err := json.Unmarshal(raw, &v); err != nil || v != float64(int64(v)) {
				http.Error(w, fmt.Sprintf(`{"error":"Setting %s must be an integer"}`, k), http.StatusBadRequest)
				return
			}
			if (f.HasMin && v < f.Min) || (f.HasMax && v > f.Max) {
				http.Error(w, fmt.Sprintf(`{"error":"Setting %s out of range"}`, k), http.StatusBadRequest)
				return
			}
			if err := t01SetPlatformColumn(ctx, db, f.Column, int64(v)); err != nil {
				writeInternalError(w, err)
				return
			}
		case "float":
			var v float64
			if err := json.Unmarshal(raw, &v); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"Setting %s must be a number"}`, k), http.StatusBadRequest)
				return
			}
			if k == "earlyWithdrawalFeePct" {
				if _, err := pricing.ParsePercent(v); err != nil {
					http.Error(w, `{"error":"Fee percent supports at most two decimal places"}`, http.StatusBadRequest)
					return
				}
			}
			if (f.HasMin && v < f.Min) || (f.HasMax && v >= f.Max) {
				http.Error(w, fmt.Sprintf(`{"error":"Setting %s out of range"}`, k), http.StatusBadRequest)
				return
			}
			if err := t01SetPlatformColumn(ctx, db, f.Column, v); err != nil {
				writeInternalError(w, err)
				return
			}
		case "bool":
			var v bool
			if err := json.Unmarshal(raw, &v); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"Setting %s must be a boolean"}`, k), http.StatusBadRequest)
				return
			}
			if err := t01SetPlatformColumn(ctx, db, f.Column, v); err != nil {
				writeInternalError(w, err)
				return
			}
		case "text":
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"Setting %s must be a string"}`, k), http.StatusBadRequest)
				return
			}
			if err := t01SetPlatformColumn(ctx, db, f.Column, v); err != nil {
				writeInternalError(w, err)
				return
			}
		case "json":
			var v interface{}
			if err := json.Unmarshal(raw, &v); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"Setting %s must be valid JSON"}`, k), http.StatusBadRequest)
				return
			}
			encoded, _ := json.Marshal(v)
			if _, err := db.ExecContext(ctx,
				"UPDATE platform_settings SET "+f.Column+" = $1::jsonb WHERE id = 1", string(encoded)); err != nil {
				writeInternalError(w, err)
				return
			}
		}
	}
	// Echo the stored values back.
	t01SettingsGetRaw(w, r, db, fields)
}

// t01SettingsGetRaw renders the current values without re-checking the
// method (used after a successful PUT).
func t01SettingsGetRaw(w http.ResponseWriter, r *http.Request, db *sql.DB, fields []t01SettingField) {
	ctx := r.Context()
	out := map[string]interface{}{}
	for _, f := range fields {
		if !t01ColumnExists(ctx, db, "platform_settings", f.Column) {
			continue
		}
		switch f.Kind {
		case "int":
			out[f.JSONKey] = t01SettingsInt(ctx, db, f)
		case "float":
			out[f.JSONKey] = t01PlatformFloat(ctx, db, f.Column, 0)
		case "bool":
			var v sql.NullBool
			if err := db.QueryRowContext(ctx,
				"SELECT "+f.Column+" FROM platform_settings WHERE id = 1").Scan(&v); err == nil && v.Valid {
				out[f.JSONKey] = v.Bool
			}
		case "text":
			out[f.JSONKey] = t01PlatformText(ctx, db, f.Column, "")
		case "json":
			out[f.JSONKey] = t01PlatformJSON(ctx, db, f.Column)
		}
	}
	t01WriteJSON(w, http.StatusOK, out)
}

// --- D14 routes ------------------------------------------------------------
// Each pair shares one handler dispatching on the method.

func t01PlatformSettingsFields() []t01SettingField {
	return []t01SettingField{
		{JSONKey: "returnWindowDays", Column: "return_window_days", Kind: "int", Min: 1, HasMin: true, Max: 365, HasMax: true},
	}
}

func handleT01AdminSettingsPlatform(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, t01PlatformSettingsFields())
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, t01PlatformSettingsFields())
	default:
		writeMethodNotAllowed(w)
	}
}

func t01ShippingSettingsFields() []t01SettingField {
	return []t01SettingField{
		{JSONKey: "supplierReturnPostalCharge", Column: "supplier_return_postal_charge", Kind: "int", Min: 0, HasMin: true, AllowNull: true, Nullable: true},
		{JSONKey: "defaultItemWeightGrams", Column: "default_item_weight_grams", Kind: "int", Min: 1, HasMin: true},
	}
}

func handleT01AdminSettingsShipping(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, t01ShippingSettingsFields())
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, t01ShippingSettingsFields())
	default:
		writeMethodNotAllowed(w)
	}
}

func t01MarketingSettingsFields() []t01SettingField {
	return []t01SettingField{
		{JSONKey: "referralCommissionPercent", Column: "referral_commission_percent", Kind: "float", Min: 0, HasMin: true},
		{JSONKey: "referralIsEnabled", Column: "referral_is_enabled", Kind: "bool"},
		{JSONKey: "trialCatalogQuota", Column: "trial_catalog_quota", Kind: "int", Min: 0, HasMin: true},
		{JSONKey: "centralCampaignBudget", Column: "central_campaign_budget", Kind: "int", Min: 0, HasMin: true},
	}
}

func handleT01AdminSettingsMarketing(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, t01MarketingSettingsFields())
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, t01MarketingSettingsFields())
	default:
		writeMethodNotAllowed(w)
	}
}

func t01ContractsSettingsFields() []t01SettingField {
	return []t01SettingField{
		{JSONKey: "sellerContractVersion", Column: "seller_contract_version", Kind: "int", Min: 0, HasMin: true},
		{JSONKey: "supplierContractVersion", Column: "supplier_contract_version", Kind: "int", Min: 0, HasMin: true},
	}
}

func handleT01AdminSettingsContracts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, t01ContractsSettingsFields())
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, t01ContractsSettingsFields())
	default:
		writeMethodNotAllowed(w)
	}
}

// handleT01AdminSettingsSettlement serves GET/PUT
// /api/v1/admin/settings/settlement (settings read/write ONLY; the
// monthly engine itself lives in finance_parity_handlers.go and is
// manual-trigger only).
func t01SettlementSettingsFields() []t01SettingField {
	return []t01SettingField{
		{JSONKey: "settlementDay", Column: "settlement_day", Kind: "int", Min: 1, HasMin: true, Max: 31, HasMax: true, AllowNull: true},
		{JSONKey: "earlyWithdrawalFeePct", Column: "early_withdrawal_fee_pct", Kind: "float", Min: 0, HasMin: true, Max: 100, HasMax: true, AllowNull: true},
	}
}

func handleT01AdminSettingsSettlement(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, t01SettlementSettingsFields())
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, t01SettlementSettingsFields())
	default:
		writeMethodNotAllowed(w)
	}
}

func handleT01AdminSettingsCampaignBudget(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	fields := []t01SettingField{
		{JSONKey: "centralCampaignBudget", Column: "central_campaign_budget", Kind: "int", Min: 0, HasMin: true},
	}
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, fields)
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, fields)
	default:
		writeMethodNotAllowed(w)
	}
}

func handleT01AdminSettingsTrialQuota(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	fields := []t01SettingField{
		{JSONKey: "trialCatalogQuota", Column: "trial_catalog_quota", Kind: "int", Min: 0, HasMin: true},
	}
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, fields)
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, fields)
	default:
		writeMethodNotAllowed(w)
	}
}

func t01JSONSettingsHandler(column, jsonKey string) func(http.ResponseWriter, *http.Request, *sql.DB) {
	fields := []t01SettingField{{JSONKey: jsonKey, Column: column, Kind: "json"}}
	return func(w http.ResponseWriter, r *http.Request, db *sql.DB) {
		switch r.Method {
		case http.MethodGet:
			t01SettingsGet(w, r, db, fields)
		case http.MethodPut, http.MethodPatch:
			t01SettingsPut(w, r, db, fields)
		default:
			writeMethodNotAllowed(w)
		}
	}
}

func handleT01AdminSettingsWarehouseTariffs(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	t01JSONSettingsHandler("warehouse_tariffs", "tariffs")(w, r, db)
}

func handleT01AdminSettingsPackagingTariffs(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	t01JSONSettingsHandler("packaging_tariffs", "tariffs")(w, r, db)
}

func handleT01AdminSettingsTapinTariff(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	fields := []t01SettingField{
		{JSONKey: "tapinTariffUrl", Column: "tapin_tariff_url", Kind: "text"},
	}
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, fields)
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, fields)
	default:
		writeMethodNotAllowed(w)
	}
}

// handleT01AdminSettingsTapinPlatform serves GET/PUT
// /api/v1/admin/settings/tapin-platform. It NEVER accepts, stores, or
// returns the platform Tapin token (U63): only the shop id and the
// default box id are readable/writable here. Any other key in the
// request body (including a token field) is rejected by the generic
// PUT allowlist rather than silently dropped.
func handleT01AdminSettingsTapinPlatform(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	fields := []t01SettingField{
		{JSONKey: "tapinPlatformShopId", Column: "tapin_platform_shop_id", Kind: "text"},
		{JSONKey: "tapinDefaultBoxId", Column: "tapin_default_box_id", Kind: "text"},
	}
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, fields)
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, fields)
	default:
		writeMethodNotAllowed(w)
	}
}

func handleT01AdminSettingsSupplierContract(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	fields := []t01SettingField{
		{JSONKey: "supplierContractVersion", Column: "supplier_contract_version", Kind: "int", Min: 0, HasMin: true},
	}
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, fields)
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, fields)
	default:
		writeMethodNotAllowed(w)
	}
}

// handleT01AdminSettingsStorageFees serves GET/PUT
// /api/v1/admin/settings/storage-fees (gap 7). The flag defaults OFF
// (migration 202611050005); activation is a separate owner decision.
// Tariffs are integer Tomans; the assessment engine reads these same
// columns (repository GetStorageFeeSettings).
func t01StorageFeeSettingsFields() []t01SettingField {
	return []t01SettingField{
		{JSONKey: "storageFeeEnabled", Column: "storage_fee_enabled", Kind: "bool"},
		{JSONKey: "storageFeeFreeDays", Column: "storage_fee_free_days", Kind: "int", Min: 0, HasMin: true, Max: 365, HasMax: true},
		{JSONKey: "storageFeePerKgDay", Column: "storage_fee_per_kg_day", Kind: "int", Min: 0, HasMin: true},
		{JSONKey: "storageFeePerM3Day", Column: "storage_fee_per_m3_day", Kind: "int", Min: 0, HasMin: true},
	}
}

func handleT01AdminSettingsStorageFees(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	switch r.Method {
	case http.MethodGet:
		t01SettingsGet(w, r, db, t01StorageFeeSettingsFields())
	case http.MethodPut, http.MethodPatch:
		t01SettingsPut(w, r, db, t01StorageFeeSettingsFields())
	default:
		writeMethodNotAllowed(w)
	}
}
