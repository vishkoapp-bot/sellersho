// In-memory synthetic SMS provider; no outbound HTTP client, file or log writes.
import http from 'node:http';
import {pathToFileURL} from 'node:url';
export function createSMSDouble({marker,key,delayMs=25,mode='success'}){
 if(!/^[a-f0-9]{24}$/.test(marker)||typeof key!=='string'||key.length<32)throw Error('private capacity marker and ephemeral key required');
 if(!Number.isInteger(delayMs)||delayMs<0||delayMs>5000||!['success','reject'].includes(mode))throw Error('invalid bounded SMS simulation');
 const codes=new Map();let sends=0,rejected=0;
 return http.createServer(async(req,res)=>{
  const reply=(status,body)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(body))};
  if(req.headers.authorization!==key){reply(401,{error:'unauthorized'});return}
  if(req.url==='/metrics'){reply(200,{sms_sends:sends,sms_rejected:rejected,retained_codes:codes.size,delay_ms:delayMs,mode});return}
  let text='';for await(const chunk of req){text+=chunk;if(text.length>8192){reply(413,{error:'too large'});return}}
  let body;try{body=JSON.parse(text)}catch{reply(400,{error:'invalid json'});return}
  if(req.url==='/fixture/otp'){
   const code=codes.get(body.recipient);reply(code?200:404,code?{code}:{error:'not found'});return
  }
  if(req.url!=='/v1/api/send'||req.method!=='POST'){reply(404,{error:'unsupported synthetic route'});return}
  if(!Array.isArray(body.recipients)||body.recipients.length!==1||!/^9899\d{8}$/.test(body.recipients[0])||!/^\d{4}$/.test(body.params?.code||'')){reply(400,{error:'synthetic OTP recipient/code required'});return}
  await new Promise(resolve=>setTimeout(resolve,delayMs));
  if(mode==='reject'){rejected++;reply(503,{meta:{status:false,message:'synthetic rejection'}});return}
  if(codes.size>=1000&&!codes.has(body.recipients[0])){reply(503,{error:'synthetic recipient bound reached'});return}
  sends++;codes.set(body.recipients[0],body.params.code);
  reply(200,{meta:{status:true},data:{message_outbox_ids:['synthetic-'+sends]}});
 });
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href){
 createSMSDouble({marker:process.env.SELLERSHO_CAPACITY_RUN,key:process.env.CAPACITY_SMS_KEY,delayMs:Number(process.env.CAPACITY_SMS_DELAY_MS||25),mode:process.env.CAPACITY_SMS_MODE||'success'}).listen(8081,'0.0.0.0');
}
