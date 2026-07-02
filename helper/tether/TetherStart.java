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
