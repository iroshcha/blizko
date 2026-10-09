package ru.blizko.chat;

import android.Manifest;
import android.app.*;
import android.content.*;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.Bitmap;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.net.Uri;
import android.os.*;
import android.view.*;
import android.widget.*;
import org.json.*;
import java.text.SimpleDateFormat;
import java.util.*;
import com.google.zxing.integration.android.IntentIntegrator;
import com.google.zxing.integration.android.IntentResult;

public final class MainActivity extends Activity {
    private static final int BG=0xFFF5F6F2,INK=0xFF1C302B,GREEN=0xFF236C58,MUTED=0xFF65766F;
    private ChatApp app;
    private LinearLayout root;
    private String peer="",last="",draft="";
    private EditText compose;
    private static final int PICK_QR_IMAGE=42;
    private final Handler handler=new Handler(Looper.getMainLooper());
    private final Runnable poll=new Runnable(){public void run(){
        String snapshot=app.node==null?"":app.node.snapshot();
        if(!snapshot.equals(last)||root==null){last=snapshot;render(snapshot);}
        if(app.error!=null){String e=app.error;app.error=null;notice(e);}
        handler.postDelayed(this,1000);
    }};
    @Override public void onCreate(Bundle state){super.onCreate(state);app=(ChatApp)getApplication();getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);if(state!=null){peer=state.getString("peer","");draft=state.getString("draft","");}}
    @Override protected void onResume(){super.onResume();app.activeScreen=true;handler.post(poll);}
    @Override protected void onPause(){super.onPause();app.activeScreen=false;handler.removeCallbacks(poll);}
    @Override protected void onSaveInstanceState(Bundle out){super.onSaveInstanceState(out);out.putString("peer",peer);out.putString("draft",compose==null?draft:compose.getText().toString());}
    @Override public void onBackPressed(){if(!peer.isEmpty()){peer="";draft="";compose=null;render(last);}else super.onBackPressed();}
    private int dp(int n){return Math.round(n*getResources().getDisplayMetrics().density);}
    private GradientDrawable bg(int color,int radius){GradientDrawable g=new GradientDrawable();g.setColor(color);g.setCornerRadius(dp(radius));return g;}
    private LinearLayout column(){LinearLayout l=new LinearLayout(this);l.setOrientation(LinearLayout.VERTICAL);return l;}
    private TextView text(String value,int size,int color){TextView t=new TextView(this);t.setText(value);t.setTextSize(size);t.setTextColor(color);t.setPadding(0,dp(4),0,dp(4));return t;}
    private Button button(String value,Runnable action){Button b=new Button(this);b.setText(value);b.setAllCaps(false);b.setTextColor(GREEN);b.setTextSize(15);b.setOnClickListener(v->action.run());return b;}
    private void primary(Button b){b.setTextColor(Color.WHITE);b.setBackground(bg(GREEN,14));LinearLayout.LayoutParams p=new LinearLayout.LayoutParams(-1,dp(52));p.setMargins(0,dp(8),0,dp(8));b.setLayoutParams(p);}
    private void gap(LinearLayout l,int h){View v=new View(this);l.addView(v,new LinearLayout.LayoutParams(1,dp(h)));}
    private EditText input(String hint){EditText e=new EditText(this);e.setHint(hint);e.setTextColor(INK);e.setTextSize(16);e.setPadding(dp(12),dp(10),dp(12),dp(10));return e;}
    private void notice(String value){new AlertDialog.Builder(this).setTitle("Близко").setMessage(value).setPositiveButton("Понятно",null).show();}
    interface Task{void run()throws Exception;}
    private void work(Task task){app.io.execute(()->{try{task.run();runOnUiThread(()->{last="";});}catch(Exception e){runOnUiThread(()->notice(e.getMessage()==null?"Не удалось выполнить действие":e.getMessage()));}});}
    private void render(String raw){
        if(compose!=null)draft=compose.getText().toString();compose=null;
        root=column();root.setBackgroundColor(BG);root.setPadding(dp(22),dp(14),dp(22),dp(12));
        root.setOnApplyWindowInsetsListener((v,i)->{root.setPadding(dp(22),dp(14)+i.getSystemWindowInsetTop(),dp(22),dp(12)+i.getSystemWindowInsetBottom());return i;});setContentView(root);
        try{
            JSONObject s=raw.isEmpty()?new JSONObject():new JSONObject(raw);
            LinearLayout head=new LinearLayout(this);head.setGravity(Gravity.CENTER_VERTICAL);
            TextView title=text(peer.isEmpty()?"Близко":"‹  "+peerName(s,peer),30,INK);title.setTypeface(null,Typeface.BOLD);head.addView(title,new LinearLayout.LayoutParams(0,-2,1));
            if(!peer.isEmpty())title.setOnClickListener(v->onBackPressed());
            head.addView(button("ⓘ",()->notice("История и очередь сообщений хранятся только на телефонах.\n\niroh встроен: аккаунт и отдельный VPN не нужны. При невозможности прямой связи используются публичные ретрансляторы зашифрованных данных. Бесплатная инфраструктура предназначена для экспериментов и личных проектов.\n\nОба телефона должны быть в сети с включённым приёмом. Удаление приложения удалит историю.")));root.addView(head);
            root.addView(text("Личное остаётся у вас",14,MUTED));gap(root,14);
            if(app.node==null){root.addView(text("Открываем защищённое хранилище…",16,MUTED));return;}
            LinearLayout card=column();card.setPadding(dp(14),dp(10),dp(14),dp(10));card.setBackground(bg(Color.WHITE,16));
            card.addView(text((s.optBoolean("online")?"●  ":"○  ")+s.optString("status","Подготовка…"),14,GREEN));
            if(peer.isEmpty()){
                boolean enabled=s.optBoolean("enabled");card.addView(button(enabled?"Выключить приём":"Включить приём",()->{
                    if(enabled)stopService(new Intent(this,ChatService.class));else{
                        if(Build.VERSION.SDK_INT>=33&&checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)!=PackageManager.PERMISSION_GRANTED)requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS},11);
                        startForegroundService(new Intent(this,ChatService.class));
                    }
                }));
                card.addView(button("Настройки соединения",this::networkSetup));
            }
            root.addView(card);gap(root,12);
            if(peer.isEmpty())home(s);else conversation(s);
        }catch(Exception e){root.addView(text("Не удалось отобразить чат",16,MUTED));}
    }
    private String peerName(JSONObject s,String id){JSONArray a=s.optJSONArray("contacts");if(a!=null)for(int i=0;i<a.length();i++){JSONObject c=a.optJSONObject(i);if(c!=null&&id.equals(c.optString("id")))return c.optString("name");}return "Чат";}
    private void home(JSONObject s)throws Exception{
        LinearLayout actions=new LinearLayout(this);
        actions.addView(button("Мой QR",this::shareCode),new LinearLayout.LayoutParams(0,dp(50),1));
        actions.addView(button("+ Контакт",this::addContact),new LinearLayout.LayoutParams(0,dp(50),1));root.addView(actions);gap(root,12);
        root.addView(button("Обновления · "+BuildConfig.VERSION_NAME,()->startActivity(new Intent(this,UpdateActivity.class))));
        TextView label=text("ПЕРЕПИСКИ",12,MUTED);label.setLetterSpacing(.13f);root.addView(label);
        ScrollView scroll=new ScrollView(this);LinearLayout list=column();scroll.addView(list);root.addView(scroll,new LinearLayout.LayoutParams(-1,0,1));
        JSONArray contacts=s.optJSONArray("contacts"),messages=s.optJSONArray("messages");
        if(contacts==null||contacts.length()==0){gap(list,40);list.addView(text("Ваш первый разговор",25,INK));list.addView(text("Включите приём, отправьте другу свой QR и добавьте его QR. Вход в аккаунт не нужен. Если обновились со старой версии, обменяйтесь новыми QR на обоих телефонах.",16,MUTED));return;}
        for(int i=0;i<contacts.length();i++){
            JSONObject c=contacts.getJSONObject(i);String id=c.getString("id");LinearLayout row=column();row.setPadding(dp(16),dp(12),dp(16),dp(12));row.setBackground(bg(Color.WHITE,16));
            TextView name=text(c.getString("name"),19,INK);name.setTypeface(null,Typeface.BOLD);row.addView(name);
            String preview=c.optString("address").isEmpty()?"Нужен новый QR собеседника":"Начать разговор";if(messages!=null)for(int j=messages.length()-1;j>=0;j--){JSONObject m=messages.getJSONObject(j);if(id.equals(m.getString("peer"))){preview=m.getString("text");break;}}
            TextView snippet=text(preview,14,MUTED);snippet.setMaxLines(1);snippet.setEllipsize(android.text.TextUtils.TruncateAt.END);row.addView(snippet);
            row.setOnClickListener(v->{peer=id;draft="";render(last);});list.addView(row);gap(list,8);
        }
    }
    private void conversation(JSONObject s)throws Exception{
        root.addView(button("Проверить связь с собеседником",()->{String target=peer;work(()->{String result=app.node.checkContact(target);runOnUiThread(()->notice(result));});}));
        JSONObject issues=s.optJSONObject("deliveryIssues");String issue=issues==null?"":issues.optString(peer);
        if(!issue.isEmpty())root.addView(text(issue,13,MUTED));
        ScrollView scroll=new ScrollView(this);LinearLayout bubbles=column();scroll.addView(bubbles);root.addView(scroll,new LinearLayout.LayoutParams(-1,0,1));
        JSONArray messages=s.optJSONArray("messages");int count=0;
        if(messages!=null)for(int i=0;i<messages.length();i++){
            JSONObject m=messages.getJSONObject(i);if(!peer.equals(m.getString("peer")))continue;count++;
            boolean out=m.getBoolean("out");LinearLayout bubble=column();bubble.setPadding(dp(14),dp(9),dp(14),dp(9));bubble.setBackground(bg(out?0xFFDCEEE4:Color.WHITE,16));
            bubble.addView(text(m.getString("text"),16,INK));
            String meta=new SimpleDateFormat("HH:mm",Locale.getDefault()).format(new Date(m.getLong("time")));
            if(out)meta+=" · "+(m.getBoolean("delivered")?"Доставлено":"В очереди · не доставлено");bubble.addView(text(meta,11,MUTED));
            LinearLayout.LayoutParams p=new LinearLayout.LayoutParams(-2,-2);p.gravity=out?Gravity.END:Gravity.START;p.setMargins(out?dp(26):0,dp(4),out?0:dp(26),dp(4));bubbles.addView(bubble,p);
        }
        if(count==0)bubbles.addView(text("Сообщения видны только вам и собеседнику. Добавьте QR-коды контактов на обоих телефонах.",15,MUTED));
        scroll.post(()->scroll.fullScroll(View.FOCUS_DOWN));gap(root,8);
        LinearLayout line=new LinearLayout(this);line.setGravity(Gravity.BOTTOM);compose=input("Сообщение…");compose.setText(draft);compose.setMaxLines(4);compose.setBackground(bg(Color.WHITE,16));line.addView(compose,new LinearLayout.LayoutParams(0,-2,1));
        line.addView(button("↑",()->{String content=compose.getText().toString();String target=peer;work(()->{app.node.send(target,content);runOnUiThread(()->{draft="";if(compose!=null)compose.setText("");});});}));root.addView(line);
    }
    private void shareCode(){work(()->{
        String code=app.node.myCode();
        Bitmap bitmap=ContactQr.image(code);
        Uri imageUri=ContactQr.shareImage(this,bitmap);
        runOnUiThread(()->{
            LinearLayout content=column();content.setPadding(dp(12),dp(8),dp(12),dp(8));
            ImageView image=new ImageView(this);image.setImageBitmap(bitmap);image.setAdjustViewBounds(true);
            image.setContentDescription("QR-код моего контакта");image.setScaleType(ImageView.ScaleType.FIT_CENTER);
            content.addView(image,new LinearLayout.LayoutParams(-1,Math.min(dp(300),getResources().getDisplayMetrics().widthPixels-dp(80))));
            content.addView(text("Отправьте этот QR другу и добавьте его QR у себя. Код содержит открытые ключи контакта; пароль и регистрация не нужны.",14,MUTED));
            new AlertDialog.Builder(this).setTitle("Мой QR-код").setView(content)
                .setPositiveButton("Поделиться QR",(d,w)->{
                    Intent send=new Intent(Intent.ACTION_SEND).setType("image/png").putExtra(Intent.EXTRA_STREAM,imageUri).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
                    send.setClipData(ClipData.newUri(getContentResolver(),"QR контакта",imageUri));
                    startActivity(Intent.createChooser(send,"QR контакта"));
                }).setNegativeButton("Закрыть",null).show();
        });
    });}
    private void networkSetup(){
        try {
            boolean relay=new JSONObject(app.node.snapshot()).optBoolean("relayOnly");
            new AlertDialog.Builder(this).setTitle("Соединение iroh")
                .setSingleChoiceItems(new String[]{"Автоматически: напрямую или через ретранслятор","Только через ретранслятор (проверка)"},relay?1:0,(dialog,which)->{
                    work(()->app.node.setRelayOnly(which==1));dialog.dismiss();
                }).setNegativeButton("Закрыть",null).show();
        }catch(Exception e){notice("Хранилище ещё открывается");}
    }
    private void addContact(){new AlertDialog.Builder(this).setTitle("Добавить по QR")
        .setItems(new String[]{"Сканировать камерой","Выбрать QR из фото"},(dialog,which)->{
            if(which==0)new IntentIntegrator(this).setCaptureActivity(QrScanActivity.class).setDesiredBarcodeFormats(IntentIntegrator.QR_CODE)
                .setPrompt("Наведите камеру на QR контакта «Близко»").setOrientationLocked(false).setBeepEnabled(false).setBarcodeImageEnabled(false).initiateScan();
            else startActivityForResult(new Intent(Intent.ACTION_OPEN_DOCUMENT).setType("image/*").addCategory(Intent.CATEGORY_OPENABLE),PICK_QR_IMAGE);
        }).setNegativeButton("Отмена",null).show();}
    @Override protected void onActivityResult(int request,int result,Intent data){
        super.onActivityResult(request,result,data);
        if(request==PICK_QR_IMAGE){
            if(result==RESULT_OK&&data!=null&&data.getData()!=null){Uri uri=data.getData();work(()->{String code=ContactQr.readImage(this,uri);runOnUiThread(()->confirmQrContact(code));});}
            return;
        }
        IntentResult scan=IntentIntegrator.parseActivityResult(request,result,data);
        if(scan!=null&&scan.getContents()!=null)confirmQrContact(scan.getContents());
        else if(scan!=null&&checkSelfPermission(Manifest.permission.CAMERA)!=PackageManager.PERMISSION_GRANTED)
            notice("Для сканирования разрешите доступ к камере в настройках приложения или выберите QR из фото.");
    }
    AlertDialog confirmQrContact(String scanned){
        final String code;
        try{code=ContactQr.requireContact(scanned);}catch(Exception e){notice(e.getMessage());return null;}
        LinearLayout fields=column();fields.setPadding(dp(20),dp(6),dp(20),dp(6));
        fields.addView(text("QR считан. Как назвать собеседника?",15,MUTED));

        EditText name=input("Имя собеседника");fields.addView(name);
        AlertDialog dialog=new AlertDialog.Builder(this).setTitle("Контакт из QR").setView(fields).setPositiveButton("Добавить",null).setNegativeButton("Отмена",null).create();
        dialog.setOnShowListener(v->dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(b->{String n=name.getText().toString();work(()->{app.node.addContact(n,code);runOnUiThread(()->{dialog.dismiss();notice("Контакт сохранён. Другу нужно добавить ваш QR тоже. Включите приём на обоих телефонах.");});});}));dialog.show();return dialog;
    }
}
