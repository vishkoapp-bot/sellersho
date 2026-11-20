export function validateBrowserTarget(container,network,target){
 const label='sellersho.capacity-run',name='sellersho-cap-'+target.nonce;
 const bindings=container.NetworkSettings?.Ports?.['443/tcp'];
 if(!/^[a-f0-9]{24}$/.test(target.nonce)||!/^\d{1,5}$/.test(target.port)||Number(target.port)<1||Number(target.port)>65535||
 target.base!=='https://capacity.example.test:'+target.port||target.network!==name||
 container.Id!==target.cid||container.Name!=='/'+name+'-tls'||container.Config?.Labels?.[label]!==target.nonce||
 JSON.stringify(Object.keys(container.NetworkSettings?.Networks||{}))!==JSON.stringify([name])||
 network.Id!==target.network_id||network.Name!==name||network.Internal!==true||network.Labels?.[label]!==target.nonce||
 bindings?.length!==1||bindings[0].HostIp!=='127.0.0.1'||bindings[0].HostPort!==target.port)throw Error('browser target lacks exact disposable container/network/loopback binding identity');
}

export function validateInternalBrowserTarget(target,marker){
 if(!/^[a-f0-9]{24}$/.test(marker||'')||target.nonce!==marker||target.base!=='https://capacity.example.test')throw Error('browser requires the private internal target capability');
}
export function validateRelayBrowserTarget(target,marker){
 if(!/^[a-f0-9]{24}$/.test(marker||'')||target.nonce!==marker||target.listen!=='127.0.0.1'||!Number.isInteger(target.port)||target.port<1||target.port>65535||target.base!=='https://capacity.example.test:'+target.port)throw Error('browser relay requires exact loopback/private run capability');
}

export function validateNativeBrowserTarget(target,marker){
 if(!/^[a-f0-9]{24}$/.test(marker||'')||target.nonce!==marker||target.listen!=='127.0.0.1'||!Number.isInteger(target.port)||target.port<1||target.port>65535||target.base!=='https://capacity.example.test:'+target.port)throw Error('browser native target requires exact loopback/private run capability');
}
export function isNetworkResponse(row){return row.status>0&&!row.from_cache&&!row.from_service_worker}
