import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const storefront = '/store/mone?seller&drp';
const panel = (tab: string, flags = '') => '/?panel=seller&tab=' + tab + '&seller&drp' + flags;
const scope = '[aria-label="فروشگاه فعال"]';

test('DRP-3 restores a saved seller on the exact storefront instead of sending them to the panel', async () => {
  await visit(storefront, 390, async page => {
    await page.waitForFunction(() => !!(window as any).batch8Fixture.state);
    await page.waitForFunction(() => !!localStorage.getItem('drup_current_user'));
    await page.waitForFunction(() => (window as any).batch8Fixture.ownedReads() > 0);
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    assert.equal(new URL(page.url()).pathname, '/store/mone');
    assert.equal(await page.evaluate(() => (window as any).batch8Fixture.state().view), 'seller_storefront');
  });
  await visit('/dashboard?seller&drp', 390, async page => {
    await page.waitForSelector(scope);
    assert.deepEqual(await page.$eval(scope, node => [(node as HTMLSelectElement).value, location.pathname]), [
      'fixture-store', '/seller',
    ]);
  });
});

for (const tab of ['orders', 'wallet']) {
  test('DRP-5 ' + tab + ' waits for the authoritative owned selection on a deep link', async () => {
    await visit(panel(tab, '&delayOwner'), 390, async page => {
      await page.waitForFunction(() => (window as any).batch8Fixture.ownerReadyToRelease());
      assert.deepEqual(await page.evaluate(() => (window as any).batch8Fixture.privateCalls()), []);
      await page.evaluate(() => (window as any).batch8Fixture.releaseOwned());
      await waitText(page, tab === 'orders' ? 'FIRST-ORDER' : '۸۹۰٬۰۰۰');
      const calls = await page.evaluate(() => (window as any).batch8Fixture.privateCalls());
      assert.ok(calls.includes('mone:' + (tab === 'orders' ? 'orders' : 'balance')));
      assert.ok(calls.every((call: string) => call.startsWith('mone:')));
      assert.doesNotMatch(await page.evaluate(() => document.body.textContent ?? ''), /forbidden unowned store/);
    });
  });
}

test('DRP-5 a failed ownership check never loads orders and Retry preserves the deep link', async () => {
  await visit(panel('orders', '&failOwner'), 390, async page => {
    await waitText(page, 'بارگذاری فروشگاه‌های شما انجام نشد.');
    assert.deepEqual(await page.evaluate(() => (window as any).batch8Fixture.privateCalls()), []);
    await clickText(page, 'تلاش مجدد');
    await waitText(page, 'FIRST-ORDER');
    assert.equal(new URL(page.url()).searchParams.get('tab'), 'orders');
  });
});

test('DRP-5 late order results cannot replace the new selection', async () => {
  await visit(panel('orders', '&delayOldOrders'), 390, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture.pendingReads() > 0);
    await page.select(scope, 'fixture-store-b');
    await waitText(page, 'SECOND-ORDER');
    await page.evaluate(async () => {
      (window as any).batch8Fixture.releaseOldReads();
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    });
    const text = await page.evaluate(() => document.body.textContent ?? '');
    assert.match(text, /SECOND-ORDER/);
    assert.doesNotMatch(text, /FIRST-ORDER/);
  });
});

test('DRP-5 late wallet error does not show an error for the new selection', async () => {
  await visit(panel('wallet', '&delayOldBalance'), 390, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture.pendingReads() > 0);
    await page.select(scope, 'fixture-store-b');
    await waitText(page, '۱۲۳٬۰۰۰');
    await page.evaluate(async () => {
      (window as any).batch8Fixture.releaseOldReads();
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    });
    assert.doesNotMatch(await page.evaluate(() => document.body.textContent ?? ''), /موجودی فروشگاه مونه بارگذاری نشد/);
  });
});

test('DRP-4 cancelled returns have no pending QC badge but retain recorded findings', async () => {
  await visit(panel('returns'), 390, async page => {
    await waitText(page, 'CANCELLED-RETURN');
    const articles = await page.$$eval('article', nodes => nodes.map(node => node.textContent ?? ''));
    const canceled = articles.find(body => body.includes('CANCELLED-RETURN')) ?? '';
    const open = articles.find(body => body.includes('OPEN-RETURN')) ?? '';
    assert.match(canceled, /لغوشده/);
    assert.doesNotMatch(canceled, /در انتظار بررسی/);
    assert.match(canceled, /سالم/);
    assert.match(canceled, /معیوب/);
    assert.match(open, /در انتظار بررسی/);
  });
});

