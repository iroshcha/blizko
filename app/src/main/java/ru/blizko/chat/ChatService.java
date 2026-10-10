package ru.blizko.chat;

import android.app.*;
import android.content.*;
import android.content.pm.ServiceInfo;
import android.os.*;
import org.json.JSONObject;

public final class ChatService extends Service {
    private final Handler handler = new Handler(Looper.getMainLooper());
    private int incoming = -1;
    private JSONObject previousUnread;
    private ChatApp app;
    private boolean destroyed=false,polling=false;
    private static final String RECEIVE="receiveEnabled";
    private final Runnable watch = new Runnable() { public void run() {
        if(!polling&&app.node!=null){polling=true;app.io.execute(()->{
            String raw=app.node.status();handler.post(()->{polling=false;if(destroyed)return;try {
            JSONObject s=new JSONObject(raw); int count=s.optInt("incoming");
            JSONObject unread=s.optJSONObject("unread");String notifyPeer="";
            if(previousUnread!=null&&unread!=null){java.util.Iterator<String> peers=unread.keys();while(peers.hasNext()){String peer=peers.next();if(unread.optInt(peer)>previousUnread.optInt(peer)&&(!app.activeScreen||!peer.equals(app.activePeer))){notifyPeer=peer;break;}}}
            if(!notifyPeer.isEmpty()) {
                getSystemService(NotificationManager.class).notify(2,new Notification.Builder(ChatService.this,"messages")
                    .setSmallIcon(ru.blizko.chat.R.drawable.ic_chat).setContentTitle("Близко")
                    .setContentText("Новое сообщение").setContentIntent(openPeer(notifyPeer)).setAutoCancel(true).build());
            }
            previousUnread=unread;
            incoming=count;
            getSystemService(NotificationManager.class).notify(1,notification(s.optString("status")));
        }catch(Exception ignored){} });});}
        handler.postDelayed(this,5000);
    }};
    private PendingIntent open(){return PendingIntent.getActivity(this,0,new Intent(this,MainActivity.class),PendingIntent.FLAG_IMMUTABLE|PendingIntent.FLAG_UPDATE_CURRENT);}
    private PendingIntent openPeer(String peer){return PendingIntent.getActivity(this,2,new Intent(this,MainActivity.class).putExtra("peer",peer).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP|Intent.FLAG_ACTIVITY_CLEAR_TOP),PendingIntent.FLAG_IMMUTABLE|PendingIntent.FLAG_UPDATE_CURRENT);}
    private Notification notification(String status){
        PendingIntent stop=PendingIntent.getService(this,1,new Intent(this,ChatService.class).setAction("STOP"),PendingIntent.FLAG_IMMUTABLE);
        return new Notification.Builder(this,"connection").setSmallIcon(R.drawable.ic_chat).setContentTitle("Близко · фоновый приём")
            .setContentText(status).setOngoing(true).setContentIntent(open()).addAction(new Notification.Action.Builder(null,"Выключить",stop).build()).build();
    }
    @Override public void onCreate(){
        super.onCreate();app=(ChatApp)getApplication();NotificationManager nm=getSystemService(NotificationManager.class);
        nm.createNotificationChannel(new NotificationChannel("connection","Подключение",NotificationManager.IMPORTANCE_LOW));
        nm.createNotificationChannel(new NotificationChannel("messages","Сообщения",NotificationManager.IMPORTANCE_DEFAULT));
    }
    @Override public int onStartCommand(Intent intent,int flags,int id){
        android.content.SharedPreferences preferences=getSharedPreferences("connection",MODE_PRIVATE);
        if(intent!=null&&"STOP".equals(intent.getAction())){preferences.edit().putBoolean(RECEIVE,false).commit();stopSelf();return START_NOT_STICKY;}
        if(intent==null&&!preferences.getBoolean(RECEIVE,false)){stopSelf();return START_NOT_STICKY;}
        if(intent!=null)preferences.edit().putBoolean(RECEIVE,true).commit();
        if(Build.VERSION.SDK_INT>=34)startForeground(1,notification("Подключение…"),ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
        else startForeground(1,notification("Подключение…"));
        app.io.execute(()->{try{if(app.node==null)throw new Exception();app.refreshInterfaces();app.node.start();}
            catch(Exception e){app.error="Не удалось включить приём. Попробуйте подключиться заново.";stopSelf();}});
        handler.removeCallbacks(watch);handler.post(watch);return START_STICKY;
    }
    @Override public void onDestroy(){destroyed=true;handler.removeCallbacks(watch);app.io.execute(()->{if(app.node!=null)app.node.stop();});super.onDestroy();}
    @Override public IBinder onBind(Intent intent){return null;}
}
