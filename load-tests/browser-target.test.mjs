import test from 'node:test';import assert from 'node:assert/strict';
test('failed browser attempts are not received network responses',async()=>{
 const {isNetworkResponse}=await import('./browser-target.mjs');
 assert.equal(isNetworkResponse({status:0,from_cache:false}),false);
 assert.equal(isNetworkResponse({status:200,from_cache:true}),false);
 assert.equal(isNetworkResponse({status:200,from_cache:false}),true);
 assert.equal(isNetworkResponse({status:500,from_cache:false}),true);
});
test('relay browser rejects arbitrary origin and mismatched run',async()=>{
 const {validateRelayBrowserTarget}=await import('./browser-target.mjs');const nonce='a'.repeat(24);
 const target={nonce,base:'https://capacity.example.test:49152',port:49152,listen:'127.0.0.1'};
 assert.doesNotThrow(()=>validateRelayBrowserTarget(target,nonce));
 for(const change of [{listen:'0.0.0.0'},{base:'https://example.com:49152'},{nonce:'b'.repeat(24)},{port:0}])assert.throws(()=>validateRelayBrowserTarget({...target,...change},nonce));
});
test('browser refuses localhost without exact private Docker identity',async()=>{
 const {validateBrowserTarget}=await import('./browser-target.mjs');
 const nonce='1234567890abcdef12345678',name='sellersho-cap-'+nonce;
 const target={nonce,cid:'c'.repeat(64),network:name,network_id:'n'.repeat(64),port:'49152',base:'https://capacity.example.test:49152'};
 const container={Id:target.cid,Name:'/'+name+'-tls',Config:{Labels:{'sellersho.capacity-run':nonce}},NetworkSettings:{Networks:{[name]:{}},Ports:{'443/tcp':[{HostIp:'127.0.0.1',HostPort:'49152'}]}}};
 const network={Id:target.network_id,Name:name,Internal:true,Labels:{'sellersho.capacity-run':nonce}};
 assert.doesNotThrow(()=>validateBrowserTarget(container,network,target));
 assert.throws(()=>validateBrowserTarget({...container,Id:'other'},network,target));
 assert.throws(()=>validateBrowserTarget(container,{...network,Internal:false},target));
 const wrong=structuredClone(container);wrong.NetworkSettings.Ports['443/tcp'][0].HostIp='0.0.0.0';
 assert.throws(()=>validateBrowserTarget(wrong,network,target));
 assert.throws(()=>validateBrowserTarget(container,network,{...target,base:'https://localhost:49152'}));
});
test('internal browser refuses external targets and missing run capability',async()=>{
 const {validateInternalBrowserTarget}=await import('./browser-target.mjs');
 const nonce='1234567890abcdef12345678';
 assert.doesNotThrow(()=>validateInternalBrowserTarget({nonce,base:'https://capacity.example.test'},nonce));
 assert.throws(()=>validateInternalBrowserTarget({nonce,base:'https://localhost'},nonce));
 assert.throws(()=>validateInternalBrowserTarget({nonce,base:'https://capacity.example.test'},undefined));
});
test('native browser accepts only the exact loopback run target',async()=>{
 const {validateNativeBrowserTarget}=await import('./browser-target.mjs');
 const nonce='a'.repeat(24);
 const target={nonce,base:'https://capacity.example.test:8443',port:8443,listen:'127.0.0.1'};
 assert.doesNotThrow(()=>validateNativeBrowserTarget(target,nonce));
 for(const change of [{listen:'0.0.0.0'},{base:'https://sellershow.ir:8443'},{base:'https://capacity.example.test:443'},{nonce:'b'.repeat(24)},{port:0}])assert.throws(()=>validateNativeBrowserTarget({...target,...change},nonce));
});
