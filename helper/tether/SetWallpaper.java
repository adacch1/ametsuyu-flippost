package com.zflip5.tether;

// Root helper: sets a solid-colour static wallpaper. Samsung's default live
// wallpaper (FoldInteractive) makes systemui decode HEVC video on the cover
// screen nonstop, underneath the kiosk where nobody sees it: ~40% systemui,
// ~20% surfaceflinger, and a busy hardware codec. A static bitmap stops it.
//
// Must run as uid 1000 (system), not root: the wallpaper service checks that
// the calling package ("android") belongs to the caller's uid, and throws
// "package does not belong to uid:0" for root.
//
// Usage: set <aarrggbb> <which>
//   which is a WallpaperManager flag set: SYSTEM 1 | LOCK 2 | Samsung's
//   FLAG_DISPLAY_PHONE 4 (main screen) or FLAG_DISPLAY_SUB 16 (cover screen).
//   7 = main home+lock, 19 = cover home+lock.
// Prints RESULT=OK id=<n>.
public final class SetWallpaper {
    public static void main(String[] args) throws Exception {
        if (args.length < 3 || !args[0].equals("set")) {
            System.out.println("RESULT=FAILED reason=usage");
            System.exit(2);
        }
        int color = (int) Long.parseLong(args[1], 16);
        int which = Integer.parseInt(args[2]);

        Class<?> looper = Class.forName("android.os.Looper");
        looper.getMethod("prepareMainLooper").invoke(null);
        Class<?> at = Class.forName("android.app.ActivityThread");
        Object ctx = at.getMethod("getSystemContext").invoke(at.getMethod("systemMain").invoke(null));

        Class<?> wmClass = Class.forName("android.app.WallpaperManager");
        Object wm = wmClass.getMethod("getInstance", Class.forName("android.content.Context")).invoke(null, ctx);

        Class<?> bmpClass = Class.forName("android.graphics.Bitmap");
        Class<?> cfgClass = Class.forName("android.graphics.Bitmap$Config");
        // A small bitmap is enough for a solid colour; the service scales it.
        Object bmp = bmpClass.getMethod("createBitmap", int.class, int.class, cfgClass)
                .invoke(null, 64, 64, cfgClass.getField("ARGB_8888").get(null));
        bmpClass.getMethod("eraseColor", int.class).invoke(bmp, color);

        Object id = wmClass.getMethod("setBitmap", bmpClass, Class.forName("android.graphics.Rect"), boolean.class, int.class)
                .invoke(wm, bmp, null, true, which);
        System.out.println("RESULT=OK id=" + id);
        System.exit(0);
    }
}
