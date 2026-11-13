package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var ErrStoreForbidden = errors.New("store is not owned by the authenticated seller")
var ErrStoreInput = errors.New("invalid store configuration")

// ErrStoreInsufficientBalance is returned when a wallet-funded store
// purchase (subscription plan, SMS credit, ...) exceeds the store's
// available released balance. Kept distinct from ErrStoreInput so the
// handler can answer with a specific Persian message instead of a
// generic English "Invalid input".
var ErrStoreInsufficientBalance = errors.New("store wallet balance is insufficient for this purchase")

// ErrUnknownSellerService is returned when a wallet-funded purchase
// names a service the platform does not sell (unknown plan/package id).
// Kept distinct from ErrStoreInput for the same handler-message reason.
var ErrUnknownSellerService = errors.New("unknown seller service")

// ErrSMSServiceUnavailable is returned when a seller tries to buy an
// SMS credit pack (sms_500/sms_2000). The packs are NOT for sale until
// a sending path exists: selling credits with nowhere to send them
// would take wallet money for an unusable entitlement. Returned before
// any wallet debit or purchase-history write.
var ErrSMSServiceUnavailable = errors.New("sms credit packs are not for sale: no sending path exists yet")

func (r *StoreRepository) OwnedStore(ctx context.Context, ownerID, slug string) (*domain.Store, error) {
	store, err := r.GetStoreBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, sql.ErrNoRows
	}
	if store.OwnerUserID == nil || *store.OwnerUserID != ownerID || store.OwnerType != "dropshipper" {
		return nil, ErrStoreForbidden
	}
	return store, nil
}

