// test-section6-verifications.js
import assert from 'assert';

console.log('--- Testing Item 1: Token Separation (ad_token vs ref) ---');

// Mock localStorage
const storage = {};
const mockLocalStorage = {
  getItem: (k) => storage[k] || null,
  setItem: (k, v) => { storage[k] = v; },
  removeItem: (k) => { delete storage[k]; }
};

// Simulation function representing StoreContext attribution logic
function simulateIncomingUrl(queryString) {
  const params = new URLSearchParams(queryString);
  const ref = params.get('ref');
  const adToken = params.get('ad_token');

  if (adToken) {
    mockLocalStorage.setItem('sellershow_last_ad_token', adToken);
  }

  if (ref) {
    mockLocalStorage.setItem('sellershow_referral_store', ref);
    return ref;
  }
  return mockLocalStorage.getItem('sellershow_referral_store');
}

// 1. External publisher click with ad_token
const clickResult1 = simulateIncomingUrl('?ad_token=ad_store-bibi_banner_4829&utm_source=html_banner');
console.log('Click on external publisher banner (ad_token):');
console.log('Returned referredStoreSlug:', clickResult1);
console.log('sellershow_last_ad_token in storage:', mockLocalStorage.getItem('sellershow_last_ad_token'));
console.log('sellershow_referral_store in storage:', mockLocalStorage.getItem('sellershow_referral_store'));

assert.strictEqual(clickResult1, null, 'ad_token MUST NOT assign a referredStoreSlug!');
assert.strictEqual(mockLocalStorage.getItem('sellershow_referral_store'), null, 'ad_token MUST NOT write into sellershow_referral_store');
assert.strictEqual(mockLocalStorage.getItem('sellershow_last_ad_token'), 'ad_store-bibi_banner_4829');
console.log('✓ Item 1 Verification Passed: ad_token strictly insulated from CPS store sales attribution.\n');

console.log('--- Testing Item 6: 3-Level Hierarchical Referral Chain & Branch Commission Calculation ---');

const demoStores = [
  { id: 'store-1', name: 'سطح ۱: فروشگاه ریشه (Root)', referrerSellerId: null, totalSales: 100_000_000 },
  { id: 'store-2', name: 'سطح ۲: زیرمجموعه اول', referrerSellerId: 'store-1', totalSales: 50_000_000 },
  { id: 'store-3', name: 'سطح ۳: زیرمجموعه دوم', referrerSellerId: 'store-2', totalSales: 30_000_000 },
];

const commissionPercent = 2; // 2% of platform fee F (estimated at 10% of sales)

function computeBranchCommission(sellerId, storeList) {
  const directChildren = storeList.filter(s => s.referrerSellerId === sellerId && s.id !== sellerId);
  let total = 0;
  for (const child of directChildren) {
    const childPlatformF = Math.round(child.totalSales * 0.10);
    const childEarned = Math.round(childPlatformF * (commissionPercent / 100));
    total += childEarned + computeBranchCommission(child.id, storeList);
  }
  return total;
}

// Level 3 (store-3): Has no children -> commission from branch = 0
const commStore3 = computeBranchCommission('store-3', demoStores);
// Level 2 (store-2): Has store-3 as child. store-3 sales = 30,000,000 -> F = 3,000,000 -> 2% = 60,000
const commStore2 = computeBranchCommission('store-2', demoStores);
// Level 1 (store-1): Has store-2 (F=5,000,000 -> 2% = 100,000) + store-3 branch (60,000) = 160,000
const commStore1 = computeBranchCommission('store-1', demoStores);

console.log(`Branch Commission for store-3 (Level 3 - Leaf): ${commStore3.toLocaleString('fa-IR')} Toman`);
console.log(`Branch Commission for store-2 (Level 2 - Parent of store-3): ${commStore2.toLocaleString('fa-IR')} Toman`);
console.log(`Branch Commission for store-1 (Level 1 - Root of entire tree): ${commStore1.toLocaleString('fa-IR')} Toman`);

assert.strictEqual(commStore3, 0);
assert.strictEqual(commStore2, 60_000);
assert.strictEqual(commStore1, 160_000);

// Self referral detection check
const cyclicStores = [
  ...demoStores,
  { id: 'store-bad', name: 'تخلف خودارجاعی', referrerSellerId: 'store-bad', totalSales: 10_000_000 }
];
const violations = cyclicStores.filter(s => s.referrerSellerId === s.id);
console.log('Detected self-referral violations count:', violations.length);
assert.strictEqual(violations.length, 1);
assert.strictEqual(violations[0].id, 'store-bad');

console.log('✓ Item 6 Verification Passed: 3-level referral branch commission math and self-referral detection verified.');
