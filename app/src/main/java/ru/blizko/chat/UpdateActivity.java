package ru.blizko.chat;

import android.app.*;
import android.content.*;
import android.net.Uri;
import android.os.Bundle;
import android.provider.Settings;
import android.widget.*;
import androidx.core.content.FileProvider;
import java.io.File;
import java.util.concurrent.*;

public final class UpdateActivity extends Activity {
    private final ExecutorService worker=Executors.newSingleThreadExecutor();
    private TextView status;
    private Button action;
    private ProgressBar progress;
    private AppUpdates.Release release;
    private File apk;
    private boolean busy=false;
    @Override public void onCreate(Bundle saved){
        super.onCreate(saved);
        LinearLayout body=new LinearLayout(this);body.setOrientation(LinearLayout.VERTICAL);
        int pad=(int)(24*getResources().getDisplayMetrics().density);body.setPadding(pad,pad,pad,pad);
        TextView heading=new TextView(this);heading.setText("Обновления «Близко»");heading.setTextSize(26);body.addView(heading);
        TextView version=new TextView(this);version.setText("Установлена версия "+BuildConfig.VERSION_NAME);body.addView(version);
        status=new TextView(this);status.setPadding(0,pad,0,pad);body.addView(status);
        progress=new ProgressBar(this,null,android.R.attr.progressBarStyleHorizontal);progress.setMax(100);progress.setVisibility(android.view.View.GONE);body.addView(progress);
        action=new Button(this);action.setAllCaps(false);action.setText("Проверить обновления");body.addView(action);
        action.setOnClickListener(v->{if(apk!=null)install();else if(release!=null)download();else check();});
        Button close=new Button(this);close.setText("Назад");close.setOnClickListener(v->finish());body.addView(close);setContentView(body);
        if(BuildConfig.UPDATE_MANIFEST_URL.isEmpty()){
            status.setText("Канал обновлений ещё не подключён. Эта версия пока обновляется установкой нового APK.");action.setEnabled(false);
        }else status.setText("Можно проверить новую версию. Установка потребует подтверждения Android.");
    }
    private void ui(Runnable task){runOnUiThread(()->{if(!isFinishing()&&!isDestroyed())task.run();});}
    private void failed(Exception failure){ui(()->{busy=false;action.setEnabled(true);progress.setVisibility(android.view.View.GONE);
        status.setText(failure instanceof java.io.IOException?failure.getMessage():"Не удалось получить или проверить обновление. Проверьте интернет и повторите позже.");});}
    private void check(){
        if(busy)return;busy=true;action.setEnabled(false);status.setText("Проверяем новую версию…");
        worker.execute(()->{try{
            AppUpdates.Release result=AppUpdates.check(BuildConfig.UPDATE_MANIFEST_URL);
            ui(()->{busy=false;action.setEnabled(true);
                if(result.versionCode>BuildConfig.VERSION_CODE){release=result;status.setText("Доступна версия "+result.versionName+". Переписка сохранится.");action.setText("Скачать обновление");}
                else status.setText("У вас последняя доступная версия.");
            });
        }catch(Exception e){failed(e);}});
    }
    private void download(){
        if(busy)return;busy=true;action.setEnabled(false);progress.setVisibility(android.view.View.VISIBLE);status.setText("Скачиваем обновление…");
        worker.execute(()->{try{
            File file=AppUpdates.download(this,release,percent->ui(()->progress.setProgress(percent)));
            ui(()->{busy=false;apk=file;action.setEnabled(true);action.setText("Установить обновление");status.setText("Файл и подпись проверены. Нажмите «Установить обновление».");});
        }catch(Exception e){failed(e);}});
    }
    private void install(){
        if(!getPackageManager().canRequestPackageInstalls()){
            new AlertDialog.Builder(this).setTitle("Разрешение Android")
                .setMessage("Разрешите «Близко» устанавливать обновления. Затем вернитесь сюда и нажмите «Установить обновление».")
                .setPositiveButton("Открыть настройки",(d,w)->startActivity(new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,Uri.parse("package:"+getPackageName()))))
                .setNegativeButton("Отмена",null).show();return;
        }
        if(busy)return;busy=true;action.setEnabled(false);
        worker.execute(()->{try{
            AppUpdates.verify(this,apk,release);
            Uri uri=FileProvider.getUriForFile(this,getPackageName()+".qr",apk);
            ui(()->{busy=false;action.setEnabled(true);
                try{startActivity(new Intent(Intent.ACTION_VIEW).setDataAndType(uri,"application/vnd.android.package-archive").addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION));}
                catch(ActivityNotFoundException e){status.setText("Не удалось открыть установщик Android.");}
            });
        }catch(Exception e){apk=null;release=null;ui(()->action.setText("Проверить обновления"));failed(e);}});
    }
    @Override protected void onDestroy(){worker.shutdown();super.onDestroy();}
}
