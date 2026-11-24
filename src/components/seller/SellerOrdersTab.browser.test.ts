import assert from 'node:assert/strict';
import { test } from 'node:test';
import { withSellerBrowser } from '../../../scripts/fixtures/drp/browser';

const order = {
  id: 'order-fixture', orderNumber: 'DRP-ORD-01', status: 'processing', totalAmount: 400,
  discountAmount: 0, payableAmount: 420, shippingCost: 20,
  customerInfo: { province: 'تهران', city: 'تهران', fullName: 'نام خصوصی', phone: '09124567890', address: 'نشانی خصوصی' },
  items: [
    { quantity: 1, product: { title: 'عنوان تودرتو' }, productTitle: 'عنوان کم‌اولویت' },
    { quantity: 1, productTitle: 'عنوان فعلی سرور' },
    { quantity: 1, product: { title: '   ' }, productTitle: '', product_title: 'عنوان قدیمی سرور' },
    { quantity: 1 },
  ],
};
const expectedTitles = ['عنوان تودرتو', 'عنوان فعلی سرور', 'عنوان قدیمی سرور', 'کالای سفارشی'];

test('DRP-1 and DRP-11: seller order preview and opened details use the same real title precedence without exposing shipping PII', async t => {
  await withSellerBrowser(
    `import {SellerOrdersTab} from './src/components/seller/SellerOrdersTab';
    createRoot(document.getElementById('root')).render(<SellerOrdersTab/>);`,
    `const orders=[]; export const useStore=()=>({
      orders,currentStore:{id:'seller-store',slug:'seller',ownerType:'dropshipper'},
      formatPrice:String,toPersianDigits:String,setTrackingOrder:()=>{},setIsTrackingModalOpen:()=>{}
    });`,
    async page => {
      await page.waitForFunction(() => document.body.innerText.includes('DRP-ORD-01'));
      const assertTitles = (text: string) => {
        for (const title of expectedTitles) assert.ok(text.includes(title), 'Missing item title: ' + title);
        assert.equal(text.split('کالای سفارشی').length - 1, 1);
        assert.ok(!text.includes('عنوان کم‌اولویت'));
      };
      await t.test('DRP-1 preview uses both API title formats and reserves fallback for absent title', async () => {
        assertTitles(await page.$eval('#root', e => (e as HTMLElement).innerText));
      });
      await t.test('DRP-11 details open with an untitled line and keep PII masked', async () => {
        const errors: string[] = [];
        page.on('pageerror', error => errors.push(String(error)));
        await page.evaluate(() => [...document.querySelectorAll('button')].find(b => b.textContent?.includes('دیدن جزئیات'))!.click());
        // A React render exception unmounts the root. Wait for either outcome
        // so a regression reports the crash instead of a generic timeout.
        await page.waitForFunction(() => !document.querySelector('#root')?.children.length ||
          document.body.innerText.includes('جزئیات کامل سفارش'));
        assert.deepEqual(errors, []);
        const text = await page.$eval('#root > div > .fixed', e => (e as HTMLElement).innerText);
        assertTitles(text);
        assert.match(text, /حفظ حریم خصوصی/);
        assert.match(text, /0912\*\*\*890/);
        assert.match(text, /مبلغ فاکتور/);
        for (const pii of ['نام خصوصی', '09124567890', 'نشانی خصوصی']) assert.ok(!text.includes(pii));
      });
    },
    req => req.url().endsWith('/stores/seller/orders') ? { body: { orders: [order], hasMore: false } } : undefined,
  );
});
