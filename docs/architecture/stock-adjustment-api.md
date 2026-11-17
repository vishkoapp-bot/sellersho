# Stock adjustment input and response contract

INV-07 retains the absolute-total semantics from INV-03. The supplier stock API
requires an explicitly present integer `new_stock` from0 through2147483647 and a
nonblank reason (maximum2000 bytes). Missing/null quantity is invalid; zero is a
valid explicit value. V2 requires the selected tracked SKU; legacy aggregate
requests omit the variant. UUIDs are validated and normalized before row locking.

The authenticated supplier/product/SKU guards serialize the change. Other held
locations remain unchanged. Before mutation, the selected active SKU's requested
total plus the other quantities contributing to the product projection must fit
the product INT limit. The predicate matches the projection trigger, including
active variants and the NULL-SKU legacy pool only for products below schema2.
No quantity, movement or audit is committed on rejection.

| Result | HTTP |
| --- | --- |
| Missing/null/fractional/negative/oversized quantity, blank reason, malformed UUID or required SKU absent |400|
| Held stock, missing active owned origin or combined projection limit |409|
| Unknown or foreign product/SKU |404|
| Valid request, including zero |200 with actual `stock` and `productStock` |

Trimmed reason and authenticated actor are retained in the supplier audit and
quantity-change movement. Existing quantities in other SKUs and locations are
not rewritten. The mounted supplier form selects the exact SKU, blocks malformed
quantities and blank reason, preserves the draft on conflict, and refreshes the
authoritative product total after success.

Regression evidence includes actual migrated PostgreSQL HTTP tests and a mounted
Chromium form using the shipped API client. Boundary tests cover aggregate INT_MAX
success, one-over rejection, unchanged selected/sibling stock and movement count
after invalid requests, explicit zero, missing/null quantity, foreign identities,
held stock, inactive origin, reason/actor and authoritative response values.
The execution ledger records final suite and delivery evidence.
