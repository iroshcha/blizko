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
    private TextView connectionStatus;
    private ScrollView historyScroll;
    private long before=0,pageRevision=-1;
    private boolean active=false,polling=false;
    private String contentKey="",renderedPage="";
    private static final int PICK_QR_IMAGE=42;
    private final Handler handler=new Handler(Looper.getMainLooper());
    private final Runnable poll=new Runnable(){public void run(){
        if(app.node==null){if(root==null)render("");}
        else if(!polling){
            polling=true;String target=peer;long cursor=before,known=pageRevision;
            app.io.execute(()->{
                String raw=null;
                try{JSONObject status=new JSONObject(app.node.status());
                    if(status.optLong("revision")!=known)raw=app.node.snapshotPage(target,cursor,50);
                }catch(Exception ignored){}
                String result=raw;
                handler.post(()->{
                    polling=false;
                    if(!active||!target.equals(peer)||cursor!=before)return;
                    if(result!=null)try{
                        JSONObject s=new JSONObject(result);last=result;pageRevision=s.optLong("revision");
                        String key=target+":"+cursor+":"+s.optBoolean("enabled")+":"+s.opt("contacts")+":"+s.opt("messages")+":"+s.opt("deliveryIssues")+":"+s.opt("previews");
                        if(root==null||!key.equals(contentKey)){contentKey=key;render(result);}
                        if(connectionStatus!=null)connectionStatus.setText((s.optBoolean("online")?"●  ":"○  ")+s.optString("status"));
                    }catch(Exception ignored){}
                });
            });
        }
        if(app.error!=null){String e=app.error;app.error=null;notice(e);}
        handler.postDelayed(this,1000);
    }};
    @Override public void onCreate(Bundle state){super.onCreate(state);app=(ChatApp)getApplication();getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);if(state!=null){peer=state.getString("peer","");draft=state.getString("draft","");before=state.getLong("before",0);}}
    @Override protected void onResume(){super.onResume();active=true;app.activeScreen=true;handler.removeCallbacks(poll);handler.post(poll);}
    @Override protected void onPause(){super.onPause();active=false;app.activeScreen=false;handler.removeCallbacks(poll);}
    @Override protected void onSaveInstanceState(Bundle out){super.onSaveInstanceState(out);out.putString("peer",peer);out.putString("draft",compose==null?draft:compose.getText().toString());out.putLong("before",before);}
    @Override public void onBackPressed(){if(!peer.isEmpty()){peer="";before=0;draft="";compose=null;refreshPage();}else super.onBackPressed();}
    private void refreshPage(){pageRevision=-1;handler.removeCallbacks(poll);if(active)handler.post(poll);}
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
    private void work(Task task){app.io.execute(()->{try{task.run();runOnUiThread(this::refreshPage);}catch(Exception e){runOnUiThread(()->notice(e.getMessage()==null?"Не удалось выполнить действие":e.getMessage()));}});}
    private void render(String raw){
        String page=peer+":"+before;boolean samePage=page.equals(renderedPage);renderedPage=page;
        boolean focused=samePage&&compose!=null&&compose.hasFocus();int selection=compose==null?0:compose.getSelectionStart();
        int offset=samePage&&historyScroll!=null?historyScroll.getScrollY():0;
        boolean atEnd=!samePage||historyScroll==null||historyScroll.getChildCount()==0||historyScroll.getChildAt(0).getHeight()-offset-historyScroll.getHeight()<dp(48);
        if(compose!=null)draft=compose.getText().toString();compose=null;
        historyScroll=null;connectionStatus=null;
        root=column();root.setBackgroundColor(BG);root.setPadding(dp(22),dp(14),dp(22),dp(12));
        root.setOnApplyWindowInsetsListener((v,i)->{root.setPadding(dp(22),dp(14)+i.getSystemWindowInsetTop(),dp(22),dp(12)+i.getSystemWindowInsetBottom());return i;});setContentView(root);
        try{
            JSONObject s=raw.isEmpty()?new JSONObject():new JSONObject(raw);
            LinearLayout head=new LinearLayout(this);head.setGravity(Gravity.CENTER_VERTICAL);
            TextView title=text(peer.isEmpty()?"Близко":"‹  "+peerName(s,peer),30,INK);title.setTypeface(null,Typeface.BOLD);head.addView(title,new LinearLayout.LayoutParams(0,-2,1));
            if(!peer.isEmpty())title.setOnClickListener(v->onBackPressed());
            head.addView(button("ⓘ",()->notice("История и очередь сообщений хранятся только на телефонах.\n\niroh встроен: аккаунт и отдельный VPN не нужны. Если прямое соединение невозможно, зашифрованные данные проходят через ваш домашний сервер. Он должен быть включён и подключён к интернету.\n\nОба телефона должны быть в сети с включённым приёмом. Удаление приложения удалит историю.")));root.addView(head);
            root.addView(text("Личное остаётся у вас",14,MUTED));gap(root,14);
            if(app.node==null){root.addView(text("Открываем защищённое хранилище…",16,MUTED));return;}
            LinearLayout card=column();card.setPadding(dp(14),dp(10),dp(14),dp(10));card.setBackground(bg(Color.WHITE,16));
            connectionStatus=text((s.optBoolean("online")?"●  ":"○  ")+s.optString("status","Подготовка…"),14,GREEN);card.addView(connectionStatus);
            if(peer.isEmpty()){
                boolean enabled=s.optBoolean("enabled");card.addView(button(enabled?"Выключить приём":"Включить приём",()->{
                    if(enabled)startService(new Intent(this,ChatService.class).setAction("STOP"));else{
                        if(Build.VERSION.SDK_INT>=33&&checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)!=PackageManager.PERMISSION_GRANTED)requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS},11);
                        startForegroundService(new Intent(this,ChatService.class));
                    }
                }));
                card.addView(button("Настройки соединения",this::networkSetup));
            }
            root.addView(card);gap(root,12);
            if(peer.isEmpty())home(s);else conversation(s);
            if(compose!=null&&focused){compose.requestFocus();compose.setSelection(Math.min(Math.max(selection,0),compose.length()));}
            if(historyScroll!=null){ScrollView current=historyScroll;current.post(()->{if(before==0&&atEnd)current.fullScroll(View.FOCUS_DOWN);else current.scrollTo(0,offset);});}
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
        JSONArray contacts=s.optJSONArray("contacts");JSONObject previews=s.optJSONObject("previews");
        if(contacts==null||contacts.length()==0){gap(list,40);list.addView(text("Ваш первый разговор",25,INK));list.addView(text("Включите приём, отправьте другу свой QR и добавьте его QR. Вход в аккаунт не нужен. Если обновились со старой версии, обменяйтесь новыми QR на обоих телефонах.",16,MUTED));return;}
        for(int i=0;i<contacts.length();i++){
            JSONObject c=contacts.getJSONObject(i);String id=c.getString("id");LinearLayout row=column();row.setPadding(dp(16),dp(12),dp(16),dp(12));row.setBackground(bg(Color.WHITE,16));
            TextView name=text(c.getString("name"),19,INK);name.setTypeface(null,Typeface.BOLD);row.addView(name);
            String preview=c.optString("address").isEmpty()?"Нужен новый QR собеседника":"Начать разговор";if(previews!=null)preview=previews.optString(id,preview);
            TextView snippet=text(preview,14,MUTED);snippet.setMaxLines(1);snippet.setEllipsize(android.text.TextUtils.TruncateAt.END);row.addView(snippet);
            row.setOnClickListener(v->{peer=id;before=0;draft="";compose=null;refreshPage();});list.addView(row);gap(list,8);
        }
    }
    private void conversation(JSONObject s)throws Exception{
        LinearLayout pages=new LinearLayout(this);
        Button older=button("Раньше",()->{JSONArray rows=s.optJSONArray("messages");if(rows!=null&&rows.length()>0){before=rows.optJSONObject(0).optLong("order");refreshPage();}});older.setEnabled(s.optBoolean("hasMore"));pages.addView(older,new LinearLayout.LayoutParams(0,-2,1));
        Button recent=button("Последние",()->{before=0;refreshPage();});recent.setEnabled(before>0);pages.addView(recent,new LinearLayout.LayoutParams(0,-2,1));
        pages.addView(button("Очистить",()->new AlertDialog.Builder(this).setTitle("Очистить историю?").setMessage("Будут удалены тексты доставленных сообщений на этом устройстве. Ожидающие отправки сообщения сохранятся.").setNegativeButton("Отмена",null).setPositiveButton("Очистить",(d,w)->{String target=peer;work(()->app.node.clearHistory(target));}).show()),new LinearLayout.LayoutParams(0,-2,1));root.addView(pages);
        root.addView(button("Проверить связь с собеседником",()->{String target=peer;work(()->{String result=app.node.checkContact(target);runOnUiThread(()->notice(result));});}));
        JSONObject issues=s.optJSONObject("deliveryIssues");String issue=issues==null?"":issues.optString(peer);
        if(!issue.isEmpty())root.addView(text(issue,13,MUTED));
        ScrollView scroll=new ScrollView(this);LinearLayout bubbles=column();scroll.addView(bubbles);root.addView(scroll,new LinearLayout.LayoutParams(-1,0,1));
        historyScroll=scroll;
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
        gap(root,8);
        LinearLayout line=new LinearLayout(this);line.setGravity(Gravity.BOTTOM);compose=input("Сообщение…");compose.setText(draft);compose.setMaxLines(4);compose.setBackground(bg(Color.WHITE,16));line.addView(compose,new LinearLayout.LayoutParams(0,-2,1));
        line.addView(button("↑",()->{String content=compose.getText().toString();String target=peer;work(()->{app.node.send(target,content);runOnUiThread(()->{if(target.equals(peer)&&compose!=null&&content.contentEquals(compose.getText())){draft="";compose.setText("");}});});}));root.addView(line);
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
            boolean relay=new JSONObject(last).optBoolean("relayOnly");
            new AlertDialog.Builder(this).setTitle("Соединение iroh")
                .setSingleChoiceItems(new String[]{"Автоматически: напрямую или через ретранслятор","Только через ретранслятор (проверка)"},relay?1:0,(dialog,which)->{
                    work(()->app.node.setRelayOnly(which==1));dialog.dismiss();
                }).setNeutralButton("Адрес сервера",(dialog,which)->{
                    EditText address=input("https://…");try{address.setText(new JSONObject(last).optString("relayURL"));}catch(Exception ignored){}
                    AlertDialog edit=new AlertDialog.Builder(this).setTitle("Домашний сервер").setMessage("Укажите одинаковый HTTPS-адрес на всех устройствах. Пустое поле вернёт исходный адрес.").setView(address).setNegativeButton("Отмена",null).setPositiveButton("Сохранить",null).create();
                    edit.setOnShowListener(v->edit.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(b->{String value=address.getText().toString();work(()->{app.node.setRelayURL(value);runOnUiThread(edit::dismiss);});}));edit.show();
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
