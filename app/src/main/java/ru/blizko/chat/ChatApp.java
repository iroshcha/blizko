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
    private static native void initializeIroh(android.content.Context context);
    public final ExecutorService io = Executors.newSingleThreadExecutor();
    public final ExecutorService network=Executors.newFixedThreadPool(2);
    public volatile Node node;
    public volatile String error;
    public volatile boolean activeScreen;
    public volatile String activePeer="",startupError;
    @Override public void onCreate() {
        super.onCreate();
        go.Seq.setContext(this);
        initializeIroh(this);
        android.net.ConnectivityManager connectivity=getSystemService(android.net.ConnectivityManager.class);
        connectivity.registerDefaultNetworkCallback(new android.net.ConnectivityManager.NetworkCallback(){
            @Override public void onAvailable(android.net.Network network){io.execute(ChatApp.this::refreshInterfaces);}
            @Override public void onLost(android.net.Network network){io.execute(ChatApp.this::refreshInterfaces);}
        });
        io.execute(() -> {
            try { node = Mobile.newNode(new File(getNoBackupFilesDir(),"core").getPath(),StorageKey.load(this)); }
            catch (Exception e) { startupError=error=e.getMessage()==null?"Не удалось открыть защищённое хранилище. Данные не удалены.":e.getMessage(); }
        });
    }
    public void refreshInterfaces() { if(node!=null){node.setNetworkAvailable(getSystemService(android.net.ConnectivityManager.class).getActiveNetwork()!=null);node.networkChanged();} }
}
