package com.archielink.companion;

import android.content.Context;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import java.security.KeyPairGenerator;
import java.security.MessageDigest;
import java.security.PrivateKey;
import java.security.SecureRandom;
import java.util.Arrays;
import java.util.Base64;
import java.util.HashSet;
import java.util.Set;

import javax.crypto.Mac;
import javax.crypto.Cipher;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLServerSocket;
import javax.net.ssl.SSLSocket;

final class RpcServer {
    private static final String TAG = "ArchieLinkRPC";
    interface Listener {
        void onState(String state);
        void requestVPNAuthorization();
    }

    private static final String CONFIG_KEY_ALIAS = "archielink-config-v2";
    private static final int PORT = 42817;
    private static final int MAX_LINE = 64 * 1024;
    private static final char[] TLS_PASSWORD = "archielink-debug-rpc".toCharArray();
    private static final Set<String> METHODS = new HashSet<>(Arrays.asList(
        "hello", "capabilities", "prepare", "start", "status", "stop", "close"
    ));

    private final Context context;
    private final String token;
    private final String code;
    private final String deviceID;
    private final String alias;
    private final String platform;
    private volatile boolean vpnSupported;
    private final long expiresAt;
    private final Listener listener;
    private volatile boolean running;
    private volatile boolean closeRequested;
    private volatile String certificateHash;
    private PrivateKey configPrivateKey;
    private String configPublicKey;
    private String preparedConfig;
    private volatile boolean vpnAuthorized;
    private SSLServerSocket server;
    private Thread thread;

    RpcServer(
        Context context, String token, String code, String deviceID, String alias,
        String platform, boolean vpnSupported, long expiresAt, Listener listener
    ) {
        this.context = context;
        this.token = token;
        this.code = code;
        this.deviceID = deviceID;
        this.alias = alias;
        this.platform = platform;
        this.vpnSupported = vpnSupported;
        this.expiresAt = expiresAt;
        this.listener = listener;
    }

    boolean start() {
        try {
            SSLContext ssl = createSSLContext();
            loadConfigKey();
            server = (SSLServerSocket) ssl.getServerSocketFactory().createServerSocket();
            server.setReuseAddress(true);
            server.bind(new InetSocketAddress(InetAddress.getLoopbackAddress(), PORT));
            server.setEnabledProtocols(new String[]{"TLSv1.2"});
        } catch (Exception error) {
            Log.e(TAG, "TLS server start failed", error);
            close();
            return false;
        }
        running = true;
        thread = new Thread(new Runnable() {
            @Override
            public void run() {
                acceptLoop();
            }
        }, "archielink-rpc");
        thread.setDaemon(true);
        thread.start();
        return true;
    }

    void close() {
        closeRequested = true;
        running = false;
        if (server != null) {
            try {
                server.close();
            } catch (IOException ignored) {
                // Closing the listener is best effort.
            }
            server = null;
        }
    }

    private SSLContext createSSLContext() throws Exception {
        KeyStore store = KeyStore.getInstance("PKCS12");
        try (InputStream input = context.getAssets().open("archielink-rpc.p12")) {
            store.load(input, TLS_PASSWORD);
        }
        byte[] certificate = store.getCertificate("rpc").getEncoded();
        certificateHash = sha256(certificate);
        KeyManagerFactory managers = KeyManagerFactory.getInstance(
            KeyManagerFactory.getDefaultAlgorithm()
        );
        managers.init(store, TLS_PASSWORD);
        SSLContext ssl = SSLContext.getInstance("TLS");
        ssl.init(managers.getKeyManagers(), null, new SecureRandom());
        return ssl;
    }

