import test from 'node:test';
import assert from 'node:assert/strict';
test('hot SKU requires five winners and explicit insufficient-stock losers',async()=>{
 const {evaluateHotSKU}=await import('./hot-sku-policy.mjs');
 const valid={contenders:100,paid:5,conflicts:95,wrongAmount:0,failures:0,duplicateCallbacks:10};
 assert.deepEqual(evaluateHotSKU(valid),[]);
 assert.ok(evaluateHotSKU({...valid,paid:6}).length);
 assert.ok(evaluateHotSKU({...valid,conflicts:94}).length);
 assert.ok(evaluateHotSKU({...valid,wrongAmount:1}).length);
 assert.ok(evaluateHotSKU({...valid,duplicateCallbacks:9}).length);
});
