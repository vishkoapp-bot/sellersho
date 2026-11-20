#!/usr/bin/env python3
"""MIG-02 step 1 — build backend/migrations/classification.json.

Source data:
  * class per file: the MIG-02 audit Appendix A (verified: 210 files,
    counts EXPAND 95 / BACKFILL 53 / CONTRACT 61 / DESTRUCTIVE 1).
  * CONTRACT verdicts: hand verification (see CONTRACT_VERDICTS below).
    CHECK replacements were compared old-set vs new-set mechanically
    (scripts in /tmp/mig02 are scratch; the comparison inputs were the
    named CHECK in the file, or the unnamed CHECK in the CREATE TABLE
    that PostgreSQL auto-named <table>_<column>_check), then each file
    was read for the DROP TRIGGER / UNIQUE / FK / RENAME / SET NOT NULL
    cases. Verdict vocabulary:
      compatible  — old binary unaffected (widening, new table, new
                    defaulted/nullable column, drop+recreate in-file).
      tightening  — an old-binary write can fail, but only when it
                    produces a value the new constraint rejects
                    (duplicate key, out-of-range value, narrowed CHECK
                    value). Normal traffic is unaffected; the failure
                    is fail-closed. Does NOT move the rollback floor.
      breaking    — an old binary's normal write path fails
                    structurally (arbiter removed, column renamed,
                    trigger side-effect the old code depends on
                    removed). Moves the rollback floor to the
                    introducing commit.
  * BACKFILL runtimes: measured 2026-11-20 on a disposable empty
    PostgreSQL 16 database (migrate binary, full 210-file run, 26.3s
    total). Empty-schema numbers are a floor, not a prod estimate;
    the timeout budget stays the D-5 default unless a file declares
    otherwise (none does on this evidence).
  * post_conditions: exact SQL run by deploy_remote.py AFTER `migrate`
    succeeds, but ONLY for migrations this deploy applied. Each returns
    one number compared to `expected` (`eq` default, `ge` where noted).

Regenerate the .md with scripts/release/generate_classification_md.py.
"""
import hashlib
import json
import re
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
MIG = REPO / "backend/migrations"
AUDIT = Path("/home/hatch/workspace/mig02-audit/MIG-02-audit.md")
TIMING_LOG = Path("/tmp/mig02/migrate-timing.log")
OUT = MIG / "classification.json"

# ---------------------------------------------------------------- classes
audit = AUDIT.read_text()
CLASSES = {}
for m in re.finditer(r"\| `([^`]+\.up\.sql)` \| (EXPAND|BACKFILL|CONTRACT|DESTRUCTIVE) \| ([^|]*) \| (yes|NO) \|", audit):
    CLASSES[m.group(1)] = {"class": m.group(2), "audit_flags": m.group(3).strip(), "down": m.group(4) == "yes"}

# ---------------------------------------------------------------- runtimes
RUNTIME_MS = {}
for line in TIMING_LOG.read_text().splitlines():
    m = re.search(r"migration (\S+\.up\.sql) applied in (\S+)", line)
    if not m:
        continue
    raw = m.group(2)
    if raw.endswith("ms"):
        RUNTIME_MS[m.group(1)] = round(float(raw[:-2]), 3)
    elif raw.endswith("s"):
        RUNTIME_MS[m.group(1)] = round(float(raw[:-1]) * 1000, 3)

# ------------------------------------------------------- contract verdicts
# (verdict, old_binary_write, notes)
C = {
"000006_security_payments_support.up.sql": ("tightening",
  "User registration with a username that differs only by case from an existing username (users_username_unique, partial, LOWER(username)); the payment_transactions index is on a table created in this same file.",
  "CHECKs on the new tables are new; payment_transactions_one_active_per_order is on a new table. The users index is the only pre-existing-table tightening."),
"000008_warehouse_admin_finance_lifecycle.up.sql": ("compatible",
  "No old-binary write exists that the new sets reject: all four CHECK replacements are widenings (users.role +finance; inbound_shipments.status +pending/+receiving/+completed; orders.status +picking/+picked/+packed; payout_requests.status +processing/+completed) against the unnamed CHECKs in 000001/000003.",
  "Prior definitions were the unnamed CHECKs PostgreSQL auto-named <table>_<column>_check. ADD COLUMN NOT NULL columns are defaulted."),
"000011_direct_dropship.up.sql": ("compatible",
  "No old-binary write exists: the unique index is on supplier_shipments, a table created in this file.",
  "Partial unique on tracking_number WHERE NOT NULL, new table."),
"000019_seller_campaigns.up.sql": ("compatible",
  "No old-binary write exists: all three unique indexes are on seller_campaigns / seller_campaign_events, tables created in this file.",
  "Partial uniques (widget-per-store, click token, event dedupe)."),
"000020_seller_campaign_history.up.sql": ("compatible",
  "No old-binary write exists: seller_campaign_history_trigger is dropped and recreated in the same file (DROP TRIGGER IF EXISTS + CREATE TRIGGER); the trigger's side effect is unchanged.",
  "Drop+recreate in-file, not a removal."),
"000023_supplier_returns_warranty_capacity.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: pending_approvals.entity_type widens +warranty_property (prior unnamed CHECK in 000001); the two unique indexes are on tables created in this file.",
  "Widening verified against 000001's unnamed entity_type CHECK."),
"000024_inventory_returns_carriers_dimensions.up.sql": ("tightening",
  "Supplier-return insert with a duplicate non-NULL idempotency_key (uq_supplier_return_idempotency on pre-existing supplier_return_requests). The supplier_return_requests.status CHECK widens (+packed/+in_transit/+received_pending_qc/+qc_failed).",
  "Idempotency index is the tightening part; the CHECK replacement is a widening."),
