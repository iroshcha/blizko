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
        try{testReceiveStartsWithoutClosingActivity();testReceiveStartsWithoutClosingActivity();testQr();testPagedHistoryAndDraft();testApk();IrohDeliveryCheck.run(getTargetContext());result.putString("stream","OK: repeated startup, paged history and focused draft, QR, APK validation, real iroh automatic/relay delivery and offline queue");finish(Activity.RESULT_OK,result);}
        catch(Throwable failure){result.putString("stream","FAIL: "+failure.getClass().getSimpleName()+": "+failure.getMessage());finish(Activity.RESULT_CANCELED,result);}
    }
    private String contact()throws Exception{
        byte[] key=new byte[32];key[0]=9;JSONArray bytes=new JSONArray();for(byte b:key)bytes.put(b&255);
        JSONObject card=new JSONObject().put("id",AppUpdates.hex(MessageDigest.getInstance("SHA-256").digest(key)))
            .put("name","").put("key",bytes).put("address","0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef");
        return "blizko:3:"+Base64.getUrlEncoder().withoutPadding().encodeToString(card.toString().getBytes(StandardCharsets.UTF_8));
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