test('DRP-6 trial says upgrade, whereas standard renews the same offer', async () => {
  await visit(panel('wallet'), 390, async page => {
    await waitText(page, 'خرید خدمات با پول فروشگاه');
    const trial = await page.evaluate(() => document.body.textContent ?? '');
    assert.match(trial, /ارتقا.*پلن استاندارد ۳ ماهه/);
    assert.doesNotMatch(trial, /تمدید اشتراک فروشگاه/);
    assert.match(trial, /۸۹۰٬۰۰۰ تومان/);
    await clickText(page, 'خرید با کیف پول');
    await page.waitForFunction(() => (window as any).batch8Fixture.privateCalls().includes('mone:services:{"service":"standard"}'));
  });
  await visit(panel('wallet', '&standard'), 390, async page => {
    await waitText(page, 'خرید خدمات با پول فروشگاه');
    assert.match(await page.evaluate(() => document.body.textContent ?? ''), /تمدید اشتراک فروشگاه \(پلن استاندارد ۳ ماهه\)/);
  });
});

test('DRP-9 no modal flash while replacing a same-store public record with accepted v3 owner data', async () => {
  await visit(panel('overview', '&samePublic&delayOwner'), 390, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture.ownerReadyToRelease());
    await page.evaluate(() => (window as any).batch8Fixture.releaseOwned());
    await page.waitForSelector(scope);
    assert.equal(await page.evaluate(() => (window as any).batch8Fixture.hadContractModal()), false);
  });
});

test('DRP-9 unknown versions do not request signatures, but a known v1 vs v3 does', async () => {
  for (const flags of ['&unknownSigned', '&signedOne&unknownActive']) {
    await visit(panel('overview', flags), 390, async page => {
      await page.waitForSelector(scope);
      assert.equal(await page.evaluate(() => (window as any).batch8Fixture.hadContractModal()), false);
    });
  }
  await visit(panel('overview', '&signedOne&delayContract'), 390, async page => {
    await page.waitForSelector(scope);
    await page.waitForFunction(() => (window as any).batch8Fixture.contractReadyToRelease());
    assert.equal(await page.evaluate(() => (window as any).batch8Fixture.hadContractModal()), false);
    await page.evaluate(() => (window as any).batch8Fixture.releaseContract());
    await waitText(page, 'قرارداد همکاری عوض شده است');
    assert.match(await page.evaluate(() => document.body.textContent ?? ''), /نسخه جدید ۳\.۰ \(جایگزین نسخه ۱\.۰\)/);
  });
});

test('DRP-9 confirmed acceptance remains current when switching to another owned store and back', async () => {
  await visit(panel('overview', '&signedOne'), 390, async page => {
    await waitText(page, 'قرارداد همکاری عوض شده است');
    await page.click('input[type="checkbox"]');
    await clickText(page, 'تأیید نسخه جدید و ادامه فعالیت');
    await page.waitForFunction(() => !document.body.textContent?.includes('قرارداد همکاری عوض شده است'));
    assert.ok(await page.evaluate(() => (window as any).batch8Fixture.privateCalls().includes('mone:settings')));
    await page.select(scope, 'fixture-store-b');
    await page.waitForFunction(() => document.querySelector('button[aria-pressed="true"]')?.textContent?.includes('فروشگاه دوم'));
    await page.select(scope, 'fixture-store');
    await page.waitForFunction(() => document.querySelector('button[aria-pressed="true"]')?.textContent?.includes('فروشگاه مونه'));
    assert.doesNotMatch(await page.evaluate(() => document.body.textContent ?? ''), /قرارداد همکاری عوض شده است/);
  });
});

test('DRP-10 a saved tagline completes the checklist without a logo and its missing step opens settings', async () => {
  await visit(panel('overview'), 390, async page => {
    await page.waitForSelector(scope);
    const text = await page.evaluate(() => document.body.textContent ?? '');
    assert.match(text, /فروشگاهت آماده فروش است/);
    assert.doesNotMatch(text, /لوگوی فروشگاهت را بگذار/);
    await page.select(scope, 'fixture-store-b');
    await clickText(page, 'گذاشتن شعار');
    assert.match(new URL(page.url()).search, /tab=store-setup/);
    await waitText(page, 'شعار فروشگاهت');
  });
});
