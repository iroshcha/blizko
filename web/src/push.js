import {unb64} from './protocol.js';

// Permission/subscription creation stays in the button's user gesture.
export function setupPush(chat,toast) {
 const $=id=>document.getElementById(id);
 let registration,subscription,busy=false,synced='',lastSocket,refreshing=false;
 const supported='serviceWorker' in navigator&&'PushManager' in window&&'Notification' in window;
 const standalone=matchMedia('(display-mode: standalone)').matches||navigator.standalone===true;
 const apple=/iPhone|iPad|iPod/.test(navigator.userAgent)||(navigator.platform==='MacIntel'&&navigator.maxTouchPoints>1);
 function render(note) {
  const available=supported&&(!apple||standalone);
  $('push-enable').hidden=!!subscription||!available;
  $('push-disable').hidden=!subscription;
  $('push-test').hidden=!subscription;
  $('push-enable').disabled=busy||!registration||!chat.online||!chat.serverConfig?.push;
  $('push-disable').disabled=$('push-test').disabled=busy||!chat.online;
  $('push-status').textContent=note||(!available?'На iPhone установите «Близко» через Safari → «Поделиться» → «На экран “Домой”» и откройте оттуда.':Notification.permission==='denied'?'Уведомления запрещены в настройках устройства. Разрешите их для «Близко».':subscription?(synced?'Включены. Уведомление скрывает имя и текст сообщения.':'Синхронизируем подписку с сервером…'):!chat.online?'Подключитесь к серверу, чтобы включить уведомления.':!chat.serverConfig?.push?'Сервер ещё не подготовил уведомления.':'Включите уведомления, чтобы узнавать о сообщениях при закрытом приложении.');
 }
 async function sync() {
  if(!chat.online||!chat.state||!subscription)return;
  const raw=subscription.toJSON(),contacts=chat.state.contacts.map(c=>c.address).sort();
  const signature=JSON.stringify([raw.endpoint,contacts,chat.serverConfig?.publicKey]);
  if(synced===signature&&lastSocket===chat.ws)return;
  await chat.control('push-set',{subscription:{endpoint:raw.endpoint,keys:raw.keys},contacts});
  synced=signature;lastSocket=chat.ws;
 }
 async function refresh() {
  if(refreshing||busy)return;refreshing=true;
  try {await sync();render();}catch{synced='';render('Не удалось сохранить подписку на сервере. Повторим при подключении.');}finally{refreshing=false;}
 }
 chat.addEventListener('change',refresh);
 $('push-enable').onclick=async()=>{
  if(busy||!registration||!chat.serverConfig?.push)return;busy=true;render();
  try {
   subscription=await registration.pushManager.subscribe({userVisibleOnly:true,applicationServerKey:unb64(chat.serverConfig.publicKey.replaceAll('-','+').replaceAll('_','/'))});
   await sync();toast('Уведомления включены. Кнопка проверки отправит пробное уведомление.');
  }catch(e){toast(Notification.permission==='denied'?'Разрешите уведомления в настройках устройства.':e.message||'Не удалось включить уведомления');}
  finally{busy=false;render();}
 };
 $('push-disable').onclick=async()=>{
  if(busy)return;busy=true;render();
  try {await chat.control('push-remove');if(subscription&&!await subscription.unsubscribe())throw Error('Отключите уведомления также в настройках устройства');subscription=null;synced='';toast('Уведомления выключены');}
  catch(e){toast(e.message);}finally{busy=false;render();}
 };
 $('push-test').onclick=async()=>{
  if(busy)return;busy=true;render();
  try {await sync();await chat.control('push-test');toast('Проверка запрошена. Можно свернуть приложение и дождаться уведомления.');render('Пробное уведомление ожидает отправки.');}
  catch(e){toast(e.message);}finally{busy=false;}
  setTimeout(async()=>{try{const result=JSON.parse((await chat.control('push-status')).data);render(result.status==='accepted'?'Push-служба приняла уведомление. Проверьте его на устройстве.':result.status==='retry'?'Push-служба временно недоступна. Сервер повторит попытку.':!result.registered?'Подписка истекла. Выключите и включите уведомления снова.':'Уведомление ожидает отправки.');}catch{render();}},16000);
 };
 render();
 if(supported)navigator.serviceWorker.ready.then(async r=>{registration=r;subscription=await r.pushManager.getSubscription();await refresh();}).catch(()=>render('Не удалось подготовить уведомления. Перезапустите приложение.'));
}