func (r *StoreRepository) loadPanelConfig(ctx context.Context, store *domain.Store) error {
	var raw []byte
	if err := r.db.QueryRowContext(ctx, `SELECT panel_config, COALESCE(tagline,''), COALESCE(description,''), COALESCE(status,'active'), active_categories FROM stores WHERE id=$1`, store.ID).Scan(&raw, &store.Tagline, &store.Description, &store.Status, pq.Array(&store.ActiveCategories)); err != nil {
		if pgErr, ok := err.(*pq.Error); ok && (pgErr.Code == "42703" || pgErr.Code == "42P01") {
			store.SelectedNicheIDs = []string{}
			store.ActiveProductIDs = []string{}
			store.CentralDealsProductIDs = []string{}
			store.Status = "active"
			return nil
		}
		return err
	}
	var config struct {
		ShortDescription       string          `json:"shortDescription"`
		SelectedNicheIDs       []string        `json:"selectedNicheIds"`
		BuilderConfig          json.RawMessage `json:"builderConfig"`
		CentralDealsFilter     string          `json:"centralDealsFilter"`
		CentralDealsProductIDs []string        `json:"centralDealsProductIds"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	store.PanelConfig = raw
	store.ShortDescription = config.ShortDescription
	store.SelectedNicheIDs = config.SelectedNicheIDs
	store.BuilderConfig = config.BuilderConfig
	store.CentralDealsFilter = config.CentralDealsFilter
	store.CentralDealsProductIDs = config.CentralDealsProductIDs
	if store.CentralDealsProductIDs == nil {
		store.CentralDealsProductIDs = []string{}
	}
	if store.Status == "" {
		store.Status = "active"
	}
	if store.ActiveCategories == nil {
		store.ActiveCategories = []string{}
	}
	store.ActiveProductIDs = []string{}
	if store.SelectedNicheIDs == nil {
		store.SelectedNicheIDs = []string{}
	}
	rows, err := r.db.QueryContext(ctx, `SELECT product_id FROM store_products WHERE store_id=$1 ORDER BY position,product_id`, store.ID)
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "42P01" {
			return nil
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		store.ActiveProductIDs = append(store.ActiveProductIDs, id)
	}
	return rows.Err()
}

func (r *StoreRepository) SaveStoreProducts(ctx context.Context, ownerID, slug string, ids []string) error {
	store, err := r.OwnedStore(ctx, ownerID, slug)
	if err != nil {
		return err
	}
	if len(ids) > 1000 {
		return ErrStoreInput
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return ErrStoreInput
		}
		seen[id] = true
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products p WHERE p.id=ANY($1::varchar[]) AND p.supplier_base_cost>0`, pq.Array(ids)).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return ErrStoreInput
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM store_products WHERE store_id=$1::varchar AND EXISTS(SELECT 1 FROM stores WHERE id=$1::varchar AND owner_user_id=$2::uuid)`, store.ID, ownerID); err != nil {
		return err
	}
	// M3 (2026-11-13): per-store variant settings must not survive
	// assortment removal. store_variant_visibility and
	// store_variant_defaults have no FK to store_products (only to
	// stores/products/variants), so nothing cascades: without this,
	// removing product P and re-adding it later would silently
	// resurrect its old hidden variants and store default. Delete
	// both tables' rows for every product leaving the assortment,
	// in this same transaction — the rewrite is atomic, so the
	// settings can neither outlive the listing nor vanish while it
	// stays. (store_variant_overrides is intentionally untouched:
	// pre-existing table, same lifecycle question — owner decision
	// pending, not silently changed here.)
	if _, err := tx.ExecContext(ctx, `DELETE FROM store_variant_visibility WHERE store_id=$1::varchar AND NOT (product_id = ANY($2::varchar[]))`, store.ID, pq.Array(ids)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM store_variant_defaults WHERE store_id=$1::varchar AND NOT (product_id = ANY($2::varchar[]))`, store.ID, pq.Array(ids)); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO store_products(store_id,product_id,position) SELECT $1::varchar,$2::varchar,$3 WHERE EXISTS(SELECT 1 FROM stores WHERE id=$1::varchar AND owner_user_id=$4::uuid)`, store.ID, id, i, ownerID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// parseSignedContractVersion extracts the seller contract version
// from a settings map, as decoded from JSON (numbers arrive as
// float64). It returns has=false when the key is absent, so ordinary
// settings saves leave stores.signed_contract_version untouched.
// A present but non-integral, non-positive, or non-numeric value is
// ErrStoreInput: writing it to the column would corrupt the modal
// condition, and silently ignoring it would lose the reconfirmation.
func parseSignedContractVersion(config map[string]interface{}) (int, bool, error) {
	raw, ok := config["signedContractVersion"]
	if !ok {
		return 0, false, nil
	}
	switch v := raw.(type) {
	case float64:
		if v != math.Trunc(v) || v < 1 {
			return 0, false, ErrStoreInput
		}
		return int(v), true, nil
	case int:
		if v < 1 {
			return 0, false, ErrStoreInput
		}
		return v, true, nil
	case int64:
		if v < 1 {
			return 0, false, ErrStoreInput
		}
		return int(v), true, nil
	case json.Number:
		n, err := v.Int64()
		if err != nil || n < 1 {
			return 0, false, ErrStoreInput
		}
		return int(n), true, nil
	default:
		return 0, false, ErrStoreInput
	}
}

func (r *StoreRepository) SaveStoreSettings(ctx context.Context, ownerID, slug string, config map[string]interface{}) error {
	store, err := r.OwnedStore(ctx, ownerID, slug)
	if err != nil {
		return err
	}
	allowed := map[string]bool{"tagline": true, "shortDescription": true, "selectedNicheIds": true, "builderConfig": true, "centralDealsFilter": true, "centralDealsProductIds": true, "signedContractVersion": true}
	for key := range config {
		if !allowed[key] {
			return ErrStoreInput
		}
	}
	for _, key := range []string{"tagline", "shortDescription"} {
		if value, exists := config[key]; exists {
			text, ok := value.(string)
			if !ok || len(text) > 3000 {
				return ErrStoreInput
			}
		}
	}
	if ids, exists := config["selectedNicheIds"]; exists {
		values, ok := ids.([]interface{})
		if !ok || len(values) > 100 {
			return ErrStoreInput
		}
		seen := map[string]bool{}
		for _, raw := range values {
			id, ok := raw.(string)
			if !ok || seen[id] {
				return ErrStoreInput
			}
			seen[id] = true
			var found bool
			if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM niches WHERE id=$1)`, id).Scan(&found); err != nil {
				return err
			}
			if !found {
				return ErrStoreInput
			}
		}
	}
	if builder, exists := config["builderConfig"]; exists {
		value, ok := builder.(map[string]interface{})
		if !ok {
			return ErrStoreInput
		}
		theme, _ := value["selectedTheme"].(string)
		if theme != "general" && theme != "digital" && theme != "fashion" && theme != "home" {
			return ErrStoreInput
		}
		color, _ := value["accentColor"].(string)
		if len(color) != 7 || !strings.HasPrefix(color, "#") {
			return ErrStoreInput
		}
		if len(value) > 6 {
			return ErrStoreInput
		}
	}
	// signedContractVersion is stored in panel_config by the merge
	// below, but the seller contract modal reads the
	// stores.signed_contract_version column (SellerDashboardView:
	// signed < latest), so a reconfirmation must also write the
	// column or acceptance is lost on reload. Parsed here, before
	// any write, so an invalid value fails the whole save instead
	// of merging panel_config without the column.
	signedVersion, hasSignedVersion, err := parseSignedContractVersion(config)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	updateQuery := `UPDATE stores SET panel_config=panel_config || $1::jsonb, tagline=COALESCE($1::jsonb->>'tagline',tagline), description=COALESCE($1::jsonb->>'shortDescription',description), updated_at=NOW() WHERE id=$2 AND owner_user_id=$3`
	updateArgs := []interface{}{string(raw), store.ID, ownerID}
	if hasSignedVersion {
		updateQuery = `UPDATE stores SET panel_config=panel_config || $1::jsonb, tagline=COALESCE($1::jsonb->>'tagline',tagline), description=COALESCE($1::jsonb->>'shortDescription',description), signed_contract_version=$4, updated_at=NOW() WHERE id=$2 AND owner_user_id=$3`
		updateArgs = append(updateArgs, signedVersion)
	}
	result, err := r.db.ExecContext(ctx, updateQuery, updateArgs...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrStoreForbidden
	}
	return nil
}

