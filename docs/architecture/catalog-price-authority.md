# Catalog display price authority

The existing owner decision in `src/utils/cardPrice.ts` defines the main card
price: active priced store default, then product default, then the first active
priced variant in server order; otherwise the product triple. A secondary
active-variant price range does not replace the main price. Four maintained
frontend tests now pin that chain, unavailable defaults, suspended sales,
already-applied sales and the separate range.

The browser has a compatibility fallback for an old backend that did not resolve
sales: a configured sale with no `onSale` field can be applied client-side.
Consequently a modern backend's negative decision must survive serialization.
`VariantPriceResponse.OnSale` previously used `omitempty`, erasing false even
when `pricing.ApplyVariantSale` suspended a sale below reference cost R. The
browser could then interpret the missing field as legacy data and display that
forbidden sale.

The value JSON marshaler now emits `onSale:false` when a sale price is configured
and did not determine the authoritative price. It preserves the ordinary shape
when no sale is configured and the existing `onSale:true` behavior when the sale
won. Other response fields and all price computations remain intact. No order
snapshot, charge or historical record is changed.

`TestConfiguredSuspendedSaleRetainsAuthoritativeFalseOnWire` runs the actual
variant pricing helper with a below-R sale and serializes the response map. It
first failed because the flag disappeared, then passed with the explicit false.
Additional wire cases preserve no-sale and winning-sale shapes. The real frontend
card helper tests confirm that explicit false prevents legacy reapplication and
true does not stack the sale a second time.

Independent review approved the three-file code/test delta. Root-only source
tree `fe70a0d89cc112e991eb918d90c5bedd37f46cb8`, based on `031df37`, passed the
complete default server unit package (DB opt-ins intentionally absent), all four
new client tests without skips and TypeScript checking. It excludes simultaneous
INV04 and CAP harness work.

This narrow transport fix precedes PERF-01's shared asOf pricing service and
authoritative index. Those implementations, complete temporal snapshot/expiry
handling, public paging/filter/bounds adoption and capacity gates remain open.
Local ignored evidence: `logs/catalog-price-wire-*.log` and
`logs/catalog-card-price-tests.log`.
