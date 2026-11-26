package com.archielink.companion;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.graphics.drawable.GradientDrawable;
import android.os.Bundle;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

/**
 * TV-004 / TV.md §5.1 guided install: shown when the TV has no usable
 * browser. Never downloads or installs anything itself; every install
 * action stays with the user in a market or the system installer.
 */
public final class GuidedInstallActivity extends Activity {
    private ContentEntryStatus.Result entry;
    private LinearLayout marketSection;
    private LinearLayout manualSection;
    private TextView marketFallback;
    private TextView gmsHint;
    private TextView recheckHint;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        PremiumUi.applyImmersive(this);
        setContentView(buildLayout());
        PremiumUi.animateIn(findViewById(android.R.id.content), PremiumUi.dp(this, 14), 30);
    }

    @Override
    protected void onResume() {
        super.onResume();
        refresh();
    }

    private void refresh() {
        entry = ContentEntryDetector.detect(this);
        boolean hasMarket = entry.marketPackage != null;
        if (marketSection != null) {
            marketSection.setVisibility(hasMarket ? View.VISIBLE : View.GONE);
        }
        if (manualSection != null) {
            manualSection.setVisibility(hasMarket ? View.GONE : View.VISIBLE);
        }
        if (gmsHint != null) {
            gmsHint.setVisibility(entry.gmsPresent ? View.GONE : View.VISIBLE);
        }
        if (marketFallback != null) {
            marketFallback.setVisibility(View.GONE);
        }
        if (recheckHint != null) {
            recheckHint.setVisibility(View.GONE);
        }
        if (entry.browserPackage != null) {
            finish();
        }
    }

    private View buildLayout() {
        FrameLayout root = new FrameLayout(this);
        root.setBackground(new GradientDrawable(
            GradientDrawable.Orientation.TL_BR,
            new int[]{PremiumUi.INK, PremiumUi.INK_DEEP}
        ));
        root.setClipChildren(false);

        LinearLayout page = new LinearLayout(this);
        page.setOrientation(LinearLayout.VERTICAL);
        page.setClipChildren(false);
        page.setClipToPadding(false);
        root.addView(page, new FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT
        ));

        LinearLayout header = new LinearLayout(this);
        header.setOrientation(LinearLayout.VERTICAL);
        header.setPadding(
            PremiumUi.dp(this, 56), PremiumUi.dp(this, 30),
            PremiumUi.dp(this, 56), PremiumUi.dp(this, 18)
        );
        header.setBackgroundColor(PremiumUi.INK);
        header.setElevation(PremiumUi.dp(this, 12));
        header.setClipChildren(false);
        header.setClipToPadding(false);
        page.addView(header, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));

        TextView title = PremiumUi.text(this, "安装电视浏览器", 34, PremiumUi.CHALK, true);
        header.addView(title);

        TextView subtitle = PremiumUi.text(
            this,
            "这台电视还没有可用的浏览器。装好浏览器后，VPN 连接即可访问外网网页。",
            16, PremiumUi.MIST, false
        );
        subtitle.setPadding(0, PremiumUi.dp(this, 8), 0, 0);
        header.addView(subtitle);

        ScrollView scroll = new ScrollView(this);
        scroll.setClipChildren(false);
        scroll.setClipToPadding(false);
        scroll.setPadding(
            PremiumUi.dp(this, 56), PremiumUi.dp(this, 26),
            PremiumUi.dp(this, 56), PremiumUi.dp(this, 30)
        );
        scroll.setVerticalScrollBarEnabled(false);
        page.addView(scroll, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, 0, 1f
        ));

        LinearLayout body = new LinearLayout(this);
        body.setOrientation(LinearLayout.VERTICAL);
        body.setGravity(Gravity.CENTER_HORIZONTAL);
        scroll.addView(body, new ViewGroup.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));

        body.addView(recommendationPanel());
        body.addView(marketPanel());
        body.addView(manualPanel());
        body.addView(gmsNote());
        body.addView(footerButtons());

        return root;
    }

    private View recommendationPanel() {
        LinearLayout panel = new LinearLayout(this);
        panel.setOrientation(LinearLayout.VERTICAL);
        panel.setPadding(
            PremiumUi.dp(this, 26), PremiumUi.dp(this, 22),
            PremiumUi.dp(this, 26), PremiumUi.dp(this, 22)
        );
        panel.setBackground(PremiumUi.rounded(PremiumUi.SURFACE, PremiumUi.STROKE, 1));
        panel.setClipChildren(false);
        panel.setClipToPadding(false);
        bodyParams(panel, 0);

        TextView heading = PremiumUi.text(
            this,
            "推荐浏览器：" + ContentEntryStatus.RECOMMENDED_BROWSER_LABEL,
            22, PremiumUi.SIGNAL, true
        );
        panel.addView(heading);

        TextView note = PremiumUi.text(
            this,
            "开源电视浏览器，专为遥控器设计；VIDAA 真机长期使用验证稳定，"
                + "支持中文搜索与常用视频网站。",
            17, PremiumUi.MIST, false
        );
        note.setLineSpacing(PremiumUi.dp(this, 6), 1f);
        note.setPadding(0, PremiumUi.dp(this, 10), 0, 0);
        panel.addView(note);

        return panel;
    }

    private View marketPanel() {
        marketSection = new LinearLayout(this);
        marketSection.setOrientation(LinearLayout.VERTICAL);
        marketSection.setGravity(Gravity.CENTER_HORIZONTAL);
        marketSection.setClipChildren(false);
        marketSection.setClipToPadding(false);
        bodyParams(marketSection, 24);

        TextView lead = PremiumUi.text(
            this, "检测到应用市场，可以一键跳转搜索安装", 18, PremiumUi.CHALK, false
        );
        marketSection.addView(lead);

        Button search = PremiumUi.primaryButton(this, "打开市场搜索「TV Bro」");
        search.setMinHeight(0);
        search.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                openMarket();
            }
        });
        marketSection.addView(search, new LinearLayout.LayoutParams(
            PremiumUi.dp(this, 420), PremiumUi.dp(this, 54)
        ));
        PremiumUi.setMargins(search, 0, PremiumUi.dp(this, 16), 0, 0);

        marketFallback = PremiumUi.text(
            this, "市场无法打开，请在市场内手动搜索「TV Bro」", 16, PremiumUi.ORANGE, false
        );
        marketFallback.setGravity(Gravity.CENTER);
        marketFallback.setPadding(0, PremiumUi.dp(this, 12), 0, 0);
        marketFallback.setVisibility(View.GONE);
        marketSection.addView(marketFallback);

        return marketSection;
    }

    private View manualPanel() {
        manualSection = new LinearLayout(this);
        manualSection.setOrientation(LinearLayout.VERTICAL);
        manualSection.setGravity(Gravity.CENTER_HORIZONTAL);
        manualSection.setClipChildren(false);
        manualSection.setClipToPadding(false);
        bodyParams(manualSection, 24);

        TextView lead = PremiumUi.text(
            this, "没有检测到应用市场，按以下步骤安装", 18, PremiumUi.CHALK, false
        );
        manualSection.addView(lead);

        String[] steps = new String[]{
            "1. 在电脑浏览器搜索「TV Bro 电视浏览器」，进入官网下载 APK 安装包",
            "2. 把 APK 拷贝到 U 盘，插入电视的 USB 接口",
            "3. 用电视自带的文件管理器或媒体中心打开 APK 安装",
            "4. 系统提示时选择允许「未知来源」安装",
            "5. 安装完成后按返回键回到 ArchieLink，会自动识别"
        };
        int top = 14;
        for (String step : steps) {
            TextView line = PremiumUi.text(this, step, 17, PremiumUi.MIST, false);
            line.setLineSpacing(PremiumUi.dp(this, 5), 1f);
            LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
            );
            params.topMargin = PremiumUi.dp(this, top);
            manualSection.addView(line, params);
            top = 10;
        }

        return manualSection;
    }

    private View gmsNote() {
        gmsHint = PremiumUi.text(
            this,
            "这台电视没有 Google 服务，YouTube 客户端无法安装；"
                + "装好浏览器后可直接使用 YouTube 网页版。",
            16, PremiumUi.MIST, false
        );
        gmsHint.setGravity(Gravity.CENTER);
        gmsHint.setLineSpacing(PremiumUi.dp(this, 5), 1f);
        bodyParams(gmsHint, 26);
        return gmsHint;
    }

    private View footerButtons() {
        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_HORIZONTAL);
        row.setClipChildren(false);
        row.setClipToPadding(false);
        bodyParams(row, 30);

        Button recheck = PremiumUi.secondaryButton(this, "重新检测");
        recheck.setMinHeight(0);
        recheck.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                entry = ContentEntryDetector.detect(GuidedInstallActivity.this);
                if (entry.browserPackage != null) {
                    finish();
                } else {
                    recheckHint.setVisibility(View.VISIBLE);
                }
            }
        });
        row.addView(recheck, new LinearLayout.LayoutParams(
            PremiumUi.dp(this, 180), PremiumUi.dp(this, 52)
        ));

        Button back = PremiumUi.secondaryButton(this, "返回 ArchieLink");
        back.setMinHeight(0);
        back.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                finish();
            }
        });
        row.addView(back, new LinearLayout.LayoutParams(
            PremiumUi.dp(this, 220), PremiumUi.dp(this, 52)
        ));
        PremiumUi.setMargins(back, PremiumUi.dp(this, 16), 0, 0, 0);

        recheckHint = PremiumUi.text(
            this, "仍未检测到浏览器，请按指引完成安装后重试", 16, PremiumUi.ORANGE, false
        );
        recheckHint.setGravity(Gravity.CENTER);
        recheckHint.setPadding(0, PremiumUi.dp(this, 14), 0, 0);
        recheckHint.setVisibility(View.GONE);
        LinearLayout.LayoutParams hintParams = new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        );
        hintParams.topMargin = PremiumUi.dp(this, 16);
        row.addView(recheckHint, hintParams);

        return row;
    }

    private void bodyParams(View view, int topMargin) {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        );
        if (topMargin > 0) {
            params.topMargin = PremiumUi.dp(this, topMargin);
        }
        view.setLayoutParams(params);
    }

    private void openMarket() {
        String market = entry == null ? null : entry.marketPackage;
        try {
            startActivity(ContentEntryDetector.marketSearchIntent(
                ContentEntryStatus.RECOMMENDED_BROWSER_LABEL
            ));
            return;
        } catch (ActivityNotFoundException error) {
            // Fall through to launching the market app directly.
        }
        if (market != null) {
            Intent launch = getPackageManager().getLaunchIntentForPackage(market);
            if (launch != null) {
                try {
                    startActivity(launch);
                    return;
                } catch (ActivityNotFoundException error) {
                    // Fall through to manual guidance.
                }
            }
        }
        marketFallback.setVisibility(View.VISIBLE);
    }
}
