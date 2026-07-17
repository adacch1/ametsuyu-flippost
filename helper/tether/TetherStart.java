package com.zflip5.tether;

import java.lang.reflect.Constructor;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.concurrent.Executor;

// Root helper: starts Wi-Fi tethering (data-sharing hotspot) using the phone's
// saved SoftAP config, by calling the framework TetheringManager via reflection
// so it needs no @SystemApi SDK to compile. Run as root via app_process (see
// magisk/service.sh); the root UID satisfies TETHER_PRIVILEGED. A default
// TetheringRequest for TETHERING_WIFI carries no SoftApConfiguration, so the
// framework reuses the last saved hotspot SSID/passphrase.
//
// Prints RESULT=STARTED on success, RESULT=FAILED code=<n> otherwise. Error
// code 5 (INTERNAL_ERROR) is what the framework returns when tethering is
// already active — treated as success by the caller (the hotspot is up).
//
// This never disables Android/Samsung thermal mitigation; enabling an AP is not
// a thermal bypass. The phone still throttles itself when hot.
public final class TetherStart {
    private static final int TETHERING_WIFI = 0;

    public static void main(String[] args) throws Exception {
        Class<?> looper = Class.forName("android.os.Looper");
        looper.getMethod("prepareMainLooper").invoke(null);

        Class<?> at = Class.forName("android.app.ActivityThread");
        Object thread = at.getMethod("systemMain").invoke(null);
        Object ctx = at.getMethod("getSystemContext").invoke(thread);

        Class<?> tmClass = Class.forName("android.net.TetheringManager");
        Method getSvc = ctx.getClass().getMethod("getSystemService", Class.class);
        Object tm = getSvc.invoke(ctx, tmClass);
        if (tm == null) { System.out.println("RESULT=FAILED code=no_tethering_manager"); System.exit(2); }

        // "stop" arg: tear the hotspot down (SSID-whitelist auto-toggle). The
        // stopTethering(int) verb is fire-and-forget; the caller re-checks the
        // swlan0 interface for ground truth.
        // Default start path: Samsung's SemWifiManager.setWifiApEnabled(cfg, true) —
        // the exact call the stock Settings hotspot toggle makes. Only this path
        // builds a SemSoftApConfiguration (vendor IE + 11ax), so hostapd gets
        // ieee80211ac=1 / 11axmode=1 and the AP runs 802.11ax HE80 (1200Mbps).
        // TetheringManager/ConnectivityManager both hand SoftApModeManager a null
        // config, which lands the AP on 802.11n HT40 (300Mbps).
        if (args.length == 0 || args[0].equals("sem")) {
            try {
                Class<?> semClass = Class.forName("com.samsung.android.wifi.SemWifiManager");
                Object sem = ctx.getClass().getMethod("getSystemService", Class.class).invoke(ctx, semClass);
                Class<?> wmClass2 = Class.forName("android.net.wifi.WifiManager");
                Object wm2 = ctx.getClass().getMethod("getSystemService", Class.class).invoke(ctx, wmClass2);
                Object cfg2 = wmClass2.getMethod("getSoftApConfiguration").invoke(wm2);
                Class<?> cfgClass2 = Class.forName("android.net.wifi.SoftApConfiguration");
                if (sem != null && cfg2 != null) {
                    Object ok2 = semClass.getMethod("setWifiApEnabled", cfgClass2, boolean.class)
                            .invoke(sem, cfg2, true);
                    if (Boolean.TRUE.equals(ok2)) {
                        System.out.println("RESULT=STARTED via=SemWifiManager.setWifiApEnabled");
                        Thread.sleep(1500);
                        System.exit(0);
                    }
                    System.out.println("NOTE sem_enable_rejected, falling back");
                }
            } catch (Throwable t) {
                System.out.println("NOTE sem_enable_unavailable=" + t);
            }
            // fall through to the TetheringManager path below
        }

        // "hotspot" arg: bring the AP up via WifiManager.startTetheredHotspot(cfg)
        // instead of a bare TetheringManager request. Diagnostic for the 802.11ax
        // path — a request with no SoftApConfiguration lands the AP on 11n HT40.
        if (args.length > 0 && args[0].equals("hotspot")) {
            Class<?> wmClass = Class.forName("android.net.wifi.WifiManager");
            Object wm = ctx.getClass().getMethod("getSystemService", Class.class).invoke(ctx, wmClass);
            Object cfg = wmClass.getMethod("getSoftApConfiguration").invoke(wm);
            Class<?> cfgClass = Class.forName("android.net.wifi.SoftApConfiguration");
            Object ok = wmClass.getMethod("startTetheredHotspot", cfgClass).invoke(wm, cfg);
            System.out.println("RESULT=" + (Boolean.TRUE.equals(ok) ? "STARTED" : "FAILED code=rejected"));
            Thread.sleep(1500);
            System.exit(Boolean.TRUE.equals(ok) ? 0 : 1);
        }

        if (args.length > 0 && args[0].equals("stop")) {
            tmClass.getMethod("stopTethering", int.class).invoke(tm, TETHERING_WIFI);
            Thread.sleep(1500); // let the teardown land before we exit
            System.out.println("RESULT=STOPPED");
            System.exit(0);
        }

        Class<?> builderClass = Class.forName("android.net.TetheringManager$TetheringRequest$Builder");
        Constructor<?> bctor = builderClass.getConstructor(int.class);
        Object builder = bctor.newInstance(TETHERING_WIFI);
        Class<?> reqClass = Class.forName("android.net.TetheringManager$TetheringRequest");

        // Attach the saved SoftApConfiguration to the request. A request with no
        // config still brings the AP up, but the HAL then starts hostapd with
        // ieee80211ac=0 / 11axmode=0 and the AP is pinned to 802.11n HT40. The
        // stock Settings toggle attaches the config, which is what unlocks
        // 802.11ax HE80 (1200Mbps vs 300Mbps).
        try {
            Class<?> wmClass = Class.forName("android.net.wifi.WifiManager");
            Object wm = ctx.getClass().getMethod("getSystemService", Class.class).invoke(ctx, wmClass);
            Object cfg = wmClass.getMethod("getSoftApConfiguration").invoke(wm);
            Class<?> cfgClass = Class.forName("android.net.wifi.SoftApConfiguration");
            if (cfg != null) {
                builderClass.getMethod("setSoftApConfiguration", cfgClass).invoke(builder, cfg);
                System.out.println("CFG attached to request");
            }
        } catch (Throwable t) {
            System.out.println("NOTE no_cfg_on_request=" + t);
        }

        Object req = builderClass.getMethod("build").invoke(builder);

        Executor exec = Runnable::run;

        final Object[] done = new Object[1];
        Class<?> cbClass = Class.forName("android.net.TetheringManager$StartTetheringCallback");
        InvocationHandler h = new InvocationHandler() {
            public Object invoke(Object proxy, Method m, Object[] margs) {
                String n = m.getName();
                if (n.equals("toString")) return "TetherCb";
                if (n.equals("hashCode")) return 0;
                if (n.equals("equals")) return proxy == margs[0];
                if (n.equals("onTetheringStarted")) { done[0] = "STARTED"; }
                else if (n.equals("onTetheringFailed")) { done[0] = "FAILED code=" + (margs != null && margs.length > 0 ? margs[0] : "?"); }
                System.out.println("CB " + n + (margs != null && margs.length > 0 ? " " + margs[0] : ""));
                return null;
            }
        };
        Object cb = Proxy.newProxyInstance(cbClass.getClassLoader(), new Class[]{cbClass}, h);

        Method start = tmClass.getMethod("startTethering", reqClass, Executor.class, cbClass);
        start.invoke(tm, req, exec, cb);

        // Wait briefly for the async callback (a binder thread runs it via the executor).
        for (int i = 0; i < 60 && done[0] == null; i++) Thread.sleep(100);
        System.out.println("RESULT=" + (done[0] == null ? "FAILED code=no_callback_timeout" : done[0]));
        System.exit(done[0] != null && done[0].toString().startsWith("STARTED") ? 0 : 1);
    }
}
