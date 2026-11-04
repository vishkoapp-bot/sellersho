#!/usr/bin/env bash
# ==============================================================================
# DRUP Platform - Direct Dropshipping (Dual Fulfillment) E2E Test Runner
# ==============================================================================

set -e

echo "=== Running Direct Dropshipping E2E Integration Suite ==="
node test_direct_dropship_e2e.cjs
echo "=== Direct Dropshipping E2E Integration Suite Finished Successfully ==="
