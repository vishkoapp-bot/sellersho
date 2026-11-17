Sellersho LOCAL capacity preparation (CAP-01 IN PROGRESS; CAP-02 OPEN)

Run from repository root:
  python scripts/test_load_test_isolated.py
  node --test load-tests/baseline-policy.test.mjs load-tests/sms-double.test.mjs
  python scripts/test-isolated.py -- go test ./cmd/capacity-fixture -run '^TestCapacityDataFloor$' -count=1 -v
  powershell -File scripts/load-test-isolated.ps1 --baseline
  python scripts/load_test_isolated.py --fixture-baseline
  python scripts/load_test_isolated.py --login-ramp
  python scripts/load_test_isolated.py --otp-background
  python scripts/load_test_isolated.py --api-rolling
  python scripts/load_test_isolated.py --journey-baseline

Enabled profiles:
- --baseline: ten-second, 100-arrival/sec TLS health probe.
- --fixture-baseline: full datafloor, 1000 distinct signed customer sessions,
  one auth/me call per customer over ten seconds, response ownership assertions.
- --login-ramp (alias --otp-login): actual customer OTP send + verify, 1000
  customers over 60 seconds, 2000 API calls (33.33 API RPS); private SMS read
  helpers are separately counted. No bypassed challenge or real SMS.
- --otp-background: same OTP flow + 4000 browsing requests over 60 seconds,
  6000 offered API calls = 100 total API RPS. All failures/drops remain failed.

A one-request trusted-TLS/domain setup smoke runs before expensive seeding.
Read p95/p99 gates remain 300/1000ms; login mutations use 750/2000ms. No warmup
is used for these SHORT probes. They do not establish sustained/soak readiness.
The earlier password-login experiment correctly returned 1000x401 because the
application limits that endpoint to an allowlisted super-admin. Customer password
provisioning was removed; login-ramp now uses the intended customer OTP flow.

Output: new Temp directory with raw request ordinal/status/timing/assertions,
service versions, binary/source/migration/image digests, private backend metrics
before/after, received API/domain/probe counts, provider SMS counts and resource
sample. HTTP bodies, tokens, passwords, runtime environments and private TLS keys
are never written to reports. Ephemeral fixture sessions stay in private temporary
storage and are removed with the environment. A single resource sample cannot
establish sustained CPU or growth acceptance; disk class remains unmeasured.

Isolation: only fresh random Docker names/labels/internal network; no public ports,
existing target URL/DSN/reuse/keep mode. New non-superuser database owner and marker,
tmpfs Postgres/Caddy, explicit app environment. Identity/network verified before
SQL/load and targeted cleanup. No pruning or existing service changes. App source,
embedded migration checksum file, migrations and load scripts are copied before
build: the runner never mounts mutable source. Paused networkreward and docs/fa
are excluded. HEAD is reference only; uncommitted source is identified by digest.
Installed image IDs are recorded, without claiming release patch certification.

TLS path: Caddy internal CA -> domain authorization -> nginx -> real backend.
Only the public root certificate is exported to the generator; chain and hostname
verification are enabled. No ACME. Private key stays in Caddy tmpfs. The runner builds a fresh source snapshot of the frontend and mounts the deployed
nginx configuration. --browser-assets uses internal Chromium when its isolated
image can be built; --browser-relay is the approved bounded host-browser fallback
for UI/count evidence only. All relay timings are excluded from capacity gates. The
Node generator is a separate container, uses keep-alive, no redirects/retries,
and explicitly counts dropped arrivals. OTP uses a bounded 1000-worker/socket pool;
the earlier 200-worker failure remains evidence. Worker saturation alone does
not prove generator causality: inspect route/SQL/pool/provider/CPU/lag evidence.

Providers: internal network blocks Internet egress. SMS uses an in-memory private
HTTP double with ephemeral key, synthetic recipient restriction, bounded delay,
success/reject behavior, at most 1000 retained codes and independent send metrics.
Payment uses the committed explicitly test-gated capacity double. The double
supports bounded verification latency/success/reject/wrong-amount/timeout; it has
no network client. Inquiry/refund/restart ambiguity still require FIN010 integration.
Carrier quote uses a private internal HTTP double with a fresh ephemeral key and
50ms default latency. Real shipment registration/dispatch is not implemented.
Adapters require exact APP_ENV=test and the private run marker; ordinary real/mock
application defaults remain unchanged. No real provider credentials are supplied.

