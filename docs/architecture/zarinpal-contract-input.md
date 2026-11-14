# FIN-010 provider input

User decision, 14 November 2026: Zarinpal is the implementation target, chosen
from the user's Zarinpal/Shaparak alternatives. Final capacity certification stays
open; tests are local until equivalent isolated staging hardware is available.

Official documentation checked on 14 November 2026:

- [Connection](https://www.zarinpal.com/docs/paymentGateway/connectToGateway):
  V4 HTTPS POST initiation at `https://payment.zarinpal.com/pg/v4/payment/request.json`;
  verify at `/pg/v4/payment/verify.json`. Initiation binds integer amount,
  merchant ID, callback URL and description; metadata may include order ID.
  Redirect uses `https://payment.zarinpal.com/pg/StartPay/{authority}`.
  Browser callback query fields are `Authority` and `Status`; browser success
  alone does not authorize money. Verification code 100 is first success,
  101 identifies a previously verified success. Persist and verify the original
  authoritative amount and authority, never a browser-supplied amount.
- [Sandbox](https://www.zarinpal.com/docs/paymentGateway/sandBox): replace the
  payment host with `sandbox.zarinpal.com`; a generated UUID can be the test
  merchant ID. Sandbox authorities begin with S. No production credential is
  required to prepare a low-rate real sandbox smoke. This does not certify a
  live merchant account or justify load against the external provider.
- [Currency](https://www.zarinpal.com/docs/paymentGateway/moreFeatures/currency):
  initiation supports explicit `IRR` (rial) or `IRT` (toman). The internal policy
  remains integer toman. The adapter must persist its chosen provider currency
  and amount and verify consistent provider semantics; do not infer units from
  callback or silently convert twice.
- [Reverse](https://www.zarinpal.com/docs/paymentGateway/moreFeatures/reverse):
  the documented `/pg/v4/payment/reverse.json` accepts merchant ID and authority
  for a successful payment within 30 minutes, and requires configured terminal
  IP restrictions. It documents no partial-amount parameter. It cannot be used
  as evidence of unrestricted partial or late-refund support. Such liabilities
  must remain durable internally and be routed to supported/manual reconciliation.

These sources are integration inputs, not evidence that the adapter has already
been implemented. Follow FIN-010's full durable-intent, response-loss/restart,
reconciliation, refund-capability and negative callback acceptance contract.
Do not log merchant credentials, card identifiers, provider payloads or tokens.
Real merchant credential injection and rotation remain controlled release work.
