# PERF-03 connection budget (DB pool)

Measured Nov 20, 2026 on the disposable PERF-03 fixture (1,012 users,
536 products, same shape as the audit) and verified live read-only on
prod the same day. Local-VM RPS numbers only — prod-hardware identity
stays OPEN for CAP-02, so no RPS figure here certifies production
capacity.

## The budget

| Deployment | Pool | Prod max_connections | Headroom line |
| --- | --- | --- | --- |
| Today, 1 replica | 25 open / 10 idle | 100 (verified live Nov 20, 2026, read-only `SHOW max_connections`; `superuser_reserved_connections` = 3) | 25/100 = 25% of connections |
| + 2nd replica | 50/100 | 100 | 50% |
| + 3rd replica, or replica + migration | 75–77/100 | 100 | **crosses the 80% headroom line — not allowed without a max_connections change or a pool cut** |

Constraint (D4): no 2nd replica is planned tonight. The 50/100 budget
and the 80% headroom line are the constraint for any future replica
decision. Steady-state headroom must stay <80% of the allocated DB
connection budget; the pool-wait p95 gate is <25 ms (see measurement
caveat below).

## Pool configuration (G4)

`DB_POOL_MAX_OPEN` (default 25), `DB_POOL_MAX_IDLE` (default 10),
`DB_POOL_MAX_LIFETIME_SECONDS` (default 0 = unlimited),
`DB_POOL_MAX_IDLE_SECONDS` (default 0 = unlimited). Validated at boot
in dev and prod: maxOpen 1..100, maxIdle 0..maxOpen, lifetimes 0..86400 s.
`cmd/migrate` and `cmd/redis-recover` are capped at 2 open / 1 idle
(they previously used uncapped `sql.Open`).

No pool-size increase was made: the spec requires demonstrated
improvement before increasing pool/replicas, and the measurements
below show the pool was never the binding constraint at c20 (zero
waits on both binaries) and the catalog cap is query cost (G6), not
pool size.

## Before / after (same day, same fixture, same VM, 30 s runs)

Old binary = audit build from e48a752; new binary = PERF-03 head.
Absolute ms are VM-noise-dominated (shared VM, host load 8–10); the
robust findings are query counts, wait counters, throughput ratios
and the 500 count.

| Profile | Old RPS | New RPS | Old p50/p95/p99 ms | New p50/p95/p99 ms | 500s old → new |
| --- | --- | --- | --- | --- | --- |
| A auth-me c20 | 257.9 | 449.7 (+74%) | 75.6 / 188.6 / 265.6 | 27.3 / 98.6 / 182.9 | 0 → 0 |
| B catalog c20 | 28.1 | 31.5 (+12%) | 685.6 / 901.4 / 1841.1 | 591.2 / 881.3 / 2667.5 | 19 → 0 |
| C catalog c50 | 29.8 | 31.2 (+5%) | 1559.8 / 2553.5 / 3515.2 | 1459.9 / 2821.8 / 3924.1 | 0 → 0 |
| D auth-me c50 | 279.2 | 464.5 (+66%) | 140.6 / 372.8 / 522.3 | 90.0 / 216.8 / 334.4 | 0 → 0 |

Audit numbers for the same profiles (Nov 19): A 230.6 RPS / 81 ms p50,
B 25.3 RPS / 718 ms p50 with 19×500, C 25.2 RPS / 1774 ms p50,
D 234.3 RPS / 180 ms p50. The same-day old-binary re-runs reproduce
them within noise, including exactly 19×500 on B again.

### What changed and why

- **G1 auth queries: 2.00 → 1.00 per request.** Old middleware ran
  `IsUserActive` + `ListActive` (7,751 calls each for 7,745 auth-me
  requests). New `LoadActiveState` is one indexed query returning
  `is_active` + capabilities. The direction 2→1 is proven (legacy
  queries flat in the new runs, delta 0), but the exact "13,497 calls
  for 13,497 requests" ratio is NOT reproducible from the preserved
  files: `run-A-new/statements-*` shows the combined query going
  0 → 15,402 (likely `run_profile.sh` snapshots `pg_stat_statements`
  with no `dbid` filter and/or requests outside the counted window).
  The auth latency/throughput gain above is this fix. Blocked/capability
  state is still read live per request — no TTL, no cache.
