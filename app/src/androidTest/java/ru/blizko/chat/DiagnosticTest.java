package ru.blizko.chat;

import android.app.*;
import android.app.job.*;
import android.content.*;
import android.os.Bundle;
import org.json.*;
import java.io.*;
import java.net.*;
import javax.net.ssl.*;
import java.security.Principal;
import java.security.cert.Certificate;
import java.util.*;
import java.util.concurrent.*;

/** Test-only HTTPS transport; production TLS trust and endpoint rules stay unchanged. */
public final class DiagnosticTest {
    private final Instrumentation runner;
    DiagnosticTest(Instrumentation runner){this.runner=runner;}
    private static volatile int response=204;
    private static volatile Fake last;
    private Context getTargetContext(){return runner.getTargetContext();}
    private void finish(int code,Bundle result){runner.finish(code,result);}
    private static void check(boolean ok,String message){if(!ok)throw new AssertionError(message);}
    private static void flush()throws Exception{java.lang.reflect.Field f=Diagnostics.class.getDeclaredField("writer");f.setAccessible(true);((ExecutorService)f.get(null)).submit(()->{}).get(10,TimeUnit.SECONDS);}
    private static JSONObject event(int n,long time)throws Exception{return new JSONObject().put("id","event-"+n).put("time",time);}
    private String shell(String command)throws Exception{
        try(InputStream in=new android.os.ParcelFileDescriptor.AutoCloseInputStream(runner.getUiAutomation().executeShellCommand(command));ByteArrayOutputStream out=new ByteArrayOutputStream()){
            byte[] buffer=new byte[4096];int n;while((n=in.read(buffer))!=-1)out.write(buffer,0,n);return out.toString("UTF-8");
        }
    }
    void run(Bundle args){Bundle result=new Bundle();try{
        Context c=getTargetContext();
        if(args!=null&&"preview".equals(args.getString("mode"))){
            runner.startActivitySync(new Intent(c,DiagnosticActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));android.os.SystemClock.sleep(1000);
            try(FileOutputStream out=new FileOutputStream(new File(c.getCacheDir(),"diagnostics-preview.png"))){runner.getUiAutomation().takeScreenshot().compress(android.graphics.Bitmap.CompressFormat.PNG,100,out);}
            result.putString("stream","PASS: diagnostics screen captured");finish(Activity.RESULT_OK,result);return;
        }
        if(args!=null&&"power".equals(args.getString("mode"))){
            Diagnostics.disable(c);Diagnostics.enable(c,Diagnostics.DEFAULT_URL,String.join("",Collections.nCopies(43,"a")));
            try{
                runner.startActivitySync(new Intent(c,MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
                c.startForegroundService(new Intent(c,ChatService.class));Thread.sleep(2000);
                shell("dumpsys battery unplug");shell("input keyevent 223");Thread.sleep(1000);shell("dumpsys deviceidle force-idle");String idle=shell("dumpsys deviceidle get deep");check("IDLE".equals(idle.trim()),"Cannot force Doze: "+idle);Thread.sleep(3000);
                shell("dumpsys deviceidle unforce");shell("dumpsys battery reset");shell("input keyevent 224");Thread.sleep(2000);Diagnostics.record(c,"manual");flush();
                JSONArray data=Diagnostics.store(c).batch(System.currentTimeMillis());boolean entered=false,left=false,screenOff=false;
                for(int i=0;i<data.length();i++){JSONObject e=data.getJSONObject(i);if(e.optBoolean("idle"))entered=true;else if(entered)left=true;if(!e.optBoolean("screen"))screenOff=true;}
                try(FileOutputStream out=new FileOutputStream(new File(c.getCacheDir(),"diagnostics-power.json"))){out.write(data.toString(2).getBytes(java.nio.charset.StandardCharsets.UTF_8));}
                check(entered&&left&&screenOff,"Missing sleep/wake observations");result.putString("stream","PASS: screen off, forced Android Doze, exit from Doze and persisted observations; battery state restored");
            }finally{shell("dumpsys deviceidle unforce");shell("dumpsys battery reset");shell("input keyevent 224");c.stopService(new Intent(c,ChatService.class));Diagnostics.disable(c);}
            finish(Activity.RESULT_OK,result);return;
        }
        Diagnostics.disable(c);Diagnostics.record(c,"manual");flush();check(Diagnostics.store(c).batch(System.currentTimeMillis()).length()==0,"Disabled collection wrote events");
        for(String bad:new String[]{"http://example.com/blizko-diagnostics/v1","https://user@example.com/blizko-diagnostics/v1","https://example.com/blizko-diagnostics/v1?token=x","https://example.com/other","https://example.com:8443/blizko-diagnostics/v1"}){boolean rejected=false;try{Diagnostics.endpoint(bad);}catch(Exception expected){rejected=true;}check(rejected,"Unsafe endpoint accepted");}
        File queue=new File(c.getCacheDir(),"diagnostic-test.json");DiagnosticStore s=new DiagnosticStore(queue);s.clear();long now=System.currentTimeMillis();s.append(event(-1,now-DiagnosticStore.RETENTION-1),now);
        for(int i=0;i<DiagnosticStore.LIMIT+7;i++)s.append(event(i,now),now);
        JSONArray first=s.batch(now);check(first.length()==64&&"event-7".equals(first.getJSONObject(0).getString("id")),"Retention or ring cap failed");
        // Restart opens the same durable file; acknowledgement must preserve later appends.
        s=new DiagnosticStore(queue);s.append(event(9999,now),now);s.acknowledge(first);JSONArray rest=s.batch(now);check("event-71".equals(rest.getJSONObject(0).getString("id")),"Acknowledgement lost newer rows");
        check(s.batch(now+DiagnosticStore.RETENTION+1).length()==0,"Expired queue retained");s.clear();
        URL.setURLStreamHandlerFactory(protocol->"https".equals(protocol)?new URLStreamHandler(){protected URLConnection openConnection(URL u){last=new Fake(u);return last;}}:null);
        Diagnostics.enable(c,Diagnostics.DEFAULT_URL,String.join("",Collections.nCopies(43,"a")));Diagnostics.record(c,"manual");flush();
        JSONArray snapshot=Diagnostics.store(c).batch(System.currentTimeMillis());check(snapshot.length()>0,"No observations");
        Set<String> allowed=new HashSet<>(Arrays.asList("id","event","time","elapsed","uptime","reason","exitTime","battery","charging","screen","idle","saving","exempt","restricted","network","validated","service","online","receive"));
        for(int i=0;i<snapshot.length();i++){Iterator<String> keys=snapshot.getJSONObject(i).keys();while(keys.hasNext())check(allowed.contains(keys.next()),"Unexpected private field");}
        JobInfo scheduled=c.getSystemService(JobScheduler.class).getPendingJob(Diagnostics.JOB);check(scheduled!=null&&scheduled.isPersisted()&&scheduled.isPeriodic()&&scheduled.getNetworkType()==JobInfo.NETWORK_TYPE_ANY,"Background job constraints");
        response=503;check(!Diagnostics.upload(c,new Diagnostics.Attempt()),"Failed server acknowledged");check(Diagnostics.store(c).batch(now).length()>0,"Failed upload erased queue");
        response=204;check(Diagnostics.upload(c,new Diagnostics.Attempt()),"Successful upload failed");check(!last.getInstanceFollowRedirects(),"Credentials follow redirects");check(last.getRequestProperty("Authorization").startsWith("Bearer "),"No access code");JSONObject payload=new JSONObject(last.body.toString("UTF-8"));check(payload.getJSONArray("events").length()>0,"Empty upload");check(Diagnostics.store(c).batch(now).length()==0,"Ack not applied");
        Diagnostics.record(c,"manual");Diagnostics.disable(c);flush();check(Diagnostics.store(c).batch(now).length()==0,"Disable race recreated log");check(!Diagnostics.prefs(c).contains("token"),"Access code retained after disable");check(c.getSystemService(JobScheduler.class).getPendingJob(Diagnostics.JOB)==null,"Upload still scheduled");
        result.putString("stream","PASS: opt-in, fixed privacy schema, endpoint validation, bounded durable queue, expiry, acknowledgement after append, upload failure retention, successful upload, redirect refusal, scheduler and disable race");finish(Activity.RESULT_OK,result);
    }catch(Throwable e){result.putString("stream","FAIL: "+e);finish(Activity.RESULT_CANCELED,result);}}
    private static final class Fake extends HttpsURLConnection {
        final ByteArrayOutputStream body=new ByteArrayOutputStream();Fake(URL u){super(u);}
        public void disconnect(){}public boolean usingProxy(){return false;}public void connect(){}
        public OutputStream getOutputStream(){return body;}public int getResponseCode(){return response;}
        public String getCipherSuite(){return "test";}public Certificate[] getLocalCertificates(){return null;}public Certificate[] getServerCertificates(){return null;}
        public Principal getPeerPrincipal(){return null;}public Principal getLocalPrincipal(){return null;}
    }
}
