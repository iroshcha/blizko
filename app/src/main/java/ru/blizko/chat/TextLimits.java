package ru.blizko.chat;
import android.text.InputFilter;
import java.nio.charset.StandardCharsets;
final class TextLimits {
    static InputFilter message(){return (source,start,end,dest,from,to)->{
        String remaining=dest.subSequence(0,from).toString()+dest.subSequence(to,dest.length());int budget=4000-remaining.getBytes(StandardCharsets.UTF_8).length;
        String insert=source.subSequence(start,end).toString();if(insert.getBytes(StandardCharsets.UTF_8).length<=budget)return null;
        int used=0,at=0;while(at<insert.length()){int cp=insert.codePointAt(at),size=cp<=0x7f?1:cp<=0x7ff?2:cp<=0xffff?3:4;if(used+size>budget)break;used+=size;at+=Character.charCount(cp);}return source.subSequence(start,start+at);
    };}
}
