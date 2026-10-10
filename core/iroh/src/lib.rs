//! A bounded request/response bridge. Chat storage and message authentication stay in Go.
use iroh::{Endpoint, EndpointAddr, EndpointId, RelayUrl, SecretKey, endpoint::presets};
use n0_watcher::Watcher;
use serde_json::{Value, json};
use std::{collections::{HashMap, HashSet}, ffi::{CStr, CString, c_char}, sync::{Arc, Mutex, RwLock, OnceLock, atomic::{AtomicU64, Ordering}}, time::Duration};
use tokio::{runtime::Runtime, sync::{mpsc, oneshot, Semaphore}};

#[cfg(all(test, windows))]
mod windows_test;
#[cfg(test)]
mod local_test;

const ALPN: &[u8] = b"blizko/chat/3";
const HOME_RELAY: &str = "https://ample-raven-6363.ru.tuna.am/";
const MAX_PACKET: usize = 20_000;
type Reply = oneshot::Sender<String>;
struct Node {
    endpoint: Endpoint,
    relay: RelayUrl,
    incoming: tokio::sync::Mutex<mpsc::Receiver<Value>>,
    replies: Mutex<HashMap<u64, Reply>>,
    allowed: RwLock<HashMap<String,Arc<Semaphore>>>,
    connections: tokio::sync::Mutex<HashMap<String,Arc<tokio::sync::Mutex<Option<iroh::endpoint::Connection>>>>>,
    requests: Mutex<Requests>,
}
#[derive(Default)]
struct Requests { active:HashMap<u64,oneshot::Sender<()>>, cancelled:HashSet<u64> }
struct IncompleteExchange { connection:iroh::endpoint::Connection, complete:bool }
impl Drop for IncompleteExchange {
    fn drop(&mut self) { if !self.complete { self.connection.close(0u32.into(),b"request cancelled"); } }
}
fn update_allowed(node:&Node, value:&Value)->Result<(),String>{
    let list=value.as_array().ok_or("invalid_contacts")?;
    if list.len()>100{return Err("too_many_contacts".into())}
    let mut next=HashMap::new();let mut allowed=node.allowed.write().unwrap();
    for item in list {let peer=item.as_str().ok_or("invalid_contacts")?;let _:EndpointId=peer.parse().map_err(err)?;
        next.insert(peer.to_owned(),allowed.get(peer).cloned().unwrap_or_else(||Arc::new(Semaphore::new(2))));}
    *allowed=next;Ok(())
}
static RUNTIME: OnceLock<Runtime> = OnceLock::new();
static NODES: OnceLock<Mutex<HashMap<u64, Arc<Node>>>> = OnceLock::new();
static SEQUENCE: AtomicU64 = AtomicU64::new(1);
// The Java VM and global Application reference remain valid for the process lifetime.
#[cfg(target_os = "android")]
#[unsafe(no_mangle)]
pub unsafe extern "C" fn blizko_iroh_android_context(vm: *mut std::ffi::c_void, context: *mut std::ffi::c_void) {
    static INIT: std::sync::Once = std::sync::Once::new();
    INIT.call_once(|| unsafe { ndk_context::initialize_android_context(vm, context); });
}
fn nodes() -> &'static Mutex<HashMap<u64, Arc<Node>>> { NODES.get_or_init(Default::default) }
fn runtime() -> &'static Runtime { RUNTIME.get_or_init(|| {
    #[cfg(windows)]
    if std::env::var_os("BLIZKO_DEBUG_NETWORK").is_some() {
        let _ = tracing_subscriber::fmt().with_max_level(tracing_subscriber::filter::LevelFilter::DEBUG)
            .with_ansi(false).with_writer(std::io::stderr).try_init();
    }
    tokio::runtime::Builder::new_multi_thread().worker_threads(2).enable_all().build().expect("runtime")
}) }
fn text(v: &Value, key: &str) -> Result<String, String> { v[key].as_str().map(str::to_owned).ok_or_else(|| "invalid_request".into()) }
fn err<E>(_: E) -> String { "connection_failed".into() }
fn connect_error<E: std::fmt::Display>(error: E) -> String {
    // Opt-in diagnostics contain connection errors only, never chat packets.
    if std::env::var_os("BLIZKO_DEBUG_NETWORK").is_some() {
        eprintln!("iroh connection: {error}");
    }
    "connection_failed".into()
}

fn configured_relay(raw: &str) -> Result<iroh::RelayUrl, String> {
    let url: iroh::RelayUrl = raw.parse().map_err(|_| "invalid_relay_url".to_owned())?;
    let local = matches!(url.host_str(), Some("127.0.0.1") | Some("[::1]"));
    if (url.scheme() != "https" && !(url.scheme() == "http" && local))
        || url.host_str().is_none() || !url.username().is_empty() || url.password().is_some()
        || url.query().is_some() || url.fragment().is_some() || url.path() != "/" {
        return Err("invalid_relay_url".into());
    }
    Ok(url)
}

