package com.zflip5.tether;

import java.lang.reflect.Method;

// Root helper: reads / writes the phone's persistent Wi-Fi SoftAP (hotspot)
// config via WifiManager.getSoftApConfiguration()/setSoftApConfiguration()
// reflection, so hotspot presets can change the SSID / passphrase / security /
// band. Same app_process + root pattern as TetherStart (root UID satisfies the
// framework permission check).
//
// Writing the config does NOT restart a running AP — the framework applies the
// new config on the next start — so a live hotspot is not dropped here; the
// caller bounces the AP separately (stop+start) when it wants the change live.
// This never touches Android/Samsung thermal mitigation.
//
// Band alone (setBand) leaves the channel to ACS. The HAL then cannot commit to
// a width before the driver picks a channel, so it starts hostapd with
// ieee80211ac=0 / 11axmode=0 and the AP falls back to 802.11n HT40 (300Mbps).
// Pinning the channel (setChannel) lets the HAL enable VHT/HE up front —
// verified: ch149 gives 802.11ax HE80, 1200Mbps on a client.
//
// Usage:
//   get                                                  -> prints CFG ssid=.. band=.. sec=..
//   set <ssid> <open|wpa2|wpa3> <pass> <2|5|6|dual> [channel] -> prints RESULT=OK / RESULT=FAILED reason=..
//
//   band <2|5|6|dual> [channel]                          -> same, keeping the current SSID/passphrase
//
// "dual" is a bridged AP: one SSID served on 2.4GHz (ch 6) and 5GHz (the
// given channel, default 149) at once, via setChannels. The chip exposes a
// second AP interface (wlan2) bridged with swlan0 for this.
public final class SetSoftApConfig {
    // SoftApConfiguration security-type + band constants (stable @SystemApi ints).
    private static final int SEC_OPEN = 0, SEC_WPA2 = 1, SEC_WPA3 = 3;
    private static final int BAND_2 = 1, BAND_5 = 2, BAND_6 = 4;

    public static void main(String[] args) throws Exception {
        Class<?> looper = Class.forName("android.os.Looper");
        looper.getMethod("prepareMainLooper").invoke(null);
        Class<?> at = Class.forName("android.app.ActivityThread");
        Object thread = at.getMethod("systemMain").invoke(null);
        Object ctx = at.getMethod("getSystemContext").invoke(thread);

        Class<?> wmClass = Class.forName("android.net.wifi.WifiManager");
        Method getSvc = ctx.getClass().getMethod("getSystemService", Class.class);
        Object wm = getSvc.invoke(ctx, wmClass);
        if (wm == null) { System.out.println("RESULT=FAILED reason=no_wifi_manager"); System.exit(2); }

        Class<?> cfgClass = Class.forName("android.net.wifi.SoftApConfiguration");

        if (args.length == 0 || args[0].equals("get")) {
            Object cfg = wmClass.getMethod("getSoftApConfiguration").invoke(wm);
            Object ssid;
            try { ssid = cfgClass.getMethod("getSsid").invoke(cfg); }
            catch (Throwable t) { ssid = "?"; }
            Object band = cfgClass.getMethod("getBand").invoke(cfg);
            Object sec = cfgClass.getMethod("getSecurityType").invoke(cfg);
            System.out.println("CFG ssid=" + ssid + " band=" + band + " sec=" + sec);
            System.exit(0);
        }

        // set <ssid> <open|wpa2|wpa3> <pass> <2|5|6> | band <2|5|6|dual>
        boolean bandOnly = args[0].equals("band");
        if (args.length < (bandOnly ? 2 : 3)) { System.out.println("RESULT=FAILED reason=usage"); System.exit(2); }
        String ssid = bandOnly ? null : args[1];
        String secStr = bandOnly ? "" : args[2];
        String pass = !bandOnly && args.length > 3 ? args[3] : "";
        String bandStr = bandOnly ? args[1] : args.length > 4 ? args[4] : "5";
        int chArg = bandOnly ? 2 : 5;
        int channel = args.length > chArg ? Integer.parseInt(args[chArg]) : 0;
        // Band-only 5GHz pins ch149: ACS drops the AP to 802.11n (see header).
        if (bandOnly && channel == 0 && bandStr.equals("5")) channel = 149;

        int sec = secStr.equals("open") ? SEC_OPEN : secStr.equals("wpa3") ? SEC_WPA3 : SEC_WPA2;
        int band = bandStr.equals("2") ? BAND_2 : bandStr.equals("6") ? BAND_6 : BAND_5;

        // Seed the builder from the CURRENT config, never from a bare Builder().
        // A fresh Builder() drops every field we don't set — including Samsung's
        // vendor elements — and the HAL then starts hostapd with ieee80211ac=0 /
        // 11axmode=0, pinning the AP to 802.11n HT40 (300Mbps instead of 1200).
        Class<?> bClass = Class.forName("android.net.wifi.SoftApConfiguration$Builder");
        Object current = wmClass.getMethod("getSoftApConfiguration").invoke(wm);
        Object b = current != null
                ? bClass.getConstructor(cfgClass).newInstance(current)
                : bClass.getConstructor().newInstance();
        if (!bandOnly) {
            bClass.getMethod("setSsid", String.class).invoke(b, ssid);
            if (sec == SEC_OPEN) {
                bClass.getMethod("setPassphrase", String.class, int.class).invoke(b, null, SEC_OPEN);
            } else {
                bClass.getMethod("setPassphrase", String.class, int.class).invoke(b, pass, sec);
            }
        }
        if (bandStr.equals("dual")) {
            Class<?> sia = Class.forName("android.util.SparseIntArray");
            Object chs = sia.getConstructor().newInstance();
            Method put = sia.getMethod("put", int.class, int.class);
            put.invoke(chs, BAND_2, 6);
            put.invoke(chs, BAND_5, channel > 0 ? channel : 149);
            bClass.getMethod("setChannels", sia).invoke(b, chs);
            // Keep both instances up; by default the idle one shuts down.
            bClass.getMethod("setBridgedModeOpportunisticShutdownEnabled", boolean.class).invoke(b, false);
        } else if (channel > 0) {
            bClass.getMethod("setChannel", int.class, int.class).invoke(b, channel, band);
        } else {
            bClass.getMethod("setBand", int.class).invoke(b, band);
        }
        Object cfg = bClass.getMethod("build").invoke(b);

        // Prefer Samsung's SemWifiManager.setSoftApConfiguration(): it is what the
        // stock Settings hotspot toggle calls, and only that path arms the vendor
        // state the HAL needs to emit ieee80211ac=1 / 11axmode=1. The AOSP
        // WifiManager setter stores the same config but leaves the AP at 802.11n
        // HT40 (300Mbps vs 1200Mbps) — verified on a client, 2026-07-17.
        Object semWm = null;
        try {
            Class<?> semClass = Class.forName("com.samsung.android.wifi.SemWifiManager");
            semWm = getSvc.invoke(ctx, semClass);
            if (semWm != null) {
                semClass.getMethod("setSoftApConfiguration", cfgClass).invoke(semWm, cfg);
                System.out.println("RESULT=OK via=SemWifiManager");
                System.exit(0);
            }
        } catch (Throwable t) {
            System.out.println("NOTE sem_setter_unavailable=" + t);
        }

        Object ok = wmClass.getMethod("setSoftApConfiguration", cfgClass).invoke(wm, cfg);
        boolean res = (ok instanceof Boolean) && (Boolean) ok;
        System.out.println("RESULT=" + (res ? "OK via=WifiManager" : "FAILED reason=rejected"));
        System.exit(res ? 0 : 1);
    }
}
