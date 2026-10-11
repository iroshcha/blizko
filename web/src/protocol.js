import nacl from 'tweetnacl';

export const utf8 = s => new TextEncoder().encode(s);
export const text = b => new TextDecoder('utf-8', {fatal:true}).decode(b);
export const hex = b => Array.from(b, n => n.toString(16).padStart(2,'0')).join('');
export const unhex = s => {
 if (typeof s !== 'string' || !/^(?:[a-f0-9]{2})+$/.test(s)) throw Error('Неверный ключ');
 return Uint8Array.from(s.match(/../g), n => parseInt(n,16));
};
export const b64 = b => btoa(String.fromCharCode(...b));
export const unb64 = s => {
 if (typeof s !== 'string' || s.length > 30000 || !/^[A-Za-z0-9+/]*={0,2}$/.test(s)) throw Error('Неверный пакет');
 return Uint8Array.from(atob(s), c => c.charCodeAt(0));
};
export const randomID = () => hex(nacl.randomBytes(16));
export const hash = async b => hex(new Uint8Array(await crypto.subtle.digest('SHA-256',b)));
const isHex = (s,n) => typeof s === 'string' && s.length === n*2 && /^[a-f0-9]+$/.test(s);
// Match Go encoding/json's HTML-safe canonical string representation exactly.
export const goJSON = value => JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, c => '\\u'+c.charCodeAt(0).toString(16).padStart(4,'0'));

export async function newIdentity(name) {
 const box = nacl.box.keyPair(), seed = nacl.randomBytes(32);
 const signing = nacl.sign.keyPair.fromSeed(seed);
 return {name, secret:b64(box.secretKey), public:b64(box.publicKey), seed:b64(seed), id:await hash(box.publicKey), address:hex(signing.publicKey)};
}
export async function validateIdentity(i) {
 if (!i || typeof i.name !== 'string' || i.name.length > 60) throw Error('Повреждён профиль');
 const sk = unb64(i.secret), seed=unb64(i.seed);
 if (sk.length !== 32 || seed.length !== 32) throw Error('Повреждены ключи');
 const pub = nacl.box.keyPair.fromSecretKey(sk).publicKey;
 if (b64(pub) !== i.public || await hash(pub) !== i.id || hex(nacl.sign.keyPair.fromSeed(seed).publicKey) !== i.address) throw Error('Повреждены ключи');
 return i;
}
export function contactCode(i) {
 const raw=utf8(goJSON({id:i.id,name:i.name,key:Array.from(unb64(i.public)),address:i.address}));
 return 'blizko:3:'+b64(raw).replaceAll('+','-').replaceAll('/','_').replace(/=+$/,'');
}
export async function parseContact(code, name) {
 code=code.trim(); name=name.trim();
 if (!name || [...name].length>60) throw Error('Введите имя до 60 символов');
 if(code.startsWith('blizko:2:')) throw Error('Это старый QR. Обновите приложение собеседника.');
 if(code.length>4096 || !/^blizko:3:[A-Za-z0-9_-]+$/.test(code)) throw Error('Неверный код контакта');
 const c=JSON.parse(text(unb64(code.slice(9).replaceAll('-','+').replaceAll('_','/'))));
 if(!Array.isArray(c.key) || c.key.length!==32 || !c.key.every(n=>Number.isInteger(n)&&n>=0&&n<=255) || !isHex(c.address,32) || /^0+$/.test(c.address)) throw Error('Неверные ключи контакта');
 const key=Uint8Array.from(c.key), probe=new Uint8Array(32); probe[0]=9;
 if(!nacl.scalarMult(probe,key).some(n=>n!==0) || await hash(key)!==c.id) throw Error('Неверный ключ контакта');
 return {id:c.id,name,key:c.key,address:c.address};
}

function unsigned(f) {
 const o={type:f.type,id:f.id,from:f.from,to:f.to};
 if(f.data) o.data=f.data;
 if(f.result) { o.result={status:f.result.status}; if(f.result.code)o.result.code=f.result.code; if(f.result.data)o.result.data=f.result.data; }
 return utf8('blizko-server-v1/frame\0'+goJSON(o));
}
export function signFrame(f,i) {
 return {...f,signature:b64(nacl.sign.detached(unsigned(f),nacl.sign.keyPair.fromSeed(unb64(i.seed)).secretKey)).replace(/=+$/,'')};
}
export function verifyFrame(f) {
 try {
  if(!f || !isHex(f.id,16)||!isHex(f.from,32)||!isHex(f.to,32))return false;
  if(f.type==='request') { if(typeof f.data!=='string'||utf8(f.data).length>20000||f.result!=null)return false; }
  else if(f.type==='response') {
   if(f.data || !f.result || !Number.isInteger(f.result.status) || f.result.status<200||f.result.status>599)return false;
   for(const [key,max] of [['code',64],['data',20000]]) if(f.result[key]!==undefined && (typeof f.result[key]!=='string'||utf8(f.result[key]).length>max)) return false;
  } else return false;
  const sig=unb64(f.signature);
  if(sig.length!==64)return false;
  // Reject non-canonical S, matching Go's strict Ed25519 verifier.
  const order=unhex('edd3f55c1a631258d69cf7a2def9de1400000000000000000000000000000010');
  let smaller=false;
  for(let j=31;j>=0;j--) {if(sig[32+j]!==order[j]) {smaller=sig[32+j]<order[j];break;}}
  return smaller && nacl.sign.detached.verify(unsigned(f),sig,unhex(f.from));
 } catch { return false; }
}
export function login(challenge,i) {
 if(!isHex(challenge,32))throw Error('Неверный ответ сервера');
 const raw=utf8('blizko-server-v1/login\0'+challenge+'\0'+i.address);
 return {type:'auth',address:i.address,signature:b64(nacl.sign.detached(raw,nacl.sign.keyPair.fromSeed(unb64(i.seed)).secretKey)).replace(/=+$/,'')};
}
export function seal(p,peer,i) {
 const nonce=nacl.randomBytes(24);
 const body=nacl.box(utf8(goJSON(p)),nonce,Uint8Array.from(peer.key),unb64(i.secret));
 if(body.length>12000)throw Error('Слишком длинное сообщение');
 const env={v:2,from:i.id,nonce:b64(nonce),body:b64(body)};
 if(utf8(goJSON(env)).length>20000)throw Error('Слишком длинное сообщение');
 return env;
}
export function open(env,peer,i) {
 if(!env || env.v!==2||env.from!==peer.id)throw Error('Неверный отправитель');
 const nonce=unb64(env.nonce), body=unb64(env.body);
 if(nonce.length!==24||body.length>12000)throw Error('Неверный пакет');
 const raw=nacl.box.open(body,nonce,Uint8Array.from(peer.key),unb64(i.secret));
 if(!raw)throw Error('Подпись сообщения не совпала');
 const p=JSON.parse(text(raw));
 if(p.v!==2||p.from!==peer.id||p.to!==i.id||!isHex(p.id,16))throw Error('Неверный пакет');
 if(p.kind==='text') { if(typeof p.text!=='string'||!utf8(p.text).length||utf8(p.text).length>4000)throw Error('Неверный текст'); }
 else if(p.kind==='ack') {if(!isHex(p.ack,16))throw Error('Неверное подтверждение');}
 else throw Error('Неизвестный пакет');
 return p;
}
