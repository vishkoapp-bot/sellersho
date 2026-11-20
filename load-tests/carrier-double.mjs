// Private Tapin price-inquiry double only. No registration/dispatch is simulated.
import http from 'node:http';
import {pathToFileURL} from 'node:url';
export function createCarrierDouble({marker,key,delay=50,mode='success'}={}){
 if(!/^[a-f0-9]{24}$/.test(marker||'')||typeof key!=='string'||key.length<32)throw Error('private carrier marker/key required');
 if(!Number.isInteger(delay)||delay<0||delay>5000||!['success','reject','timeout'].includes(mode))throw Error('invalid bounded carrier behavior');
 const counts={quotes:0,rejected:0,transport_failures:0,outbound_requests:0};
 return http.createServer((req,res)=>{
  const send=(status,data)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(data))};
  if(req.headers.authorization!=='Bearer '+key){send(403,{error:'private capability required'});return}
  if(req.method==='GET'&&req.url==='/metrics'){send(200,counts);return}
  if(req.method!=='POST'||req.url!=='/api/v4/tipax/public/user/order/check-price/'){send(404,{error:'unsupported synthetic operation'});return}
  let raw='',tooLarge=false;
  req.on('data',chunk=>{raw+=chunk;if(raw.length>32768){tooLarge=true;req.destroy()}});
  req.on('end',()=>{
   if(tooLarge)return;let body;try{body=JSON.parse(raw)}catch{send(400,{error:'invalid synthetic quote'});return}
   if(!/^cap-shop-\d+$/.test(body.shop_id||'')||!Array.isArray(body.products)||!body.products.length||body.products.length>100||body.products.some(p=>!Number.isSafeInteger(p.count)||p.count<1||p.count>100||!Number.isSafeInteger(p.amount_per_count)||p.amount_per_count<1||p.amount_per_count>1e12||!Number.isSafeInteger(p.weight_per_count)||p.weight_per_count<1)){send(400,{error:'synthetic shop/products required'});return}
   counts.quotes++;
   setTimeout(()=>{
    if(mode==='timeout'){counts.transport_failures++;res.destroy();return}
    if(mode==='reject'){counts.rejected++;send(200,{returns:{status:401,message:'synthetic rejected quote'},entries:null});return}
    send(200,{returns:{status:20,message:'synthetic success'},entries:{send_price:100000,send_tax:0,total_send_price:100000,total_product_price:body.products.reduce((n,p)=>n+p.count*p.amount_per_count,0),total_panel_price:0,total_receive_price:100000,total_weight:1000,total_discount:0,courier_discount:0}});
   },delay);
  });
 });
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href){
 const server=createCarrierDouble({marker:process.env.SELLERSHO_CAPACITY_RUN,key:process.env.CAPACITY_CARRIER_KEY,delay:Number(process.env.CAPACITY_CARRIER_DELAY_MS||50),mode:process.env.CAPACITY_CARRIER_MODE||'success'});
 server.listen(Number(process.env.CAPACITY_CARRIER_PORT||8082),'0.0.0.0');
}