"000025_audit_warehouse_capacity.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: audit_logs.category widens +warehouse_capacity (prior unnamed CHECK in 000005).",
  "Widening verified against 000005's unnamed category CHECK."),
"202610130002_super_admin_password.up.sql": ("tightening",
  "Second users row with username '09933070870' (users_super_admin_username_unique, partial WHERE username = '09933070870').",
  "Single-row predicate: only the super-admin username is constrained."),
"202610140001_super_admin_identity.up.sql": ("tightening",
  "Second users row with username '09933070870' (same partial unique index, re-created after the 202610130002 file).",
  "Same index as 202610130002; the UPDATE/INSERT in this file seeds the super-admin row itself."),
"202610160003_ledger_append_only.up.sql": ("compatible",
  "No old-binary write exists: trg_ledger_entries_append_only is dropped and recreated in the same file; the append-only guard stays in force.",
  "Drop+recreate in-file, not a removal. The ledger stays append-only for every binary."),
"202610160006_sku_returns.up.sql": ("compatible",
  "No old-binary write exists: both dropped triggers (trg_products_assign_spn, trg_variants_resync_product_stock) are recreated in the same file; products.spn is a NEW column (old code writes NULL, and UNIQUE admits multiple NULLs).",
  "Drop+recreate in-file. The SPN backfill is a DO loop assigning random unique values (idempotent by re-check)."),
"202610170001_inventory_unify.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: orders.payment_status widens +refund_pending (prior unnamed CHECK in 000006: pending/paid/failed/cancelled). trg_inventory_balances_sync_stock is dropped and recreated in-file.",
  "The file header states the widening explicitly (paid-order cancellation was failing on the old CHECK)."),
"202610170002_order_idempotency.up.sql": ("tightening",
  "Order insert with a duplicate non-NULL idempotency_key (orders_idempotency_key_uidx, partial).",
  "Pre-migration enforcement was app-level only; a duplicate the old code would have inserted as a second row now fails fail-closed."),
"202610170003_store_signature_code.up.sql": ("tightening",
  "Store insert/update with a duplicate non-NULL contract_signature_code (stores_contract_signature_code_uidx, partial).",
  "Same shape as 202610170002."),
"202610170009_drop_legacy_variant_resync.up.sql": ("breaking",
  "Any product_variants INSERT/UPDATE/DELETE by the old binary: the trigger that kept products.stock = SUM(variant stock) is GONE (DROP TRIGGER + DROP FUNCTION, not recreated). The old binary's variant writes no longer resync products.stock, so the stock projection goes stale silently.",
  "Behavioural break, not a write failure. Introducing commit is the Nov 4 snapshot 2df713b (pre-floor): it does NOT move the floor past ff57367. The resync side effect moved into application code (syncProductStockTx)."),
"202610180001_add_product_barcode.up.sql": ("tightening",
  "Product insert/update with a duplicate non-empty barcode (idx_products_barcode_unique, partial WHERE barcode IS NOT NULL AND TRIM(barcode) <> '').",
  "Empty/NULL barcodes are excluded by the partial predicate."),
"202610190002_payout_honesty.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: payout_requests.status widens +paid/+transfer_confirmed (prior named CHECK in 000008). payout_method is a new defaulted NOT NULL column.",
  "Widening verified against 000008's named CHECK."),
"202610190005_customer_return_cancel.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: customer_returns.status widens +cancelled (prior unnamed CHECK in 202610160006).",
  "Widening verified against 202610160006's unnamed status CHECK."),
"202610200002_customer_wallet_engine.up.sql": ("tightening",
  "Wallet debit that would make balance < locked_balance (customer_wallets_balance_covers_locked), or an order payment split with wallet_amount + gateway_amount > payable_amount (orders_wallet_gateway_within_payable). Both are NEW numeric CHECKs (DROP CONSTRAINT IF EXISTS + ADD: the constraints did not exist before this file).",
  "New invariants the new code maintains. If pre-migration data violated them the migration itself would have failed; post-migration, only an old binary issuing such a write fails."),
"202610200006_pending_approvals_entity_types.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: pending_approvals.entity_type widens +product_gallery_update/+product_variant_create/+product_variant_create_bulk (prior named CHECK in 000023).",
  "Widening verified against 000023's named CHECK."),
"202610210001_widen_audit_logs_category_check.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: audit_logs.category widens +approvals/+settings/+staff/+stores/+suppliers/+users (prior named CHECK in 000025).",
  "Widening verified against 000025's named CHECK. The filename states the intent."),
"202610220001_min_max_cart_qty_on_variants.up.sql": ("compatible",
  "No old-binary write exists: min_cart_qty / max_cart_qty are NEW defaulted columns added in this file and the CHECKs constrain only those columns (min >= 1; max NULL or >= 1). Old code never writes them.",
  "DROP CONSTRAINT IF EXISTS + ADD of constraints that did not exist before; new columns, new CHECKs."),
"202610230001_add_newsletter_audit_category.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: audit_logs.category widens +newsletter (prior named CHECK in 202610210001).",
  "Widening verified against 202610210001's named CHECK."),
"202610230005_complementary_products.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: pending_approvals.entity_type widens +product_addon (prior named CHECK in 202610200006); the unique index is on product_addon_attachments, a table created in this file.",
  "Widening verified against 202610200006's named CHECK."),
