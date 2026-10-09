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
        try{testReceiveStartsWithoutClosingActivity();testReceiveStartsWithoutClosingActivity();testQr();testApk();result.putString("stream","OK: repeated receive startup, QR generation, image import, contact confirmation and APK checks");finish(Activity.RESULT_OK,result);}
        catch(Throwable failure){result.putString("stream","FAIL: "+failure.getClass().getSimpleName()+": "+failure.getMessage());finish(Activity.RESULT_CANCELED,result);}
    }
    private String contact()throws Exception{
        byte[] key=new byte[32];key[0]=9;JSONArray bytes=new JSONArray();for(byte b:key)bytes.put(b&255);
        JSONObject card=new JSONObject().put("id",AppUpdates.hex(MessageDigest.getInstance("SHA-256").digest(key)))
            .put("name","").put("key",bytes).put("address","100.100.100.100");
        return "blizko:2:"+Base64.getUrlEncoder().withoutPadding().encodeToString(card.toString().getBytes(StandardCharsets.UTF_8));
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
        String invitation="https://login.tailscale.com/admin/invite/blizko-test-fixture";
        String invited=contactWithInvitation(invitation);
        Bitmap combined=ContactQr.image(invited);
        check(invited.equals(ContactQr.decode(combined)),"Combined contact/invitation QR cannot be read");combined.recycle();
        check(invitation.equals(mobile.Mobile.contactInvitation(invited)),"Invitation lost at JNI boundary");
        try{mobile.Mobile.contactInvitation(contactWithInvitation("https://evil.example/admin/invite/token"));throw new AssertionError("Untrusted invitation accepted");}catch(Exception expected){}
        IntentFilter filter=new IntentFilter(Intent.ACTION_VIEW);filter.addDataScheme("https");filter.addDataAuthority("login.tailscale.com",null);
        // Intercept the browser launch: no real invitation/account is accepted by a test.
        ActivityMonitor browser=addMonitor(filter,new ActivityResult(Activity.RESULT_CANCELED,null),true);
        try{
            runOnMainSync(()->prompt[0]=activity.confirmQrContact(invited));waitForIdleSync();
            check(browser.getHits()==0,"Scanning opened invitation without confirmation");
            String invitedName="Invited QR "+System.currentTimeMillis();
            runOnMainSync(()->{
                check(countInputs(prompt[0].getWindow().getDecorView())==1,"Combined QR exposes manual contact-code input");
                check("Добавить и подключить".contentEquals(prompt[0].getButton(AlertDialog.BUTTON_POSITIVE).getText()),"Invitation consent label missing");
                findInput(prompt[0].getWindow().getDecorView()).setText(invitedName);
                prompt[0].getButton(AlertDialog.BUTTON_POSITIVE).performClick();
            });
            app.io.submit(()->{}).get(60,java.util.concurrent.TimeUnit.SECONDS);waitForIdleSync();
            check(browser.getHits()==1,"Confirmed invitation did not open browser exactly once");
            JSONObject snapshot=new JSONObject(app.node.snapshot());
            JSONArray updated=snapshot.getJSONArray("contacts");boolean updatedContact=false;
            for(int i=0;i<updated.length();i++){
                JSONObject c=updated.getJSONObject(i);
                if(invitedName.equals(c.getString("name"))){updatedContact=true;check(!c.has("invite"),"Received invitation retained in contact");}
            }
            check(updatedContact,"Combined QR failed to save contact");
        }finally{removeMonitor(browser);}
        runOnMainSync(activity::finish);
    }
    private String contactWithInvitation(String invitation)throws Exception{
        String raw=contact().substring("blizko:2:".length());
        JSONObject card=new JSONObject(new String(Base64.getUrlDecoder().decode(raw),StandardCharsets.UTF_8));
        card.put("invite",invitation).put("dns","blizko-fixture.example.ts.net");
        return "blizko:2:"+Base64.getUrlEncoder().withoutPadding().encodeToString(card.toString().getBytes(StandardCharsets.UTF_8));
    }
    private int countInputs(View view){
        int count=view instanceof EditText?1:0;
        if(view instanceof ViewGroup){ViewGroup group=(ViewGroup)view;for(int i=0;i<group.getChildCount();i++)count+=countInputs(group.getChildAt(i));}return count;
    }
    private EditText findInput(View view){
        if(view instanceof EditText)return (EditText)view;
        if(view instanceof ViewGroup){ViewGroup group=(ViewGroup)view;for(int i=0;i<group.getChildCount();i++){EditText found=findInput(group.getChildAt(i));if(found!=null)return found;}}return null;
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
