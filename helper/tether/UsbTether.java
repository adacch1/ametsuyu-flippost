package com.zflip5.tether;

import java.lang.reflect.Constructor;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Executor;
import java.util.regex.Pattern;

// Root helper: starts/stops/checks USB tethering via the framework
// TetheringManager, by reflection (no @SystemApi SDK needed to compile), run
// as root via app_process (see magisk/service.sh) — the root UID satisfies
// TETHER_PRIVILEGED. Unlike the Wi-Fi hotspot (see TetherStart.java), USB
// tethering needs no SoftApConfiguration and its kernel interface name isn't
// a fixed constant across builds, so "status" queries the framework's own
// tethered-interface list instead of a hardcoded iface (cf. softApIface).
//
// Usage: UsbTether [start|stop|status]  (default: start)
// Prints RESULT=STARTED / RESULT=STOPPED / RESULT=FAILED code=<n> for
// start/stop, and RESULT=OK\nACTIVE=true|false\nIFACES=<csv> for status.
public final class UsbTether {
    private static final int TETHERING_USB = 1;

    public static void main(String[] args) throws Exception {
        Class<?> looper = Class.forName("android.os.Looper");
        looper.getMethod("prepareMainLooper").invoke(null);

        Class<?> at = Class.forName("android.app.ActivityThread");
        Object thread = at.getMethod("systemMain").invoke(null);
        Object ctx = at.getMethod("getSystemContext").invoke(thread);
        Method getSvc = ctx.getClass().getMethod("getSystemService", Class.class);

        String action = args.length > 0 ? args[0] : "start";

        if (action.equals("status")) {
            status(ctx, getSvc);
            return;
        }

        Class<?> tmClass = Class.forName("android.net.TetheringManager");
        Object tm = getSvc.invoke(ctx, tmClass);
        if (tm == null) { System.out.println("RESULT=FAILED code=no_tethering_manager"); System.exit(2); }

        if (action.equals("stop")) {
            tmClass.getMethod("stopTethering", int.class).invoke(tm, TETHERING_USB);
            Thread.sleep(1500); // let the teardown land before we exit
            System.out.println("RESULT=STOPPED");
            System.exit(0);
        }

        Class<?> builderClass = Class.forName("android.net.TetheringManager$TetheringRequest$Builder");
        Constructor<?> bctor = builderClass.getConstructor(int.class);
        Object builder = bctor.newInstance(TETHERING_USB);
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
                return null;
            }
        };
        Object cb = Proxy.newProxyInstance(cbClass.getClassLoader(), new Class[]{cbClass}, h);

        Method start = tmClass.getMethod("startTethering", reqClass, Executor.class, cbClass);
        start.invoke(tm, req, exec, cb);

        for (int i = 0; i < 60 && done[0] == null; i++) Thread.sleep(100);
        System.out.println("RESULT=" + (done[0] == null ? "FAILED code=no_callback_timeout" : done[0]));
        System.exit(done[0] != null && done[0].toString().startsWith("STARTED") ? 0 : 1);
    }

    // status: cross-references ConnectivityManager's currently-tethered
    // interfaces against its USB-tether interface-name regex — both are
    // long-standing hidden getters kept for framework-internal/root use
    // alongside TetheringManager, and are the only way to name the actual
    // USB iface (rndis0/usb0/... varies by kernel).
    private static void status(Object ctx, Method getSvc) throws Exception {
        Class<?> cmClass = Class.forName("android.net.ConnectivityManager");
        Object cm = getSvc.invoke(ctx, cmClass);
        if (cm == null) { System.out.println("RESULT=FAILED code=no_connectivity_manager"); System.exit(2); }

        String[] tethered = (String[]) cmClass.getMethod("getTetheredIfaces").invoke(cm);
        String[] usbRegexs = (String[]) cmClass.getMethod("getTetherableUsbRegexs").invoke(cm);

        List<String> matched = new ArrayList<>();
        if (tethered != null && usbRegexs != null) {
            for (String iface : tethered) {
                for (String rx : usbRegexs) {
                    if (Pattern.matches(rx, iface)) { matched.add(iface); break; }
                }
            }
        }
        System.out.println("RESULT=OK");
        System.out.println("ACTIVE=" + !matched.isEmpty());
        System.out.println("IFACES=" + String.join(",", matched));
    }
}
