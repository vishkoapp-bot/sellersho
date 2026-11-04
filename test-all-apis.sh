#!/bin/bash
# Drup Dropship Platform - Full API Endpoint Test Suite
# Tests all registered HTTP endpoints against http://localhost:8080

BASE_URL="${1:-http://localhost:8080}"
echo "=================================================="
echo "Drup API Verification Suite against $BASE_URL"
echo "=================================================="

PASSED=0
FAILED=0

test_endpoint() {
  local method="$1"
  local url="$2"
  local expected_status="$3"
  local token="$4"
  local data="$5"
  local desc="$6"

  local auth_header=""
  if [ -n "$token" ]; then
    auth_header="-H Authorization: Bearer $token"
  fi

  local data_param=""
  if [ -n "$data" ]; then
    data_param="-d $data"
  fi

  local status
  if [ "$method" = "GET" ]; then
    status=$(curl -s -o /dev/null -w "%{http_code}" -X GET "$url" $auth_header)
  else
    status=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" "$url" -H "Content-Type: application/json" $auth_header $data_param)
  fi

  if [ "$status" = "$expected_status" ]; then
    echo "[PASS] $method $url -> $status ($desc)"
    PASSED=$((PASSED + 1))
  else
    echo "[FAIL] $method $url -> Expected $expected_status, got $status ($desc)"
    FAILED=$((FAILED + 1))
  fi
}

echo ""
echo "--- 1. Health & Discovery ---"
test_endpoint "GET" "$BASE_URL/api/health" "200" "" "" "System Health Check"
test_endpoint "GET" "$BASE_URL/api/v1/categories" "200" "" "" "Public Categories"
test_endpoint "GET" "$BASE_URL/api/v1/niches" "200" "" "" "Public Niches"
test_endpoint "GET" "$BASE_URL/api/v1/products" "200" "" "" "Public Products List"
test_endpoint "GET" "$BASE_URL/api/v1/stores" "200" "" "" "Public Stores List"
test_endpoint "GET" "$BASE_URL/api/v1/stores/bibi" "200" "" "" "Get Store by Slug"
test_endpoint "GET" "$BASE_URL/api/v1/stores/bibi/settings" "200" "" "" "Get Store Settings"

echo ""
echo "--- 2. Authentication ---"
OTP_RESP=$(curl -s -X POST "$BASE_URL/api/v1/auth/otp/send" -H "Content-Type: application/json" -d '{"mobile":"09000000001"}')
DEMO_CODE=$(echo "$OTP_RESP" | grep -o '"demoCode":"[^"]*' | cut -d'"' -f4)

ADMIN_AUTH=$(curl -s -X POST "$BASE_URL/api/v1/auth/otp/verify" -H "Content-Type: application/json" -d "{\"mobile\":\"09000000001\",\"code\":\"$DEMO_CODE\"}")
ADMIN_TOKEN=$(echo "$ADMIN_AUTH" | grep -o '"token":"[^"]*' | cut -d'"' -f4)

test_endpoint "GET" "$BASE_URL/api/v1/auth/me" "200" "$ADMIN_TOKEN" "" "Admin Token Verification"
test_endpoint "GET" "$BASE_URL/api/v1/auth/me" "401" "" "" "Unauthorized Auth Check"

echo ""
echo "--- 3. Protected Operations (Admin / WMS / Finance) ---"
test_endpoint "GET" "$BASE_URL/api/v1/warehouse/bins" "200" "$ADMIN_TOKEN" "" "Warehouse Bins"
test_endpoint "GET" "$BASE_URL/api/v1/warehouse/inbounds" "200" "$ADMIN_TOKEN" "" "Warehouse Inbounds"
test_endpoint "GET" "$BASE_URL/api/v1/warehouse/manifests" "200" "$ADMIN_TOKEN" "" "Warehouse Manifests"
test_endpoint "GET" "$BASE_URL/api/v1/finance/ledger" "200" "$ADMIN_TOKEN" "" "Finance Ledger"
test_endpoint "GET" "$BASE_URL/api/v1/disputes" "200" "$ADMIN_TOKEN" "" "Disputes List"
test_endpoint "GET" "$BASE_URL/api/v1/admin/approvals" "200" "$ADMIN_TOKEN" "" "Admin Approvals"
test_endpoint "GET" "$BASE_URL/api/v1/admin/audit-logs" "200" "$ADMIN_TOKEN" "" "Admin Audit Logs"

echo ""
echo "=================================================="
echo "API Test Summary: $PASSED Passed, $FAILED Failed"
echo "=================================================="