func (r *StoreRepository) SaveOwnedPricing(ctx context.Context, ownerID, slug, kind, target, mode string, value float64) error {
	store, err := r.OwnedStore(ctx, ownerID, slug)
	if err != nil {
		return err
	}
	if store.IsMarginLockedByAdmin {
		return ErrStoreForbidden
	}
	// A non-positive value deletes the override (the StoreContext
	// convention: local delete + server save with value <= 0). Deletion
	// stays lock-gated above. Store margin has no delete: non-positive
	// store margins keep falling through to the range check below.
	if value <= 0 {
		switch kind {
		case "product":
			_, err := r.db.ExecContext(ctx, `DELETE FROM store_product_overrides WHERE store_id = $1 AND product_id = $2`, store.ID, target)
			return err
		case "niche":
			_, err := r.db.ExecContext(ctx, `DELETE FROM store_niche_overrides WHERE store_id = $1 AND niche_id = $2`, store.ID, target)
			return err
		}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1000000000 {
		return ErrStoreInput
	}
	settings, err := r.GetPlatformSettings(ctx)
	if err != nil {
		return err
	}
	if kind == "store_margin" {
		mode = "percent"
	}
	// Percent-only: an empty mode means percent; any other mode
	// (including "fixed") is rejected, never silently ignored.
	if mode == "" {
		mode = "percent"
	}
	if mode != "percent" {
		return ErrStoreInput
	}
	// ISO-C margin model: the per-product ceiling (when set) replaces the
	// global MaxProfitMargin for product overrides; it is resolved inside
	// the product case below. Other kinds keep the global ceiling here.
	if mode == "percent" && kind != "product" && (value < math.Max(0, settings.MinProfitMargin) || value > settings.MaxProfitMargin) {
		return ErrStoreInput
	}
	if mode == "percent" && kind == "product" && value < math.Max(0, settings.MinProfitMargin) {
		return ErrStoreInput
	}
	switch kind {
	case "store_margin":
		_, err = r.db.ExecContext(ctx, `UPDATE stores SET profit_margin_percent=$1,custom_store_margin_override=$1,updated_at=NOW() WHERE id=$2 AND owner_user_id=$3`, value, store.ID, ownerID)
	case "product":
		var exists bool
		if err = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id=$1 AND supplier_id IS NOT NULL)`, target).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrStoreInput
		}
		if mode == "percent" {
			// Per-product ceiling replaces the global ceiling for this
			// product (ISO-C margin model); absent row means global binds.
			ceiling := settings.MaxProfitMargin
			var pc float64
			if err := r.db.QueryRowContext(ctx, `SELECT max_margin_pct FROM product_margin_ceilings WHERE product_id = $1`, target).Scan(&pc); err == nil {
				ceiling = pc
			} else if err != sql.ErrNoRows {
				return err
			}
			if value > ceiling {
				return ErrStoreInput
			}
		}
		_, err = r.db.ExecContext(ctx, `INSERT INTO store_product_overrides(store_id,product_id,override_value,mode) SELECT $1::varchar,$2::varchar,$3,$4::varchar WHERE EXISTS(SELECT 1 FROM stores WHERE id=$1::varchar AND owner_user_id=$5::uuid) ON CONFLICT(store_id,product_id) DO UPDATE SET override_value=EXCLUDED.override_value,mode=EXCLUDED.mode`, store.ID, target, value, mode, ownerID)
	case "niche":
		var exists bool
		if err = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM niches WHERE id=$1)`, target).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrStoreInput
		}
		_, err = r.db.ExecContext(ctx, `INSERT INTO store_niche_overrides(store_id,niche_id,override_value,mode) SELECT $1::varchar,$2::varchar,$3,$4::varchar WHERE EXISTS(SELECT 1 FROM stores WHERE id=$1::varchar AND owner_user_id=$5::uuid) ON CONFLICT(store_id,niche_id) DO UPDATE SET override_value=EXCLUDED.override_value,mode=EXCLUDED.mode`, store.ID, target, value, mode, ownerID)
	default:
		return ErrStoreInput
	}
	return err
}

