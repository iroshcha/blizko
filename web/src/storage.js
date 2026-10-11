import {utf8,text,b64,unb64,validateIdentity} from './protocol.js';

const AAD=utf8('blizko-web-v1');
const request = r => new Promise((resolve,reject)=>{r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
const finished = tx => new Promise((resolve,reject)=>{tx.oncomplete=resolve;tx.onabort=()=>reject(tx.error||Error('Запись отменена'));tx.onerror=()=>{};});

export class Vault {
 async init() {
  const r=indexedDB.open('blizko-web-v1',1);
  r.onupgradeneeded=()=>r.result.createObjectStore('vault');
  this.db=await request(r);
  this.db.onversionchange=()=>this.db.close();
  const tx=this.db.transaction('vault','readonly'), store=tx.objectStore('vault');
  const [key,encrypted,revision]=await Promise.all([request(store.get('key')),request(store.get('state')),request(store.get('revision'))]);
  this.revision=revision||0;
  if(!key&&encrypted)throw Error('Ключ хранилища потерян. Данные сохранены без изменений.');
  if(key&&!encrypted)throw Error('Запись профиля потеряна. Хранилище не изменено.');
  this.key=key||await crypto.subtle.generateKey({name:'AES-GCM',length:256},false,['encrypt','decrypt']);
  this.freshKey=!key;
  if(!encrypted)return null;
  try {
   const raw=await crypto.subtle.decrypt({name:'AES-GCM',iv:encrypted.iv,additionalData:AAD},this.key,encrypted.body);
   return await validateState(JSON.parse(text(new Uint8Array(raw))));
  }catch {throw Error('Не удалось открыть историю. Данные не удалены. Восстановите резервную копию в другом браузере.');}
 }
 async save(state) {
  const raw=utf8(JSON.stringify(state));
  if(raw.length>8*1024*1024)throw Error('Хранилище заполнено. Сохраните резервную копию.');
  const iv=crypto.getRandomValues(new Uint8Array(12));
  const body=await crypto.subtle.encrypt({name:'AES-GCM',iv,additionalData:AAD},this.key,raw);
  const tx=this.db.transaction('vault','readwrite'), done=finished(tx),store=tx.objectStore('vault');
  // A discarded page may release its Web Lock while an IDB write is finishing.
  // Compare-and-swap prevents either generation from overwriting newer history.
  let failure;
  const check=store.get('revision');
  check.onsuccess=()=>{
   try {
    if((check.result||0)!==this.revision)throw Error('Хранилище изменено в другой вкладке. Перезапустите приложение.');
    if(this.freshKey)store.put(this.key,'key');
    store.put({iv,body},'state');store.put(this.revision+1,'revision');
   }catch(e){failure=e;tx.abort();}
  };
  try{await done;}catch(e){throw failure||e;}
  this.revision++;this.freshKey=false;
 }
}
export async function validateState(s) {
 if(!s||s.v!==1||!Array.isArray(s.contacts)||s.contacts.length>100||!Array.isArray(s.messages)||s.messages.length>40000||s.messages.filter(m=>!m?.archived).length>2000||!s.drafts||!s.read)throw Error('Неверный формат копии');
 await validateIdentity(s.identity);
 // Imported backups are authenticated, but still validate every displayed field.
 const ids=new Set();
 for(const c of s.contacts) {
  const {parseContact}=await import('./protocol.js');
  const code='blizko:3:'+b64(utf8(JSON.stringify(c))).replaceAll('+','-').replaceAll('/','_').replace(/=+$/,'');
  await parseContact(code,c.name);
  if(ids.has(c.id)||c.id===s.identity.id)throw Error('Дубликат контакта');ids.add(c.id);
 }
 const messageIDs=new Set();
 for(const m of s.messages) {
  if(!m||!ids.has(m.peer)||!/^[a-f0-9]{32}$/.test(m.id)||!Number.isFinite(m.time)||typeof m.out!=='boolean'||typeof m.delivered!=='boolean')throw Error('Повреждена история');
  const id=m.peer+'/'+m.id+'/'+m.out;
  if(messageIDs.has(id))throw Error('Повтор записи в истории');messageIDs.add(id);
  if(m.archived) {
   if(m.archived!==true||m.out||!m.delivered||m.text!==undefined||m.packet!==undefined||!/^[a-f0-9]{64}$/.test(m.digest))throw Error('Повреждено подтверждение');
  }else if(typeof m.text!=='string'||!utf8(m.text).length||utf8(m.text).length>4000)throw Error('Повреждена история');
 }
 for(const [id,draft] of Object.entries(s.drafts))if(!ids.has(id)||typeof draft!=='string'||utf8(draft).length>16000)throw Error('Повреждён черновик');
 return s;
}
async function backupKey(password,salt) {
 if([...password].length<10)throw Error('Для копии нужен пароль от 10 символов');
 const material=await crypto.subtle.importKey('raw',utf8(password),'PBKDF2',false,['deriveKey']);
 return crypto.subtle.deriveKey({name:'PBKDF2',hash:'SHA-256',salt,iterations:310000},material,{name:'AES-GCM',length:256},false,['encrypt','decrypt']);
}
export async function exportBackup(s,password) {
 const salt=crypto.getRandomValues(new Uint8Array(16)),iv=crypto.getRandomValues(new Uint8Array(12));
 const key=await backupKey(password,salt);
 const cipher=new Uint8Array(await crypto.subtle.encrypt({name:'AES-GCM',iv,additionalData:AAD},key,utf8(JSON.stringify(s))));
 // Chunking avoids overflowing the stack for a large backup.
 let binary='';for(let k=0;k<cipher.length;k+=16384)binary+=String.fromCharCode(...cipher.subarray(k,k+16384));
 return JSON.stringify({format:'blizko-web-backup-1',salt:b64(salt),iv:b64(iv),body:btoa(binary)});
}
export async function importBackup(raw,password) {
 if(raw.length>12*1024*1024)throw Error('Файл слишком большой');
 const v=JSON.parse(raw);
 if(v.format!=='blizko-web-backup-1'||typeof v.body!=='string')throw Error('Неверный файл копии');
 const salt=unb64(v.salt),iv=unb64(v.iv);
 if(salt.length!==16||iv.length!==12)throw Error('Неверная копия');
 const key=await backupKey(password,salt);
 let decoded;
 try { decoded=await crypto.subtle.decrypt({name:'AES-GCM',iv,additionalData:AAD},key,Uint8Array.from(atob(v.body),c=>c.charCodeAt(0))); }
 catch {throw Error('Неверный пароль или повреждённая копия');}
 return validateState(JSON.parse(text(new Uint8Array(decoded))));
}