    private void loadConfigKey() throws Exception {
        KeyStore store = KeyStore.getInstance("AndroidKeyStore");
        store.load(null);
        if (!store.containsAlias(CONFIG_KEY_ALIAS)) {
            KeyPairGenerator generator = KeyPairGenerator.getInstance(
                KeyProperties.KEY_ALGORITHM_RSA, "AndroidKeyStore"
            );
            generator.initialize(new KeyGenParameterSpec.Builder(
                CONFIG_KEY_ALIAS, KeyProperties.PURPOSE_DECRYPT
            )
                .setKeySize(2048)
                .setDigests(KeyProperties.DIGEST_SHA1)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_RSA_OAEP)
                .build());
            generator.generateKeyPair();
        }
        configPrivateKey = (PrivateKey) store.getKey(CONFIG_KEY_ALIAS, null);
        configPublicKey = Base64.getEncoder().encodeToString(
            store.getCertificate(CONFIG_KEY_ALIAS).getPublicKey().getEncoded()
        );
    }

    void setVPNAuthorized(boolean authorized) {
        vpnAuthorized = authorized;
    }

    void setVPNSupported(boolean supported) {
        vpnSupported = supported;
    }

    private void acceptLoop() {
        SSLServerSocket socketServer = server;
        while (running && !closeRequested && System.currentTimeMillis() < expiresAt) {
            Socket socket = null;
            try {
                socket = socketServer.accept();
                socket.setSoTimeout(10_000);
                ((SSLSocket) socket).startHandshake();
                handle(socket);
            } catch (Exception error) {
                if (running) {
                    Log.w(TAG, "TLS client failed", error);
                    listener.onState("RPC 连接异常，等待重试");
                }
            } finally {
                if (socket != null) {
                    try {
                        socket.close();
                    } catch (IOException ignored) {
                        // Closing the client socket is best effort.
                    }
                }
            }
        }
        running = false;
        closeRequested = true;
        close();
    }

    private void handle(Socket socket) throws IOException {
        InputStream input = socket.getInputStream();
        OutputStream output = socket.getOutputStream();
        String challenge = readText(input);
        if (challenge == null || challenge.length() > 128) {
            return;
        }
        try {
            if (Base64.getUrlDecoder().decode(challenge).length != 32) {
                return;
            }
        } catch (IllegalArgumentException error) {
            return;
        }
        writeText(output, proof("server", challenge));
        byte[] supplied = readLine(input, MAX_LINE);
        if (supplied == null || !MessageDigest.isEqual(
            proof("client", challenge).getBytes(StandardCharsets.US_ASCII), supplied
        )) {
            return;
        }
        byte[] raw = readLine(input, MAX_LINE);
        if (raw == null) {
            return;
        }
        JSONObject request;
        try {
            request = new JSONObject(new String(raw, StandardCharsets.UTF_8));
        } catch (JSONException error) {
            return;
        }
        String requestID = request.optString("request_id", "");
        String method = request.optString("method", "");
        if (!requestID.matches("[A-Za-z0-9._:-]{8,128}") || !METHODS.contains(method)) {
            write(output, response(requestID, false, "rejected", "invalid_request", null));
            return;
        }
        if (request.optInt("schema_version", 0) != 1) {
            write(output, response(requestID, false, "rejected", "schema_mismatch", null));
            return;
        }
        if (System.currentTimeMillis() >= expiresAt) {
            write(output, response(requestID, false, "expired", "session_expired", null));
            closeRequested = true;
            return;
        }
        switch (method) {
            case "hello":
                write(output, response(requestID, true, "ready", null, helloPayload()));
                break;
            case "capabilities":
                write(output, response(requestID, true, "ready", null, capabilitiesPayload()));
                break;
            case "status":
                write(output, response(requestID, true, "ready", null, statusPayload()));
                break;
            case "prepare":
                handlePrepare(output, requestID, request.optJSONObject("payload"));
                break;
            case "start":
                handleStart(output, requestID);
                break;
            case "stop":
                ArchieVpnService.stop(context);
                write(output, response(requestID, true, "stopped", null, statusPayload()));
                break;
            case "close":
                write(output, response(requestID, true, "closed", null, null));
                closeRequested = true;
                listener.onState("会话已关闭");
                break;
            default:
                write(output, response(requestID, false, "unsupported", "not_implemented", null));
                break;
        }
    }

    private JSONObject helloPayload() throws IOException {
        try {
            JSONObject payload = new JSONObject();
            payload.put("device_id", deviceID);
            payload.put("alias", alias);
            payload.put("fingerprint", MainActivity.fingerprint(deviceID));
            payload.put("pairing_proof", pairingProof());
            payload.put("config_key", configPublicKey);
            payload.put(
                "capabilities",
                new JSONArray(Arrays.asList("hello", "capabilities", "prepare", "status", "stop", "close"))
            );
            return payload;
        } catch (JSONException error) {
            throw new IOException(error);
        }
    }

    private JSONObject capabilitiesPayload() throws IOException {
        try {
            JSONObject payload = new JSONObject();
            payload.put(
                "methods",
                new JSONArray(Arrays.asList("hello", "capabilities", "prepare", "start", "status", "stop", "close"))
            );
            payload.put("unimplemented", new JSONArray());
            return payload;
        } catch (JSONException error) {
            throw new IOException(error);
        }
    }

    private JSONObject statusPayload() throws IOException {
        try {
            JSONObject payload = new JSONObject();
            payload.put("platform", platform);
            payload.put("vpn_supported", vpnSupported);
            payload.put("vpn_authorized", vpnAuthorized);
            payload.put("prepared", preparedConfig != null || SecureConfigStore.has(context));
            payload.put("connection", ArchieVpnService.connection());
            payload.put("health", ArchieVpnService.health());
            payload.put("tunnel_backend", "wireguard-go");
            payload.put("routed_app_count", ArchieVpnService.routedAppCount());
            return payload;
        } catch (JSONException error) {
            throw new IOException(error);
        }
    }

    private String pairingProof() {
        return sha256((token + ":" + code + ":" + deviceID).getBytes(StandardCharsets.UTF_8));
    }

    private void handlePrepare(OutputStream output, String requestID, JSONObject payload)
        throws IOException {
        if (payload == null) {
            write(output, response(requestID, false, "invalid", "invalid_payload", null));
            return;
        }
        try {
            byte[] encryptedKey = Base64.getDecoder().decode(payload.getString("encrypted_key"));
            byte[] nonce = Base64.getDecoder().decode(payload.getString("nonce"));
            byte[] encryptedConfig = Base64.getDecoder().decode(payload.getString("encrypted_config"));
            Cipher rsa = Cipher.getInstance("RSA/ECB/OAEPWithSHA-1AndMGF1Padding");
            rsa.init(Cipher.DECRYPT_MODE, configPrivateKey);
            byte[] aesKey = rsa.doFinal(encryptedKey);
            Cipher gcm = Cipher.getInstance("AES/GCM/NoPadding");
            gcm.init(
                Cipher.DECRYPT_MODE,
                new SecretKeySpec(aesKey, "AES"),
                new GCMParameterSpec(128, nonce)
            );
            byte[] raw = gcm.doFinal(encryptedConfig);
            Arrays.fill(aesKey, (byte) 0);
            String config = new String(raw, StandardCharsets.UTF_8);
            Arrays.fill(raw, (byte) 0);
            WireGuardConfig.parse(config);
            preparedConfig = config;
            SecureConfigStore.save(context, config);
            write(output, response(requestID, true, "prepared", null, statusPayload()));
        } catch (Exception error) {
            Log.w(TAG, "config preparation failed", error);
            write(output, response(requestID, false, "invalid", "invalid_config", null));
        }
    }

    private void handleStart(OutputStream output, String requestID) throws IOException {
        if (preparedConfig == null) {
            preparedConfig = SecureConfigStore.load(context);
        }
        if (preparedConfig == null) {
            write(output, response(requestID, false, "not_prepared", "not_prepared", null));
            return;
        }
        if (!vpnAuthorized) {
            listener.requestVPNAuthorization();
            write(output, response(requestID, true, "authorization_required", null, statusPayload()));
            return;
        }
        try {
            ArchieVpnService.start(context, preparedConfig);
            write(output, response(requestID, true, "connected", null, statusPayload()));
        } catch (Exception error) {
            Log.e(TAG, "WireGuard tunnel start failed", error);
            write(output, responseWithMessage(
                requestID, false, "error", "tunnel_start_failed", error.toString(), null
            ));
        }
    }

    private String proof(String role, String challenge) throws IOException {
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(token.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
            String message = role + ":" + challenge + ":" + certificateHash;
            return hex(mac.doFinal(message.getBytes(StandardCharsets.UTF_8)));
        } catch (Exception error) {
            throw new IOException(error);
        }
    }

    private byte[] response(
        String requestID, boolean ok, String state, String errorCode, JSONObject payload
    ) {
        return responseWithMessage(requestID, ok, state, errorCode, null, payload);
    }

    private byte[] responseWithMessage(
        String requestID, boolean ok, String state, String errorCode, String message, JSONObject payload
    ) {
        try {
            JSONObject value = new JSONObject();
            value.put("schema_version", 1);
            value.put("request_id", requestID);
            value.put("ok", ok);
            value.put("state", state);
            if (errorCode != null) {
                value.put("error_code", errorCode);
            }
            if (message != null) {
                value.put("message", message);
            }
            if (payload != null) {
                value.put("payload", payload);
            }
            return (value.toString() + "\n").getBytes(StandardCharsets.UTF_8);
        } catch (JSONException error) {
            throw new IllegalStateException(error);
        }
    }

    private static String sha256(byte[] value) {
        try {
            return "sha256:" + hex(MessageDigest.getInstance("SHA-256").digest(value));
        } catch (Exception error) {
            throw new IllegalStateException(error);
        }
    }

    private static String hex(byte[] value) {
        StringBuilder result = new StringBuilder(value.length * 2);
        for (byte item : value) {
            result.append(String.format("%02x", item));
        }
        return result.toString();
    }

    private static String readText(InputStream input) throws IOException {
        byte[] raw = readLine(input, MAX_LINE);
        return raw == null ? null : new String(raw, StandardCharsets.US_ASCII);
    }

    private static byte[] readLine(InputStream input, int maxBytes) throws IOException {
        ByteArrayOutputStream output = new ByteArrayOutputStream();
        while (output.size() <= maxBytes) {
            int value = input.read();
            if (value < 0 || value == '\n') {
                break;
            }
            if (value != '\r') {
                output.write(value);
            }
        }
        if (output.size() > maxBytes) {
            return null;
        }
        return output.toByteArray();
    }

    private static void writeText(OutputStream output, String value) throws IOException {
        write(output, (value + "\n").getBytes(StandardCharsets.US_ASCII));
    }

    private static void write(OutputStream output, byte[] response) throws IOException {
        output.write(response);
        output.flush();
    }
}
