# QUAL-01 HTTP method, tenant, and ownership review

Source baseline: `77537979b6b3d6f1fdd8340466a098a97eecb1e1`. This is a source audit of the two Go HTTP listeners, not a claim that all authorization controls pass runtime tests. The [route invariant matrix](route-invariant-matrix.txt) remains the tracking document for the six business jobs, financial and inventory invariants, and the open QUAL-01 acceptance gates.

Baseline coverage: all 297 HTTP registrations at the stated source commit had one row across the eight shards. The current source inventory includes one additional staff city assignment registration, documented in shard 07. Changes to warehouse city authorization and plan expiry after the stated baseline are recorded in the matrix under QUAL-01-04 and QUAL-01-05. When a historical shard and those follow-ups differ, use the follow-ups for current behavior. Registration coverage does not verify runtime behavior.

| Shard | Production registration source |
| --- | --- |
| [01: entry, auth, catalog, storefront](route-actions-01.md) | `backend/cmd/server/main.go`, lines 275 to 726 |
| [02: orders, returns, warehouse, finance](route-actions-02.md) | `main.go`, lines 731 to 1249 |
| [03: admin pricing and supplier entry](route-actions-03.md) | `main.go`, lines 1258 to 1613 |
| [04: supplier and supplier administration](route-actions-04.md) | `main.go`, lines 1614 to 2140 |
| [05: public, customer, seller and admin parity](route-actions-05.md) | `main.go`, lines 2150 to 2263 |
| [06: administrator settings and content](route-actions-06.md) | `main.go`, lines 2266 to 2336 |
| [07: administrator finance, jobs, capability and product actions](route-actions-07.md) | `main.go`, lines 2344 to 2424 |
| [08: finance, warehouse, files and metrics](route-actions-08.md) | `main.go`, lines 2428 to 2496 and `backend/internal/observability/metrics.go` |

Each table uses the literal registered mux pattern as its key. A row can contain multiple dispatched methods and suffixes. Membership in a role group is only the entry gate. Tenant context and object filters must be evaluated separately. A caller-supplied selector, terminal-state guard, or lookup by ID is not ownership. `Global` describes implementation scope under a staff capability, not proof that product policy requires global access. `No method guard` describes the application dispatch, without assuming behavior of the edge or the Go standard library. Stubs are not treated as implemented actions. CORS preflight, slash redirects, reverse proxy and network exposure are not equivalent to application authorization.

Prioritize follow-up fixtures for missing method checks, warehouse actor-to-site assignment, role/capability revocation across delegated handlers, ambiguous parent/child identifiers, and allowlisted projections for sensitive reports. Results marked open or unverified below remain open. No live endpoint testing or production access was part of this checkpoint.