func (r *StoreRepository) StoreProductActive(ctx context.Context, storeID, productID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM store_products WHERE store_id=$1 AND product_id=$2)`, storeID, productID).Scan(&exists)
	return exists, err
}

// ---------------------------------------------------------------------------
// P2(a) per-store variant visibility + P2(c) per-store default variant.
// Absence of a visibility row = visible; absence of a default row =
// fall back to the product-level default. Central store never has rows.
// ---------------------------------------------------------------------------

// IsVariantVisibleInStore reports whether a variant may sell in a
// store: false only when an explicit is_visible=false row exists.
// Central stores are never filtered (always visible). Used by the
// order/cart path to re-check visibility at checkout.
func (r *StoreRepository) IsVariantVisibleInStore(ctx context.Context, storeID, variantID string) (bool, error) {
	if storeID == "" || storeID == "central" {
		return true, nil
	}
	var visible bool
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT is_visible FROM store_variant_visibility WHERE store_id=$1 AND variant_id=$2), TRUE)`, storeID, variantID).Scan(&visible)
	return visible, err
}

// SetStoreVariantVisibility upserts is_visible=false, or DELETEs the
// row when visible=true (reset to default-visible, table stays sparse).
// Caller must have verified ownership, assortment membership, and that
// the variant belongs to the product.
func (r *StoreRepository) SetStoreVariantVisibility(ctx context.Context, storeID, productID, variantID string, visible bool) error {
	if visible {
		_, err := r.db.ExecContext(ctx, `DELETE FROM store_variant_visibility WHERE store_id=$1 AND product_id=$2 AND variant_id=$3`, storeID, productID, variantID)
		return err
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO store_variant_visibility (store_id, product_id, variant_id, is_visible) VALUES ($1,$2,$3,FALSE)
		ON CONFLICT (store_id, product_id, variant_id) DO UPDATE SET is_visible = FALSE, updated_at = NOW()`, storeID, productID, variantID)
	return err
}

// ListHiddenVariantIDs returns the variant IDs explicitly hidden in a
// store for one product (is_visible=false rows only).
func (r *StoreRepository) ListHiddenVariantIDs(ctx context.Context, storeID, productID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT variant_id FROM store_variant_visibility WHERE store_id=$1 AND product_id=$2 AND is_visible = FALSE ORDER BY variant_id`, storeID, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetStoreVariantDefault returns the store's default variant for one
// product, or "" when no row exists (fall back to product default).
func (r *StoreRepository) GetStoreVariantDefault(ctx context.Context, storeID, productID string) (string, error) {
	var vid string
	err := r.db.QueryRowContext(ctx, `SELECT variant_id FROM store_variant_defaults WHERE store_id=$1 AND product_id=$2`, storeID, productID).Scan(&vid)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return vid, err
}

// SetStoreVariantDefault upserts the single default variant for a
// (store, product) pair (PK guarantees exactly one). Caller must have
// verified ownership, assortment membership, and that the variant
// belongs to the product, is active, and is visible in the store.
func (r *StoreRepository) SetStoreVariantDefault(ctx context.Context, storeID, productID, variantID string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO store_variant_defaults (store_id, product_id, variant_id) VALUES ($1,$2,$3)
		ON CONFLICT (store_id, product_id) DO UPDATE SET variant_id = EXCLUDED.variant_id, updated_at = NOW()`, storeID, productID, variantID)
	return err
}

// ClearStoreVariantDefault deletes the row: the store falls back to
// the product-level default.
func (r *StoreRepository) ClearStoreVariantDefault(ctx context.Context, storeID, productID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM store_variant_defaults WHERE store_id=$1 AND product_id=$2`, storeID, productID)
	return err
}

type SellerReport struct {
	Revenue       int64 `json:"revenue"`
	Profit        int64 `json:"profit"`
	Orders        int   `json:"orders"`
	Units         int   `json:"units"`
	PendingProfit int64 `json:"pendingProfit"`
}

func (r *StoreRepository) SellerReport(ctx context.Context, ownerID, slug, start, end string) (*SellerReport, error) {
	store, err := r.OwnedStore(ctx, ownerID, slug)
	if err != nil {
		return nil, err
	}
	var result SellerReport
	err = r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(payable_amount),0),COALESCE(SUM(dropshipper_profit),0),COUNT(*),COALESCE(SUM((SELECT SUM(quantity) FROM order_items WHERE order_id=o.id)),0) FROM orders o WHERE store_id=$1 AND payment_status='paid' AND status<>'cancelled' AND ($2='' OR created_at>=NULLIF($2,'')::date) AND ($3='' OR created_at<NULLIF($3,'')::date+INTERVAL '1 day')`, store.ID, start, end).Scan(&result.Revenue, &result.Profit, &result.Orders, &result.Units)
	if err != nil {
		return &result, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(dropshipper_profit),0) FROM orders WHERE store_id=$1 AND profit_released=false AND status<>'cancelled'`, store.ID).Scan(&result.PendingProfit)
	return &result, err
}

