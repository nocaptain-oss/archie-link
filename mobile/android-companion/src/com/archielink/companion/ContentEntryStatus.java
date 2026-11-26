package com.archielink.companion;

import java.util.Arrays;
import java.util.List;
import java.util.Set;

/**
 * Pure content-entry decision logic (no Android imports) so it stays
 * JVM-testable alongside WireGuardConfig / Ipv4Routes / VpnAppPolicy.
 *
 * Contract: docs/platform/TV.md §4 TV-003 (2026-11-26).
 */
public final class ContentEntryStatus {
    public static final String READY_WITH_APP = "ready_with_app";
    public static final String READY_WITH_BROWSER = "ready_with_browser";
    public static final String NEEDS_BROWSER = "needs_browser";

    /** Preference order: system browser first (free), then long-term verified picks. */
    public static final List<String> KNOWN_BROWSERS = Arrays.asList(
        "com.android.browser",
        "io.github.tfederico.tvbro",
        "com.phlox.tvwebbrowser",
        "org.mozilla.firefox_tv",
        "org.mozilla.firefox",
        "com.android.chrome"
    );

    public static final List<String> YOUTUBE_PACKAGES = Arrays.asList(
        "com.google.android.youtube.tv",
        "com.google.android.youtube"
    );

    /** Preference order for guided-install jump: Dangbei before Play on VIDAA-class TVs. */
    public static final List<String> MARKET_PACKAGES = Arrays.asList(
        "com.dangbei.market",
        "com.android.vending"
    );

    public static final String GMS_PACKAGE = "com.google.android.gms";
    public static final String RECOMMENDED_BROWSER_PACKAGE = "io.github.tfederico.tvbro";
    public static final String RECOMMENDED_BROWSER_LABEL = "TV Bro";

    public static final class Result {
        public final String status;
        public final String browserPackage;
        public final String marketPackage;
        public final boolean gmsPresent;

        public Result(String status, String browserPackage, String marketPackage, boolean gmsPresent) {
            this.status = status;
            this.browserPackage = browserPackage;
            this.marketPackage = marketPackage;
            this.gmsPresent = gmsPresent;
        }
    }

    private ContentEntryStatus() {}

    public static Result decide(
        Set<String> enabledPackages,
        String defaultBrowserPackage,
        String marketHandlerPackage
    ) {
        String browser = firstEnabled(KNOWN_BROWSERS, enabledPackages);
        if (browser == null
            && defaultBrowserPackage != null
            && enabledPackages.contains(defaultBrowserPackage)) {
            browser = defaultBrowserPackage;
        }
        boolean youtube = firstEnabled(YOUTUBE_PACKAGES, enabledPackages) != null;
        String market = firstEnabled(MARKET_PACKAGES, enabledPackages);
        if (market == null
            && marketHandlerPackage != null
            && enabledPackages.contains(marketHandlerPackage)) {
            market = marketHandlerPackage;
        }
        boolean gms = enabledPackages.contains(GMS_PACKAGE);
        String status;
        if (youtube) {
            status = READY_WITH_APP;
        } else if (browser != null) {
            status = READY_WITH_BROWSER;
        } else {
            status = NEEDS_BROWSER;
        }
        return new Result(status, browser, market, gms);
    }

    private static String firstEnabled(List<String> candidates, Set<String> enabled) {
        for (String candidate : candidates) {
            if (enabled.contains(candidate)) {
                return candidate;
            }
        }
        return null;
    }
}