fn selected_relay() -> Result<RelayUrl, String> {
    // Desktop overrides are retained for local transport tests and explicit diagnostics.
    #[cfg(windows)]
    if let Ok(raw) = std::env::var("BLIZKO_RELAY_URL") {
        return configured_relay(&raw);
    }
    configured_relay(HOME_RELAY)
}

async fn serve(node:Arc<Node>,tx:mpsc::Sender<Value>) {
    let handshakes=Arc::new(Semaphore::new(32));
    let streams=Arc::new(Semaphore::new(16));
    while let Some(incoming)=node.endpoint.accept().await {
        let Ok(handshake)=handshakes.clone().try_acquire_owned() else{incoming.refuse();continue};
        let node=node.clone();let tx=tx.clone();let streams=streams.clone();
        tokio::spawn(async move {
            let conn=match tokio::time::timeout(Duration::from_secs(2),incoming).await {Ok(Ok(c))=>c,_=>return};
            let peer=conn.remote_id().to_string();
            let permission=node.allowed.read().unwrap().get(&peer).cloned();
            let Some(permission)=permission else{conn.close(0u32.into(),b"unknown contact");return};
            let Ok(_contact_slot)=permission.try_acquire_owned() else{conn.close(0u32.into(),b"contact busy");return};
            drop(handshake);
            loop {
                let (mut send,mut recv)=match tokio::time::timeout(Duration::from_secs(60),conn.accept_bi()).await{Ok(Ok(pair))=>pair,_=>break};
                if !node.allowed.read().unwrap().contains_key(&peer){break}
                let Ok(_stream)=streams.clone().try_acquire_owned() else{break};
                let request_id=SEQUENCE.fetch_add(1,Ordering::Relaxed);
                let request=async {
                    let data=tokio::time::timeout(Duration::from_secs(5),recv.read_to_end(MAX_PACKET)).await.map_err(err)?.map_err(err)?;
                    let data=String::from_utf8(data).map_err(err)?;
                    let (reply_tx,reply_rx)=oneshot::channel();
                    node.replies.lock().unwrap().insert(request_id,reply_tx);
                    tx.try_send(json!({"request":request_id,"peer":peer,"data":data})).map_err(err)?;
                    let response=reply_rx.await.map_err(err)?;
                    send.write_all(response.as_bytes()).await.map_err(err)?;
                    send.finish().map_err(err)?;
                    Ok::<_,String>(())
                };
                let succeeded=tokio::select! {
                    result=tokio::time::timeout(Duration::from_secs(20),request)=>matches!(result,Ok(Ok(()))),
                    _=conn.closed()=>false
                };
                node.replies.lock().unwrap().remove(&request_id);
                if !succeeded{break}
            }
            conn.close(0u32.into(),b"idle or closed");
        });
    }
}

