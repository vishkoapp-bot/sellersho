# Migration classification — all 210 migrations

> Generated from `backend/migrations/classification.json` — the JSON is the
> source of truth. Regenerate with
> `python3 scripts/release/generate_classification_md.py` after editing the
> JSON (via `scripts/release/build_classification.py`). Do not hand-edit
> this file. `validate_release.py` fails a release when any `*.up.sql`
> file is missing from the classification.

Repo commit classified: `92f9cca`. Counts: EXPAND 97 / BACKFILL 54 / CONTRACT 62 / DESTRUCTIVE 1 (total 214). Verdicts: compatible 192 / tightening 18 / breaking 4.

## Verdict vocabulary

- **compatible** — the old binary is unaffected (widening, new table,
  new defaulted/nullable column, drop+recreate in-file).
- **tightening** — an old-binary write can fail, but only when it produces
  a value the new constraint rejects (duplicate key, out-of-range value,
  narrowed CHECK value). Normal traffic is unaffected; the failure is
  fail-closed. A tightening does NOT move the rollback floor.
- **breaking** — an old binary's normal write path fails structurally
  (arbiter removed, column renamed, trigger side-effect the old code
  depends on removed). Moves the rollback floor to the introducing commit.

## Rollback floor

The rollback floor is the newest commit introducing a 'breaking' migration: ff57367 (2026-11-11, 202611110003_sku_inventory). The other two breaking files (202610170009, 202610250005) resolve to the Nov 4 snapshot commit 2df713b, which is older. The DESTRUCTIVE file does not move the floor (0 rows, 0 code references). The 202611140006 'sold' narrowing is 'tightening', not 'breaking' — see its notes and the FLOOR-REVIEW FLAG; a reviewer finding a live 'sold'-balances writer moves the floor.

Breaking files: `202610170009_drop_legacy_variant_resync.up.sql`, `202610250005_rename_ssm_supplier_id.up.sql`, `202611030001_remove_pricing_tiers.up.sql`, `202611110003_sku_inventory.up.sql`.

## Runtimes and timeouts

runtime_ms_disposable: per-file wall time of the 210-file run on a disposable empty PostgreSQL 16 database, 2026-11-20 (26.3s total; slowest single file 202611200002_perf02_bounding_indexes 562ms). Empty-schema numbers are a floor, not a prod estimate.

timeout_lock_seconds / timeout_statement_seconds are null for all 210 files: no file needs more than the D-5 defaults (lock 30s / statement 10min) on the measured evidence. A migration author declares a larger budget here; make_manifest.py copies non-null budgets into the release manifest and deploy_remote.py passes them to `migrate`.

## Contract files (61) — per-file verdicts

Each entry names the old-binary write that could violate the change, or
states that none exists. CHECK replacements were compared old-set vs
new-set (named CHECK in the file, or the unnamed CHECK in the CREATE TABLE
that PostgreSQL auto-named `<table>_<column>_check`).

### `000006_security_payments_support.up.sql` — tightening

Old-binary write: User registration with a username that differs only by case from an existing username (users_username_unique, partial, LOWER(username)); the payment_transactions index is on a table created in this same file.

CHECKs on the new tables are new; payment_transactions_one_active_per_order is on a new table. The users index is the only pre-existing-table tightening.

Post-conditions:

- `SELECT count(*) FROM users WHERE failed_login_attempts IS NULL` → eq 0
- `SELECT count(*) FROM orders WHERE payment_status IS NULL` → eq 0

### `000008_warehouse_admin_finance_lifecycle.up.sql` — compatible

Old-binary write: No old-binary write exists that the new sets reject: all four CHECK replacements are widenings (users.role +finance; inbound_shipments.status +pending/+receiving/+completed; orders.status +picking/+picked/+packed; payout_requests.status +processing/+completed) against the unnamed CHECKs in 000001/000003.

Prior definitions were the unnamed CHECKs PostgreSQL auto-named <table>_<column>_check. ADD COLUMN NOT NULL columns are defaulted.

Post-conditions:

- `SELECT count(*) FROM platform_settings WHERE supplier_fee_overrides IS NULL` → eq 0
- `SELECT count(*) FROM stores WHERE status IS NULL` → eq 0

### `000011_direct_dropship.up.sql` — compatible

Old-binary write: No old-binary write exists: the unique index is on supplier_shipments, a table created in this file.

Partial unique on tracking_number WHERE NOT NULL, new table.

### `000019_seller_campaigns.up.sql` — compatible

Old-binary write: No old-binary write exists: all three unique indexes are on seller_campaigns / seller_campaign_events, tables created in this file.

Partial uniques (widget-per-store, click token, event dedupe).

### `000020_seller_campaign_history.up.sql` — compatible

Old-binary write: No old-binary write exists: seller_campaign_history_trigger is dropped and recreated in the same file (DROP TRIGGER IF EXISTS + CREATE TRIGGER); the trigger's side effect is unchanged.

Drop+recreate in-file, not a removal.

### `000023_supplier_returns_warranty_capacity.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: pending_approvals.entity_type widens +warranty_property (prior unnamed CHECK in 000001); the two unique indexes are on tables created in this file.

Widening verified against 000001's unnamed entity_type CHECK.

### `000024_inventory_returns_carriers_dimensions.up.sql` — tightening

Old-binary write: Supplier-return insert with a duplicate non-NULL idempotency_key (uq_supplier_return_idempotency on pre-existing supplier_return_requests). The supplier_return_requests.status CHECK widens (+packed/+in_transit/+received_pending_qc/+qc_failed).

Idempotency index is the tightening part; the CHECK replacement is a widening.

Post-conditions:

- `SELECT count(*) FROM products WHERE measurement_status IS NULL` → eq 0
- `SELECT count(*) FROM products WHERE measurement_version IS NULL` → eq 0
- `SELECT count(*) FROM warehouse_bins WHERE occupied_volume_cm3 IS NULL` → eq 0
- `SELECT count(*) FROM warehouse_bins WHERE occupied_weight_grams IS NULL` → eq 0
- `SELECT count(*) FROM warehouse_bins WHERE capacity_status IS NULL` → eq 0
- `SELECT count(*) FROM supplier_return_requests WHERE accepted_quantity IS NULL` → eq 0
- `SELECT count(*) FROM supplier_return_requests WHERE failed_quantity IS NULL` → eq 0
- `SELECT count(*) FROM supplier_return_requests WHERE updated_at IS NULL` → eq 0

### `000025_audit_warehouse_capacity.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: audit_logs.category widens +warehouse_capacity (prior unnamed CHECK in 000005).

Widening verified against 000005's unnamed category CHECK.

