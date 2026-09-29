import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.concurrent.Executor;

/** Runs as the already-authorized Android ADB shell, not as an installed app. */
public final class NativeTether {
    public static void main(String[] args) throws Exception {
        if (args.length != 1 || !(args[0].equals("start") || args[0].equals("stop")
                || args[0].equals("start-ethernet") || args[0].equals("stop-ethernet"))) {
            throw new IllegalArgumentException("Usage: NativeTether start|stop|start-ethernet|stop-ethernet");
        }
        Class<?> looper = Class.forName("android.os.Looper");
        looper.getMethod("prepareMainLooper").invoke(null);
        Class<?> activityThread = Class.forName("android.app.ActivityThread");
        Object thread = activityThread.getMethod("systemMain").invoke(null);
        Object system = activityThread.getMethod("getSystemContext").invoke(thread);
        Class<?> context = Class.forName("android.content.Context");
        Object shellContext = context.getMethod("createPackageContext", String.class, int.class)
                .invoke(system, "com.android.shell", 0);
        // app_process uses ActivityThread's system context, which inherits the
        // op package "android". Attribute requests to the actual shell UID's
        // package; claiming android would fail the service's package/UID check.
        int uid = (Integer) Class.forName("android.os.Process").getMethod("myUid").invoke(null);
        if (uid != 2000) throw new SecurityException("Run only from the authorized ADB shell");
        Class<?> attributionBuilderClass = Class.forName("android.content.AttributionSource$Builder");
        Object attributionBuilder = attributionBuilderClass.getConstructor(int.class).newInstance(uid);
        attributionBuilderClass.getMethod("setPackageName", String.class)
                .invoke(attributionBuilder, "com.android.shell");
        java.lang.reflect.Field attribution = shellContext.getClass().getDeclaredField("mAttributionSource");
        attribution.setAccessible(true);
        attribution.set(shellContext, attributionBuilderClass.getMethod("build").invoke(attributionBuilder));
        if (!"com.android.shell".equals(context.getMethod("getOpPackageName").invoke(shellContext))) {
            throw new SecurityException("ADB shell package attribution failed");
        }
        if (((Integer) context.getMethod("checkSelfPermission", String.class)
                .invoke(shellContext, "android.permission.TETHER_PRIVILEGED")) != 0) {
            throw new SecurityException("ADB shell lacks tethering permission");
        }
        Object manager = context.getMethod("getSystemService", String.class)
                .invoke(shellContext, "tethering");
        if (manager == null) throw new IllegalStateException("Tethering service unavailable");
        Class<?> managerClass = Class.forName("android.net.TetheringManager");
        int ncm = managerClass.getField(args[0].endsWith("-ethernet")
                ? "TETHERING_ETHERNET" : "TETHERING_NCM").getInt(null);
        new Thread(() -> {
            try { Thread.sleep(20000); } catch (InterruptedException ignored) { }
            System.err.println("Timed out waiting for tethering callback");
            System.exit(2);
        }, "tethering-timeout").start();
        if (args[0].startsWith("stop")) {
            managerClass.getMethod("stopTethering", int.class).invoke(manager, ncm);
            System.out.println("Sharing stop requested; verify device state");
            System.exit(0);
        }
        Class<?> builderClass = Class.forName("android.net.TetheringManager$TetheringRequest$Builder");
        Object builder = builderClass.getConstructor(int.class).newInstance(ncm);
        int global = managerClass.getField("CONNECTIVITY_SCOPE_GLOBAL").getInt(null);
        builderClass.getMethod("setConnectivityScope", int.class).invoke(builder, global);
        Object request = builderClass.getMethod("build").invoke(builder);
        Class<?> callbackClass = Class.forName("android.net.TetheringManager$StartTetheringCallback");
        Object callback = Proxy.newProxyInstance(callbackClass.getClassLoader(), new Class<?>[]{callbackClass},
                (proxy, method, values) -> {
                    switch (method.getName()) {
                        case "onTetheringStarted":
                            System.out.println("Internet sharing start accepted; verify DHCP and connectivity");
                            System.exit(0);
                            break;
                        case "onTetheringFailed":
                            int error = ((Integer) values[0]).intValue();
                            try {
                                if (error == managerClass.getField("TETHER_ERROR_DUPLICATE_REQUEST").getInt(null)) {
                                    System.out.println("Sharing request already exists; verify DHCP and connectivity");
                                    System.exit(0);
                                }
                            } catch (NoSuchFieldException ignored) { }
                            System.err.println("Sharing start failed, Android error: " + error);
                            System.exit(1);
                            break;
                        case "hashCode": return System.identityHashCode(proxy);
                        case "equals": return proxy == values[0];
                        case "toString": return "NativeTetherCallback";
                    }
                    return null;
                });
        Method start = managerClass.getMethod("startTethering", request.getClass(), Executor.class, callbackClass);
        start.invoke(manager, request, (Executor) Runnable::run, callback);
        looper.getMethod("loop").invoke(null);
    }
}
