package ru.blizko.chat;

import android.content.Context;
import java.io.File;
import java.security.SecureRandom;
import org.json.*;
import mobile.Mobile;
import mobile.Node;

/** Real Go + local-server round trips on the Android runtime. No account or mocks. */
final class ServerDeliveryCheck {
    static void run(Context context,String server) throws Exception {
        File dir=new File(context.getNoBackupFilesDir(),"server-test-"+System.currentTimeMillis());
        byte[] key=new byte[32];new SecureRandom().nextBytes(key);
        Node a=Mobile.newNode(new File(dir,"a").getPath(),key);
        Node b=Mobile.newNode(new File(dir,"b").getPath(),key);
        try {
            a.setRelayURL(server);b.setRelayURL(server);
            a.addContact("B",b.myCode());b.addContact("A",a.myCode());
            String aid=new JSONObject(a.snapshot()).getString("id"),bid=new JSONObject(b.snapshot()).getString("id");
            for(int round=0;round<2;round++) {
                a.start();b.start();
                String first="Android A → B "+round,second="Android B → A "+round;
                a.send(bid,first);b.send(aid,second);
                awaitDelivered(a,first);awaitDelivered(b,second);
                a.stop();b.stop();
            }
            a.start();
            a.send(bid,"Queued during offline");Thread.sleep(1500);b.start();
            awaitDelivered(a,"Queued during offline");
            if(!a.checkContact(bid).contains("подтвердил ваш контакт"))throw new AssertionError("Contact probe failed");
        } finally { a.stop();b.stop(); }
    }
    private static void awaitDelivered(Node node,String text)throws Exception {
        long until=System.currentTimeMillis()+90000;
        while(System.currentTimeMillis()<until) {
            JSONArray messages=new JSONObject(node.snapshot()).getJSONArray("messages");
            for(int i=0;i<messages.length();i++) {
                JSONObject m=messages.getJSONObject(i);
                if(m.optBoolean("out")&&text.equals(m.optString("text"))&&m.optBoolean("delivered"))return;
            }
            Thread.sleep(300);
        }
        throw new AssertionError("No verified server receipt within 90 seconds");
    }
}
