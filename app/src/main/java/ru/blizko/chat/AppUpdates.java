package ru.blizko.chat;

import android.content.Context;
import android.content.pm.*;
import android.os.Build;
import org.json.JSONObject;
import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.*;

/** Explicit downloads, verified against the installed app's identity and signer. */
final class AppUpdates {
    static final long MAX_APK=200L*1024*1024;
    static final class Release {
        final int versionCode;
        final String versionName,sha256,url;
        final long size;
        Release(String json) throws Exception {
            JSONObject data=new JSONObject(json);
            versionCode=data.getInt("versionCode");versionName=data.getString("versionName");
            sha256=data.getString("sha256").toLowerCase(Locale.ROOT);url=data.getString("url");size=data.getLong("size");
            if(versionCode<=0||versionName.isEmpty()||versionName.length()>40||!sha256.matches("[0-9a-f]{64}")||size<=0||size>MAX_APK)
                throw new IOException("Некорректные сведения об обновлении.");
            https(url);
        }
    }
    interface Progress { void changed(int percent); }
    static URL https(String value) throws Exception {
        URL url=new URL(value);
        if(!"https".equals(url.getProtocol())||url.getHost().isEmpty()||url.getUserInfo()!=null)
            throw new IOException("Обновление доступно только по защищённой ссылке HTTPS.");
        return url;
    }
    private static HttpURLConnection open(String value) throws Exception {
        URL url=https(value);
        for(int i=0;i<5;i++){
            HttpURLConnection connection=(HttpURLConnection)url.openConnection();
            connection.setConnectTimeout(15000);connection.setReadTimeout(20000);connection.setInstanceFollowRedirects(false);
            connection.setRequestProperty("Accept-Encoding","identity");connection.setRequestProperty("Cache-Control","no-cache");
            int status=connection.getResponseCode();
            if(status>=300&&status<400){String location=connection.getHeaderField("Location");connection.disconnect();
                if(location==null)throw new IOException("Ссылка на обновление недоступна.");
                url=https(new URL(url,location).toString());continue;}
            if(status!=200){connection.disconnect();throw new IOException("Сервис обновлений недоступен. Попробуйте позже.");}
            return connection;
        }
        throw new IOException("Слишком много перенаправлений.");
    }
    static Release check(String feed) throws Exception {
        HttpURLConnection connection=open(feed);
        try(InputStream input=connection.getInputStream();ByteArrayOutputStream output=new ByteArrayOutputStream()){
            byte[] chunk=new byte[4096];int count;
            while((count=input.read(chunk))!=-1){if(output.size()+count>65536)throw new IOException("Ответ сервиса слишком большой.");output.write(chunk,0,count);}
            return new Release(output.toString(StandardCharsets.UTF_8.name()));
        }finally{connection.disconnect();}
    }
    static File download(Context context,Release release,Progress progress) throws Exception {
        File directory=new File(context.getCacheDir(),"updates");
        if(!directory.isDirectory()&&!directory.mkdirs())throw new IOException("Не удалось создать папку обновления.");
        File file=new File(directory,"Blizko-update.apk");boolean verified=false;
        HttpURLConnection connection=open(release.url);
        try(InputStream input=connection.getInputStream();OutputStream output=new FileOutputStream(file)){
            byte[] chunk=new byte[65536];int count,last=-1;long received=0;
            while((count=input.read(chunk))!=-1){
                received+=count;if(received>release.size)throw new IOException("Размер обновления не совпадает.");
                output.write(chunk,0,count);int percent=(int)(received*100/release.size);
                if(percent!=last){last=percent;progress.changed(percent);}
            }
            output.flush();verify(context,file,release);verified=true;return file;
        }finally{connection.disconnect();if(!verified)file.delete();}
    }
    static void verify(Context context,File file,Release release) throws Exception {
        if(file.length()!=release.size)throw new IOException("Обновление загружено не полностью.");
        MessageDigest digest=MessageDigest.getInstance("SHA-256");
        try(InputStream input=new FileInputStream(file)){byte[] buffer=new byte[65536];int count;while((count=input.read(buffer))!=-1)digest.update(buffer,0,count);}
        if(!hex(digest.digest()).equals(release.sha256))throw new IOException("Проверка целостности обновления не пройдена.");
        PackageManager pm=context.getPackageManager();
        int flags=Build.VERSION.SDK_INT>=28?PackageManager.GET_SIGNING_CERTIFICATES:PackageManager.GET_SIGNATURES;
        PackageInfo candidate=pm.getPackageArchiveInfo(file.getPath(),flags),installed=pm.getPackageInfo(context.getPackageName(),flags);
        if(candidate==null||!context.getPackageName().equals(candidate.packageName)||version(candidate)!=release.versionCode||version(candidate)<version(installed))
            throw new IOException("Этот файл не подходит для обновления «Близко».");
        Set<String> trusted=signers(installed);
        if(trusted.isEmpty()||!trusted.equals(signers(candidate)))throw new IOException("Подпись обновления не совпадает с установленным приложением.");
    }
    private static long version(PackageInfo info){return Build.VERSION.SDK_INT>=28?info.getLongVersionCode():info.versionCode;}
    private static Set<String> signers(PackageInfo info)throws Exception{
        Signature[] signatures=Build.VERSION.SDK_INT>=28?(info.signingInfo==null?null:info.signingInfo.getApkContentsSigners()):info.signatures;
        Set<String> values=new HashSet<>();
        if(signatures!=null)for(Signature signature:signatures)values.add(hex(MessageDigest.getInstance("SHA-256").digest(signature.toByteArray())));
        return values;
    }
    static String hex(byte[] bytes){StringBuilder out=new StringBuilder();for(byte b:bytes)out.append(String.format(Locale.ROOT,"%02x",b&255));return out.toString();}
}
