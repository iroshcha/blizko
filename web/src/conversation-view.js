import {deliveryView} from './ui-state.js';

export function element(tag,cls,value) {
 const e=document.createElement(tag);if(cls)e.className=cls;if(value!==undefined)e.textContent=value;return e;
}
export function setText(node,value){if(node.textContent!==value)node.textContent=value;}
// Keep existing nodes in place, including their selection, focus and scroll state.
export function reconcile(parent,nodes) {
 const wanted=new Set(nodes);
 for(const child of [...parent.children])if(!wanted.has(child))child.remove();
 let cursor=parent.firstChild;
 for(const node of nodes) {
  if(node===cursor)cursor=cursor.nextSibling;
  else parent.insertBefore(node,cursor);
 }
}
const time=t=>new Date(t).toLocaleTimeString('ru',{hour:'2-digit',minute:'2-digit'});
const day=t=>new Date(t).toLocaleDateString('ru',{day:'numeric',month:'long',year:'numeric'});

export class ConversationView {
 constructor(box,jump,{retry,cancel,onBottom}) {
  this.box=box;this.jump=jump;this.retry=retry;this.cancel=cancel;this.onBottom=onBottom;
  this.peer='';this.nodes=new Map();this.unseen=0;
  box.addEventListener('scroll',()=>{if(this.atBottom()){this.unseen=0;this.updateJump();onBottom();}},{passive:true});
  jump.addEventListener('click',()=>{this.toBottom();onBottom();});
 }
 atBottom(){return this.box.scrollHeight-this.box.scrollTop-this.box.clientHeight<64;}
 updateJump(){this.jump.hidden=!this.unseen;setText(this.jump,`Новые сообщения · ${this.unseen} ↓`);}
 toBottom(){this.box.scrollTop=this.box.scrollHeight;this.unseen=0;this.updateJump();}
 render(peer,name,messages,chat) {
  const changed=this.peer!==peer,follow=this.atBottom(),scroll=this.box.scrollTop;
  if(changed){this.nodes.clear();this.peer=peer;this.unseen=0;}
  const next=new Map(),ordered=[];let previous='',newOutgoing=false;
  if(!messages.length) {
   const empty=this.nodes.get('empty')||element('div','empty-contacts');
   if(!empty.children.length)empty.append(element('h2'),element('p','','Обменяйтесь QR-кодами и напишите первое сообщение.'));
   setText(empty.firstChild,`Напишите ${name}`);next.set('empty',empty);ordered.push(empty);
  }
  for(const m of messages) {
   const key=`${m.out}/${m.id}`,date=day(m.time);
   if(date!==previous) {
    const k='day/'+key,node=this.nodes.get(k)||element('div','day-label',date);
    next.set(k,node);ordered.push(node);previous=date;
   }
   let node=this.nodes.get(key);
   if(!node) {
    node=element('article','bubble'+(m.out?' out':''));node.dataset.messageId=m.id;
    const body=element('div','bubble-text',m.text),meta=element('div','bubble-meta');
    node.append(body,meta);
    if(m.out) {
     const actions=element('div','bubble-actions'),retry=element('button'),cancel=element('button','','Остановить повторы');
     retry.type=cancel.type='button';
     retry.onclick=()=>this.retry(peer,m.id);cancel.onclick=()=>this.cancel(peer,m.id);
     cancel.title='Уже отправленное сообщение может быть доставлено';
     actions.append(retry,cancel);node.append(actions);
    }
    // Animate only an arriving message, never an entire restored history.
    if(!changed){
     if(m.out)newOutgoing=true;else this.unseen++;
     if(m.out||follow){node.classList.add('message-arrival');setTimeout(()=>node.classList.remove('message-arrival'),200);}
    }
   }
   // Text nodes are never rewritten on delivery, connection or draft updates.
   const status=deliveryView(m,{online:chat.online,sending:chat.activeMessages.get(peer)===m.id,issue:chat.issues.get(peer)});
   setText(node.children[1],time(m.time)+(m.out?' · '+status.text:''));
   if(m.out) {
    node.dataset.status=status.kind;
    const actions=node.children[2];actions.hidden=m.delivered;
    const [retry,cancel]=actions.children;
    setText(retry,m.cancelled?'Отправить снова':'Повторить сейчас');
    retry.disabled=status.kind==='sending';cancel.hidden=!!m.cancelled||!!m.submitted;
   }
   next.set(key,node);ordered.push(node);
  }
  reconcile(this.box,ordered);this.nodes=next;
  if(changed||follow||newOutgoing)this.toBottom();
  else {this.box.scrollTop=scroll;this.updateJump();}
 }
}