"202610230006_user_addresses.up.sql": ("compatible",
  "No old-binary write exists: the unique index is on user_addresses, a table created in this file.",
  "Partial unique (one default address per user) on a new table."),
"202610250005_rename_ssm_supplier_id.up.sql": ("breaking",
  "ANY statement by the old binary referencing supplier_shipping_methods.supplier_id: the column is renamed to owner_user_id, so the old name fails at SQL level. The DELETE in this file removes only orphan rows that resolve to no users row (data-impact note below).",
  "Introducing commit is the Nov 4 snapshot 2df713b (pre-floor): it does NOT move the floor past ff57367. The rename is the clearest contract-stage example in the set."),
"202610250006_refund_status.up.sql": ("compatible",
  "No old-binary write exists that the new sets reject: orders.payment_status widens +refunded (prior ANY-form CHECK in 202610170001) and payment_transactions.status widens +refunded (prior unnamed CHECK in 000006: pending/paid/failed).",
  "The file header states the widening explicitly (MarkPaymentRefunded was failing on both old CHECKs)."),
"202610250008_ledger_truncate_guard.up.sql": ("compatible",
  "No old-binary write exists: trg_ledger_entries_no_truncate is dropped and recreated in the same file.",
  "Drop+recreate in-file. The TRUNCATE guard stays in force for every binary."),
"202610250101_withdrawal_idempotency.up.sql": ("tightening",
  "Withdrawal insert with a duplicate (customer_id, non-NULL idempotency_key) (idx_customer_withdrawals_idempotency_key, partial).",
  "Same shape as 202610170002."),
"202610250301_supplier_customer_returns.up.sql": ("compatible",
  "No old-binary write exists: the unique index is on supplier_return_back_requests, a table created in this file.",
  "Partial unique on active return-back rows, new table."),
"202610260002_warehouse_city_confirmed.up.sql": ("compatible",
  "No old-binary write exists: the CHECK constrains warehouse_city_source, a NEW nullable column added in this file (old code writes NULL, which the IS NULL OR ... predicate admits). warehouse_city_confirmed is a new defaulted NOT NULL column.",
  "The file header states no city value changes: existing rows get confirmed=FALSE, source='default'."),
"202610280001_supplier_warehouses_origin.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: inbound_shipments.status widens +cancelled/+scheduled (prior named CHECK in 000008).",
  "Widening verified against 000008's named CHECK."),
"202610280004_variant_supplier_base_cost.up.sql": ("compatible",
  "No old-binary write exists: supplier_base_cost is a NEW nullable column on product_variants and the CHECK is NULL-tolerant (IS NULL OR > 0); old code writes NULL.",
  "File header: NULL means 'inherit from products.supplier_base_cost'."),
"202610280005_category_hierarchy.up.sql": ("tightening",
  "Category insert/update with parent_id equal to its own id (categories_no_self_parent). depth/sort_order are new defaulted NOT NULL columns (old code never writes them) and depth BETWEEN 0 AND 5 constrains only the new column.",
  "The only pre-existing-column tightening is the pathological self-parent write; no evidence old code ever wrote one."),
"202610280006_product_url_numbers.up.sql": ("compatible",
  "No old-binary write exists: spn_number / spnc_number are NEW sequence-backed columns; an old-binary insert without those columns gets its numbers from the sequence defaults (products.spn_number SET NOT NULL after the backfill; product_variants.spnc_number stays NULL-able by design and gets SET DEFAULT nextval). The backfill is documented idempotent (re-derives the same numbers on retry).",
  "SET NOT NULL data-impact note: products.spn_number is fully backfilled in-file (UPDATE ... FROM numbered sequences) before its SET NOT NULL; product_variants.spnc_number is deliberately left nullable (file line 70 comment) with a sequence default instead."),
"202610280009_tariff_return_pricing.up.sql": ("compatible",
  "No old-binary write exists: all three unique indexes are on tapin_tariff_versions / return_price_reviews, tables created in this file.",
  "Partial uniques (one active tariff, one pending/accepted review per return+group)."),
"202610310001_add_sold_to_inventory_balances_status_check.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: inventory_balances.status widens +sold (prior unnamed CHECK in 000024).",
  "Widening verified against 000024's unnamed status CHECK. The file header documents the live-check caveat (staging vs live); the CHECK itself is a pure widening."),
"202610310004_product_reviews_legacy_shape.up.sql": ("tightening",
  "Review insert with a duplicate (product_id, user_id) (product_reviews_product_user_unique). The SET NOT NULLs (user_id, comment) are safe: the file backfills user_id from customer_id and re-keys duplicate (product_id, user_id) rows with a '#0004-superseded-' suffix before adding the UNIQUE.",
  "SET NOT NULL data-impact note: user_id is backfilled from customer_id (recoverable provenance); duplicate rows are re-keyed, not deleted, but the re-keying mutates derived data — disclosed here. The UNIQUE is the tightening part."),
"202610310007_platform_settings_return_window_days.up.sql": ("compatible",
  "No old-binary write exists: return_window_days is a NEW defaulted NOT NULL column on platform_settings and the 1..365 CHECK constrains only it.",
  "New column, new CHECK."),
"202610310011_variant_cost.up.sql": ("compatible",
  "No old-binary write exists: same new nullable column + NULL-tolerant CHECK as 202610280004, added idempotently (ADD COLUMN IF NOT EXISTS + constraint only if absent).",
  "Deploy-set copy of 202610280004; applying it after that file is a no-op."),
"202611040010_customer_return_shipped_status.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: customer_returns.status widens +shipped (prior named CHECK in 202610190005).",
  "Widening verified against 202610190005's named CHECK."),