Guarded fixture: internal/testdb.Open, actual disposable migrated PostgreSQL,
no disabled triggers. Full runner seeds1000customers,100sellers/stores/suppliers,
100owned origins,10000demand products,50000legacy SKUs plus100aged central products and one separate hot-SKU product/variant.
Five million current sellable demand units and2000distinct aged central units have
receipt provenance. Historical100000orders contain140000lines with varied quantities
and60%delivered/10%shipped/10%processing/10%cancelled/10%pending status distribution,
accepted immutable money snapshots,conserved supplier origins,80%paid transactions,
historical receipts/captures and paired posted releases.100releases and100pending
payout reservations execute real repository helpers with idempotent replay and
current/prior-month posted/reserved reconciliation.100aged positions cover10..110days;
fee assessment is tested in a disposable flag-enabled variant then restoredOFF.
This synthetic fixture does not certify production history or full return/refund
correctness. Background workers pause only while provisioning/asserting fixtures,
then normal scheduling resumes before measured load.

Commerce profiles (Node equivalent,not dependent on installed k6):
--api-baseline: exact10seconds/1000calls,240separate preparation calls.
--api-rolling:30seconds;--api-sustained:30minutes;--api-soak:2hours.
One API call per arrival,exact30/20/15/15/8/4/4/4 mix. Rolling bounded lifecycle
queues preserve owner/idempotency/deadline and never extend production holds.
Bootstrap helpers and post-run recovery reads are separately counted; bounded
mutation recovery consumes its declared API slots. Missing/expired/ambiguous work
remains failed. The30second local run failed;long durations are not executed.
--journey-baseline:90seconds;--journey:30minutes.1000independent closed-loop carts,
orders and sessions,uniform5..15second think time;actual throughput reported.
The90second run failed at catalog reads,with zero complete checkouts. Neither
profile implies100RPS or certification. api-100rps.js is an older prepared k6
alternative,not used by the runner. See profiles.json and local-baseline-evidence.md
for implemented versus open profiles and retained measurements.

Additional prepared diagnostics:
--hot-sku:100customers contest five units;duplicate callbacks and post-run stock/
payment oracle. --staff-baseline:980customers+20staff at100API RPS,30seconds,
real role-scoped reads. These do not replace the full qualification.
--browser-relay:10anonymous contexts/cold+warm actual built UI. A127.0.0.1
ephemeral socket forwards encrypted bytes only through a uniquely marked internal
relay container to that run's Caddy tls:443. No arbitrary host/port arguments.
Exact container/network guards run before listening and each connection;32active
connections,64MiB per direction,120s per connection,240s whole relay maximum.
All subprocess pipes are drained, children terminated and listener closed before
Docker cleanup. Host browser uses normal sandbox and only this ephemeral CA SPKI.
External page requests are blocked; browser and nginx/domain/API counters reconcile.
Relay timing and unmeasured host-browser CPU are excluded from capacity evidence.

Not yet completed:full price/ledger/refund oracle,hot-account,fault/restart/expiry/
device,continuous200RPS headroom+recovery,qualified cold/warm cache comparison,
full-duration commerce/journeys/staff and production-hardware CAP02. See current
cap01-report.md and local-baseline-evidence.md for exact verdicts.

Review fix 1: arrival and cleanup policy
--------------------------------------
All executed Node open-loop drivers now share ArrivalScheduler: a slot is due at
(ordinal+1)/rate seconds, with a fixed 10ms generator dispatch tolerance. Slots
late by more than 10ms are recorded as missed_schedule drops and are never replayed
in a catch-up burst. Every actual call records scheduled time and dispatch lag;
a late actual dispatch also fails the scheduling gate. Client attempted/received
response rates use actual elapsed time including graceful completion, separately
from planned_api_rps. Server API/domain counters still reconcile total calls,
including explicit helpers; they are not misreported as a measured-only rate.

Applied consistently to commerce-baseline, commerce-rolling (including staff),
baseline health/auth and login-otp flow/browsing schedules. OTP verify is dependent
fan-out, so the combined 100 API RPS plan is nominal; actual API throughput and
missed flow/API slots are reported. Closed-loop users-1000 already reports actual
throughput; hot-SKU is a declared burst, not a paced rate. Browser is UI/count only.
Legacy unexecuted k6 alternatives are not driven by this runner or covered by this
scheduler guarantee. Older raw runs retain their original outcomes and scheduling
limitations; zero recorded drops in an old run is not proof of on-time arrivals.

Container attachment NetworkID must equal the independently verified internal
network ID; names alone never authorize operations. Cleanup rechecks network
ID/name/internal/label before each container removal. Temporary source deletion
checks the captured canonical direct-child path, device/inode identity, resolved
temporary parent, symlink/junction refusal immediately before recursive removal.
Unsafe-target regressions use mocked deletion only; no unsafe filesystem target
is touched. CAP01/CAP02 remain OPEN; no production deployment proof follows.

Fresh container startup uses Docker --network=<immutable network ID>. Before
start, inspect must show the exact created container identity, created state and
HostConfig.NetworkMode equal to that ID (Docker has not populated attachment ID
yet). After start, the strict populated attachment NetworkID check is mandatory.
There is no exception for existing/running containers or destructive cleanup.
