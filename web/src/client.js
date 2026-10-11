import {newIdentity,parseContact,randomID,goJSON,signFrame,verifyFrame,login,seal,open,utf8} from './protocol.js';
import {Vault} from './storage.js';

export class Chat extends EventTarget {
 constructor(base=location.origin) {
  super(); this.base=base; this.vault=new Vault(); this.state=null; this.chain=Promise.resolve();
  this.pending=new Map();this.inbound=0;this.online=false;this.stopped=true;this.backoff=500;this.active=new Set();this.retry=new Map();this.issues=new Map();
 }
 change(){this.dispatchEvent(new Event('change'));}
 async init(){this.state=await this.vault.init();return this.state;}
 async create(name) {
  name=name.trim();if(!name||[...name].length>60)throw Error('Введите имя до 60 символов');
  if(this.state)throw Error('Профиль уже существует');
  const state={v:1,identity:await newIdentity(name),contacts:[],messages:[],drafts:{},read:{}};
  await this.vault.save(state);this.state=state;this.change();
 }
 mutate(fn) {
  const result=this.chain.then(async()=>{
   const next=structuredClone(this.state);
   const value=await fn(next);
   await this.vault.save(next);this.state=next;this.change();return value;
  });
  this.chain=result.catch(()=>{});return result;
 }
 async addContact(name,code) {
  const c=await parseContact(code,name);
  await this.mutate(s=>{
   if(c.id===s.identity.id)throw Error('Это ваш собственный контакт');
   const i=s.contacts.findIndex(p=>p.id===c.id);
   if(s.contacts.some(p=>p.address===c.address&&p.id!==c.id))throw Error('Этот адрес уже используется другим контактом');
   if(i>=0) {
    if(s.contacts[i].address!==c.address)throw Error('Адрес контакта изменился. Для замены ключей создайте новый контакт.');
    s.contacts[i]=c;
   }else {if(s.contacts.length>=100)throw Error('Достигнут лимит 100 контактов');s.contacts.push(c);}
  });
  this.retry.clear();this.flush();return c.id;
 }
 async send(peer,text) {
  text=text.trim();if(!text||utf8(text).length>4000)throw Error('Сообщение должно содержать от 1 до 4000 байт текста');
  await this.mutate(s=>{
   const contact=s.contacts.find(c=>c.id===peer);if(!contact)throw Error('Контакт не найден');
   if(s.messages.length>=2000)throw Error('Достигнут лимит истории: 2000 сообщений');
   if(s.messages.filter(m=>m.out&&!m.delivered&&!m.cancelled).length>=200)throw Error('В очереди уже 200 сообщений');
   const id=randomID(), packet=seal({v:2,id,from:s.identity.id,to:peer,kind:'text',text},contact,s.identity);
   s.messages.push({id,peer,text,out:true,delivered:false,time:Date.now(),packet});s.drafts[peer]='';
  });
  this.retry.delete(peer);this.flush();
 }
 draft(peer,value) {return this.mutate(s=>{if(utf8(value).length>16000)throw Error('Черновик слишком длинный');s.drafts[peer]=value;});}
 markRead(peer) {return this.mutate(s=>{s.read[peer]=Date.now();});}
 async cancel(peer,id) {await this.mutate(s=>{const m=s.messages.find(m=>m.peer===peer&&m.id===id&&m.out);if(m&&!m.delivered)m.cancelled=true;});}
 async retryMessage(peer,id) {await this.mutate(s=>{const m=s.messages.find(m=>m.peer===peer&&m.id===id&&m.out);if(m&&!m.delivered)m.cancelled=false;});this.retry.delete(peer);this.flush();}
 async accept(from,data) {
  const c=this.state.contacts.find(c=>c.address===from);
  if(from===this.state.identity.address&&data==='{"probe":true}')return {status:200};
  if(!c)return {status:403,code:'unknown_contact'};
  if(data==='{"probe":true}')return {status:200};
  let p;
  try {p=open(JSON.parse(data),c,this.state.identity);if(p.kind!=='text')throw Error();}
  catch{return {status:403,code:'invalid_message'};}
  try {
   await this.mutate(s=>{
    const old=s.messages.find(m=>!m.out&&m.peer===c.id&&m.id===p.id);
    if(old) {if(old.text!==p.text)throw Error('Повтор с другим содержимым');return;}
    if(s.messages.length>=2000)throw Object.assign(Error(),{full:true});
    s.messages.push({id:p.id,peer:c.id,text:p.text,out:false,delivered:true,time:Date.now()});
   });
  }catch(e){return {status:e.full?507:503,code:e.full?'history_full':'storage_failed'};}
  const ack=seal({v:2,id:randomID(),from:this.state.identity.id,to:c.id,kind:'ack',ack:p.id},c,this.state.identity);
  return {status:200,data:goJSON(ack)};
 }
 start(){if(!this.stopped)return;this.stopped=false;this.connect();this.tick=setInterval(()=>this.flush(),2000);this.heartbeat=setInterval(()=>this.checkAlive(),25000);}
 stop(){this.stopped=true;clearTimeout(this.reconnectTimer);clearInterval(this.tick);clearInterval(this.heartbeat);this.disconnect();}
 disconnect(){const ws=this.ws;this.ws=null;this.online=false;clearTimeout(this.handshake);if(ws)ws.close();for(const p of this.pending.values())p.reject(Error('Нет соединения'));this.pending.clear();this.change();}
 reconnect(){if(this.stopped)return;clearTimeout(this.reconnectTimer);this.disconnect();this.connect();}
 connect() {
  if(this.stopped)return;
  const url=new URL('/blizko/v1/socket',this.base);url.protocol=url.protocol==='https:'?'wss:':'ws:';
  const ws=new WebSocket(url);this.ws=ws;let phase='challenge';
  this.handshake=setTimeout(()=>ws.close(),10000);
  ws.onmessage=async event=>{
   if(this.ws!==ws)return;
   try {
    if(typeof event.data!=='string'||utf8(event.data).length>48000)throw Error();
    const f=JSON.parse(event.data);
    if(phase==='challenge') {if(f.type!=='challenge')throw Error();ws.send(JSON.stringify(login(f.challenge,this.state.identity)));phase='ready';return;}
    if(phase==='ready') {if(f.type!=='ready')throw Error();phase='frames';clearTimeout(this.handshake);this.online=true;this.backoff=500;this.retry.clear();this.change();this.flush();return;}
    if(f.to!==this.state.identity.address)return;
    const p=this.pending.get(f.id);
    if(f.type==='unavailable') {if(p&&p.peer===f.from)p.reject(Error('Собеседник не в сети'));return;}
    if(!verifyFrame(f))throw Error();
    if(f.type==='response') {if(p&&p.peer===f.from)p.resolve(f.result);return;}
    if(this.inbound>=8)return;
    this.inbound++;
    try {const result=await this.accept(f.from,f.data);if(this.ws===ws&&ws.readyState===WebSocket.OPEN)ws.send(JSON.stringify(signFrame({type:'response',id:f.id,from:this.state.identity.address,to:f.from,result},this.state.identity)));}
    finally{this.inbound--;}
   }catch{ws.close();}
  };
  ws.onclose=()=>{
   if(this.ws!==ws)return;this.disconnect();
   if(!this.stopped){this.reconnectTimer=setTimeout(()=>this.connect(),this.backoff+Math.random()*300);this.backoff=Math.min(15000,this.backoff*2);}
  };
  ws.onerror=()=>ws.close();
 }
 exchange(peer,data) {
  if(!this.online||!this.ws||this.pending.size>=16)return Promise.reject(Error('Соединение недоступно'));
  const id=randomID();
  return new Promise((resolve,reject)=>{
   const done=(fn,value)=>{clearTimeout(timer);this.pending.delete(id);fn(value);};
   const timer=setTimeout(()=>done(reject,Error('Собеседник не ответил')),15000);
   this.pending.set(id,{peer,resolve:v=>done(resolve,v),reject:e=>done(reject,e)});
   try{this.ws.send(JSON.stringify(signFrame({type:'request',id,from:this.state.identity.address,to:peer,data},this.state.identity)));}
   catch(e){done(reject,e);}
  });
 }
 async checkAlive(){if(!this.online)return;const ws=this.ws;try{await this.exchange(this.state.identity.address,'{"probe":true}');}catch{if(this.ws===ws)ws?.close();}}
 async probe(peer){const c=this.state.contacts.find(c=>c.id===peer);if(!c)throw Error('Контакт не найден');const r=await this.exchange(c.address,'{"probe":true}');if(r.status!==200)throw Error('У собеседника нет вашего QR-кода');return 'Собеседник в сети. Контакт подтверждён.';}
 flush() {
  if(!this.online||!this.state)return;
  for(const c of this.state.contacts) {
   if(this.active.size>=4)break;
   if(this.active.has(c.id)||Date.now()<(this.retry.get(c.id)?.next||0))continue;
   const m=this.state.messages.find(m=>m.peer===c.id&&m.out&&!m.delivered&&!m.cancelled);
   if(!m)continue;
   this.active.add(c.id);
   this.deliver(c,m).finally(()=>{this.active.delete(c.id);});
  }
 }
 async deliver(c,m) {
  try {
   const r=await this.exchange(c.address,goJSON(m.packet));
   if(r.status!==200)throw Error(({unknown_contact:'У собеседника нет вашего QR',history_full:'У собеседника заполнена история',storage_failed:'Собеседник не смог сохранить сообщение',invalid_message:'Собеседник отклонил сообщение'})[r.code]||'Отправка будет повторена');
   const ack=open(JSON.parse(r.data),c,this.state.identity);
   if(ack.kind!=='ack'||ack.ack!==m.id)throw Error('Неверное подтверждение доставки');
   await this.mutate(s=>{const found=s.messages.find(x=>x.out&&x.id===m.id&&x.peer===c.id);if(found){found.delivered=true;found.cancelled=false;delete found.packet;}});
   this.retry.delete(c.id);this.issues.delete(c.id);this.change();
  }catch(e){const delay=Math.min(30000,(this.retry.get(c.id)?.delay||1000)*2);this.retry.set(c.id,{delay,next:Date.now()+delay});this.issues.set(c.id,e.message);this.change();}
 }
}