"202611040011_ledger_warehouse_target_type.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: ledger_entries.target_type widens +warehouse (prior unnamed CHECK in 000001: seller/supplier/carrier/platform).",
  "Widening verified against 000001's unnamed target_type CHECK."),
"202611050002_wishlist_per_store.up.sql": ("compatible",
  "No old-binary write exists that breaks: store_id is a NEW NOT NULL column DEFAULT 'store-central', and the primary key widens from (user_id, product_id) to (user_id, store_id, product_id). An old-binary wishlist insert (no store_id) lands in 'store-central' exactly as before.",
  "Per-store wishlist doctrine: existing rows were all placed through the central surface, so the default attributes them correctly; no heart is lost or moved."),
"202611060001_campaign_scope_variant.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: seller_discount_campaigns.scope widens +variant (prior unnamed CHECK in 202610230004: product/niche/store).",
  "Widening verified against 202610230004's unnamed scope CHECK."),
"202611090001_operations_automation.up.sql": ("compatible",
  "No old-binary write exists: both SET NOT NULLs are preceded in-file by a backfill UPDATE and a SET DEFAULT (qc_discrepancies.response_due_at = created_at + 48h; orders.payment_expires_at = GREATEST(created_at + 15m, NOW() + 15m)).",
  "SET NOT NULL data-impact note: this is the model for how a NOT NULL must be staged (backfill + default first). The hardcoded deploy assertion on this migration is replaced by the manifest check (MIG-02 step 2)."),
"202611100001_supplier_inbound_states.up.sql": ("compatible",
  "No old-binary write exists: the CHECK is re-added with the SAME allowed set as the prior named CHECK in 202610280001 (scheduled/in_transit/pending/receiving/received/putaway_completed/qc_rejected/completed/cancelled). Semantically a no-op re-add.",
  "Same-set replacement verified against 202610280001."),
"202611110002_product_model_v2.up.sql": ("tightening",
  "Variant insert with a duplicate (product_id, non-NULL combination_key) (product_variants_combination_unique, partial). The ADD CHECKs constrain only new defaulted columns (product_type, schema_version, selection, inventory_mode, is_active).",
  "Tightening is the combination unique only; the new-column CHECKs are compatible."),
"202611110003_sku_inventory.up.sql": ("breaking",
  "EVERY inventory-balance upsert by the old binary: INSERT INTO inventory_balances ... ON CONFLICT (product_id, owner_type, location_type, location_key, status) DO UPDATE — that arbiter was exactly the dropped legacy unique constraint. PostgreSQL requires the ON CONFLICT column list to match a unique index; after this migration there is no such index (the replacements are partial and need WHERE variant_id IS NULL / IS NOT NULL, which the old code does not supply). Inbound receiving, putaway, returns and warehouse transfers all fail, while /api/health still returns 200.",
  "PROVEN breaker (audit §4.4, verified against ff57367^ code: supplier_repo.go:1676,1859 and warehouse_repo.go). Introducing commit ff57367 (2026-11-11) is the rollback floor (D-2). This is the newest breaking migration in the set."),
"202611130002_variant_sale_price.up.sql": ("compatible",
  "No old-binary write exists: the CHECKs are NULL-tolerant (sale_price IS NULL OR > 0; sale window NULL-tolerant) on the new sale columns; old code writes NULL.",
  "New columns, NULL-tolerant CHECKs."),
"202611140002_inventory_origin_identity.up.sql": ("tightening",
  "DELETE FROM warehouses for a warehouse that still has inventory_balances rows: the re-added FK is ON DELETE RESTRICT, replacing the prior ON DELETE SET NULL (auto-named FK from 202610240001, which added warehouse_id REFERENCES warehouses(id) ON DELETE SET NULL). The old binary's warehouse delete would have nulled the balances; it now fails instead. The new FK columns (supplier_warehouse_id, warehouse_id) are nullable, so old inserts (NULL) pass.",
  "Audit §4.3 open check, now verified: prior action was SET NULL (202610240001). Warehouse deletes are admin-rare; the tightening is real but narrow. Flagged for the reviewer: confirm no admin flow deletes a warehouse with live balances."),
"202611140004_finance_adjustment_identity.up.sql": ("tightening",
  "Finance-adjustment insert with a duplicate (target_kind, target_id, non-NULL request_key), or supplier-return financial-event insert with a duplicate non-NULL event_identity (partial uniques on pre-existing tables).",
  "Idempotency enforcement: the old code generated request_key/event_identity to dedupe; a true duplicate now fails fail-closed instead of inserting a second row."),
"202611140005_carton_business_idempotency.up.sql": ("tightening",
  "Shipment insert with a duplicate (supplier_id, non-NULL creation_key), or shipment_items insert with a duplicate (shipment_id, variant_id) (partial uniques on pre-existing shipments / shipment_items).",
  "Same idempotency shape as 202611140004."),
"202611140006_inventory_transfer_provenance.up.sql": ("tightening",
  "Inventory-balance write with status='sold': the rebuilt inventory_balances.status CHECK adds 'inbound_reserved' but DROPS 'sold' (prior named CHECK in 202610310001 allowed sold). The shipments.status CHECK widens (+in_transit/+cancelled) and the new unique index is on inbound_inventory_lines, a table created in this file.",
  "NARROWING on 'sold' — the one CHECK replacement in the set that removes a value. Floor impact: NONE on current evidence — no writer of inventory_balances.status='sold' exists in the current tree (the only 'sold' literals in Go code are inventory_movements from/to_status, which this CHECK does not constrain); the writer named in 202610310001's header (decrementCanonicalSaleTx in inventory_sync.go) no longer exists in any post-snapshot commit. The binaries that could have written 'sold' balances predate the ff57367 floor. FLOOR-REVIEW FLAG: if a reviewer finds a deployed binary between 202610310001 and 202611140006 that wrote 'sold' balances, this becomes breaking and the floor moves — record the verdict, do not silently reclassify."),
