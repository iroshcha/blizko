use super::*;

#[test]
fn windows_relay_round_trip() {
    if std::env::var("BLIZKO_NETWORK_SMOKE").as_deref() != Ok("1") { return; }
    runtime().block_on(async {
        let a = dispatch(json!({"op":"start","key":hex::encode(SecretKey::generate().to_bytes()),"relayOnly":true,"peers":[]})).await.unwrap();
        let b = dispatch(json!({"op":"start","key":hex::encode(SecretKey::generate().to_bytes()),"relayOnly":true,"peers":[]})).await.unwrap();
        let ah = a["handle"].as_u64().unwrap();
        let bh = b["handle"].as_u64().unwrap();
        dispatch(json!({"op":"allow","handle":ah,"peers":[b["address"]]})).await.unwrap();
        dispatch(json!({"op":"allow","handle":bh,"peers":[a["address"]]})).await.unwrap();
        tokio::time::sleep(Duration::from_secs(5)).await;
        let expected = selected_relay().unwrap().to_string();
        for handle in [ah, bh] {
            let status = dispatch(json!({"op":"status","handle":handle})).await.unwrap();
            assert_eq!(status["relay"], expected, "wrong home relay");
            assert_eq!(status["online"], true, "home relay is not connected");
        }
        let sender = tokio::spawn(dispatch(json!({"op":"exchange","handle":ah,"peer":b["address"],"data":"windows transport test"})));
        let receiver = async {
            loop {
                let event = dispatch(json!({"op":"next","handle":bh})).await.unwrap();
                if event["request"].is_u64() {
                    assert_eq!(event["data"], "windows transport test");
                    dispatch(json!({"op":"reply","handle":bh,"request":event["request"],"data":"authenticated reply fixture"})).await.unwrap();
                    return;
                }
            }
        };
        let received = tokio::time::timeout(Duration::from_secs(18), receiver).await;
        let result = sender.await.unwrap();
        dispatch(json!({"op":"close","handle":ah})).await.unwrap();
        dispatch(json!({"op":"close","handle":bh})).await.unwrap();
        assert!(received.is_ok(), "receiver timed out; exchange result: {result:?}");
        assert_eq!(result.unwrap()["data"], "authenticated reply fixture");
    });
}

#[test]
fn windows_direct_connection_upgrades_from_home_relay() {
    if std::env::var("BLIZKO_NETWORK_SMOKE").as_deref() != Ok("1") { return; }
    runtime().block_on(async {
        let a = dispatch(json!({"op":"start","key":hex::encode(SecretKey::generate().to_bytes()),"relayOnly":false,"peers":[]})).await.unwrap();
        let b = dispatch(json!({"op":"start","key":hex::encode(SecretKey::generate().to_bytes()),"relayOnly":false,"peers":[]})).await.unwrap();
        let ah = a["handle"].as_u64().unwrap();
        let bh = b["handle"].as_u64().unwrap();
        dispatch(json!({"op":"allow","handle":ah,"peers":[b["address"]]})).await.unwrap();
        dispatch(json!({"op":"allow","handle":bh,"peers":[a["address"]]})).await.unwrap();
        let an = nodes().lock().unwrap().get(&ah).unwrap().clone();
        let bn = nodes().lock().unwrap().get(&bh).unwrap().clone();
        let address = EndpointAddr::new(bn.endpoint.id()).with_relay_url(an.relay.clone());
        let conn = an.endpoint.connect(address, ALPN).await.unwrap();
        let upgraded = tokio::time::timeout(Duration::from_secs(15), async {
            while !conn.paths().iter().any(|path| path.is_ip() && path.is_selected()) {
                tokio::time::sleep(Duration::from_millis(100)).await;
            }
        }).await;
        conn.close(0u32.into(), b"test done");
        dispatch(json!({"op":"close","handle":ah})).await.unwrap();
        dispatch(json!({"op":"close","handle":bh})).await.unwrap();
        assert!(upgraded.is_ok(), "same-computer peers did not select a direct IP path");
    });
}

#[test]
fn custom_relay_requires_https_outside_loopback() {
    for url in ["https://relay.example.com", "http://127.0.0.1:3340", "http://[::1]:3340"] {
        assert!(configured_relay(url).is_ok(), "{url}");
    }
    for url in ["http://relay.example.com", "ftp://relay.example.com", "https://secret@relay.example.com", "https://relay.example.com/?token=secret", "https://relay.example.com/path", "not a url"] {
        assert!(configured_relay(url).is_err(), "{url}");
    }
}
