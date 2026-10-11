import {chromium,webkit} from 'playwright';
import {spawn} from 'node:child_process';
import {createInterface} from 'node:readline';
import {mkdtemp,mkdir,readFile,writeFile} from 'node:fs/promises';
import {join,resolve} from 'node:path';
import {setTimeout as pause} from 'node:timers/promises';
import assert from 'node:assert/strict';
import QRCode from 'qrcode';

const root=resolve(import.meta.dirname,'../..'), output=process.env.BLIZKO_WEB_TEST_OUTPUT||join(root,'.tools/web-checks');await mkdir(output,{recursive:true});
const base=process.env.BLIZKO_WEB_TEST_URL||'http://127.0.0.1:39443';
let server=process.env.BLIZKO_WEB_TEST_URL?null:spawn(join(root,'.tools/Blizko.WebTestServer.exe'),['-listen','127.0.0.1:39443','-web-origin',base],{windowsHide:true});
let serverLog='';server?.stderr.on('data',b=>serverLog+=b);
async function until(fn,label,timeout=30000){const end=Date.now()+timeout;while(Date.now()<end){if(await fn())return;await pause(150);}throw Error('Timeout: '+label);}
await until(async()=>{try{return (await fetch(base+'/healthz')).status===204;}catch{return false;}},'server');
async function visit(p){await p.goto(base+'/web/');const gate=p.getByRole('button',{name:'Посетить',exact:true});if(await gate.isVisible())await gate.click();}
const summary=[];
try {
 for(const name of (process.env.BLIZKO_TEST_BROWSERS||'chromium,webkit').split(',')) {
  const temp=await mkdtemp(join(output,name+'-')), browser=await ({chromium,webkit})[name].launch({headless:true});
  const context=await browser.newContext({viewport:{width:390,height:844},deviceScaleFactor:2,isMobile:true,hasTouch:true,acceptDownloads:true});
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  const peer=spawn(join(root,'.tools/Blizko.WebTestPeer.exe'),[base,join(temp,'native')],{windowsHide:true});
  let peerLog='';peer.stderr.on('data',b=>peerLog+=b);const answers=[];createInterface({input:peer.stdout}).on('line',l=>{answers.shift()?.(JSON.parse(l));});
  const command=req=>new Promise((resolve,reject)=>{const timer=setTimeout(()=>reject(Error('Native peer timeout '+peerLog)),15000);answers.push(v=>{clearTimeout(timer);v.error?reject(Error(v.error)):resolve(v.value);});peer.stdin.write(JSON.stringify(req)+'\n');});
  try {
   await visit(page);await page.locator('#my-name').fill('Айфон · тест');
   // Failing the state write after key insertion must abort both records.
   await page.evaluate(()=>{window.originalPut=IDBObjectStore.prototype.put;IDBObjectStore.prototype.put=function(...args){if(args[1]==='state')throw new DOMException('Quota full','QuotaExceededError');return window.originalPut.apply(this,args);};});
   await page.locator('#create-form button').click();await page.locator('#toast').waitFor({state:'visible'});
   const partial=await page.evaluate(()=>new Promise((resolve,reject)=>{const r=indexedDB.open('blizko-web-v1');r.onerror=()=>reject(r.error);r.onsuccess=()=>{const db=r.result,tx=db.transaction('vault'),q=tx.objectStore('vault').getAllKeys();tx.oncomplete=()=>{db.close();resolve(q.result);};};}));
   assert.deepEqual(partial,[],'failed creation must leave no orphan key');
   await page.evaluate(()=>{IDBObjectStore.prototype.put=window.originalPut;});
   await page.locator('#create-form button').click();
   await page.locator('#app').waitFor({state:'visible'});await until(()=>page.locator('#status').textContent().then(t=>t.includes('Подключено')),'browser authenticated');
   await page.locator('#qr-open').click();const code=await page.locator('#own-code').inputValue();const identity=JSON.parse(Buffer.from(code.slice(9),'base64url'));await page.locator('#qr-dialog [data-close]').click();
   await command({op:'add',name:'Web',code});const nativeCode=await command({op:'code'});
   const qr=join(temp,'native-qr.png');await QRCode.toFile(qr,nativeCode,{width:900,margin:4});
   await page.locator('#add-open').click();await page.locator('#contact-name').fill('Компьютер');await page.locator('#photo-file').setInputFiles(qr);
   await until(()=>page.locator('#contact-code').inputValue().then(t=>t===nativeCode),'QR decoded');await page.locator('#add-form [type=submit]').click();
   await page.locator('#chat').waitFor({state:'visible'});
   const hello='Привет с iPhone 👋 <>& «Близко»\u2028вторая строка';await page.locator('#message').fill(hello);await page.locator('#send').click();
   await until(async()=> (await command({op:'snapshot'})).messages.some(m=>m.text===hello&&!m.out),'web -> native');
   await until(()=>page.locator('.bubble-meta').allTextContents().then(a=>a.some(s=>s.includes('Доставлено'))),'receipt');
   await command({op:'send',peer:identity.id,text:'Ответ из общего ядра Android/Windows 🔐'});
   await page.locator('#messages').getByText('Ответ из общего ядра Android/Windows 🔐',{exact:true}).waitFor();
   await until(async()=> (await command({op:'snapshot'})).messages.some(m=>m.out&&m.delivered),'native receipt');
   await page.screenshot({animations:'disabled',path:join(output,name+'-chat.png')});
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'mobile horizontal overflow');
   // Simulate a newer page committing after this page acquired its snapshot.
   await page.evaluate(()=>new Promise((resolve,reject)=>{const r=indexedDB.open('blizko-web-v1');r.onsuccess=()=>{const db=r.result,tx=db.transaction('vault','readwrite'),s=tx.objectStore('vault'),q=s.get('revision');q.onsuccess=()=>s.put(q.result+1,'revision');tx.oncomplete=()=>{db.close();resolve();};tx.onabort=()=>reject(tx.error);};}));
   await page.locator('#message').fill('Не перезаписывать новую историю');
   await until(()=>page.locator('#toast').textContent().then(t=>t.includes('другой вкладке')),'stale state rejected');
   await page.reload();await page.getByRole('button',{name:'Открыть чат: Компьютер'}).click();
   assert.equal(await page.locator('#message').inputValue(),'','stale draft did not overwrite newer state');
   // A failed IndexedDB commit must never create a receipt or erase the draft.
   await page.evaluate(()=>{window.originalPut=IDBObjectStore.prototype.put;IDBObjectStore.prototype.put=function(...args){if(args[1]==='state')throw new DOMException('Quota full','QuotaExceededError');return window.originalPut.apply(this,args);};});
   await command({op:'send',peer:identity.id,text:'Сначала сохранение, потом подтверждение'});await pause(2500);
   const notSaved=(await command({op:'snapshot'})).messages.find(m=>m.text==='Сначала сохранение, потом подтверждение');assert.equal(notSaved.delivered,false);
   assert.equal(await page.locator('#messages').getByText('Сначала сохранение, потом подтверждение',{exact:true}).count(),0);
   await page.evaluate(()=>{IDBObjectStore.prototype.put=window.originalPut;});
   await page.locator('#messages').getByText('Сначала сохранение, потом подтверждение',{exact:true}).waitFor({timeout:35000});
   // Native peer offline, queue survives browser reload with the same identity.
   await command({op:'stop'});await pause(200);await page.locator('#message').fill('Сохранено без собеседника');await page.locator('#send').click();
   await page.locator('#messages').getByText('Сохранено без собеседника',{exact:true}).waitFor();await page.locator('#message').fill('Черновик после перезапуска');await pause(500);
   await page.reload();await page.getByRole('button',{name:'Открыть чат: Компьютер'}).click();assert.equal(await page.locator('#message').inputValue(),'Черновик после перезапуска');
   await command({op:'start'});await until(async()=> (await command({op:'snapshot'})).messages.filter(m=>m.text==='Сохранено без собеседника').length===1,'offline queue delivered once');
   await page.locator('#back').click();await page.locator('#qr-open').click();assert.equal(await page.locator('#own-code').inputValue(),code,'stable identity');await page.locator('#qr-dialog [data-close]').click();
   // A second tab cannot race the vault and replace the active socket.
   const second=await context.newPage();await second.goto(base+'/web/');await second.locator('#fatal').waitFor({state:'visible'});assert.match(await second.locator('#fatal-text').textContent(),/другой вкладке/);await second.close();
   const raw=await page.evaluate(()=>new Promise((resolve,reject)=>{const r=indexedDB.open('blizko-web-v1');r.onsuccess=()=>{const db=r.result,tx=db.transaction('vault'),s=tx.objectStore('vault'),a=s.get('state'),b=s.get('key');tx.oncomplete=()=>resolve({encrypted:a.result.body instanceof ArrayBuffer,extractable:b.result.extractable,raw:JSON.stringify(a.result)});tx.onerror=()=>reject(tx.error);};}));
   assert.equal(raw.encrypted,true);assert.equal(raw.extractable,false);assert.ok(!raw.raw.includes(hello));
   // Download and restore the actual encrypted backup through the interface.
   await page.locator('#settings-open').click();await page.locator('#backup-open').click();await page.locator('#backup-password').fill('backup-test-password');
   const download=page.waitForEvent('download');await page.locator('#backup-submit').click();const file=join(temp,'backup.json');await (await download).saveAs(file);
   const backup=await readFile(file,'utf8');assert.ok(!backup.includes(hello));
   await context.close();
   const restored=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true});const restoredPage=await restored.newPage();await visit(restoredPage);await restoredPage.locator('#restore-open').click();await restoredPage.locator('#backup-file').setInputFiles(file);await restoredPage.locator('#backup-password').fill('backup-test-password');await restoredPage.locator('#backup-submit').click();await restoredPage.locator('#app').waitFor({state:'visible'});await restoredPage.getByRole('button',{name:'Открыть чат: Компьютер'}).click();await restoredPage.locator('#messages').getByText(hello,{exact:true}).waitFor();
   // Offline shell and history after the service worker takes control.
   await restoredPage.evaluate(()=>navigator.serviceWorker.ready);await restoredPage.reload();
   const workerState=await restoredPage.evaluate(async()=>({controlled:!!navigator.serviceWorker.controller,cacheKeys:await caches.keys()}));
   assert.equal(workerState.controlled,true,'service worker controls the page');
   if(name==='webkit' && server) {server.kill();await pause(1000);} else await restored.setOffline(true);
   await restoredPage.reload();await restoredPage.getByRole('button',{name:'Открыть чат: Компьютер'}).click();await restoredPage.locator('#messages').getByText(hello,{exact:true}).waitFor();
   if(name==='webkit' && server) {server=spawn(join(root,'.tools/Blizko.WebTestServer.exe'),['-listen','127.0.0.1:39443','-web-origin',base],{windowsHide:true});server.stderr.on('data',b=>serverLog+=b);} else await restored.setOffline(false);
   await restoredPage.locator('#back').click();await restoredPage.screenshot({animations:'disabled',path:join(output,name+'-contacts.png')});
   await restoredPage.locator('#settings-open').click();await restoredPage.locator('#clear-open').click();await restoredPage.locator('#clear-confirm').click();await restoredPage.locator('#clear-dialog').waitFor({state:'hidden'});await restoredPage.locator('#settings-dialog [data-close]').click();
   await restoredPage.getByRole('button',{name:'Открыть чат: Компьютер'}).click();assert.equal(await restoredPage.locator('#messages .bubble').count(),0,'cleared history is hidden');
   assert.equal(await restoredPage.locator('#message').inputValue(),'Черновик после перезапуска','clearing preserves drafts');
   await restoredPage.reload();await restoredPage.getByRole('button',{name:'Открыть чат: Компьютер'}).click();assert.equal(await restoredPage.locator('#messages .bubble').count(),0,'cleared history remains cleared after reopening');
   await restored.close();
   assert.deepEqual(errors,[],'browser errors');summary.push(name+': PASS — Go interop, encrypted receipts, QR photo, storage failure, offline outbox, reload, draft, tab lock/CAS, atomic profile creation, backup/restore, offline shell, history clearing, mobile layout');console.log(summary.at(-1));
  }catch(e){await page.screenshot({animations:'disabled',path:join(output,name+'-failure.png')}).catch(()=>{});throw e;}
  finally{peer.stdin.end();peer.kill();await browser.close();}
 }
 await writeFile(join(output,'result.txt'),summary.join('\n')+'\n');
}finally{server?.kill();if(serverLog)console.log(serverLog.trim());}
