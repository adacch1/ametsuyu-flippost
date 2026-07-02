package com.zflip5.tether;

import java.lang.reflect.Array;
import java.lang.reflect.Field;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;

// Root helper: one-shot Wi-Fi scan via the internal WifiScanner API, run as
// root through app_process. The public WifiManager.startScan path is blocked
// on Samsung while the SoftAP is up (SemWifiManagerProxy rejects it); the
// scanning service itself is not, so the SSID-whitelist hotspot auto-toggle
// scans here. Needs location services ON — Android only brings up the
// scan-only interface then (verified on-device); callers detect location-off
// and pause instead of failing.
//
// Output: one "AP\t<ssid>\t<bssid>\t<rssi>" line per result, then RESULT=OK.
// On failure: RESULT=FAIL reason=<...>. Read-only: never connects, never
// touches STA state or the SoftAP.
public final class WifiScan {
    public static void main(String[] args) throws Exception {
        Class<?> looperCls = Class.forName("android.os.Looper");
        looperCls.getMethod("prepareMainLooper").invoke(null);

        Class<?> at = Class.forName("android.app.ActivityThread");
        Object thread = at.getMethod("systemMain").invoke(null);
        Object ctx = at.getMethod("getSystemContext").invoke(thread);

        Class<?> wsCls = Class.forName("android.net.wifi.WifiScanner");
        Object ws = ctx.getClass().getMethod("getSystemService", Class.class).invoke(ctx, wsCls);
        if (ws == null) { System.out.println("RESULT=FAIL reason=no_wifiscanner_service"); System.exit(2); }

        // Arm the scanner — the same call the framework makes when scan-only
        // mode starts. Harmless if already enabled.
        try {
            wsCls.getMethod("setScanningEnabled", boolean.class).invoke(ws, true);
        } catch (Throwable ignored) { /* proceed; scan fails visibly below if off */ }

        Class<?> setCls = Class.forName("android.net.wifi.WifiScanner$ScanSettings");
        Object settings = setCls.getConstructor().newInstance();
        setCls.getField("band").setInt(settings, wsCls.getField("WIFI_BAND_BOTH_WITH_DFS").getInt(null));
        setCls.getField("reportEvents").setInt(settings, wsCls.getField("REPORT_EVENT_AFTER_EACH_SCAN").getInt(null));

        Class<?> listenerCls = Class.forName("android.net.wifi.WifiScanner$ScanListener");
        InvocationHandler h = (proxy, m, margs) -> {
            String n = m.getName();
            if (n.equals("toString")) return "ZF5WifiScan";
            if (n.equals("hashCode")) return 0;
            if (n.equals("equals")) return proxy == margs[0];
            if (n.equals("onFailure")) {
                System.out.println("RESULT=FAIL reason=" + margs[0] + (margs.length > 1 ? " " + margs[1] : ""));
                System.exit(1);
            }
            if (n.equals("onResults")) {
                try { dump(margs[0]); } catch (Throwable t) {
                    System.out.println("RESULT=FAIL reason=parse " + t);
                    System.exit(1);
                }
                System.out.println("RESULT=OK");
                System.exit(0);
            }
            return null;
        };
        Object listener = Proxy.newProxyInstance(listenerCls.getClassLoader(), new Class[]{listenerCls}, h);

        wsCls.getMethod("startScan", setCls, listenerCls).invoke(ws, settings, listener);

        new Thread(() -> {
            try { Thread.sleep(25000); } catch (InterruptedException ignored) {}
            System.out.println("RESULT=FAIL reason=timeout");
            System.exit(3);
        }).start();

        looperCls.getMethod("loop").invoke(null); // pump listener callbacks
    }

    private static void dump(Object scanDatas) throws Exception {
        for (int i = 0, n = Array.getLength(scanDatas); i < n; i++) {
            Object sd = Array.get(scanDatas, i);
            Object results = sd.getClass().getMethod("getResults").invoke(sd);
            for (int j = 0, rn = Array.getLength(results); j < rn; j++) {
                Object r = Array.get(results, j);
                Field ssid = r.getClass().getField("SSID");
                Field bssid = r.getClass().getField("BSSID");
                Field level = r.getClass().getField("level");
                // SSIDs are attacker-controlled-ish text: strip tabs/newlines so
                // one AP can't forge extra output lines.
                String name = String.valueOf(ssid.get(r)).replaceAll("[\\t\\r\\n]", " ");
                System.out.println("AP\t" + name + "\t" + bssid.get(r) + "\t" + level.get(r));
            }
        }
    }
}
