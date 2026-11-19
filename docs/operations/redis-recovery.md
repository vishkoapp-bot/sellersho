# Redis — hot-key holds, persistence policy, restart recovery (PERF-04)

## 1. What lives in Redis, and what happens when it is lost

| Data class | Keys | TTL | Canonical source | If Redis loses it |
|---|---|---|---|---|
| Inventory holds | `inventory:holds:<pool>`, `inventory:hold_qty:<pool>:<order>`, `inventory:held_qty:<pool>` (`<pool>` = `<product>` or `<product>:<variant>`) | Hold deadline, ≤ 15 min; quantity key outlives the deadline by a 60 s grace (see §4) | PostgreSQL: `orders` (pending, not captured, deadline in future) + `order_items` | **Reconstructible** — run `redis-recover rebuild` (§5). Until rebuilt, a pending order's payment callback sees no hold and the verified payment is rejected to the refund path (`RejectVerifiedPaymentAdmission`); money is never lost, the customer gets a refund instead of goods. |
| OTP | `otp:<mobile>` 120 s, `otp:cooldown:<mobile>` 30 s, `otp:attempts:<mobile>` 120 s | as shown | None (random code, only in Redis + the SMS already sent) | **Not reconstructible.** The customer must re-request a code. The cooldown key is lost too, so the re-request is allowed immediately. Verification fails closed on a missing code — a lost code can never verify. |
| Catalog cache | `catalog:categories:all`, `catalog:niches:*` | 1 h | PostgreSQL catalog tables | Reconstructible — the next request refills from PostgreSQL (cache miss path). |
| Ad dedup | `ad_token:*` | caller-defined | PostgreSQL ad tables | Partially — a lost token can allow one duplicate CPC charge inside the TTL window. The API is currently uncalled. |
| SMS bulk marker | `sms:bulk:<id>` | 30 d | PostgreSQL `sms_logs` | Reconstructible — the marker is a dedup aid only; `sms_logs` is canonical. |

Pool rule (PERF-04): holds are **per-SKU**. A tracked variant holds against
`inventory:holds:<product>:<variant>` bounded by the variant's own stock;
anything else holds against `inventory:holds:<product>` bounded by product
stock. PostgreSQL capture (`captureOrderInventoryTx`: row-locked conditional
`stock >= qty` decrement + immutable `order_inventory_captures`) is the
authoritative backstop — Redis over-admission can delay a payment, it can
never oversell.

## 2. Persistence / eviction / memory policy (in the compose files)

The Redis command in `docker-compose.yml`, `docker-compose.prod.yml` and
`backend/docker-compose.yml` carries the policy explicitly (verify with
`docker compose -f docker-compose.yml -f docker-compose.prod.yml config`):

- `--appendonly yes` — every write is logged to `redis_data:/data`.
- `--appendfsync everysec` — at most ~1 s of writes can be lost, and only
  on a **host crash / power loss** (see §3).
- `--maxmemory 512mb` — explicit ceiling. Holds + OTP + cache for this
  workload are megabytes; 512 MB is a generous bound, not a usage estimate.
- `--maxmemory-policy noeviction` — Redis **never evicts** a hold or an OTP
  code to make room. Under memory pressure, cache writes fail with `OOM`
  instead (fail-closed; the endpoint falls back to PostgreSQL, which is
  already its cache-miss path). An `allkeys-lru` policy was proven in the
  PERF-04 audit to evict live holds and OTP codes under a cache flood —
  never use it here.

## 3. Restart recovery drill (what was proven, 2026-11-19)

Setup: scratch Redis 7.0.15 with the production-equivalent command
(`--appendonly yes --appendfsync everysec`), one OTP key, one cache key,
one hold (ZSET + quantity key).

| Scenario | Result |
|---|---|
| Clean `SHUTDOWN NOSAVE` + restart, same data dir | 4/4 keys survive |
| `kill -9` immediately after writes, 3 rounds | 3/3 rounds, all keys survive |
| Host crash / power loss | **Not simulable by killing the process.** With `everysec`, up to ~1 s of the most recent writes can be lost. That is the only loss window. |

Why a process kill loses nothing: AOF writes go to the OS page cache on
every command; `everysec` only controls the `fsync` to disk. Killing the
process leaves the page cache intact, so the restart replays everything.
Only a host crash discards the un-fsynced tail.

After **any** restart, before declaring recovery complete:

1. `redis-cli ping` → `PONG`, and the backend health check reports Redis up.
2. Spot-check a live hold: `ZSCORE inventory:holds:<product> <order>` and
   `GET inventory:hold_qty:<product>:<order>` both return values.
3. Check the aggregate: `GET inventory:held_qty:<product>` must equal the
   sum of live quantity keys; if it does not, run `redis-recover reconcile`
   (§5) for that pool.
4. If the restart followed a **host crash**, assume up to 1 s of writes is
   gone and run §5's pending-order reconcile as well.

## 4. How the hold aggregate works (for the on-call reader)

