// Owns stage membership; it never performs requests, retries or extends deadlines.
export class LifecycleQueues {
 constructor(users,now=Date.now,limit=2000){
  if(!Number.isInteger(limit)||limit<1||limit>10000)throw Error('invalid backlog bound');
  this.users=users;this.now=now;this.limit=limit;this.queues={create:[],initiate:[],callback:[]};this.seen=new Set();this.pending=new Map();
  this.stats={enqueued:0,dequeued:0,expired:0,overflow:0,missing:0,ambiguous:0,recovered:0};
 }
 push(stage,item){
  if(!this.queues[stage]||!item.logicalId||!Number.isInteger(item.principal)||this.users[item.principal]?.id!==item.userId)throw Error('queue stage/owner mismatch');
  const remaining=item.expiresAt-this.now();if(!Number.isFinite(remaining)||remaining<=0||remaining>900000)throw Error('invalid or extended lifecycle deadline');
  const key=stage+':'+item.logicalId;if(this.seen.has(key))throw Error('duplicate lifecycle stage');
  if(this.seen.size>=100000||this.queues[stage].length>=this.limit){this.stats.overflow++;throw Error('bounded lifecycle queue full')}
  this.seen.add(key);this.queues[stage].push(item);this.stats.enqueued++;
 }
 take(stage){
  if(!this.queues[stage])throw Error('unknown lifecycle stage');
  while(this.queues[stage].length){const item=this.queues[stage].shift();if(item.expiresAt<=this.now()){this.stats.expired++;continue}this.stats.dequeued++;return item}
  this.stats.missing++;return null;
 }
 ambiguous(stage,item){
  const key=stage+':'+item.logicalId;if(this.pending.has(key))throw Error('duplicate ambiguous record');
  if(this.pending.size>=this.limit){this.stats.overflow++;throw Error('ambiguous recovery backlog full')}
  this.pending.set(key,{stage,item});this.stats.ambiguous++;
 }
 recovery(){return [...this.pending.values()]}
 resolve(stage,id){if(!this.pending.delete(stage+':'+id))throw Error('unknown recovery');this.stats.recovered++}
 snapshot(){return {stats:{...this.stats},depths:Object.fromEntries(Object.entries(this.queues).map(([k,v])=>[k,v.length])),unresolved:this.pending.size}}
}
