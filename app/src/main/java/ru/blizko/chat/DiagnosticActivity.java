package ru.blizko.chat;

import android.app.*;
import android.app.job.*;
import android.content.*;
import android.os.*;
import android.text.InputType;
import android.view.View;
import android.widget.*;

public final class DiagnosticActivity extends Activity {
    private TextView status;
    private final Handler main=new Handler(Looper.getMainLooper());
    private final Runnable refresh=new Runnable(){public void run(){
        long last=Diagnostics.prefs(DiagnosticActivity.this).getLong("lastSent",0);
        status.setText((Diagnostics.enabled(DiagnosticActivity.this)?"Диагностика включена\nКод устройства: "+Diagnostics.prefs(DiagnosticActivity.this).getString("device",""):"Диагностика выключена")+"\n"+Diagnostics.prefs(DiagnosticActivity.this).getString("status","")+(last>0?"\nОтправлено: "+android.text.format.DateFormat.format("dd.MM HH:mm",last):""));main.postDelayed(this,2000);
    }};
    @Override public void onCreate(Bundle state){super.onCreate(state);show();}
    @Override public void onResume(){super.onResume();main.post(refresh);}
    @Override public void onPause(){main.removeCallbacks(refresh);super.onPause();}
    private void show(){
        ScrollView scroll=new ScrollView(this);LinearLayout list=new LinearLayout(this);list.setOrientation(LinearLayout.VERTICAL);int pad=(int)(20*getResources().getDisplayMetrics().density);list.setPadding(pad,pad,pad,pad);scroll.addView(list);setContentView(scroll);
        TextView title=new TextView(this);title.setText("Диагностика фоновой работы");title.setTextSize(24);title.setTextColor(0xFF1C302B);list.addView(title);
        TextView info=new TextView(this);info.setText("Записывает сон, заряд и состояние связи. Отправляет технический журнал на ваш сервер. Содержимое переписки не собирается.\n");info.setTextSize(16);list.addView(info);
        String details="Помогает проверить приём сообщений при выключенном экране. Записывает время событий, заряд, сон и экономию батареи, тип сети, состояние приёма, факт поступления/постановки сообщений в очередь без содержимого и доступные Android причины завершения процесса.\n\nТексты сообщений, контакты, адреса устройств и ключи переписки не собираются. Случайный код связывает события одного тестового устройства.\n\nЖурнал хранится до 7 дней: до 512 событий на телефоне, до 32 МиБ на вашем сервере (очистка каждый час). Android сам выбирает время фоновой отправки; во сне она может откладываться. Отсутствие событий само по себе не доказывает причину остановки.\n";
        button(list,"Какие данные собираются",()->new AlertDialog.Builder(this).setTitle("Технический журнал").setMessage(details).setPositiveButton("Понятно",null).show());
        status=new TextView(this);list.addView(status);
        if(!Diagnostics.enabled(this)){
            EditText url=new EditText(this);url.setSingleLine(true);url.setInputType(InputType.TYPE_CLASS_TEXT|InputType.TYPE_TEXT_VARIATION_URI);url.setText(Diagnostics.DEFAULT_URL);url.setHint("HTTPS-адрес приёмника журнала");list.addView(url);
            EditText token=new EditText(this);token.setSingleLine(true);token.setInputType(InputType.TYPE_CLASS_TEXT|InputType.TYPE_TEXT_VARIATION_PASSWORD);token.setHint("Код доступа с сервера");token.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);list.addView(token);
            button(list,"Включить и отправлять на мой сервер",()->{try{Diagnostics.enable(this,url.getText().toString().trim(),token.getText().toString().trim());show();}catch(Exception e){Toast.makeText(this,e.getMessage(),Toast.LENGTH_LONG).show();}});
        }else{
            button(list,"Отметить начало теста",()->{Diagnostics.record(this,"manual");Toast.makeText(this,"Отметка сохранена",Toast.LENGTH_SHORT).show();});
            button(list,"Отправить накопленные записи",()->{Diagnostics.sendSoon(this);Toast.makeText(this,"Отправка запрошена; Android выполнит её при доступной сети",Toast.LENGTH_LONG).show();});
            button(list,"Выключить и удалить журнал с телефона",()->{Diagnostics.disable(this);show();});
        }
        TextView note=new TextView(this);note.setText("После выключения новые события не собираются. Уже доставленные записи удаляются на сервере по сроку хранения. Для теста отметьте начало, выключите экран и отправьте сообщение с другого устройства. Позже откройте приложение и отправьте журнал.");list.addView(note);
        button(list,"Назад",this::finish);
    }
    private void button(LinearLayout list,String label,Runnable action){Button b=new Button(this);b.setText(label);b.setOnClickListener(v->action.run());list.addView(b);}
}