"202611140008_inbound_qc_disposition.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: inventory_balances.status widens +qc_accepted (prior named CHECK in 202611140006).",
  "Widening verified against 202611140006's named CHECK (which is the post-narrowing set)."),
"202611150002_customer_return_bin_entry.up.sql": ("compatible",
  "No old-binary write exists: warehouse_placement_insert is dropped and recreated in the same file; the ADD CHECK constrains source_kind, a new column; the INSERTs in the file are inside the recreated trigger function body, not direct writes.",
  "Drop+recreate in-file."),
"202611150004_order_refund_principal.up.sql": ("compatible",
  "No old-binary write exists: payment_transactions_one_active_per_order is dropped and recreated with the same predicate PLUS 'NOT compensation_capture' (audit §4.3 verified: no Go code references the index by name; old inserts carry compensation_capture=FALSE by default, so they are covered exactly as before). The other two unique indexes are on tables created in this file.",
  "DROP INDEX + CREATE UNIQUE INDEX, same name, widened predicate (a narrowing of the index's coverage in the safe direction: compensation rows are now exempt)."),
"202611150006_monthly_settlement_progress.up.sql": ("tightening",
  "Settlement-effect insert with a duplicate (cycle_period, store_id) (monthly_settlement_cycle_store_effect UNIQUE on pre-existing monthly_settlement_items). The status CHECK widens +running/+blocked (prior: 'completed' only).",
  "The file carries a fail-closed guard that raises on duplicate/invalid settlement effects — evidence the duplicate write was a live hazard. Forward-repair note (audit §4.3): a rollback after a cycle entered 'running' leaves a cycle the old binary has no code to resume — needs a documented reconcile step, not a code change."),
"202611160001_parcel_charge_identity.up.sql": ("tightening",
  "Second 'original' supplier-return financial-event row for the same parcel_id (supplier_parcel_original_charge, partial WHERE charge_component='original', on the pre-existing table). The file UPDATEs existing rows before creating the index. The parcel_charge_original_receipt index is on parcel_charge_receipts, a table created in this file.",
  "Tightening is the financial-events index only."),
"202611180004_obs01_slo_alert_disk_band.up.sql": ("compatible",
  "No old-binary write exists that the new set rejects: observability_alert_state.code widens +slo_breach (prior unnamed CHECK in 202611140003). severity_band is a new defaulted NOT NULL column.",
  "Widening verified against 202611140003's unnamed code CHECK. Prod precedent: retained after the OBS-01 rollback, old binary booted fine."),
"202611190003_customer_withdrawal_intent.up.sql": ("compatible",
  "No old-binary write exists: the partial unique index is on request_key_sha256, a NEW nullable column; old inserts leave it NULL and the partial predicate (WHERE request_key_sha256 IS NOT NULL) excludes them.",
  "QUAL-01 withdrawal-intent idempotency; the probe in step 4 exercises this index."),
}

