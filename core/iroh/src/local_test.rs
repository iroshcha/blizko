use super::*;

async fn local_node()->(u64,Arc<Node>) {
    let endpoint=Endpoint::builder(presets::Minimal).alpns(vec![ALPN.to_vec()])
        .relay_mode(iroh::RelayMode::Disabled).bind_addr("127.0.0.1:0").unwrap().bind().await.unwrap();
    let(tx,rx)=mpsc::channel(8);
    let node=Arc::new(Node{endpoint,relay:configured_relay(HOME_RELAY).unwrap(),incoming:tokio::sync::Mutex::new(rx),
        replies:Mutex::new(HashMap::new()),allowed:RwLock::new(HashMap::new()),connections:tokio::sync::Mutex::new(HashMap::new()),requests:Mutex::new(Requests::default())});
    let handle=SEQUENCE.fetch_add(1,Ordering::Relaxed);nodes().lock().unwrap().insert(handle,node.clone());
    tokio::spawn(serve(node.clone(),tx));(handle,node)
}
async fn direct(a:&Node,b:&Node)->iroh::endpoint::Connection {
    let addr=b.endpoint.bound_sockets().into_iter().find(|a|a.is_ipv4()).unwrap();
    tokio::time::timeout(Duration::from_secs(3),a.endpoint.connect(EndpointAddr::new(b.endpoint.id()).with_ip_addr(addr),ALPN)).await.unwrap().unwrap()
}
async fn install_connection(a:&Node,b:&Node){
    let connection=direct(a,b).await;
    a.connections.lock().await.insert(b.endpoint.id().to_string(),Arc::new(tokio::sync::Mutex::new(Some(connection))));
}
async fn next(node:&Node)->Value {
    tokio::time::timeout(Duration::from_secs(3),node.incoming.lock().await.recv()).await.unwrap().unwrap()
}
#[test]
fn unknown_idle_connections_do_not_block_known_contact_and_connection_is_reused(){
    runtime().block_on(async{
        let(ah,a)=local_node().await;let(bh,b)=local_node().await;let(xh,x)=local_node().await;
        update_allowed(&a,&json!([b.endpoint.id().to_string()])).unwrap();
        update_allowed(&b,&json!([a.endpoint.id().to_string()])).unwrap();
        for _ in 0..12 {
            let unknown=direct(&x,&b).await;
            tokio::time::timeout(Duration::from_secs(2),unknown.closed()).await.expect("unknown connection retained a slot");
        }
        install_connection(&a,&b).await;
        for data in ["one","two"] {
            let sender=tokio::spawn(dispatch(json!({"op":"exchange","handle":ah,"peer":b.endpoint.id().to_string(),"data":data})));
            let event=next(&b).await;assert_eq!(event["data"],data);
            dispatch(json!({"op":"reply","handle":bh,"request":event["request"],"data":"receipt"})).await.unwrap();
            assert_eq!(sender.await.unwrap().unwrap()["data"],"receipt");
        }
        assert_eq!(a.connections.lock().await.len(),1);
        for h in [ah,bh,xh]{dispatch(json!({"op":"close","handle":h})).await.unwrap();}
    });
}
#[test]
fn cancellation_cleans_request_and_releases_remote_slot(){
    runtime().block_on(async{
        let(ah,a)=local_node().await;let(bh,b)=local_node().await;
        update_allowed(&a,&json!([b.endpoint.id().to_string()])).unwrap();
        update_allowed(&b,&json!([a.endpoint.id().to_string()])).unwrap();
        install_connection(&a,&b).await;
        let sender=tokio::spawn(dispatch(json!({"op":"exchange","handle":ah,"peer":b.endpoint.id().to_string(),"data":"no reply","request":98765})));
        let _event=next(&b).await;
        dispatch(json!({"op":"cancel","handle":ah,"request":98765})).await.unwrap();
        assert_eq!(tokio::time::timeout(Duration::from_secs(2),sender).await.unwrap().unwrap().unwrap_err(),"cancelled");
        assert!(a.requests.lock().unwrap().active.is_empty());
        tokio::time::timeout(Duration::from_secs(2),async{while !b.replies.lock().unwrap().is_empty(){tokio::time::sleep(Duration::from_millis(10)).await;}}).await.unwrap();
        dispatch(json!({"op":"allow","handle":ah,"peers":[]})).await.unwrap();
        assert!(a.connections.lock().await.is_empty());
        for h in [ah,bh]{dispatch(json!({"op":"close","handle":h})).await.unwrap();}
    });
}
