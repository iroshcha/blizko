package ru.blizko.chat;

import android.app.*;
import android.app.job.*;
import android.content.*;

/** Small metadata checks scheduled by Android; APK downloads remain explicit. */
final class AutoUpdates {
    static final int PERIODIC_JOB=210, STARTUP_JOB=211, NOTIFICATION=3;
    static final long PERIOD=12*60*60*1000L, STARTUP_INTERVAL=6*60*60*1000L;
    static final String CHANNEL="updates";
    static SharedPreferences prefs(Context context){return context.getSharedPreferences("updates",Context.MODE_PRIVATE);}
    static boolean enabled(Context context){return prefs(context).getBoolean("automatic",true)&&!BuildConfig.UPDATE_MANIFEST_URL.isEmpty();}
    static void setEnabled(Context context,boolean enabled){prefs(context).edit().putBoolean("automatic",enabled).apply();schedule(context);}
    static void schedule(Context context){
        JobScheduler scheduler=context.getSystemService(JobScheduler.class);
        if(!enabled(context)){scheduler.cancel(PERIODIC_JOB);scheduler.cancel(STARTUP_JOB);context.getSystemService(NotificationManager.class).cancel(NOTIFICATION);return;}
        ComponentName service=new ComponentName(context,UpdateJobService.class);
        if(scheduler.getPendingJob(PERIODIC_JOB)==null)scheduler.schedule(new JobInfo.Builder(PERIODIC_JOB,service)
            .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY).setPeriodic(PERIOD).setPersisted(true)
            .setBackoffCriteria(60*60*1000L,JobInfo.BACKOFF_POLICY_EXPONENTIAL).build());
        long age=System.currentTimeMillis()-prefs(context).getLong("lastAttempt",0);
        if((age<0||age>=STARTUP_INTERVAL)&&scheduler.getPendingJob(STARTUP_JOB)==null)
            scheduler.schedule(new JobInfo.Builder(STARTUP_JOB,service).setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setMinimumLatency(1000).setBackoffCriteria(60*60*1000L,JobInfo.BACKOFF_POLICY_EXPONENTIAL).build());
        showCached(context);
    }
    static AppUpdates.Release available(Context context){
        try{AppUpdates.Release release=new AppUpdates.Release(prefs(context).getString("available",""));
            return release.versionCode>BuildConfig.VERSION_CODE?release:null;
        }catch(Exception ignored){return null;}
    }
    static synchronized void record(Context context,AppUpdates.Release release){
        SharedPreferences preferences=prefs(context);
        if(release.versionCode>BuildConfig.VERSION_CODE){
            AppUpdates.Release old=available(context);
            if(old==null||release.versionCode>=old.versionCode)preferences.edit().putString("available",release.json).apply();
            showCached(context);
        }else if(available(context)==null){
            preferences.edit().remove("available").apply();context.getSystemService(NotificationManager.class).cancel(NOTIFICATION);
        }
    }
    static synchronized void showCached(Context context){
        AppUpdates.Release release=available(context);NotificationManager manager=context.getSystemService(NotificationManager.class);
        if(release==null){manager.cancel(NOTIFICATION);return;}
        if(!enabled(context)||!manager.areNotificationsEnabled())return;
        manager.createNotificationChannel(new NotificationChannel(CHANNEL,"Обновления приложения",NotificationManager.IMPORTANCE_DEFAULT));
        NotificationChannel channel=manager.getNotificationChannel(CHANNEL);
        if(channel==null||channel.getImportance()==NotificationManager.IMPORTANCE_NONE)return;
        String key=release.versionCode+":"+release.sha256;
        if(key.equals(prefs(context).getString("notified","")))return;
        PendingIntent open=PendingIntent.getActivity(context,NOTIFICATION,new Intent(context,UpdateActivity.class),PendingIntent.FLAG_UPDATE_CURRENT|PendingIntent.FLAG_IMMUTABLE);
        try{
            manager.notify(NOTIFICATION,new Notification.Builder(context,CHANNEL).setSmallIcon(ru.blizko.chat.R.drawable.ic_chat)
                .setContentTitle("Близко: новая версия "+release.versionName).setContentText("Нажмите, чтобы скачать и установить обновление")
                .setContentIntent(open).setAutoCancel(true).setOnlyAlertOnce(true).build());
            prefs(context).edit().putString("notified",key).apply();
        }catch(SecurityException ignored){ /* Permission can be revoked between the check and notify. */ }
    }
}
