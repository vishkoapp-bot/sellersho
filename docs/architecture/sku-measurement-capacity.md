# Verified physical measurements and putaway capacity

INV-08 uses one effective-measurement resolver for the queue, bin suggestions,
assignment and confirmation. A variant with any physical override requires its
own complete warehouse measurement. An override never inherits the parent's
verification. A variant without overrides uses the verified product snapshot.
Supplier declarations and proposal values remain unverified.

Warehouse measurement records append-only `inventory_measurement_revisions` with
the product/SKU identity, physical tuple, reason and actor. The current row refers
to that immutable revision. Supplier edits clear verification. Repository edits
release affected active reservations and reset assigned tasks to pending under
supplier/product/SKU guards, followed by ordered task/bin/reservation locks.
Parent edits affect fallback tasks; independently measured overrides retain their
own reservation. Raw historical edits cannot confirm a stale revision.

Capacity is `ceil(length_mm * width_mm * height_mm * units / 1000)` cm³ and
`weight_grams * units`, calculated with checked arbitrary-precision arithmetic.
Rounding applies to the whole lot. Assignment and confirmation check verified
bin limits and active reservations under the bin lock, using subtraction checks
to avoid overflowing totals. Suggestions are advisory and use the same resolver.

An active reservation records its revision and rounded physical amounts.
Confirmation consumes the exact accepted source and writes one immutable
`warehouse_placements` snapshot before completing the task. Database guards
validate the exact allocation, supplier, SKU, warehouse, bin, quantity, active
reservation, current verified revision and rounded amounts. Retry does not append
another placement. Later measurement changes never rewrite completed occupancy.
Physical departure and occupancy release are the separate INV-06 gate.

Migration `202611140010_sku_measurement_revision` preserves existing valid verified
parent readings as explicit snapshots. Existing variant overrides remain
unverified. It does not infer historical placements, revise bin counters or
recalculate previous occupancy from current dimensions. Read-only
`warehouse_measurement_reconciliation` exposes completed tasks without placement
evidence and old active reservations needing reassignment. A populated downgrade
refuses to discard measurement evidence. Earlier requested filename suffix004 was
already occupied; the implementation uses the next unused migration010.

The warehouse measurement endpoint accepts optional `variantId` and returns the
actual immutable revision and source. The mounted putaway form identifies the
SKU/name/SPNC and sends that exact variant when warehouse staff verify it.

Local evidence is recorded in the execution ledger. Isolated PostgreSQL tests
exercise parent10mm/10g versus SKU100mm/1000g, unverified overrides, insufficient
suggestion/assignment/confirmation capacity, new and changed revisions, supplier
declarations, parent fallback, aggregate rounding, bounds/overflow, replay,
immutable completed occupancy, forged/stale SQL inserts, and migration history
preservation. These checks do not certify historical physical counts or the
separate production hardware throughput target.