type SellerTicket struct {
	ID        string    `json:"id"`
	Subject   string    `json:"subject"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	Messages  []string  `json:"messages"`
}

func (r *StoreRepository) SellerTickets(ctx context.Context, userID string) ([]SellerTicket, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT t.id,t.subject,t.status,t.created_at,COALESCE((SELECT ARRAY_AGG(body ORDER BY created_at) FROM support_ticket_messages WHERE ticket_id=t.id AND NOT is_internal),'{}') FROM support_tickets t WHERE requester_user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SellerTicket{}
	for rows.Next() {
		var item SellerTicket
		var msgs pq.StringArray
		if err := rows.Scan(&item.ID, &item.Subject, &item.Status, &item.CreatedAt, &msgs); err != nil {
			return nil, err
		}
		item.Messages = []string(msgs)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (r *StoreRepository) CreateSellerTicket(ctx context.Context, userID, subject, message string) error {
	if strings.TrimSpace(subject) == "" || len(subject) > 200 || strings.TrimSpace(message) == "" || len(message) > 10000 {
		return ErrStoreInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := "ticket-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO support_tickets(id,requester_user_id,requester_role,subject) VALUES($1,$2,'seller',$3)`, id, userID, subject); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO support_ticket_messages(ticket_id,author_user_id,body) VALUES($1,$2,$3)`, id, userID, message); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *StoreRepository) RequestStoreChange(ctx context.Context, userID, slug, kind, value string) error {
	store, err := r.OwnedStore(ctx, userID, slug)
	if err != nil {
		return err
	}
	if kind != "store_name" && kind != "store_slug" && kind != "custom_domain" {
		return ErrStoreInput
	}
	if strings.TrimSpace(value) == "" || len(value) > 150 {
		return ErrStoreInput
	}
	raw, _ := json.Marshal(map[string]string{"value": value, "storeId": store.ID})
	_, err = r.db.ExecContext(ctx, `INSERT INTO pending_approvals(id,entity_type,target_id,title,requested_data,requested_by,status) VALUES($1,$2,$3,$4,$5,$6,'pending')`, "req-"+uuid.NewString(), kind, store.ID, fmt.Sprintf("%s: %s", kind, value), raw, userID)
	return err
}

// --- Team 05 (Phase 2, live parity): seller discount campaigns (D48) ---
//
// MANAGEMENT ONLY: these methods store, list, update and delete
// campaigns. They are deliberately never called from any price path:
// campaign price application is unproven in live (U04), so no
// campaign query exists in pricing or order code in this tree.

const team05CampaignColumns = `id, store_id, name, scope, target_id, mode, value, starts_at, ends_at, is_active, created_at, updated_at`

func team05ValidateCampaign(name, scope, mode string, value float64, starts, ends time.Time) error {
	if strings.TrimSpace(name) == "" {
		return ErrStoreInput
	}
	if scope != "product" && scope != "variant" && scope != "niche" && scope != "store" {
		return ErrStoreInput
	}
	if mode != "percent" && mode != "fixed" {
		return ErrStoreInput
	}
	if value < 0 {
		return ErrStoreInput
	}
	if !ends.After(starts) {
		return ErrStoreInput
	}
	return nil
}

func team05ScanCampaignRow(scan func(dest ...interface{}) error) (domain.SellerDiscountCampaign, error) {
	var c domain.SellerDiscountCampaign
	err := scan(&c.ID, &c.StoreID, &c.Name, &c.Scope, &c.TargetID, &c.Mode, &c.Value, &c.StartsAt, &c.EndsAt, &c.IsActive, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// ListStoreDiscountCampaigns returns a store's campaigns newest first,
// matching the live list order (ORDER BY created_at DESC).
func (r *StoreRepository) ListStoreDiscountCampaigns(ctx context.Context, storeID string) ([]domain.SellerDiscountCampaign, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+team05CampaignColumns+` FROM seller_discount_campaigns WHERE store_id = $1 ORDER BY created_at DESC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SellerDiscountCampaign{}
	for rows.Next() {
		c, err := team05ScanCampaignRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetStoreDiscountCampaign returns one campaign scoped to its store.
func (r *StoreRepository) GetStoreDiscountCampaign(ctx context.Context, storeID, id string) (*domain.SellerDiscountCampaign, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+team05CampaignColumns+` FROM seller_discount_campaigns WHERE id = $1 AND store_id = $2`, id, storeID)
	c, err := team05ScanCampaignRow(row.Scan)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// DiscountApplicationLocked mirrors the admin margin lock check in
// GetActiveDiscountCampaign (discount_repo.go): the legacy boolean
// stores.is_margin_locked_by_admin, or any store_margin_locks row
// scoped to this product or one of its niches. Store scope is not
// consulted, exactly as in the campaign check: the legacy boolean
// covers the store-wide case. On a lock-query error the error is
// returned so the caller can fail safe (no discount) rather than
// apply a discount past a lock it could not read.
func (r *StoreRepository) DiscountApplicationLocked(ctx context.Context, storeID, productID string, nicheIDs []string, variantID string) (bool, error) {
	var marginLocked bool
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(is_margin_locked_by_admin, FALSE) FROM stores WHERE id=$1`, storeID).Scan(&marginLocked); err != nil {
		return false, err
	}
	if marginLocked {
		return true, nil
	}
	lockQuery := `SELECT COUNT(*) FROM store_margin_locks WHERE store_id=$1 AND ((scope='product' AND scope_id=$2) OR (scope='niche' AND scope_id = ANY($3))`
	lockArgs := []interface{}{storeID, productID, pq.Array(nicheIDs)}
	if variantID != "" {
		lockQuery += ` OR (scope='variant' AND scope_id=$4)`
		lockArgs = append(lockArgs, variantID)
	}
	lockQuery += `)`
	var lockedCount int
	if err := r.db.QueryRowContext(ctx, lockQuery, lockArgs...).Scan(&lockedCount); err != nil {
		return false, err
	}
	return lockedCount > 0, nil
}

// CreateStoreDiscountCampaign validates, inserts and re-reads the row
// so the caller gets the database timestamps.
func (r *StoreRepository) CreateStoreDiscountCampaign(ctx context.Context, c domain.SellerDiscountCampaign) (*domain.SellerDiscountCampaign, error) {
	if err := team05ValidateCampaign(c.Name, c.Scope, c.Mode, c.Value, c.StartsAt, c.EndsAt); err != nil {
		return nil, err
	}
	// Store-scope campaigns resolve with target_id='' (see
	// GetActiveDiscountCampaign), so normalise a verbatim non-empty
	// target here: otherwise a store-scope campaign could never match
	// and would silently never apply.
	if c.Scope == "store" {
		c.TargetID = ""
	}
	if c.Scope == "variant" && strings.TrimSpace(c.TargetID) == "" {
		return nil, ErrStoreInput
	}
	if c.Scope == "variant" && strings.TrimSpace(c.TargetID) == "" {
		return nil, ErrStoreInput
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO seller_discount_campaigns (id, store_id, name, scope, target_id, mode, value, starts_at, ends_at, is_active) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, c.ID, c.StoreID, c.Name, c.Scope, c.TargetID, c.Mode, c.Value, c.StartsAt, c.EndsAt, c.IsActive); err != nil {
		return nil, err
	}
	return r.GetStoreDiscountCampaign(ctx, c.StoreID, c.ID)
}

// UpdateStoreDiscountCampaign applies a partial patch to a campaign
// scoped to its store. It returns found=false when no such campaign
// exists for the store. The patched row is fully revalidated, so a
// patch can never produce an invalid window, scope or mode.
func (r *StoreRepository) UpdateStoreDiscountCampaign(ctx context.Context, storeID, id string, p domain.SellerDiscountCampaignPatch) (bool, error) {
	current, err := r.GetStoreDiscountCampaign(ctx, storeID, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if p.Name != nil {
		current.Name = *p.Name
	}
	if p.Scope != nil {
		current.Scope = *p.Scope
	}
	if p.TargetID != nil {
		current.TargetID = *p.TargetID
	}
	if p.Mode != nil {
		current.Mode = *p.Mode
	}
	if p.Value != nil {
		current.Value = *p.Value
	}
	if p.StartsAt != nil {
		current.StartsAt = *p.StartsAt
	}
	if p.EndsAt != nil {
		current.EndsAt = *p.EndsAt
	}
	if p.IsActive != nil {
		current.IsActive = *p.IsActive
	}
	if err := team05ValidateCampaign(current.Name, current.Scope, current.Mode, current.Value, current.StartsAt, current.EndsAt); err != nil {
		return false, err
	}
	// Normalise on every update, not just scope changes: a patch
	// to any other field must also repair a store-scope row previously
	// stored with a non-empty target_id, or it would stay unmatchable.
	if current.Scope == "store" {
		current.TargetID = ""
	}
	_, err = r.db.ExecContext(ctx, `UPDATE seller_discount_campaigns SET name = $1, scope = $2, target_id = $3, mode = $4, value = $5, starts_at = $6, ends_at = $7, is_active = $8, updated_at = NOW() WHERE id = $9 AND store_id = $10`, current.Name, current.Scope, current.TargetID, current.Mode, current.Value, current.StartsAt, current.EndsAt, current.IsActive, id, storeID)
	if err != nil {
		return false, err
	}
	return true, nil
}

// DeleteStoreDiscountCampaign deletes by id AND store_id, matching
// the live delete pattern. It returns found=false when nothing was
// deleted.
func (r *StoreRepository) DeleteStoreDiscountCampaign(ctx context.Context, storeID, id string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM seller_discount_campaigns WHERE id = $1 AND store_id = $2`, id, storeID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// --- Team 05 (Phase 2, live parity): seller subscribers (D47) ---
//
// ListStoreSubscribers reads newsletter_subscribers for one store,
// newest first, optionally filtered by status. The unsubscribe_token
// column is never selected. A missing table on an older schema
// yields an empty list rather than an error, so the seller
// dashboard does not break before the migration lands.
func (r *StoreRepository) ListStoreSubscribers(ctx context.Context, storeID, status string, limit int) ([]domain.StoreSubscriber, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, store_id, mobile, COALESCE(email, ''), status, source, created_at, unsubscribed_at FROM newsletter_subscribers WHERE store_id = $1`
	args := []interface{}{storeID}
	if strings.TrimSpace(status) != "" {
		q += ` AND status = $2`
		args = append(args, strings.TrimSpace(status))
	}
	q += ` ORDER BY created_at DESC LIMIT ` + fmt.Sprintf("%d", limit)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "42P01" {
			return []domain.StoreSubscriber{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	out := []domain.StoreSubscriber{}
	for rows.Next() {
		var s domain.StoreSubscriber
		var unsub sql.NullTime
		if err := rows.Scan(&s.ID, &s.StoreID, &s.Mobile, &s.Email, &s.Status, &s.Source, &s.CreatedAt, &unsub); err != nil {
			return nil, err
		}
		if unsub.Valid {
			t := unsub.Time
			s.UnsubscribedAt = &t
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
