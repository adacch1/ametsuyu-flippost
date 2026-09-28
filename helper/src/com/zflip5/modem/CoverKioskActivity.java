package com.zflip5.modem;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.Uri;
import android.os.Bundle;
import android.view.WindowManager;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.webkit.WebSettings;

// Minimal cover-screen kiosk: a full-screen WebView pinned to the local daemon
// dashboard. The read-status token is seeded once via an intent extra and kept
// in SharedPreferences, so the URL only carries it on the first launch:
//   adb shell am start -n com.zflip5.modem/.CoverKioskActivity -e token <READ_STATUS>
public class CoverKioskActivity extends Activity {
    private static final String BASE = "http://127.0.0.1:18080/";
    private static final int FILE_CHOOSER_REQUEST = 51;

    // No AndroidX in this Gradle-free build, so the file-chooser handoff uses
    // the classic startActivityForResult/onActivityResult pair rather than the
    // modern Activity Result API.
    private ValueCallback<Uri[]> filePathCallback;
    private WebView wv;

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        // Cover-screen comfort: appear directly over the (insecure) cover
        // keyguard and wake the panel, so the Action button opens the dashboard
        // in one press with no swiping.
        setShowWhenLocked(true);
        setTurnScreenOn(true);
        // Always-on cover display: hold the panel awake for as long as this
        // activity is in front. Scoped to the window rather than a wake lock, so
        // it releases itself the moment the kiosk goes away — nothing to leak.
        // The panel is dimmed while the kiosk owns it (see setCoverDim in the
        // daemon) because a permanently lit OLED burns in at full brightness.
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);

        SharedPreferences sp = getSharedPreferences("zf5", MODE_PRIVATE);
        String token = getIntent() != null ? getIntent().getStringExtra("token") : null;
        if (token != null && token.length() > 0) {
            sp.edit().putString("token", token).apply();
        } else {
            token = sp.getString("token", "");
        }
        // radio-control token (optional): cached in the app's sandboxed prefs so
        // owner writes auto-fill on every launch. Seeded by action.sh only.
        String rtoken = getIntent() != null ? getIntent().getStringExtra("rtoken") : null;
        if (rtoken != null && rtoken.length() > 0) {
            sp.edit().putString("rtoken", rtoken).apply();
        } else {
            rtoken = sp.getString("rtoken", "");
        }

        wv = new WebView(this);
        // Keep every navigation inside this WebView. With no WebViewClient set,
        // WebView hands http(s) URLs to the ActivityManager instead of loading
        // them: tapping the cover screen's link to the control panel launched
        // Chrome, which pushed the kiosk off the Flex Window, and coverwatch.sh
        // then relaunched it back at "/" a few seconds later.
        wv.setWebViewClient(new WebViewClient());
        WebSettings s = wv.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        // Plain WebView has no file-chooser UI by default: <input type="file">
        // (the dashboard's background-photo picker) is a silent no-op without
        // this. onShowFileChooser hands off to the system picker and returns
        // the result via filePathCallback in onActivityResult below.
        wv.setWebChromeClient(new WebChromeClient() {
            @Override
            public boolean onShowFileChooser(WebView webView, ValueCallback<Uri[]> callback, FileChooserParams params) {
                if (filePathCallback != null) {
                    filePathCallback.onReceiveValue(null);
                }
                filePathCallback = callback;
                Intent intent = new Intent(Intent.ACTION_GET_CONTENT);
                intent.addCategory(Intent.CATEGORY_OPENABLE);
                intent.setType("image/*");
                try {
                    startActivityForResult(intent, FILE_CHOOSER_REQUEST);
                } catch (ActivityNotFoundException e) {
                    filePathCallback = null;
                    return false;
                }
                return true;
            }
        });
        setContentView(wv);

        String url = BASE;
        if (token.length() > 0) {
            url = BASE + "?token=" + token;
            if (rtoken.length() > 0) {
                url = url + "&rtoken=" + rtoken;
            }
        }
        wv.loadUrl(url);
    }

    // Back returns to the previous page (control panel -> cover screen) instead
    // of finishing the activity and leaving the Flex Window black until the
    // watcher notices.
    @Override
    public void onBackPressed() {
        if (wv != null && wv.canGoBack()) {
            wv.goBack();
            return;
        }
        super.onBackPressed();
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode != FILE_CHOOSER_REQUEST || filePathCallback == null) {
            return;
        }
        Uri[] results = null;
        if (resultCode == RESULT_OK && data != null && data.getData() != null) {
            results = new Uri[]{data.getData()};
        }
        filePathCallback.onReceiveValue(results);
        filePathCallback = null;
    }
}
