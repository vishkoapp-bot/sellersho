# SPNCX staff lookup and label jobs

This backend slice reads the already-issued unit ledger. It does not mint
codes, backfill inventory, render labels, or add a portal screen.

The authenticated staff prefixes are:

- `/api/v1/admin/spncx`: admin operations scope.
- `/api/v1/supplier/inventory/spncx`: supplier inventory access. Lookup
  is restricted to the supplier's products; printing also requires current
  custody at one of its active warehouses.
- `/api/v1/warehouse/inventory/spncx`: warehouse staff with a confirmed
  city. Lookup is restricted to that city's current custody, or to the
  precise order allocation for a sold unit; printing requires current
  custody. Prior work references from other sites are not returned.

`POST {prefix}/lookup` takes `{"code":"A0C00001-X0001"}`. The response
contains the unit's status, issuance and print metadata, recent lifecycle
events, and its current order ID only when sold. It contains no buyer
identity, staff account details, or raw event metadata. An absent code and
a code outside the caller's scope use the same response. Codes travel in
the request body, not the URI. Staff responses are non-cacheable and the
public product serializers are unchanged.

`POST {prefix}/print-jobs` requires an `Idempotency-Key` header and a
body with `productId`, `variantId`, up to 100 existing `codes` of that
SKU, and `reprint` (default `false`). This slice accepts a new label job
only for units currently in sellable stock; it does not implement receipt
line selection or printing during QC. It returns the original codes in
issuance order and `barcodeFormat: "CODE128"` for a frontend renderer;
there is no QR option.

A first job records `printed_at` and a print event. A reprint requires
a previous first print, keeps the original code and first timestamp,
updates the last-print timestamp and count, and appends a reprint event.
An authorized retry with the same key and exact payload returns the same
job without updating history. These fields confirm that the backend
issued a print job, not that paper came out of a printer.

New tracked external receipts record the existing canonical receipt-line
provenance in the same transaction that issues their codes. This allows
the current QC and storage-history validators to recognize the real
receipt and preserves the code through QC and putaway. No prior receipt
or existing unit was changed by this slice.
