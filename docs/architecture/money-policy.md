# Money policy for new computations

Policy: `toman-bps-nearest100-floor-v1` (FIN-012).

Money is an integer number of **toman**. Internal checked helpers accept
nonnegative int64 amounts through 9,223,372,036,854,775,807. Public JSON money and
aggregate order totals are limited to 9,007,199,254,740,991 because the frontend
uses JavaScript numbers. Reject negative, fractional, nonfinite and unsafe
transport amounts. Checked products and sums reject overflow before creating an
order. Percentage inputs support two decimal places: 5.25% is 525 basis points.
Platform and markup inputs permit 0..1000%; discounts permit 0..100%; early
withdrawal fees must leave positive net money and cannot exceed 100%.

The legacy float64 fields remain transport/storage compatibility fields. Pricing,
discount, widget and settlement setting endpoints validate original JSON decimal
tokens before unmarshalling those fields. The engine converts stored floats
through their shortest decimal representation into exact basis points and
rejects unsupported precision. Decimal tokens must use JSON number grammar,
be at most 128 characters, and have exponent magnitude at most 128; these limits
are checked before arbitrary precision parsing. All monetary products, ratios and rounding
then use integers with arbitrary precision intermediates. The TypeScript
helpers use BigInt and enforce the same decimal scale and JSON amount bound.
Derived display ratios such as marginPercent do not authorize a money movement.

Let N100(x) round a nonnegative rational x to the nearest multiple of 100 toman,
with exact half100 ties rounded upward. For wholesale cost C, platform percentage
p and selected markup m:

```
R = max(C, N100(C * (10000 + platformBasisPoints) / 10000))
P = max(R, N100(R * (10000 + markupBasisPoints) / 10000))
platformFee = R - C
sellerMargin = P - R
```

Both price stages retain nearest100 rounding. The floor prevents rounding from
creating a negative platform fee or seller margin. A floor can retain a price
that is not a multiple of 100. A zero percentage does not guarantee a zero
reported fee for nonrounded costs: rounding 150 to 200 reports 50; rounding 149
to 100 is floored to 149 and reports zero. Before this policy, C=149,p=0 produced
R=100 and platformFee=-49. These are intentional versioned boundary changes.
Costs already divisible by 100 retain the prior formula.

Campaigns use the owner's current percentage-off-price decision of 2026-11-06.
A 10% campaign on P=970000 subtracts 97000, resulting in 873000 and a margin of
73000 when R=800000. It does **not** set margin to 10% of R. Fixed campaigns and
codes subtract integer toman; a zero fixed discount is a no-op. A campaign runs
before a code. Each discount is capped at the remaining margin; its exact
rational amount is subtracted before the final price is rounded once and clamped
to [R, previous P]. The four old target-margin assertions were reconciled to
this policy. Widget discounts retain their separate margin-percentage-point
rule; variant sales retain their absolute-price rule and suspension below R.

Early-withdrawal fees retain truncation for nonnegative values:
`fee=floor(gross*basisPoints/10000)`, `net=gross-fee`. Addon percentage fees and
referral amounts also retain their existing truncation stage. Refunds and exact
financial events do not acquire nearest100 rounding.

`AllocateMoney` allocates each positive weight except the last by floor ratio;
the last positive index receives the remainder. All-zero weights put the amount
on the final index. The caller must supply a stable ordering. Return shipping
retains the existing deterministic item/leg ordering and last-index remainder
policy, while products and summed weights can no longer wrap int64.
`PartialRefund(originalLineTotal, quantity, alreadyRefundedQty, refundQty)` is
the difference of two cumulative floor ratios. Any grouping of partial quantities
therefore sums to the immutable parent total. A refund writer must lock and
persist the cumulative refunded quantity; the pure helper does not supply that
transaction protocol (FIN-003).

Shipping tariffs retain their existing separate unit contract: stored base and
per-kilogram rates are rials; the integer leg fare is converted once to toman,
with half-up rounding to one toman. Billable kilograms use integer ceiling.
Volumetric products and rate products use checked arbitrary precision before
checking the final int64 result. Quote weight/amount aggregates reject overflow
and unsafe JSON integers; quote creation and order recomputation propagate an
unavailable error, never a zero fare. The existing Tapin shipping adapter's
toman-to-rial goods conversion is checked once before request construction, and
its rial fare conversion and summed leg fares are checked. Tests call pure
arithmetic and mock/local quote paths; no provider request was made.

New order lines record the policy in `order_items.pricing_policy_version`.
Historical rows receive only the marker `legacy-unversioned`; their unit price,
cost, fee and margin are never recomputed. Unsupported legacy pricing input is
quarantined at computation with `pricingError`; order creation and discount
quotes reject it. Current public catalogs omit quarantined products and selectable
variants rather than advertising a zero price; product detail returns HTTP 409.
The internal resolution retains the error marker for the caller. Existing percentages are not silently rounded
or rewritten in a migration.

The read-only inventory is `scripts/audit-money-policy.sql`. It reports unsupported
precision, nonfinite/out-of-range amounts, inert historical fixed overrides, and
historical snapshot inconsistencies without applying today's price formula to
history. The disposable current-schema test seeds a platform percentage of 1.001
and a paid snapshot with cost 149, fee -49, final price 100. The report finds both;
price and fee remain unchanged afterward. The migration test independently
verifies historical amounts remain unchanged when the version column is added.

Separate release dependencies remain:

- FIN-014 reviews a report from real historical data before enabling production
  writes. No production data was read by this implementation, and unsupported
  legacy values were not silently rewritten.
- FIN-003 owns the locked cumulative partial-refund transaction state. FIN-012
  tests exact cumulative allocation, but does not claim that refund writers have
  been integrated. Provider conversion and refund execution remain FIN-010; no
  PSP unit has been inferred and no outbound money service was called.
- The broad disposable suite remains red on legacy fixture/schema assumptions
  and cross-package destructive fixtures (TEST-02). Scoped pricing, allocation,
  return-shipping, decimal-request, quarantine and policy-snapshot evidence does
  not establish the whole release as ready.
