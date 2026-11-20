package com.archielink.companion;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.net.LocalSocket;
import android.net.LocalSocketAddress;
import android.net.VpnService;
import android.os.Build;
import android.os.IBinder;
import android.os.ParcelFileDescriptor;
import android.system.Os;
import android.system.OsConstants;
import android.util.Log;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileDescriptor;
import java.io.FileInputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.List;

public final class ArchieVpnService extends VpnService {
    private static final String TAG = "ArchieLinkVPN";
    private static final String CHANNEL_ID = "archielink-vpn";
    private static final int NOTIFICATION_ID = 4101;
    private static final String ACTION_START = "com.archielink.companion.START_VPN";
    private static final String ACTION_STOP = "com.archielink.companion.STOP_VPN";
    private static final String EXTRA_CONFIG = "config";
    private static final String CONTROL_PREFS = "vpn_control";
    private static final String STOP_REQUESTED = "stop_requested";
    private static final Object LOCK = new Object();
    private static volatile ArchieVpnService instance;
    private static volatile String state = "disconnected";
    private static volatile String lastError = "";
    private static volatile String health = "unavailable";
    private static volatile int routedAppCount;

    private Process process;
    private ParcelFileDescriptor tun;
    private volatile boolean stopping;

    static String start(Context context, String config) throws Exception {
        context.getSharedPreferences(CONTROL_PREFS, Context.MODE_PRIVATE)
            .edit()
            .putBoolean(STOP_REQUESTED, false)
            .commit();
        ArchieVpnService current = instance;
        if (current != null) {
            current.stopping = false;
        }
        synchronized (LOCK) {
            state = "connecting";
            lastError = "";
        }
        Intent intent = new Intent(context, ArchieVpnService.class)
            .setAction(ACTION_START)
            .putExtra(EXTRA_CONFIG, config);
        if (Build.VERSION.SDK_INT >= 26) {
            context.startForegroundService(intent);
        } else {
            context.startService(intent);
        }
        long deadline = System.currentTimeMillis() + 15_000;
        synchronized (LOCK) {
            while (System.currentTimeMillis() < deadline) {
                if (state.equals("connected")) {
                    return state;
                }
                if (state.equals("error") || state.equals("disconnected")) {
                    throw new IllegalStateException(lastError.isEmpty() ? "VPN start failed" : lastError);
                }
                LOCK.wait(Math.max(1, deadline - System.currentTimeMillis()));
            }
        }
        throw new IllegalStateException("VPN start timed out");
    }

    static void stop(Context context) {
        ArchieVpnService current = instance;
        if (current != null) {
            current.stopTunnel();
            return;
        }
        context.stopService(new Intent(context, ArchieVpnService.class));
    }

    static String connection() {
        return state;
    }

    static String health() {
        ArchieVpnService current = instance;
        return current == null ? "unavailable" : current.checkHealth();
    }

    static int routedAppCount() {
        return routedAppCount;
    }

    @Override
    public void onCreate() {
        super.onCreate();
        instance = this;
        createNotificationChannel();
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (stopping || stopRequested()) {
            stopProcessOnly();
            stopSelf(startId);
            return START_NOT_STICKY;
        }
        if (intent != null && ACTION_STOP.equals(intent.getAction())) {
            stopTunnel();
            return START_NOT_STICKY;
        }
        startForeground(NOTIFICATION_ID, notification("正在建立 VPN"));
        final String config = intent == null ? null : intent.getStringExtra(EXTRA_CONFIG);
        if (config == null) {
            fail("missing WireGuard config");
            return START_NOT_STICKY;
        }
        new Thread(new Runnable() {
            @Override
            public void run() {
                try {
                    startTunnel(config);
                } catch (Exception error) {
                    Log.e(TAG, "VPN start failed", error);
                    fail(error.getMessage() == null ? "VPN start failed" : error.getMessage());
                }
            }
        }, "archielink-vpn-start").start();
        return START_NOT_STICKY;
    }

    @Override
    public void onRevoke() {
        stopTunnel();
    }

    @Override
    public void onDestroy() {
        stopping = true;
        stopProcessOnly();
        stopForeground(true);
        instance = null;
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) {
        if (SERVICE_INTERFACE.equals(intent.getAction())) {
            return super.onBind(intent);
        }
        return null;
    }

