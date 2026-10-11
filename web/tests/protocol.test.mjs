import {test} from 'node:test';
import assert from 'node:assert/strict';
import {newIdentity,contactCode,parseContact,goJSON,seal,open,signFrame,verifyFrame,randomID} from '../src/protocol.js';
import {exportBackup,importBackup} from '../src/storage.js';
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
