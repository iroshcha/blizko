package ru.blizko.chat;

import android.os.Bundle;
import android.view.WindowManager;
import com.journeyapps.barcodescanner.CaptureActivity;

public final class QrScanActivity extends CaptureActivity {
    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
    }
}
