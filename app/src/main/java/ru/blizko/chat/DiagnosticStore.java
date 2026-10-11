package ru.blizko.chat;

import android.util.AtomicFile;
import org.json.*;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.util.*;

/** Bounded, private, durable outbox. No message/contact data belongs in this file. */
final class DiagnosticStore {
    static final int LIMIT=512, BATCH=64, MAX_BYTES=1024*1024;
    static final long RETENTION=7L*24*60*60*1000;
    private final AtomicFile file;
    DiagnosticStore(File path){file=new AtomicFile(path);}
    private JSONArray read() throws Exception {
        if(!file.getBaseFile().exists()&&!new File(file.getBaseFile()+".bak").exists())return new JSONArray();
        try(InputStream in=file.openRead();ByteArrayOutputStream out=new ByteArrayOutputStream()){
            byte[] bytes=new byte[4096];int n;
            while((n=in.read(bytes))!=-1){if(out.size()+n>MAX_BYTES)throw new IOException("Diagnostic outbox too large");out.write(bytes,0,n);}
            return new JSONArray(out.toString("UTF-8"));
        }
    }
    private void write(JSONArray events)throws Exception {
        byte[] bytes=events.toString().getBytes(StandardCharsets.UTF_8);
        if(bytes.length>MAX_BYTES)throw new IOException("Diagnostic outbox too large");
        FileOutputStream out=null;
        try{out=file.startWrite();out.write(bytes);file.finishWrite(out);}
        catch(Exception e){if(out!=null)file.failWrite(out);throw e;}
    }
    private JSONArray trim(JSONArray input,long now){
        JSONArray result=new JSONArray();
        for(int i=Math.max(0,input.length()-LIMIT);i<input.length();i++){
            JSONObject event=input.optJSONObject(i);
            if(event!=null&&event.optLong("time")>=now-RETENTION&&event.optLong("time")<=now+RETENTION)result.put(event);
        }
        return result;
    }
    synchronized void append(JSONObject event,long now)throws Exception{JSONArray all=read();all.put(event);write(trim(all,now));}
    synchronized JSONArray batch(long now)throws Exception{
        JSONArray all=trim(read(),now),batch=new JSONArray();write(all);
        for(int i=0;i<Math.min(all.length(),BATCH);i++)batch.put(all.getJSONObject(i));return batch;
    }
    synchronized void acknowledge(JSONArray sent)throws Exception{
        Set<String> ids=new HashSet<>();for(int i=0;i<sent.length();i++)ids.add(sent.getJSONObject(i).getString("id"));
        JSONArray all=read(),remaining=new JSONArray();for(int i=0;i<all.length();i++)if(!ids.contains(all.getJSONObject(i).getString("id")))remaining.put(all.getJSONObject(i));write(remaining);
    }
    synchronized void clear(){file.delete();}
}
