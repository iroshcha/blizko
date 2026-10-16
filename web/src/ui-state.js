// Transient interface state only. Keys, history and drafts stay in Chat/Vault.
export function createUIStore() {
 let state={peer:'',selection:0,search:'',sending:false,sendError:'',probe:null,add:{step:1,busy:false,error:'',peer:'',name:'',verified:false}};
 const listeners=new Set();
 return {
  get state(){return state;},
  subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn);},
  dispatch(action){
   switch(action.type) {
    case 'select': state={...state,peer:action.peer,selection:state.selection+1,sendError:'',probe:null};break;
    case 'search': state={...state,search:action.value};break;
    case 'send-start': state={...state,sending:true,sendError:''};break;
    case 'send-end': state={...state,sending:false,sendError:action.peer===state.peer?(action.error||''):state.sendError};break;
    case 'send-error': state={...state,sendError:action.error};break;
    case 'probe': if(action.peer!==state.peer||action.selection!==state.selection)return;state={...state,probe:action};break;
    case 'add-reset': state={...state,add:{step:1,busy:false,error:'',peer:'',name:'',verified:false}};break;
    case 'add': state={...state,add:{...state.add,...action.value}};break;
    default: throw Error('Unknown UI action');
   }
   for(const fn of listeners)fn(state);
  }
 };
}

export function connectionView(connection,hasNetwork=true) {
 if(!hasNetwork)return {kind:'offline',text:'Нет интернета',hint:'Сообщения сохранятся и отправятся, когда связь вернётся.'};
 if(connection==='online')return {kind:'online',text:'Подключено к серверу',hint:'Для доставки собеседник тоже должен открыть приложение.'};
 if(connection==='connecting')return {kind:'connecting',text:'Подключаемся…',hint:'Очередь сохранена. Устанавливаем связь с сервером.'};
 return {kind:'offline',text:'Сервер недоступен',hint:'Очередь сохранена. Подключение повторится автоматически.'};
}

export function deliveryView(message,{online,sending,issue}) {
 if(message.delivered)return {kind:'delivered',text:'Доставлено ✓✓'};
 if(message.cancelled)return {kind:'cancelled',text:'Повторы остановлены'};
 if(!online)return {kind:'queued',text:'Ждёт подключения'};
 if(sending)return {kind:'sending',text:'Отправляем…'};
 if(issue)return {kind:'retry',text:'Ожидает повторной отправки'};
 return {kind:'queued',text:'В очереди ◷'};
}

export function composerView(value,sending,error='') {
 const size=new TextEncoder().encode(value.trim()).length;
 return {disabled:sending||!size||size>4000,error:size>4000?'Сообщение слишком длинное. Сократите текст.':error};
}
