//! A bounded request/response bridge. Chat storage and message authentication stay in Go.
use iroh::{Endpoint, EndpointId, SecretKey, endpoint::presets};
use n0_watcher::Watcher;
use serde_json::{Value, json};
use std::{collections::HashMap, ffi::{CStr, CString, c_char}, sync::{Arc, Mutex, OnceLock, atomic::{AtomicU64, Ordering}}, time::Duration};
use tokio::{runtime::Runtime, sync::{mpsc, oneshot, Semaphore}};

const ALPN: &[u8] = b"blizko/chat/3";
const MAX_PACKET: usize = 20_000;
type Reply = oneshot::Sender<String>;
struct Node {
    endpoint: Endpoint,
    incoming: tokio::sync::Mutex<mpsc::Receiver<Value>>,
    replies: Mutex<HashMap<u64, Reply>>,
}
static RUNTIME: OnceLock<Runtime> = OnceLock::new();
static NODES: OnceLock<Mutex<HashMap<u64, Arc<Node>>>> = OnceLock::new();
static SEQUENCE: AtomicU64 = AtomicU64::new(1);
fn nodes() -> &'static Mutex<HashMap<u64, Arc<Node>>> { NODES.get_or_init(Default::default) }
fn runtime() -> &'static Runtime { RUNTIME.get_or_init(|| tokio::runtime::Builder::new_multi_thread().worker_threads(2).enable_all().build().expect("runtime")) }
fn text(v: &Value, key: &str) -> Result<String, String> { v[key].as_str().map(str::to_owned).ok_or_else(|| "invalid_request".into()) }
fn err<E: std::fmt::Display>(_: E) -> String { "connection_failed".into() }

async fn serve(node: Arc<Node>, tx: mpsc::Sender<Value>) {
    let permits = Arc::new(Semaphore::new(8));
    while let Some(incoming) = node.endpoint.accept().await {
        let Ok(permit) = permits.clone().try_acquire_owned() else { incoming.refuse(); continue; };
        let node = node.clone(); let tx = tx.clone();
        tokio::spawn(async move {
            let _permit = permit;
            let request_id = SEQUENCE.fetch_add(1, Ordering::Relaxed);
            let _ = tokio::time::timeout(Duration::from_secs(20), async {
                let conn = incoming.await.map_err(err)?;
                let (mut send, mut recv) = conn.accept_bi().await.map_err(err)?;
                let data = recv.read_to_end(MAX_PACKET).await.map_err(err)?;
                let data = String::from_utf8(data).map_err(err)?;
                let (reply_tx, reply_rx) = oneshot::channel();
                node.replies.lock().unwrap().insert(request_id, reply_tx);
                tx.try_send(json!({"request":request_id,"peer":conn.remote_id().to_string(),"data":data})).map_err(err)?;
                let response = reply_rx.await.map_err(err)?;
                send.write_all(response.as_bytes()).await.map_err(err)?;
                send.finish().map_err(err)?;
                let _ = send.stopped().await;
                conn.close(0u32.into(), b"done");
                Ok::<_, String>(())
            }).await;
            node.replies.lock().unwrap().remove(&request_id);
        });
    }
}

async fn dispatch(v: Value) -> Result<Value, String> {
    let op = text(&v, "op")?;
    if op == "start" {
        let key: [u8;32] = hex::decode(text(&v, "key")?).map_err(err)?.try_into().map_err(err)?;
        let mut builder = Endpoint::builder(presets::N0).secret_key(SecretKey::from_bytes(&key)).alpns(vec![ALPN.to_vec()]);
        if v["relayOnly"].as_bool() == Some(true) { builder = builder.clear_ip_transports(); }
        let endpoint = builder.bind().await.map_err(err)?;
        let address = endpoint.id().to_string();
        let (tx, rx) = mpsc::channel(8);
        let node = Arc::new(Node { endpoint, incoming: tokio::sync::Mutex::new(rx), replies: Mutex::new(HashMap::new()) });
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
            node.endpoint.close().await;
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
            let peer: EndpointId = text(&v, "peer")?.parse().map_err(err)?;
            let data = text(&v, "data")?;
            if data.len() > MAX_PACKET { return Err("too_large".into()); }
            tokio::time::timeout(Duration::from_secs(15), async {
                let conn = node.endpoint.connect(peer, ALPN).await.map_err(err)?;
                let result = async {
                    let (mut send, mut recv) = conn.open_bi().await.map_err(err)?;
                    send.write_all(data.as_bytes()).await.map_err(err)?;
                    send.finish().map_err(err)?;
                    let response = recv.read_to_end(MAX_PACKET).await.map_err(err)?;
                    let data = String::from_utf8(response).map_err(err)?;
                    Ok(json!({"data":data}))
                }.await;
                conn.close(0u32.into(), b"done");
                result
            }).await.map_err(|_| "timeout".to_owned())?
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

