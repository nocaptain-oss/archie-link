package com.archielink.companion;

import android.animation.ValueAnimator;
import android.app.Activity;
import android.app.Dialog;
import android.content.Context;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.StateListDrawable;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.WindowManager;
import android.widget.Button;
import android.widget.TextView;

final class PremiumUi {
    static final int INK = Color.rgb(7, 19, 21);
    static final int INK_DEEP = Color.rgb(9, 30, 33);
    static final int SURFACE = Color.rgb(15, 43, 47);
    static final int SIGNAL = Color.rgb(118, 214, 191);
    static final int BRASS = Color.rgb(217, 183, 115);
    static final int ORANGE = Color.rgb(246, 176, 82);
    static final int CHALK = Color.rgb(243, 241, 232);
    static final int MIST = Color.rgb(145, 167, 164);
    static final int STROKE = Color.rgb(44, 72, 73);
    static final int DANGER = Color.rgb(239, 91, 91);

    private PremiumUi() {
    }

    static void applyImmersive(Activity activity) {
        activity.getWindow().setFlags(
            WindowManager.LayoutParams.FLAG_FULLSCREEN,
            WindowManager.LayoutParams.FLAG_FULLSCREEN
        );
        activity.getWindow().getDecorView().setSystemUiVisibility(
            View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                | View.SYSTEM_UI_FLAG_FULLSCREEN
                | View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
        );
    }

    static int dp(Context context, float value) {
        return Math.round(value * context.getResources().getDisplayMetrics().density);
    }

    static TextView text(
        Context context, String value, float size, int color, boolean serif
    ) {
        TextView view = new TextView(context);
        view.setText(value);
        view.setTextSize(size);
        view.setTextColor(color);
        view.setTypeface(
            serif ? Typeface.create("serif", Typeface.NORMAL)
                : Typeface.create("sans-serif", Typeface.NORMAL)
        );
        view.setIncludeFontPadding(false);
        return view;
    }

    static Button primaryButton(Context context, String label) {
        return button(context, label, true);
    }

    static Button secondaryButton(Context context, String label) {
        return button(context, label, false);
    }

    static Button warningButton(Context context, String label) {
        Button button = button(context, label, true);
        button.setBackground(warningBackground());
        button.setTextColor(INK);
        return button;
    }

    static Button button(Context context, String label, boolean primary) {
        Button button = new Button(context);
        button.setText(label);
        button.setTextSize(primary ? 23 : 18);
        button.setAllCaps(false);
        button.setTypeface(Typeface.create("sans-serif-medium", Typeface.NORMAL));
        button.setGravity(android.view.Gravity.CENTER);
        button.setTextColor(primary ? INK : CHALK);
        button.setPadding(dp(context, 24), dp(context, 12), dp(context, 24), dp(context, 12));
        button.setMinHeight(dp(context, primary ? 64 : 52));
        button.setBackground(buttonBackground(context, primary));
        button.setBackgroundTintList(null);
        button.setStateListAnimator(null);
        button.setDefaultFocusHighlightEnabled(false);
        button.setContentDescription(label);
        animateFocus(button, primary ? 1.035f : 1.018f);
        return button;
    }

    static void setMargins(View view, int left, int top, int right, int bottom) {
        ViewGroup.MarginLayoutParams params =
            (ViewGroup.MarginLayoutParams) view.getLayoutParams();
        params.setMargins(left, top, right, bottom);
        view.setLayoutParams(params);
    }

    static void animateIn(View view, float offsetY, long delay) {
        if (!ValueAnimator.areAnimatorsEnabled()) {
            return;
        }
        view.setAlpha(0f);
        view.setTranslationY(offsetY);
        view.animate()
            .alpha(1f)
            .translationY(0f)
            .setStartDelay(delay)
            .setDuration(480)
            .setInterpolator(new android.view.animation.DecelerateInterpolator())
            .start();
    }

    static void showDialog(Dialog dialog, int widthDp) {
        dialog.show();
        Window window = dialog.getWindow();
        if (window == null) {
            return;
        }
        window.setBackgroundDrawableResource(android.R.color.transparent);
        window.setDimAmount(0.62f);
        window.addFlags(WindowManager.LayoutParams.FLAG_DIM_BEHIND);
        View decor = window.getDecorView();
        if (decor instanceof ViewGroup) {
            ((ViewGroup) decor).setClipChildren(false);
            ((ViewGroup) decor).setClipToPadding(false);
        }
        window.setLayout(dp(dialog.getContext(), widthDp), ViewGroup.LayoutParams.WRAP_CONTENT);
    }

    private static StateListDrawable buttonBackground(Context context, boolean primary) {
        GradientDrawable normal = rounded(
            primary ? SIGNAL : Color.TRANSPARENT,
            primary ? SIGNAL : STROKE,
            primary ? 0 : 1
        );
        GradientDrawable focused = rounded(
            primary ? Color.rgb(145, 231, 211) : Color.rgb(19, 55, 58),
            primary ? CHALK : SIGNAL,
            2
        );
        GradientDrawable pressed = rounded(
            primary ? Color.rgb(100, 192, 172) : Color.rgb(12, 42, 45),
            SIGNAL,
            2
        );
        StateListDrawable states = new StateListDrawable();
        states.addState(new int[]{android.R.attr.state_pressed}, pressed);
        states.addState(new int[]{android.R.attr.state_focused}, focused);
        states.addState(new int[]{}, normal);
        return states;
    }

    private static StateListDrawable warningBackground() {
        GradientDrawable normal = rounded(BRASS, BRASS, 0);
        GradientDrawable focused = rounded(Color.rgb(236, 204, 139), CHALK, 2);
        GradientDrawable pressed = rounded(Color.rgb(189, 151, 77), CHALK, 2);
        StateListDrawable states = new StateListDrawable();
        states.addState(new int[]{android.R.attr.state_pressed}, pressed);
        states.addState(new int[]{android.R.attr.state_focused}, focused);
        states.addState(new int[]{}, normal);
        return states;
    }

    static GradientDrawable rounded(int fill, int stroke, int strokeWidth) {
        GradientDrawable drawable = new GradientDrawable();
        drawable.setColor(fill);
        drawable.setCornerRadius(18f);
        if (strokeWidth > 0) {
            drawable.setStroke(strokeWidth, stroke);
        }
        return drawable;
    }

    static GradientDrawable roundedTop(int fill) {
        GradientDrawable drawable = new GradientDrawable();
        drawable.setColor(fill);
        drawable.setCornerRadii(new float[]{18f, 18f, 18f, 18f, 0f, 0f, 0f, 0f});
        return drawable;
    }

    static void animateFocus(View view, float focusedScale) {
        if (!ValueAnimator.areAnimatorsEnabled()) {
            return;
        }
        view.setOnFocusChangeListener(new View.OnFocusChangeListener() {
            @Override
            public void onFocusChange(View focused, boolean hasFocus) {
                focused.animate()
                    .scaleX(hasFocus ? focusedScale : 1f)
                    .scaleY(hasFocus ? focusedScale : 1f)
                    .setDuration(140)
                    .setInterpolator(new android.view.animation.DecelerateInterpolator())
                    .start();
            }
        });
    }
}
