const fs = require('fs');

async function testSupplierFeatures() {
  const base = 'http://localhost:8080/api/v1';

  // 1. Login
  const loginRes = await fetch(`${base}/auth/password/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'supplier_ali', password: 'supplier123' }),
  });
  if (!loginRes.ok) {
    throw new Error(`Login failed with status ${loginRes.status}: ${await loginRes.text()}`);
  }
  const loginData = await loginRes.json();
  const token = loginData.token;
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
  };

  // 2. Get Products
  const prodRes = await fetch(`${base}/supplier/products`, { headers });
  if (!prodRes.ok) {
    throw new Error(`Get products failed with status ${prodRes.status}: ${await prodRes.text()}`);
  }
  const products = await prodRes.json();
  if (!products || products.length === 0) {
    throw new Error('No supplier products found to test');
  }
  const targetProduct = products[0];
  console.log(`[PASS] Logged in as supplier. Testing on product ID: ${targetProduct.id} (${targetProduct.title})`);

  // 3. Update Supply Option
  const supplyOptionRes = await fetch(`${base}/supplier/products/${targetProduct.id}/supply-option`, {
    method: 'PUT',
    headers,
    body: JSON.stringify({ supply_option: '3hour_local' }),
  });
  if (!supplyOptionRes.ok) {
    throw new Error(`Update supply option failed: ${await supplyOptionRes.text()}`);
  }
  const supplyData = await supplyOptionRes.json();
  console.log('[PASS] Updated supply option to 3hour_local:', supplyData);

  // 4. Save Pricing Tiers
  const sampleTiers = [
    { min_quantity: 1, max_quantity: 10, price: 1500000 },
    { min_quantity: 11, max_quantity: 50, price: 1400000 },
    { min_quantity: 51, max_quantity: null, price: 1300000 },
  ];

  const saveTiersRes = await fetch(`${base}/supplier/pricing-tiers`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ product_id: targetProduct.id, tiers: sampleTiers }),
  });
  if (!saveTiersRes.ok) {
    throw new Error(`Save pricing tiers failed: ${await saveTiersRes.text()}`);
  }
  const saveTiersData = await saveTiersRes.json();
  console.log('[PASS] Saved pricing tiers via POST /api/v1/supplier/pricing-tiers:', saveTiersData);

  // 5. Get Pricing Tiers
  const getTiersRes = await fetch(`${base}/supplier/products/${targetProduct.id}/pricing-tiers`, { headers });
  if (!getTiersRes.ok) {
    throw new Error(`Get pricing tiers failed: ${await getTiersRes.text()}`);
  }
  const retrievedTiers = await getTiersRes.json();
  console.log('[PASS] Retrieved persisted pricing tiers:', JSON.stringify(retrievedTiers, null, 2));

  if (!Array.isArray(retrievedTiers) || retrievedTiers.length !== 3) {
    throw new Error(`Expected 3 tiers, but got ${retrievedTiers.length}`);
  }

  // 6. Verify invalid supply option rejected
  const invalidSupplyRes = await fetch(`${base}/supplier/products/${targetProduct.id}/supply-option`, {
    method: 'PUT',
    headers,
    body: JSON.stringify({ supply_option: 'invalid_choice' }),
  });
  if (invalidSupplyRes.status === 400) {
    console.log('[PASS] Invalid supply option rejected with 400 Bad Request');
  } else {
    throw new Error(`Expected 400 for invalid supply option, got ${invalidSupplyRes.status}`);
  }

  console.log('\n>>> ALL SUPPLIER SUPPLY-TIER ENDPOINT TESTS PASSED! <<<');
}

testSupplierFeatures().catch((err) => {
  console.error('[FAIL]', err);
  process.exit(1);
});
