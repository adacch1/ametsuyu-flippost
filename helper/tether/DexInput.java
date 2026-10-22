import java.lang.reflect.Method;
import java.lang.reflect.Proxy;

// Root helper: make the forced DeX desktop accept mouse clicks.
//
// `cmd desktopmode on` runs DeX on a virtual "Desktop" display that Samsung
// creates without VIRTUAL_DISPLAY_FLAG_SUPPORTS_TOUCH (DexController sets flags
// 265225). DisplayManagerService then never marks its layer stack as receiving
// input, so SurfaceFlinger makes every window on it NOT_TOUCHABLE and the
// InputDispatcher drops all pointer events, even from a real (uhid) mouse.
//
// This adds a second display on the same layer stack with the receives-input
// flag, backed by a drained ImageReader, and stays running. Killing the
// process destroys the display and reverts the change. Same app_process + root
// pattern as TetherStart, pure reflection (no android.jar).
//
// Usage: DexInput <displayId>   -> prints RESULT=OK ... then blocks
public class DexInput {
    private static final int RGBA_8888 = 1;          // PixelFormat.RGBA_8888
    private static final int DISPLAY_RECEIVES_INPUT = 1;

    public static void main(String[] args) throws Exception {
        if (args.length != 1) { System.out.println("RESULT=FAILED reason=usage"); System.exit(2); }
        int displayId = Integer.parseInt(args[0]);

        Class<?> looper = Class.forName("android.os.Looper");
        looper.getMethod("prepareMainLooper").invoke(null);
        Object mainLooper = looper.getMethod("getMainLooper").invoke(null);

        // Layer stack and size of the DeX display, from the hidden DisplayInfo.
        Class<?> dmg = Class.forName("android.hardware.display.DisplayManagerGlobal");
        Object global = dmg.getMethod("getInstance").invoke(null);
        Object info = dmg.getMethod("getDisplayInfo", int.class).invoke(global, displayId);
        if (info == null) { System.out.println("RESULT=FAILED reason=no_display"); System.exit(1); }
        int layerStack = info.getClass().getField("layerStack").getInt(info);
        int w = info.getClass().getField("logicalWidth").getInt(info);
        int h = info.getClass().getField("logicalHeight").getInt(info);

        Object token = createDisplay("zf5-dexinput");

        // Drain frames so SurfaceFlinger never blocks on a full buffer queue.
        Class<?> irClass = Class.forName("android.media.ImageReader");
        Object reader = irClass.getMethod("newInstance", int.class, int.class, int.class, int.class)
                .invoke(null, w, h, RGBA_8888, 2);
        Class<?> listener = Class.forName("android.media.ImageReader$OnImageAvailableListener");
        Method acquire = irClass.getMethod("acquireLatestImage");
        Object cb = Proxy.newProxyInstance(listener.getClassLoader(), new Class<?>[]{listener}, (p, m, a) -> {
            if (m.getName().equals("onImageAvailable")) {
                Object img = acquire.invoke(a[0]);
                if (img != null) img.getClass().getMethod("close").invoke(img);
            }
            return null;
        });
        Class<?> handlerClass = Class.forName("android.os.Handler");
        Object handler = handlerClass.getConstructor(looper).newInstance(mainLooper);
        irClass.getMethod("setOnImageAvailableListener", listener, handlerClass).invoke(reader, cb, handler);
        Object surface = irClass.getMethod("getSurface").invoke(reader);

        Class<?> binder = Class.forName("android.os.IBinder");
        Class<?> surfaceClass = Class.forName("android.view.Surface");
        Class<?> rectClass = Class.forName("android.graphics.Rect");
        Object rect = rectClass.getConstructor(int.class, int.class, int.class, int.class).newInstance(0, 0, w, h);
        Class<?> tx = Class.forName("android.view.SurfaceControl$Transaction");
        Object t = tx.getConstructor().newInstance();
        tx.getMethod("setDisplaySurface", binder, surfaceClass).invoke(t, token, surface);
        tx.getMethod("setDisplayLayerStack", binder, int.class).invoke(t, token, layerStack);
        tx.getMethod("setDisplayProjection", binder, int.class, rectClass, rectClass).invoke(t, token, 0, rect, rect);
        tx.getMethod("setDisplayFlags", binder, int.class).invoke(t, token, DISPLAY_RECEIVES_INPUT);
        tx.getMethod("apply").invoke(t);

        System.out.println("RESULT=OK display=" + displayId + " layerStack=" + layerStack + " " + w + "x" + h);
        looper.getMethod("loop").invoke(null);
    }

    // Android 14+ moved display creation to DisplayControl in services.jar,
    // backed by libandroid_servers (the same approach scrcpy uses).
    private static Object createDisplay(String name) throws Exception {
        try {
            return Class.forName("android.view.SurfaceControl")
                    .getMethod("createDisplay", String.class, boolean.class).invoke(null, name, false);
        } catch (NoSuchMethodException e) {
            Class<?> pcl = Class.forName("dalvik.system.PathClassLoader");
            ClassLoader cl = (ClassLoader) pcl.getConstructor(String.class, ClassLoader.class)
                    .newInstance("/system/framework/services.jar", ClassLoader.getSystemClassLoader());
            Class<?> dc = cl.loadClass("com.android.server.display.DisplayControl");
            Method load = Runtime.class.getDeclaredMethod("loadLibrary0", Class.class, String.class);
            load.setAccessible(true);
            load.invoke(Runtime.getRuntime(), dc, "android_servers");
            return dc.getMethod("createVirtualDisplay", String.class, boolean.class).invoke(null, name, false);
        }
    }
}
