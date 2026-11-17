# Request and dependency budgets (OPS-02)

These are per-process operating limits, not a certificate of production capacity.
The production-equivalent1000-user/100-RPS acceptance remains open.

| Class | Go operation | Go socket write | nginx upstream read | Caddy header wait | Browser |
| --- | --- | --- | --- | --- | --- |
| Ordinary API |8s|10s|13s|14s|15s|
| POST image upload collections |60s|65s|68s|70s|75s|
| Internal domain authorization |2s|3s|—|4s|internal only|

The backend header/read/write/idle ceilings are5/65/70/60s; per-route socket
limits shorten them. Static backend downloads have a20s operation/22s write
limit. Caddy applies hard header/body/write/idle ceilings5/65/75/60s. Its
header-wait limit is distinct from an entire response-body deadline.
[Official Caddy transport reference](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy),
[server timeout reference](https://caddyserver.com/docs/caddyfile/options#timeouts).

Image uploads are only POST `/api/v1/upload/product-image`,
`/api/v1/supplier/products/{id}/images` and
`/api/v1/supplier/products/{id}/variants/{variant}/images`.
GET collections and reorder actions retain the ordinary limit. nginx's named
upload route retains method/body; API request buffering is disabled so the
backend deadline starts while the body streams. Existing image-size caps remain.
Synchronous exports stay on8s and existing bounded result limits. Larger exports
need a durable job (JOB-01), not arbitrary larger synchronous waits.

The middleware admits64 ordinary requests and4 uploads with no pending queue.
Rejected admission returns503 with Retry-After1. It executes handlers in the
serving goroutine, passes the deadline to existing context-aware SQL/Redis/provider
calls, preserves ResponseController unwrapping, and never detaches timed-out work.
A504 before headers has a human-readable error and separate `request_timeout`
code. For a mutation it explicitly says outcome unknown; it must be resolved via
its stored checkout/finance/shipment identity. Already-started responses abort,
so a partial success cannot replace recovery. Caller cancellation and the
browser's own finite timeout both apply. There is no automatic browser mutation
retry; uploads also have a finite75s client bound.

## Providers and Redis

| Dependency | In-flight | Rate / burst | Operation | Retry |
| --- | --- | --- | --- | --- |
| Tapin |16|32 per second /32|5s|safe reads only, at most2 attempts|
| IPPanel |8|8 per second /8|5s|credit/recipient reads only, at most2 attempts|
| Redis |32 active connections|pool wait1s|dial1s/read2s/write2s, shortened by caller|no automatic command retry|

Provider clients share transport, admission and token-bucket state across new
instances. Reads get2s attempts within the5s operation and bounded exponential
jitter100-250ms. Only transport failures and408/429/5xx may retry; business
rejections do not. Backoff and response-body reads honor cancellation. Tapin
options/check-price/filter POSTs are explicitly reads; label GET is a read.
Registration, OTP SMS and unknown commands never replay automatically. Existing
PSP/refund operation identities and fail-closed production adapters remain;
FIN-010 must apply the same contract to the actual PSP integration.

A Tapin send first claims its existing pending/processing, unregistered shipment
atomically. One successful claim owns any later definitive no-send/rejection
cleanup; concurrent stale readers receive409 and cannot send or clear another
owner's guard. Local overload may rearm that exclusive guard because no request
was sent. Unknown registration keeps the marker, returns explicit unknown /
retryable=false, and requires inquiry before another send. This narrow guard
does not close INV-12's full shipping command/reconciliation acceptance.

The existing OTP30s cooldown,120s validity and five-guess guard remain. Code and
cooldown generation are reserved atomically before SMS; unavailable/unconfirmed
Redis means no send. Unknown delivery retains a usable code and cooldown, so
response-loss retry cannot create another message. Definitive rejection cleanup
compares both code and generation, never deleting a newer reservation. Cleanup
uses the current request with a250ms cap; cancellation never creates detached
cleanup work. No OTP is returned in responses. SMS logs distinguish unknown from
failed. Redis uses context-aware sockets and no hidden command retry, including
in the marker-verified disposable fixture.

Fixed `tapin_request` / `ippanel_request` telemetry records overload, timeout,
cancellation and unknown results without URLs, arguments, tokens or response
bodies. Actual504 responses remain timeout observations even if the client then
closes its connection. HTTP503 admission rejects are counted by existing bounded
route telemetry. Provider limits multiply with replicas; cluster quota enforcement
and external collector acceptance remain separate open work.

## Evidence

`python scripts/test_request_budgets_edge.py` validates the actual configs with
network disabled, then uses only labelled containers on an internal Docker
network. Probes run inside that network; no host service/port is replaced and no
Internet provider is contacted. An upstream delayed14.5s produces ordinary504
at about13s, permits a POST image upload, preserves its body/method, and leaves
GET image collections on the short budget. `/internal/*` must produce404 before
proxy/auth routing. Explicit Caddy route order preserves this invariant.

Other acceptance uses local httptest providers plus disposable, marker-verified
PostgreSQL/Redis. It covers SQL cancellation/pool recovery, response loss after
committed checkout, client disconnect, frozen stock/ledger/order invariants,
uncertain SMS/code verification, generation cleanup, stalled Redis and concurrent
exclusive Tapin claims. No real PSP transfer or SMS is performed.

Post-dispatch HTTP errors, including400/429, retain an unknown mutation outcome.
An IPPanel rejection with an issued reference is contradictory and retains the
OTP reservation. Missing/null/nonpositive Tapin envelope status retains the
registration guard. Only a complete business rejection may release those guards.

Provider redirects are returned as responses rather than followed, including303,
307 and308. They cannot replay a send or bypass the attempt/rate counters. A
mutation with a redirect response retains an unknown outcome.
