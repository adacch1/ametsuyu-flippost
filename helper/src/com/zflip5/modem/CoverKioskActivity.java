package com.zflip5.modem;

import android.app.Activity;
import android.content.SharedPreferences;
import android.os.Bundle;
import android.webkit.WebView;
import android.webkit.WebSettings;

// Minimal cover-screen kiosk: a full-screen WebView pinned to the local daemon
// dashboard. The read-status token is seeded once via an intent extra and kept
// in SharedPreferences, so the URL only carries it on the first launch:
//   adb shell am start -n com.zflip5.modem/.CoverKioskActivity -e token <READ_STATUS>
public class CoverKioskActivity extends Activity {
    private static final String BASE = "http://127.0.0.1:18080/";

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        SharedPreferences sp = getSharedPreferences("zf5", MODE_PRIVATE);
        String token = getIntent() != null ? getIntent().getStringExtra("token") : null;
        if (token != null && token.length() > 0) {
            sp.edit().putString("token", token).apply();
        } else {
            token = sp.getString("token", "");
        }

        WebView wv = new WebView(this);
        WebSettings s = wv.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        setContentView(wv);

        String url = BASE;
        if (token.length() > 0) {
            url = BASE + "?token=" + token;
        }
        wv.loadUrl(url);
    }
}
