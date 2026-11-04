# Live deployment model (Team 10, Phase 2 live parity)

## D69: bind-mounts, NOT dated images

Live runs via bind-mounts of the Nov 2 binary/dist into generic images,
NOT via the dated `drup-backend` / `drup-frontend` images:

- Backend container: generic `alpine:3.19` image, command `/app/server`,
  with the Nov 2 binary bind-mounted read-only as `/app/server`
  (plus migrations and `static/` mounts).
- Frontend container: generic `nginx:alpine` image, with the Nov 2 `dist`
  bind-mounted read-only as `/usr/share/nginx/html` and `nginx.conf`
  bind-mounted read-only as `/etc/nginx/conf.d/default.conf`.
- The `drup-backend` (Oct 14) and `drup-frontend` (Oct 13) images are
  effectively unused by the running containers.

Consequence for a clean deploy: do NOT rebuild/deploy from those dated
images. A clean deploy must use the bind-mount model above (generic
base image + mounted current binary/dist/nginx.conf/migrations), or an
equivalent image build that contains exactly the Nov 2-equivalent
artifacts -- never the October images. This note is docs-only and
changes no runtime behaviour.

## D45/D46: nginx SPA fallback + 301 redirect map (verified, not changed)

The integration worktree's `nginx.conf` was verified byte-identical to
the live `/opt/drup/nginx.conf` (363 lines, `diff` clean) at Team 10
time, so it was deliberately NOT edited: editing it would diverge it
from live, and the 301 map must be preserved exactly.

- SPA fallback: `location /` uses
  `try_files $uri $uri/ /index.html;` (nginx.conf:340), so
  `/store/<slug>`, `/product/<id>`, `/about|terms|privacy|faq|license`
  and unknown paths reach `index.html` instead of nginx 404s. The
  frontend's own `not_found` view (frontend work, not Team 10) handles
  them from there.
- Preserved behaviour: `/assets/` is immutable-cached with
  `try_files $uri =404;`, `/api/` proxies to the Go backend, and
  `/static/` proxies to the backend with `autoindex off` (that flag
  only governs nginx itself; see "Uploads" below).
- 301 map: `map $uri $product_redirect` holds 268 entries (legacy
  `/product/<id-or-slug>` -> canonical `/product/spn-<n>/<slug>/`,
  Oct 28, 2026 restructure), served with
  `return 301 $product_redirect$is_args$args;` (nginx.conf:291).
  The map was copied/kept verbatim from live; NO entry was invented,
  added, or guessed by Team 10. No entry was unverifiable, so no
  redirect TODO was needed.

## Honest health (source gap; U61 caveat)

`/api/health` (`backend/cmd/server/main.go`, `health.go`) still pings
Postgres and Redis and still reports `postgres`/`redis` as `"up"` /
`"down"`, and the healthy-case shape is unchanged
(`status:"ok"`, HTTP 200, `postgres:"up"`, `redis:"up"`).

What changed: `status` is no longer unconditionally `"ok"`. If either
Postgres or Redis is down, the endpoint now returns
`status:"degraded"` with HTTP 503, so a monitor reading only `status`
(or only the HTTP code) can no longer mistake an outage for health.

U61 CAVEAT (read this before claiming live parity): live DB-down
behaviour is UNVERIFIED. Phase 1 deliberately never stopped the live
database to test it, so this is a documented honesty fix for the
source gap, NOT a proven live behaviour.

## Uploads handling (verified, NOT weakened; U64 caveat)

`POST /api/v1/upload/product-image`
(`backend/cmd/server/upload_handlers.go`, auth-gated in `main.go` via
`RequireSupplierAccess`) was verified unchanged and strict:

- Request body capped at 10 MB (`MaxBytesReader`); per-file cap 5 MB.
- Extension whitelist: `.jpg`, `.jpeg`, `.png`, `.webp` only.
- Strict magic-bytes check, fail-closed: JPEG `FF D8 FF`, PNG 8-byte
  signature, WEBP `RIFF....WEBP`; anything else is rejected, and the
  stored extension comes from the detected magic bytes, not the
  client filename.
- Stored name is a UUID (`<uuid>_<unix>.<ext>`); the raw client
  filename is ignored (no path traversal via filename).
- No new public unauthenticated file listing was added by Team 10,
  and uploads serving was not changed.

U64 CAVEAT: live upload validation details are UNVERIFIED -- Phase 1
only probed an unauthenticated GET (401); no real upload with a
valid/invalid file was ever tested against live (out of read-only
scope). Do not claim live parity for upload validation beyond the
verified source behaviour above.

KNOWN PRE-EXISTING ISSUE, DELIBERATELY UNCHANGED BY TEAM 10:
the Go backend serves `/static/` with `http.FileServer`, which
directory-lists `/static/uploads/products/` when no index file is
present; nginx's `autoindex off` on `/static/` does not stop this
because the listing comes from the Go FileServer, not nginx (Phase 1
Team 10 report, section 8). Team 10 did not change `/static/`
serving: that fix (disable listing and/or gate serving) is a
behaviour change for the finishing supervisor to approve explicitly,
since product-image URLs served from `/static/` must keep working.
It must not be silently bundled into a parity patch.

## D73: Redis key patterns (verified, unchanged)

Patterns in the integrated code are unchanged, and there is NO Flush
anywhere in the backend (`Flush`/`FLUSHDB`/`FLUSHALL` grep: no code
hits; the only match is a comment in `worker/scheduler.go` stating the
patterns are untouched):

- `otp:*` (+ `otp:cooldown:*`, `otp:attempts:*`) -- `internal/auth/otp.go`
- `catalog:categories:all`, `catalog:niches:*` -- `cmd/server/main.go`
- `jwt:revoked:*` -- referenced in comments only; origin unknown (U42,
  see MASTER-AUDIT section 3(f))
- `sms:bulk:*` -- `internal/auth/otp.go`

Never add a Flush in a parity change. The origin of `sms:bulk:*` keys
remains a TODO (U58), not a guessed implementation.

## D66: migrations untouched

Team 10 touched NO migration filename and NO `schema_migrations`
record (Team 09 owns D66). No migration file was added, renamed, or
deleted in this patch.

## Explicitly OUT of scope for Team 10 (not done, not claimed)

- NO TLS/certificate changes of any kind (D71 cert expiry on
  2026-11-08 is separate urgent work).
- NO secret, `.env`, or compose-secret edits.
- NO Drive backup work.
- NO global rate-limit invention (shared gap, unverified).
- NO pricing changes, NO `seller_discount_codes`, NO installments
  (never brought back), NO merges of `feature/monthly-settlement`,
  `feature/remove-tiered-pricing`, or `feature/dropshipper-discounts`.
- NO frontend files (Team 08 owns frontend routing/`not_found`).
- U-items stay as TODOs, never guessed implementations.
