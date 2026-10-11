package ru.blizko.chat;

import android.app.*;
import android.app.job.*;
import android.content.*;
import android.net.*;
import android.os.*;
import org.json.*;
import java.io.*;
import java.net.URL;
import javax.net.ssl.HttpsURLConnection;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.concurrent.*;

/** Opt-in observations only: never log raw core status, exceptions, addresses or identifiers. */
final class Diagnostics {
    static final String DEFAULT_URL="https://ample-raven-6363.ru.tuna.am/blizko-diagnostics/v1";
    static final int JOB=734103;
    private static final Set<String> EVENTS=new HashSet<>(Arrays.asList("enabled","process_start","process_exit","foreground","background","network","power","screen","service_start","service_restart","service_stop","service_destroy","task_removed","start_failed","connection","heartbeat","pause_gap","manual","message_received","send_queued"));
    private static final ThreadPoolExecutor writer=new ThreadPoolExecutor(1,1,0,TimeUnit.MILLISECONDS,new ArrayBlockingQueue<>(64),r->new Thread(r,"blizko-diagnostics"),new ThreadPoolExecutor.DiscardPolicy());
    private static final Object lock=new Object();
    private static volatile HttpsURLConnection active;
    private static final ScheduledExecutorService deadlines=Executors.newSingleThreadScheduledExecutor();
    static final class Attempt {
        private HttpsURLConnection connection;private boolean stopped;
        synchronized boolean attach(HttpsURLConnection value){if(stopped)return false;connection=value;return true;}
        synchronized boolean stopped(){return stopped;}
        synchronized void cancel(){stopped=true;if(connection!=null)connection.disconnect();}
    }
    private static long generation,lastSample,lastTick;
    private static String lastConnection="";
    private static volatile boolean serviceRunning,coreOnline;
    static SharedPreferences prefs(Context c){return c.getSharedPreferences("diagnostics",Context.MODE_PRIVATE);}
    static boolean enabled(Context c){return prefs(c).getBoolean("enabled",false);}
    static DiagnosticStore store(Context c){return new DiagnosticStore(new File(c.getNoBackupFilesDir(),"diagnostics.json"));}
    static URL endpoint(String value)throws Exception{
        URL url=new URL(value);
        if(!"https".equals(url.getProtocol())||url.getHost().isEmpty()||url.getUserInfo()!=null||url.getQuery()!=null||url.getRef()!=null||(url.getPort()!=-1&&url.getPort()!=443)||!"/blizko-diagnostics/v1".equals(url.getPath()))throw new IOException("Нужен HTTPS-адрес сервера с путём /blizko-diagnostics/v1");
        return url;
    }
    static void enable(Context c,String url,String token)throws Exception{
        endpoint(url);if(!token.matches("[A-Za-z0-9_-]{43}"))throw new IOException("Нужен код доступа, созданный на сервере");
        synchronized(lock){generation++;if(active!=null)active.disconnect();store(c).clear();lastSample=lastTick=0;lastConnection="";
            if(!prefs(c).edit().clear().putBoolean("enabled",true).putString("url",url).putString("token",token).putString("device",UUID.randomUUID().toString()).commit())throw new IOException("Не удалось сохранить настройку");}
        record(c,"enabled");schedule(c);previousExit(c);
    }
    static void disable(Context c){synchronized(lock){generation++;prefs(c).edit().clear().commit();if(active!=null)active.disconnect();store(c).clear();}c.getSystemService(JobScheduler.class).cancel(JOB);c.getSystemService(JobScheduler.class).cancel(JOB+1);}
    static void sendSoon(Context c){if(enabled(c))c.getSystemService(JobScheduler.class).schedule(new JobInfo.Builder(JOB+1,new ComponentName(c,DiagnosticJobService.class)).setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY).setBackoffCriteria(15*60*1000L,JobInfo.BACKOFF_POLICY_EXPONENTIAL).build());}
    static void schedule(Context c){if(!enabled(c))return;JobScheduler scheduler=c.getSystemService(JobScheduler.class);
        if(scheduler.getPendingJob(JOB)==null)scheduler.schedule(new JobInfo.Builder(JOB,new ComponentName(c,DiagnosticJobService.class)).setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY).setPeriodic(60*60*1000L).setBackoffCriteria(15*60*1000L,JobInfo.BACKOFF_POLICY_EXPONENTIAL).setPersisted(true).build());}
    static void initialize(Application c){
        IntentFilter filter=new IntentFilter();filter.addAction(PowerManager.ACTION_DEVICE_IDLE_MODE_CHANGED);filter.addAction(PowerManager.ACTION_POWER_SAVE_MODE_CHANGED);filter.addAction(Intent.ACTION_SCREEN_OFF);filter.addAction(Intent.ACTION_SCREEN_ON);filter.addAction(Intent.ACTION_POWER_CONNECTED);filter.addAction(Intent.ACTION_POWER_DISCONNECTED);
        BroadcastReceiver receiver=new BroadcastReceiver(){public void onReceive(Context context,Intent intent){record(c,intent.getAction()!=null&&intent.getAction().startsWith("android.intent.action.SCREEN")?"screen":"power");}};
        if(Build.VERSION.SDK_INT>=33)c.registerReceiver(receiver,filter,Context.RECEIVER_NOT_EXPORTED);else c.registerReceiver(receiver,filter);
        c.registerActivityLifecycleCallbacks(new Application.ActivityLifecycleCallbacks(){
            private int started;
            public void onActivityStarted(Activity a){if(started++==0)record(c,"foreground");}
            public void onActivityStopped(Activity a){if(--started==0)record(c,"background");}
            public void onActivityCreated(Activity a,Bundle b){}public void onActivityResumed(Activity a){}public void onActivityPaused(Activity a){}public void onActivitySaveInstanceState(Activity a,Bundle b){}public void onActivityDestroyed(Activity a){}
        });
        record(c,"process_start");previousExit(c);schedule(c);
    }
    private static void previousExit(Context c){
        if(!enabled(c)||Build.VERSION.SDK_INT<30)return;
        try{long last=prefs(c).getLong("lastExit",0),newest=last;
            for(ApplicationExitInfo e:c.getSystemService(ActivityManager.class).getHistoricalProcessExitReasons(c.getPackageName(),0,5))if(e.getTimestamp()>last){record(c,"process_exit",e.getReason(),e.getTimestamp());newest=Math.max(newest,e.getTimestamp());}
            prefs(c).edit().putLong("lastExit",newest).apply();
        }catch(RuntimeException ignored){}
    }
    static void service(Context c,String event,boolean running){serviceRunning=running;record(c,event);}
    static void sample(Context c,JSONObject status){
        coreOnline=status.optBoolean("online");if(!enabled(c))return;
        long now=SystemClock.elapsedRealtime();String state=status.optBoolean("enabled")+":"+coreOnline+":"+status.optBoolean("networkAvailable");
        if(lastTick>0&&now-lastTick>120000)record(c,"pause_gap");lastTick=now;
        if(!state.equals(lastConnection)){lastConnection=state;lastSample=now;record(c,"connection");}
        else if(now-lastSample>=15*60*1000){lastSample=now;record(c,"heartbeat");}
    }
    static void record(Context c,String event){record(c,event,0,0);}
    private static void record(Context context,String event,int reason,long exitTime){
        if(!EVENTS.contains(event)||!enabled(context))return;Context c=context.getApplicationContext();
        long epoch; synchronized(lock){epoch=generation;}
        // Capture at observation time, before disk work is queued.
        try{JSONObject e=new JSONObject().put("id",UUID.randomUUID().toString()).put("event",event).put("time",System.currentTimeMillis()).put("elapsed",SystemClock.elapsedRealtime()).put("uptime",SystemClock.uptimeMillis()).put("reason",reason).put("exitTime",exitTime);
            PowerManager p=c.getSystemService(PowerManager.class);ActivityManager am=c.getSystemService(ActivityManager.class);
            Intent b=c.registerReceiver(null,new IntentFilter(Intent.ACTION_BATTERY_CHANGED));int scale=b==null?0:b.getIntExtra(BatteryManager.EXTRA_SCALE,0);int battery=scale<=0?-1:Math.max(-1,Math.min(100,b.getIntExtra(BatteryManager.EXTRA_LEVEL,-1)*100/scale));
            ConnectivityManager cm=c.getSystemService(ConnectivityManager.class);NetworkCapabilities n=cm.getNetworkCapabilities(cm.getActiveNetwork());
            String network=n==null?"none":n.hasTransport(NetworkCapabilities.TRANSPORT_VPN)?"vpn":n.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)?"wifi":n.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)?"cellular":"other";
            e.put("battery",battery).put("charging",b!=null&&b.getIntExtra(BatteryManager.EXTRA_PLUGGED,0)!=0).put("screen",p.isInteractive()).put("idle",p.isDeviceIdleMode()).put("saving",p.isPowerSaveMode()).put("exempt",p.isIgnoringBatteryOptimizations(c.getPackageName())).put("restricted",Build.VERSION.SDK_INT>=28&&am.isBackgroundRestricted()).put("network",network).put("validated",n!=null&&n.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)).put("service",serviceRunning).put("online",coreOnline).put("receive",c.getSharedPreferences("connection",Context.MODE_PRIVATE).getBoolean("receiveEnabled",false));
            writer.execute(()->{synchronized(lock){if(epoch!=generation||!enabled(c))return;try{store(c).append(e,System.currentTimeMillis());}catch(Exception ignored){prefs(c).edit().putString("status","Не удалось записать диагностику").apply();}}});
        }catch(Exception ignored){}
    }
    static boolean upload(Context c,Attempt attempt)throws Exception{
        JSONArray batch;String device,token,url;long epoch;
        synchronized(lock){if(!enabled(c))return true;epoch=generation;batch=store(c).batch(System.currentTimeMillis());device=prefs(c).getString("device","");token=prefs(c).getString("token","");url=prefs(c).getString("url","");}
        if(batch.length()==0)return true;
        byte[] body=new JSONObject().put("v",1).put("device",device).put("version",BuildConfig.VERSION_CODE).put("sdk",Build.VERSION.SDK_INT).put("events",batch).toString().getBytes(StandardCharsets.UTF_8);
        if(body.length>65536)throw new IOException("Batch limit");
        HttpsURLConnection connection=(HttpsURLConnection)endpoint(url).openConnection();
        if(!attempt.attach(connection))return false;
        ScheduledFuture<?> timeout=deadlines.schedule(attempt::cancel,45,TimeUnit.SECONDS);
        try{
            synchronized(lock){if(epoch!=generation||!enabled(c))return true;if(active!=null)return false;active=connection;}
            connection.setConnectTimeout(15000);connection.setReadTimeout(15000);connection.setInstanceFollowRedirects(false);connection.setRequestMethod("POST");connection.setDoOutput(true);connection.setFixedLengthStreamingMode(body.length);connection.setRequestProperty("Content-Type","application/json");connection.setRequestProperty("Authorization","Bearer "+token);
            try(OutputStream out=connection.getOutputStream()){out.write(body);}int code=connection.getResponseCode();
            synchronized(lock){if(epoch!=generation||!enabled(c))return true;if(attempt.stopped())return false;
                if(code!=204){prefs(c).edit().putString("status",code==401?"Сервер отклонил код доступа":"Сервер не принял журнал ("+code+")").apply();return false;}
                store(c).acknowledge(batch);prefs(c).edit().putLong("lastSent",System.currentTimeMillis()).putString("status","Порция журнала доставлена").apply();return true;}
        }finally{timeout.cancel(false);connection.disconnect();synchronized(lock){if(active==connection)active=null;}}
    }
}
