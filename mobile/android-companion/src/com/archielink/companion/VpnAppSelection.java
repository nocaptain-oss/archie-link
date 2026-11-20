package com.archielink.companion;

import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageManager;
import android.content.pm.ResolveInfo;

import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

final class VpnAppSelection {
    static final class App {
        final int uid;
        final String packageName;
        final String label;
        final boolean suggested;

        App(int uid, String packageName, String label, boolean suggested) {
            this.uid = uid;
            this.packageName = packageName;
            this.label = label;
            this.suggested = suggested;
        }
    }

    private static final String PREFS = "vpn_apps";
    private static final String EXCLUDED = "excluded";
    private static final String REMOVED = "removed";
    private static final String INITIALIZED = "initialized";

    private VpnAppSelection() {
    }

    static List<App> eligibleApps(Context context) {
        PackageManager manager = context.getPackageManager();
        Set<String> candidates = new LinkedHashSet<>();
        for (ApplicationInfo info : manager.getInstalledApplications(0)) {
            if ((info.flags & ApplicationInfo.FLAG_SYSTEM) == 0) {
                candidates.add(info.packageName);
            }
        }
        addLaunchable(candidates, manager, Intent.CATEGORY_LEANBACK_LAUNCHER);
        addLaunchable(candidates, manager, Intent.CATEGORY_LAUNCHER);

        Map<Integer, List<ApplicationInfo>> byUid = new HashMap<>();
        int companionUid = context.getApplicationInfo().uid;
        for (String packageName : candidates) {
            if (packageName.equals(context.getPackageName()) || isControlPackage(packageName)) {
                continue;
            }
            try {
                ApplicationInfo info = manager.getApplicationInfo(packageName, 0);
                if (VpnAppPolicy.isRoutableUid(info.uid, companionUid)) {
                    byUid.computeIfAbsent(info.uid, ignored -> new ArrayList<>()).add(info);
                }
            } catch (PackageManager.NameNotFoundException ignored) {
                // Package was removed during enumeration.
            }
        }

        List<App> result = new ArrayList<>();
        for (Map.Entry<Integer, List<ApplicationInfo>> entry : byUid.entrySet()) {
            List<ApplicationInfo> group = entry.getValue();
            group.sort(Comparator.comparing(info -> info.packageName));
            List<String> labels = new ArrayList<>();
            boolean suggested = false;
            for (ApplicationInfo info : group) {
                String label = info.loadLabel(manager).toString().trim();
                if (!label.isEmpty() && !labels.contains(label)) {
                    labels.add(label);
                }
                if (VpnAppPolicy.shouldOfferApp(
                    info.packageName, label,
                    info.category == ApplicationInfo.CATEGORY_GAME
                )) {
                    suggested = true;
                }
            }
            Collections.sort(labels, String.CASE_INSENSITIVE_ORDER);
            String label = labels.isEmpty() ? group.get(0).packageName : labels.get(0);
            if (labels.size() > 1) {
                label += " 等 " + labels.size() + " 个应用";
            }
            result.add(new App(
                entry.getKey(), group.get(0).packageName, label, suggested
            ));
        }
        result.sort((left, right) ->
            left.label.compareToIgnoreCase(right.label));
        return result;
    }

    static Set<String> selectedPackages(Context context) {
        ensureInitialized(context);
        Set<String> selected = new HashSet<>();
        Set<String> removed = removed(context);
        for (App app : eligibleApps(context)) {
            if (!removed.contains(app.packageName) && isEnabled(context, app.packageName)) {
                selected.add(app.packageName);
            }
        }
        return selected;
    }

    static List<App> visibleApps(Context context) {
        ensureInitialized(context);
        Set<String> removed = removed(context);
        List<App> visible = new ArrayList<>();
        for (App app : eligibleApps(context)) {
            if (!removed.contains(app.packageName)) {
                visible.add(app);
            }
        }
        return visible;
    }

    static List<App> removedApps(Context context) {
        ensureInitialized(context);
        Set<String> removed = removed(context);
        List<App> result = new ArrayList<>();
        for (App app : eligibleApps(context)) {
            if (removed.contains(app.packageName) && app.suggested) {
                result.add(app);
            }
        }
        return result;
    }

    static boolean isEnabled(Context context, String packageName) {
        ensureInitialized(context);
        return !excluded(context).contains(packageName);
    }

