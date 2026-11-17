# Browser session lifecycle (AUTH-01 / AUTH-02)

All panels use one browser credential. Every panel logout calls the same operation,
regardless of base role or capabilities. It invalidates pending authentication,
removes private user/order/wishlist storage, clears every role session and opens
the guest readiness gate. Public per-store product carts remain in memory and
storage; private orders, staged addresses and tracking are handled by AUTH-03.
Removing the shared credential or clearing storage logs out other open tabs.
This is browser-session clearing, not server JWT revocation.

Credential installation uses `authenticate`: start an identity generation before
the request, verify both generation and expected credential before commit, and
return an `isCurrent` guard for subsequent panel profile/store work. All five
panel entry points use this operation, including supplier registration. Raw
session setters refuse obsolete credentials. Current API 401 responses clear the
provider through a same-tab expiry signal; an old request's 401 cannot cancel a
newer authentication attempt. Profile/restore/refresh responses check identity,
credential, request ordering and mounted ownership before success or failure
side effects. The latest valid profile owner also completes session readiness.

Verification on 14 November 2026:

```
node --import tsx --test --test-timeout=120000 src/context/AuthContext.lifecycle.test.tsx src/context/AuthContext.test.ts src/App.routing.test.ts src/components/authEntryPoints.contract.test.ts
npm run build
```

41 tests passed, none skipped. The production build passed. Mounted tests use a
loopback Vite fixture and headless Chrome with synthetic identities/providers.
They cover all panel roles, capability-only access, shared-credential absence on
a real subsequent request, reload, two-tab logout, stale refresh success/error
after logout or account replacement, initial restore versus login, out-of-order
refreshes, unmount, actual current 401, obsolete 401 during a newer pending login,
pending credential installation after logout, and late supplier profile success
and failure. A source contract additionally prevents direct token installation
or unwrapped authentication in all five real entry components.

Original mounted regression reproduced three failed panel logouts (finance,
support, logistics). Deferred-result tests reproduced session resurrection and
new-login erasure. Review caught current-401 readiness and direct panel-entry
bypasses; dedicated regressions failed before the fixes and passed afterward.
Raw local logs are `logs/auth01-red-confirmed.log`, `logs/auth02-red.log`,
`logs/auth02-401-red.log` and `logs/auth-final.log` (ignored).

Private cache isolation remains the separate AUTH-03 gate. Real-provider login,
capacity, token expiry/device behavior and final integrated release are not
certified by synthetic browser fixtures.