### `202610130002_super_admin_password.up.sql` — tightening

Old-binary write: Second users row with username '09933070870' (users_super_admin_username_unique, partial WHERE username = '09933070870').

Single-row predicate: only the super-admin username is constrained.

### `202610140001_super_admin_identity.up.sql` — tightening

Old-binary write: Second users row with username '09933070870' (same partial unique index, re-created after the 202610130002 file).

Same index as 202610130002; the UPDATE/INSERT in this file seeds the super-admin row itself.

### `202610160003_ledger_append_only.up.sql` — compatible

Old-binary write: No old-binary write exists: trg_ledger_entries_append_only is dropped and recreated in the same file; the append-only guard stays in force.

Drop+recreate in-file, not a removal. The ledger stays append-only for every binary.

### `202610160006_sku_returns.up.sql` — compatible

Old-binary write: No old-binary write exists: both dropped triggers (trg_products_assign_spn, trg_variants_resync_product_stock) are recreated in the same file; products.spn is a NEW column (old code writes NULL, and UNIQUE admits multiple NULLs).

Drop+recreate in-file. The SPN backfill is a DO loop assigning random unique values (idempotent by re-check).

### `202610170001_inventory_unify.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: orders.payment_status widens +refund_pending (prior unnamed CHECK in 000006: pending/paid/failed/cancelled). trg_inventory_balances_sync_stock is dropped and recreated in-file.

The file header states the widening explicitly (paid-order cancellation was failing on the old CHECK).

### `202610170002_order_idempotency.up.sql` — tightening

Old-binary write: Order insert with a duplicate non-NULL idempotency_key (orders_idempotency_key_uidx, partial).

Pre-migration enforcement was app-level only; a duplicate the old code would have inserted as a second row now fails fail-closed.

### `202610170003_store_signature_code.up.sql` — tightening

Old-binary write: Store insert/update with a duplicate non-NULL contract_signature_code (stores_contract_signature_code_uidx, partial).

Same shape as 202610170002.

### `202610170009_drop_legacy_variant_resync.up.sql` — breaking

Old-binary write: Any product_variants INSERT/UPDATE/DELETE by the old binary: the trigger that kept products.stock = SUM(variant stock) is GONE (DROP TRIGGER + DROP FUNCTION, not recreated). The old binary's variant writes no longer resync products.stock, so the stock projection goes stale silently.

Behavioural break, not a write failure. Introducing commit is the Nov 4 snapshot 2df713b (pre-floor): it does NOT move the floor past ff57367. The resync side effect moved into application code (syncProductStockTx).

### `202610180001_add_product_barcode.up.sql` — tightening

Old-binary write: Product insert/update with a duplicate non-empty barcode (idx_products_barcode_unique, partial WHERE barcode IS NOT NULL AND TRIM(barcode) <> '').

Empty/NULL barcodes are excluded by the partial predicate.

### `202610190002_payout_honesty.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: payout_requests.status widens +paid/+transfer_confirmed (prior named CHECK in 000008). payout_method is a new defaulted NOT NULL column.

Widening verified against 000008's named CHECK.

Post-conditions:

- `SELECT count(*) FROM payout_requests WHERE payout_method IS NULL` → eq 0

### `202610190005_customer_return_cancel.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: customer_returns.status widens +cancelled (prior unnamed CHECK in 202610160006).

Widening verified against 202610160006's unnamed status CHECK.

### `202610200002_customer_wallet_engine.up.sql` — tightening

Old-binary write: Wallet debit that would make balance < locked_balance (customer_wallets_balance_covers_locked), or an order payment split with wallet_amount + gateway_amount > payable_amount (orders_wallet_gateway_within_payable). Both are NEW numeric CHECKs (DROP CONSTRAINT IF EXISTS + ADD: the constraints did not exist before this file).

New invariants the new code maintains. If pre-migration data violated them the migration itself would have failed; post-migration, only an old binary issuing such a write fails.

Post-conditions:

- `SELECT count(*) FROM customer_wallets WHERE locked_balance IS NULL` → eq 0
- `SELECT count(*) FROM orders WHERE wallet_amount IS NULL` → eq 0
- `SELECT count(*) FROM orders WHERE gateway_amount IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE wallet_spend_enabled IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE wallet_withdraw_enabled IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE wallet_min_withdrawal IS NULL` → eq 0

### `202610200006_pending_approvals_entity_types.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: pending_approvals.entity_type widens +product_gallery_update/+product_variant_create/+product_variant_create_bulk (prior named CHECK in 000023).

Widening verified against 000023's named CHECK.

### `202610210001_widen_audit_logs_category_check.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: audit_logs.category widens +approvals/+settings/+staff/+stores/+suppliers/+users (prior named CHECK in 000025).

Widening verified against 000025's named CHECK. The filename states the intent.

### `202610220001_min_max_cart_qty_on_variants.up.sql` — compatible

Old-binary write: No old-binary write exists: min_cart_qty / max_cart_qty are NEW defaulted columns added in this file and the CHECKs constrain only those columns (min >= 1; max NULL or >= 1). Old code never writes them.

DROP CONSTRAINT IF EXISTS + ADD of constraints that did not exist before; new columns, new CHECKs.

Post-conditions:

- `SELECT count(*) FROM product_variants WHERE min_cart_qty IS NULL` → eq 0

### `202610230001_add_newsletter_audit_category.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: audit_logs.category widens +newsletter (prior named CHECK in 202610210001).

Widening verified against 202610210001's named CHECK.

### `202610230005_complementary_products.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: pending_approvals.entity_type widens +product_addon (prior named CHECK in 202610200006); the unique index is on product_addon_attachments, a table created in this file.

Widening verified against 202610200006's named CHECK.

Post-conditions:

- `SELECT count(*) FROM orders WHERE platform_addon_amount IS NULL` → eq 0
- `SELECT count(*) FROM orders WHERE supplier_addon_amount IS NULL` → eq 0

### `202610230006_user_addresses.up.sql` — compatible

Old-binary write: No old-binary write exists: the unique index is on user_addresses, a table created in this file.

Partial unique (one default address per user) on a new table.

### `202610250005_rename_ssm_supplier_id.up.sql` — breaking

Old-binary write: ANY statement by the old binary referencing supplier_shipping_methods.supplier_id: the column is renamed to owner_user_id, so the old name fails at SQL level. The DELETE in this file removes only orphan rows that resolve to no users row (data-impact note below).

