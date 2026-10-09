use super::*;

#[test]
fn windows_relay_round_trip() {
    if std::env::var("BLIZKO_NETWORK_SMOKE").as_deref() != Ok("1") { return; }
    runtime().block_on(async {
        let a = dispatch(json!({"op":"start","key":hex::encode(SecretKey::generate().to_bytes()),"relayOnly":true})).await.unwrap();
        let b = dispatch(json!({"op":"start","key":hex::encode(SecretKey::generate().to_bytes()),"relayOnly":true})).await.unwrap();
        let ah = a["handle"].as_u64().unwrap();
        let bh = b["handle"].as_u64().unwrap();
        tokio::time::sleep(Duration::from_secs(5)).await;
        eprintln!("A status {}", dispatch(json!({"op":"status","handle":ah})).await.unwrap());
        eprintln!("B status {}", dispatch(json!({"op":"status","handle":bh})).await.unwrap());
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
fn custom_relay_requires_https_outside_loopback() {
    for url in ["https://relay.example.com", "http://127.0.0.1:3340", "http://[::1]:3340"] {
        assert!(configured_relay(url).is_ok(), "{url}");
    }
    for url in ["http://relay.example.com", "ftp://relay.example.com", "https://secret@relay.example.com", "https://relay.example.com/?token=secret", "https://relay.example.com/path", "not a url"] {
        assert!(configured_relay(url).is_err(), "{url}");
    }
}
