# Operational automation

The operations features use migration `202611090001_operations_automation` and the existing JWT capability and ownership guards. Network-reward allocation remains paused and is not connected to any operation here.

## Seller domains and automatic HTTPS

The seller's store setup panel can request a domain within the current plan quota. A request enters the existing admin approval inbox. The seller must publish the exact TXT token at `_sellersho-verification.<domain>` and point **all** A/AAAA records to the platform edge. Verification checks both ownership and routing, using DNS rather than fetching arbitrary URLs. Domains must be ASCII DNS names; international domains use punycode.

After approval and DNS verification, Caddy may issue a certificate on the first HTTPS visit and renew it automatically. The permission endpoint rejects every other hostname. A per-request forward-auth check also rejects disabled domains even if Caddy already cached their certificate. Primary platform domains are explicitly allowlisted with `PLATFORM_DOMAINS`.

Deployment requires public ports 80 and 443, persistent Caddy certificate storage, DNS control, and these environment settings:

```text
ACME_EMAIL=certificates@example.com
DOMAIN_EDGE_HOST=edge.example.com
PLATFORM_DOMAINS=example.com,www.example.com
```

`ACME_EMAIL` is recommended but optional. `PLATFORM_HTTP_HOST` optionally preserves an existing IP-based HTTP entry point; other HTTP hosts redirect to HTTPS. The existing IP-based TLS listener on port 9443 is independent of Caddy.

Load these into the shell or Compose environment, then apply the overlay:

```powershell
docker compose -f docker-compose.prod.yml -f docker-compose.ssl.yml up -d --build
```

The overlay moves public listeners from Nginx to Caddy. Use a recent Docker Compose supporting `!reset`. The existing Nginx application routing and redirects are retained. Trusted proxy CIDRs must match the actual internal deployment network; the overlay sets the common Docker private networks. The backend trusts `X-Real-IP` only from those configured peers, and Nginx resolves the real client through Caddy's forwarded chain.

For the existing production installation, use `./deploy.ps1 -EnableSSL`. It builds Linux backend and frontend artifacts from the pushed commit, uploads a checksum-verified release, preserves runtime secrets, backs up PostgreSQL and current code on the server, deploys with health checks, and restores the previous code if checks fail. Existing SSL edge configuration is preserved on subsequent ordinary `./deploy.ps1` runs. It never restores a database backup over new orders. The explicitly authorized one-time Xray port change uses `-EnableSSL -MoveXray443`, moving its 443 inbound to 24443; Xray clients using that inbound must update their port. Release metadata is recorded in `/opt/drup/.deployed-commit`, and full source/artifacts remain under `/opt/drup/releases/<commit>`.

The `/api/v1/domain-store` endpoint resolves the verified request Host to its store slug. The SPA scopes domain root and product entries to that store. DNS validation and Caddy configuration can be tested locally; a publicly trusted certificate requires a real controlled domain pointing at the deployed edge. The source implementation does not deploy or issue a public certificate merely by building.

## Picking routes and nearest suitable bins

Logistics/admin users record bin coordinates in the putaway panel. All coordinates must share a warehouse coordinate system, in metres. The start point is the operator's current position or packing desk. Travel uses Manhattan distance along warehouse aisles; it does not model obstacles absent from the coordinate data.

The picking planner uses paid warehouse/mixed orders and actual canonical bin quantities; legacy bin stock is considered only for products without canonical inventory. It distributes quantities across bins and avoids planning the same stock twice within a batch. Unlocated, insufficient, or other-warehouse items are explicitly reported as missing; a partial route is never labelled complete. Planning is advisory and does not reserve stock across separate batches.

Tours of up to 14 bins use exact Held-Karp TSP. Larger tours, bounded to 200 bins and 50 queued orders, use nearest-neighbour plus 2-opt and are labelled approximate. Every tour returns to the supplied start point.

Bin suggestions require verified measurements, verified physical capacity, known coordinates, the same warehouse, and sufficient unoccupied/unreserved volume **and weight**. Required cubic centimetres are calculated from millimetres with upward rounding and overflow checks. Assignment rechecks capacity and warehouse under the bin row lock, so a suggestion is not a capacity guarantee until assignment succeeds. Confirmation rejects measurements changed after assignment and requires reassignment. Existing capacity reservations are preserved; previously mismeasured values require ordinary operational reconciliation rather than a silent data rewrite.

