import { before, after, test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { createServer, type ViteDevServer } from 'vite';
import puppeteer, { type Browser } from 'puppeteer';

let server: ViteDevServer;
let browser: Browser;
let url: string;
before(async () => {
  server = await createServer({ configFile: false, logLevel: 'error',
    cacheDir: 'releases/.vite-auth-test', esbuild: { jsx: 'automatic' },
    define: { 'import.meta.env.VITE_USE_MOCK': '"false"' },
    optimizeDeps: { entries: ['scripts/fixtures/auth/index.html'] },
    plugins: [{ name: 'auth-test-endpoint', configureServer(server) {
      server.middlewares.use('/api/v1/test-auth', (req, res) => {
        res.setHeader('Content-Type', 'application/json');
        if (req.url?.includes('unauthorized')) { res.statusCode = 401; res.end(JSON.stringify({ error: 'Synthetic expired session' })); return; }
        res.end(JSON.stringify(req.headers.authorization || null));
      });
    } }],
    server: { host: '127.0.0.1', port: 0 },
  });
  await server.listen();
  const address = server.httpServer!.address() as { port: number };
  url = `http://127.0.0.1:${address.port}/scripts/fixtures/auth/index.html`;
  const executablePath = process.env.CHROME_PATH || [
    'C:/Program Files/Google/Chrome/Application/chrome.exe',
    'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  ].find(existsSync);
  browser = await puppeteer.launch({ headless: true, ...(executablePath ? { executablePath } : {}) });
});
after(async () => { await browser?.close(); await server?.close(); });

for (const [role, logout] of [
  ['customer','logoutCustomer'], ['seller','logoutSeller'], ['supplier','logoutSupplier'],
  ['warehouse_staff','logoutWarehouse'], ['logistics_specialist','logoutWarehouse'],
  ['admin','logoutAdmin'], ['super_admin','logoutAdmin'], ['finance','logoutAdmin'],
  ['finance_specialist','logoutAdmin'], ['support_specialist','logoutAdmin'],
  ['customer:admin','logoutAdmin'],
]) {
  test(`${role}: panel logout clears the shared credential and every session`, async () => {
    const context = await browser.createBrowserContext();
    try {
      const page = await context.newPage();
      await page.goto(url);
      await page.waitForFunction(() => !!(window as any).authHarness?.auth);
      await page.evaluate(async (role) => { await (window as any).authHarness.auth.loginWithOTP(role, 'synthetic'); }, role);
      await page.waitForFunction(() => !!(window as any).authHarness.state().user);
      await page.evaluate((logout) => (window as any).authHarness.auth[logout](), logout);
      await page.waitForFunction(() => !(window as any).authHarness.state().user, { timeout: 1500 });
      const state = await page.evaluate(() => (window as any).authHarness.state());
      assert.equal(state.token, null); assert.equal(state.credential, null);
      for (const session of ['admin','seller','supplier','warehouse']) assert.equal(state[session], null);
      assert.equal(state.customer.isLoggedIn, false);
      assert.equal(await page.evaluate(() => localStorage.getItem('drup_current_user')), null);
      assert.equal(await page.evaluate(() => (window as any).authHarness.privilegedRequest()), null);
      await page.reload();
      await page.waitForFunction(() => !!(window as any).authHarness?.auth);
      assert.equal(await page.evaluate(() => (window as any).authHarness.state().credential), null);
    } finally { await context.close(); }
  });
}

test('logout removes the session from another open tab', async () => {
  const context = await browser.createBrowserContext();
  try {
    const first = await context.newPage();
    await first.goto(url);
    await first.waitForFunction(() => !!(window as any).authHarness?.auth);
    await first.evaluate(async () => { await (window as any).authHarness.auth.loginWithOTP('admin', 'synthetic'); });
    const second = await context.newPage();
    // Seed the fixture's mock claims independently while preserving shared storage.
    await second.goto(url);
    await second.waitForFunction(() => !!(window as any).authHarness?.auth);
    await second.evaluate(async () => { await (window as any).authHarness.auth.loginWithOTP('admin', 'synthetic'); });
    await second.waitForFunction(() => !!(window as any).authHarness.state().user);
    assert.equal(await first.evaluate(() => (window as any).authHarness.state().user?.id), 'admin');
    await first.evaluate(() => (window as any).authHarness.auth.logoutUser());
    await second.waitForFunction(() => !(window as any).authHarness.state().user, { timeout: 1500 });
    assert.equal(await second.evaluate(() => (window as any).authHarness.state().credential), null);
  } finally { await context.close(); }
});

test('signing into a different account in one tab replaces the previous administrative session in the other', async () => {
  const context = await browser.createBrowserContext();
  try {
    const first = await context.newPage();
    await first.goto(url);
    await first.waitForFunction(() => !!(window as any).authHarness?.auth);
    await first.evaluate(() => (window as any).authHarness.auth.loginWithOTP('admin', 'synthetic'));
    await first.waitForFunction(() => (window as any).authHarness.state().admin?.role === 'admin');

    const second = await context.newPage();
    await second.goto(url);
    await second.waitForFunction(() => (window as any).authHarness.state().admin?.role === 'admin');
    await second.evaluate(() => (window as any).authHarness.auth.loginWithOTP('customer', 'synthetic'));
    await first.waitForFunction(() => {
      const state = (window as any).authHarness.state();
      return state.ready && state.user?.id === 'customer' && state.admin === null;
    }, { timeout: 3000 });
    assert.equal(await first.evaluate(() => (window as any).authHarness.state().token), 'fixture-customer');
  } finally { await context.close(); }
});

test('refresh replaces an administrative role with the active lower-privilege membership for the same token', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage();
    await page.goto(url);
    await page.waitForFunction(() => !!(window as any).authHarness?.auth);
    await page.evaluate(() => (window as any).authHarness.auth.loginWithOTP('admin', 'synthetic'));
    await page.waitForFunction(() => (window as any).authHarness.state().ready && (window as any).authHarness.state().admin?.role === 'admin');
    await page.evaluate(() => {
      const harness = (window as any).authHarness;
      harness.deferMe();
      harness.refreshed = harness.auth.refreshToken();
    });
    await page.waitForFunction(() => (window as any).authHarness.pendingMe() === 1);
    await page.evaluate(async () => {
      const harness = (window as any).authHarness;
      harness.finishMe(0, false, { capabilities: ['finance'] });
      await harness.refreshed;
    });
    await page.waitForFunction(() => (window as any).authHarness.state().admin?.role === 'finance_specialist', { timeout: 3000 });
  } finally { await context.close(); }
});

