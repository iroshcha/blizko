package ru.blizko.chat;

import android.app.Activity;
import android.app.Instrumentation;
import android.content.Intent;
import android.os.Bundle;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import org.json.JSONObject;

/** Exercises the actual activity button, foreground service and embedded Go runtime. */
public class ReceiveStartupTest extends Instrumentation {
    @Override public void onCreate(Bundle arguments) { super.onCreate(arguments); start(); }

    @Override public void onStart() {
        Bundle result = new Bundle();
        try {
            testReceiveStartsWithoutClosingActivity();
            result.putString("stream", "OK: receive starts and connects to iroh without closing the activity");
            finish(Activity.RESULT_OK, result);
        } catch (Throwable failure) {
            result.putString("stream", "FAIL: " + failure.getClass().getSimpleName() + ": " + failure.getMessage());
            finish(Activity.RESULT_CANCELED, result);
        }
    }

    private void check(boolean condition, String message) {
        if (!condition) throw new AssertionError(message);
    }

    private Button findButton(View view, String label) {
        if (view instanceof Button && label.contentEquals(((Button) view).getText())) return (Button) view;
        if (view instanceof ViewGroup) {
            ViewGroup group = (ViewGroup) view;
            for (int i = 0; i < group.getChildCount(); i++) {
                Button found = findButton(group.getChildAt(i), label);
                if (found != null) return found;
            }
        }
        return null;
    }

    public void testReceiveStartsWithoutClosingActivity() throws Exception {
        MainActivity activity = (MainActivity) startActivitySync(new Intent(getTargetContext(), MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
        ChatApp app = (ChatApp) activity.getApplication();
        long deadline = System.currentTimeMillis() + 30000;
        while (app.node == null && System.currentTimeMillis() < deadline) Thread.sleep(200);
        check(app.node != null, "Shared core did not initialize");
        waitForIdleSync();
        Thread.sleep(1500);
        runOnMainSync(() -> {
            Button start = findButton(activity.getWindow().getDecorView(), "Включить приём");
            check(start != null, "Receive button missing");
            start.performClick();
        });
        deadline = System.currentTimeMillis() + 60000;
        JSONObject state = new JSONObject(app.node.snapshot());
        while (System.currentTimeMillis() < deadline) {
            state = new JSONObject(app.node.snapshot());
            if (state.optBoolean("online")) break;
            Thread.sleep(500);
        }
        check(!activity.isFinishing(), "Activity closed during receive startup");
        check(state.optBoolean("enabled"), "Receiver is disabled: " + state.optString("status"));
        check("iroh".equals(state.optString("transport")), "Wrong transport");
        check(state.optBoolean("online"),
            "No iroh connection: " + state.optString("status"));
        runOnMainSync(() -> {
            activity.stopService(new Intent(activity, ChatService.class));
            activity.finish();
        });
        app.io.submit(()->{}).get(60,java.util.concurrent.TimeUnit.SECONDS);
    }
}
