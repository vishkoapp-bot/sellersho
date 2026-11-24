import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { test } from 'node:test';
import { build } from 'esbuild';

test('DRP-11: render order details from a real-shaped flattened API item and a missing product', async () => {
  const { outputFiles } = await build({
    write: false, bundle: true, platform: 'node', format: 'cjs', jsx: 'automatic',
    external: ['react', 'react-dom/server'],
    stdin: {
      loader: 'tsx', resolveDir: process.cwd(),
      contents: `import {renderToStaticMarkup} from 'react-dom/server';
        import {OrderDetailModal} from './src/components/common/OrderDetailModal';
        export const render=(items)=>renderToStaticMarkup(<OrderDetailModal order={{
          orderNumber: 'DRP-FIXTURE', customerInfo: {fullName: 'Private Name', phone: '09123456789', address: 'Private Street'},
          items, totalAmount: 200, discountAmount: 0, payableAmount: 200
        }} isOpen={true} onClose={()=>{}} hideSensitiveCustomerInfo={true}/>);`,
    },
    plugins: [{ name: 'order-session-fixture', setup(b) {
      b.onResolve({ filter: /(?:^|\/)StoreContext$/ }, () => ({ path: 'store', namespace: 'fixture' }));
      b.onResolve({ filter: /(?:^|\/)ToastContext$/ }, () => ({ path: 'toast', namespace: 'fixture' }));
      b.onLoad({ filter: /.*/, namespace: 'fixture' }, a => ({
        loader: 'js',
        contents: a.path === 'store' ? 'export const useStore=()=>({formatPrice:String,toPersianDigits:String});'
          : 'export const useToast=()=>({success:()=>{}});',
      }));
    } }],
  });
  const bundled = { exports: {} as { render(items: unknown[]): string } };
  const evaluate = new Function('module', 'exports', 'require', outputFiles[0].text);
  evaluate(bundled, bundled.exports, createRequire(import.meta.url));
  const text = bundled.exports.render([{ productTitle: 'کالای واقعی', quantity: 1 }, { quantity: 1 }]);
  assert.match(text, /کالای واقعی/);
  assert.match(text, /کالای سفارشی/);
  assert.match(text, /0912\*\*\*789/);
  assert.doesNotMatch(text, /Private Name|Private Street|09123456789/);
});