for (const failure of [false, true]) {
  for (const replacement of ['logout', 'login-B']) {
    test(`obsolete refresh ${failure ? 'failure' : 'success'} after ${replacement} cannot change identity`, async () => {
      const context = await browser.createBrowserContext();
      try {
        const page = await context.newPage();
        await page.goto(url);
        await page.waitForFunction(() => !!(window as any).authHarness?.auth);
        await page.evaluate(async () => { await (window as any).authHarness.auth.loginWithOTP('customer', 'synthetic'); });
        await page.waitForFunction(() => (window as any).authHarness.state().ready);
        await page.evaluate(() => {
          const h = (window as any).authHarness; h.deferMe();
          h.refreshDone = h.auth.refreshToken().catch(() => undefined);
        });
        await page.waitForFunction(() => (window as any).authHarness.pendingMe() === 1);
        await page.evaluate(async (replacement) => {
          const h = (window as any).authHarness;
          if (replacement === 'logout') h.auth.logoutUser();
          else await h.auth.loginWithOTP('seller', 'synthetic');
        }, replacement);
        await page.evaluate(async (failure) => { const h = (window as any).authHarness; h.finishMe(0, failure); await h.refreshDone; }, failure);
        await new Promise(resolve => setTimeout(resolve, 50));
        const state = await page.evaluate(() => (window as any).authHarness.state());
        assert.equal(state.user?.id || null, replacement === 'logout' ? null : 'seller');
        assert.equal(state.credential, replacement === 'logout' ? null : 'fixture-seller');
      } finally { await context.close(); }
    });
  }
}

