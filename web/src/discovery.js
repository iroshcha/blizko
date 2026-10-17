import nacl from 'tweetnacl';
const encoder=new TextEncoder();
export const DIRECTORY_URL='https://raw.githubusercontent.com/iroshcha/blizko/relay-address/relay.json';
export const PUBLIC_KEY='a39185062bec2cd0d01d26cdc319d3d3e7f0bb7bca117b1da1b68246936037e2';
export const FALLBACK_URL='https://ample-raven-6363.a.tuna.free/';
const decode=value=>Uint8Array.from(atob(value),c=>c.charCodeAt(0));
const publicKey=()=>Uint8Array.from(PUBLIC_KEY.match(/../g),v=>parseInt(v,16));
export function verifyDirectory(raw,key=publicKey(),now=Date.now()) {
 if(typeof raw!=='string'||raw.length>4096)throw Error('Invalid directory');
 const envelope=JSON.parse(raw),payload=decode(envelope.payload),signature=decode(envelope.signature);
 const prefix=encoder.encode('blizko-relay-directory-v1\0'),signed=new Uint8Array(prefix.length+payload.length);signed.set(prefix);signed.set(payload,prefix.length);
 if(!nacl.sign.detached.verify(signed,signature,key))throw Error('Invalid directory signature');
 const r=JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(payload)),u=new URL(r.url);
 if(r.v!==1||u.protocol!=='https:'||u.username||u.password||u.port||u.pathname!=='/'||u.search||u.hash||u.href!==r.url||!u.hostname.includes('.')||/^[\d.]+$/.test(u.hostname)||u.hostname.endsWith('.local')||u.hostname.endsWith('.localhost')||!Number.isSafeInteger(r.revision)||!Number.isSafeInteger(r.expires)||r.revision<=0||r.revision>now+600000||r.expires<=now||r.expires<=r.revision||r.expires-r.revision>8*86400000)throw Error('Invalid or expired directory record');
 return r;
}
export class RelayDirectory {
 constructor({fetcher=fetch,storage=localStorage,onURL=()=>{},key=publicKey(),url=DIRECTORY_URL}={}) {
  this.fetcher=fetcher;this.storage=storage;this.onURL=onURL;this.key=key;this.url=url;this.record=null;this.busy=false;
  try{this.record=verifyDirectory(storage.getItem('blizko-relay-directory-v1'),key);onURL(this.record.url);}catch{}
 }
 async refresh() {
  if(this.busy)return;this.busy=true;
  try{
   const response=await this.fetcher(this.url+'?t='+Math.floor(Date.now()/60000),{cache:'no-store',credentials:'omit',redirect:'error',signal:AbortSignal.timeout(7000)});
   if(!response.ok)throw Error('Directory unavailable');
   // Bound decoded response bytes, including when Content-Length is absent.
   const reader=response.body.getReader();let raw='',size=0;const decoder=new TextDecoder();
   try{for(;;){const {value,done}=await reader.read();if(done)break;size+=value.length;if(size>4096)throw Error('Directory too large');raw+=decoder.decode(value,{stream:true});}raw+=decoder.decode();}finally{await reader.cancel();}
   const next=verifyDirectory(raw,this.key);
   if(this.record&&(next.revision<this.record.revision||(next.revision===this.record.revision&&next.url!==this.record.url)))throw Error('Directory rollback');
   try{this.storage.setItem('blizko-relay-directory-v1',raw);}catch{}
   this.record=next;this.onURL(next.url);
  }catch{/* Keep the last verified endpoint and encrypted outbox during outages. */}finally{this.busy=false;}
 }
 start(){this.refresh();this.timer=setInterval(()=>this.refresh(),60000);}
 stop(){clearInterval(this.timer);}
}
