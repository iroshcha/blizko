/* Shell only: chat traffic, keys, QR and history are never put in this cache. */
const CACHE='blizko-web-pages-977a8ba8f4919e69';
const FILES=['./','./index.html','./app.js','./style.css','./manifest.webmanifest','./icon.svg','./icon-192.png','./icon-512.png'];
self.addEventListener('install',event=>event.waitUntil(caches.open(CACHE).then(cache=>cache.addAll(FILES))));
self.addEventListener('activate',event=>event.waitUntil(caches.keys().then(keys=>Promise.all(keys.filter(k=>k.startsWith('blizko-web-')&&k!==CACHE).map(k=>caches.delete(k))))));
self.addEventListener('message',event=>{if(event.data==='activate')self.skipWaiting();});
self.addEventListener('push',event=>{
 // No chat keys, sender names or message contents are available to the worker.
 event.waitUntil(self.registration.showNotification('Близко',{body:'Новое сообщение. Откройте приложение, чтобы прочитать.',icon:'./icon-192.png',badge:'./icon-192.png',tag:'blizko-message'}));
});
self.addEventListener('notificationclick',event=>{
 event.notification.close();
 event.waitUntil((async()=>{
  const target=new URL('./',self.registration.scope);
  const windows=await self.clients.matchAll({type:'window',includeUncontrolled:true});
  for(const client of windows){const url=new URL(client.url);if(url.origin===target.origin&&url.pathname.startsWith(target.pathname))return client.focus();}
  return self.clients.openWindow(target.href);
 })());
});
self.addEventListener('fetch',event=>{
 const url=new URL(event.request.url);
 if(event.request.method!=='GET'||url.origin!==self.location.origin||url.search)return;
 const allowed=new Set(FILES.map(p=>new URL(p,self.registration.scope).pathname));
 if(!allowed.has(url.pathname))return;
 event.respondWith(caches.open(CACHE).then(async cache=>(await cache.match(event.request))||fetch(event.request)));
});
