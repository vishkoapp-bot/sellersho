# W3 — Seeded fixtures for write-path verification (staging API, fully reverted)

- **Date:** 2026-11-04 (run completed same day, fixtures created and deleted within minutes)
- **Worker:** W3, Team 6 (remainder: backend/data/Tapin)
- **Scope:** staging API only. **No live access of any kind, no live auth probing.**
- **Build under test:** the staging backend at `http://127.0.0.1:18080` (loopback-only,
  container `drup_staging_phase3_backend`) **as currently deployed — the deployed Fix69 build**.
  These tests verify deployed Fix69 code, **not** uncommitted branch code. W3 wrote **no branch
  code** and performed **no commit, merge, deploy, rebuild, or preview change**. The only files
  W3 created outside `/tmp` scratch (removed after the run) are in this report directory.
- **Auth:** staging JWT obtained only via the staging OTP flow (`POST /api/v1/auth/otp/send`,
  then `/api/v1/auth/otp/verify` with the code read from staging Redis server-side). OTP codes
  and JWTs were held in memory only, used immediately, and are **not printed or stored** anywhere
  in this report.
- **Test identities (staging only):** dropshipper/supplier mobile `09331796649`
  (store `mone` = `store-44d7be7c-6747-42ab-a0d2-9a52ddb477ff`; user capabilities on staging:
  seller + supplier + customer) and admin `09933070870` (not needed; no admin call was required).
- **Result summary: 22/22 checks PASS. Residuals: none. Zero diff after revert (see §5).**

## 1. Fixed pricing inputs (recorded before any fixture)

