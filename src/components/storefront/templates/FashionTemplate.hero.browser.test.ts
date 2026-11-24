import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { withSellerBrowser } from '../../../../scripts/fixtures/drp/browser';

test('Fashion hero stacks in a 390px frame on desktop, splits at desktop width and stacks on real mobile', async () => {
  await withSellerBrowser(
    `import {FashionTemplate} from './src/components/storefront/templates/FashionTemplate';
    const store={id:'s',slug:'s',name:'فروشگاه',ownerType:'dropshipper',activeProductIds:[]};
    const photo='data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
    const config={sections:{hero:true},custom:{heroImage:photo,heroTitle:'کالکشن تازه برای یک فروشگاه ایرانی حرفه‌ای'}};
    const Template=()=> <FashionTemplate store={store} products={[]} accentColor="#793f3c" config={config}/>;
    createRoot(document.getElementById('root')).render(<><div id="preview"><Template/></div><div id="standalone"><Template/></div></>);`,
    `export const useStore=()=>({
      searchQuery:'',selectedCategory:'all',toPersianDigits:String,formatPrice:String,
      setSelectedCategory:()=>{},setSearchQuery:()=>{},clearCatalogBrand:()=>{}
    });`,
    async page => {
      const check = async (container: string, columns: number, fontSize: number) => {
        const selector = container + ' .fashion-hero';
        await page.waitForSelector(selector);
        const layout = await page.$eval(selector, hero => {
          const media = hero.querySelector('.fashion-hero-media')!.getBoundingClientRect();
          const copy = hero.querySelector('.fashion-hero-copy')!.getBoundingClientRect();
          const title = hero.querySelector('h1')!;
          const tr = title.getBoundingClientRect();
          return {
            columns: getComputedStyle(hero).gridTemplateColumns.split(' ').length,
            stacked: copy.top >= media.bottom - 1,
            fontSize: parseFloat(getComputedStyle(title).fontSize),
            titleFits: tr.left >= copy.left && tr.right <= copy.right && title.scrollWidth <= title.clientWidth + 1,
          };
        });
        assert.equal(layout.columns, columns);
        assert.equal(layout.stacked, columns === 1);
        assert.equal(layout.fontSize, fontSize);
        assert.equal(layout.titleFits, true);
      };
      await check('#preview', 1, 28);
      await check('#standalone', 2, 48.96);
      await page.setViewport({ width: 390, height: 900 });
      await check('#standalone', 1, 28);
    },
    undefined,
    'body { margin: 0; } #preview { width: 390px; max-width:100%; }\n' +
      readFileSync(new URL('./fashion.css', import.meta.url), 'utf8'),
  );
});
