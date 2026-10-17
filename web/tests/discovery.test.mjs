import {test} from 'node:test';
import assert from 'node:assert/strict';
import nacl from 'tweetnacl';
import {RelayDirectory,verifyDirectory} from '../src/discovery.js';
const key=nacl.sign.keyPair(),now=Date.now();
function signed(url,revision=now,expires=now+7*86400000){const payload=Buffer.from(JSON.stringify({v:1,url,revision,expires}));const signature=nacl.sign.detached(Buffer.concat([Buffer.from('blizko-relay-directory-v1\0'),payload]),key.secretKey);return JSON.stringify({payload:payload.toString('base64'),signature:Buffer.from(signature).toString('base64')});}
test('directory requires authentic, current metadata and a safe HTTPS URL',()=>{
 assert.equal(verifyDirectory(signed('https://relay.example/'),key.publicKey).url,'https://relay.example/');
 assert.throws(()=>verifyDirectory(signed('https://relay.example/'),nacl.sign.keyPair().publicKey));
 for(const url of ['http://relay.example/','https://user@relay.example/','https://127.0.0.1/','https://relay.example/path','https://relay.example/?x=1'])assert.throws(()=>verifyDirectory(signed(url),key.publicKey));
 assert.throws(()=>verifyDirectory(signed('https://relay.example/',now,now-1),key.publicKey));
 assert.throws(()=>verifyDirectory(signed('https://relay.example/',now,now+9*86400000),key.publicKey));
});
test('rotation changes only the relay, survives directory outage, and rejects rollback',async()=>{
 const values=new Map(),calls=[];let raw=signed('https://first.example/');
 const options={key:key.publicKey,storage:{getItem:k=>values.get(k),setItem:(k,v)=>values.set(k,v)},fetcher:async()=>new Response(raw),onURL:u=>calls.push(u)};
 const d=new RelayDirectory(options);await d.refresh();raw=signed('https://second.example/',now+1);await d.refresh();
 assert.deepEqual(calls,['https://first.example/','https://second.example/']);
 raw=signed('https://first.example/');await d.refresh();assert.equal(calls.length,2);
 raw='broken';await d.refresh();assert.equal(calls.length,2);
 new RelayDirectory(options);assert.equal(calls.at(-1),'https://second.example/');
 assert.deepEqual([...values.keys()],['blizko-relay-directory-v1']);
});
