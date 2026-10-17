import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';

test('closed-app worker shows only generic notification and opens its own origin',async()=>{
 const events={},notifications=[];let focused=0,opened='';
 const self={location:{origin:'https://blizko.example'},registration:{scope:'https://blizko.example/web/',showNotification:async(...args)=>notifications.push(args)},addEventListener:(name,fn)=>events[name]=fn,clients:{matchAll:async()=>[{url:'https://evil.example/web/',focus:()=>{throw Error('foreign window')}},{url:'https://blizko.example/web/',focus:async()=>focused++}],openWindow:async url=>opened=url}};
 vm.runInNewContext(await readFile(new URL('../src/sw.js',import.meta.url),'utf8'),{self,URL});
 let complete;events.push({data:{json:()=>({text:'PRIVATE MESSAGE',url:'https://evil.example'})},waitUntil:p=>complete=p});await complete;
 assert.equal(notifications.length,1);assert.equal(notifications[0][0],'Близко');assert.equal(notifications[0][1].body,'Новое сообщение. Откройте приложение, чтобы прочитать.');assert.ok(!JSON.stringify(notifications).includes('PRIVATE'));
 const click={notification:{data:{url:'https://evil.example'},close:()=>{}},waitUntil:p=>complete=p};events.notificationclick(click);await complete;assert.equal(focused,1);assert.equal(opened,'');
 self.clients.matchAll=async()=>[];events.notificationclick(click);await complete;assert.equal(opened,'https://blizko.example/web/');
});