    private void startTunnel(String configText) throws Exception {
        stopProcessOnly();
        WireGuardConfig config = WireGuardConfig.parse(configText);
        Builder builder = new Builder()
            .setSession("ArchieLink")
            .setMtu(1280)
            .setBlocking(false);
        for (String address : config.addresses) {
            String[] split = splitCIDR(address);
            builder.addAddress(split[0], Integer.parseInt(split[1]));
        }
        for (String route : config.allowedIPs) {
            addRoutes(builder, route);
        }
        for (String dns : config.dns) {
            builder.addDnsServer(dns);
        }
        routedAppCount = configureApplications(builder);
        tun = builder.establish();
        if (tun == null) {
            throw new IllegalStateException("system did not grant a VPN interface");
        }
        File socketDirectory = new File(getFilesDir(), "wireguard");
        if (!socketDirectory.isDirectory() && !socketDirectory.mkdirs()) {
            throw new IllegalStateException("cannot create WireGuard socket directory");
        }
        File socketFile = new File(socketDirectory, "archie0.sock");
        if (socketFile.exists() && !socketFile.delete()) {
            throw new IllegalStateException("cannot remove stale WireGuard socket");
        }
        Os.dup2(tun.getFileDescriptor(), 0);
        String binary = getApplicationInfo().nativeLibraryDir + "/libwireguard.so";
        ProcessBuilder processBuilder = new ProcessBuilder(binary, "-f", "archie0");
        File childLog = new File(getFilesDir(), "wireguard.log");
        processBuilder.inheritIO();
        processBuilder.redirectErrorStream(true);
        processBuilder.redirectOutput(childLog);
        processBuilder.environment().put("WG_TUN_FD", "0");
        processBuilder.environment().put("WG_PROCESS_FOREGROUND", "1");
        processBuilder.environment().put("LOG_LEVEL", "verbose");
        process = processBuilder.start();
        FileDescriptor devNull = Os.open("/dev/null", OsConstants.O_RDONLY, 0);
        Os.dup2(devNull, 0);
        Os.close(devNull);
        waitForSocket(socketFile, childLog);
        if (!process.isAlive()) {
            throw new IllegalStateException("wireguard-go exited during startup");
        }
        configure(socketFile, config);
        synchronized (LOCK) {
            state = "connected";
            health = "pending";
            lastError = "";
            LOCK.notifyAll();
        }
        updateNotification("VPN 已连接");
        monitorProcess();
    }

    private void configure(File socketFile, WireGuardConfig config) throws Exception {
        LocalSocket socket = new LocalSocket();
        try {
            socket.connect(new LocalSocketAddress(
                socketFile.getAbsolutePath(), LocalSocketAddress.Namespace.FILESYSTEM
            ));
            socket.setSoTimeout(5_000);
            StringBuilder request = new StringBuilder();
            request.append("set=1\n");
            request.append("private_key=").append(config.privateKey).append('\n');
            if (!config.listenPort.isEmpty()) {
                request.append("listen_port=").append(config.listenPort).append('\n');
            }
            request.append("replace_peers=true\n");
            request.append("public_key=").append(config.peerPublicKey).append('\n');
            if (config.presharedKey != null) {
                request.append("preshared_key=").append(config.presharedKey).append('\n');
            }
            request.append("endpoint=").append(config.endpoint).append('\n');
            if (!config.persistentKeepalive.isEmpty()) {
                request.append("persistent_keepalive_interval=")
                    .append(config.persistentKeepalive).append('\n');
            }
            request.append("replace_allowed_ips=true\n");
            for (String route : config.allowedIPs) {
                request.append("allowed_ip=").append(route).append('\n');
            }
            request.append('\n');
            OutputStream output = socket.getOutputStream();
            output.write(request.toString().getBytes(StandardCharsets.UTF_8));
            output.flush();
            BufferedReader reader = new BufferedReader(
                new InputStreamReader(socket.getInputStream(), StandardCharsets.UTF_8)
            );
            String errno = reader.readLine();
            if (!"errno=0".equals(errno)) {
                throw new IllegalStateException("WireGuard configuration rejected: " + errno);
            }
        } finally {
            socket.close();
        }
    }

    private void monitorProcess() {
        new Thread(new Runnable() {
            @Override
            public void run() {
                Process current = process;
                if (current == null) {
                    return;
                }
                try {
                    int exit = current.waitFor();
                    synchronized (LOCK) {
                        if (process == current) {
            state = "disconnected";
            health = "unavailable";
            lastError = "wireguard-go exited: " + exit;
                            LOCK.notifyAll();
                        }
                    }
                } catch (InterruptedException ignored) {
                    Thread.currentThread().interrupt();
                }
            }
        }, "archielink-vpn-monitor").start();
    }

    private void waitForSocket(File socketFile, File childLog) throws Exception {
        long deadline = System.currentTimeMillis() + 5_000;
        while (System.currentTimeMillis() < deadline) {
            if (socketFile.exists()) {
                return;
            }
            if (!process.isAlive()) {
                throw new IllegalStateException(
                    "wireguard-go exited: " + process.exitValue() + "\n" + logTail(childLog)
                );
            }
            Thread.sleep(100);
        }
        throw new IllegalStateException("WireGuard UAPI socket did not appear\n" + logTail(childLog));
    }

    private void stopTunnel() {
        stopping = true;
        getSharedPreferences(CONTROL_PREFS, MODE_PRIVATE)
            .edit()
            .putBoolean(STOP_REQUESTED, true)
            .commit();
        stopProcessOnly();
        synchronized (LOCK) {
            state = "disconnected";
            lastError = "";
            LOCK.notifyAll();
        }
        stopForeground(true);
        stopSelf();
        stopService(new Intent(this, ArchieVpnService.class));
    }

