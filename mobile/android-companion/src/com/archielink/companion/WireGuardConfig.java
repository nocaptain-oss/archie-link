package com.archielink.companion;

import java.util.ArrayList;
import java.util.Base64;
import java.util.List;
import java.util.Locale;

final class WireGuardConfig {
    String privateKey;
    String listenPort = "";
    final List<String> addresses = new ArrayList<>();
    final List<String> dns = new ArrayList<>();
    String peerPublicKey;
    String presharedKey;
    String endpoint;
    String persistentKeepalive = "";
    final List<String> allowedIPs = new ArrayList<>();

    static WireGuardConfig parse(String text) {
        if (text == null || text.trim().isEmpty()) {
            throw new IllegalArgumentException("empty WireGuard config");
        }
        WireGuardConfig config = new WireGuardConfig();
        String section = "";
        boolean sawPeer = false;
        for (String rawLine : text.split("\\r?\\n")) {
            String line = rawLine.trim();
            if (line.isEmpty() || line.startsWith("#") || line.startsWith(";")) {
                continue;
            }
            if (line.startsWith("[") && line.endsWith("]")) {
                section = line.substring(1, line.length() - 1).trim().toLowerCase(Locale.ROOT);
                if (section.equals("peer")) {
                    if (sawPeer) {
                        throw new IllegalArgumentException("multiple WireGuard peers are not supported");
                    }
                    sawPeer = true;
                }
                continue;
            }
            int separator = line.indexOf('=');
            if (separator <= 0) {
                throw new IllegalArgumentException("invalid WireGuard config line");
            }
            String key = line.substring(0, separator).trim().toLowerCase(Locale.ROOT);
            String value = line.substring(separator + 1).trim();
            if (section.equals("interface")) {
                switch (key) {
                    case "privatekey":
                        config.privateKey = keyHex(value, "PrivateKey");
                        break;
                    case "listenport":
                        config.listenPort = port(value);
                        break;
                    case "address":
                        addCSV(config.addresses, value);
                        break;
                    case "dns":
                        addCSV(config.dns, value);
                        break;
                    default:
                        break;
                }
            } else if (section.equals("peer")) {
                switch (key) {
                    case "publickey":
                        config.peerPublicKey = keyHex(value, "PublicKey");
                        break;
                    case "presharedkey":
                        config.presharedKey = keyHex(value, "PresharedKey");
                        break;
                    case "endpoint":
                        config.endpoint = value;
                        break;
                    case "persistentkeepalive":
                        config.persistentKeepalive = integer(value, "PersistentKeepalive");
                        break;
                    case "allowedips":
                        addCSV(config.allowedIPs, value);
                        break;
                    default:
                        break;
                }
            }
        }
        if (config.privateKey == null || config.peerPublicKey == null || config.endpoint == null
            || config.addresses.isEmpty() || config.allowedIPs.isEmpty()) {
            throw new IllegalArgumentException("WireGuard config is incomplete");
        }
        return config;
    }

    private static void addCSV(List<String> target, String value) {
        for (String item : value.split(",")) {
            String trimmed = item.trim();
            if (!trimmed.isEmpty()) {
                target.add(trimmed);
            }
        }
    }

    private static String keyHex(String value, String name) {
        try {
            byte[] raw = Base64.getDecoder().decode(value);
            if (raw.length != 32) {
                throw new IllegalArgumentException(name + " must be 32 bytes");
            }
            StringBuilder hex = new StringBuilder(64);
            for (byte item : raw) {
                hex.append(String.format(Locale.ROOT, "%02x", item));
            }
            return hex.toString();
        } catch (IllegalArgumentException error) {
            throw new IllegalArgumentException("invalid " + name, error);
        }
    }

    private static String port(String value) {
        int parsed = Integer.parseInt(value);
        if (parsed < 0 || parsed > 65535) {
            throw new IllegalArgumentException("invalid ListenPort");
        }
        return Integer.toString(parsed);
    }

    private static String integer(String value, String name) {
        int parsed = Integer.parseInt(value);
        if (parsed < 0 || parsed > 65535) {
            throw new IllegalArgumentException("invalid " + name);
        }
        return Integer.toString(parsed);
    }
}
