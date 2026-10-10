package ru.blizko.chat;

import android.app.*;
import android.content.*;
import android.graphics.Bitmap;
import android.net.Uri;
import android.os.Bundle;
import android.view.*;
import android.widget.*;
import org.json.*;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.Base64;

/** Emulator regression checks for contact QR transport and APK verification. */
public final class QrAndUpdateTest extends ReceiveStartupTest {
    private Bundle arguments;
    @Override public void onCreate(Bundle args){arguments=args;super.onCreate(args);}
    private void check(boolean value,String reason){if(!value)throw new AssertionError(reason);}
    @Override public void onStart(){
        Bundle result=new Bundle();
        try{
            if(arguments!=null&&"true".equals(arguments.getString("stopOnly"))){
                getTargetContext().startService(new Intent(getTargetContext(),ChatService.class).setAction("STOP"));
                long until=System.currentTimeMillis()+10000;
                while(getTargetContext().getSharedPreferences("connection",Context.MODE_PRIVATE).getBoolean("receiveEnabled",false)&&System.currentTimeMillis()<until)Thread.sleep(100);
                check(!getTargetContext().getSharedPreferences("connection",Context.MODE_PRIVATE).getBoolean("receiveEnabled",false),"Explicit stop retained restart intention");
                ChatApp app=(ChatApp)getTargetContext().getApplicationContext();app.io.submit(()->{}).get(60,java.util.concurrent.TimeUnit.SECONDS);
                check(!new JSONObject(app.node.status()).getBoolean("enabled"),"Explicit stop left node enabled");
                result.putString("stream","OK: explicit stop disables receive and automatic restart");finish(Activity.RESULT_OK,result);return;
            }
            testReceiveStartsWithoutClosingActivity();testReceiveStartsWithoutClosingActivity();testQr();testPagedHistoryAndDraft();testConversationTools();testApk();testAutomaticUpdates();IrohDeliveryCheck.run(getTargetContext());
            result.putString("stream","OK: repeated startup, paged history and focused draft, QR, APK validation, persistent update scheduling and deduplicated notifications, real iroh automatic/relay delivery and offline queue");finish(Activity.RESULT_OK,result);
        }
        catch(Throwable failure){result.putString("stream","FAIL: "+failure.getClass().getSimpleName()+": "+failure.getMessage());finish(Activity.RESULT_CANCELED,result);}
    }
    private String contact()throws Exception{
        byte[] key=new byte[32];key[0]=9;JSONArray bytes=new JSONArray();for(byte b:key)bytes.put(b&255);
        JSONObject card=new JSONObject().put("id",AppUpdates.hex(MessageDigest.getInstance("SHA-256").digest(key)))
            .put("name","").put("key",bytes).put("address","0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef");
        return "blizko:3:"+Base64.getUrlEncoder().withoutPadding().encodeToString(card.toString().getBytes(StandardCharsets.UTF_8));
    }
    private void testAutomaticUpdates()throws Exception{
        Context context=getTargetContext();android.app.job.JobScheduler scheduler=context.getSystemService(android.app.job.JobScheduler.class);
        NotificationManager manager=context.getSystemService(NotificationManager.class);
        AutoUpdates.setEnabled(context,false);AutoUpdates.prefs(context).edit().clear().commit();AutoUpdates.schedule(context);
        android.app.job.JobInfo periodic=scheduler.getPendingJob(AutoUpdates.PERIODIC_JOB);
        check(periodic!=null&&periodic.isPersisted()&&periodic.isPeriodic()&&periodic.getIntervalMillis()==AutoUpdates.PERIOD,"Periodic check not persisted");
        check(periodic.getNetworkType()==android.app.job.JobInfo.NETWORK_TYPE_ANY,"Update job can run offline");
        check(scheduler.getPendingJob(AutoUpdates.STARTUP_JOB)!=null,"Startup check missing");
        scheduler.cancel(AutoUpdates.STARTUP_JOB);
        AutoUpdates.prefs(context).edit().putLong("lastAttempt",System.currentTimeMillis()).commit();AutoUpdates.schedule(context);
        check(scheduler.getPendingJob(AutoUpdates.STARTUP_JOB)==null,"Startup checks are not throttled");
        String hash=String.join("",java.util.Collections.nCopies(64,"a"));
        AppUpdates.Release next=new AppUpdates.Release(new JSONObject().put("versionCode",BuildConfig.VERSION_CODE+1).put("versionName","test-next")
            .put("sha256",hash).put("size",1).put("url","https://example.com/Blizko.apk").toString());
        AutoUpdates.record(context,next);
        android.service.notification.StatusBarNotification found=null;
        long until=System.currentTimeMillis()+3000;
        while(found==null&&System.currentTimeMillis()<until){
            for(android.service.notification.StatusBarNotification notification:manager.getActiveNotifications())if(notification.getId()==AutoUpdates.NOTIFICATION)found=notification;
            if(found==null)Thread.sleep(100);
        }
        check(found!=null&&found.getNotification().contentIntent!=null,"Update notification or install-screen intent missing");
        check(found.getNotification().getChannelId().equals(AutoUpdates.CHANNEL),"Update shares message notification channel");
        manager.cancel(AutoUpdates.NOTIFICATION);AutoUpdates.record(context,next);Thread.sleep(200);
        for(android.service.notification.StatusBarNotification notification:manager.getActiveNotifications())check(notification.getId()!=AutoUpdates.NOTIFICATION,"Dismissed update notified twice");
        AutoUpdates.setEnabled(context,false);
        check(scheduler.getPendingJob(AutoUpdates.PERIODIC_JOB)==null&&scheduler.getPendingJob(AutoUpdates.STARTUP_JOB)==null,"Opt-out retained jobs");
        AutoUpdates.prefs(context).edit().remove("notified").commit();AutoUpdates.record(context,next);
        for(android.service.notification.StatusBarNotification notification:manager.getActiveNotifications())check(notification.getId()!=AutoUpdates.NOTIFICATION,"Opt-out still notifies");
        check(AutoUpdates.available(context)!=null,"Notification opt-out removed in-app update info");
        AutoUpdates.prefs(context).edit().clear().commit();
        AppUpdates.Release installed=new AppUpdates.Release(new JSONObject(next.json).put("versionCode",BuildConfig.VERSION_CODE).toString());
        AutoUpdates.record(context,installed);check(AutoUpdates.available(context)==null,"Installed update still offered");
        AutoUpdates.schedule(context);
    }
    private void testQr()throws Exception{
        Context context=getTargetContext();String code=contact();Bitmap qr=ContactQr.image(code);
        check(code.equals(ContactQr.decode(qr)),"QR round trip failed");
        Uri uri=ContactQr.shareImage(context,qr);
        check(code.equals(ContactQr.readImage(context,uri)),"Shared QR could not be imported");
        try{ContactQr.requireContact("https://example.com");throw new AssertionError("External QR accepted");}catch(IllegalArgumentException expected){}
        Bitmap empty=Bitmap.createBitmap(256,256,Bitmap.Config.ARGB_8888);empty.eraseColor(0xFFFFFFFF);
        try{ContactQr.decode(empty);throw new AssertionError("Blank image accepted");}catch(IllegalArgumentException expected){}finally{empty.recycle();qr.recycle();}
        MainActivity activity=(MainActivity)startActivitySync(new Intent(context,MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
        ChatApp app=(ChatApp)activity.getApplication();long until=System.currentTimeMillis()+30000;
        while(app.node==null&&System.currentTimeMillis()<until)Thread.sleep(100);
        check(app.node!=null,"Core unavailable");
        app.io.submit(()->{}).get(60,java.util.concurrent.TimeUnit.SECONDS);
        JSONObject before=new JSONObject(app.node.snapshot());int count=before.getJSONArray("contacts").length();
        AlertDialog[] prompt=new AlertDialog[1];
        runOnMainSync(()->prompt[0]=activity.confirmQrContact(code));waitForIdleSync();
        // A contact must not be saved merely by scanning; user confirms its name.
        check(new JSONObject(app.node.snapshot()).getJSONArray("contacts").length()==count,"Scan bypassed confirmation");
        runOnMainSync(()->{
            check(countInputs(prompt[0].getWindow().getDecorView())==1,"Manual code input is still present");
            prompt[0].getButton(AlertDialog.BUTTON_NEGATIVE).performClick();
        });
        check(new JSONObject(app.node.snapshot()).getJSONArray("contacts").length()==count,"Cancel added contact");
        runOnMainSync(()->prompt[0]=activity.confirmQrContact(ContactQr.requireContact(code)));
        waitForIdleSync(); // Dialog.OnShowListener is delivered asynchronously before real user input.
        String contactName="QR regression "+System.currentTimeMillis();
        runOnMainSync(()->{
            findInput(prompt[0].getWindow().getDecorView()).setText(contactName);
            prompt[0].getButton(AlertDialog.BUTTON_POSITIVE).performClick();
        });
        until=System.currentTimeMillis()+30000;boolean saved=false;
        while(System.currentTimeMillis()<until){
            JSONArray contacts=new JSONObject(app.node.snapshot()).getJSONArray("contacts");
            for(int i=0;i<contacts.length();i++)if(contactName.equals(contacts.getJSONObject(i).getString("name")))saved=true;
            if(saved)break;Thread.sleep(100);
        }
        check(saved,"Confirmed QR contact was not saved");
        app.io.submit(()->{}).get(60,java.util.concurrent.TimeUnit.SECONDS);waitForIdleSync();
        try{ContactQr.requireContact("blizko:2:legacy");throw new AssertionError("Legacy QR accepted");}catch(IllegalArgumentException expected){}
        runOnMainSync(activity::finish);
    }
    private int countInputs(View view){
        int count=view instanceof EditText?1:0;
        if(view instanceof ViewGroup){ViewGroup group=(ViewGroup)view;for(int i=0;i<group.getChildCount();i++)count+=countInputs(group.getChildAt(i));}return count;
    }
    private View findText(View view,String value){
        if(view instanceof TextView&&value.contentEquals(((TextView)view).getText()))return view;
        if(view instanceof ViewGroup){ViewGroup group=(ViewGroup)view;for(int i=0;i<group.getChildCount();i++){View found=findText(group.getChildAt(i),value);if(found!=null)return found;}}return null;
    }
    private void testPagedHistoryAndDraft()throws Exception{
        MainActivity activity=(MainActivity)startActivitySync(new Intent(getTargetContext(),MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
        ChatApp app=(ChatApp)activity.getApplication();
        app.io.submit(()->{app.node.addContact("History regression",contact());return null;}).get(60,java.util.concurrent.TimeUnit.SECONDS);
        String id=new JSONObject(app.node.snapshot()).getJSONArray("contacts").getJSONObject(0).getString("id");
        app.io.submit(()->{for(int i=0;i<65;i++)app.node.send(id,"history "+i);return null;}).get(60,java.util.concurrent.TimeUnit.SECONDS);
        Thread.sleep(1800);waitForIdleSync();
        runOnMainSync(()->{View row=findText(activity.getWindow().getDecorView(),"History regression");check(row!=null,"History contact missing");((View)row.getParent()).performClick();});
        Thread.sleep(1500);waitForIdleSync();
        JSONObject latest=new JSONObject(app.node.snapshotPage(id,0,50));
        check(latest.getJSONArray("messages").length()==50&&latest.getBoolean("hasMore"),"History page is unbounded");
        runOnMainSync(()->{EditText editor=findInput(activity.getWindow().getDecorView());check(editor!=null,"Composer missing");editor.setText("draft retained");editor.requestFocus();editor.setSelection(5);});
        app.io.submit(()->{app.node.send(id,"history refresh");return null;}).get(60,java.util.concurrent.TimeUnit.SECONDS);
        Thread.sleep(1800);waitForIdleSync();
        runOnMainSync(()->{EditText editor=findInput(activity.getWindow().getDecorView());check("draft retained".contentEquals(editor.getText())&&editor.hasFocus()&&editor.getSelectionStart()==5,"Refresh lost focused draft");
            View older=findText(activity.getWindow().getDecorView(),"Раньше");check(older!=null&&older.isEnabled(),"Older history unavailable");older.performClick();});
        Thread.sleep(1500);waitForIdleSync();
        runOnMainSync(()->{check(findText(activity.getWindow().getDecorView(),"history 0")!=null,"Older page did not render");activity.finish();});
    }
    private EditText findInput(View view){
        if(view instanceof EditText)return (EditText)view;
        if(view instanceof ViewGroup){ViewGroup group=(ViewGroup)view;for(int i=0;i<group.getChildCount();i++){EditText found=findInput(group.getChildAt(i));if(found!=null)return found;}}return null;
    }
    private void testConversationTools()throws Exception{
        MainActivity activity=(MainActivity)startActivitySync(new Intent(getTargetContext(),MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));ChatApp app=(ChatApp)activity.getApplication();Thread.sleep(1500);waitForIdleSync();
        String id=new JSONObject(app.node.snapshot()).getJSONArray("contacts").getJSONObject(0).getString("id");
        runOnMainSync(()->{View row=findText(activity.getWindow().getDecorView(),"History regression");check(row!=null,"Contact missing after returning");((View)row.getParent()).performClick();});Thread.sleep(1500);waitForIdleSync();
        runOnMainSync(()->{EditText editor=findInput(activity.getWindow().getDecorView());check("draft retained".contentEquals(editor.getText()),"Switching activity lost saved draft");editor.setText(String.join("",java.util.Collections.nCopies(2100,"я")));check(editor.getText().toString().getBytes(StandardCharsets.UTF_8).length==4000,"Composer byte boundary wrong");editor.setText("persistent draft");activity.onBackPressed();});
        app.io.submit(()->{}).get(60,java.util.concurrent.TimeUnit.SECONDS);Thread.sleep(1500);waitForIdleSync();
        runOnMainSync(()->{View row=findText(activity.getWindow().getDecorView(),"History regression");check(row!=null,"Contact missing on home");((View)row.getParent()).performClick();});Thread.sleep(1500);waitForIdleSync();
        AlertDialog[] prompt=new AlertDialog[1];
        runOnMainSync(()->{check("persistent draft".contentEquals(findInput(activity.getWindow().getDecorView()).getText()),"Back lost per-contact draft");View find=findText(activity.getWindow().getDecorView(),"Найти");check(find!=null,"Conversation search missing");prompt[0]=activity.searchMessages();});waitForIdleSync();
        runOnMainSync(()->{findInput(prompt[0].getWindow().getDecorView()).setText("HISTORY 0");prompt[0].getButton(AlertDialog.BUTTON_POSITIVE).performClick();});Thread.sleep(1500);waitForIdleSync();
        runOnMainSync(()->{check(findText(activity.getWindow().getDecorView(),"history 0")!=null,"Search result not rendered");check(findText(activity.getWindow().getDecorView(),"history 64")==null,"Search displays unmatched recent rows");View reset=findText(activity.getWindow().getDecorView(),"Поиск: HISTORY 0 · Сбросить");check(reset!=null,"Search reset missing");reset.performClick();});Thread.sleep(1500);waitForIdleSync();
        JSONObject results=new JSONObject(app.node.searchPage(id,"HISTORY 0",0,50));check(results.getJSONArray("messages").length()==1,"Search does not inspect older pages");
        JSONArray messages=new JSONObject(app.node.snapshotPage(id,0,50)).getJSONArray("messages");String pending=messages.getJSONObject(messages.length()-1).getString("id");
        app.io.submit(()->{app.node.cancelMessage(id,pending);return null;}).get(60,java.util.concurrent.TimeUnit.SECONDS);
        check(new JSONObject(app.node.snapshotPage(id,0,50)).getJSONArray("messages").getJSONObject(messages.length()-1).getBoolean("cancelled"),"Cancel not shown in snapshot");
        app.io.submit(()->{app.node.retryMessage(id,pending);return null;}).get(60,java.util.concurrent.TimeUnit.SECONDS);
        runOnMainSync(activity::finish);
    }
    private AppUpdates.Release release(File file,int version,String hash)throws Exception{
        return new AppUpdates.Release(new JSONObject().put("versionCode",version).put("versionName","test")
            .put("url","https://example.com/test.apk").put("size",file.length()).put("sha256",hash).toString());
    }
    private String hash(File file)throws Exception{
        MessageDigest digest=MessageDigest.getInstance("SHA-256");
        try(InputStream input=new FileInputStream(file)){byte[] bytes=new byte[65536];int n;while((n=input.read(bytes))!=-1)digest.update(bytes,0,n);}
        return AppUpdates.hex(digest.digest());
    }
    private void testApk()throws Exception{
        Context context=getTargetContext();File apk=new File(context.getApplicationInfo().sourceDir);
        String hash=hash(apk);AppUpdates.verify(context,apk,release(apk,BuildConfig.VERSION_CODE,hash));
        try{AppUpdates.verify(context,apk,release(apk,BuildConfig.VERSION_CODE,String.join("",java.util.Collections.nCopies(64,"0"))));throw new AssertionError("Bad hash accepted");}catch(IOException expected){}
        try{AppUpdates.verify(context,apk,release(apk,BuildConfig.VERSION_CODE+1,hash));throw new AssertionError("Wrong version accepted");}catch(IOException expected){}
        try{AppUpdates.https("http://example.com/test.apk");throw new AssertionError("HTTP accepted");}catch(IOException expected){}
        File foreign=new File(getContext().getApplicationInfo().sourceDir);
        try{AppUpdates.verify(context,foreign,release(foreign,BuildConfig.VERSION_CODE,hash(foreign)));throw new AssertionError("Foreign package accepted");}catch(IOException expected){}
        if(arguments!=null&&arguments.containsKey("wrongSigner")){
            File wrongSigner=new File(arguments.getString("wrongSigner"));
            check(wrongSigner.isFile(),"Wrong signer fixture missing");
            try{AppUpdates.verify(context,wrongSigner,release(wrongSigner,BuildConfig.VERSION_CODE,hash(wrongSigner)));throw new AssertionError("Wrong signer accepted");}
            catch(IOException expected){check(expected.getMessage().contains("Подпись"),"Wrong signer fixture failed before signer check");}
        }
        if(arguments!=null&&arguments.containsKey("updateFeed")){
            AppUpdates.Release published=AppUpdates.check(arguments.getString("updateFeed"));
            File downloaded=AppUpdates.download(context,published,percent->{});
            AppUpdates.verify(context,downloaded,published);
        }
    }
}
