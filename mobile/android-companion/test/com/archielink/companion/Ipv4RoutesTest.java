package com.archielink.companion;

import java.util.List;

public final class Ipv4RoutesTest {
    public static void main(String[] args) {
        List<String> routes = Ipv4Routes.publicRoutes("0.0.0.0", 0);
        require(Ipv4Routes.contains(routes, "1.1.1.1"), "public DNS missing");
        require(Ipv4Routes.contains(routes, "8.8.8.8"), "public address missing");
        require(!Ipv4Routes.contains(routes, "192.168.1.134"), "LAN entered VPN");
        require(!Ipv4Routes.contains(routes, "10.0.0.1"), "RFC1918 entered VPN");
        require(!Ipv4Routes.contains(routes, "100.64.0.1"), "CGNAT entered VPN");
        require(!Ipv4Routes.contains(routes, "127.0.0.1"), "loopback entered VPN");
        require(!Ipv4Routes.contains(routes, "169.254.1.1"), "link-local entered VPN");
        require(!Ipv4Routes.contains(routes, "224.0.0.1"), "multicast entered VPN");
        List<String> testRange = Ipv4Routes.publicRoutes("203.0.113.0", 24);
        require(Ipv4Routes.contains(testRange, "203.0.113.5"), "test range missing");
        System.out.println("ipv4-routes-test: PASS");
    }

    private static void require(boolean value, String message) {
        if (!value) {
            throw new AssertionError(message);
        }
    }
}
