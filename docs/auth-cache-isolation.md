# Account and tenant cache isolation (AUTH-03)

The backend `/orders` endpoint already selects `claims.UserID` and repository
`customer_user_id`. The reproduced ownership defects were browser caches and
name/recipient-phone filtering. Customer views now use the backend-owned list,
with store filtering, rather than treating a matching name as authority.

Private Order/Store and descendant panel state is keyed by verified immutable
account ID and authenticated tenant. Identity replacement/logout discards order,
tracking, staged address, checkout modal, return and panel state before a new
identity's children render. The key does not change for a pending authentication
attempt. Every asynchronous operation independently checks its captured credential
and generation; obsolete successes and errors cannot populate another account.
Mounted operation ownership releases busy flags separately, so a rejected
replacement login cannot permanently disable the preserved account's controls
and an old operation cannot clear a newer operation's busy flag.

Public product carts remain above the private boundary and persist across logout,
account switching, reload and browser history. Guest wishlists use a guest-only
storage key. Signed-in wishlist data never becomes a subsequent account's guest
merge. Supplier draft persistence is scoped by immutable account/tenant, with late
proposal/approval responses guarded. Unknown-owner legacy cache records are not
assigned to a guessed owner. Live catalogue membership comes from the server;
local custom-approval caches and local `addProduct` cannot publish live products.

Verification, 14 November 2026:

```
node --import tsx --test src/context/OrderContext.identity.test.tsx
node --import tsx --test src/context/ProductContext.test.ts
npx tsc --noEmit
```

26 mounted browser regressions and three existing ProductContext regressions
passed; typechecking passed. The fixture mounts real Auth/Product/Cart/Order/
Finance/Warehouse/Store providers and actual customer views. It covers equal-name
accounts, failed B fetches, late order/tracking/create/payment continuations,
reload/history, guest carts, wishlist/draft canaries, real SellerAuthGuard OTP
navigation, failed replacement login, pending create/confirm/dispute cleanup, and
overlapping operations across A/B. Original five order/tracking and two legacy
draft/catalogue cases failed before fixes. Independent review reproduced six busy
cleanup cases; those failed before the operation-ownership fix and passed after it.

The fixture defines 1,000 synthetic identities across ten stores; browser canary
checks exercise twenty sequential identities. This is not a 1,000-active-session
load measurement. Final capacity certification stays open by the user's explicit
decision. Durable logical checkout-attempt recovery remains CHECKOUT-01, not a
claim made by private cache isolation.