Introducing commit is the Nov 4 snapshot 2df713b (pre-floor): it does NOT move the floor past ff57367. The rename is the clearest contract-stage example in the set. DATA-DELETE NOTE: DELETE FROM supplier_shipping_methods removes only orphan rows that resolve to no users row (NOT EXISTS users WHERE u.id::text = owner_user_id). No accepted order/payment data is deleted; the rows could never be joined to a supplier or an admin decision. Post-condition below proves zero orphans remain.

Post-conditions:

- `SELECT count(*) FROM supplier_shipping_methods ssm WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id::text = ssm.owner_user_id)` → eq 0

### `202610250006_refund_status.up.sql` — compatible

Old-binary write: No old-binary write exists that the new sets reject: orders.payment_status widens +refunded (prior ANY-form CHECK in 202610170001) and payment_transactions.status widens +refunded (prior unnamed CHECK in 000006: pending/paid/failed).

The file header states the widening explicitly (MarkPaymentRefunded was failing on both old CHECKs).

### `202610250008_ledger_truncate_guard.up.sql` — compatible

Old-binary write: No old-binary write exists: trg_ledger_entries_no_truncate is dropped and recreated in the same file.

Drop+recreate in-file. The TRUNCATE guard stays in force for every binary.

### `202610250101_withdrawal_idempotency.up.sql` — tightening

Old-binary write: Withdrawal insert with a duplicate (customer_id, non-NULL idempotency_key) (idx_customer_withdrawals_idempotency_key, partial).

Same shape as 202610170002.

### `202610250301_supplier_customer_returns.up.sql` — compatible

Old-binary write: No old-binary write exists: the unique index is on supplier_return_back_requests, a table created in this file.

Partial unique on active return-back rows, new table.

### `202610260002_warehouse_city_confirmed.up.sql` — compatible

Old-binary write: No old-binary write exists: the CHECK constrains warehouse_city_source, a NEW nullable column added in this file (old code writes NULL, which the IS NULL OR ... predicate admits). warehouse_city_confirmed is a new defaulted NOT NULL column.

The file header states no city value changes: existing rows get confirmed=FALSE, source='default'.

Post-conditions:

- `SELECT count(*) FROM suppliers WHERE warehouse_city_confirmed IS NULL` → eq 0

### `202610280001_supplier_warehouses_origin.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: inbound_shipments.status widens +cancelled/+scheduled (prior named CHECK in 000008).

Widening verified against 000008's named CHECK.

### `202610280004_variant_supplier_base_cost.up.sql` — compatible

Old-binary write: No old-binary write exists: supplier_base_cost is a NEW nullable column on product_variants and the CHECK is NULL-tolerant (IS NULL OR > 0); old code writes NULL.

File header: NULL means 'inherit from products.supplier_base_cost'.

### `202610280005_category_hierarchy.up.sql` — tightening

Old-binary write: Category insert/update with parent_id equal to its own id (categories_no_self_parent). depth/sort_order are new defaulted NOT NULL columns (old code never writes them) and depth BETWEEN 0 AND 5 constrains only the new column.

The only pre-existing-column tightening is the pathological self-parent write; no evidence old code ever wrote one.

Post-conditions:

- `SELECT count(*) FROM categories WHERE depth IS NULL` → eq 0
- `SELECT count(*) FROM categories WHERE sort_order IS NULL` → eq 0

### `202610280006_product_url_numbers.up.sql` — compatible

Old-binary write: No old-binary write exists: spn_number / spnc_number are NEW sequence-backed columns; an old-binary insert without those columns gets its numbers from the sequence defaults (products.spn_number SET NOT NULL after the backfill; product_variants.spnc_number stays NULL-able by design and gets SET DEFAULT nextval). The backfill is documented idempotent (re-derives the same numbers on retry).

SET NOT NULL data-impact note: products.spn_number is fully backfilled in-file (UPDATE ... FROM numbered sequences) before its SET NOT NULL; product_variants.spnc_number is deliberately left nullable (file line 70 comment) with a sequence default instead.

Post-conditions:

- `SELECT count(*) FROM products WHERE spn_number IS NULL` → eq 0

### `202610280009_tariff_return_pricing.up.sql` — compatible

Old-binary write: No old-binary write exists: all three unique indexes are on tapin_tariff_versions / return_price_reviews, tables created in this file.

Partial uniques (one active tariff, one pending/accepted review per return+group).

### `202610310001_add_sold_to_inventory_balances_status_check.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: inventory_balances.status widens +sold (prior unnamed CHECK in 000024).

Widening verified against 000024's unnamed status CHECK. The file header documents the live-check caveat (staging vs live); the CHECK itself is a pure widening.

### `202610310004_product_reviews_legacy_shape.up.sql` — tightening

Old-binary write: Review insert with a duplicate (product_id, user_id) (product_reviews_product_user_unique). The SET NOT NULLs (user_id, comment) are safe: the file backfills user_id from customer_id and re-keys duplicate (product_id, user_id) rows with a '#0004-superseded-' suffix before adding the UNIQUE.

SET NOT NULL data-impact note: user_id is backfilled from customer_id (recoverable provenance); duplicate rows are re-keyed, not deleted, but the re-keying mutates derived data — disclosed here. The UNIQUE is the tightening part.

Post-conditions:

- `SELECT count(*) FROM product_reviews WHERE user_id IS NULL OR comment IS NULL` → eq 0
- `SELECT count(*) FROM (SELECT product_id, user_id FROM product_reviews GROUP BY product_id, user_id HAVING count(*) > 1) d` → eq 0

### `202610310007_platform_settings_return_window_days.up.sql` — compatible

Old-binary write: No old-binary write exists: return_window_days is a NEW defaulted NOT NULL column on platform_settings and the 1..365 CHECK constrains only it.

New column, new CHECK.

Post-conditions:

- `SELECT count(*) FROM platform_settings WHERE return_window_days IS NULL` → eq 0

### `202610310011_variant_cost.up.sql` — compatible

Old-binary write: No old-binary write exists: same new nullable column + NULL-tolerant CHECK as 202610280004, added idempotently (ADD COLUMN IF NOT EXISTS + constraint only if absent).

Deploy-set copy of 202610280004; applying it after that file is a no-op.

### `202611040010_customer_return_shipped_status.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: customer_returns.status widens +shipped (prior named CHECK in 202610190005).

Widening verified against 202610190005's named CHECK.

### `202611040011_ledger_warehouse_target_type.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: ledger_entries.target_type widens +warehouse (prior unnamed CHECK in 000001: seller/supplier/carrier/platform).

Widening verified against 000001's unnamed target_type CHECK.

### `202611050002_wishlist_per_store.up.sql` — compatible

Old-binary write: No old-binary write exists that breaks: store_id is a NEW NOT NULL column DEFAULT 'store-central', and the primary key widens from (user_id, product_id) to (user_id, store_id, product_id). An old-binary wishlist insert (no store_id) lands in 'store-central' exactly as before.

