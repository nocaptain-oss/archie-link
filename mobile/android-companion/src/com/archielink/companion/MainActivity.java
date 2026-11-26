package com.archielink.companion;

import android.app.Activity;
import android.app.Dialog;
import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.drawable.GradientDrawable;
import android.net.VpnService;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.Base64;

public final class MainActivity extends Activity {
    private static final int REQUEST_VPN = 4001;
    private final Handler handler = new Handler(Looper.getMainLooper());
    private TextView stateView;
    private TextView detailView;
    private TextView entryView;
    private TextView codeView;
    private Button toggleButton;
    private Button openBrowserButton;
    private Button guidedInstallButton;
    private ConnectionRouteView connectionView;
    private RpcServer server;
    private ContentEntryStatus.Result entryResult;
    private Runnable expiry;
    private Runnable refresh;
    private boolean startAfterAuthorization;
    private boolean pairingActive;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        PremiumUi.applyImmersive(this);
        setContentView(buildLayout());
        handleIntent(getIntent());
        refresh = new Runnable() {
            @Override
            public void run() {
                updateConnectionStatus();
                handler.postDelayed(this, 1000);
            }
        };
        handler.post(refresh);
        PremiumUi.animateIn(findViewById(android.R.id.content), PremiumUi.dp(this, 16), 40);
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        handleIntent(intent);
    }

    @Override
    protected void onResume() {
        super.onResume();
        updateConnectionStatus();
        refreshEntryStatus();
    }

    @Override
    protected void onDestroy() {
        if (refresh != null) {
            handler.removeCallbacks(refresh);
        }
        stopServer();
        super.onDestroy();
    }

    private View buildLayout() {
        FrameLayout root = new FrameLayout(this);
        GradientDrawable background = new GradientDrawable(
            GradientDrawable.Orientation.TL_BR,
            new int[]{PremiumUi.INK, PremiumUi.INK_DEEP, PremiumUi.SURFACE}
        );
        root.setBackground(background);
        root.setClipChildren(false);

        LinearLayout page = new LinearLayout(this);
        page.setOrientation(LinearLayout.VERTICAL);
        page.setClipChildren(false);
        page.setClipToPadding(false);
        root.addView(page, new FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT
        ));

        LinearLayout header = new LinearLayout(this);
        header.setOrientation(LinearLayout.HORIZONTAL);
        header.setGravity(Gravity.CENTER_VERTICAL);
        header.setPadding(
            PremiumUi.dp(this, 64), PremiumUi.dp(this, 24),
            PremiumUi.dp(this, 64), PremiumUi.dp(this, 14)
        );
        header.setClipChildren(false);
        header.setClipToPadding(false);
        page.addView(header, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));

        LinearLayout identity = new LinearLayout(this);
        identity.setOrientation(LinearLayout.VERTICAL);
        identity.setGravity(Gravity.START);
        header.addView(identity, new LinearLayout.LayoutParams(
            0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f
        ));

        TextView brand = PremiumUi.text(this, "ArchieLink", 36, PremiumUi.CHALK, true);
        identity.addView(brand);

        TextView brandCn = PremiumUi.text(
            this, "汪连 · 外网访问专家", 15, PremiumUi.ORANGE, false
        );
        brandCn.setPadding(0, PremiumUi.dp(this, 4), 0, 0);
        identity.addView(brandCn);

        LinearLayout deviceInfo = new LinearLayout(this);
        deviceInfo.setOrientation(LinearLayout.VERTICAL);
        deviceInfo.setGravity(Gravity.END);
        header.addView(deviceInfo, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));

        TextView device = PremiumUi.text(
            this, Build.MANUFACTURER + " " + Build.MODEL, 16, PremiumUi.CHALK, false
        );
        device.setGravity(Gravity.END);
        deviceInfo.addView(device);

        TextView system = PremiumUi.text(
            this, "Android " + Build.VERSION.RELEASE + "  /  API " + Build.VERSION.SDK_INT,
            13, PremiumUi.MIST, false
        );
        system.setGravity(Gravity.END);
        system.setPadding(0, PremiumUi.dp(this, 4), 0, 0);
        deviceInfo.addView(system);

        codeView = PremiumUi.text(this, "------", 21, PremiumUi.BRASS, false);
        codeView.setPadding(0, PremiumUi.dp(this, 8), 0, 0);
        codeView.setVisibility(View.GONE);
        identity.addView(codeView);

        View rule = new View(this);
        rule.setBackgroundColor(Color.argb(120, 44, 72, 73));
        page.addView(rule, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, PremiumUi.dp(this, 1)
        ));

        FrameLayout content = new FrameLayout(this);
        content.setPadding(
            PremiumUi.dp(this, 64), PremiumUi.dp(this, 18),
            PremiumUi.dp(this, 64), PremiumUi.dp(this, 34)
        );
        content.setTranslationX(-PremiumUi.dp(this, 36));
        content.setClipChildren(false);
        content.setClipToPadding(false);
        page.addView(content, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, 0, 1f
        ));

        LinearLayout columns = new LinearLayout(this);
        columns.setOrientation(LinearLayout.HORIZONTAL);
        columns.setGravity(Gravity.CENTER_VERTICAL);
        columns.setClipChildren(false);
        content.addView(columns, new FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT,
            Gravity.CENTER_VERTICAL
        ));

        LinearLayout instrument = new LinearLayout(this);
        instrument.setOrientation(LinearLayout.VERTICAL);
        instrument.setGravity(Gravity.CENTER_HORIZONTAL);
        // The route artwork leaves optical space above its ink; align the visible group with the buttons.
        instrument.setTranslationY(-PremiumUi.dp(this, 18));
        columns.addView(instrument, new LinearLayout.LayoutParams(
            0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f
        ));

        connectionView = new ConnectionRouteView(this);
        instrument.addView(connectionView, new LinearLayout.LayoutParams(
            PremiumUi.dp(this, 300), PremiumUi.dp(this, 170)
        ));

        stateView = PremiumUi.text(this, "", 25, PremiumUi.CHALK, false);
        stateView.setGravity(Gravity.CENTER);
        instrument.addView(stateView);

        detailView = PremiumUi.text(this, "", 17, PremiumUi.MIST, false);
        detailView.setGravity(Gravity.CENTER);
        detailView.setPadding(0, PremiumUi.dp(this, 9), 0, 0);
        instrument.addView(detailView);

        entryView = PremiumUi.text(this, "", 15, PremiumUi.MIST, false);
        entryView.setGravity(Gravity.CENTER);
        entryView.setPadding(0, PremiumUi.dp(this, 16), 0, 0);
        instrument.addView(entryView);

        LinearLayout actions = new LinearLayout(this);
        actions.setOrientation(LinearLayout.VERTICAL);
        actions.setGravity(Gravity.CENTER_VERTICAL | Gravity.END);
        actions.setClipChildren(false);
        columns.addView(actions, new LinearLayout.LayoutParams(
            PremiumUi.dp(this, 300), ViewGroup.LayoutParams.WRAP_CONTENT
        ));

        toggleButton = PremiumUi.primaryButton(this, "连接 VPN");
        toggleButton.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                toggleVPN();
            }
        });
        addActionButton(actions, toggleButton, 0);

        Button manageApps = PremiumUi.secondaryButton(this, "管理转发应用");
        manageApps.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                startActivity(new Intent(MainActivity.this, AppSelectionActivity.class));
            }
        });
        addActionButton(actions, manageApps, 8);

        openBrowserButton = PremiumUi.secondaryButton(this, "打开浏览器");
        openBrowserButton.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                openBrowser();
            }
        });
        addActionButton(actions, openBrowserButton, 8);
        openBrowserButton.setVisibility(View.GONE);

        guidedInstallButton = PremiumUi.secondaryButton(this, "安装浏览器引导");
        guidedInstallButton.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                startActivity(new Intent(MainActivity.this, GuidedInstallActivity.class));
            }
        });
        addActionButton(actions, guidedInstallButton, 8);
        guidedInstallButton.setVisibility(View.GONE);

        Button clear = PremiumUi.secondaryButton(this, "清除已保存配置");
        clear.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                showClearConfigDialog();
            }
        });
        addActionButton(actions, clear, 8);

        Button home = PremiumUi.secondaryButton(this, "返回桌面");
        home.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                Intent intent = new Intent(Intent.ACTION_MAIN);
                intent.addCategory(Intent.CATEGORY_HOME);
                intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
                startActivity(intent);
            }
        });
        addActionButton(actions, home, 8);

        return root;
    }

    private void addActionButton(LinearLayout actions, Button button, int topMargin) {
        button.setMinHeight(0);
        button.setPadding(
            PremiumUi.dp(this, 24), 0, PremiumUi.dp(this, 24), 0
        );
        actions.addView(button, widthParams());
        if (topMargin > 0) {
            PremiumUi.setMargins(button, 0, PremiumUi.dp(this, topMargin), 0, 0);
        }
    }

    private LinearLayout.LayoutParams widthParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        );
        params.height = PremiumUi.dp(this, 50);
        return params;
    }

    private void handleIntent(Intent intent) {
        String token = intent.getStringExtra("session_token");
        String code = intent.getStringExtra("pairing_code");
        String expiresAt = intent.getStringExtra("expires_at");
        String platform = intent.getStringExtra("platform");
        if (token == null || code == null || expiresAt == null) {
            stopServer();
            pairingActive = false;
            codeView.setVisibility(View.GONE);
            updateConnectionStatus();
            return;
        }
        long expires;
        try {
            expires = Instant.parse(expiresAt).toEpochMilli();
        } catch (RuntimeException error) {
            stopServer();
            stateView.setText("配对材料无效");
            return;
        }
        if (token.isEmpty() || token.length() > 128 || !code.matches("[0-9]{6}")
            || expires <= System.currentTimeMillis()) {
            stopServer();
            stateView.setText("配对已过期");
            return;
        }
        String deviceID = loadDeviceID();
        boolean vpnSupported = vpnSupported();
        if (platform == null || platform.isEmpty()) {
            platform = isTelevision() ? "android-tv" : "android";
        }
        stopServer();
        pairingActive = true;
        codeView.setText(code);
        codeView.setVisibility(View.VISIBLE);
        stateView.setText("电脑控制已连接");
        server = new RpcServer(
            getApplicationContext(),
            token,
            code,
            deviceID,
            Build.MANUFACTURER + " " + Build.MODEL,
            platform,
            vpnSupported,
            expires,
            new RpcServer.Listener() {
                @Override
                public void onState(String state) {
                    runOnUiThread(new Runnable() {
                        @Override
                        public void run() {
                            stateView.setText(state);
                        }
                    });
                }

                @Override
                public void requestVPNAuthorization() {
                    runOnUiThread(new Runnable() {
                        @Override
                        public void run() {
                            requestVPN(false);
                        }
                    });
                }
            }
        );
        if (!server.start()) {
            server = null;
            stateView.setText("本地控制通道启动失败");
            return;
        }
        expiry = new Runnable() {
            @Override
            public void run() {
                stopServer();
                pairingActive = false;
                codeView.setVisibility(View.GONE);
                updateConnectionStatus();
            }
        };
        handler.postDelayed(expiry, expires - System.currentTimeMillis());
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode != REQUEST_VPN) {
            return;
        }
        if (server != null) {
            server.setVPNAuthorized(resultCode == RESULT_OK);
        }
        if (resultCode == RESULT_OK && startAfterAuthorization) {
            startAfterAuthorization = false;
            connectStoredConfig();
        } else {
            startAfterAuthorization = false;
            updateConnectionStatus();
        }
    }

    private void toggleVPN() {
        String connection = ArchieVpnService.connection();
        if (connection.equals("connected") || connection.equals("connecting")) {
            ArchieVpnService.stop(this);
            updateConnectionStatus();
            return;
        }
        if (!SecureConfigStore.has(this)) {
            stateView.setText("请先用电脑下发配置");
            return;
        }
        requestVPN(true);
    }

    private void requestVPN(boolean startAfter) {
        Intent intent = VpnService.prepare(this);
        if (intent == null) {
            if (server != null) {
                server.setVPNSupported(true);
                server.setVPNAuthorized(true);
            }
            if (startAfter) {
                connectStoredConfig();
            } else {
                stateView.setText("系统 VPN 已授权");
            }
            return;
        }
        if (getPackageManager().resolveActivity(intent, 0) == null) {
            if (server != null) {
                server.setVPNSupported(false);
                server.setVPNAuthorized(false);
            }
            stateView.setText("需要电脑执行 authorize-vpn");
            return;
        }
        if (server != null) {
            server.setVPNSupported(true);
        }
        startAfterAuthorization = startAfter;
        stateView.setText("等待遥控器授权");
        try {
            startActivityForResult(intent, REQUEST_VPN);
        } catch (ActivityNotFoundException error) {
            startAfterAuthorization = false;
            stateView.setText("需要电脑执行 authorize-vpn");
        }
    }

    private void connectStoredConfig() {
        toggleButton.setEnabled(false);
        stateView.setText("正在建立安全连接");
        new Thread(new Runnable() {
            @Override
            public void run() {
                try {
                    String config = SecureConfigStore.load(MainActivity.this);
                    if (config == null) {
                        throw new IllegalStateException("没有已保存的配置");
                    }
                    ArchieVpnService.start(MainActivity.this, config);
                } catch (Exception error) {
                    runOnUiThread(new Runnable() {
                        @Override
                        public void run() {
                            stateView.setText("连接未完成");
                            detailView.setText(error.getMessage());
                        }
                    });
                } finally {
                    runOnUiThread(new Runnable() {
                        @Override
                        public void run() {
                            toggleButton.setEnabled(true);
                            updateConnectionStatus();
                        }
                    });
                }
            }
        }, "archielink-standalone-connect").start();
    }

    private void updateConnectionStatus() {
        if (toggleButton == null || stateView == null) {
            return;
        }
        String connection = ArchieVpnService.connection();
        String health = ArchieVpnService.health();
        boolean connected = connection.equals("connected");
        boolean connecting = connection.equals("connecting");
        connectionView.setState(connection, health);
        toggleButton.setText(connected || connecting ? "断开 VPN" : "连接 VPN");

        if (connected) {
            stateView.setText("安全连接已建立");
            detailView.setText(
                "通信成功  ·  " + ArchieVpnService.routedAppCount() + " 个应用可访问外网"
            );
        } else if (connecting) {
            stateView.setText("正在建立安全连接");
            detailView.setText("正在检查通信状态");
        } else if (SecureConfigStore.has(this)) {
            stateView.setText("准备就绪");
            detailView.setText(
                VpnAppSelection.selectedCount(this) + " 个应用待访问外网"
            );
        } else if (!pairingActive) {
            stateView.setText("等待配置");
            detailView.setText("请先用电脑完成首次配置");
        }
    }

    private void refreshEntryStatus() {
        if (entryView == null || openBrowserButton == null || guidedInstallButton == null) {
            return;
        }
        entryResult = ContentEntryDetector.detect(this);
        boolean needsBrowser = ContentEntryStatus.NEEDS_BROWSER.equals(entryResult.status);
        if (needsBrowser) {
            entryView.setText("未检测到可用浏览器");
            entryView.setTextColor(PremiumUi.ORANGE);
        } else if (ContentEntryStatus.READY_WITH_APP.equals(entryResult.status)) {
            entryView.setText("内容入口：YouTube 客户端");
            entryView.setTextColor(PremiumUi.MIST);
        } else {
            entryView.setText("内容入口：浏览器就绪");
            entryView.setTextColor(PremiumUi.MIST);
        }
        openBrowserButton.setVisibility(
            entryResult.browserPackage != null ? View.VISIBLE : View.GONE
        );
        guidedInstallButton.setVisibility(needsBrowser ? View.VISIBLE : View.GONE);
    }

    private void openBrowser() {
        if (entryResult == null || entryResult.browserPackage == null) {
            return;
        }
        try {
            startActivity(ContentEntryDetector.openBrowserIntent(this, entryResult.browserPackage));
        } catch (ActivityNotFoundException error) {
            Toast.makeText(this, "浏览器无法启动", Toast.LENGTH_SHORT).show();
        }
    }

    private boolean vpnSupported() {
        Intent intent = VpnService.prepare(this);
        return intent == null || getPackageManager().resolveActivity(intent, 0) != null;
    }

    private void showClearConfigDialog() {
        Dialog dialog = new Dialog(this);
        dialog.requestWindowFeature(Window.FEATURE_NO_TITLE);
        LinearLayout panel = new LinearLayout(this);
        panel.setOrientation(LinearLayout.VERTICAL);
        panel.setPadding(
            PremiumUi.dp(this, 34), PremiumUi.dp(this, 28),
            PremiumUi.dp(this, 34), PremiumUi.dp(this, 28)
        );
        panel.setBackground(PremiumUi.rounded(PremiumUi.SURFACE, PremiumUi.STROKE, 1));
        panel.setClipChildren(false);
        panel.setClipToPadding(false);

        TextView title = PremiumUi.text(this, "清除已保存配置？", 29, PremiumUi.CHALK, true);
        panel.addView(title);

        TextView message = PremiumUi.text(
            this,
            "清除后，VPN 功能无法切换，需重新连接电脑 Agent + ArchieLink 官方 Skill 完成初始配置。",
            18, PremiumUi.MIST, false
        );
        message.setLineSpacing(PremiumUi.dp(this, 6), 1f);
        message.setPadding(0, PremiumUi.dp(this, 18), 0, PremiumUi.dp(this, 28));
        panel.addView(message);

        LinearLayout buttons = new LinearLayout(this);
        buttons.setOrientation(LinearLayout.HORIZONTAL);
        buttons.setGravity(Gravity.END);
        buttons.setClipChildren(false);
        buttons.setClipToPadding(false);
        Button cancel = PremiumUi.secondaryButton(this, "取消");
        cancel.setMinHeight(0);
        cancel.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                dialog.dismiss();
            }
        });
        buttons.addView(cancel, new LinearLayout.LayoutParams(
            PremiumUi.dp(this, 150), PremiumUi.dp(this, 56)
        ));

        Button confirm = PremiumUi.warningButton(this, "确认清除");
        confirm.setMinHeight(0);
        confirm.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                ArchieVpnService.stop(MainActivity.this);
                stopServer();
                SecureConfigStore.clear(MainActivity.this);
                updateConnectionStatus();
                dialog.dismiss();
            }
        });
        buttons.addView(confirm, new LinearLayout.LayoutParams(
            PremiumUi.dp(this, 170), PremiumUi.dp(this, 56)
        ));
        PremiumUi.setMargins(confirm, PremiumUi.dp(this, 12), 0, 0, 0);
        panel.addView(buttons);

        dialog.setContentView(panel);
        dialog.setCanceledOnTouchOutside(true);
        PremiumUi.showDialog(dialog, 780);
    }

    private boolean isTelevision() {
        return getPackageManager().hasSystemFeature("android.software.leanback")
            || Build.MODEL.toLowerCase().contains("tv");
    }

    private String loadDeviceID() {
        SharedPreferences preferences = getSharedPreferences("device", MODE_PRIVATE);
        String id = preferences.getString("device_id", "");
        if (!id.isEmpty()) {
            return id;
        }
        byte[] random = new byte[18];
        new SecureRandom().nextBytes(random);
        id = Base64.getUrlEncoder().withoutPadding().encodeToString(random);
        preferences.edit().putString("device_id", id).commit();
        return id;
    }

    private void stopServer() {
        if (expiry != null) {
            handler.removeCallbacks(expiry);
            expiry = null;
        }
        if (server != null) {
            server.close();
            server = null;
        }
    }

    static String fingerprint(String deviceID) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] sum = digest.digest(deviceID.getBytes(StandardCharsets.UTF_8));
            StringBuilder value = new StringBuilder("sha256:");
            for (byte item : sum) {
                value.append(String.format("%02x", item));
            }
            return value.toString();
        } catch (Exception error) {
            throw new IllegalStateException(error);
        }
    }
}
