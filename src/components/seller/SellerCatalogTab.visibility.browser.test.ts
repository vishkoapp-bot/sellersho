import assert from 'node:assert/strict';
import { test } from 'node:test';
import { withSellerBrowser } from '../../../scripts/fixtures/drp/browser';

const ids = Array.from({ length: 12 }, (_, i) => `catalog-test-${i}`);
const items = ids.map((id, index) => ({
  id, title: `کالای نمونه ${index}`, slug: id, category: 'other', nicheIds: ['niche-test'],
  originalPrice: 1000, price: 1000, baseCostB: 800, platformFeeF: 0, stock: 2,
  isActive: index === 11, image: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7',
}));

test('DRP-8: owner sees twelve choices but only one public product; an unsaved toggle does not change publication', async () => {
  await withSellerBrowser(
    `import {SellerCatalogTab} from './src/components/seller/SellerCatalogTab';
    createRoot(document.getElementById('root')).render(<SellerCatalogTab/>);`,
    `const products=[]; const currentStore={
      id:'seller',slug:'seller',ownerType:'dropshipper',activeProductIds:${JSON.stringify(ids)},selectedNicheIds:['niche-test']
    }; export const useStore=()=>({
      products,currentStore,platformSettings:{},formatPrice:String,toPersianDigits:String,
      updateStoreActiveProducts:()=>Promise.resolve({ok:true}),updateStoreSelectedNiches:()=>{}
    });`,
    async page => {
      await page.waitForSelector('[role=switch]');
      assert.equal(await page.$$eval('[role=switch][aria-checked=true]', switches => switches.length), 12);
      const row = (index: number) => `[role=switch][aria-label$="کالای نمونه ${index} برای فروشگاه"]`;
      const rowText = (index: number) => page.$eval(row(index), button => (button.parentElement!.parentElement as HTMLElement).innerText);
      assert.match(await page.$eval('#root', e => (e as HTMLElement).innerText), /12 کالای انتخاب‌شده.*1 کالا در ویترین عمومی/s);
      assert.match(await rowText(0), /پلتفرم: غیرفعال.*ویترین عمومی: نمایش داده نمی‌شود/s);
      assert.match(await rowText(11), /پلتفرم: فعال.*ویترین عمومی: نمایش داده می‌شود/s);
      await page.$eval(row(11), button => (button as HTMLElement).click());
      assert.equal(await page.$eval(row(11), button => button.getAttribute('aria-checked')), 'false');
      assert.match(await rowText(11), /ویترین عمومی: نمایش داده می‌شود/);
    },
    req => {
      const url = new URL(req.url());
      if (url.pathname === '/api/v1/products/' && url.searchParams.get('includeInactive') === '1') {
        return { body: { items, total: items.length, hasMore: false, includeInactiveApplied: true } };
      }
      // The separate pricing overrides do not affect publication.
      return { body: [] };
    },
  );
});
