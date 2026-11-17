# Sellersho agent handoff — 2026-11-17

The owner requested publishing the latest completed changes, then handing the remaining backlog to another agent. Do not start another feature in this release session. The release commit is the commit containing this file; verify it against `git rev-parse HEAD`, the pushed branch and production `.deployed-commit` before continuing. Production evidence is recorded after deployment in the local `releases/fin007-production-receipt.json`; this document alone is not a claim that deployment has completed.

## Read first

- `AGENTS.md` and the owner instructions in this chat.
- `docs/superpowers/plans/2026-11-14-correctness-capacity-progress.md` for live task status.
- `docs/superpowers/plans/2026-11-14-correctness-capacity-tasks.txt` for the unchanged original 54-task contract. It matches the supplied attachment. Original unchecked boxes are not current execution status.
- `docs/superpowers/specs/2026-11-17-coupon-reservations-design.md`, `docs/superpowers/plans/2026-11-17-coupon-reservations.md`, `docs/architecture/transaction-contract.md` for FIN007.

## Standing workflow and restrictions

Use the existing `codex/correctness-capacity` branch/dedicated checkout unless isolation is needed. Change, validate, commit, push, then run `deploy.ps1 -BuildOnServer`. Release only committed source, both applications, with backup and health verification. Do not ask again for routine authorized push/deploy. Preserve production data and newly accepted orders; never restore an old DB just to roll back application code.

Exclude the 13 documents in `docs/fa/` and the paused `backend/internal/networkreward/` from reads, tests, compilation, staging, commits and deployment. Never run root backend `go test ./...` in the working checkout. Freeze an explicit Git allowlist into an archive without protected paths and use the existing top-level-cloned disposable test runner. Ordinary broad Go commands sharing one DB cause private-fixture collisions; they are not a substitute for the isolated runner. Do not stop the owner's unrelated drup/cafe Docker stacks.

Production SSH alias: `parspack-sellersho`; root `/opt/drup`; PostgreSQL container `drup_postgres`, database `drup_db`, role `drup_admin`. Existing SSH configuration holds access; never print passwords/private keys/API keys/full environments. Runtime credentials stay on the server. Restricted backups remain on-server. Preserve primary-site access and unrelated services; Xray on24443 is the already-authorized arrangement. Avoid broad Docker pruning or deletion of unknown volumes/releases/backups. Current constrained release-space preparation only removes verified generated dependencies and hardlinks byte-identical static duplicates while retaining every static path.

## FIN007 delivered behavior

Order-owned reservation/event/counter writes replace ignored post-commit coupon increments. Exact coupon-only discount is separate from aggregate seller discount. Keyed checkout replay returns the original order before new pricing/admission; exhaustion is explicit409. Confirmed durable receipts consume capacity; eligible unpaid cancellation/expiry releases once. Consumed occupancy survives refunds. Late confirmed money after release preserves receipt and FIN003 compensation without recreating a slot or stock.

Additive migration202611170001 backfills conservative history without repricing. Unknown/ambiguous legacy coupon identity remains immutable reconciliation evidence: financial facts/refunds are preserved, new capture/dispatch denied, existing physical carrier/delivery facts preserved. Runtime cannot forge legacy evidence or NULL admission amounts, unsafe historical release, or backdated admission. The reservation admission trigger takes/reuses its owner coupon guard350, then stamps/checks the database clock; payment/supplier child fences do not lock earlier parents. Compatible creation FK parents use KEY SHARE before private insert; existing lifecycle finance guard order remains canonical.

Actual old08c6d5b/new binary rehearsal covers migration drain/refusal/retry, real old checkout/payment/cancel/expiry/supplier dispatch fences, ordinary nondiscounted traffic, additive old-code rollback restrictions and new receipt/replay. Keep additive data/fences on rollback; never run down migration over populated evidence to regain old coupon availability. After this release succeeds, seal202611170001 up as `edfccf790452cbee20e89596835a68b0a065eded34e827e74fb69f97ec3047e5` (SHA256 after CRLF normalization); all earlier applied migration blobs remain immutable.

## Backlog after FIN007 local completion

29 DONE local task gates, five Checkpoint OPEN, 20 TODO:25 not closed. A local DONE is not real-provider certification, historical repair approval or intended-hardware capacity certification.