test('a pending initial restore cannot clear an explicit newer login', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage();
    await page.goto(url);
    await page.waitForFunction(() => !!(window as any).authHarness?.auth && (window as any).authHarness.state().ready);
    await page.evaluate(() => {
      localStorage.setItem('seler_sho_jwt_token','fixture-customer');
      localStorage.setItem('drup_auth_token','fixture-customer');
    });
    await page.goto(url + '?deferRestore');
    await page.waitForFunction(() => (window as any).authHarness?.pendingMe() === 1);
    await page.evaluate(async () => { await (window as any).authHarness.auth.loginWithOTP('seller','synthetic'); });
    await page.evaluate(() => (window as any).authHarness.finishMe(0, true));
    await page.waitForFunction(() => (window as any).authHarness.state().ready);
    assert.equal(await page.evaluate(() => (window as any).authHarness.state().user?.id), 'seller');
    assert.equal(await page.evaluate(() => (window as any).authHarness.state().credential), 'fixture-seller');
  } finally { await context.close(); }
});

test('out-of-order refreshes apply only the newest profile', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url);
    await page.waitForFunction(() => !!(window as any).authHarness?.auth);
    await page.evaluate(async () => { await (window as any).authHarness.auth.loginWithOTP('customer','synthetic'); });
    await page.waitForFunction(() => (window as any).authHarness.state().ready);
    await page.evaluate(() => {
      const h = (window as any).authHarness;
      h.deferMe(); h.first = h.auth.refreshToken();
      h.deferMe(); h.second = h.auth.refreshToken();
    });
    await page.waitForFunction(() => (window as any).authHarness.pendingMe() === 2);
    await page.evaluate(async () => {
      const h = (window as any).authHarness;
      h.finishMe(1, false, { fullName: 'Newest', capabilities: ['customer','supplier'] }); await h.second;
      h.finishMe(0, false, { fullName: 'Obsolete', capabilities: ['customer'] }); await h.first;
    });
    await page.waitForFunction(() => (window as any).authHarness.state().user.fullName === 'Newest');
    const user = await page.evaluate(() => (window as any).authHarness.state().user);
    assert.deepEqual(user.capabilities, ['customer','supplier']);
  } finally { await context.close(); }
});

test('a refresh completed after provider unmount cannot persist credentials', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url);
    await page.waitForFunction(() => !!(window as any).authHarness?.auth);
    await page.evaluate(async () => { await (window as any).authHarness.auth.loginWithOTP('customer','synthetic'); });
    await page.waitForFunction(() => (window as any).authHarness.state().ready);
    await page.evaluate(() => { const h = (window as any).authHarness; h.deferMe(); h.done = h.auth.refreshToken(); });
    await page.waitForFunction(() => (window as any).authHarness.pendingMe() === 1);
    await page.evaluate(async () => {
      const h = (window as any).authHarness;
      h.unmount(); localStorage.clear(); h.finishMe(0); await h.done;
    });
    assert.equal(await page.evaluate(() => localStorage.getItem('drup_auth_token')), null);
    assert.equal(await page.evaluate(() => localStorage.getItem('drup_current_user')), null);
  } finally { await context.close(); }
});

test('a current restore receiving an actual 401 clears identity and opens the readiness gate', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url);
    await page.waitForFunction(() => !!(window as any).authHarness?.auth && (window as any).authHarness.state().ready);
    await page.evaluate(async () => { await (window as any).authHarness.auth.loginWithOTP('customer','synthetic'); });
    await page.waitForFunction(() => !!(window as any).authHarness.state().user);
    await page.goto(url + '?failRestore');
    await page.waitForFunction(() => !!(window as any).authHarness?.auth && (window as any).authHarness.state().ready, { timeout: 1500 });
    const state = await page.evaluate(() => (window as any).authHarness.state());
    assert.equal(state.user, null); assert.equal(state.credential, null);
    assert.equal(await page.evaluate(() => localStorage.getItem('drup_auth_token')), null);
  } finally { await context.close(); }
});