- `inventory:held_qty:<pool>` is the sum of live holders' quantities,
  maintained **inside the same Lua scripts** that mutate the ZSET and the
  quantity keys, so it cannot drift within a script. Admission reads the
  aggregate — O(1) — instead of scanning every holder (the old script was
  O(holders): p95 23 ms at 10,000 holders, and it degraded OTP verification
  ~78× while it ran).
- Expiry cleanup reaps at most **128** stale members per hold call.
- The aggregate can only ever **over-count**: a quantity key TTL-reaped
  before cleanup, or a member removed without its decrement, leaves a
  phantom quantity behind. Over-counting is the safe direction (it can
  reject a hold that would fit; it can never admit an oversell). A
  rejection is therefore re-checked against an exact full scan, which also
  heals the aggregate — the scan runs only on the reject path.
- The quantity key outlives the ZSET deadline by a **60 s grace** so the
  cleanup decrement almost always finds the key present and exact. The
  hold itself still ends at the ZSET deadline: admission and
  `HasInventoryHold` read the score, never the key TTL.
- If the aggregate key is missing (first hold on a pool after an upgrade,
  restart loss, manual deletion), the hold script rebuilds it once from a
  full scan inside the same atomic script.
- Releases are best-effort across all of an order's pools: one pool's Redis
  error no longer leaks the remaining pools' holds (PERF-04 D-03). A
  release failure is logged (`redis_release_failed`) and joined into the
  returned error. Any residual leak is bounded by the hold TTL (≤ 15 min)
  and the 5 s unpaid-order-expiry job.

## 5. Recovery procedures

### 5.1 Pending-order reconcile (after Redis data loss)

Build the maintenance binary from the **same pushed commit as the deployed
server** (it is not part of the server image):

```sh
cd backend && go build -o /tmp/redis-recover ./cmd/redis-recover
```

Run it on the server host with the server's environment (credentials come
from `DATABASE_URL` / `REDIS_URL` in the environment, never as arguments):

```sh
DATABASE_URL='<from /opt/drup/.env>' REDIS_URL='<from /opt/drup/.env>' \
  /tmp/redis-recover rebuild
```

What it does: re-creates holds for every order that is still payable
(payment pending, status pending/processing, inventory not captured,
deadline in the future), with each order's own deadline capped at 15
minutes. It prints the rebuilt pool count; per-order failures are printed
and do not stop the remaining orders (exit 1 if any failed — re-run after
fixing the cause). Cancelled, expired, paid, and captured orders never get
a hold back. Re-running it is idempotent.

Afterwards, verify: for a sample of pending orders,
`redis-cli ZSCORE inventory:holds:<product> <order>` returns the order's
deadline, and the unpaid-order-expiry job's backlog gauge is not growing.

### 5.2 One pool's aggregate disagrees with the scan

```sh
DATABASE_URL='…' REDIS_URL='…' /tmp/redis-recover reconcile -product <product-id> [-variant <variant-id>]
```

Recomputes the pool's aggregate from a full scan and stores the exact sum;
prints the sum. Run it when no holds are being placed on the pool, or
accept that a concurrent hold may move the sum again right after.

### 5.3 A customer must re-verify OTP (after OTP loss)

1. Tell the customer to request a new code (login / verification screen).
   The cooldown key is lost with the code, so the re-request is allowed
   immediately — no waiting out a phantom 30 s cooldown.
2. The new code is a fresh random code with a fresh 120 s TTL; the old
   code is gone and can never verify (fail-closed).
3. No action is needed for already-verified sessions: session/JWT state is
   not in Redis.

### 5.4 Stale-hold bound

- A hold whose order expired in PostgreSQL is released by the
  `unpaid-order-expiry` job within ~5 s of the PG deadline (one pass every
  5 s, 200 orders per pass, per-order retry/review states on failure).
- A hold with no live order behind it (orphaned by a crash between the
  Redis hold and the PG order write) ends at its own deadline, at most
  15 minutes after it was placed, via ZSET expiry + the 60 s quantity-key
  grace.
- A hold that survives both (Redis lost the ZSET member but kept the
  quantity key, or vice versa) is harmless: admission reads the aggregate
  and the ZSET together, and the next hold on that pool cleans it up
  (bounded 128/call) or the reject-path rescan heals the aggregate.

## 6. Budgets (gates G-01..G-04, measured 2026-11-19 on scratch Redis 7.0.15)

| Measurement | Budget | Audit baseline |
|---|---|---|
| Hold p95, 10,000 holders, serial | ≤ 5 ms | 22.99 ms |
| Hold p95, 10,000 holders, 8 concurrent clients | ≤ 30 ms | 88.4 ms |
| OTP verify p95 under continuous hot-key hold load | ≤ 2 ms | 21.84 ms (idle 0.28 ms) |
| First hold after a 10,000-expired-member burst | ≤ 10 ms | 31.2 ms |

Re-run the harness in `~/workspace/perf04-work/bench/` (exact Lua copied
from `backend/internal/repository/redis.go`) after any change to the hold
scripts; the Go tests in `backend/internal/repository/redis_inventory_test.go`
pin the semantics (aggregate == scan, re-hold/double-release, SKU
admission, best-effort release, burst repair).
