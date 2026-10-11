import QRCode from 'qrcode';
import jsQR from 'jsqr';
import {Chat} from './client.js';
import {contactCode} from './protocol.js';
import {exportBackup,importBackup} from './storage.js';

const $=id=>document.getElementById(id);
const chat=new Chat();
let current='',messageKey='',toastTimer,backupMode='export',registration,releaseLock;
function toast(s){$('toast').textContent=s;$('toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').hidden=true,6500);}
function fail(e){toast(e?.message||'Действие не выполнено. Попробуйте снова.');}
function fatal(e){$('welcome').hidden=true;$('app').hidden=true;$('fatal').hidden=false;$('fatal-text').textContent=e.message;}
function element(tag,cls,value){const e=document.createElement(tag);if(cls)e.className=cls;if(value!==undefined)e.textContent=value;return e;}
function dialog(id){$(id).showModal();}
function action(id,fn){$(id).addEventListener('click',async()=>{const b=$(id);b.disabled=true;try{await fn();}catch(e){fail(e);}finally{b.disabled=false;}});}
function form(id,fn){$(id).addEventListener('submit',async e=>{e.preventDefault();const button=$(id).querySelector('[type=submit]');button.disabled=true;try{await fn();}catch(error){fail(error);}finally{button.disabled=false;}});}
function initial(name){return [...name][0]?.toLocaleUpperCase('ru')||'·';}
const time=t=>new Date(t).toLocaleTimeString('ru',{hour:'2-digit',minute:'2-digit'});
const day=t=>new Date(t).toLocaleDateString('ru',{day:'numeric',month:'long'});
function render() {
 const s=chat.state;if(!s)return;
 $('welcome').hidden=true;$('app').hidden=false;
 $('status').textContent=chat.online?'Подключено к вашему серверу':'Нет связи с сервером';
 $('status-dot').classList.toggle('online',chat.online);$('reconnect').hidden=chat.online;
 $('contact-count').textContent=String(s.contacts.length);
 const search=$('search').value.toLocaleLowerCase('ru');
 const list=$('contacts');list.replaceChildren();
 for(const c of s.contacts) {
  if(!c.name.toLocaleLowerCase('ru').includes(search))continue;
  const all=s.messages.filter(m=>m.peer===c.id&&!m.archived), last=all.at(-1);
  const button=element('button','contact'+(current===c.id?' selected':''));button.setAttribute('aria-label',`Открыть чат: ${c.name}`);
  button.append(element('div','avatar',initial(c.name)));
  const info=element('div','contact-info');info.append(element('div','contact-name',c.name),element('div','preview',last?(last.out?'Вы: ':'')+last.text:'Обменяйтесь QR и поздоровайтесь'));button.append(info);
  const unread=all.filter(m=>!m.out&&m.time>(s.read[c.id]||0)).length;
  if(unread&&current!==c.id)button.append(element('span','unread',String(unread)));
  button.onclick=()=>openChat(c.id).catch(fail);list.append(button);
 }
 $('empty-contacts').hidden=s.contacts.length>0;list.hidden=!s.contacts.length;
 $('no-chat').hidden=!!current;$('chat').hidden=!current;
 if(!current)return;
 const c=s.contacts.find(c=>c.id===current);if(!c)return;
 $('chat-name').textContent=c.name;$('chat-avatar').textContent=initial(c.name);
 const issue=chat.issues.get(current);$('delivery-issue').hidden=!issue;$('delivery-issue').textContent=issue||'';
 const messages=s.messages.filter(m=>m.peer===current&&!m.archived),key=JSON.stringify(messages);
 if(key===messageKey)return;messageKey=key;
 const box=$('messages'),nearBottom=box.scrollHeight-box.scrollTop-box.clientHeight<100;
 box.replaceChildren();let previous='';
 if(!messages.length){const empty=element('div','empty-contacts');empty.append(element('h2','',`Напишите ${c.name}`),element('p','','Для переписки добавьте QR-коды друг друга и откройте приложения на обоих устройствах.'));box.append(empty);}
 for(const m of messages) {
  const date=day(m.time);if(date!==previous){box.append(element('div','day-label',date));previous=date;}
  const b=element('article','bubble'+(m.out?' out':''));b.append(element('div','bubble-text',m.text));
  b.append(element('div','bubble-meta',time(m.time)+(m.out?' · '+(m.delivered?'Доставлено ✓✓':m.cancelled?'Отменено':'В очереди ◷'):'')));
  if(m.out&&!m.delivered){const controls=element('div','bubble-actions');const retry=element('button','',m.cancelled?'Отправить снова':'Повторить');retry.onclick=()=>chat.retryMessage(m.peer,m.id).catch(fail);controls.append(retry);if(!m.cancelled){const cancel=element('button','','Отменить');cancel.onclick=()=>chat.cancel(m.peer,m.id).catch(fail);controls.append(cancel);}b.append(controls);}
  box.append(b);
 }
 if(nearBottom||messages.at(-1)?.out)box.scrollTop=box.scrollHeight;
}
async function openChat(id) {
 current=id;messageKey='';$('app').classList.add('in-chat');$('message').value=chat.state.drafts[id]||'';render();$('messages').scrollTop=$('messages').scrollHeight;
 await chat.markRead(id);
}
chat.addEventListener('change',()=>{
 render();
 if(current&&document.visibilityState==='visible'&&chat.state.messages.some(m=>m.peer===current&&!m.out&&!m.archived&&m.time>(chat.state.read[current]||0)))chat.markRead(current).catch(fail);
});
$('search').addEventListener('input',render);
$('message').addEventListener('input',()=>{if(current)chat.draft(current,$('message').value).catch(fail);});
form('create-form',async()=>{await chat.create($('my-name').value);chat.start();await navigator.storage?.persist?.();});
form('send-form',async()=>{const peer=current,value=$('message').value;await chat.send(peer,value);if(current===peer&&$('message').value===value)$('message').value='';});
action('back',()=>{current='';$('app').classList.remove('in-chat');render();});
action('reconnect',()=>chat.reconnect());
action('probe',async()=>toast(await chat.probe(current)));
action('add-open',()=>dialog('add-dialog'));
form('add-form',async()=>{const id=await chat.addContact($('contact-name').value,$('contact-code').value);$('add-dialog').close();$('add-form').reset();await openChat(id);toast('Контакт добавлен. Не забудьте показать свой QR.');});
action('qr-open',async()=>{
 const code=contactCode(chat.state.identity);$('qr-name').textContent=chat.state.identity.name;$('own-code').value=code;
 await QRCode.toCanvas($('qr-canvas'),code,{width:640,margin:4,errorCorrectionLevel:'M',color:{dark:'#163f36',light:'#ffffff'}});dialog('qr-dialog');
});
action('copy-code',async()=>{try{await navigator.clipboard.writeText($('own-code').value);toast('Код скопирован');}catch{$('own-code').focus();$('own-code').select();toast('Выделили код: выберите «Скопировать».');}});
action('settings-open',()=>{$('profile-name').textContent=chat.state.identity.name;dialog('settings-dialog');});
action('clear-open',()=>dialog('clear-dialog'));
action('clear-confirm',async()=>{const removed=await chat.clearHistory();$('clear-dialog').close();toast(`Очищено сообщений: ${removed}. Неотправленные сообщения и черновики сохранены.`);});
for(const button of document.querySelectorAll('[data-close]'))button.addEventListener('click',()=>button.closest('dialog').close());
action('scan-camera',()=>$('camera-file').click());action('scan-photo',()=>$('photo-file').click());
for(const id of ['camera-file','photo-file'])$(id).addEventListener('change',async()=>{
 const file=$(id).files[0];if(!file)return;
 let url;
 try {
  if(file.size>20*1024*1024)throw Error('Фото слишком большое — максимум 20 МБ');
  url=URL.createObjectURL(file);const img=new Image();img.src=url;await img.decode();
  const scale=Math.min(1,1600/Math.max(img.width,img.height));const canvas=document.createElement('canvas');canvas.width=Math.round(img.width*scale);canvas.height=Math.round(img.height*scale);
  const ctx=canvas.getContext('2d',{willReadFrequently:true});ctx.drawImage(img,0,0,canvas.width,canvas.height);const pixels=ctx.getImageData(0,0,canvas.width,canvas.height);
  const result=jsQR(pixels.data,pixels.width,pixels.height,{inversionAttempts:'attemptBoth'});
  if(!result||!result.data.startsWith('blizko:3:'))throw Error('QR «Близко» не найден. Снимите его ближе или вставьте код.');
  $('contact-code').value=result.data;toast('QR прочитан. Укажите имя и нажмите «Добавить».');
 }catch(e){fail(e);}finally{if(url)URL.revokeObjectURL(url);$(id).value='';}
});
function backupDialog(mode) {
 backupMode=mode;$('backup-form').reset();$('backup-file-label').hidden=mode!=='import';$('backup-file').required=mode==='import';
 $('backup-title').textContent=mode==='import'?'Восстановить профиль':'Резервная копия';
 $('backup-description').textContent=mode==='import'?'Выберите копию веб-версии «Близко» и введите её пароль. После восстановления закройте приложение со старым профилем на другом устройстве.':'Файл содержит ваши ключи, контакты и историю. Он зашифрован паролем. Сохраните файл и пароль отдельно.';
 $('backup-submit').textContent=mode==='import'?'Восстановить':'Скачать копию';dialog('backup-dialog');
}
action('backup-open',()=>backupDialog('export'));action('restore-open',()=>backupDialog('import'));
form('backup-form',async()=>{
 const password=$('backup-password').value;
 if(backupMode==='import'){
  if(chat.state)throw Error('Восстановление доступно только до создания профиля');
  const file=$('backup-file').files[0];if(!file||file.size>12*1024*1024)throw Error('Выберите файл копии до 12 МБ');
  const state=await importBackup(await file.text(),password);await chat.vault.save(state);chat.state=state;render();chat.start();toast('Профиль и история восстановлены');
 }else{
  await chat.chain;const raw=await exportBackup(chat.state,password),url=URL.createObjectURL(new Blob([raw],{type:'application/json'}));
  const a=document.createElement('a');a.href=url;a.download='blizko-backup-'+new Date().toISOString().slice(0,10)+'.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),60000);toast('Сохраните загруженную копию и её пароль');
 }
 $('backup-password').value='';$('backup-dialog').close();
});
action('reload',()=>location.reload());
action('update-check',async()=>{if(!registration)throw Error('Обновления доступны при установке через HTTPS');await registration.update();$('update-status').textContent=registration.waiting?'Доступна новая версия. Можно обновить.':'Проверка выполнена. Если найдена новая версия, появится кнопка обновления.';});
action('update-apply',async()=>{await chat.chain;registration?.waiting?.postMessage('activate');});
async function setupWorker() {
 if(!('serviceWorker' in navigator))return;
 registration=await navigator.serviceWorker.register('./sw.js',{scope:'./'});
 const waiting=()=>{if(registration.waiting&&navigator.serviceWorker.controller){$('update-apply').hidden=false;$('update-status').textContent='Новая версия готова. Обновите приложение, чтобы применить.';toast('Доступно обновление веб-приложения');}};
 waiting();registration.addEventListener('updatefound',()=>{const worker=registration.installing;worker?.addEventListener('statechange',waiting);});
 let reloading=false;navigator.serviceWorker.addEventListener('controllerchange',()=>{if(!reloading){reloading=true;location.reload();}});
}
async function boot() {
 if(!window.isSecureContext||!crypto.subtle||!navigator.locks)throw Error('Откройте приложение в современном Safari через защищённую ссылку HTTPS.');
 await new Promise((resolve,reject)=>navigator.locks.request('blizko-web-active',{ifAvailable:true},async lock=>{
  if(!lock){reject(Error('«Близко» уже открыто в другой вкладке. Закройте её и нажмите «Попробовать снова».'));return;}
  resolve();await new Promise(r=>releaseLock=r);
 }).catch(reject));
 await chat.init();if(chat.state){render();chat.start();}
 $('create-form').querySelector('[type=submit]').disabled=false;$('restore-open').disabled=false;$('boot-status').hidden=true;
 setupWorker().catch(()=>{});
}
window.addEventListener('online',()=>chat.reconnect());
document.addEventListener('visibilitychange',()=>{if(document.visibilityState==='visible'&&chat.state){chat.reconnect();if(current)chat.markRead(current).catch(fail);}});
window.addEventListener('pagehide',()=>{chat.stop();chat.chain.finally(()=>releaseLock?.());});
window.addEventListener('pageshow',e=>{if(e.persisted)location.reload();});
boot().catch(fatal);
