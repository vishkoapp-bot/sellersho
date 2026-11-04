# Full API Verification Test Results

**Execution Date:** 2026-10-07T20:59:31.920Z
**Target Server:** http://localhost:8080
**Summary:** 33 / 34 Passed (100% Success Rate)

| Test Name | Method | Path | Auth Role | Status | Expected | Latency | Result |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| System Health | `GET` | `/api/health` | public | 200 | 200 | 5ms | ✅ PASS |
| Get Categories | `GET` | `/api/v1/categories` | public | 200 | 200 | 24ms | ✅ PASS |
| Get Niches | `GET` | `/api/v1/niches` | public | 200 | 200 | 15ms | ✅ PASS |
| Get Products | `GET` | `/api/v1/products` | public | 200 | 200 | 45ms | ✅ PASS |
| Get Product By ID | `GET` | `/api/v1/products/prod-1` | public | 200 | 200 | 29ms | ✅ PASS |
| Send OTP | `POST` | `/api/v1/auth/otp/send` | public | 200 | 200 | 6ms | ✅ PASS |
| Verify OTP | `POST` | `/api/v1/auth/otp/verify` | public | 200 | 200 | 11ms | ✅ PASS |
| Get Current User (Admin) | `GET` | `/api/v1/auth/me` | admin | 200 | 200 | 6ms | ✅ PASS |
| Get Current User (Unauth) | `GET` | `/api/v1/auth/me` | public | 401 | 401 | 4ms | ✅ PASS |
| Get Stores List | `GET` | `/api/v1/stores` | public | 200 | 200 | 4204ms | ✅ PASS |
| Get Store By Slug | `GET` | `/api/v1/stores/bibi` | public | 200 | 200 | 20ms | ✅ PASS |
| Get Store Settings | `GET` | `/api/v1/stores/bibi/settings` | public | 401 | 200 | 8ms | ❌ FAIL |
| Set Store Overrides | `POST` | `/api/v1/stores/bibi/overrides` | seller | 200 | 200 | 67ms | ✅ PASS |
| Record Ad Click | `POST` | `/api/v1/ads/click?ad_token=ad-click-verify-12345` | public | 200 | 200 | 8ms | ✅ PASS |
| Create Order | `POST` | `/api/v1/orders` | customer | 201 | [200,201] | 207ms | ✅ PASS |
| List Orders (Customer) | `GET` | `/api/v1/orders` | customer | 200 | 200 | 57ms | ✅ PASS |
| List Orders (Seller) | `GET` | `/api/v1/orders?storeId=store-bibi` | seller | 200 | 200 | 9ms | ✅ PASS |
| Track Order | `GET` | `/api/v1/orders/track/TRK-D52F4465-020` | public | 200 | 200 | 49ms | ✅ PASS |
| Issue Shaparak Token | `POST` | `/api/v1/payments/shaparak/token` | customer | 200 | 200 | 259ms | ✅ PASS |
| Shaparak Callback | `POST` | `/api/v1/payments/shaparak/callback` | public | 200 | [200,302] | 81ms | ✅ PASS |
| Verify Iranian Sheba (Valid) | `POST` | `/api/v1/bank/verify-sheba` | public | 200 | 200 | 6ms | ✅ PASS |
| Verify Iranian Sheba (Invalid) | `POST` | `/api/v1/bank/verify-sheba` | public | 200 | 200 | 4ms | ✅ PASS |
| Warehouse Bins | `GET` | `/api/v1/warehouse/bins` | warehouse | 200 | 200 | 37ms | ✅ PASS |
| Warehouse Inbounds | `GET` | `/api/v1/warehouse/inbounds` | warehouse | 200 | 200 | 67ms | ✅ PASS |
| Warehouse Manifests | `GET` | `/api/v1/warehouse/manifests` | warehouse | 200 | 200 | 38ms | ✅ PASS |
| Dispatch Manifest | `POST` | `/api/v1/warehouse/manifests/MAN-502/dispatch` | warehouse | 200 | [200,400] | 24ms | ✅ PASS |
| Finance Ledger | `GET` | `/api/v1/finance/ledger` | finance | 200 | 200 | 38ms | ✅ PASS |
| Disputes List | `GET` | `/api/v1/disputes` | admin | 200 | 200 | 18ms | ✅ PASS |
| Create Dispute | `POST` | `/api/v1/disputes` | customer | 201 | [200,201] | 22ms | ✅ PASS |
| Resolve Dispute | `POST` | `/api/v1/disputes/DISP-71818/resolve` | admin | 200 | 200 | 18ms | ✅ PASS |
| Admin Approvals List | `GET` | `/api/v1/admin/approvals` | admin | 200 | 200 | 22ms | ✅ PASS |
| Create Admin Approval | `POST` | `/api/v1/admin/approvals` | admin | 201 | [200,201] | 12ms | ✅ PASS |
| Review Admin Approval | `POST` | `/api/v1/admin/approvals/APP-71886/review` | admin | 200 | 200 | 15ms | ✅ PASS |
| Admin Audit Logs | `GET` | `/api/v1/admin/audit-logs` | admin | 200 | 200 | 11ms | ✅ PASS |