# ------------------------------------------------- backfill post-conditions
# Hand-derived exact post-conditions (UPDATE/DELETE/INSERT-SELECT files).
# Each entry: list of {"sql", "expected", "comparator"?}.
PC_OVERRIDE = {
"000002_seed_data.up.sql": [
  {"sql": "SELECT count(*) FROM stores WHERE (id = 'store-sepehr' AND referrer_seller_id IS DISTINCT FROM 'store-bibi') OR (id = 'store-parsian' AND referrer_seller_id IS DISTINCT FROM 'store-sepehr')", "expected": 0},
  {"sql": "SELECT count(*) FROM platform_settings WHERE id = 1", "expected": 1, "comparator": "ge"},
],
"202610160003_remediate_dev_seed_credentials.up.sql": [
  {"sql": "SELECT count(*) FROM users WHERE password_hash IN ('$2a$10$boGlIBfo9lgFx7cwl0xKs.wnyHJe3St98d2feLRKC1bmYbFrFIfWK', '$2a$10$p6zuh4wx1l.S1.z3aScOaexWy1nQaaXIA8vbdH5HUA5Ilp1.G')", "expected": 0},
  {"sql": "SELECT count(*) FROM stores WHERE id IN ('store-central', 'store-bibi', 'store-sepehr', 'store-parsian') AND total_sales IN (184500000, 44100000, 52000000, 38000000)", "expected": 0},
],
"202610180002_backfill_store_owner_phone.up.sql": [
  {"sql": "SELECT count(*) FROM stores s JOIN users u ON s.owner_user_id = u.id WHERE (s.owner_phone IS NULL OR TRIM(s.owner_phone) = '') AND u.mobile IS NOT NULL AND TRIM(u.mobile) <> ''", "expected": 0},
],
"202610190003_supplier_approval.up.sql": [
  {"sql": "SELECT count(*) FROM suppliers WHERE approval_status IS NULL OR approval_status = ''", "expected": 0},
],
"202610200001_supplier_signed_contract_version.up.sql": [
  {"sql": "SELECT count(*) FROM suppliers WHERE signed_contract_version < 2", "expected": 0},
],
"202610200003_product_brand_and_reviews.up.sql": [
  {"sql": "SELECT count(*) FROM products WHERE COALESCE(TRIM(specs->>'برند'), '') <> '' AND brand = ''", "expected": 0},
],
"202610200007_homepage_identity_waveb.up.sql": [
  {"sql": "SELECT count(*) FROM homepage_sections WHERE section_type = 'suppliers_showcase' AND is_active", "expected": 0},
],
"202610250009_inventory_captured.up.sql": [
  {"sql": "SELECT count(*) FROM orders WHERE inventory_captured = FALSE AND ((wallet_amount = payable_amount AND wallet_amount > 0) OR EXISTS (SELECT 1 FROM payment_transactions pt WHERE pt.order_id = orders.id))", "expected": 0},
],
"202610250010_remove_stale_seed_putaway.up.sql": [
  {"sql": "SELECT count(*) FROM putaway_capacity_reservations WHERE task_id = 'PUT-INIT-001' AND status = 'active'", "expected": 0},
  {"sql": "SELECT count(*) FROM putaway_tasks WHERE id = 'PUT-INIT-001' AND status IN ('pending', 'assigned')", "expected": 0},
],
"202610250013_returns_shipping_nonrefundable.up.sql": [
  {"sql": "SELECT count(*) FROM static_pages WHERE key = 'terms' AND content NOT LIKE '%هزینه ارسال غیرقابل‌استرداد است%'", "expected": 0, "note": "only when a 'terms' page row exists; the deploy runner skips a post-condition whose table has no matching seed row only if the query itself returns 0 rows for a missing key — this query returns 0 when no 'terms' row exists, so it is safe on both shapes"},
],
"202611100002_fashion_editorial_redesign.up.sql": [
  {"sql": "SELECT count(*) FROM store_templates WHERE id = 'tpl-fashion-1' AND description <> 'قالب روشن و مینیمال مد؛ بنر دو‌بخشی، مجموعه‌های تصویری و چیدمان ریسپانسیو'", "expected": 0},
],
"000018_backfill_store_products.up.sql": [
  {"sql": "SELECT count(*) FROM stores s CROSS JOIN products p WHERE s.owner_type = 'dropshipper' AND p.supplier_base_cost > 0 AND NOT EXISTS (SELECT 1 FROM store_products sp WHERE sp.store_id = s.id AND sp.product_id = p.id)", "expected": 0},
],
"202611170001_coupon_reservations.up.sql": [
  {"sql": "SELECT count(*) FROM seller_discount_codes c WHERE c.used_count <> (SELECT count(*) FROM order_coupon_reservations r WHERE r.coupon_id = c.id AND r.state <> 'released')", "expected": 0},
],
# SET NOT NULL files (CONTRACT class, data-impact first).
"202610280006_product_url_numbers.up.sql": [
  {"sql": "SELECT count(*) FROM products WHERE spn_number IS NULL", "expected": 0},
  # NOTE: product_variants.spnc_number stays NULL-able BY DESIGN (file
  # line 70 comment) — no NULL post-condition is valid for it. The
  # backfill completeness for variants is covered by the UNIQUE
  # constraint (DDL-enforced) plus the sequence default.
],
"202610310004_product_reviews_legacy_shape.up.sql": [
  {"sql": "SELECT count(*) FROM product_reviews WHERE user_id IS NULL OR comment IS NULL", "expected": 0},
  {"sql": "SELECT count(*) FROM (SELECT product_id, user_id FROM product_reviews GROUP BY product_id, user_id HAVING count(*) > 1) d", "expected": 0},
],
"202611090001_operations_automation.up.sql": [
  {"sql": "SELECT count(*) FROM qc_discrepancies WHERE response_due_at IS NULL", "expected": 0},
],
}

# Files whose backfill is INSERT...SELECT (no exact row-count derivable
# from the file alone) and whose post-condition is a reviewer TODO.
PC_TODO = {
"202611140010_sku_measurement_revision.up.sql": "INSERT...SELECT into inventory_measurement_revisions plus an ADD COLUMN NOT NULL (NULL-check emitted); the SELECT-side completeness check needs the revision-source rule from a reviewer.",
}

def strip(sql):
    sql = re.sub(r"/\*.*?\*/", " ", sql, flags=re.S)
    return "\n".join(ln.split("--", 1)[0] if "--" in ln else ln for ln in sql.splitlines())

def _split_top_commas(body):
    parts, depth, start = [], 0, 0
    for i, ch in enumerate(body):
        if ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
        elif ch == "," and depth == 0:
            parts.append(body[start:i])
            start = i + 1
    parts.append(body[start:])
    return parts

def add_not_null_conditions(sql, conditions):
    # Split each ALTER TABLE statement into top-level comma chunks so a
    # NOT NULL on a later column in the same statement cannot leak onto
    # an earlier nullable column (that bug produced false post-conditions
    # for products.length_mm, categories.parent_id and
    # product_variants.measurement_revision_id).
    for stmt in re.finditer(r"ALTER\s+TABLE\s+(?:ONLY\s+)?\"?([a-z_][a-z0-9_]*)\"?\s+(.*?);", sql, flags=re.I | re.S):
        table, body = stmt.group(1), stmt.group(2)
        for chunk in _split_top_commas(body):
            m = re.match(r"\s*ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?\"?([\w]+)\"?\s+(.*)", chunk, flags=re.I | re.S)
            if m and re.search(r"\bNOT\s+NULL\b", m.group(2), flags=re.I):
                conditions.append({"sql": f"SELECT count(*) FROM {table} WHERE {m.group(1)} IS NULL", "expected": 0})

