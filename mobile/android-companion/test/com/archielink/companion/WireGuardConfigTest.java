package com.archielink.companion;

public final class WireGuardConfigTest {
    public static void main(String[] args) {
        WireGuardConfig config = WireGuardConfig.parse(
            "[Interface]\n"
                + "PrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\n"
                + "Address = 10.66.0.2/32, fd00::2/128\n"
                + "DNS = 1.1.1.1\n\n"
                + "[Peer]\n"
                + "PublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\n"
                + "AllowedIPs = 0.0.0.0/0, ::/0\n"
                + "Endpoint = 127.0.0.1:51820\n"
        );
        require(config.addresses.size() == 2, "address count");
        require(config.allowedIPs.size() == 2, "allowed IP count");
        require(config.privateKey.length() == 64, "private key conversion");
        require(config.peerPublicKey.length() == 64, "peer key conversion");
        try {
            WireGuardConfig.parse("[Interface]\nPrivateKey = bad\n");
            throw new AssertionError("invalid config accepted");
        } catch (IllegalArgumentException expected) {
            System.out.println("wireguard-config-test: PASS");
        }
    }

    private static void require(boolean value, String message) {
        if (!value) {
            throw new AssertionError(message);
        }
    }
}
