package com.archielink.companion;

public final class VpnAppPolicyTest {
    public static void main(String[] args) {
        require(!VpnAppPolicy.isRoutableUid(1000, 10038), "system UID entered VPN");
        require(!VpnAppPolicy.isRoutableUid(2000, 10038), "shell UID entered VPN");
        require(!VpnAppPolicy.isRoutableUid(10038, 10038), "companion UID entered VPN");
        require(VpnAppPolicy.isRoutableUid(10044, 10038), "user app missing");
        require(VpnAppPolicy.isRoutableUid(1010044, 10038), "secondary user app missing");
        require(VpnAppPolicy.shouldCreateEmptyList(false, false), "first install not empty");
        require(!VpnAppPolicy.shouldCreateEmptyList(true, false), "initialized list reset");
        require(!VpnAppPolicy.shouldCreateEmptyList(false, true), "existing list reset");
        require(VpnAppPolicy.shouldOfferApp(
            "org.chromium.chrome", "Chrome", false
        ), "browser filtered");
        require(VpnAppPolicy.shouldOfferApp(
            "com.tv.shipinjiasu", "视频加速器", false
        ), "video accelerator filtered");
        require(VpnAppPolicy.shouldOfferApp(
            "com.android.vending", "Google Play", false
        ), "overseas app store filtered");
        require(VpnAppPolicy.shouldOfferApp(
            "com.example.tvgame", "Example TV Game",
            true
        ), "game filtered");
        require(!VpnAppPolicy.shouldOfferApp(
            "com.android.gallery3d", "相册", false
        ), "gallery offered");
        require(!VpnAppPolicy.shouldOfferApp(
            "com.android.calendar", "日历", false
        ), "calendar offered");
        require(!VpnAppPolicy.shouldOfferApp(
            "cn.miguvideo.migutv", "咪视界", false
        ), "domestic video offered");
        require(!VpnAppPolicy.shouldOfferApp(
            "com.gitv.dvb.live.shanxi", "黄河TV", false
        ), "domestic live video offered");
        System.out.println("vpn-app-policy-test: PASS");
    }

    private static void require(boolean value, String message) {
        if (!value) {
            throw new AssertionError(message);
        }
    }
}