## Advertising fraud signals

The client hashes a first-party visitor identifier and limited browser traits. It sends the digest and automation signals, not the raw trait list. The server binds a short-lived signed challenge to campaign, fingerprint, user agent and network identity. It rejects webdriver/common Headless Chrome, Puppeteer, Playwright, Selenium and PhantomJS signals, invalid signatures, expired proofs and clicks less than one second after the proof was issued. Client code never sleeps to manufacture a valid dwell time. Impressions establish a cached proof for subsequent clicks; clicks without a sufficiently old valid proof are unverified and are not counted.

IP and fingerprint each permit at most one accepted event of a given type per campaign per rolling 24 hours. Deduplication and accepted-event insertion share one database transaction. Rejections are recorded for the existing fraud reports. Challenge issuance is limited to 60 requests per network identity per minute. Fingerprints and IP identities are HMAC protected on the server. The old proof-free `/ads/click` endpoint returns 410.

These checks detect ordinary automation and replay; browser signals can be spoofed by a determined client. They are not a proof that a visitor is human. Rejected events do not enter the accepted-event counters.

## QC response deadlines

The database assigns a 48-hour response deadline when a discrepancy is created. Suppliers can respond only to their own open, unexpired discrepancy and cannot overwrite a recorded response. The server rejects late responses even before the next scheduled sweep.

An unanswered overdue case is escalated for human review, without accepting the shortage or creating any financial charge. The admin operations panel lists overdue cases and records the reviewer's resolution and audit entry. This preserves the established successful-response behaviour while enforcing the new deadline.

## Automatic direct-shipping SLA grants

Every operations cycle recomputes on-time dispatch performance over the last 90 days from supplier shipments. The deadline is shipment creation plus the applicable product preparation days, bounded to 1–30 days. Shipped records, failed records, and overdue unshipped records enter the denominator. Unpaid or cancelled orders are excluded.

At least one assessed record and a score **strictly greater than 98%**, an approved supplier, and an active owner are required. No-history suppliers do not qualify from the old default score of 100. The grant is written to the actual `supplier_shipping_methods` table used by quote/order routing. Explicit administrative decisions are preserved, including a prior denial followed by a supplier re-request. A system-owned grant is disabled if eligibility degrades. Existing orders retain their persisted fulfilment origins.

The supplier shipping-method panel displays score and sample count; the admin operations panel shows the network assessment. A system-owned grant permits the method; the existing product flag and warehouse/direct method combination still choose each order line's route.

## Unpaid-order expiry and scheduling

New orders get a database payment deadline 15 minutes after creation. Legacy pending orders receive at least a full 15-minute grace period at rollout. The operations worker runs once at startup and then every two minutes. It cancels only pending-payment, uncaptured orders in pending/processing status, fails their pending payment transactions, cancels their pending direct shipments, and releases Redis holds. It never adds stock that was not captured.

Payment completion and expiry serialize on the same order row, in order-before-payment lock order. A late callback cannot resurrect a cancelled/expired order; a paid order remains paid on retries. Redis reservations use a 15-minute TTL, and still expire if a cleanup connection fails.

Each expiry, QC and SLA pass is recorded in `operations_job_runs` and shown in the admin panel. Runs are safe to retry and to execute from multiple replicas. No nightly bank reconciliation, automatic Paya transfer, or network reward is enabled by this change.

## Validation

The integration tests require a disposable database **named `ops_test`**, migrated with all active `.up.sql` files, plus:

```powershell
$env:OPERATIONS_TEST_DATABASE_URL='postgres://ops_test:ops_test@127.0.0.1:55439/ops_test?sslmode=disable'
go test ./... -run 'TestOperations|TestExactTour|TestHeuristic' -count=1
go vet ./...
```

The test database contains synthetic fixtures only. Do not point this variable at production. Existing unrelated integration suites use other environment variables and some contain hardcoded local addresses; they are not prerequisites for running this isolated suite.

Validation also includes the complete fresh migration chain, `npm run build`, Caddy configuration validation, and Compose overlay validation. The new panels were rendered with synthetic API responses at widths 390 and 1280 pixels; picking and suggestion interactions completed with no JavaScript errors or horizontal overflow. This UI check does not substitute for live DNS/ACME issuance.
