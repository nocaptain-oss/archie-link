package com.archielink.companion;

import android.content.Context;
import android.content.Intent;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageManager;
import android.content.pm.ResolveInfo;
import android.net.Uri;

import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * Android-side scanner for TV-003 content-entry detection.
 * Decision logic lives in ContentEntryStatus (pure, JVM-tested).
 */
public final class ContentEntryDetector {
    private ContentEntryDetector() {}

    public static ContentEntryStatus.Result detect(Context context) {
        PackageManager manager = context.getPackageManager();
        return ContentEntryStatus.decide(
            enabledPackages(manager),
            defaultHandler(manager, "https://www.google.com"),
            defaultHandler(
                manager,
                "market://search?q=" + Uri.encode(ContentEntryStatus.RECOMMENDED_BROWSER_LABEL)
            )
        );
    }

    public static Set<String> enabledPackages(PackageManager manager) {
        Set<String> packages = new HashSet<>();
        List<ApplicationInfo> apps = manager.getInstalledApplications(0);
        for (ApplicationInfo app : apps) {
            if (app.enabled) {
                packages.add(app.packageName);
            }
        }
        return packages;
    }

    /** Best default handler for a URI, or null when nothing eligible resolves. */
    public static String defaultHandler(PackageManager manager, String uri) {
        try {
            Intent intent = new Intent(Intent.ACTION_VIEW, Uri.parse(uri));
            ResolveInfo handler = manager.resolveActivity(intent, PackageManager.MATCH_DEFAULT_ONLY);
            if (handler == null || handler.activityInfo == null) {
                return null;
            }
            return handler.activityInfo.packageName;
        } catch (RuntimeException error) {
            return null;
        }
    }

    public static Intent marketSearchIntent(String query) {
        return new Intent(Intent.ACTION_VIEW, Uri.parse("market://search?q=" + Uri.encode(query)));
    }
}
