# Location-safe legacy aggregate edits (INV-03)

The stock edit remains an absolute sellable total. For a legacy product, only
the explicitly selected active supplier warehouse's NULL-SKU sellable lot may
change. Supplier/product guards precede all existing product balance locks in
bytewise ID order. Held sellable at every other location/owner/SKU is subtracted
from the requested total; a total below held stock fails without any write.
Reserved, transit, QC, damaged and unrelated suppliers' quantities are untouched.

Five local plus five warehouse units saved as ten stays five plus five for all
twenty retries. Saving eight changes only local to three; saving four fails.
Capture/update concurrency permits either serialized result: save10 then capture1
leaves9, or capture1 then save10 leaves10. Warehouse units are never recreated by
the edit. PostgreSQL INT bounds and a nonempty counted reason are required. New
balance creation needs an owned active physical source. Zero without a balance or
origin does not invent one. V2 products still require an exact tracked SKU.

Changes record a positive-magnitude adjustment movement with direction, exact
location, reason, actor, prior local/projection, held quantity and requested total.
No-op absolute retries do not create adjustment movements. Audit rows remain
explicit user commands. HTTP obtains its stock/productStock response from the
transaction's actual projections, after commit; it never echoes the request.
Both supplier edit forms collect the reason and describe the aggregate meaning.
Older cached clients get a reason-required response asking them to refresh.

Initial publication uses separate seeding. Atomic INSERT ON CONFLICT DO NOTHING
determines the first publisher; a prior unlocked EXISTS cannot authorize seeding.
A conflicting historical proposal locks the persisted product, checks owner and
V2 restrictions, and updates only permitted non-stock fields. It cannot attach
another origin or seed stock. Its default legacy label uses current stock rather
than the historical requested count. Existing typed publications require an
explicit amendment. No schema/history rewrite is performed by INV-03.

Historical inflation is not inferred or automatically subtracted. The read-only
`scripts/audit-legacy-stock-locations.sql` reports physical lots, recent inventory
movements and candidate supplier audit text for counted reconciliation. Candidate
audit text is not authoritative physical evidence. Each page is at most500 rows,
with has_more. Start with no cursor. In the same read-only transaction, use
`SELECT set_config('sellersho.audit_after_product_id', $1, TRUE)` with the last
returned product ID for the next page; the cursor and SQL order are bytewise.
Use a bounded statement timeout. No production report data is printed or copied
off-server by these tests.

Evidence: initial defect RED at15 instead of10 (`logs/inv03-before.log`), historical
amendment RED at50 instead of8 (`logs/inv03-reapproval-before.log`), controlled
concurrent first publication RED with two lots totaling10
(`logs/inv03-publication-race-before.log`), and HTTP reason error RED at500 instead
of400 (`logs/inv03-http-before.log`). Affected35-case integration run passed;
final23-case review-fix run passed, including the controlled race/winner origin and
501-product complete report pagination. Other tests cover zero/no-balance/bounds,
other suppliers and non-sellable states, twelve capture/update races, SKU isolation,
publication/review fixtures and actual HTTP actor/response. Logs:
`logs/inv03-affected-full.log`, `logs/inv03-review-fixes-final.log`.

Independent reviewer review_inv03 approved the final atomic publication and report
fixes with no remaining Critical/Important finding. Scope exclusions were counted
historical repair (prohibited without evidence), unrelated inventory work (tracked
separately), and production rollout (performed by the release workflow).