def insert_seed_conditions(sql, conditions, name):
    # Plain VALUES-form INSERTs only (no SELECT, no NOT EXISTS guard):
    # right after this migration applies, each target table holds at
    # least the tuples the file inserted.
    if re.search(r"INSERT\s+INTO\s+\w+[^;]*?SELECT", sql, flags=re.S | re.I):
        return
    for m in re.finditer(r"INSERT\s+INTO\s+\"?([a-z_][a-z0-9_]*)\"?\s*(\([^)]*\))?\s*VALUES\s*(.*?)(?:ON\s+CONFLICT|;)", sql, flags=re.I | re.S):
        table, collist, values = m.group(1), m.group(2) or "", m.group(3)
        # split top-level tuples on '),(' boundaries (balanced parens)
        tuples, depth, start = [], 0, 0
        for i, ch in enumerate(values):
            if ch == "(":
                if depth == 0:
                    start = i
                depth += 1
            elif ch == ")":
                depth -= 1
                if depth == 0:
                    tuples.append(values[start:i + 1])
        if not tuples:
            continue
        ids = []
        if collist.strip().lower().startswith("(id"):
            for t in tuples:
                lit = re.search(r"\(\s*'([^']*)'", t)
                if lit:
                    ids.append(lit.group(1))
        if ids:
            in_list = ", ".join("'" + i.replace("'", "''") + "'" for i in ids)
            conditions.append({"sql": f"SELECT count(*) FROM {table} WHERE id IN ({in_list})", "expected": len(ids), "comparator": "ge"})
        else:
            conditions.append({"sql": f"SELECT count(*) FROM {table}", "expected": len(tuples), "comparator": "ge",
                               "note": "table-count floor: file inserts this many VALUES tuples into a table whose key column could not be identified from the column list"})

