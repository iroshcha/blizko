import {test} from 'node:test';
import assert from 'node:assert/strict';
import {newIdentity,contactCode,parseContact,goJSON,seal,open,signFrame,verifyFrame,randomID} from '../src/protocol.js';
import {exportBackup,importBackup} from '../src/storage.js';
import {Chat} from '../src/client.js';

const settle=()=>new Promise(resolve=>setImmediate(resolve));
async function queuedChat(peers=1,messages=2) {
 const chat=new Chat('https://example.test'),identities=[];
 chat.vault={save:async()=>{}};
 await chat.create('Sender');
 for(let i=0;i<peers;i++) {
  const peer=await newIdentity('Peer '+i);identities.push(peer);
  await chat.addContact(peer.name,contactCode(peer));
  for(let n=0;n<messages;n++)await chat.send(peer.id,'Message '+n);
 }
 const self=await parseContact(contactCode(chat.state.identity),'Sender'),requests=[];
 chat.exchange=(address,data)=>new Promise((resolve,reject)=>{
  const peer=identities.find(p=>p.address===address),message=open(JSON.parse(data),self,peer);
  requests.push({stored:()=>resolve({status:202,code:"stored",data:String(Date.now()+86400000)}),peer:peer.id,id:message.id,reject,ack:()=>resolve({status:200,data:goJSON(seal({v:2,id:randomID(),from:peer.id,to:self.id,kind:'ack',ack:message.id},self,peer))})});
 });
 chat.online=true;chat.stopped=false;
 return {chat,identities,requests};
}

test('outbox advances without polling, preserves FIFO and paces fast receipts',async t=>{
 const {chat,requests}=await queuedChat();t.after(()=>chat.stop());
 t.mock.timers.enable({apis:['setTimeout','Date'],now:10000});
 chat.flush();await settle();assert.equal(requests.length,1);
 requests[0].ack();await settle();
 assert.equal(chat.state.messages[0].delivered,true);
 assert.equal(requests.length,1,'a fast receipt must not cause an unlimited burst');
 t.mock.timers.tick(150);await settle();
 assert.equal(requests.length,2,'next message starts without the 2-second poll');
 assert.equal(requests[1].id,chat.state.messages[1].id);
 requests[1].ack();await settle();assert.ok(chat.state.messages.every(m=>m.delivered));
});

test('outbox gives other contacts a turn and bounds concurrent deliveries',async t=>{
 const {chat,identities,requests}=await queuedChat(5);t.after(()=>chat.stop());
 t.mock.timers.enable({apis:['setTimeout','Date'],now:10000});
 chat.flush();await settle();assert.equal(requests.length,1);
 for(let i=0;i<3;i++){t.mock.timers.tick(150);await settle();}
 assert.equal(requests.length,4);assert.equal(chat.active.size,4);
 t.mock.timers.tick(150);await settle();assert.equal(requests.length,4);
 requests[0].ack();await settle();
 assert.equal(requests.length,5);
 assert.equal(requests[4].peer,identities[4].id,'fifth contact precedes first contact backlog');
 assert.equal(chat.active.size,4);
});

test('outbox waits after failure, retries the same message and stops scheduled work',async t=>{
 const {chat,requests}=await queuedChat();t.after(()=>chat.stop());
 t.mock.timers.enable({apis:['setTimeout','Date'],now:10000});
 chat.flush();await settle();requests[0].reject(Error('offline'));await settle();
 t.mock.timers.tick(1999);await settle();assert.equal(requests.length,1);
 t.mock.timers.tick(1);await settle();assert.equal(requests.length,2);
 assert.equal(requests[1].id,requests[0].id);assert.equal(chat.state.messages[0].delivered,false);
 requests[1].ack();await settle();chat.stop();
 t.mock.timers.tick(10000);await settle();assert.equal(requests.length,2);
});

test('reactivating cancelled messages cannot exceed the outbox limit',async()=>{
 const {chat,identities}=await queuedChat(1,1);chat.online=false;
 const peer=identities[0].id,first=chat.state.messages[0].id;
 await chat.cancel(peer,first);
 for(let n=0;n<200;n++)await chat.send(peer,'Pending '+n);
 await assert.rejects(chat.retryMessage(peer,first),/200/);
 assert.equal(chat.state.messages.find(m=>m.id===first).cancelled,true);
 await chat.cancel(peer,chat.state.messages[1].id);
 await chat.retryMessage(peer,first);
 assert.equal(chat.state.messages.filter(m=>m.out&&!m.delivered&&!m.cancelled).length,200);
});