- Product: `prod-brush-set` (warehouse fulfillment, central supplier `sup-selersho-central`,
  in `store_products` for `mone`, so the order path's "published by this store" gate passes).
- Supplier base cost C = 580,000 Toman; `platform_pct` = 0, so reference cost R = 580,000
  (platform fee F = 0); store margin 15% (custom override 15.00, no product override for this
  product, no margin locks for this store) → undiscounted final price P = 667,000,
  dropshipper margin D = 87,000. All quote/order expectations below derive from these numbers.
- Store `mone` margin lock flag: `is_margin_locked_by_admin = false`; `store_margin_locks`:
  0 rows. Pre-existing fixture-adjacent state: 0 discount codes and 0 discount campaigns for
  this store (clean slate, so every fixture row below is attributable to W3).

## 2. Target (a) — Discount-quote POST math: PASS (10/10 checks)

Endpoint: `POST /api/v1/stores/mone/discount-quote` (public, no auth — observed, not assumed).
All quotes used 1 × `prod-brush-set`. Amounts in Toman.

| # | Case | Request | Response (HTTP 200 unless noted) | Expected | Verdict |
|---|------|---------|----------------------------------|----------|---------|
| A0 | Baseline, no campaign, no code | items only | original 667,000; payable 667,000; sellerDiscount 0; discountCodeValid false | same | PASS |
| A1 | Campaign-only | campaign C1: scope `product`, target `prod-brush-set`, mode `percent`, value 5 (created via API, id `dcamp-97ed6c23-672e-4845-8809-43ebc709754c`) | original 667,000; payable 609,000; sellerDiscount 58,000; valid false | target margin 5% × 580,000 = 29,000 → final 609,000, discount 58,000 | PASS |
| A1b | Quote never consumes a code | code `W3PCT10` created (id below), campaign-only quote | used_count stays 0 | 0 | PASS |
| A2 | Stacked campaign + code | C1 active + code `W3PCT10` (percent 10) | payable 580,000; sellerDiscount 87,000; valid true | campaign → 609,000 (margin left 29,000); code 10% of 609,000 = 60,900 capped at remaining margin 29,000 → floor 580,000, total discount 87,000 | PASS |
| A2b | Stacked quote never consumes | — | used_count stays 0 after stacked quote | 0 | PASS |
| A1c | Campaign delete via API | `DELETE /stores/mone/discount-campaigns/<C1>` | HTTP 200, DB rows for C1 = 0 | deleted | PASS |
| A3 | Code-only | code `W3PCT10` (percent 10, maxUses 10, id `dcode-32553ac9-0e08-460a-9364-49a4e48a677f`), no campaign | payable 600,300; sellerDiscount 66,700; valid true | 10% × 667,000 = 66,700 (< margin 87,000), final 600,300 | PASS |
| A4 | Zero-margin floor, code | code `W3FIXBIG` (fixed 999,999,000, maxUses 10, id `dcode-9408fe7d-7b40-44fd-93df-c9bf18c54449`) | payable 580,000; sellerDiscount 87,000; valid true | fixed discount capped at margin 87,000; price never below wholesale + fee = 580,000 + 0 | PASS |
| A5 | Zero-margin floor, campaign | campaign C2: fixed 0 (id `dcamp-453373ec-157e-48be-b27d-0cd2d06baf8f`) | payable 580,000; sellerDiscount 87,000; valid false (no code) | target margin 0 → final = base cost 580,000 | PASS |
| A5b | Campaign delete via API | `DELETE …/<C2>` | HTTP 200, DB rows = 0 | deleted | PASS |
| A6 | No-price-hike clamp | campaign C3: percent 50 (id `dcamp-26c8fadb-6061-4d2c-95fb-68c8ba550c5b`) | payable 667,000; sellerDiscount 0; valid false | target margin 50% × 580,000 = 290,000 > original margin 87,000 → clamped to 87,000; price unchanged, **not raised** | PASS |
| A6b | Campaign delete via API | `DELETE …/<C3>` | HTTP 200, DB rows = 0 | deleted | PASS |
| A7 | Invalid code in quote | discountCode `W3NOPE` (never created) | payable 667,000; sellerDiscount 0; discountCodeValid false | campaign-only totals with no campaign = baseline; invalid code is reported, not silently ignored into a full-price charge at order time (see B1) | PASS |

Notes on (a): campaigns were created and deleted **serially** (one active at a time) because
the resolver picks the most recent campaign per scope; overlapping campaigns would have
contaminated each other's expectations. Campaign/code windows were now−1h to now+48h,
`isActive = true`. Code creation and campaign creation both used the seller API
(`POST /stores/mone/discount-codes`, `POST /stores/mone/discount-campaigns`); no direct-DB
fixture inserts were needed for (a).

## 3. Target (b) — Order creation with a discount code: PASS (6/6 checks)

Endpoint: `POST /api/v1/orders` with `storeId: "mone"` (the handler resolves the store by slug;
sending the store UUID would have fallen back to `central`, so the slug was used deliberately).
Customer info was the fixture identity; no `shippingQuoteId`, no `idempotencyKey`,
no `SaveAddress`, no addons — so no shipping, wallet, or address-book side effects.
Payment method is forced to `cash` server-side; payment status stayed `pending` (no PSP call).

| # | Case | Request / response | DB evidence after the call | Verdict |
|---|------|--------------------|----------------------------|---------|
| B1 | Invalid code fails the whole order **before** persistence | `discountCode: "W3NOPE"`, qty 1 → **HTTP 400** `{"error":"Discount code is invalid or expired"}` | store order count 7 → 7 (no new row); no order with that code | PASS |
| B2 | Valid code + failing order consumes **nothing** | `discountCode: "W3PCT10"`, qty 37 (stock is 36) → **HTTP 400** `{"error":"Insufficient stock for product prod-brush-set"}` | store order count 7 → 7; code `W3PCT10` used_count stays 0 | PASS — this is the "consumed only after persistence" negative: validation passes, persistence never happens, no use is consumed |
| B3 | Valid code order, snapshots | `discountCode: "W3PCT10"`, qty 1 → **HTTP 201**, order `ord-2ac8751e-3418-4009-8a4d-d24e036f18f4` | orders row: payable 600,300; `seller_discount_amount` 66,700; `discount_code` `W3PCT10`; `dropshipper_profit` 20,300 (= 600,300 − 580,000); `platform_profit` 0; status `processing`; `shipping_cost` 0; `shipping_quote_id` empty; `online_shipping_selection` NULL. order_items row: wholesale 580,000; platform_fee 0; seller_margin 20,300; final_unit_price 600,300; discount_amount 66,700; qty 1 | PASS |
| B3b | Code use consumed exactly once, after persistence | — | code `W3PCT10` used_count 0 → 1 after B3 | PASS |
| B4 | Campaign-only order, snapshots | campaign C4: percent 5 (id `dcamp-02b59878-9b53-4f97-82ee-3a51dc3ec2c2`), no code → **HTTP 201**, order `ord-16f9d5d5-690e-481f-87b0-b9e36d89c0c3` | orders row: payable 609,000; `seller_discount_amount` 58,000; `discount_code` empty; `dropshipper_profit` 29,000. order_items: final 609,000; margin 29,000; discount 58,000; wholesale 580,000; fee 0 | PASS |
| B4b | Campaign delete via API | `DELETE …/<C4>` | HTTP 200, DB rows = 0 | PASS |

Caution on column names (observed, to prevent misreading): `orders.discount_amount` on the
B3 order is 289,700 and on B4 is 281,000 — that column is **OriginalPrice − final price**
(890,000 − 600,300; 890,000 − 609,000), **not** the seller discount. The seller discount is
`seller_discount_amount` (66,700 / 58,000) and the per-item `order_items.discount_amount`
(66,700 / 58,000). Settlement must use the latter, not `orders.discount_amount`.

## 4. Target (c) — Cancel and confirm-delivery transitions: PASS (3/3 checks)

Both transitions ran on fixture orders created in this run (same product, qty 1, no discounts).

| # | Case | Request / response | DB evidence | Verdict |
|---|------|--------------------|-------------|---------|
| C1 | Cancel from `processing` | `POST /api/v1/orders/ord-a13f8d50-595b-4ce0-95db-7fb82036aba5/cancel` → **HTTP 200** `{"success":true,"status":"cancelled"}` | status `cancelled`, payment_status `cancelled` | PASS |
| C2 | Confirm-delivery from `processing` is rejected | `POST /api/v1/orders/confirm-delivery/ord-38b20b7e-88de-40bd-9662-dd6edb8cb648` → **HTTP 404** `{"error":"Order not found or not owned by user"}` | status stays `processing` | PASS (negative, as designed: the repo rule admits only `shipped`, `delivered_unconfirmed`, `delivered`) |
| C3 | Confirm-delivery from `shipped` | Same order as C2, after a **disclosed fixture-only** direct-DB `UPDATE orders SET status='shipped'` (no warehouse-role API is available to this test identity, and no API path exists for it to advance a fixture order; the UPDATE touched only this fixture order) → `POST …/confirm-delivery/…` → **HTTP 200** `{"success":true,"status":"delivered_confirmed"}` | status `delivered_confirmed`; `confirmed_at` set; `return_window_ends_at` set; `profit_released` stays `false` (profit stays locked until the return window closes, per the endpoint message) | PASS |

## 5. Revert table — zero diff (all fixtures deleted in the same run, promptly)

Pre-values were recorded before creating anything; post-values were re-read after revert.
Order creation in this build does **not** decrement `products.stock` (no stock UPDATE in the
create path; `inventory_captured` is false), and cancel therefore had no stock to restore —
stock is 36 before, during, and after, which is itself verified, not assumed.

| Fixture | Table | Rows created | Rows deleted | Notes |
|---------|-------|--------------|--------------|-------|
| Campaign C1 `dcamp-97ed6c23-672e-4845-8809-43ebc709754c` (percent 5) | `seller_discount_campaigns` | 1 | 1 (via API DELETE) | Deleted before A3 so it could not contaminate later quotes |
| Campaign C2 `dcamp-453373ec-157e-48be-b27d-0cd2d06baf8f` (fixed 0) | `seller_discount_campaigns` | 1 | 1 (via API DELETE) | |
| Campaign C3 `dcamp-26c8fadb-6061-4d2c-95fb-68c8ba550c5b` (percent 50) | `seller_discount_campaigns` | 1 | 1 (via API DELETE) | |
| Campaign C4 `dcamp-02b59878-9b53-4f97-82ee-3a51dc3ec2c2` (percent 5) | `seller_discount_campaigns` | 1 | 1 (via API DELETE) | |
| Code `W3PCT10` `dcode-32553ac9-0e08-460a-9364-49a4e48a677f` (percent 10, maxUses 10) | `seller_discount_codes` | 1 | 1 (direct DB DELETE — no code-DELETE endpoint exists) | used_count 0 → 1 during B3, then the whole row was deleted, so no use-count restoration was needed or left behind |
| Code `W3FIXBIG` `dcode-9408fe7d-7b40-44fd-93df-c9bf18c54449` (fixed 999,999,000, maxUses 10) | `seller_discount_codes` | 1 | 1 (direct DB DELETE — no code-DELETE endpoint exists) | used_count stayed 0 (quote-only) |
| Order B3 `ord-2ac8751e-3418-4009-8a4d-d24e036f18f4` | `orders` / `order_items` | 1 / 1 | 1 / 1 | `supplier_shipments` rows for this order: 0 created, 0 remaining |
| Order B4 `ord-16f9d5d5-690e-481f-87b0-b9e36d89c0c3` | `orders` / `order_items` | 1 / 1 | 1 / 1 | shipments: 0 / 0 |
| Order C1 `ord-a13f8d50-595b-4ce0-95db-7fb82036aba5` (cancelled) | `orders` / `order_items` | 1 / 1 | 1 / 1 | shipments: 0 / 0; cancellation itself was reverted by deletion (a cancelled row is still a leftover if kept) |
| Order C2/C3 `ord-38b20b7e-88de-40bd-9662-dd6edb8cb648` (delivered_confirmed) | `orders` / `order_items` | 1 / 1 | 1 / 1 | shipments: 0 / 0 |

Zero-diff proof (before → after):

| Measure | Before | After | Diff |
|---------|--------|-------|------|
| `products.stock` for `prod-brush-set` | 36 | 36 | 0 |
| `orders` for store `mone` | 7 | 7 | 0 |
| `orders` for customer user `66c6f94d-…-8792` | 17 | 17 | 0 |
| `seller_discount_codes` for store `mone` | 0 | 0 | 0 |
| `seller_discount_campaigns` for store `mone` | 0 | 0 | 0 |
| `suppliers` count (untouched table) | 7 | 7 | 0 |
| `user_addresses` for test user | 1 | 1 | 0 |
| `user_capabilities` for test user (OTP login must not duplicate grants) | 3 (seller, supplier, customer) | 3 | 0 |
| Fixture ids present in `orders` / `order_items` / `supplier_shipments`, or any order carrying `discount_code` `W3PCT10`/`W3FIXBIG` | — | 0 in every check | 0 |

**Residuals: none.** No fixture row, code use, campaign, order, item, shipment, address, or
capability row remains. The revert ran in a `finally` block in the same script run, so it
executed even though no test failed; had a test failed, the same revert would still have run
and any leftover would have been listed here as a residual instead of silently kept.

## 6. Forbidden-compliance

- **Tapin fixture untouched:** no statement of any kind was issued against `suppliers` or any
  `supplier_*` table except a `COUNT(*)` read; no Tapin endpoint was called; no Tapin DELETE
  was executed. Fixture products are central-warehouse products (`sup-selersho-central`), not
  `sup-supplier_test_0933` products. Tapin shop/token values were never read or printed.
- **Shipping rows untouched:** every fixture order had `shipping_cost` 0, empty
  `shipping_quote_id`, and NULL `online_shipping_selection`; `supplier_shipments` counts for
  all fixture orders were 0. No shipping-quote or shipping-fixture row was created, read for
  mutation, or modified.
- **Real data untouched:** no real user order was modified (only the 4 fixture orders above,
  all deleted); no live system was contacted; no real payment was attempted (cash/pending
  only); the only SMS-adjacent action was the prescribed staging OTP login to the staging
  test identity itself.
- **No out-of-scope edits:** nothing under `/root/drup-deploy-candidate`, preview dist,
  staging binaries, `.bak-fix68`, or any shipping worktree/branch was touched. Server-side
  scratch (`/tmp/w3_test.py`, `/tmp/w3_pre_post.json`) was removed after the run.

## 7. Left unverified (explicit, not claimed)

1. **Admin margin-lock path:** the lock-dropped-code behavior (code validated but dropped on
   every line → not recorded, no use consumed) was **not** exercised, because it requires an
   admin margin lock fixture on this store/product and no admin lock manipulation was in scope
   for W3. The code path exists in the deployed code but is unproven by this run.
2. **Variant pricing:** no `variantId` was used; variant overrides, variant stock gates, and
   variant snapshots are untested here.
3. **Multi-item / multi-quantity quotes and orders:** all priced checks used qty 1 (plus the
   qty-37 stock-failure negative). Per-unit math is linear in the code, but a multi-line
   stacked quote was not run.
4. **Payment, settlement, and scheduler effects:** fixture orders were `pending` cash orders
   deleted within minutes, before any settlement/scheduler tick could act on them. Whether a
   confirmed fixture order would later release profit was therefore not observed — and
   deliberately not left observable.
5. **Admin identity:** the admin mobile was not used; no admin-only endpoint was needed for
   these targets.
6. **Build identity limitation:** "deployed Fix69 build" is taken from the task brief and from
   the fact that no rebuild/redeploy was performed by W3; no binary hash was compared against
   a Fix69 branch reference in this run.