def build():
    files = sorted(p.name for p in MIG.glob("*.up.sql"))
    assert len(files) == 210, len(files)
    assert set(files) == set(CLASSES), "audit table / files mismatch"
    entries = []
    for name in files:
        sql_bytes = (MIG / name).read_bytes()
        sql_raw = sql_bytes.decode("utf-8")
        sql = strip(sql_raw)
        cls = CLASSES[name]["class"]
        digest_ops = []
        for pat, label in [
            (r"DROP\s+TABLE\s+(\S+)", "DROP TABLE"),
            (r"DROP\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?(\S+)", "DROP CONSTRAINT"),
            (r"DROP\s+INDEX\s+(?:IF\s+EXISTS\s+)?(\S+)", "DROP INDEX"),
            (r"DROP\s+TRIGGER\s+(?:IF\s+EXISTS\s+)?(\S+)", "DROP TRIGGER"),
            (r"DROP\s+FUNCTION\s+(?:IF\s+EXISTS\s+)?([^\s(]+)", "DROP FUNCTION"),
            (r"RENAME\s+COLUMN\s+(\S+)\s+TO\s+(\S+)", "RENAME COLUMN"),
            (r"SET\s+NOT\s+NULL", "SET NOT NULL"),
            (r"CREATE\s+UNIQUE\s+INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(\S+)\s+ON\s+(\S+)", "CREATE UNIQUE INDEX"),
            (r"ADD\s+CONSTRAINT\s+(\S+)\s+UNIQUE", "ADD CONSTRAINT UNIQUE"),
            (r"ADD\s+CONSTRAINT\s+(\S+)\s+FOREIGN\s+KEY", "ADD CONSTRAINT FK"),
            (r"ADD\s+CONSTRAINT\s+(\S+)\s+CHECK", "ADD CONSTRAINT CHECK"),
            (r"ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?(\S+)[^,;]*NOT\s+NULL", "ADD COLUMN NOT NULL"),
            (r"\bUPDATE\s+(\S+)", "UPDATE"),
            (r"\bINSERT\s+INTO\s+(\S+)", "INSERT INTO"),
            (r"\bDELETE\s+FROM\s+(\S+)", "DELETE FROM"),
            (r"CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\S+)", "CREATE TABLE"),
        ]:
            for m in re.finditer(pat, sql, flags=re.I):
                digest_ops.append(f"{label}: {' '.join(g for g in m.groups() if g)}")
        tables = sorted(set(re.findall(r"(?:FROM|INTO|TABLE|UPDATE)\s+(?:IF\s+NOT\s+EXISTS\s+)?\"?([a-z_][a-z0-9_]*)\"?", sql, flags=re.I)))
        entry = {
            "name": name,
            "class": cls,
            "sha256": hashlib.sha256(sql_bytes).hexdigest(),
            "has_down_sql": (MIG / (name[:-7] + ".down.sql")).exists(),
            "tables": tables,
            "operations": sorted(set(digest_ops)),
            "runtime_ms_disposable": RUNTIME_MS.get(name),
            "chunked": False,
            "timeout_lock_seconds": None,
            "timeout_statement_seconds": None,
            "post_conditions": [],
            "post_condition_status": "none",
        }
        if cls == "CONTRACT":
            verdict, old_write, notes = C[name]
            entry["verdict"] = verdict
            entry["old_binary_write"] = old_write
            entry["notes"] = notes
            entry["moves_floor"] = verdict == "breaking"
        elif cls == "DESTRUCTIVE":
            entry["verdict"] = "breaking"
            entry["old_binary_write"] = "No old-binary write exists: no Go code at 92f9cca references supplier_pricing_tiers (0 hits outside the migrations themselves); the engine was removed from backend and frontend before this migration."
            entry["notes"] = ("DROP TABLE supplier_pricing_tiers. 0-row evidence: the migration header states 'The table held 0 rows at removal time' "
                              "(2026-11-03 user decision to remove tiered pricing). A live prod row count was NOT verifiable from this VM (no prod access); "
                              "the claim rests on the migration author's note plus the complete absence of code references. Never used by code: verified by grep over backend/ at 92f9cca.")
            entry["moves_floor"] = False
            entry["notes"] += " Floor note: a DROP TABLE is breaking in principle, but with 0 rows and 0 code references there is no live binary that can observe it; the floor stays ff57367."
        elif cls == "BACKFILL":
            entry["verdict"] = "compatible"
            entry["old_binary_write"] = "Backfill only: the old binary's writes are unaffected provided the backfill completes (a failed backfill is a deploy failure, not a compat failure). ADD COLUMN NOT NULL columns are defaulted or backfilled in-file."
            entry["notes"] = "Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower."
        else:  # EXPAND
            entry["verdict"] = "compatible"
            entry["old_binary_write"] = "No old-binary write exists that this migration affects: additive only (new tables/columns nullable or defaulted, plain indexes, triggers/functions on new objects)."
            entry["notes"] = ""
        # data-DELETE notes (both live in BACKFILL except the rename file, CONTRACT)
        if name == "202610250005_rename_ssm_supplier_id.up.sql":
            entry["notes"] += (" DATA-DELETE NOTE: DELETE FROM supplier_shipping_methods removes only orphan rows that resolve to no users row "
                               "(NOT EXISTS users WHERE u.id::text = owner_user_id). No accepted order/payment data is deleted; the rows could never be joined to a supplier or an admin decision. "
                               "Post-condition below proves zero orphans remain.")
            entry["post_conditions"] = [{"sql": "SELECT count(*) FROM supplier_shipping_methods ssm WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id::text = ssm.owner_user_id)", "expected": 0}]
            entry["post_condition_status"] = "verified"
        if name == "202610250010_remove_stale_seed_putaway.up.sql":
            entry["notes"] += (" DATA-DELETE NOTE: deletes only the stale seed putaway task PUT-INIT-001 (and its capacity reservation), status-guarded "
                               "(task pending/assigned, reservation active). It is seed data from 000008, not accepted business data; the guard makes it a no-op on any DB where the task legitimately completed.")
        # post-conditions
        conditions = list(PC_OVERRIDE.get(name, []))
        add_not_null_conditions(sql, conditions)
        if cls == "BACKFILL":
            insert_seed_conditions(sql, conditions, name)
        if name in PC_TODO:
            entry["post_condition_status"] = "todo"
            entry["post_condition_note"] = PC_TODO[name]
        if conditions:
            # de-duplicate identical SQL
            seen, uniq = set(), []
            for pc in conditions:
                if pc["sql"] not in seen:
                    seen.add(pc["sql"])
                    uniq.append(pc)
            entry["post_conditions"] = uniq
            if entry["post_condition_status"] != "todo":
                entry["post_condition_status"] = "verified"
        entries.append(entry)
    doc = {
        "schema_version": 1,
        "generated_by": "scripts/release/build_classification.py (MIG-02 step 1). Regenerate: python3 scripts/release/build_classification.py, then python3 scripts/release/generate_classification_md.py.",
        "repo_commit": "92f9cca",
        "counts": {"EXPAND": 95, "BACKFILL": 53, "CONTRACT": 61, "DESTRUCTIVE": 1, "total": 210},
        "verdicts": {"compatible": sum(1 for e in entries if e["verdict"] == "compatible"),
                     "tightening": sum(1 for e in entries if e["verdict"] == "tightening"),
                     "breaking": sum(1 for e in entries if e["verdict"] == "breaking")},
        "breaking_files": [e["name"] for e in entries if e["verdict"] == "breaking"],
        "floor_note": ("The rollback floor is the newest commit introducing a 'breaking' migration: ff57367 (2026-11-11, 202611110003_sku_inventory). "
                       "The other two breaking files (202610170009, 202610250005) resolve to the Nov 4 snapshot commit 2df713b, which is older. "
                       "The DESTRUCTIVE file does not move the floor (0 rows, 0 code references). "
                       "The 202611140006 'sold' narrowing is 'tightening', not 'breaking' — see its notes and the FLOOR-REVIEW FLAG; a reviewer finding a live 'sold'-balances writer moves the floor."),
        "runtime_note": ("runtime_ms_disposable: per-file wall time of the 210-file run on a disposable empty PostgreSQL 16 database, 2026-11-20 "
                         "(26.3s total; slowest single file 202611200002_perf02_bounding_indexes 562ms). Empty-schema numbers are a floor, not a prod estimate."),
        "timeout_note": ("timeout_lock_seconds / timeout_statement_seconds are null for all 210 files: no file needs more than the D-5 defaults "
                         "(lock 30s / statement 10min) on the measured evidence. A migration author declares a larger budget here; make_manifest.py "
                         "copies non-null budgets into the release manifest and deploy_remote.py passes them to `migrate`."),
        "entries": entries,
    }
    OUT.write_text(json.dumps(doc, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"wrote {OUT}: {len(entries)} entries")
    print("counts:", doc["counts"], "verdicts:", doc["verdicts"])
    print("breaking:", doc["breaking_files"])
    no_runtime = [e["name"] for e in entries if e["runtime_ms_disposable"] is None]
    print("missing runtimes:", no_runtime)
    no_pc_backfill = [e["name"] for e in entries if e["class"] == "BACKFILL" and not e["post_conditions"]]
    print("backfill files without post-conditions:", len(no_pc_backfill), no_pc_backfill)
    todo = [e["name"] for e in entries if e["post_condition_status"] == "todo"]
    print("todo post-conditions:", todo)

if __name__ == "__main__":
    build()