Per-store wishlist doctrine: existing rows were all placed through the central surface, so the default attributes them correctly; no heart is lost or moved.

Post-conditions:

- `SELECT count(*) FROM customer_wishlists WHERE store_id IS NULL` → eq 0

### `202611060001_campaign_scope_variant.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: seller_discount_campaigns.scope widens +variant (prior unnamed CHECK in 202610230004: product/niche/store).

Widening verified against 202610230004's unnamed scope CHECK.

### `202611090001_operations_automation.up.sql` — compatible

Old-binary write: No old-binary write exists: both SET NOT NULLs are preceded in-file by a backfill UPDATE and a SET DEFAULT (qc_discrepancies.response_due_at = created_at + 48h; orders.payment_expires_at = GREATEST(created_at + 15m, NOW() + 15m)).

SET NOT NULL data-impact note: this is the model for how a NOT NULL must be staged (backfill + default first). The hardcoded deploy assertion on this migration is replaced by the manifest check (MIG-02 step 2).

Post-conditions:

- `SELECT count(*) FROM qc_discrepancies WHERE response_due_at IS NULL` → eq 0
- `SELECT count(*) FROM qc_discrepancies WHERE admin_resolution IS NULL` → eq 0

### `202611100001_supplier_inbound_states.up.sql` — compatible

Old-binary write: No old-binary write exists: the CHECK is re-added with the SAME allowed set as the prior named CHECK in 202610280001 (scheduled/in_transit/pending/receiving/received/putaway_completed/qc_rejected/completed/cancelled). Semantically a no-op re-add.

Same-set replacement verified against 202610280001.

### `202611110002_product_model_v2.up.sql` — tightening

Old-binary write: Variant insert with a duplicate (product_id, non-NULL combination_key) (product_variants_combination_unique, partial). The ADD CHECKs constrain only new defaulted columns (product_type, schema_version, selection, inventory_mode, is_active).

Tightening is the combination unique only; the new-column CHECKs are compatible.

Post-conditions:

- `SELECT count(*) FROM products WHERE product_type IS NULL` → eq 0
- `SELECT count(*) FROM products WHERE schema_version IS NULL` → eq 0
- `SELECT count(*) FROM product_variants WHERE selection IS NULL` → eq 0
- `SELECT count(*) FROM product_variants WHERE inventory_mode IS NULL` → eq 0
- `SELECT count(*) FROM product_variants WHERE is_active IS NULL` → eq 0

### `202611110003_sku_inventory.up.sql` — breaking

Old-binary write: EVERY inventory-balance upsert by the old binary: INSERT INTO inventory_balances ... ON CONFLICT (product_id, owner_type, location_type, location_key, status) DO UPDATE — that arbiter was exactly the dropped legacy unique constraint. PostgreSQL requires the ON CONFLICT column list to match a unique index; after this migration there is no such index (the replacements are partial and need WHERE variant_id IS NULL / IS NOT NULL, which the old code does not supply). Inbound receiving, putaway, returns and warehouse transfers all fail, while /api/health still returns 200.

PROVEN breaker (audit §4.4, verified against ff57367^ code: supplier_repo.go:1676,1859 and warehouse_repo.go). Introducing commit ff57367 (2026-11-11) is the rollback floor (D-2). This is the newest breaking migration in the set.

Post-conditions:

- `SELECT count(*) FROM shipment_items WHERE inventory_received_qty IS NULL` → eq 0

### `202611130002_variant_sale_price.up.sql` — compatible

Old-binary write: No old-binary write exists: the CHECKs are NULL-tolerant (sale_price IS NULL OR > 0; sale window NULL-tolerant) on the new sale columns; old code writes NULL.

New columns, NULL-tolerant CHECKs.

### `202611140002_inventory_origin_identity.up.sql` — tightening

Old-binary write: DELETE FROM warehouses for a warehouse that still has inventory_balances rows: the re-added FK is ON DELETE RESTRICT, replacing the prior ON DELETE SET NULL (auto-named FK from 202610240001, which added warehouse_id REFERENCES warehouses(id) ON DELETE SET NULL). The old binary's warehouse delete would have nulled the balances; it now fails instead. The new FK columns (supplier_warehouse_id, warehouse_id) are nullable, so old inserts (NULL) pass.

Audit §4.3 open check, now verified: prior action was SET NULL (202610240001). Warehouse deletes are admin-rare; the tightening is real but narrow. Flagged for the reviewer: confirm no admin flow deletes a warehouse with live balances.

### `202611140004_finance_adjustment_identity.up.sql` — tightening

Old-binary write: Finance-adjustment insert with a duplicate (target_kind, target_id, non-NULL request_key), or supplier-return financial-event insert with a duplicate non-NULL event_identity (partial uniques on pre-existing tables).

Idempotency enforcement: the old code generated request_key/event_identity to dedupe; a true duplicate now fails fail-closed instead of inserting a second row.

### `202611140005_carton_business_idempotency.up.sql` — tightening

Old-binary write: Shipment insert with a duplicate (supplier_id, non-NULL creation_key), or shipment_items insert with a duplicate (shipment_id, variant_id) (partial uniques on pre-existing shipments / shipment_items).

Same idempotency shape as 202611140004.

### `202611140006_inventory_transfer_provenance.up.sql` — tightening

Old-binary write: Inventory-balance write with status='sold': the rebuilt inventory_balances.status CHECK adds 'inbound_reserved' but DROPS 'sold' (prior named CHECK in 202610310001 allowed sold). The shipments.status CHECK widens (+in_transit/+cancelled) and the new unique index is on inbound_inventory_lines, a table created in this file.

NARROWING on 'sold' — the one CHECK replacement in the set that removes a value. Floor impact: NONE on current evidence — no writer of inventory_balances.status='sold' exists in the current tree (the only 'sold' literals in Go code are inventory_movements from/to_status, which this CHECK does not constrain); the writer named in 202610310001's header (decrementCanonicalSaleTx in inventory_sync.go) no longer exists in any post-snapshot commit. The binaries that could have written 'sold' balances predate the ff57367 floor. FLOOR-REVIEW FLAG: if a reviewer finds a deployed binary between 202610310001 and 202611140006 that wrote 'sold' balances, this becomes breaking and the floor moves — record the verdict, do not silently reclassify.

### `202611140008_inbound_qc_disposition.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: inventory_balances.status widens +qc_accepted (prior named CHECK in 202611140006).

Widening verified against 202611140006's named CHECK (which is the post-narrowing set).

### `202611150002_customer_return_bin_entry.up.sql` — compatible

