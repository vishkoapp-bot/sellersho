import { before, after, test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { createServer, type ViteDevServer } from 'vite';
import puppeteer, { type Browser, type Page } from 'puppeteer';

let server: ViteDevServer; let browser: Browser; let url: string;
before(async () => {
  server = await createServer({ configFile: false, logLevel: 'error', cacheDir: 'releases/.vite-order-identity-test', esbuild: { jsx: 'automatic' }, define: { 'import.meta.env.VITE_USE_MOCK': '"false"' }, optimizeDeps: { entries: ['scripts/fixtures/order-identity/index.html'] }, server: { host: '127.0.0.1', port: 0 } });
  await server.listen(); url = `http://127.0.0.1:${(server.httpServer!.address() as any).port}/scripts/fixtures/order-identity/index.html`;
  const executablePath = process.env.CHROME_PATH || ['C:/Program Files/Google/Chrome/Application/chrome.exe', 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(existsSync);
  browser = await puppeteer.launch({ headless: true, ...(executablePath ? { executablePath } : {}) });
});
after(async () => { await browser?.close(); await server?.close(); });
const login = async (page: Page, i: number) => {
  await page.evaluate(async i => { await (window as any).orderHarness.auth.loginUser(String(i), 'synthetic'); }, i);
  await page.waitForFunction(i => (window as any).orderHarness.state().user === `account-${i}`, {}, i);
};
const state = (page: Page) => page.evaluate(() => (window as any).orderHarness.state());
const isolated = (value: any) => {
  assert.ok(!JSON.stringify(value).includes('CANARY-0'), 'Previous account order must be absent');
  assert.ok(!JSON.stringify(value).includes('PRIVATE-ADDRESS-0'), 'Previous account address must be absent');
  assert.equal(value.current, null); assert.equal(value.tracking, null); assert.equal(value.modal, false); assert.equal(value.address, null);
};
for (const logout of [false, true]) test(`same-name A -> ${logout ? 'logout -> ' : ''}B clears order/tracking/address on failed B fetch, reload and back`, async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth);
    assert.equal(await page.evaluate(() => (window as any).orderHarness.count), 1000);
    await login(page, 0);
    await page.evaluate(async () => {
      const h = (window as any).orderHarness; await h.orders.fetchMyOrders();
      h.orders.setTrackingOrder(h.orders.orders[0]); h.orders.setIsTrackingModalOpen(true);
      h.store.setDirectBuyAddress({ address: 'PRIVATE-ADDRESS-0', phone: '09000000000' }); h.stage();
      h.addCart(); history.pushState({}, '', '?private-A');
    });
    await page.waitForFunction(() => (window as any).orderHarness.state().tracking !== null);
    await page.evaluate(logout => { const h = (window as any).orderHarness; if (logout) h.auth.logoutUser(); h.fail(); }, logout);
    await login(page, 10); await page.evaluate(() => (window as any).orderHarness.orders.fetchMyOrders());
    isolated(await state(page));
    assert.equal(await page.evaluate(() => (window as any).orderHarness.takeStage()), null);
    assert.ok(await page.evaluate(() => localStorage.getItem('drup_carts')));
    assert.equal((await state(page)).cart.length, 1);
    await page.reload(); await page.waitForFunction(() => !!(window as any).orderHarness?.state && (window as any).orderHarness.state().user === 'account-10');
    isolated(await state(page)); assert.equal((await state(page)).cart.length, 1); await page.goBack();
    await page.waitForFunction(() => !!(window as any).orderHarness?.state); isolated(await state(page));
  } finally { await context.close(); }
});
test('private supplier draft canaries are isolated across same-name accounts and reload', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
    await page.evaluate(() => (window as any).orderHarness.store.submitProposedProduct({ title: 'PRIVATE-DRAFT-A', proposedCost: 100, supplierId: 'supplier-A', supplierName: 'Same Name' }));
    await page.waitForFunction(() => (window as any).orderHarness.state().proposals.some((p: any) => p.title === 'PRIVATE-DRAFT-A'));
    await login(page, 10);
    assert.ok(!JSON.stringify((await state(page)).proposals).includes('PRIVATE-DRAFT-A'));
    await page.reload(); await page.waitForFunction(() => (window as any).orderHarness?.state?.().user === 'account-10');
    assert.ok(!JSON.stringify((await state(page)).proposals).includes('PRIVATE-DRAFT-A'));
    await login(page, 0);
    assert.ok(JSON.stringify((await state(page)).proposals).includes('PRIVATE-DRAFT-A'));
  } finally { await context.close(); }
});
test('legacy unowned supplier drafts and locally approved products never hydrate into another account', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth);
    await page.evaluate(() => {
      localStorage.setItem('drup_proposed_products', JSON.stringify([{ id: 'old-A', title: 'PRIVATE-LEGACY-DRAFT-A', status: 'approved', supplierName: 'Same Name' }]));
      localStorage.setItem('drup_custom_approved_products', JSON.stringify([{ id: 'old-product-A', title: 'PRIVATE-LEGACY-CATALOG-A' }]));
    });
    await login(page, 10); await page.reload(); await page.waitForFunction(() => (window as any).orderHarness?.state?.().user === 'account-10');
    assert.ok(!JSON.stringify(await state(page)).includes('PRIVATE-LEGACY-'));
  } finally { await context.close(); }
});
for (const failure of [false, true]) test(`late A order ${failure ? 'error' : 'success'} returns no private data after B`, async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
    await page.evaluate(async () => { const h = (window as any).orderHarness; await h.orders.fetchMyOrders(); h.defer(); h.late = h.orders.fetchMyOrders(); });
    await page.waitForFunction(() => (window as any).orderHarness.pending() === 1); await login(page, 10);
    const result = await page.evaluate(async failure => { const h = (window as any).orderHarness; h.finish(0, failure); return h.late; }, failure);
    assert.deepEqual(result, []); isolated(await state(page));
  } finally { await context.close(); }
});
test('late A tracking result cannot be consumed by B', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
    await page.evaluate(() => { const h = (window as any).orderHarness; h.late = h.orders.trackOrder('uncached-A'); });
    await page.waitForFunction(() => (window as any).orderHarness.pending() === 1); await login(page, 10);
    assert.equal(await page.evaluate(async () => { const h = (window as any).orderHarness; h.finish(0); return h.late; }), null);
    isolated(await state(page));
  } finally { await context.close(); }
});
test('server-owned order remains visible when recipient name and phone differ from account', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 20);
    await page.evaluate(() => (window as any).orderHarness.orders.fetchMyOrders());
    await page.waitForFunction(() => document.body.textContent?.includes('CANARY-20'));
    assert.ok((await page.$eval('body', el => el.textContent))?.includes('۱ سفارش ثبت شده'));
  } finally { await context.close(); }
});
for (const failure of [false, true]) test(`late A create ${failure ? 'error' : 'success'} cannot restore staged address into B`, async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
    await page.evaluate(() => {
      const h = (window as any).orderHarness; h.stage();
      h.late = h.orders.placeOrder({ customerInfo: { fullName: 'Same Name', phone: '09000000000' }, paymentMethod: 'cash', cartItems: [], currentStore: { id: 'store-0', slug: 'store-0' } }).catch((error: Error) => error.message);
    });
    await page.waitForFunction(() => (window as any).orderHarness.pending() === 1); await login(page, 10);
    assert.equal(await page.evaluate(async failure => { const h = (window as any).orderHarness; h.finish(0, failure); return h.late; }, failure), 'Session changed during order request');
    isolated(await state(page)); assert.equal(await page.evaluate(() => (window as any).orderHarness.takeStage()), null);
  } finally { await context.close(); }
});
test('a payment-token result from A cannot start a callback in B session', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
    await page.evaluate(() => {
      const h = (window as any).orderHarness;
      h.late = h.orders.placeOrder({ customerInfo: { fullName: 'Same Name' }, paymentMethod: 'cash', cartItems: [], currentStore: { id: 'store-0', slug: 'store-0' } }).catch((error: Error) => error.message);
    });
    await page.waitForFunction(() => (window as any).orderHarness.pending() === 1);
    await page.evaluate(() => (window as any).orderHarness.finish(0));
    await page.waitForFunction(() => (window as any).orderHarness.paymentPending() === 1); await login(page, 10);
    assert.equal(await page.evaluate(async () => { const h = (window as any).orderHarness; h.finishPayment(); return h.late; }), 'Session changed during order request');
    assert.equal(await page.evaluate(() => (window as any).orderHarness.paymentCallbacks), 0); isolated(await state(page));
  } finally { await context.close(); }
});
test('response ownership remains isolated across twenty same-name accounts in ten stores', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth);
    for (let i = 100; i < 120; i++) {
      await login(page, i);
      const response = await page.evaluate(async i => {
        const h = (window as any).orderHarness;
        h.store.setCurrentStore(h.store.stores.find((s: any) => s.id === `store-${i % 10}`));
        return h.orders.fetchMyOrders();
      }, i);
      assert.deepEqual(response.map((o: any) => o.id), [`order-${i}`]);
      assert.equal(response[0].customerInfo.address, `PRIVATE-ADDRESS-${i}`);
      assert.equal(response[0].storeId, `store-${i % 10}`);
      assert.equal((await state(page)).user, `account-${i}`);
    }
  } finally { await context.close(); }
});
test('A private wishlists are neither visible nor merged into B when B hydration fails', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
    await page.waitForFunction(() => (window as any).orderHarness.state().wishlist.includes('PRIVATE-WISH-0'));
    assert.ok(!(await page.evaluate(() => localStorage.getItem('drup_wishlists')))?.includes('PRIVATE-WISH-0'));
    await page.evaluate(() => (window as any).orderHarness.failWishlist()); await login(page, 10);
    assert.deepEqual((await state(page)).wishlist, []);
    assert.ok(!(await page.evaluate(() => (window as any).orderHarness.added)).some((entry: any) => entry.owner === 'account-10' && entry.productId === 'PRIVATE-WISH-0'));
  } finally { await context.close(); }
});
test('obsolete A wishlist hydration is discarded on direct B login', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth);
    await page.evaluate(() => (window as any).orderHarness.holdWishlist()); await login(page, 0);
    await page.waitForFunction(() => (window as any).orderHarness.wishlistPending() === 1); await login(page, 10);
    await page.evaluate(() => (window as any).orderHarness.finishWishlist());
    await page.waitForFunction(() => (window as any).orderHarness.state().wishlist.includes('PRIVATE-WISH-10'));
    assert.ok(!(await state(page)).wishlist.includes('PRIVATE-WISH-0'));
  } finally { await context.close(); }
});
test('anonymous per-store wishlist product IDs merge once into the first account only', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth);
    await page.evaluate(() => (window as any).orderHarness.store.toggleWishlist('GUEST-PRODUCT', { id: 'store-0', slug: 'store-0' }));
    await page.waitForFunction(() => (window as any).orderHarness.state().wishlist.includes('GUEST-PRODUCT'));
    await login(page, 0);
    await page.waitForFunction(() => (window as any).orderHarness.added.some((entry: any) => entry.owner === 'account-0' && entry.productId === 'GUEST-PRODUCT'));
    await login(page, 10);
    assert.ok(!(await page.evaluate(() => (window as any).orderHarness.added)).some((entry: any) => entry.owner === 'account-10' && entry.productId === 'GUEST-PRODUCT'));
  } finally { await context.close(); }
});
test('post-login navigation still reaches the target after the private subtree resets', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth);
    await page.evaluate(async () => {
      const h = (window as any).orderHarness; const navigate = h.store.setActiveView;
      await h.auth.loginUser('10', 'synthetic'); navigate('seller_dashboard');
    });
    await page.waitForFunction(() => (window as any).orderHarness.state().user === 'account-10');
    assert.equal((await state(page)).view, 'seller_dashboard');
  } finally { await context.close(); }
});
test('actual seller OTP guard keeps the pending form mounted and opens the authenticated panel', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url + '?seller-guard'); await page.waitForSelector('input[type="tel"]');
    await page.type('input[type="tel"]', '09121112233'); await page.click('button[type="submit"]');
    await page.waitForSelector('input[placeholder="1234"]'); await page.type('input[placeholder="1234"]', '1234'); await page.click('button[type="submit"]');
    await page.waitForFunction(() => typeof (window as any).orderHarness.finishOtp === 'function');
    assert.equal(await page.$eval('input[placeholder="1234"]', el => (el as HTMLInputElement).value), '1234');
    await page.evaluate(() => (window as any).orderHarness.finishOtp()); await page.waitForSelector('#seller-panel-ready');
    assert.equal((await state(page)).user, 'account-700');
  } finally { await context.close(); }
});
test('a failed replacement login preserves A UI and permits fresh A requests', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
    await page.evaluate(() => (window as any).orderHarness.orders.setIsTrackingModalOpen(true));
    await page.waitForFunction(() => (window as any).orderHarness.state().modal);
    const response = await page.evaluate(async () => {
      const h = (window as any).orderHarness;
      await h.auth.authenticate(() => Promise.reject(new Error('Synthetic rejected replacement login'))).catch(() => undefined);
      return (window as any).orderHarness.orders.fetchMyOrders();
    });
    assert.deepEqual(response.map((order: any) => order.id), ['order-0']);
    const current = await state(page); assert.equal(current.user, 'account-0'); assert.equal(current.modal, true);
  } finally { await context.close(); }
});
for (const failure of [false, true]) test(`pending create ${failure ? 'failure' : 'success'} releases A busy state after rejected replacement login`, async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
    await page.evaluate(() => {
      const h = (window as any).orderHarness;
      h.operation = h.orders.placeOrder({ customerInfo: { fullName: 'Same Name' }, paymentMethod: 'cash', cartItems: [], currentStore: { id: 'store-0', slug: 'store-0' } }).catch((error: Error) => error.message);
    });
    await page.waitForFunction(() => (window as any).orderHarness.pending() === 1 && (window as any).orderHarness.state().creating);
    await page.evaluate(async failure => {
      const h = (window as any).orderHarness;
      await h.auth.authenticate(() => Promise.reject(new Error('Synthetic replacement login rejection'))).catch(() => undefined);
      h.finish(0, failure); await h.operation;
      await new Promise(requestAnimationFrame);
    }, failure);
    assert.equal((await state(page)).creating, false, 'A must leave its creating state when its operation settles');
    assert.equal((await state(page)).current, null, 'An obsolete response cannot populate private success state');
    const fresh = await page.evaluate(() => (window as any).orderHarness.orders.fetchMyOrders());
    assert.deepEqual(fresh.map((order: any) => order.id), failure ? ['order-0'] : ['order-0-created', 'order-0']);
    assert.equal((await state(page)).user, 'account-0');
    await login(page, 10); isolated(await state(page));
  } finally { await context.close(); }
});
for (const operation of ['confirmOrderDelivery', 'disputeOrderDelivery']) {
  for (const failure of [false, true]) test(`pending ${operation} ${failure ? 'failure' : 'success'} releases A busy state after rejected replacement login`, async () => {
    const context = await browser.createBrowserContext();
    try {
      const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
      await page.waitForFunction(() => (window as any).orderHarness.state().orders.some((order: any) => order.id === 'order-0'));
      await page.evaluate(operation => { const h = (window as any).orderHarness; h.operation = h.orders[operation]('order-0').catch((error: Error) => error.message); }, operation);
      await page.waitForFunction(() => (window as any).orderHarness.pending() === 1 && (window as any).orderHarness.state().busy === 'order-0');
      await page.evaluate(async failure => {
        const h = (window as any).orderHarness;
        await h.auth.authenticate(() => Promise.reject(new Error('Synthetic replacement login rejection'))).catch(() => undefined);
        h.finish(0, failure); await h.operation;
        await new Promise(requestAnimationFrame);
      }, failure);
      assert.equal((await state(page)).busy, null, 'A must leave its delivery busy state when its operation settles');
      const fresh = await page.evaluate(() => (window as any).orderHarness.orders.fetchMyOrders());
      assert.deepEqual(fresh.map((order: any) => order.id), ['order-0']);
      assert.equal(fresh[0].status, failure ? 'processing' : operation === 'confirmOrderDelivery' ? 'delivered_confirmed' : 'delivery_disputed');
      await login(page, 10); isolated(await state(page));
    } finally { await context.close(); }
  });
}
for (const replacement of ['rejected-login-A', 'login-B']) test(`settling an older create cannot clear a newer create busy flag after ${replacement}`, async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url); await page.waitForFunction(() => !!(window as any).orderHarness?.auth); await login(page, 0);
    await page.evaluate(() => {
      const h = (window as any).orderHarness;
      h.first = h.orders.placeOrder({ customerInfo: { fullName: 'Same Name' }, paymentMethod: 'cash', cartItems: [], currentStore: { id: 'store-0', slug: 'store-0' } }).catch(() => undefined);
    });
    await page.waitForFunction(() => (window as any).orderHarness.pending() === 1);
    if (replacement === 'login-B') await login(page, 10);
    else await page.evaluate(async () => { await (window as any).orderHarness.auth.authenticate(() => Promise.reject(new Error('Synthetic replacement login rejection'))).catch(() => undefined); });
    await page.evaluate(() => {
      const h = (window as any).orderHarness;
      h.second = h.orders.placeOrder({ customerInfo: { fullName: 'Same Name' }, paymentMethod: 'cash', cartItems: [], currentStore: { id: 'store-0', slug: 'store-0' } }).catch(() => undefined);
    });
    await page.waitForFunction(() => (window as any).orderHarness.pending() === 2 && (window as any).orderHarness.state().creating);
    await page.evaluate(async () => { const h = (window as any).orderHarness; h.finish(0, true); await h.first; await new Promise(requestAnimationFrame); });
    assert.equal((await state(page)).creating, true, 'The newer operation still owns a busy flag');
    await page.evaluate(async () => { const h = (window as any).orderHarness; h.finish(1, true); await h.second; await new Promise(requestAnimationFrame); });
    assert.equal((await state(page)).creating, false);
    if (replacement === 'login-B') isolated(await state(page));
  } finally { await context.close(); }
});
