package ru.blizko.chat;

import android.app.Application;
import mobile.Mobile;
import mobile.Node;
import java.io.File;
import java.net.NetworkInterface;
import java.net.InterfaceAddress;
import java.util.Collections;
import java.util.concurrent.*;
import org.json.*;

public final class ChatApp extends Application {
    public final ExecutorService io = Executors.newSingleThreadExecutor();
    public volatile Node node;
    public volatile String error;
    public volatile boolean activeScreen;
    @Override public void onCreate() {
        super.onCreate();
        go.Seq.setContext(this);
        io.execute(() -> {
            try { refreshInterfaces(); node = Mobile.newNode(new File(getNoBackupFilesDir(),"core").getPath(),StorageKey.load(this)); }
            catch (Exception e) { error = "Не удалось открыть защищённое хранилище. Данные не удалены."; }
        });
    }
    public void refreshInterfaces() {
        try {
            JSONArray rows=new JSONArray();
            for(NetworkInterface n:Collections.list(NetworkInterface.getNetworkInterfaces())) {
                JSONArray addresses=new JSONArray();
                for(InterfaceAddress a:n.getInterfaceAddresses()) {
                    String host=a.getAddress().getHostAddress(); if(host==null)continue;
                    int zone=host.indexOf('%');if(zone>=0)host=host.substring(0,zone);
                    addresses.put(host+"/"+a.getNetworkPrefixLength());
                }
                rows.put(new JSONObject().put("index",n.getIndex()).put("name",n.getName()).put("mtu",n.getMTU())
                    .put("up",n.isUp()).put("loopback",n.isLoopback()).put("addresses",addresses));
            }
            Mobile.setInterfaces(rows.toString());
        }catch(Exception ignored) { }
    }
}
