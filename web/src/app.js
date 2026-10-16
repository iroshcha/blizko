import QRCode from 'qrcode';
import jsQR from 'jsqr';
import {Chat} from './client.js';
import {contactCode,parseContact} from './protocol.js';
import {exportBackup,importBackup} from './storage.js';
import {createUIStore,connectionView,composerView} from './ui-state.js';
import {ConversationView,element,setText,reconcile} from './conversation-view.js';

const $=id=>document.getElementById(id);
const chat=new Chat(),ui=createUIStore(),contactNodes=new Map();
let toastTimer,backupMode='export',registration,releaseLock,markingRead=false;
function toast(s){setText($('toast'),s);$('toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').hidden=true,6500);}
function errorText(e){return e?.name==='QuotaExceededError'?'Недостаточно места для сохранения. Освободите место на устройстве.':e?.message||'Действие не выполнено. Попробуйте снова.';}
function fail(e){toast(errorText(e));}
function clearToast(){clearTimeout(toastTimer);$('toast').hidden=true;}
function fatal(e){$('welcome').hidden=true;$('app').hidden=true;$('fatal').hidden=false;$('fatal-text').textContent=e.message;}
function dialog(id){clearToast();$(id).showModal();}
function action(id,fn){$(id).addEventListener('click',async()=>{const b=$(id);b.disabled=true;try{await fn();}catch(e){fail(e);}finally{b.disabled=false;render();}});}
function form(id,fn){$(id).addEventListener('submit',async e=>{e.preventDefault();const button=$(id).querySelector('[type=submit]');button.disabled=true;try{await fn();}catch(error){fail(error);}finally{button.disabled=false;}});}
function initial(name){return [...name][0]?.toLocaleUpperCase('ru')||'·';}
const conversation=new ConversationView($('messages'),$('new-messages'),{
 retry:(peer,id)=>chat.retryMessage(peer,id).catch(fail),cancel:(peer,id)=>chat.cancel(peer,id).catch(fail),onBottom:markVisibleRead
});
async function markVisibleRead() {
 const peer=ui.state.peer;
 if(markingRead||!peer||!chat.state||document.visibilityState!=='visible'||document.querySelector('dialog[open]')||!conversation.atBottom())return;
 if(!chat.state.messages.some(m=>m.peer===peer&&!m.out&&!m.archived&&m.time>(chat.state.read[peer]||0)))return;
 markingRead=true;
 try{await chat.markRead(peer);}catch(e){fail(e);}finally{markingRead=false;}
}
function renderComposer() {
 const input=$('message'),view=composerView(input.value,ui.state.sending,ui.state.sendError);
 // Hidden textareas have no measurable scrollHeight; resize after opening a chat.
 if(ui.state.peer){input.style.height='auto';input.style.height=Math.min(160,input.scrollHeight)+'px';input.style.overflowY=input.scrollHeight>160?'auto':'hidden';}
 $('send').disabled=view.disabled;setText($('send'),ui.state.sending?'…':'↑');
 $('send').setAttribute('aria-label',ui.state.sending?'Сохраняем сообщение':'Отправить');
 $('composer-error').hidden=!view.error;setText($('composer-error'),view.error);input.setAttribute('aria-invalid',String(!!view.error));
 const shortcut=matchMedia('(pointer:coarse)').matches?'↵ — новая строка':'Ctrl/⌘ + Enter — отправить';
 setText($('composer-hint'),ui.state.sending?'Сохраняем сообщение…':chat.online?shortcut:'Можно писать без связи — сообщение останется в очереди');
}
function render() {
 const s=chat.state;if(!s)return;
 const current=ui.state.peer,connection=connectionView(chat.connection,navigator.onLine);
 $('welcome').hidden=true;$('app').hidden=false;$('app').classList.toggle('in-chat',!!current);
 setText($('status'),connection.text);setText($('chat-connection'),connection.text);
 $('status-dot').classList.toggle('online',connection.kind==='online');$('reconnect').hidden=connection.kind!=='offline';
 setText($('connection-hint'),connection.hint);setText($('contact-count'),String(s.contacts.length));
 const search=ui.state.search.trim().toLocaleLowerCase('ru'),summaries=new Map();
 for(const m of s.messages)if(!m.archived){const summary=summaries.get(m.peer)||{last:null,unread:0};summary.last=m;if(!m.out&&m.time>(s.read[m.peer]||0))summary.unread++;summaries.set(m.peer,summary);}
 const list=$('contacts'),ordered=[];
 for(const c of [...s.contacts].sort((a,b)=>(summaries.get(b.id)?.last?.time||0)-(summaries.get(a.id)?.last?.time||0))) {
  let button=contactNodes.get(c.id);
  if(!button){
   button=element('button','contact');button.type='button';button.append(element('div','avatar'));
   const info=element('div','contact-info');info.append(element('div','contact-name'),element('div','preview'));button.append(info,element('span','unread'));
   button.onclick=()=>openChat(c.id).catch(fail);contactNodes.set(c.id,button);
  }
  const {last,unread}=summaries.get(c.id)||{unread:0};
  button.classList.toggle('selected',current===c.id);button.setAttribute('aria-label',`Открыть чат: ${c.name}`);
  if(current===c.id)button.setAttribute('aria-current','true');else button.removeAttribute('aria-current');
  button.hidden=!c.name.toLocaleLowerCase('ru').includes(search);
  setText(button.children[0],initial(c.name));setText(button.children[1].children[0],c.name);
  setText(button.children[1].children[1],last?(last.out?'Вы: ':'')+last.text:'Обменяйтесь QR и поздоровайтесь');
  setText(button.children[2],String(unread));button.children[2].hidden=!unread;ordered.push(button);
 }
 reconcile(list,ordered);$('search-empty').hidden=!s.contacts.length||ordered.some(node=>!node.hidden);
 $('empty-contacts').hidden=s.contacts.length>0;list.hidden=!s.contacts.length;
 $('no-chat').hidden=!!current;$('chat').hidden=!current;renderAdd();renderComposer();
 if(!current)return;
 const c=s.contacts.find(c=>c.id===current);if(!c)return;
 setText($('chat-name'),c.name);setText($('chat-avatar'),initial(c.name));
 const hasPending=s.messages.some(m=>m.peer===current&&m.out&&!m.delivered&&!m.cancelled);
 const issue=hasPending?chat.issues.get(current):'';$('delivery-issue').hidden=!issue;setText($('delivery-issue'),issue?issue+'. Сообщение сохранено в очереди.':'');
 const probe=ui.state.probe;$('probe').disabled=!chat.online||probe?.status==='checking';
 $('probe-result').hidden=!probe;setText($('probe-result'),probe?(probe.status==='checking'?'':'Последняя проверка: ')+probe.text:'');
 $('probe-result').dataset.status=probe?.status||'';
 conversation.render(current,c.name,s.messages.filter(m=>m.peer===current&&!m.archived),chat);
}
async function openChat(id) {
 $('message').value=chat.state.drafts[id]||'';ui.dispatch({type:'select',peer:id});conversation.toBottom();await markVisibleRead();
}
ui.subscribe(render);
chat.addEventListener('change',()=>{render();markVisibleRead();});
$('search').addEventListener('input',()=>ui.dispatch({type:'search',value:$('search').value}));
$('message').addEventListener('input',()=>{
 ui.dispatch({type:'send-error',error:''});
 const peer=ui.state.peer;if(peer)chat.draft(peer,$('message').value).catch(e=>{if(peer===ui.state.peer)ui.dispatch({type:'send-error',error:'Не удалось сохранить черновик. '+errorText(e)});fail(e);});
});
$('message').addEventListener('keydown',e=>{if(e.key==='Enter'&&(e.ctrlKey||e.metaKey)&&!e.isComposing){e.preventDefault();$('send-form').requestSubmit();}});
form('create-form',async()=>{await chat.create($('my-name').value);clearToast();chat.start();await navigator.storage?.persist?.();});
$('send-form').addEventListener('submit',async e=>{
 e.preventDefault();const peer=ui.state.peer,value=$('message').value;if(!peer||composerView(value,ui.state.sending).disabled)return;
 ui.dispatch({type:'send-start'});let error='';
 try{await chat.send(peer,value);clearToast();if(ui.state.peer===peer&&$('message').value===value)$('message').value='';}
 catch(e){error='Не удалось отправить. '+errorText(e);fail(e);}
 finally{ui.dispatch({type:'send-end',peer,error});}
});
action('back',()=>ui.dispatch({type:'select',peer:''}));
action('reconnect',()=>chat.reconnect());
action('probe',async()=>{
 const {peer,selection}=ui.state;ui.dispatch({type:'probe',peer,selection,status:'checking',text:'Проверяем связь с собеседником…'});
 try{ui.dispatch({type:'probe',peer,selection,status:'success',text:await chat.probe(peer)});}
 catch(e){ui.dispatch({type:'probe',peer,selection,status:'error',text:e.message});}
});
function renderAdd() {
 const a=ui.state.add;
 for(let step=1;step<=3;step++){$('add-step-'+step).hidden=a.step!==step;$('add-progress-'+step).setAttribute('aria-current',a.step===step?'step':'false');}
 setText($('add-step-label'),`Шаг ${a.step} из 3`);
 $('contact-code').required=a.step===1;$('contact-name').required=a.step===2;
 $('contact-code').disabled=a.busy||a.step!==1;$('contact-name').disabled=a.busy||a.step!==2;
 $('add-submit').hidden=a.step===3;$('add-submit').disabled=a.busy;setText($('add-submit'),a.busy?(a.step===1?'Проверяем…':'Сохраняем…'):a.step===1?'Продолжить':'Сохранить контакт');
 $('add-previous').hidden=a.step!==2;$('add-previous').disabled=a.busy;
 $('add-error').hidden=!a.error;setText($('add-error'),a.error);
 $('add-probe').disabled=a.busy||!chat.online;setText($('add-probe'),a.busy?'Проверяем…':'Проверить связь');
 $('add-done').disabled=a.busy;setText($('add-done'),a.verified?'Начать переписку':'Открыть чат');
 setText($('add-result'),a.verified?'Связь проверена. Можно начинать переписку.':`Контакт «${a.name}» сохранён. Попросите собеседника добавить ваш QR.`);
 for(const b of $('add-dialog').querySelectorAll('[data-close],.scan-buttons button'))b.disabled=a.busy;
}
const drawQR=(canvas,code)=>QRCode.toCanvas(canvas,code,{width:640,margin:4,errorCorrectionLevel:'M',color:{dark:'#163f36',light:'#ffffff'}});
async function copyCode(code) {try{await navigator.clipboard.writeText(code);toast('Код скопирован');}catch{toast('Копирование недоступно. Покажите QR или выделите код вручную.');}}
action('add-open',()=>{$('add-form').reset();setText($('add-code-status'),'');ui.dispatch({type:'add-reset'});dialog('add-dialog');});
$('add-dialog').addEventListener('cancel',e=>{if(ui.state.add.busy)e.preventDefault();});
$('add-form').addEventListener('submit',async e=>{
 e.preventDefault();const step=ui.state.add.step;if(ui.state.add.busy||step===3)return;
 ui.dispatch({type:'add',value:{busy:true,error:''}});
 try {
  if(step===1){
   const c=await parseContact($('contact-code').value,'Контакт');
   if(c.id===chat.state.identity.id)throw Error('Это ваш QR. Попросите код собеседника.');
   const existing=chat.state.contacts.find(p=>p.id===c.id);if(existing)$('contact-name').value=existing.name;
   ui.dispatch({type:'add',value:{step:2}});
  }else{
   const name=$('contact-name').value.trim(),id=await chat.addContact(name,$('contact-code').value);
   ui.dispatch({type:'add',value:{step:3,peer:id,name}});
   const code=contactCode(chat.state.identity);$('add-own-code').value=code;await drawQR($('add-qr'),code);
  }
 }catch(e){ui.dispatch({type:'add',value:{error:errorText(e)}});}
 finally{ui.dispatch({type:'add',value:{busy:false}});if(ui.state.add.step===2)$('contact-name').focus();}
});
action('add-previous',()=>ui.dispatch({type:'add',value:{step:1,error:''}}));
action('add-copy',()=>copyCode(contactCode(chat.state.identity)));
action('add-probe',async()=>{
 ui.dispatch({type:'add',value:{busy:true,error:'',verified:false}});
 try{await chat.probe(ui.state.add.peer);ui.dispatch({type:'add',value:{verified:true}});}
 catch(e){ui.dispatch({type:'add',value:{error:e.message+'. Контакт уже сохранён — проверить связь можно позже.'}});}
 finally{ui.dispatch({type:'add',value:{busy:false}});}
});
action('add-done',async()=>{$('add-dialog').close();await openChat(ui.state.add.peer);});
action('qr-open',async()=>{
 const code=contactCode(chat.state.identity);$('qr-name').textContent=chat.state.identity.name;$('own-code').value=code;
 await drawQR($('qr-canvas'),code);dialog('qr-dialog');
});


