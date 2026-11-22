import {test,mock} from 'node:test';import assert from 'node:assert/strict';import{financeApi}from'./financeApi';
test('FIN005 resolution refuses malformed success projections',async()=>{
 const f=mock.method(globalThis,'fetch',async()=>new Response('{}',{headers:{'Content-Type':'application/json'}}));
 try{await assert.rejects(financeApi.resolveDispute('d',{action:'release_to_seller',responsibleParty:'seller'}));}finally{f.mock.restore();}
});

test('D7 getDisputes preserves per-row status for the header counts',async()=>{
 const rows=[
  {id:'d-pending',orderNumber:'ORDER-1',type:'delivery_undelivered',customerName:'مشتری',productTitle:'کالای باز',claimAmount:100000,reason:'نرسیده',storeId:'store',status:'pending',createdAt:'2026-11-22T00:00:00Z'},
  {id:'d-resolved',orderNumber:'ORDER-2',type:'return_7day',customerName:'مشتری',productTitle:'کالای مختومه',claimAmount:200000,reason:'مرجوعی',storeId:'store',status:'resolved',createdAt:'2026-11-22T00:00:00Z'},
 ];
 const f=mock.method(globalThis,'fetch',async()=>new Response(JSON.stringify(rows),{headers:{'Content-Type':'application/json'}}));
 try{
  const list=await financeApi.getDisputes();
  assert.equal(list.length,2);
  // The D7 header counts "open" as status !== 'resolved' and renders the
  // resolved count alongside; that arithmetic needs the raw status per row.
  assert.equal(list.filter(d=>d.status!=='resolved').length,1);
  assert.equal(list.filter(d=>d.status==='resolved').length,1);
  assert.equal(list.find(d=>d.id==='d-resolved')?.productTitle,'کالای مختومه');
 }finally{f.mock.restore();}
});
