# Immutable supplier-origin lookup

The local CAP fixture exposed a pathological supplier-balance query before the
mixed load began. A 140k-item/origin fixture produced a correlated sequential
scan of `order_item_origins` for every item. The observed active query exceeded
503 seconds; the retained plan is a bounded EXPLAIN without ANALYZE, not an
executed full-query timing report. CAP owns the sanitized
`load-tests/supplier-read-plan.json` evidence.

Canonical migrations define both `order_items.id` and
`order_item_origins.order_item_id` as UUID. The origin's existing unique index
starts with `order_item_id`. Converting both columns to text made that equality
ineligible for an ordinary UUID index condition. The follow-up uses native-type
equality, including single-item parameter lookups and account discovery. Private
legacy fixtures whose corresponding columns are both text still compare text
to text.

The financial authority is unchanged: origin rows must exist, contain positive
quantities, conserve the order-item quantity, reference the same order, and name
exactly one nonblank immutable supplier. Current product ownership remains
insufficient to authorize earned money. No historical records, schema or indexes
are changed by this patch.

The maintained regression copies actual migrated UUID tables and indexes into a
transaction-private schema. On 5,000 synthetic origins it checks that the
correlated predicate can use an **Index Cond**, then checks the exact earned
amount. `enable_seqscan=off` is used only for this index-eligibility diagnostic;
a full index scan with a casted Filter does not pass. This is not a claimed
production execution plan or latency gate. All fixture changes roll back.

The old predicate failed this regression; the native predicate passed. Affected
financial, immutable-principal, return and storage regressions passed 55 top-level
tests without skips in the shared working tree. Root-only committed-source
tree `12a78b63542220670dfe184efebe15f033983461` passed the same 55 tests without
skips in 22.551s, with verified cleanup; repository `go vet` also passed. This
archive excludes simultaneous inbound/CAP changes. Local ignored logs:
`logs/finance-origin-index-{red,green,regressions,exact}.log`.

The same large realistic fixture must be remeasured. Repeated per-supplier
treasury aggregation and full financial 100-RPS profiling remain FIN-013 work.
This narrow fix does not certify intended hardware or complete CAP-02.
