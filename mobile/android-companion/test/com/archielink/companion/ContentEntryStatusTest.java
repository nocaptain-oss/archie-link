package com.archielink.companion;

import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.Set;

public final class ContentEntryStatusTest {
    public static void main(String[] args) {
        // VIDAA-like: enabled system browser, no YouTube client, no GMS.
        ContentEntryStatus.Result vidaa = ContentEntryStatus.decide(
            enabled("com.android.browser", "com.hisense.odinbrowser"),
            "com.android.browser",
            null
        );
        require(
            ContentEntryStatus.READY_WITH_BROWSER.equals(vidaa.status),
            "VIDAA browser not ready"
        );
        require(
            "com.android.browser".equals(vidaa.browserPackage),
            "system browser not picked"
        );
        require(!vidaa.gmsPresent, "GMS wrongly detected");
        require(vidaa.marketPackage == null, "market invented");

        // V1.1.2 amendment: TV-native browsers outrank the mouse-oriented
        // system browser for the ceremony moment (TV.md §4/§5.4).
        ContentEntryStatus.Result tvPriority = ContentEntryStatus.decide(
            enabled("com.android.browser", "com.phlox.tvwebbrowser"),
            "com.android.browser",
            null
        );
        require(
            "com.phlox.tvwebbrowser".equals(tvPriority.browserPackage),
            "Phlox TV browser must outrank system browser"
        );
        ContentEntryStatus.Result broFirst = ContentEntryStatus.decide(
            enabled(
                "io.github.tfederico.tvbro",
                "com.phlox.tvwebbrowser",
                "com.android.browser"
            ),
            "com.android.browser",
            null
        );
        require(
            "io.github.tfederico.tvbro".equals(broFirst.browserPackage),
            "TV Bro must be the top browser pick"
        );

        // pm hide simulation: system browser hidden, nothing else installed.
        ContentEntryStatus.Result hidden = ContentEntryStatus.decide(
            enabled("com.archielink.companion"),
            null,
            null
        );
        require(
            ContentEntryStatus.NEEDS_BROWSER.equals(hidden.status),
            "hidden browser not needs_browser"
        );
        require(hidden.browserPackage == null, "browser invented for hidden state");

        // YouTube client present wins over browser for status.
        ContentEntryStatus.Result app = ContentEntryStatus.decide(
            enabled("com.google.android.youtube.tv", "com.android.chrome"),
            "com.android.chrome",
            null
        );
        require(
            ContentEntryStatus.READY_WITH_APP.equals(app.status),
            "YouTube client not ready_with_app"
        );

        // SmartTube (open-source YouTube client, no GMS needed) is ready_with_app.
        ContentEntryStatus.Result smarttube = ContentEntryStatus.decide(
            enabled("org.smarttube.stable"),
            null,
            null
        );
        require(
            ContentEntryStatus.READY_WITH_APP.equals(smarttube.status),
            "SmartTube not ready_with_app"
        );
        require(smarttube.browserPackage == null, "browser invented for SmartTube");

        // Legacy and F-Droid SmartTube packages still recognized.
        ContentEntryStatus.Result legacySmarttube = ContentEntryStatus.decide(
            enabled("com.liskovsoft.smarttubetv.beta"),
            null,
            null
        );
        require(
            ContentEntryStatus.READY_WITH_APP.equals(legacySmarttube.status),
            "legacy SmartTube not recognized"
        );
        ContentEntryStatus.Result fdroidSmarttube = ContentEntryStatus.decide(
            enabled("app.smarttube.fdroid"),
            null,
            null
        );
        require(
            ContentEntryStatus.READY_WITH_APP.equals(fdroidSmarttube.status),
            "F-Droid SmartTube not recognized"
        );

        // Unknown default handler still counts as a usable browser.
        ContentEntryStatus.Result unknown = ContentEntryStatus.decide(
            enabled("com.example.web", "com.archielink.companion"),
            "com.example.web",
            null
        );
        require(
            ContentEntryStatus.READY_WITH_BROWSER.equals(unknown.status),
            "unknown default handler not accepted"
        );
        require(
            "com.example.web".equals(unknown.browserPackage),
            "unknown handler not reported"
        );

        // TV Bro recognized without being the default handler; V1.1.2
        // amendment: TV-native pick outranks the system browser.
        ContentEntryStatus.Result tvbro = ContentEntryStatus.decide(
            enabled("io.github.tfederico.tvbro", "com.android.browser"),
            null,
            null
        );
        require(
            "io.github.tfederico.tvbro".equals(tvbro.browserPackage),
            "TV Bro should outrank system browser (V1.1.2 amendment)"
        );

        // Hidden system browser, TV Bro still installed: TV Bro picked.
        ContentEntryStatus.Result tvbroOnly = ContentEntryStatus.decide(
            enabled("io.github.tfederico.tvbro"),
            null,
            null
        );
        require(
            "io.github.tfederico.tvbro".equals(tvbroOnly.browserPackage),
            "TV Bro not picked"
        );

        // Market preference: Dangbei over Play, generic market handler as fallback.
        ContentEntryStatus.Result dangbei = ContentEntryStatus.decide(
            enabled("com.dangbei.market", "com.android.vending"),
            null,
            null
        );
        require(
            "com.dangbei.market".equals(dangbei.marketPackage),
            "Dangbei not preferred over Play"
        );
        ContentEntryStatus.Result genericMarket = ContentEntryStatus.decide(
            enabled("com.alt.market"),
            null,
            "com.alt.market"
        );
        require(
            "com.alt.market".equals(genericMarket.marketPackage),
            "generic market handler not accepted"
        );

        // A resolved handler package that is not installed does not count.
        ContentEntryStatus.Result staleHandler = ContentEntryStatus.decide(
            Collections.<String>emptySet(),
            "com.android.browser",
            "com.dangbei.market"
        );
        require(
            ContentEntryStatus.NEEDS_BROWSER.equals(staleHandler.status),
            "stale handler counted as browser"
        );
        require(staleHandler.marketPackage == null, "stale market counted");

        // Empty device: needs_browser, no market.
        ContentEntryStatus.Result empty = ContentEntryStatus.decide(
            Collections.<String>emptySet(),
            null,
            null
        );
        require(
            ContentEntryStatus.NEEDS_BROWSER.equals(empty.status),
            "empty device not needs_browser"
        );

        requireCeremony();
        System.out.println("content-entry-status-test: PASS");
    }

    private static Set<String> enabled(String... packages) {
        return new HashSet<>(Arrays.asList(packages));
    }

    private static void require(boolean value, String message) {
        if (!value) {
            throw new AssertionError(message);
        }
    }

    /** TV.md §5.4: first-connect ceremony trigger matrix. */
    private static void requireCeremony() {
        require(
            ContentEntryStatus.firstVisitCeremonyDue(true, true, false),
            "first healthy connect with browser must fire ceremony"
        );
        require(
            !ContentEntryStatus.firstVisitCeremonyDue(false, true, false),
            "not-yet-healthy connect must not fire ceremony"
        );
        require(
            !ContentEntryStatus.firstVisitCeremonyDue(true, false, false),
            "browser-less device must not fire ceremony"
        );
        require(
            !ContentEntryStatus.firstVisitCeremonyDue(true, true, true),
            "completed ceremony must not fire again"
        );
        require(
            "https://www.google.com".equals(ContentEntryStatus.CEREMONY_URL),
            "ceremony URL drifted from TV.md §5.4"
        );
    }
}
