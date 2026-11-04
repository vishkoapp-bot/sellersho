const assert = require('node:assert/strict');
const { request, getAuthToken } = require('./test_helpers.cjs');
const { execFileSync } = require('node:child_process');
function db(q) { return JSON.parse(execFileSync('docker', ['exec','drup_postgres','psql','-U','drup_admin','-d','drup_db','-tAc',q], {encoding:'utf8'}).trim()); }
async function run() {
  const sellers = db("SELECT json_agg(x) FROM (SELECT s.slug,u.mobile FROM stores s JOIN users u ON u.id=s.owner_user_id WHERE s.slug LIKE 'direct-e2e-%' ORDER BY s.created_at DESC LIMIT 2) x");
  const tokens = await Promise.all(sellers.map(s=>getAuthToken(s.mobile)));
  let count=0;
  for(let i=0;i<2;i++) {
    for(const [method,action,body] of [['PUT','settings',{tagline:'forbidden'}],['PUT','products',{productIds:[]}],['GET','orders'],['GET','report'],['GET','balance'],['GET','payouts'],['GET','campaigns']]) {
      const r=await request(method,`/api/v1/stores/${sellers[1-i].slug}/${action}`,body||null,tokens[i]);
      assert.equal(r.status,403,`${method} ${action}: ${JSON.stringify(r.body)}`);count++;
    }
  }
  const suppliers=db("SELECT json_agg(x) FROM (SELECT s.id,u.mobile FROM suppliers s JOIN users u ON u.id=s.owner_user_id WHERE s.id IN ('sup-direct_e2e_104852','sup-direct_e2e_505251')) x");
  for(const supplier of suppliers) {
    const t=await getAuthToken(supplier.mobile);
    const other=suppliers.find(s=>s.id!==supplier.id);
    const own=await request('GET','/api/v1/supplier/settlement/balance',null,t);
    const spoof=await request('GET',`/api/v1/supplier/settlement/balance?supplierId=${other.id}`,null,t);
    assert.equal(own.status,200);assert.equal(spoof.status,200);assert.deepEqual(spoof.body,own.body);count++;
    const history=await request('GET',`/api/v1/supplier/settlement/history?supplierId=${other.id}`,null,t);
    assert.equal(history.status,200);
    assert.ok(history.body.every(p=>(p.supplier_id||p.supplierId)===supplier.id || (!p.supplier_id && !p.supplierId)));count++;
    const foreign=db(`SELECT row_to_json(x) FROM (SELECT id FROM payout_requests WHERE supplier_id='${other.id}' ORDER BY created_at DESC LIMIT 1) x`);
    for(const [method,path,body] of [['GET',`/api/v1/finance/settlements/${foreign.id}`],['POST',`/api/v1/finance/settlements/${foreign.id}/approve`,{}]]) {
      const r=await request(method,path,body||null,t);assert.equal(r.status,403);count++;
    }
    const r=await request('POST',`/api/v1/supplier/settlement/request?supplierId=${other.id}`,{amount:own.body.available+1,shaba:'IR060170000000000000000000',account_holder:'Audit',supplierId:other.id},t);
    assert.equal(r.status,409);count++;
  }
  console.log(`Horizontal isolation: ${count} assertions passed. Public store metadata remains intentionally public; supplierId spoofing is ignored in favor of JWT ownership.`);
}
run().catch(e=>{console.error(e);process.exitCode=1});