action('copy-code',async()=>{try{await navigator.clipboard.writeText($('own-code').value);toast('Код скопирован');}catch{$('own-code').focus();$('own-code').select();toast('Выделили код: выберите «Скопировать».');}});
action('settings-open',()=>{$('profile-name').textContent=chat.state.identity.name;dialog('settings-dialog');});
action('clear-open',()=>dialog('clear-dialog'));
action('clear-confirm',async()=>{const removed=await chat.clearHistory();$('clear-dialog').close();toast(`Очищено сообщений: ${removed}. Неотправленные сообщения и черновики сохранены.`);});
for(const button of document.querySelectorAll('[data-close]'))button.addEventListener('click',()=>button.closest('dialog').close());
action('scan-camera',()=>$('camera-file').click());action('scan-photo',()=>$('photo-file').click());
for(const id of ['camera-file','photo-file'])$(id).addEventListener('change',async()=>{
 const file=$(id).files[0];if(!file)return;
 if(ui.state.add.busy)return;
 ui.dispatch({type:'add',value:{busy:true,error:''}});
 let url;
 try {
  if(file.size>20*1024*1024)throw Error('Фото слишком большое — максимум 20 МБ');
  url=URL.createObjectURL(file);const img=new Image();img.src=url;await img.decode();
  const scale=Math.min(1,1600/Math.max(img.width,img.height));const canvas=document.createElement('canvas');canvas.width=Math.round(img.width*scale);canvas.height=Math.round(img.height*scale);
  const ctx=canvas.getContext('2d',{willReadFrequently:true});ctx.drawImage(img,0,0,canvas.width,canvas.height);const pixels=ctx.getImageData(0,0,canvas.width,canvas.height);
  const result=jsQR(pixels.data,pixels.width,pixels.height,{inversionAttempts:'attemptBoth'});
  if(!result||!result.data.startsWith('blizko:3:'))throw Error('QR «Близко» не найден. Снимите его ближе или вставьте код.');
  $('contact-code').value=result.data;setText($('add-code-status'),'QR прочитан. Нажмите «Продолжить».');ui.dispatch({type:'add',value:{error:''}});
 }catch(e){ui.dispatch({type:'add',value:{error:e.message}});}finally{if(url)URL.revokeObjectURL(url);$(id).value='';ui.dispatch({type:'add',value:{busy:false}});}
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
window.addEventListener('offline',()=>chat.disconnect());
document.addEventListener('visibilitychange',()=>{if(document.visibilityState==='visible'&&chat.state){chat.reconnect();markVisibleRead();}});
function resizeViewport(){if(window.visualViewport?.scale===1)document.documentElement.style.setProperty('--app-height',window.visualViewport.height+'px');}
window.visualViewport?.addEventListener('resize',resizeViewport);resizeViewport();
window.addEventListener('pagehide',()=>{chat.stop();chat.chain.finally(()=>releaseLock?.());});
window.addEventListener('pageshow',e=>{if(e.persisted)location.reload();});
boot().catch(fatal);