Old-binary write: No old-binary write exists: warehouse_placement_insert is dropped and recreated in the same file; the ADD CHECK constrains source_kind, a new column; the INSERTs in the file are inside the recreated trigger function body, not direct writes.

Drop+recreate in-file.

Post-conditions:

- `SELECT count(*) FROM warehouse_placements WHERE source_kind IS NULL` → eq 0

### `202611150004_order_refund_principal.up.sql` — compatible

Old-binary write: No old-binary write exists: payment_transactions_one_active_per_order is dropped and recreated with the same predicate PLUS 'NOT compensation_capture' (audit §4.3 verified: no Go code references the index by name; old inserts carry compensation_capture=FALSE by default, so they are covered exactly as before). The other two unique indexes are on tables created in this file.

DROP INDEX + CREATE UNIQUE INDEX, same name, widened predicate (a narrowing of the index's coverage in the safe direction: compensation rows are now exempt).

Post-conditions:

- `SELECT count(*) FROM payment_transactions WHERE compensation_capture IS NULL` → eq 0

### `202611150006_monthly_settlement_progress.up.sql` — tightening

Old-binary write: Settlement-effect insert with a duplicate (cycle_period, store_id) (monthly_settlement_cycle_store_effect UNIQUE on pre-existing monthly_settlement_items). The status CHECK widens +running/+blocked (prior: 'completed' only).

The file carries a fail-closed guard that raises on duplicate/invalid settlement effects — evidence the duplicate write was a live hazard. Forward-repair note (audit §4.3): a rollback after a cycle entered 'running' leaves a cycle the old binary has no code to resume — needs a documented reconcile step, not a code change.

Post-conditions:

- `SELECT count(*) FROM monthly_settlement_cycles WHERE progress_initialized IS NULL` → eq 0

### `202611160001_parcel_charge_identity.up.sql` — tightening

Old-binary write: Second 'original' supplier-return financial-event row for the same parcel_id (supplier_parcel_original_charge, partial WHERE charge_component='original', on the pre-existing table). The file UPDATEs existing rows before creating the index. The parcel_charge_original_receipt index is on parcel_charge_receipts, a table created in this file.

Tightening is the financial-events index only.

### `202611180004_obs01_slo_alert_disk_band.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: observability_alert_state.code widens +slo_breach (prior unnamed CHECK in 202611140003). severity_band is a new defaulted NOT NULL column.

Widening verified against 202611140003's unnamed code CHECK. Prod precedent: retained after the OBS-01 rollback, old binary booted fine.

Post-conditions:

- `SELECT count(*) FROM observability_alert_state WHERE severity_band IS NULL` → eq 0

### `202611190003_customer_withdrawal_intent.up.sql` — compatible

Old-binary write: No old-binary write exists: the partial unique index is on request_key_sha256, a NEW nullable column; old inserts leave it NULL and the partial predicate (WHERE request_key_sha256 IS NOT NULL) excludes them.

QUAL-01 withdrawal-intent idempotency; the probe in step 4 exercises this index.

### `202611200004_fin013_alert_codes.up.sql` — compatible

Old-binary write: No old-binary write exists that the new set rejects: observability_alert_state.code widens +finance_invariant +finance_unknown_stale (prior CHECK in 202611180004). An old binary running against the new schema only ever writes the six old codes.

FIN-013: the two new in-app alert codes must be writable or DeliverAlerts fails on every tick (the loop upserts all eight codes) and no alert of any kind is delivered. Regression caught by the existing observability suite (5 tests). Runtime measured 2026-11-20 on a disposable DB: 35.485 ms drop + 32.602 ms re-add.

Post-conditions:

- `SELECT count(*) FROM pg_constraint WHERE conrelid='observability_alert_state'::regclass AND conname='observability_alert_state_code_check' AND pg_get_constraintdef(oid) LIKE '%finance_invariant%' AND pg_get_constraintdef(oid) LIKE '%finance_unknown_stale%'` → eq 1

## Backfill files (53)

Single full-table operation each (no chunking in any of the 210 files).
`runtime_ms_disposable` is the measured empty-schema disposable number.
Post-conditions run after `migrate` succeeds, only for migrations the
deploy applied; a failure fails the deploy.

### `000002_seed_data.up.sql` — 22.495 ms, tables: SET, categories, niches, platform_settings, product_niches, products, stores, suppliers

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM stores WHERE (id = 'store-sepehr' AND referrer_seller_id IS DISTINCT FROM 'store-bibi') OR (id = 'store-parsian' AND referrer_seller_id IS DISTINCT FROM 'store-sepehr')` → eq 0
- `SELECT count(*) FROM platform_settings WHERE id = 1` → ge 1
- `SELECT count(*) FROM platform_settings` → ge 1
- `SELECT count(*) FROM categories WHERE id IN ('cat-digital', 'cat-fashion', 'cat-beauty', 'cat-appliances', 'cat-gold')` → ge 5
- `SELECT count(*) FROM niches WHERE id IN ('niche-smartwatch', 'niche-powerbank', 'niche-headphone', 'niche-gold18', 'niche-leather', 'niche-home-appliances', 'niche-skincare', 'niche-books', 'niche-auto')` → ge 9
- `SELECT count(*) FROM suppliers WHERE id IN ('sup-khanoumi', 'sup-technolife', 'sup-filimo')` → ge 3
- `SELECT count(*) FROM stores WHERE id IN ('store-central', 'store-bibi', 'store-sepehr', 'store-parsian')` → ge 4
- `SELECT count(*) FROM products WHERE id IN ('prod-1', 'prod-2', 'prod-3', 'prod-4', 'prod-5', 'prod-6')` → ge 6
- `SELECT count(*) FROM product_niches` → ge 6

### `000004_seed_wms_and_ledger.up.sql` — 18.743 ms, tables: inbound_shipments, ledger_entries, shipping_manifests, warehouse_bins

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM warehouse_bins` → ge 3
- `SELECT count(*) FROM inbound_shipments WHERE id IN ('INB-9941')` → ge 1
- `SELECT count(*) FROM shipping_manifests WHERE id IN ('MAN-501', 'MAN-502')` → ge 2
- `SELECT count(*) FROM ledger_entries WHERE id IN ('ADJ-301', 'ADJ-302')` → ge 2

### `000013_supply_option.up.sql` — 16.17 ms, tables: products

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM products WHERE supply_option IS NULL` → eq 0

### `000016_store_panel_config.up.sql` — 36.674 ms, tables: store_products, stores

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM stores WHERE panel_config IS NULL` → eq 0

### `000017_seller_wallet.up.sql` — 38.201 ms, tables: payout_requests, seller_service_purchases

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM payout_requests WHERE available_balance IS NULL` → eq 0

### `000018_backfill_store_products.up.sql` — 101.264 ms, tables: store_products, stores

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM stores s CROSS JOIN products p WHERE s.owner_type = 'dropshipper' AND p.supplier_base_cost > 0 AND NOT EXISTS (SELECT 1 FROM store_products sp WHERE sp.store_id = s.id AND sp.product_id = p.id)` → eq 0

### `000022_seed_operations_staff.up.sql` — 19.538 ms, tables: SET, users

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM users WHERE id IN ('a0000000-0000-0000-0000-000000000044', 'a0000000-0000-0000-0000-000000000055')` → ge 2

### `202610130003_ippanel_settings.up.sql` — 49.248 ms, tables: platform_settings

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE ippanel_enabled IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE ippanel_base_url IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE ippanel_api_key IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE ippanel_sender IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE ippanel_pattern_code IS NULL` → eq 0

### `202610160003_order_cancel_shipping.up.sql` — 21.839 ms, tables: orders, platform_settings

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE shipping_base_rate IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE shipping_per_kg IS NULL` → eq 0

### `202610160003_remediate_dev_seed_credentials.up.sql` — 16.321 ms, tables: stores, users

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM users WHERE password_hash IN ('$2a$10$boGlIBfo9lgFx7cwl0xKs.wnyHJe3St98d2feLRKC1bmYbFrFIfWK', '$2a$10$p6zuh4wx1l.S1.z3aScOaexWy1nQaaXIA8vbdH5HUA5Ilp1.G')` → eq 0
- `SELECT count(*) FROM stores WHERE id IN ('store-central', 'store-bibi', 'store-sepehr', 'store-parsian') AND total_sales IN (184500000, 44100000, 52000000, 38000000)` → eq 0

### `202610160007_finance_admin_settings.up.sql` — 46.348 ms, tables: platform_settings, supplier_promotions

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE product_fee_overrides IS NULL` → eq 0

### `202610160008_warehouse_tariffs.up.sql` — 31.701 ms, tables: platform_settings

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE warehouse_tariffs IS NULL` → eq 0

### `202610160009_marketing_admin.up.sql` — 78.466 ms, tables: platform_settings, subscription_plans

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE trial_catalog_quota IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE central_campaign_budget IS NULL` → eq 0
- `SELECT count(*) FROM subscription_plans WHERE id IN ('trial', 'standard', 'pro', 'vip')` → ge 4

### `202610160010_store_templates.up.sql` — 78.415 ms, tables: store_templates, stores

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM stores WHERE template_config IS NULL` → eq 0
- `SELECT count(*) FROM store_templates WHERE id IN ('tpl-modern-1')` → ge 1

### `202610170004_product_is_active.up.sql` — 45.875 ms, tables: products

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM products WHERE is_active IS NULL` → eq 0

### `202610170005_license_page.up.sql` — 30.047 ms, tables: SET, static_pages

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM static_pages` → ge 1

### `202610170006_warehouse_staff_city.up.sql` — 24.044 ms, tables: users

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM users WHERE warehouse_city IS NULL` → eq 0

### `202610170007_footer_links_social.up.sql` — 34.264 ms, tables: site_content

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM site_content` → ge 11

### `202610170008_store_onboarding_fields.up.sql` — 41.229 ms, tables: stores

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM stores WHERE active_categories IS NULL` → eq 0
- `SELECT count(*) FROM stores WHERE owner_phone IS NULL` → eq 0

### `202610180002_backfill_store_owner_phone.up.sql` — 34.066 ms, tables: stores, users

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM stores s JOIN users u ON s.owner_user_id = u.id WHERE (s.owner_phone IS NULL OR TRIM(s.owner_phone) = '') AND u.mobile IS NOT NULL AND TRIM(u.mobile) <> ''` → eq 0

### `202610190003_supplier_approval.up.sql` — 92.289 ms, tables: suppliers

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM suppliers WHERE approval_status IS NULL OR approval_status = ''` → eq 0
- `SELECT count(*) FROM suppliers WHERE approval_status IS NULL` → eq 0

### `202610190008_store_template_tech.up.sql` — 18.556 ms, tables: store_templates

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM store_templates WHERE id IN ('tpl-tech-1')` → ge 1

### `202610190009_pricing_engine.up.sql` — 49.674 ms, tables: platform_settings, products, suppliers

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE default_markup_type IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE default_markup_value IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE central_store_markup_percent IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE max_dropshipper_margin_percent IS NULL` → eq 0

### `202610190010_store_template_electronic.up.sql` — 22.499 ms, tables: store_templates

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM store_templates WHERE id IN ('tpl-electronic-1')` → ge 1

### `202610190011_store_template_fashion.up.sql` — 79.108 ms, tables: store_templates

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM store_templates WHERE id IN ('tpl-fashion-1')` → ge 1

### `202610190012_store_template_digikala.up.sql` — 28.317 ms, tables: store_templates

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM store_templates WHERE id IN ('tpl-digikala-1')` → ge 1

### `202610200001_supplier_signed_contract_version.up.sql` — 33.258 ms, tables: suppliers

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM suppliers WHERE signed_contract_version < 2` → eq 0
- `SELECT count(*) FROM suppliers WHERE signed_contract_version IS NULL` → eq 0

### `202610200003_product_brand_and_reviews.up.sql` — 277.416 ms, tables: product_reviews, products

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM products WHERE COALESCE(TRIM(specs->>'برند'), '') <> '' AND brand = ''` → eq 0
- `SELECT count(*) FROM products WHERE brand IS NULL` → eq 0
- `SELECT count(*) FROM products WHERE rating IS NULL` → eq 0
- `SELECT count(*) FROM products WHERE reviews_count IS NULL` → eq 0

### `202610200007_homepage_identity_waveb.up.sql` — 56.678 ms, tables: homepage_sections

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM homepage_sections WHERE section_type = 'suppliers_showcase' AND is_active` → eq 0

### `202610230007_shipping_legs.up.sql` — 111.942 ms, tables: order_shipping_legs, platform_settings, supplier_shipments

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE default_item_weight_grams IS NULL` → eq 0

### `202610230008_category_visibility.up.sql` — 66.809 ms, tables: categories

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM categories WHERE is_active IS NULL` → eq 0

### `202610250009_inventory_captured.up.sql` — 21.303 ms, tables: orders, payment_transactions

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM orders WHERE inventory_captured = FALSE AND ((wallet_amount = payable_amount AND wallet_amount > 0) OR EXISTS (SELECT 1 FROM payment_transactions pt WHERE pt.order_id = orders.id))` → eq 0
- `SELECT count(*) FROM orders WHERE inventory_captured IS NULL` → eq 0

### `202610250010_remove_stale_seed_putaway.up.sql` — 17.399 ms, tables: putaway_capacity_reservations, putaway_tasks

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower. DATA-DELETE NOTE: deletes only the stale seed putaway task PUT-INIT-001 (and its capacity reservation), status-guarded (task pending/assigned, reservation active). It is seed data from 000008, not accepted business data; the guard makes it a no-op on any DB where the task legitimately completed.

- `SELECT count(*) FROM putaway_capacity_reservations WHERE task_id = 'PUT-INIT-001' AND status = 'active'` → eq 0
- `SELECT count(*) FROM putaway_tasks WHERE id = 'PUT-INIT-001' AND status IN ('pending', 'assigned')` → eq 0

### `202610250013_returns_shipping_nonrefundable.up.sql` — 18.772 ms, tables: static_pages

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM static_pages WHERE key = 'terms' AND content NOT LIKE '%هزینه ارسال غیرقابل‌استرداد است%'` → eq 0

### `202610250201_returns_warehouse_prepaid.up.sql` — 62.906 ms, tables: customer_returns

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM customer_returns WHERE prepaid IS NULL` → eq 0
- `SELECT count(*) FROM customer_returns WHERE tracking_number IS NULL` → eq 0

### `202610250202_ret_money_return_shipping_settlement.up.sql` — 160.784 ms, tables: customer_return_shipping_charges, customer_returns

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM customer_returns WHERE prepaid IS NULL` → eq 0
- `SELECT count(*) FROM customer_returns WHERE cod_refused IS NULL` → eq 0

### `202610260001_stuck_order_expiry_flag.up.sql` — 41.162 ms, tables: platform_settings

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE stuck_order_expiry_enabled IS NULL` → eq 0

### `202610280002_warehouse_capacity.up.sql` — 86.508 ms, tables: warehouses

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM warehouses WHERE is_receiving_open IS NULL` → eq 0

### `202610280003_dimensions_provenance.up.sql` — 20.77 ms, tables: order_shipping_legs, platform_settings, products

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE volumetric_divisor IS NULL` → eq 0

### `202610280007_tapin_default_box.up.sql` — 21.176 ms, tables: customer_returns, platform_settings

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE tapin_default_box_id IS NULL` → eq 0

### `202610310002_mock_backends.up.sql` — 137.03 ms, tables: customer_addresses, customer_wishlists, platform_settings, product_reviews, subscription_plans

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE product_fee_overrides IS NULL` → eq 0
- `SELECT count(*) FROM subscription_plans WHERE id IN ('trial', 'standard', 'pro', 'vip')` → ge 4

### `202610310003_pricing_system.up.sql` — 325.872 ms, tables: platform_settings, product_market_ceilings, product_price_exceptions, supplier_price_increases, support_tickets

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE platform_pct IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE central_profit_pct IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE increase_cap_pct IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE increase_locked IS NULL` → eq 0
- `SELECT count(*) FROM support_tickets WHERE section IS NULL` → eq 0

### `202610310006_implicit_agreement.up.sql` — 43.958 ms, tables: products, supplier_price_increases

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM products WHERE implicit_agreement IS NULL` → eq 0
- `SELECT count(*) FROM supplier_price_increases WHERE auto_approved IS NULL` → eq 0
- `SELECT count(*) FROM supplier_price_increases WHERE agreement_review IS NULL` → eq 0

### `202610310014_store_settings.up.sql` — 69.852 ms, tables: disputes, stores

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM stores WHERE logo IS NULL` → eq 0

### `202611030002_seller_discount_codes.up.sql` — 171.94 ms, tables: order_items, orders, seller_discount_codes

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM orders WHERE seller_discount_amount IS NULL` → eq 0
- `SELECT count(*) FROM order_items WHERE discount_amount IS NULL` → eq 0

### `202611050005_storage_fee_settings.up.sql` — 21.546 ms, tables: platform_settings

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM platform_settings WHERE storage_fee_enabled IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE storage_fee_free_days IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE storage_fee_per_kg_day IS NULL` → eq 0
- `SELECT count(*) FROM platform_settings WHERE storage_fee_per_m3_day IS NULL` → eq 0

### `202611050010_price_increase_clear_on_approve.up.sql` — 27.297 ms, tables: supplier_price_increases

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM supplier_price_increases WHERE clear_on_approve IS NULL` → eq 0

### `202611100002_fashion_editorial_redesign.up.sql` — 28.007 ms, tables: store_templates, stores

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM store_templates WHERE id = 'tpl-fashion-1' AND description <> 'قالب روشن و مینیمال مد؛ بنر دو‌بخشی، مجموعه‌های تصویری و چیدمان ریسپانسیو'` → eq 0

### `202611130004_variant_shipping_tax_class.up.sql` — 20.587 ms, tables: product_variants, products

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM products WHERE shipping_class IS NULL` → eq 0
- `SELECT count(*) FROM products WHERE tax_class IS NULL` → eq 0

### `202611140001_money_policy_version.up.sql` — 21.07 ms, tables: order_items

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM order_items WHERE pricing_policy_version IS NULL` → eq 0

### `202611140010_sku_measurement_revision.up.sql` — 168.935 ms, tables: OLD, ON, OR, capacity, effective, inbound_putaway_allocations, inventory, inventory_measurement_revisions, product, product_variants, products, putaway_capacity_reservations, putaway_tasks, r, reference, warehouse, warehouse_placements

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM product_variants WHERE measurement_status IS NULL` → eq 0
- `SELECT count(*) FROM product_variants WHERE measurement_version IS NULL` → eq 0
- TODO: INSERT...SELECT into inventory_measurement_revisions plus an ADD COLUMN NOT NULL (NULL-check emitted); the SELECT-side completeness check needs the revision-source rule from a reviewer.

### `202611170001_coupon_reservations.up.sql` — 256.168 ms, tables: FALSE, NEW, ON, OR, coupon, coupon_reconciliation_issues, evidence, fin007_counts, fin007_history, migration, o, oid, order, order_coupon_events, order_coupon_reservations, order_payment_receipts, order_payment_verifications, orders, payment_transactions, r, reservation, seller_discount_codes

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM seller_discount_codes c WHERE c.used_count <> (SELECT count(*) FROM order_coupon_reservations r WHERE r.coupon_id = c.id AND r.state <> 'released')` → eq 0
- `SELECT count(*) FROM seller_discount_codes WHERE revision IS NULL` → eq 0

### `202611190004_qual01_warehouse_staff_city.up.sql` — 29.175 ms, tables: users

Single full-table operation (no chunking in any of the 210 files; the five LIMIT occurrences in the set are single-row guards, not batches). Runtime is the measured empty-schema disposable number; prod-shaped data will be slower.

- `SELECT count(*) FROM users WHERE warehouse_city_confirmed IS NULL` → eq 0

### `202611230001_batch9_variant_rename.up.sql` — unmeasured, tables: product_variants

Batch 9: irreversibly derives display names from non-empty attribute values on variable or multi-variant products. Rows with no non-empty values are left for supplier action.


## Destructive file (1)

### `202611030001_remove_pricing_tiers.up.sql` — breaking

No old-binary write exists: no Go code at 92f9cca references supplier_pricing_tiers (0 hits outside the migrations themselves); the engine was removed from backend and frontend before this migration.

DROP TABLE supplier_pricing_tiers. 0-row evidence: the migration header states 'The table held 0 rows at removal time' (2026-11-03 user decision to remove tiered pricing). A live prod row count was NOT verifiable from this VM (no prod access); the claim rests on the migration author's note plus the complete absence of code references. Never used by code: verified by grep over backend/ at 92f9cca. Floor note: a DROP TABLE is breaking in principle, but with 0 rows and 0 code references there is no live binary that can observe it; the floor stays ff57367.

## Expand files (95)

Additive only; the old binary is unaffected. Listed by name — see the
JSON for tables, operations and runtimes.

- `000001_initial_schema.up.sql`
- `000003_wms_and_payouts.up.sql`
- `000005_audit_logs.up.sql`
- `000007_supplier_lifecycle.up.sql`
- `000009_reserved_reconciliation.up.sql`
- `000010_reserved_settlements.up.sql`
- `000012_referral_bonus.up.sql`
- `000014_supplier_team_roles.up.sql`
- `000015_store_ad_widgets.up.sql`
- `000021_normalize_user_mobiles.up.sql`
- `000026_historical_reconciliation_reviews.up.sql`
- `000027_user_capabilities.up.sql`
- `202610130001_sms_logs.up.sql`
- `202610160001_banners.up.sql`
- `202610160002_homepage_sections.up.sql`
- `202610160004_product_gallery.up.sql`
- `202610160005_site_content.up.sql`
- `202610160011_store_metrics.up.sql`
- `202610160012_finance_metrics.up.sql`
- `202610160013_support_metrics.up.sql`
- `202610160014_growth_metrics.up.sql`
- `202610160016_overview_metrics.up.sql`
- `202610170010_proposal_field_reviews.up.sql`
- `202610170011_payout_status_honesty.up.sql`
- `202610170012_supplier_shipping_methods.up.sql`
- `202610180003_staged_profit_release.up.sql`
- `202610180004_payment_callback_events.up.sql`
- `202610180005_notification_outbox.up.sql`
- `202610190001_payment_callback_retries.up.sql`
- `202610190003_add_orders_return_window_ends_at.up.sql`
- `202610190004_profit_lock_view.up.sql`
- `202610190006_pack_box_size.up.sql`
- `202610190007_cms_improvements.up.sql`
- `202610200004_newsletter_subscribers.up.sql`
- `202610200005_order_items_variant.up.sql`
- `202610220002_supplier_tapin.up.sql`
- `202610220003_search_queries.up.sql`
- `202610220004_exclusivity_challenges.up.sql`
- `202610230002_views.up.sql`
- `202610230003_price_snapshots.up.sql`
- `202610230004_discount_campaigns.up.sql`
- `202610240001_multi_origin_warehouses.up.sql`
- `202610250007_refunds_table.up.sql`
- `202610250011_payout_transfer_confirmed_cashflow.up.sql`
- `202610250012_tapin_register_guard.up.sql`
- `202610250401_store_variant_overrides.up.sql`
- `202610250402_store_related_products.up.sql`
- `202610250403_addon_store_selection.up.sql`
- `202610280004_dynamic_origin.up.sql`
- `202610280005_storage_fee.up.sql`
- `202610280006_storage_fee_assessed.up.sql`
- `202610280008_packaging_dimensions.up.sql`
- `202610280010_supplier_shipment_packaging_dims.up.sql`
- `202610280011_digikala_attributes.up.sql`
- `202610280012_product_attribute_values.up.sql`
- `202610310005_platform_pct_ladder.up.sql`
- `202610310008_job_runs.up.sql`
- `202610310009_search_catalog_count.up.sql`
- `202610310010_store_margin_locks.up.sql`
- `202610310015_lock_snapshots.up.sql`
- `202610310016_order_dispute_reason.up.sql`
- `202611020002_monthly_settlement.up.sql`
- `202611020003_add_banners_category_slug.up.sql`
- `202611020004_ad_event_rejections.up.sql`
- `202611030003_shipping_quote_id.up.sql`
- `202611040001_addon_linked_product.up.sql`
- `202611040012_finance_adjustments.up.sql`
- `202611040020_product_margin_ceilings.up.sql`
- `202611050001_customer_store_links.up.sql`
- `202611050003_return_destination_supplier_warehouse.up.sql`
- `202611050004_user_email.up.sql`
- `202611050006_customer_return_financial_events.up.sql`
- `202611050007_supplier_suspension.up.sql`
- `202611050008_tapin_boxes_and_shipment_registration.up.sql`
- `202611050009_price_increase_variant.up.sql`
- `202611110001_granular_review.up.sql`
- `202611130001_variant_image_and_physical.up.sql`
- `202611130003_store_variant_visibility.up.sql`
- `202611130005_store_variant_defaults.up.sql`
- `202611140003_observability_alerts.up.sql`
- `202611140007_supplier_warehouse_geography.up.sql`
- `202611140009_exact_inbound_putaway.up.sql`
- `202611150001_warehouse_occupancy.up.sql`
- `202611150003_storage_receipt_history.up.sql`
- `202611150005_checkout_attempts.up.sql`
- `202611150007_background_jobs.up.sql`
- `202611160002_dispute_eligibility.up.sql`
- `202611180001_treasury_cash_evidence.up.sql`
- `202611180002_sec01_tapin_token_encryption.up.sql`
- `202611180003_obs01_backup_signal.up.sql`
- `202611180005_fin011_referral_reversals.up.sql`
- `202611190001_fin009_seller_profit_reversals.up.sql`
- `202611190002_perf01_catalog_price_index.up.sql`
- `202611200001_qual01_reconciliation_decision_history.up.sql`
- `202611200002_perf02_bounding_indexes.up.sql`
- `202611200003_pending_approvals_target_lookup.up.sql`
- `202611210001_fin014_repair_batches.up.sql`