async fn dispatch(v: Value) -> Result<Value, String> {
    let op = text(&v, "op")?;
    if op == "start" {
        let key: [u8;32] = hex::decode(text(&v, "key")?).map_err(err)?.try_into().map_err(err)?;
        let relay = selected_relay()?;
        // All app versions know the same home relay, so peer IDs need no public lookup.
        // IP transports stay enabled: iroh can upgrade to a direct connection via discovery.
        let mut builder = Endpoint::builder(presets::Minimal).secret_key(SecretKey::from_bytes(&key))
            .alpns(vec![ALPN.to_vec()])
            .relay_mode(iroh::RelayMode::Custom(iroh::RelayMap::from_iter([relay.clone()])));
        #[cfg(all(test, windows))]
        if let Ok(path) = std::env::var("BLIZKO_TEST_LOCAL_CERT") {
            let certificate = std::fs::read(path).unwrap();
            builder = builder.ca_tls_config(iroh::tls::CaTlsConfig::embedded().with_extra_roots([certificate.into()]));
        }
        if v["relayOnly"].as_bool() == Some(true) { builder = builder.clear_ip_transports(); }
        let endpoint = builder.bind().await.map_err(err)?;
        let address = endpoint.id().to_string();
        let (tx, rx) = mpsc::channel(8);
        let node = Arc::new(Node { endpoint, relay, incoming: tokio::sync::Mutex::new(rx), replies: Mutex::new(HashMap::new()),allowed:RwLock::new(HashMap::new()),connections:tokio::sync::Mutex::new(HashMap::new()),requests:Mutex::new(Requests::default()) });
        if let Err(error)=update_allowed(&node,&v["peers"]){node.endpoint.close().await;return Err(error)};
        let handle = SEQUENCE.fetch_add(1, Ordering::Relaxed);
        nodes().lock().unwrap().insert(handle, node.clone());
        tokio::spawn(serve(node, tx));
        return Ok(json!({"handle":handle,"address":address}));
    }
    let handle = v["handle"].as_u64().ok_or("invalid_handle")?;
    let node = nodes().lock().unwrap().get(&handle).cloned().ok_or("closed")?;
    match op.as_str() {
        "close" => {
            nodes().lock().unwrap().remove(&handle);
            node.replies.lock().unwrap().clear();
            for (_,cancel) in node.requests.lock().unwrap().active.drain(){let _=cancel.send(());}
            node.connections.lock().await.clear();
            node.endpoint.close().await;
            Ok(json!({}))
        }
        "allow"=>{
            update_allowed(&node,&v["peers"])?;
            let peers:HashSet<String>=node.allowed.read().unwrap().keys().cloned().collect();
            node.connections.lock().await.retain(|peer,_|peers.contains(peer));
            Ok(json!({}))
        }
        "cancel"=>{
            let id=v["request"].as_u64().ok_or("invalid_request")?;
            let mut requests=node.requests.lock().unwrap();
            if let Some(cancel)=requests.active.remove(&id){let _=cancel.send(());}else{
                if requests.cancelled.len()>=1024 {
                    if let Some(oldest)=requests.cancelled.iter().min().copied(){requests.cancelled.remove(&oldest);}
                }
                requests.cancelled.insert(id);
            }
            Ok(json!({}))
        }
        "status" => {
            let statuses = node.endpoint.home_relay_status().get();
            let online = statuses.iter().any(|s| s.is_connected());
            let relay = node.endpoint.addr().relay_urls().next().map(ToString::to_string).unwrap_or_default();
            Ok(json!({"online":online,"relay":relay}))
        }
        "network" => { node.endpoint.network_change().await; Ok(json!({})) }
        "next" => {
            let mut receiver = node.incoming.lock().await;
            Ok(tokio::time::timeout(Duration::from_secs(1), receiver.recv()).await.ok().flatten().unwrap_or(json!({})))
        }
        "reply" => {
            let data = text(&v, "data")?;
            if data.len() > MAX_PACKET { return Err("too_large".into()); }
            let id = v["request"].as_u64().ok_or("invalid_request")?;
            if let Some(reply) = node.replies.lock().unwrap().remove(&id) { let _ = reply.send(data); }
            Ok(json!({}))
        }
        "exchange" => {
            let peer:EndpointId=text(&v,"peer")?.parse().map_err(err)?;
            let peer_text=peer.to_string();
            if !node.allowed.read().unwrap().contains_key(&peer_text){return Err("unknown_contact".into())}
            let data=text(&v,"data")?;if data.len()>MAX_PACKET{return Err("too_large".into())}
            let id=v["request"].as_u64().unwrap_or_else(||SEQUENCE.fetch_add(1,Ordering::Relaxed));
            let (cancel_tx,cancel_rx)=oneshot::channel();
            {let mut requests=node.requests.lock().unwrap();if requests.cancelled.remove(&id){return Err("cancelled".into())}requests.active.insert(id,cancel_tx);}
            let operation=async {
                let slot={let mut cache=node.connections.lock().await;
                    cache.entry(peer_text).or_insert_with(||Arc::new(tokio::sync::Mutex::new(None))).clone()};
                let mut cached=slot.lock().await;
                if cached.as_ref().is_none_or(|c|c.close_reason().is_some()){
                    let address=EndpointAddr::new(peer).with_relay_url(node.relay.clone());
                    *cached=Some(node.endpoint.connect(address,ALPN).await.map_err(connect_error)?);
                }
                let conn=cached.as_ref().unwrap().clone();
                let mut guard=IncompleteExchange{connection:conn.clone(),complete:false};
                let result=async {
                    let (mut send,mut recv)=conn.open_bi().await.map_err(err)?;
                    send.write_all(data.as_bytes()).await.map_err(err)?;send.finish().map_err(err)?;
                    let response=recv.read_to_end(MAX_PACKET).await.map_err(err)?;
                    let data=String::from_utf8(response).map_err(err)?;Ok(json!({"data":data}))
                }.await;
                guard.complete=result.is_ok();
                if result.is_err(){conn.close(0u32.into(),b"request failed");*cached=None;}
                result
            };
            let result=tokio::select!{
                _=cancel_rx=>Err("cancelled".to_owned()),
                answer=tokio::time::timeout(Duration::from_secs(15),operation)=>match answer {Ok(answer)=>answer,Err(_)=>Err("timeout".to_owned())}
            };
            node.requests.lock().unwrap().active.remove(&id);
            result
        }
        _ => Err("invalid_operation".into())
    }
}

/// The caller owns the input; every returned string must be freed once.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn blizko_iroh_call(input: *const c_char) -> *mut c_char {
    let result = std::panic::catch_unwind(|| {
        if input.is_null() { return json!({"error":"invalid_request"}); }
        let raw = unsafe { CStr::from_ptr(input) }.to_bytes();
        if raw.len() > 100_000 { return json!({"error":"too_large"}); }
        let request = match serde_json::from_slice(raw) { Ok(v) => v, Err(_) => return json!({"error":"invalid_request"}) };
        match runtime().block_on(dispatch(request)) { Ok(v) => v, Err(e) => json!({"error":e}) }
    }).unwrap_or_else(|_| json!({"error":"native_failure"}));
    CString::new(result.to_string()).expect("JSON has no NUL").into_raw()
}
#[unsafe(no_mangle)]
pub unsafe extern "C" fn blizko_iroh_free(value: *mut c_char) {
    if !value.is_null() { drop(unsafe { CString::from_raw(value) }); }
}

