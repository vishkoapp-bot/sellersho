# List pagination + report-bounding contract (PERF-02)

Status: IMPLEMENTED (PERF-02, Nov 2026). This is the single contract for
every list/report endpoint in the platform. New list endpoints MUST follow
it; the pre-existing exceptions are listed in §6 and are NOT to be copied.

## 1. The one invalid-limit policy

`?limit=` is parsed by `parseListPage` (`backend/cmd/server/pagination_helpers.go`)
on top of `NormalizeListLimit` (`backend/internal/repository/list_pagination.go`):

| Input | Result |
|---|---|
| missing or `0` | default **50** |
| `1`–`100` | honored as given |
| `> 100` | **clamped to 100**; the effective limit is echoed in the response `limit` field |
| non-integer or negative | **400** (`{"error":"Invalid limit"}`) |

`?cursor=` malformed → **400** (`{"error":"cursor is invalid"}` — the raw
token is never echoed). `?page=` on cursor-paged routes → **400**
("page pagination is removed; use cursor"). Cursor pagination is the
contract for every route in §4 — but OFFSET pagination is NOT deleted
from the codebase: the pre-existing exceptions in §6 still accept
`?page=` / `?offset=` (warehouse-orders, product-reviews). Do not copy
them; new list endpoints MUST use the cursor contract. (The
`LIMIT…OFFSET` that used to sit in the reconciliation report's
`queryHistoricalReport` was dead code — its only caller passed offset 0 —
and was deleted in the PERF-02 critic-conditions round; the live
reconciliation report pages by cursor.)

Every paged response is an envelope:

```json
{"<items>": [...], "nextCursor": "<opaque or empty>", "hasMore": true, "limit": 50}
```

`nextCursor` is `""` and `hasMore` is `false` on the last page. Clients
MUST page with `nextCursor`, never by guessing a cursor. Rows are fetched
with the limit+1 pattern; the extra row is never returned.

## 2. Cursor format and stability semantics

Two cursor families exist (both base64url, opaque to clients):

- **Created-at cursors** — `base64url(RFC3339Nano(created_at) + "\n" + id)`,
  sort `(created_at DESC, id DESC)`. Used by all order/return/shipment/
  payout/ledger/ticket/supplier/monthly lists. The `id DESC` tiebreak is
  load-bearing: without it, equal timestamps make pages non-deterministic.
- **Statement cursors** — `(occurred DESC, id, reason)` (FIN-009, unchanged).
- **Reconciliation cursors** — `base64url(product_id)`, sort `product_id ASC`
  (deterministic catalogue order; no timestamp exists on the row).
- **Pending-shipments work queue** — ascending `(created_at, id)` (oldest
  work first); the cursor is a `>` bound, not a `<` bound.

Stability semantics (all families): a row inserted **after** the cursor was
issued that sorts **before** the cursor is never returned on later pages
(standard omission semantics — it is not lost, it belongs to an earlier
page). No row is ever duplicated across pages. Concurrent inserts therefore
cannot shift or duplicate a page; they can only make the total grow.

Only the reconciliation report (full and summary views), the monthly
settlement detail, and the statement read their page and their `total`
inside one repeatable-read transaction, so the page and the total agree
with each other (reconciliation totals were previously computed outside
any transaction and could disagree with the page under concurrent
inserts). The other cursor-paged lists (orders, returns, shipments,
payouts, ledger, tickets, supplier lists) run one keyset query per page
and return NO `total` — their envelope carries `nextCursor` / `hasMore` /
`limit` only. Clients must page to the end instead of expecting a total.

## 3. Report windows

Report endpoints (`seller report`, `ledger filtered`, and any new report)
take `?start=&end=` as `YYYY-MM-DD`:

| Case | Window |
|---|---|
| both empty | default **31 days** ending today |
| end only | the end bound extended back 30 days (`end-30d..end`) |
| start only | `start..today`, **max 366 days**; over-wide → 400 (a future start collapses to a single-day window) |
| explicit | honored, **max 366 days**; `start > end` → 400 |
| malformed date | 400 |