test('logout invalidates a pending panel authentication result before credential installation', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url);
    await page.waitForFunction(() => !!(window as any).authHarness?.auth);
    await page.evaluate(() => {
      const h = (window as any).authHarness;
      h.deferLogin(); h.loginDone = h.auth.loginWithOTP('admin','synthetic').catch((error: Error) => error.name);
    });
    await page.waitForFunction(() => (window as any).authHarness.pendingLogin() === 1);
    await page.evaluate(async () => {
      const h = (window as any).authHarness; h.auth.logoutUser(); h.finishLogin(0); await h.loginDone;
    });
    assert.equal(await page.evaluate(() => (window as any).authHarness.state().credential), null);
    assert.equal(await page.evaluate(() => (window as any).authHarness.state().user), null);
  } finally { await context.close(); }
});

test('an old protected 401 cannot invalidate an in-flight newer login', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url);
    await page.waitForFunction(() => !!(window as any).authHarness?.auth);
    await page.evaluate(async () => { await (window as any).authHarness.auth.loginWithOTP('customer','synthetic'); });
    await page.waitForFunction(() => (window as any).authHarness.state().ready);
    await page.setRequestInterception(true);
    let release: (request: any) => void;
    const intercepted = new Promise<any>(resolve => { release = resolve; });
    page.on('request', request => {
      if (request.url().includes('delayed401')) release(request);
      else void request.continue();
    });
    await page.evaluate(() => { const h = (window as any).authHarness; h.old401 = h.privilegedRequest('/test-auth?delayed401').catch(() => undefined); });
    const request = await intercepted;
    await page.evaluate(() => { const h = (window as any).authHarness; h.deferLogin(); h.newLogin = h.auth.loginWithOTP('seller','synthetic'); });
    await page.waitForFunction(() => (window as any).authHarness.pendingLogin() === 1);
    await request.respond({ status: 401, contentType: 'application/json', body: '{"error":"Synthetic obsolete request"}' });
    await page.evaluate(async () => { const h = (window as any).authHarness; await h.old401; h.finishLogin(0); await h.newLogin; });
    await page.waitForFunction(() => (window as any).authHarness.state().user?.id === 'seller');
    assert.equal(await page.evaluate(() => (window as any).authHarness.state().credential), 'fixture-seller');
  } finally { await context.close(); }
});

for (const failure of [false, true]) {
  test(`late supplier profile ${failure ? 'failure' : 'success'} cannot repopulate a logged-out panel`, async () => {
    const context = await browser.createBrowserContext();
    try {
      const page = await context.newPage(); await page.goto(url);
      await page.waitForFunction(() => !!(window as any).authHarness?.auth);
      await page.evaluate(async () => { const h = (window as any).authHarness; h.deferProfiles(); await h.auth.loginWithOTP('supplier','synthetic'); });
      await page.waitForFunction(() => (window as any).authHarness.pendingProfiles() > 0);
      await page.evaluate((failure) => { const h = (window as any).authHarness; h.auth.logoutUser(); h.finishProfiles(failure); }, failure);
      await new Promise(resolve => setTimeout(resolve, 50));
      const state = await page.evaluate(() => (window as any).authHarness.state());
      assert.equal(state.supplier, null); assert.equal(state.user, null); assert.equal(state.error, null);
      // A late component setter is also rejected after credential invalidation.
      await page.evaluate(() => (window as any).authHarness.auth.setSupplierSession({ token: 'fixture-supplier', supplierId: 'obsolete-supplier' }));
      await new Promise(resolve => setTimeout(resolve, 25));
      assert.equal(await page.evaluate(() => (window as any).authHarness.state().supplier), null);
    } finally { await context.close(); }
  });
}

test('a current refresh replacing a pending restore completes readiness', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await page.goto(url);
    await page.waitForFunction(() => !!(window as any).authHarness?.auth && (window as any).authHarness.state().ready);
    await page.evaluate(() => { localStorage.setItem('seler_sho_jwt_token','fixture-customer'); localStorage.setItem('drup_auth_token','fixture-customer'); });
    await page.goto(url + '?deferRestore');
    await page.waitForFunction(() => (window as any).authHarness?.pendingMe() === 1);
    await page.evaluate(async () => { const h = (window as any).authHarness; await h.auth.refreshToken(); });
    await page.waitForFunction(() => (window as any).authHarness.state().ready, { timeout: 1500 });
    await page.evaluate(() => (window as any).authHarness.finishMe(0, true));
    assert.equal(await page.evaluate(() => (window as any).authHarness.state().credential), 'fixture-customer');
  } finally { await context.close(); }
});
