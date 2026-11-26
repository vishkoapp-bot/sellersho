import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Page } from 'puppeteer';
import { batch8Browser, fillInput, waitText } from '../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const panels = [
  { id: 'seller_dashboard', path: '/seller', shell: 'seller', label: 'فروشنده' },
  { id: 'supplier_dashboard', path: '/supplier', shell: 'supplier', label: 'تأمین‌کننده' },
  { id: 'warehouse_panel', path: '/warehouse', shell: 'warehouse', label: 'انبار' },
  { id: 'admin_panel', path: '/admin', shell: 'admin', label: 'مدیریت' },
] as const;

const cases = [
  { role: 'customer', capabilities: ['customer'], panels: [] },
  { role: 'seller', capabilities: ['seller'], panels: ['seller'] },
  { role: 'supplier', capabilities: ['supplier'], panels: ['supplier'] },
  { role: 'warehouse_staff', capabilities: ['warehouse_staff'], panels: ['warehouse'] },
  { role: 'logistics_specialist', capabilities: ['logistics_specialist'], panels: ['warehouse'] },
  { role: 'finance', capabilities: ['finance'], panels: ['admin'] },
  { role: 'finance_specialist', capabilities: ['finance_specialist'], panels: ['admin'] },
  { role: 'support_specialist', capabilities: ['support_specialist'], panels: ['admin'] },
  { role: 'admin', capabilities: ['admin'], panels: ['admin'] },
  { role: 'super_admin', capabilities: ['super_admin'], panels: ['admin'] },
  { role: 'supplier', capabilities: ['customer', 'seller', 'supplier'], panels: ['seller', 'supplier'] },
  { role: 'super_admin', capabilities: ['support_specialist', 'logistics_specialist'], panels: ['warehouse', 'admin'] },
  { role: 'admin', capabilities: [], panels: [] },
] as const;

function scenario(path: string, role: string, capabilities: readonly string[], otp = false) {
  const url = new URL(path, 'http://fixture.test');
  url.searchParams.set('access-fixture-role', role);
  url.searchParams.set('access-fixture-caps', capabilities.join(','));
  if (otp) url.searchParams.set('access-fixture-otp', '1');
  return url.pathname + url.search;
}

async function openWorkspace(page: Page, view: string) {
  await page.evaluate(target => (window as any).batch8Fixture.openWorkspace(target), view);
}

for (const account of cases) {
  test(`only granted workspaces open for ${account.role} [${account.capabilities.join(',')}]`, { timeout: 120000 }, () =>
    visit(scenario('/admin?section=overview', account.role, account.capabilities), 320, async page => {
      await page.waitForFunction(() => (window as any).batch8Fixture?.state().isSessionReady);
      for (const panel of panels) {
        await openWorkspace(page, panel.id);
        const shell = `[data-panel-workspace="${panel.shell}"]`;
        if ((account.panels as readonly string[]).includes(panel.shell)) {
          await page.waitForSelector(shell, { visible: true });
        } else {
          const reason = panel.shell === 'warehouse' ? 'حساب فعلی مجوز پنل انبار را ندارد' : `حساب فعلی مجوز پنل ${panel.label} را ندارد`;
          await waitText(page, reason);
          assert.equal(await page.$('[data-panel-workspace]'), null, `${panel.path} mounted workspace without a grant`);
        }
        assert.equal(new URL(page.url()).pathname, panel.path);
      }
      if (!(account.capabilities as readonly string[]).includes('customer')) {
        await openWorkspace(page, 'customer_profile');
        await waitText(page, 'حساب فعلی مجوز پنل فروشگاه را ندارد');
        assert.equal(await page.$('[data-panel-workspace]'), null);
      }
    }));
}

test('stored base role does not widen a specialist workspace and combined grants expose both permitted sections', () =>
  visit(scenario('/admin?section=orders', 'super_admin', ['finance_specialist', 'support_specialist', 'logistics_specialist']), 390, async page => {
    await page.waitForSelector('#admin-main');
    await waitText(page, 'این بخش برای حساب شما در دسترس نیست');
    assert.equal(await page.$('aside [data-section="orders"]'), null);
    assert.equal(await page.$('aside [data-section="staff"]'), null);
    assert.equal(await page.$('aside [data-section="finance-callback-retries"]'), null);
    for (const section of ['finance-overview', 'support-tickets', 'ad-network', 'historical-reconciliation']) {
      assert.ok(await page.$(`aside [data-section="${section}"]`), section);
    }
    assert.deepEqual(await page.$$eval('nav[aria-label="پنل‌های مجاز"] button', buttons => buttons.map(button => button.textContent?.trim())), ['انبار', 'مدیریت']);
  }));

test('warehouse city scope is honored in the menu and direct link even with an active admin grant', () =>
  visit(scenario('/admin?section=historical-reconciliation', 'admin', ['admin', 'warehouse_staff']), 390, async page => {
    await page.waitForSelector('#admin-main');
    await waitText(page, 'این بخش برای حساب شما در دسترس نیست');
    for (const section of ['historical-reconciliation', 'warehouses', 'operations']) {
      assert.equal(await page.$(`aside [data-section="${section}"]`), null, section);
    }
    assert.ok(await page.$('aside [data-section="orders"]'));
    assert.equal(await page.evaluate(() => (window as any).batch8Fixture.adminReads.includes('/api/v1/admin/warehouses')), false);
  }));

test('admin OTP for a customer shows the access message and does not install the credential', () =>
  visit(scenario('/admin', 'customer', ['customer'], true), 390, async page => {
    await fillInput(page, 'input[type="tel"]', '09120000000');
    await page.click('button[type="submit"]');
    await fillInput(page, '#admin-otp', '1234');
    await page.click('button[type="submit"]');
    await waitText(page, 'این شماره مجوز ورود به پنل مدیریت را ندارد');
    assert.equal(await page.$('#admin-main'), null);
    assert.equal(await page.evaluate(() => localStorage.getItem('seler_sho_jwt_token')), null);
  }));

test('admin OTP login resolves management membership supplied for an account whose base role is customer', () =>
  visit(scenario('/admin?section=orders', 'customer', ['finance_specialist'], true), 390, async page => {
    assert.equal(await page.$('input[type="password"]'), null);
    await fillInput(page, 'input[type="tel"]', '09120000000');
    await page.click('button[type="submit"]');
    await fillInput(page, '#admin-otp', '1234');
    await page.click('button[type="submit"]');
    await page.waitForSelector('#admin-main');
    assert.equal(await page.$eval('#admin-page-title', title => title.textContent), 'تراز و دفتر کل');
    assert.equal(await page.$$('aside [data-section="orders"]').then(links => links.length), 0);
  }));

test('supplier sign-in on mobile lists no suppliers and opens the panel only after SMS verification', () =>
  visit(scenario('/supplier', 'customer', ['supplier'], true), 390, async page => {
    await page.waitForSelector('#supplier-sign-in-mobile');
    assert.equal(await page.$('form select, form input[type="password"], form input[type="checkbox"]'), null);
    assert.equal(await page.$('nav[aria-label="پنل‌های مجاز"]'), null);
    await fillInput(page, '#supplier-sign-in-mobile', '09120000000');
    await page.click('button[type="submit"]');
    await page.waitForSelector('#supplier-sign-in-otp[autocomplete="one-time-code"]');
    assert.equal(await page.$('[data-panel-workspace]'), null);
    await fillInput(page, '#supplier-sign-in-otp', '1234');
    await page.click('button[type="submit"]');
    await page.waitForSelector('[data-panel-workspace="supplier"]', { visible: true });
  }));
