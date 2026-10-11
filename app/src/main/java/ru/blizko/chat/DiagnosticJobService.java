package ru.blizko.chat;

import android.app.job.*;
import android.os.*;
import java.util.concurrent.*;
import java.util.*;

public final class DiagnosticJobService extends JobService {
    private final ExecutorService worker=Executors.newSingleThreadExecutor();
    private final Handler main=new Handler(Looper.getMainLooper());
    private final Map<Integer,Diagnostics.Attempt> running=new HashMap<>();
    @Override public boolean onStartJob(JobParameters params){
        if(!Diagnostics.enabled(this))return false;Diagnostics.Attempt attempt=new Diagnostics.Attempt();running.put(params.getJobId(),attempt);
        worker.execute(()->{boolean retry=false;try{retry=!Diagnostics.upload(this,attempt);}catch(Exception e){retry=true;if(Diagnostics.enabled(this))Diagnostics.prefs(this).edit().putString("status","Связь с сервером недоступна; журнал сохранён на телефоне").apply();}
            boolean again=retry;main.post(()->{if(running.get(params.getJobId())==attempt){running.remove(params.getJobId());jobFinished(params,again&&Diagnostics.enabled(this));}});});return true;
    }
    @Override public boolean onStopJob(JobParameters params){Diagnostics.Attempt attempt=running.remove(params.getJobId());if(attempt!=null)attempt.cancel();return Diagnostics.enabled(this);}
    @Override public void onDestroy(){for(Diagnostics.Attempt attempt:running.values())attempt.cancel();running.clear();worker.shutdownNow();super.onDestroy();}
}