The seller report SQL is unchanged — the Step-0 `orders(store_id, ...)`
index collapsed it from ~21 s to ~22 ms; only the window is bounded.

## 4. Bounded routes (PERF-02)

Default 50 / max 100 unless noted. Envelope key in parentheses.

| Route | Items key | Notes |
|---|---|---|
| `GET /api/v1/orders` | `orders` | No frontend consumer (only `POST createOrder` in `orderApi.ts`) |
| `GET /api/v1/stores/{slug}/orders` | `orders` | Store-orders branch |
| `GET /api/v1/customer/returns` | `returns` | |
| `GET /api/v1/supplier/customer-returns` | `returns` | |
| `GET /api/v1/warehouses/returns` | `returns` | |
| `GET /api/v1/supplier/orders` | `orders` | Still returns `success: true` for the old client shape |
| `GET /api/v1/supplier/shipments` | `shipments` | |
| `GET /api/v1/supplier/shipments/pending` | `shipments` | ASC work-queue order |
| `GET /api/v1/finance/ledger` | `entries` | Windowed (§3); also echoes `dateFrom`/`dateTo` |
| `GET /api/v1/stores/{slug}/payouts` | `payouts` | |
| `GET /api/v1/finance/settlements/pending` | `settlements` | |
| `GET /api/v1/seller/tickets` | `tickets` | Ticket messages capped to latest 50, chronological; `MessageTotal` reports the full count |
| `GET /api/v1/admin/suppliers/records` | `suppliers` | `?status=` filter is pushed into SQL (was client-side in Go, which would break paging) |
| `GET /api/v1/admin/suppliers/list` | `suppliers` | |
| `GET /api/v1/admin/suppliers/pending` | `suppliers` | ASC order |
| `GET /api/v1/admin/monthly-settlements/{id}` | `items` (on the detail struct) | Totals + page share one repeatable-read tx |
| `GET /api/v1/warehouse/inventory/reconciliation-report` | `rows` | **Max 100** (tightened from 200, D1). `?view=summary` (default, counts/flags only, no nested JSONB) or `?view=full` (capped evidence lists). `?productId=` returns the single FULL row for the review modal. Response also carries `total` and `view`. Nested evidence is capped at 50 entries per list in SQL (SUM/COUNT still cover the full set); `EvidenceTotal`/`EvidenceTruncated` report the pre-truncation counts. |
| `GET /api/v1/finance/statement` | (FIN-009 shape) | **Max 200, NOT tightened** (D1: its per-page work is UNION-bound, not row-bound). Totals and page share one repeatable-read tx; pages concatenated equal `GetStoreStatement` month totals. |

N+1 kills shipped with the bounds (same PR): return items are loaded in
one `= ANY($1::uuid[])` query; supplier order pages load item counts
(one GROUP BY) and fulfillment lines (one query keyed by `order_id`)
instead of 2N queries; the origins cast join is `::uuid` on both sides
so the planner uses the `order_items(order_id)` index (25–27 ms → 0.03 ms).

## 5. Response bounds (measured, disposable PG16)

Seed shape: 10,006 products / 20,001 orders / 100,002 ledger entries
(`~/workspace/perf02-impl/measure*.out`); statement scale-up to 100,005
orders in `~/workspace/perf02-impl/scale_100k.sql` +
`measure_statement_100k.out`.

- Order-list page (50 orders + items): 15.2 ms → **0.30 ms**
- Ledger filtered, no dates, LIMIT 50: 29 ms → **0.096 ms**
- Seller report (unchanged SQL): ~21 s → **22–32 ms**
- Reconciliation report page (200 rows, pre-tightening): 8.7 s → **28–47 ms**
- Reconciliation single row, dense product (5,005 movements + 5,001 active
  reservations): 27–106 ms → **0.023–0.038 ms**; capped movements JSONB
  **2,788 bytes** vs uncapped 280,548 bytes
