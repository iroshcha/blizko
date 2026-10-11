import {test} from 'node:test';
import assert from 'node:assert/strict';
import {newIdentity,contactCode,parseContact,goJSON,seal,open,signFrame,verifyFrame,randomID} from '../src/protocol.js';
import {exportBackup,importBackup} from '../src/storage.js';
import {Chat} from '../src/client.js';
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