- **G2 catalog settings loads: 2× → 1× per request** (scope-tx load
  reused for response pricing; proven by pg_stat_statements deltas in
  `TestPERF03CatalogListLoadsPricingInputsOnce`).
- **G7 19×500: root cause was PostgreSQL serialization failures
  (40001), not the pool.** On a cold price index, concurrent
  RepeatableRead snapshot transactions refreshing the same stale
  `catalog_public_prices` rows collide and all but one lose with
  `could not serialize access due to concurrent update`. Fix: one
  bounded retry of the whole snapshot tx
  (`retryCatalogTxOnSerialization` in `cmd/server/handlers.go`,
  applied to the product list and the price-bounds handler). The
  retry re-probes against the winner's committed refresh and finds no
  stale rows. An advisory lock was tried first and REJECTED by its own
  regression test: it serializes the refresh but cannot refresh the
  loser's snapshot, taken at the tx's first read before the lock.
  Catalog p99 rose slightly at c20/c50 (retried requests run two txs)
  while errors went to zero — that trade is intended.
- **G3 OTP isolation:** OTP sends now run on their own 8/s shedder
  executor; admin reads keep the shared one. Rate unchanged at 8/s
  (calibration OPEN pending IPPanel's real limit).

### Pool waits and connections

- c20 (A and B): **zero pool waits on both binaries** — the pool is
  not the constraint at this load.
- Catalog c50: waits 15,437 (old) → 9,645 (new) per 30 s run (−38%).
- Auth-me c50: waits 8,541 (old) → 732 (new) per 30 s run (−91%).
- Max PG connections observed in any run: 26 total (25 pool + 1),
  max 17 active. 26/100 = 26% — inside the 80% headroom line.
- **G5 wait histogram** (`sellersho_db_wait_seconds`, in /metrics):
  one sample per 100 ms tick with new waits = the interval's mean
  wait. During C-new (catalog c50): 283 samples, interval-mean p95
  in (100 ms, 250 ms], max interval mean 206 ms. Caveat: this is the
  p95 of per-interval MEANS, not of individual waits — it
  underestimates the true per-wait tail. The p95<25 ms gate is met at
  c20 (zero waits) and NOT met at c50 catalog; that is overload (50
  clients on a 25-connection pool serving ~1.5 s transactions), not
  steady state, and is justified by the measurement: waits appear
  only above pool concurrency, and catalog throughput is capped by
  query cost (G6), so raising the pool would not raise throughput.

### D5: absolute-latency attribution

Auth-me p50 81 ms (audit) vs ~3.6 ms DB time is NOT explained by log
writes: the same old binary with stdout → /dev/null instead of a file
measured 72.7 ms p50 / 307.5 RPS vs 75.6 ms / 257.9 RPS to a file
(−4% p50). Per-query logging DOES exist (one `log.Printf` line per
SQL query, `component=sql operation=query`, plus one per HTTP
request — ~87k lines in one old-server log), but the write path is
not the bottleneck; the residual is VM-noise-dominated plus
per-request Go/HTTP overhead outside the DB. There is no env flag to
disable operation logging, so a disable/sample change would be code
work with no evidence it moves the needle — not done here.

## Carry-forwards (NOT fixed here)

- **G6** (PERF-01-path follow-up): catalog throughput caps at ~25–31
  RPS regardless of pool size; `GetProductsByIDs` path is the
  suspect. Do NOT "fix" by raising the pool.
- **G8** (PERF-02 carry-forward): product-reviews in-memory + N+1.
- **G3 calibration**: OTP token rate stays 8/s; the real IPPanel
  limit is still unknown.

## Files

Implementation: `backend/internal/repository/capability_repo.go`
(G1), `backend/internal/auth/jwt.go` (G1),
`backend/cmd/server/auth_handlers.go`, `backend/cmd/server/handlers.go`
(G1/G2/G7), `backend/internal/ippanel/client.go` (G3),
`backend/internal/config/config.go`,
`backend/internal/repository/postgres.go`, `backend/cmd/server/main.go`,
`backend/cmd/migrate/main.go`, `backend/cmd/redis-recover/main.go`
(G4), `backend/internal/observability/metrics.go`,
`backend/internal/observability/sql.go` (G5/G7 instrumentation).
Evidence: `~/workspace/perf03-impl/run-{A,B,C,D}-{old,new}/`,
`run-G7-repro-catalog20/`, `run-G7-repro-new/`.
