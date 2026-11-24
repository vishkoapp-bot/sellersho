# Sellersho: prepare a release candidate, then promote separately

The manual **Release candidate (no deployment)** workflow is the preparation
half, not a production deployment. It does not authenticate to production, use
production secrets, enroll runners, change firewalls, or alter the existing
deployment and rollback policy.

## Prepare and verify an exact revision

1. Put the reviewed change on protected `main`. Wait for the latest `ci.yml`
   **push** run on that full commit SHA to pass. A PR run or a green older
   version does not qualify.
2. In Actions, choose **Release candidate (no deployment)**, select `main`,
   and enter that exact 40-character SHA. If `main` advanced since you copied
   the SHA, the run should fail: use the new reviewed revision or stop. The
   workflow must be on the default branch before GitHub will dispatch it.
3. The workflow checks out the dispatch revision, verifies the CI run and
   attempt, builds a Linux/amd64 REL-01 real-mode archive, verifies the archive
   checksum and validates its extracted contents. A new disposable
   PostgreSQL/Redis smoke tests the extracted bytes with **mock** payment and
   synthetic login. No live PSP or SMS is exercised.
4. Record the canonical repository, run ID and run attempt, linked successful
   CI attempt, full commit and artifact name. Download the artifact from that
   successful candidate run (not a fork, cache, local rebuild, or another
   attempt). Extract GitHub's enclosing ZIP, require the one expected tarball
   and its `SHA256SUMS`, then verify the digest from that directory:
   `sha256sum -c SHA256SUMS`. The tarball and validated `release.json` must
   match the approved full commit before staging. The checksum detects transfer
   corruption, not authenticity; trust the canonical run, protected workflow
   branch and approved revision, not a checksum delivered out of band alone.
   The artifact expires after seven days; rerun preparation if it expired.

## Promotion boundary

Do not promote directly from an Actions runner. The candidate is ready for an
**explicit operator decision**, not automatic approval. The operator must use
an already-authorized host/network and the existing reviewed server coordinator
for the normal release (no SSL topology or Xray changes). Do not use
`deploy.ps1` as an artifact uploader: its ordinary path performs its own
build. The promotion adapter for reusing the exact downloaded artifact is not
installed or granted new privileges by this preparation.

Before installing such an adapter, prove in staging it stages the same
validated bytes in a new private incoming directory, rejects existing or partial
destinations, and serializes staging/promotion so no parallel invocation can
replace those bytes while the coordinator reads them. Prove the coordinator
retains its real deployment lock, verified backup and schema prechecks, bounded
health/version checks and schema-aware code rollback or forward repair.
Require verified backup/offsite health before promotion and record the
resulting readiness, public version and rollback point. Never automatically
restore a database backup over orders. See
[rollback runbook](rollback-runbook.md) and [REL-01](../../scripts/release/README.md).

If promotion is later automated, confirm branch protection and create a
production environment restricted to protected `main`, with a required
reviewer and disabled bypass. Enable prevent self-review only with a second
eligible operator; otherwise keep the manual promotion boundary. Never place
root SSH credentials in Actions, make a production host a general-purpose
runner, grant the runner Docker-group access on production, or expand the
firewall just to reach an ephemeral runner. A dedicated non-production
runner on an already-approved network with access only to an audited,
fixed-command promotion endpoint is one option. An operator-controlled
outbound artifact pull through a trusted origin is the alternative if inbound
SSH is not appropriate.

Hosted runner reachability is **unverified** and not required by the
candidate workflow. If an administrator selects HTTPS artifact pull, verify
outbound reachability and a harmless staging download from that operator
machine. If an administrator instead selects the already-authorized network
path for a restricted runner, test only the configured single host/port from
that runner via `timeout 6 nc -z "$PROBE_HOST" "$PROBE_PORT"`, with no
credentials, shell login or firewall change. A successful TCP handshake
does not validate host identity, privileges or deploy readiness.