Checkpoint OPEN: OBS-01, CAP-01, SEC-01, TEST-02, PERF-01.
TODO: FIN-010, INV-12, FIN-008, FIN-011, FIN-009, FIN-013, CHECKOUT-02, REL-01, BAK-01, MIG-02, FIN-014, CHECKOUT-03, PERF-02, PERF-03, PERF-04, EDGE-01, REL-02, QUAL-01, CAP-02, GO-01.

Owner decisions: Zarinpal selected but merchant/sandbox access is not ready; continue independent work, do not request secrets in chat or substitute simulation for real PSP proof. Final1000-user/100-RPS certification on production-equivalent hardware stays OPEN; local tests only for now. Alerts belong inside the admin panel. Paid cancellations refund the original funding split (wallet part to wallet, gateway part to original transaction). Network-reward development remains paused. Wallet checkout remains disabled; reporting work must not enable a payment path.

## Suggested next independent task: FIN008

No FIN008 implementation has been started. Optional local research is `releases/fin008-research.md`; it is ignored evidence and may not exist in a fresh clone. Use original FIN008 acceptance as authority, not research as completed work.

Current treasury allTimeIn from paid noncancelled orders is not bank cash. Canonical available balance already subtracts payout reservations across statuses; subtracting pending again and omitting approved/processing can falsely change safeToSpend without money movement. Aggregate positive available liabilities plus unconfirmed transfer holds per principal exactly once, report negative recovery debt separately and never net one party's recovery against another's liability. Avoid overlap between already-earned supplier entitlement and unreleased whole-order obligations. Wallet locked amounts are a subset, not another balance; a wallet refund replaces the corresponding refund obligation rather than double-counting it.

The code lacks a complete bank-statement/opening-cash/settlement evidence model. Preserve known liability/outflow components but keep unverified production liquidCash/safeToSpend/headroom unknown with reconciliation_required, not guessed zero or a fabricated cash import/config. A read-only REPEATABLE READ set-based snapshot and checked arithmetic is the suggested approach; failed reads must fail the report.

The executable conservation oracle can use explicit local20m verified cash with real DB payout snapshots: earned10m, pending2m gives available8m/hold2m/out0; approved stays the same; confirmed gives available8m/hold0/out2m. Liability10m->10m->8m and cash20m->20m->18m leave safeToSpend10m throughout. Production HTTP separately must show unknown cash where evidence is absent. Update actual FinanceOverviewTab/DTO null/error/trend handling. Do not invent a customer-withdrawal writer or real settlement/refund behavior absent from provider evidence.

## Local evidence and resumption

Final tested candidate tree432e76625223b4ce65104d9505b5e38389da2908; final commit may differ only in completion/handoff documentation. `releases/fin007-final-gates.json` and source/commit equivalence proof identify the full accepted revision. `releases/fin007-old-binary-run-9/manifest.json` plus `releases/fin007-compatibility-source-432e76625223.json` record actual binary/source identity and cleanup. The old archive has547 literal Git blobs and976 CRLF-normalized matches, zero non-line-ending changes; do not call all old bytes literal Git-identical.

Reports: `.superpowers/sdd/2026-11-17-coupon-reservations/task-1-report.md`, original/scoped/final review reports there. Earlier nonpassing and stale test runs are retained; `logs/fin007-62757-nonpassing/` records888 tests/3 fixture failures corrected later. Do not relabel those failures as passing. Do not redispatch/reimplement FIN007 after compaction; read completion evidence and Git history first.

Post-release receipt must include intended/pushed/live commit,8 migration seals, health/live/ready, frontend and primary-site access, six jobs succeeding after the new backend start, restricted backup, coupon counter/issue audit and before/after20-table old-column plus monthly-history fingerprints. Raw financial data stays in server memory; only counts/hashes leave it. If real business activity changes a fingerprint, investigate attributable events; do not restore or rewrite data to force equality.

## Final local release gate

Full root pipeline on tree432e completed successfully:891 Go top-level entries, zero failures, with exactly two intentional standalone crash-process helper skips (the owning parent tests pass);309 frontend tests, zero skips/failures; TypeScript/Vite build and go vet pass;72 Linux CGO race tests pass with zero skips; cold Linux production-equivalent server/migrator builds pass. The actual deployment prerequisite tests pass7/7. Source proof matches1532 literal candidate Git blobs and leaves every applied migration unchanged. Actual old/new binary run9 and independent Git/source-equivalence proof pass with verified owned cleanup. Completion/handoff-only documentation is added after this tested tree; the final staged proof records that distinction. Production release and postchecks occur after this commit and must be verified from the live marker/receipt.
