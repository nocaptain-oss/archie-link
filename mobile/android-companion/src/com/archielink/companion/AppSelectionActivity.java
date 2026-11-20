package com.archielink.companion;

import android.app.Activity;
import android.app.Dialog;
import android.content.DialogInterface;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.StateListDrawable;
import android.os.Bundle;
import android.view.Gravity;
import android.view.KeyEvent;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

import java.util.List;

public final class AppSelectionActivity extends Activity {
    private LinearLayout list;
    private Button addAppsButton;
    private Button selectAllButton;
    private Button selectNoneButton;
    private Button doneButton;
    private AppRow firstRow;
    private AppRow lastRow;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        PremiumUi.applyImmersive(this);
        setContentView(buildLayout());
        PremiumUi.animateIn(findViewById(android.R.id.content), PremiumUi.dp(this, 14), 30);
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
        header.setOrientation(LinearLayout.HORIZONTAL);
        header.setGravity(Gravity.CENTER_VERTICAL);
        header.setPadding(
            PremiumUi.dp(this, 56), PremiumUi.dp(this, 32),
            PremiumUi.dp(this, 56), PremiumUi.dp(this, 18)
        );
        header.setBackgroundColor(PremiumUi.INK);
        header.setElevation(PremiumUi.dp(this, 12));
        header.setClipChildren(false);
        header.setClipToPadding(false);
        page.addView(header, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));

        LinearLayout heading = new LinearLayout(this);
        heading.setOrientation(LinearLayout.VERTICAL);
        header.addView(heading, new LinearLayout.LayoutParams(
            0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f
        ));

        TextView title = PremiumUi.text(this, "转发清单", 34, PremiumUi.CHALK, true);
        heading.addView(title);

        TextView subtitle = PremiumUi.text(
            this,
            "选择需要通过安全连接访问外网的应用，其余应用保持直连；按菜单键可移出清单。",
            16, PremiumUi.MIST, false
        );
        subtitle.setPadding(0, PremiumUi.dp(this, 7), 0, 0);
        heading.addView(subtitle);

        addAppsButton = PremiumUi.secondaryButton(this, "添加应用");
        addAppsButton.setMinHeight(0);
        addAppsButton.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                showAddAppsDialog();
            }
        });
        header.addView(addAppsButton, new LinearLayout.LayoutParams(
            PremiumUi.dp(this, 156), PremiumUi.dp(this, 52)
        ));
        PremiumUi.setMargins(
            addAppsButton, PremiumUi.dp(this, 24), 0, 0, 0
        );
        addAppsButton.setOnKeyListener(new View.OnKeyListener() {
            @Override
            public boolean onKey(View view, int keyCode, KeyEvent event) {
                if (keyCode != KeyEvent.KEYCODE_DPAD_DOWN) {
                    return false;
                }
                if (event.getAction() == KeyEvent.ACTION_DOWN) {
                    if (firstRow != null) {
                        firstRow.requestFocus();
                    } else {
                        selectAllButton.requestFocus();
                    }
                }
                return true;
            }
        });

        ScrollView scroll = new ScrollView(this);
        scroll.setClipChildren(false);
        scroll.setClipToPadding(false);
        scroll.setPadding(
            PremiumUi.dp(this, 50), PremiumUi.dp(this, 18),
            PremiumUi.dp(this, 50), PremiumUi.dp(this, 18)
        );
        list = new LinearLayout(this);
        list.setOrientation(LinearLayout.VERTICAL);
        list.setClipChildren(false);
        scroll.addView(list, new ScrollView.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));
        page.addView(scroll, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, 0, 1f
        ));

        LinearLayout footer = new LinearLayout(this);
        footer.setOrientation(LinearLayout.HORIZONTAL);
        footer.setGravity(Gravity.END);
        footer.setPadding(
            PremiumUi.dp(this, 56), PremiumUi.dp(this, 16),
            PremiumUi.dp(this, 56), PremiumUi.dp(this, 28)
        );
        footer.setBackgroundColor(PremiumUi.INK_DEEP);
        footer.setElevation(PremiumUi.dp(this, 12));
        footer.setClipChildren(false);
        footer.setClipToPadding(false);
        page.addView(footer, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));

        selectAllButton = PremiumUi.secondaryButton(this, "全部转发");
        selectAllButton.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                VpnAppSelection.selectAll(AppSelectionActivity.this);
                rebuildRows();
                selectAllButton.requestFocus();
            }
        });
        footer.addView(selectAllButton, actionParams());
        installFooterNavigation(selectAllButton, 0);

        selectNoneButton = PremiumUi.secondaryButton(this, "全部直连");
        selectNoneButton.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                VpnAppSelection.selectNone(AppSelectionActivity.this);
                rebuildRows();
                selectNoneButton.requestFocus();
            }
        });
        footer.addView(selectNoneButton, actionParams());
        PremiumUi.setMargins(selectNoneButton, PremiumUi.dp(this, 10), 0, 0, 0);
        installFooterNavigation(selectNoneButton, 1);

        doneButton = PremiumUi.primaryButton(this, "完成");
        doneButton.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View view) {
                finish();
            }
        });
        footer.addView(doneButton, actionParams());
        PremiumUi.setMargins(doneButton, PremiumUi.dp(this, 10), 0, 0, 0);
        installFooterNavigation(doneButton, 2);

        rebuildRows();
        root.post(new Runnable() {
            @Override
            public void run() {
                if (firstRow != null) {
                    firstRow.requestFocus();
                } else {
                    selectAllButton.requestFocus();
                }
            }
        });
        return root;
    }

    private LinearLayout.LayoutParams actionParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
            PremiumUi.dp(this, 154), PremiumUi.dp(this, 56)
        );
        return params;
    }

    private void rebuildRows() {
        if (list == null) {
            return;
        }
        list.removeAllViews();
        firstRow = null;
        lastRow = null;
        List<VpnAppSelection.App> apps = VpnAppSelection.visibleApps(this);
        for (int index = 0; index < apps.size(); index++) {
            AppRow row = new AppRow(apps.get(index), index == 0, index == apps.size() - 1);
            LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, PremiumUi.dp(this, 78)
            );
            params.bottomMargin = PremiumUi.dp(this, 8);
            list.addView(row, params);
            if (firstRow == null) {
                firstRow = row;
            }
            lastRow = row;
        }
        if (apps.isEmpty()) {
            TextView empty = PremiumUi.text(
                this, "清单为空。选择右上角“添加应用”加入需要转发的应用。",
                18, PremiumUi.MIST, false
            );
            empty.setGravity(Gravity.CENTER);
            empty.setPadding(0, PremiumUi.dp(this, 34), 0, 0);
            list.addView(empty);
        }
    }

    private final class AppRow extends LinearLayout {
        private final VpnAppSelection.App app;
        private final TextView badge;
        private final boolean first;
        private final boolean last;

        AppRow(VpnAppSelection.App app, boolean first, boolean last) {
            super(AppSelectionActivity.this);
            this.app = app;
            this.first = first;
            this.last = last;
            setOrientation(LinearLayout.HORIZONTAL);
            setGravity(Gravity.CENTER_VERTICAL);
            setPadding(
                PremiumUi.dp(AppSelectionActivity.this, 22), 0,
                PremiumUi.dp(AppSelectionActivity.this, 18), 0
            );
            setFocusable(true);
            setClickable(true);
            setClipChildren(false);

            LinearLayout names = new LinearLayout(AppSelectionActivity.this);
            names.setOrientation(VERTICAL);
            addView(names, new LinearLayout.LayoutParams(
                0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f
            ));

            TextView name = PremiumUi.text(
                AppSelectionActivity.this, app.label, 19, PremiumUi.CHALK, false
            );
            name.setTypeface(android.graphics.Typeface.create(
                "sans-serif-medium", android.graphics.Typeface.NORMAL
            ));
            names.addView(name);

            TextView packageName = PremiumUi.text(
                AppSelectionActivity.this, app.packageName, 13, PremiumUi.MIST, false
            );
            packageName.setPadding(0, PremiumUi.dp(AppSelectionActivity.this, 4), 0, 0);
            names.addView(packageName);

            badge = PremiumUi.text(
                AppSelectionActivity.this, "", 14, PremiumUi.CHALK, false
            );
            badge.setGravity(Gravity.CENTER);
            badge.setPadding(
                PremiumUi.dp(AppSelectionActivity.this, 16), PremiumUi.dp(
                    AppSelectionActivity.this, 7
                ),
                PremiumUi.dp(AppSelectionActivity.this, 16), PremiumUi.dp(
                    AppSelectionActivity.this, 7
                )
            );
            addView(badge);

            setOnClickListener(new View.OnClickListener() {
                @Override
                public void onClick(View view) {
                    boolean enabled = !VpnAppSelection.isEnabled(
                        AppSelectionActivity.this, app.packageName
                    );
                    VpnAppSelection.setEnabled(
                        AppSelectionActivity.this, app.packageName, enabled
                    );
                    render();
                }
            });
            setOnKeyListener(new View.OnKeyListener() {
                @Override
                public boolean onKey(View view, int keyCode, KeyEvent event) {
                    if (keyCode != KeyEvent.KEYCODE_DPAD_UP
                        && keyCode != KeyEvent.KEYCODE_DPAD_DOWN
                        && keyCode != KeyEvent.KEYCODE_DPAD_LEFT
                        && keyCode != KeyEvent.KEYCODE_DPAD_RIGHT
                        && keyCode != KeyEvent.KEYCODE_MENU) {
                        return false;
                    }
                    if (event.getAction() == KeyEvent.ACTION_UP) {
                        return true;
                    }
                    if (event.getAction() != KeyEvent.ACTION_DOWN) {
                        return false;
                    }
                    if (keyCode == KeyEvent.KEYCODE_DPAD_LEFT
                        || keyCode == KeyEvent.KEYCODE_DPAD_RIGHT) {
                        return true;
                    }
                    if (keyCode == KeyEvent.KEYCODE_MENU) {
                        showRowMenu();
                        return true;
                    }
                    if (keyCode == KeyEvent.KEYCODE_DPAD_DOWN && last) {
                        selectAllButton.requestFocus();
                        return true;
                    }
                    if (keyCode == KeyEvent.KEYCODE_DPAD_UP && first) {
                        addAppsButton.requestFocus();
                        return true;
                    }
                    return false;
                }
            });
            PremiumUi.animateFocus(this, 1.012f);
            render();
        }

        private void showRowMenu() {
            Dialog dialog = new Dialog(AppSelectionActivity.this);
            LinearLayout panel = new LinearLayout(AppSelectionActivity.this);
            panel.setOrientation(LinearLayout.VERTICAL);
            panel.setPadding(
                PremiumUi.dp(AppSelectionActivity.this, 32),
                PremiumUi.dp(AppSelectionActivity.this, 26),
                PremiumUi.dp(AppSelectionActivity.this, 32),
                PremiumUi.dp(AppSelectionActivity.this, 28)
            );
            panel.setBackground(PremiumUi.rounded(PremiumUi.SURFACE, PremiumUi.STROKE, 1));
            panel.setClipChildren(false);
            panel.setClipToPadding(false);

            TextView title = PremiumUi.text(
                AppSelectionActivity.this, app.label, 27, PremiumUi.CHALK, true
            );
            panel.addView(title);

            TextView message = PremiumUi.text(
                AppSelectionActivity.this,
                "移出后，可从顶栏“添加应用”中恢复。",
                17, PremiumUi.MIST, false
            );
            message.setPadding(
                0, PremiumUi.dp(AppSelectionActivity.this, 12),
                0, PremiumUi.dp(AppSelectionActivity.this, 24)
            );
            panel.addView(message);

            LinearLayout buttons = new LinearLayout(AppSelectionActivity.this);
            buttons.setGravity(Gravity.END);
            buttons.setClipChildren(false);
            buttons.setClipToPadding(false);

            Button cancel = PremiumUi.secondaryButton(AppSelectionActivity.this, "取消");
            cancel.setMinHeight(0);
            cancel.setOnClickListener(new View.OnClickListener() {
                @Override
                public void onClick(View view) {
                    dialog.dismiss();
                }
            });
            buttons.addView(cancel, dialogButtonParams(132));

            Button remove = PremiumUi.secondaryButton(
                AppSelectionActivity.this, "移出转发清单"
            );
            remove.setMinHeight(0);
            remove.setOnClickListener(new View.OnClickListener() {
                @Override
                public void onClick(View view) {
                    dialog.dismiss();
                    VpnAppSelection.removeFromList(
                        AppSelectionActivity.this, app.packageName
                    );
                    rebuildRows();
                    if (firstRow != null) {
                        firstRow.requestFocus();
                    } else {
                        addAppsButton.requestFocus();
                    }
                }
            });
            buttons.addView(remove, dialogButtonParams(210));
            PremiumUi.setMargins(
                remove, PremiumUi.dp(AppSelectionActivity.this, 10), 0, 0, 0
            );
            panel.addView(buttons);

            dialog.setContentView(panel);
            dialog.setCanceledOnTouchOutside(true);
            PremiumUi.showDialog(dialog, 650);
            cancel.requestFocus();
        }

        private void render() {
            boolean enabled = VpnAppSelection.isEnabled(
                AppSelectionActivity.this, app.packageName
            );
            badge.setText(enabled ? "走 VPN" : "直连");
            badge.setTextColor(enabled ? PremiumUi.INK : PremiumUi.MIST);
            badge.setBackground(PremiumUi.rounded(
                enabled ? PremiumUi.SIGNAL : PremiumUi.SURFACE,
                enabled ? PremiumUi.SIGNAL : PremiumUi.STROKE,
                1
            ));
            setBackground(selectionRowBackground());
        }
    }

    private void showAddAppsDialog() {
        Dialog dialog = new Dialog(this);
        LinearLayout panel = new LinearLayout(this);
        panel.setOrientation(LinearLayout.VERTICAL);
        panel.setBackground(PremiumUi.rounded(PremiumUi.SURFACE, PremiumUi.STROKE, 1));
        panel.setClipChildren(false);
        panel.setClipToPadding(false);

        LinearLayout header = new LinearLayout(this);
        header.setOrientation(LinearLayout.VERTICAL);
        header.setPadding(
            PremiumUi.dp(this, 32), PremiumUi.dp(this, 26),
            PremiumUi.dp(this, 32), PremiumUi.dp(this, 18)
        );
        header.setBackground(PremiumUi.roundedTop(PremiumUi.SURFACE));
        header.setElevation(PremiumUi.dp(this, 12));
        header.setClipChildren(false);
        header.setClipToPadding(false);
        panel.addView(header, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));

        TextView title = PremiumUi.text(this, "添加应用", 30, PremiumUi.CHALK, true);
        header.addView(title);

        List<VpnAppSelection.App> apps = VpnAppSelection.removedApps(this);
        View firstAddRow = null;
        ScrollView scroll = new ScrollView(this);
        scroll.setClipChildren(false);
        scroll.setClipToPadding(false);
        scroll.setPadding(
            PremiumUi.dp(this, 32), PremiumUi.dp(this, 14),
            PremiumUi.dp(this, 32), PremiumUi.dp(this, 22)
        );
        LinearLayout items = new LinearLayout(this);
        items.setOrientation(LinearLayout.VERTICAL);
        scroll.addView(items, new ScrollView.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));
        if (apps.isEmpty()) {
            firstAddRow = addAppsEmptyState(items);
        }
        for (VpnAppSelection.App app : apps) {
            LinearLayout row = new LinearLayout(this);
            row.setOrientation(LinearLayout.HORIZONTAL);
            row.setGravity(Gravity.CENTER_VERTICAL);
            row.setPadding(
                PremiumUi.dp(this, 20), 0,
                PremiumUi.dp(this, 18), 0
            );
            row.setFocusable(true);
            row.setClickable(true);
            row.setBackground(selectionRowBackground());
            row.setClipChildren(false);

            LinearLayout names = new LinearLayout(this);
            names.setOrientation(LinearLayout.VERTICAL);
            row.addView(names, new LinearLayout.LayoutParams(
                0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f
            ));
            names.addView(PremiumUi.text(
                this, app.label, 18, PremiumUi.CHALK, false
            ));
            TextView packageName = PremiumUi.text(
                this, app.packageName, 13, PremiumUi.MIST, false
            );
            packageName.setPadding(0, PremiumUi.dp(this, 4), 0, 0);
            names.addView(packageName);

            TextView add = PremiumUi.text(
                this, "添加", 15, PremiumUi.SIGNAL, false
            );
            add.setGravity(Gravity.CENTER);
            row.addView(add);

            row.setOnClickListener(new View.OnClickListener() {
                @Override
                public void onClick(View view) {
                    VpnAppSelection.addToList(
                        AppSelectionActivity.this, app.packageName
                    );
                    rebuildRows();
                    items.removeView(row);
                    if (items.getChildCount() == 0) {
                        focusWhenReady(addAppsEmptyState(items));
                    } else {
                        focusWhenReady(items.getChildAt(0));
                    }
                }
            });
            PremiumUi.animateFocus(row, 1.01f);

            LinearLayout.LayoutParams rowParams = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, PremiumUi.dp(this, 72)
            );
            rowParams.bottomMargin = PremiumUi.dp(this, 8);
            items.addView(row, rowParams);
            if (firstAddRow == null) {
                firstAddRow = row;
            }
        }
        int height = apps.isEmpty() ? 130 : Math.min(apps.size(), 4) * 80 + 40;
        panel.addView(scroll, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, PremiumUi.dp(this, height)
        ));

        dialog.setContentView(panel);
        dialog.setCanceledOnTouchOutside(true);
        dialog.setOnDismissListener(new DialogInterface.OnDismissListener() {
            @Override
            public void onDismiss(DialogInterface ignored) {
                if (firstRow != null) {
                    firstRow.requestFocus();
                } else {
                    addAppsButton.requestFocus();
                }
            }
        });
        PremiumUi.showDialog(dialog, 760);
        if (firstAddRow != null) {
            focusWhenReady(firstAddRow);
        }
    }

    private TextView addAppsEmptyState(LinearLayout items) {
        TextView empty = PremiumUi.text(
            this, "所有应用均已加入转发清单", 18, PremiumUi.MIST, false
        );
        empty.setGravity(Gravity.CENTER);
        empty.setFocusable(true);
        empty.setPadding(
            0, PremiumUi.dp(this, 34), 0, PremiumUi.dp(this, 34)
        );
        items.addView(empty, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT
        ));
        return empty;
    }

    private void focusWhenReady(final View view) {
        view.post(new Runnable() {
            @Override
            public void run() {
                view.requestFocus();
            }
        });
    }

    private void installFooterNavigation(Button button, int index) {
        button.setMinHeight(0);
        button.setOnKeyListener(new View.OnKeyListener() {
            @Override
            public boolean onKey(View view, int keyCode, KeyEvent event) {
                if (keyCode != KeyEvent.KEYCODE_DPAD_LEFT
                    && keyCode != KeyEvent.KEYCODE_DPAD_RIGHT
                    && keyCode != KeyEvent.KEYCODE_DPAD_UP) {
                    return false;
                }
                if (event.getAction() == KeyEvent.ACTION_UP) {
                    return true;
                }
                if (event.getAction() != KeyEvent.ACTION_DOWN) {
                    return false;
                }
                if (keyCode == KeyEvent.KEYCODE_DPAD_UP) {
                    if (firstRow != null) {
                        lastRow.requestFocus();
                    } else {
                        addAppsButton.requestFocus();
                    }
                    return true;
                }
                if (keyCode == KeyEvent.KEYCODE_DPAD_LEFT && index > 0) {
                    footerButton(index - 1).requestFocus();
                } else if (keyCode == KeyEvent.KEYCODE_DPAD_RIGHT && index < 2) {
                    footerButton(index + 1).requestFocus();
                }
                return true;
            }
        });
    }

    private Button footerButton(int index) {
        if (index == 0) {
            return selectAllButton;
        }
        if (index == 1) {
            return selectNoneButton;
        }
        return doneButton;
    }

    private LinearLayout.LayoutParams dialogButtonParams(int width) {
        return new LinearLayout.LayoutParams(
            PremiumUi.dp(this, width), PremiumUi.dp(this, 54)
        );
    }

    private StateListDrawable selectionRowBackground() {
        StateListDrawable states = new StateListDrawable();
        states.addState(
            new int[]{android.R.attr.state_focused},
            PremiumUi.rounded(PremiumUi.INK_DEEP, PremiumUi.SIGNAL, 2)
        );
        states.addState(
            new int[]{},
            PremiumUi.rounded(PremiumUi.SURFACE, PremiumUi.STROKE, 1)
        );
        return states;
    }
}
