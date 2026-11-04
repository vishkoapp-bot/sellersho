const { execSync } = require('child_process');

console.log('================================================================');
console.log(' DRUP DROPSHIP PLATFORM - AUTOMATED VERIFICATION SUITE');
console.log('================================================================\n');

try {
  console.log('>>> RUNNING TASK 2: FULL API ENDPOINT VERIFICATION');
  execSync('node test_api_all.cjs', { stdio: 'inherit' });

  console.log('\n>>> RUNNING TASK 3: SECURITY & ERROR-HANDLING PROBES');
  execSync('node test_security.cjs', { stdio: 'inherit' });

  console.log('\n>>> RUNNING TASK 4: END-TO-END ORDER LIFECYCLE TEST');
  execSync('node test_lifecycle_e2e.cjs', { stdio: 'inherit' });

  console.log('\n>>> RUNNING TASK 5: SUPPLIER E2E LIFECYCLE TEST');
  execSync('node test_supplier_e2e.cjs', { stdio: 'inherit' });

  console.log('\n>>> RUNNING TASK 6: WAREHOUSE STAFF E2E LIFECYCLE TEST');
  execSync('node test_warehouse_e2e.cjs', { stdio: 'inherit' });

  console.log('\n>>> RUNNING TASK 7: ADMIN MANAGEMENT E2E LIFECYCLE TEST');
  execSync('node test_admin_e2e.cjs', { stdio: 'inherit' });

  console.log('\n>>> RUNNING TASK 8: FINANCE SPECIALIST E2E LIFECYCLE TEST');
  execSync('node test_finance_e2e.cjs', { stdio: 'inherit' });

  console.log('\n>>> RUNNING TASK 9: DIRECT DROPSHIPPING (DUAL FULFILLMENT) E2E LIFECYCLE TEST');
  execSync('node test_direct_dropship_e2e.cjs', { stdio: 'inherit' });

  console.log('\n================================================================');
  console.log(' ALL SUITES EXECUTED SUCCESSFULLY - 100% PASS RATE');
  console.log(' Reports generated:');
  console.log('  1. coverage-analysis.md');
  console.log('  2. api-test-results.md');
  console.log('  3. security-test-report.md');
  console.log('  4. supplier-e2e-test-report.md');
  console.log('  5. warehouse-e2e-test-report.md');
  console.log('  6. admin-e2e-test-report.md');
  console.log('  7. finance-e2e-test-report.md');
  console.log('  8. direct-dropship-test-report.md');
  console.log('================================================================');
} catch (err) {
  console.error('\n❌ Test execution encountered an error:', err.message);
  process.exit(1);
}