- Statement totals/page at 100k orders: no-index 106–197 ms; 17 Step-0
  indexes 43–97 ms; **20 indexes 8–10 ms warm** (the three statement-leg
  indexes were added in Step 10 after EXPLAIN showed the released-profit,
  finance-adjustment and referral-commission legs seq-scanning)
- Latency gates for the 100-RPS load run are the CAP-01 capacity-contract
  gates: reads p95 300 ms / p99 1000 ms; mutations p95 750 ms / p99 2000 ms

## 6. Pre-existing exceptions (NOT the contract — do not copy)

- **Supplier warehouse-orders: `?page=` / `?pageSize=` OFFSET**
  (`GET /api/v1/supplier/warehouse-orders` →
  `GetSupplierWarehouseOrders` `LIMIT…OFFSET`, plus a per-order item
  N+1). Pre-existing, NOT converted by PERF-02; follow-up task
  (critic N2).
- **Product reviews: `?offset=`** (`GET /api/v1/products/{id}/reviews`,
  in-memory slice paging, limit 1–50). Pre-existing, NOT converted by
  PERF-02; `GetProductReviews` also loads all reviews into memory with a
  per-review `GetUserDisplayName` N+1 (critic observation, follow-up
  inventory only).
- **Statement page: max 200** (D1). Same 400/clamp/default policy,
  different ceiling.
- **`t01ListRows`: silent default 100** on invalid/out-of-range input
  (`team01_helpers.go`). Unchanged by PERF-02; a future cleanup should
  route it through `parseListPage`.
- **Ad-network report: `Limit: 50`** (pre-existing, unchanged).
- **Notifications: `LIMIT 100`; admin users: `LIMIT 100`** (pre-existing,
  unchanged).
- **Catalog envelope (PERF-01)**: default 24 / max 100, its own versioned
  cursor with a SHA256 query binding — see `catalog-pagination.md`.
  Different cursor scheme, same limit policy family.

## 7. Exports: DISABLED

There is **no export endpoint** in the platform, and PERF-02 deliberately
did not build one (D4). No synchronous endpoint may serve a full dump:
every list response is capped by §1 (≤100 rows, ≤200 for the statement),
and the reconciliation full view caps nested evidence at 50 entries per
list per row. Any future export MUST be an asynchronous background job
with all of the following, decided before it ships:

- an explicit **row cap** (max rows per export) and **size cap**
  (max bytes of the produced file);
- a **runtime cap** (max wall-clock per job, after which the job is
  cancelled, not retried blindly);
- **cancellation** (the requesting user can cancel a running export);
- progress reporting and a download that does not hold a request worker.

Until such a job exists, the answer to "export this list" is: page through
the cursor API (§1) or ask for the export job to be built. Do not add a
`?format=csv` / `?limit=1000000` parameter to any list route.

## 8. Frontend obligations

- Page with `nextCursor` / `hasMore`; show a "show more" control when
  `hasMore` is true. Client-side search/filter applies to **loaded rows
  only** — say so in the UI; do not present a filtered first page as the
  whole catalogue.
- The reconciliation tab loads summary pages (`?view=summary`, 50 rows)
  and fetches the single full row (`?productId=`) when a product is opened
  for review — products past the first page are reviewable only through
  that endpoint.
- The supplier orders tab pages the same way (50 rows).
- The statement page has **no frontend consumer** (verified Step 9: no
  `statement` call site exists in `src/`); if one is built it must consume
  the FIN-009 cursor contract, not fetch full months.
- `GET /api/v1/orders` has **no frontend consumer** either.
- Services that unwrap envelopes must accept both the envelope and a bare
  array (`Array.isArray(res) ? res : res.<key> ?? []`) so old cached
  responses and tests keep working.