test('failed receipt persistence retains the packet and prevents advancing past it',async t=>{
 const {chat,requests}=await queuedChat();t.after(()=>chat.stop());
 t.mock.timers.enable({apis:['setTimeout','Date'],now:10000});
 const packet=structuredClone(chat.state.messages[0].packet);
 chat.flush();await settle();
 chat.vault.save=async()=>{throw Error('Disk full');};
 requests[0].ack();await settle();
 assert.equal(chat.state.messages[0].delivered,false);
 assert.deepEqual(chat.state.messages[0].packet,packet);
 t.mock.timers.tick(1999);await settle();assert.equal(requests.length,1);
 chat.vault.save=async()=>{};
 t.mock.timers.tick(1);await settle();assert.equal(requests[1].id,requests[0].id);
 requests[1].ack();await settle();
 t.mock.timers.tick(150);await settle();assert.equal(requests[2].id,chat.state.messages[1].id);
 requests[2].ack();await settle();assert.ok(chat.state.messages.every(m=>m.delivered));
});
test('contact, encrypted text and authenticated receipts',async()=>{
 const a=await newIdentity('Аня'),b=await newIdentity('Борис');
 const ca=await parseContact(contactCode(a),'Аня'),cb=await parseContact(contactCode(b),'Борис');
 const p={v:2,id:randomID(),from:a.id,to:b.id,kind:'text',text:'Привет <>& 👋\u2028'};
 const env=seal(p,cb,a);assert.deepEqual(open(env,ca,b),p);
 assert.throws(()=>open({...env,body:env.body.slice(0,-4)+'AAAA'},ca,b));
 const f=signFrame({type:'request',id:randomID(),from:a.address,to:b.address,data:goJSON(env)},a);
 assert.ok(verifyFrame(f));assert.equal(verifyFrame({...f,to:a.address}),false);
 assert.equal(verifyFrame({...f,data:'forged'}),false);
 const r=signFrame({type:'response',id:f.id,from:b.address,to:a.address,result:{status:200,data:'<>&\u2028'}},b);
 assert.ok(verifyFrame(r));assert.equal(verifyFrame({...r,result:{status:403}}),false);
 await assert.rejects(parseContact('blizko:3:'+Buffer.from(JSON.stringify({...cb,key:Array(32).fill(0)})).toString('base64url'),'bad'));
});
test('password backup rejects wrong password and round-trips the identity',async()=>{
 const state={v:1,identity:await newIdentity('Тест'),contacts:[],messages:[],drafts:{},read:{}};
 const raw=await exportBackup(state,'test password 123');
 assert.ok(!raw.includes(state.identity.secret));assert.deepEqual(await importBackup(raw,'test password 123'),state);
 await assert.rejects(importBackup(raw,'wrong password'));await assert.rejects(exportBackup(state,'short'));
});

test('clearing full history keeps the outbox and acknowledges a replay without resurrecting text',async()=>{
 const chat=new Chat('https://example.test');let saved;
 chat.vault={save:async state=>{saved=structuredClone(state);}};
 await chat.create('Аня');
 const bob=await newIdentity('Борис');
 await chat.addContact('Борис',contactCode(bob));
 const self=await parseContact(contactCode(chat.state.identity),'Аня');
 const incoming={v:2,id:randomID(),from:bob.id,to:self.id,kind:'text',text:'Сообщение до очистки'};
 const encrypted=goJSON(seal(incoming,self,bob));
 const first=await chat.accept(bob.address,encrypted);assert.equal(first.status,200);
 await chat.send(bob.id,'Остаётся в очереди');
 const pending=chat.state.messages.find(m=>m.out);
 await chat.cancel(bob.id,pending.id);
 await chat.draft(bob.id,'Остаётся в черновике');
 for(let n=chat.state.messages.length;n<2000;n++)chat.state.messages.push({id:randomID(),peer:bob.id,text:'Доставлено',out:true,delivered:true,time:1});
 await assert.rejects(chat.send(bob.id,'Нет места'),/История заполнена/);
 assert.equal(await chat.clearHistory(),1999);
 assert.equal(chat.state.messages.filter(m=>!m.archived).length,1);
 assert.ok(chat.state.messages.find(m=>m.id===pending.id).packet);
 assert.equal(chat.state.drafts[bob.id],'Остаётся в черновике');
 assert.equal(JSON.stringify(saved).includes(incoming.text),false);
 const replay=await chat.accept(bob.address,encrypted);assert.equal(replay.status,200);
 const bobContact=await parseContact(contactCode(bob),'Борис');
 assert.equal(open(JSON.parse(replay.data),self,bob).ack,incoming.id);
 assert.equal(chat.state.messages.filter(m=>!m.archived).length,1);
 const tampered=goJSON(seal({...incoming,text:'Подмена с тем же ID'},self,bob));
 assert.notEqual((await chat.accept(bob.address,tampered)).status,200);
 const raw=await exportBackup(chat.state,'backup password 123');
 assert.deepEqual(await importBackup(raw,'backup password 123'),chat.state);
 await chat.send(bob.id,'Снова есть место');
});

test('failed persistence never acknowledges or drops a message during clearing',async()=>{
 const chat=new Chat('https://example.test');let fail=false;
 chat.vault={save:async()=>{if(fail)throw Error('Disk full');}};
 await chat.create('Аня');const bob=await newIdentity('Борис');await chat.addContact('Борис',contactCode(bob));
 const self=await parseContact(contactCode(chat.state.identity),'Аня');
 const packet=goJSON(seal({v:2,id:randomID(),from:bob.id,to:self.id,kind:'text',text:'Нельзя терять'},self,bob));
 fail=true;assert.equal((await chat.accept(bob.address,packet)).status,503);assert.equal(chat.state.messages.length,0);
 fail=false;assert.equal((await chat.accept(bob.address,packet)).status,200);
 const before=structuredClone(chat.state);fail=true;await assert.rejects(chat.clearHistory());assert.deepEqual(chat.state,before);
});


test('server storage advances the paced queue without claiming delivery or permitting cancellation',async t=>{
 const {chat,requests}=await queuedChat();t.after(()=>chat.stop());
 t.mock.timers.enable({apis:['setTimeout','Date'],now:10000});
 chat.flush();await settle();requests[0].stored();await settle();
 const m=chat.state.messages[0];assert.equal(m.delivered,false);assert.equal(m.submitted,true);assert.ok(m.storedUntil>Date.now());
 await assert.rejects(chat.cancel(m.peer,m.id),/Отмена недоступна/);
 t.mock.timers.tick(150);await settle();assert.equal(requests.length,2);assert.equal(requests[1].id,chat.state.messages[1].id);
 requests[1].ack();await settle();
 t.mock.timers.tick(29849);await settle();assert.equal(requests.length,2,'stored packet is not polled early');
 t.mock.timers.tick(1);await settle();assert.equal(requests[2].id,m.id);requests[2].ack();await settle();
 assert.ok(chat.state.messages.every(m=>m.delivered));
});
