package com.archielink.companion;

import java.util.Locale;

final class VpnAppPolicy {
    private static final String[] LOCAL_ONLY = {
        "gallery3d", "camera2", "com.android.calendar", "deskclock",
        "com.android.contacts", "systeminfo", "filemanager", "tvlauncher",
        "launcher", "settings", "systemui", "vpndialogs", "com.android.music",
        "dbappstore", "wechathci", "hicloud", "nfgctv",
        "会员中心", "相册", "相机", "日历", "时钟", "通讯录", "文件管理",
        "系统信息", "桌面", "当贝市场", "电视家"
    };
    private static final String[] DOMESTIC_VIDEO = {
        "miguvideo", "migutv", "咪视界", "咪咕", "youku", "优酷",
        "iqiyi", "qiyi", "爱奇艺", "mgtv", "hunantv", "芒果",
        "bilibili", "哔哩", "sohu", "搜狐", "pptv", "le.com", "gitv",
        "黄河tv", "cntv", "cibn", "wasu", "华数", "futuretv", "litchi",
        "未来电视", "银河", "聚好看", "douyin", "抖音", "kuaishou", "快手"
    };
    private static final String[] NETWORK_APPS = {
        "browser", "chrome", "firefox", "opera", "edge", "webkit", "webview",
        "youtube", "netflix", "hulu", "disney", "primevideo", "prime video",
        "hbo", "twitch", "plex", "vimeo", "dailymotion", "crunchyroll", "spotify",
        "vending", "apkpure", "aptoide", "apkmirror", "aurora", "fdroid",
        "google play", "vpn", "proxy", "accelerator", "hproxy", "shipinjiasu",
        "视频加速", "加速器", "github", "openai", "chatgpt", "telegram",
        "whatsapp", "discord", "reddit", "twitter", "facebook", "instagram",
        "tiktok", "com.google.", "video", "vod", "movie", "film", "cinema",
        "stream", "player"
    };

    private VpnAppPolicy() {
    }

    static boolean isRoutableUid(int uid, int companionUid) {
        int appId = uid % 100000;
        return uid != companionUid && appId >= 10000;
    }

    static boolean shouldCreateEmptyList(boolean initialized, boolean hasSelectionData) {
        return !initialized && !hasSelectionData;
    }

    static boolean shouldOfferApp(
        String packageName, String label, boolean gameCategory
    ) {
        String value = (packageName + " " + label).toLowerCase(Locale.ROOT);
        if (containsAny(value, LOCAL_ONLY) || containsAny(value, DOMESTIC_VIDEO)) {
            return false;
        }
        return gameCategory || containsAny(value, NETWORK_APPS)
            || value.contains("game") || value.contains("游戏");
    }

    private static boolean containsAny(String value, String[] patterns) {
        for (String pattern : patterns) {
            if (value.contains(pattern)) {
                return true;
            }
        }
        return false;
    }
}
