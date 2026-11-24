import assert from 'node:assert/strict';
import { test } from 'node:test';
import { withSellerBrowser } from '../../../scripts/fixtures/drp/browser';

test('DRP-2: quick-add persists a central match to a genuinely empty seller store and only marks success after acknowledgment', async () => {
  const mutations: unknown[] = [];
  await withSellerBrowser(
    `import {SellerSearchInsightTab} from './src/components/seller/SellerSearchInsightTab';
    createRoot(document.getElementById('root')).render(<SellerSearchInsightTab/>);`,
    `import {useState} from 'react';
    import {storeApi} from './src/services/storeApi';
    const products=[{id:'central-match'},{id:'other-irrelevant-global-product'}];
    export function useStore() {
      const [activeProductIds,setIds]=useState([]);
      return {
        products,currentStore:{id:'store-seller',slug:'seller',ownerType:'dropshipper',activeProductIds},toPersianDigits:String,
        updateStoreActiveProducts: async (ids) => {
          try { await storeApi.saveStoreProducts('seller',ids); setIds(ids); return {ok:true}; }
          catch { return {ok:false}; }
        }
      };
    }`,
    async page => {
      const add = () => page.evaluate(() => [...document.querySelectorAll('button')]
        .find(b => b.textContent?.trim() === 'افزودن به فروشگاه' && !b.disabled)?.click());
      await page.waitForFunction(() => document.body.innerText.includes('پاوربانک'));
      assert.equal(await page.evaluate(() => [...document.querySelectorAll('button')]
        .some(b => b.textContent?.trim() === 'افزودن به فروشگاه' && !b.disabled)), true);
      await add();
      await page.waitForFunction(() => !!document.querySelector('[role=alert]'));
      assert.doesNotMatch(await page.$eval('#root', e => (e as HTMLElement).innerText), /در فروشگاهت هست/);
      await add();
      await page.waitForFunction(() => document.body.innerText.includes('در فروشگاهت هست'));
      assert.deepEqual(mutations, [{ productIds: ['central-match'] }, { productIds: ['central-match'] }]);
    },
    req => {
      const url = new URL(req.url());
      if (url.pathname === '/api/v1/seller/search-insight' && url.searchParams.get('store_id') === 'store-seller') {
        return { body: {
          topSearches: [],
          unmetInStore: [{ term: 'پاوربانک', searches: 1, zeroResultSearches: 1,
            catalogMatches: [{ productId: 'central-match', title: 'پاوربانک' }] }],
          unmetCatalog: [],
        } };
      }
      if (url.pathname === '/api/v1/stores/seller/products' && req.method() === 'PUT') {
        mutations.push(JSON.parse(req.postData() ?? '{}'));
        return mutations.length === 1 ? { status: 500, body: { error: 'fixture failure' } } : { body: { id: 'store-seller', activeProductIds: ['central-match'] } };
      }
      return undefined;
    },
  );
});
