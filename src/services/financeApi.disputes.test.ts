import {test,mock} from 'node:test';import assert from 'node:assert/strict';import{financeApi}from'./financeApi';
test('FIN005 resolution refuses malformed success projections',async()=>{
 const f=mock.method(globalThis,'fetch',async()=>new Response('{}',{headers:{'Content-Type':'application/json'}}));
 try{await assert.rejects(financeApi.resolveDispute('d',{action:'release_to_seller',responsibleParty:'seller'}));}finally{f.mock.restore();}
});
