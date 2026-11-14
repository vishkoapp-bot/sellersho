# Catalog paging contract — implementation checkpoint

PERF-01 is OPEN. The initial `internal/catalogpage` package is not connected to
the public catalog routes. This checkpoint does not claim that current catalog
requests are paginated or that the measured bottleneck is fixed.

The new protocol defaults to 24 products and permits at most 100. Explicit zero,
negative, invalid, duplicate or conflicting size inputs fail. Offset pages after
page 1 fail instead of silently returning duplicate first pages. Public handler
and client adoption must happen together after the authoritative price index is
ready.

A bounded versioned base64url cursor carries public ordering values and a
product-ID tie breaker. Its SHA256 query binding includes the canonical resolved
store ID, owner type, authoritative scope revision and normalized public filters.
Equivalent defaults and set orderings share a binding. A changed store, revision
or filter requires a paging restart. The binding is query identity, **not an
authorization token or secret MAC**: SQL must independently resolve scope, apply
visibility/tenant predicates, and recheck the current authoritative revision.
Untrusted cursor values never authorize access.

Keys contain displayed public price/discount, creation/featured metadata or
store position according to the sort. They never contain supplier cost. The
parser rejects unknown fields, duplicate JSON keys, malformed/oversized values,
invalid UTF-8 and keys inconsistent with the selected sort. Error text does not
echo the submitted token.

Independent architecture review rejected routine whole-catalog invalidation,
full rebuilds and cold-request 503 as the default solution. The intended next
stage is a lean public-price index with affected product/scope updates in the
source transaction; broad pricing changes prepare a shadow index before atomic
activation. A writer map, time-bound campaign/sale policy, bounded worker/storage
budgets and complete client migration remain required before activation.

Main-price ordering and filtering will follow the existing owner decision in
`src/utils/cardPrice.ts`: an active priced store-default variant, then product
default, then first active priced variant; otherwise the product price. Discount
ordering uses the displayed integer percentage. Main-price bounds must describe
that same filter key; secondary active-variant ranges remain separate. Cross
language regression vectors will establish this contract before route adoption.

Local checks so far:

- New protocol regressions passed after first failing for missing implementation.
- A 10-second, two-worker fuzz run processed 151,330 inputs without a panic.
- Independent review found case-insensitive JSON aliases bypassed duplicate
  detection. Added failing `v`/`V` and `price`/`Price` cases, fixed Unicode case
  folding, and reran units plus a fresh 49,100-input fuzz pass. Strict UTF-8
  rejection also has a deterministic regression.
- `go vet ./internal/catalogpage` passed.

Local evidence is in ignored `logs/perf01-cursor-*.log`. SQL page
selection, pricing only page-sized data, complete indexed search/bounds,
consumer migrations and 10k/100k query/load evidence are outstanding. Final
intended-hardware capacity certification remains open under the user's ruling.