    static void setEnabled(Context context, String packageName, boolean enabled) {
        ensureInitialized(context);
        Set<String> excluded = excluded(context);
        if (enabled) {
            excluded.remove(packageName);
        } else {
            excluded.add(packageName);
        }
        saveExcluded(context, excluded);
    }

    static void selectAll(Context context) {
        ensureInitialized(context);
        saveExcluded(context, new HashSet<>());
        saveRemoved(context, new HashSet<>());
    }

    static void selectNone(Context context) {
        ensureInitialized(context);
        Set<String> excluded = new HashSet<>();
        for (App app : eligibleApps(context)) {
            excluded.add(app.packageName);
        }
        saveExcluded(context, excluded);
    }

    static void removeFromList(Context context, String packageName) {
        ensureInitialized(context);
        Set<String> removed = removed(context);
        removed.add(packageName);
        saveRemoved(context, removed);
    }

    static void addToList(Context context, String packageName) {
        ensureInitialized(context);
        Set<String> removed = removed(context);
        removed.remove(packageName);
        saveRemoved(context, removed);
        setEnabled(context, packageName, true);
    }

    static int selectedCount(Context context) {
        return selectedPackages(context).size();
    }

    private static void addLaunchable(
        Set<String> candidates, PackageManager manager, String category
    ) {
        Intent intent = new Intent(Intent.ACTION_MAIN).addCategory(category);
        for (ResolveInfo resolved : manager.queryIntentActivities(intent, 0)) {
            if (resolved.activityInfo != null) {
                candidates.add(resolved.activityInfo.packageName);
            }
        }
    }

    private static Set<String> excluded(Context context) {
        return new HashSet<>(context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .getStringSet(EXCLUDED, Collections.emptySet()));
    }

    private static void ensureInitialized(Context context) {
        SharedPreferences preferences =
            context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        boolean initialized = preferences.getBoolean(INITIALIZED, false);
        boolean hasSelectionData =
            preferences.contains(EXCLUDED) || preferences.contains(REMOVED);
        if (!VpnAppPolicy.shouldCreateEmptyList(initialized, hasSelectionData)) {
            if (!initialized) {
                preferences.edit().putBoolean(INITIALIZED, true).commit();
            }
            pruneStaleEntries(context);
            return;
        }
        Set<String> removed = new HashSet<>();
        for (App app : eligibleApps(context)) {
            removed.add(app.packageName);
        }
        preferences.edit()
            .putStringSet(REMOVED, removed)
            .putBoolean(INITIALIZED, true)
            .commit();
        pruneStaleEntries(context);
    }

    private static void pruneStaleEntries(Context context) {
        Set<String> installed = new HashSet<>();
        for (ApplicationInfo info : context.getPackageManager().getInstalledApplications(0)) {
            installed.add(info.packageName);
        }
        SharedPreferences preferences =
            context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        Set<String> excluded = new HashSet<>(
            preferences.getStringSet(EXCLUDED, Collections.emptySet()));
        Set<String> removed = new HashSet<>(
            preferences.getStringSet(REMOVED, Collections.emptySet()));
        Set<String> keptExcluded = new HashSet<>(excluded);
        keptExcluded.retainAll(installed);
        Set<String> keptRemoved = new HashSet<>(removed);
        keptRemoved.retainAll(installed);
        if (keptExcluded.size() == excluded.size() && keptRemoved.size() == removed.size()) {
            return;
        }
        preferences.edit()
            .putStringSet(EXCLUDED, keptExcluded)
            .putStringSet(REMOVED, keptRemoved)
            .commit();
    }

    private static void saveExcluded(Context context, Set<String> excluded) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .putStringSet(EXCLUDED, excluded)
            .putBoolean(INITIALIZED, true)
            .commit();
    }

    private static Set<String> removed(Context context) {
        return new HashSet<>(context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .getStringSet(REMOVED, Collections.emptySet()));
    }

    private static void saveRemoved(Context context, Set<String> removed) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .putStringSet(REMOVED, removed)
            .putBoolean(INITIALIZED, true)
            .commit();
    }

    private static boolean isControlPackage(String packageName) {
        return packageName.equals("android")
            || packageName.equals("com.android.shell")
            || packageName.equals("com.android.settings")
            || packageName.equals("com.android.systemui")
            || packageName.equals("com.android.vpndialogs")
            // VIDAA's system proxy service must stay on the direct network path.
            || packageName.equals("com.hisense.service.hproxy");
    }
}