    private void stopProcessOnly() {
        Process current = process;
        process = null;
        if (current != null) {
            current.destroy();
            try {
                current.waitFor();
            } catch (InterruptedException ignored) {
                Thread.currentThread().interrupt();
            }
            if (current.isAlive()) {
                current.destroyForcibly();
            }
        }
        if (tun != null) {
            try {
                tun.close();
            } catch (Exception ignored) {
                // Closing the TUN is best effort.
            }
            tun = null;
        }
    }

    private void fail(String message) {
        getSharedPreferences(CONTROL_PREFS, MODE_PRIVATE)
            .edit()
            .putBoolean(STOP_REQUESTED, true)
            .commit();
        synchronized (LOCK) {
            state = "error";
            lastError = message;
            LOCK.notifyAll();
        }
        stopProcessOnly();
        stopForeground(true);
        stopSelf();
    }

    private void createNotificationChannel() {
        NotificationManager manager = getSystemService(NotificationManager.class);
        NotificationChannel channel = new NotificationChannel(
            CHANNEL_ID, "ArchieLink VPN", NotificationManager.IMPORTANCE_LOW
        );
        manager.createNotificationChannel(channel);
    }

    private Notification notification(String text) {
        return new Notification.Builder(this, CHANNEL_ID)
            .setContentTitle("ArchieLink")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setOngoing(true)
            .build();
    }

    private void updateNotification(String text) {
        getSystemService(NotificationManager.class).notify(NOTIFICATION_ID, notification(text));
    }

    private String checkHealth() {
        if (!state.equals("connected")) {
            health = "unavailable";
            return health;
        }
        LocalSocket socket = new LocalSocket();
        try {
            File socketFile = new File(new File(getFilesDir(), "wireguard"), "archie0.sock");
            socket.connect(new LocalSocketAddress(
                socketFile.getAbsolutePath(), LocalSocketAddress.Namespace.FILESYSTEM
            ));
            socket.setSoTimeout(2_000);
            OutputStream output = socket.getOutputStream();
            output.write("get=1\n\n".getBytes(StandardCharsets.UTF_8));
            output.flush();
            BufferedReader reader = new BufferedReader(
                new InputStreamReader(socket.getInputStream(), StandardCharsets.UTF_8)
            );
            long latest = 0;
            String line;
            while ((line = reader.readLine()) != null && !line.isEmpty()) {
                if (line.startsWith("last_handshake_time_sec=")) {
                    latest = Math.max(latest, Long.parseLong(
                        line.substring("last_handshake_time_sec=".length())
                    ));
                }
            }
            health = latest > 0 ? "pass" : "pending";
        } catch (Exception error) {
            health = "unavailable";
        } finally {
            try {
                socket.close();
            } catch (Exception ignored) {
                // Closing the health-check socket is best effort.
            }
        }
        return health;
    }

    private static String logTail(File file) {
        if (!file.isFile()) {
            return "";
        }
        try (FileInputStream input = new FileInputStream(file)) {
            byte[] raw = new byte[(int) Math.min(file.length(), 4096)];
            int size = input.read(raw);
            return size <= 0 ? "" : new String(raw, 0, size, StandardCharsets.UTF_8);
        } catch (Exception error) {
            return "cannot read wireguard-go log: " + error;
        }
    }

    private boolean stopRequested() {
        return getSharedPreferences(CONTROL_PREFS, MODE_PRIVATE)
            .getBoolean(STOP_REQUESTED, false);
    }

    private static String[] splitCIDR(String value) {
        int slash = value.indexOf('/');
        if (slash < 0) {
            return new String[]{value, value.contains(":") ? "128" : "32"};
        }
        if (slash == 0 || slash == value.length() - 1) {
            throw new IllegalArgumentException("invalid CIDR");
        }
        return new String[]{value.substring(0, slash), value.substring(slash + 1)};
    }

    private static void addRoutes(Builder builder, String allowed) {
        String[] split = splitCIDR(allowed);
        int prefix = Integer.parseInt(split[1]);
        if (!split[0].contains(":")) {
            for (String route : Ipv4Routes.publicRoutes(split[0], prefix)) {
                String[] routeParts = splitCIDR(route);
                builder.addRoute(routeParts[0], Integer.parseInt(routeParts[1]));
            }
            return;
        }
        if (split[0].equals("::") && prefix == 0) {
            builder.addRoute("2000::", 3);
            return;
        }
        builder.addRoute(split[0], prefix);
    }

    private int configureApplications(Builder builder) throws Exception {
        List<String> allowed = new java.util.ArrayList<>(
            VpnAppSelection.selectedPackages(this)
        );
        java.util.Collections.sort(allowed);
        if (allowed.isEmpty()) {
            throw new IllegalStateException("没有选择需要转发到 VPN 的应用");
        }
        for (String packageName : allowed) {
            builder.addAllowedApplication(packageName);
        }
        return allowed.size();
    }
}
