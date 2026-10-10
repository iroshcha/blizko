package ru.blizko.chat;

import android.app.job.*;
import android.os.Handler;
import android.os.Looper;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.*;

public final class UpdateJobService extends JobService {
    private final ExecutorService worker=Executors.newSingleThreadExecutor();
    private final Handler main=new Handler(Looper.getMainLooper());
    private final Map<Integer,JobParameters> running=new HashMap<>();
    @Override public boolean onStartJob(JobParameters parameters){
        if(!AutoUpdates.enabled(this))return false;
        AutoUpdates.prefs(this).edit().putLong("lastAttempt",System.currentTimeMillis()).apply();
        running.put(parameters.getJobId(),parameters);
        worker.submit(()->{
            boolean retry=false;
            AppUpdates.Release result=null;
            try{
                result=AppUpdates.check(BuildConfig.UPDATE_MANIFEST_URL);
            }catch(Exception ignored){retry=true;}
            boolean again=retry;AppUpdates.Release release=result;
            main.post(()->{
                if(running.get(parameters.getJobId())!=parameters)return;
                running.remove(parameters.getJobId());
                if(AutoUpdates.enabled(this)&&release!=null)AutoUpdates.record(this,release);
                jobFinished(parameters,again&&AutoUpdates.enabled(this));
            });
        });
        return true;
    }
    @Override public boolean onStopJob(JobParameters parameters){
        if(running.get(parameters.getJobId())==parameters)running.remove(parameters.getJobId());
        return AutoUpdates.enabled(this);
    }
    @Override public void onDestroy(){running.clear();worker.shutdownNow();super.onDestroy();}
}
