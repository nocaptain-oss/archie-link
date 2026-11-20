package com.archielink.companion;

import android.animation.ValueAnimator;
import android.content.Context;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.DashPathEffect;
import android.graphics.Paint;
import android.graphics.RadialGradient;
import android.graphics.RectF;
import android.graphics.Shader;
import android.view.View;
import android.view.animation.LinearInterpolator;

final class ConnectionRouteView extends View {
    private final Paint paint = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final float density;
    private String connection = "disconnected";
    private String health = "unavailable";
    private float dashPhase;
    private float globePhase;
    private float livePulse;
    private ValueAnimator dashMotion;
    private ValueAnimator liveMotion;

    ConnectionRouteView(Context context) {
        super(context);
        density = getResources().getDisplayMetrics().density;
        setLayerType(View.LAYER_TYPE_SOFTWARE, null);
    }

    void setState(String connection, String health) {
        if (connection.equals(this.connection) && health.equals(this.health)) {
            return;
        }
        this.connection = connection;
        this.health = health;
        if (connection.equals("connecting") && ValueAnimator.areAnimatorsEnabled()) {
            startDashMotion();
        } else {
            stopDashMotion();
        }
        if (connection.equals("connected") && ValueAnimator.areAnimatorsEnabled()) {
            startLiveMotion();
        } else {
            stopLiveMotion();
        }
        invalidate();
    }

    @Override
    protected void onDetachedFromWindow() {
        stopDashMotion();
        stopLiveMotion();
        super.onDetachedFromWindow();
    }

    @Override
    protected void onDraw(Canvas canvas) {
        super.onDraw(canvas);
        float width = getWidth();
        float height = getHeight();
        float centerY = height * 0.48f;
        float leftX = width * 0.17f;
        float rightX = width * 0.83f;
        int accent = accentColor();

        paint.setDither(true);
        float glowRadiusX = width * 0.48f;
        float glowRadiusY = height * 0.45f;
        float glowCenterY = centerY;
        canvas.save();
        canvas.scale(
            1f, glowRadiusY / glowRadiusX,
            width / 2f, glowCenterY
        );
        paint.setShader(new RadialGradient(
            width / 2f, glowCenterY, glowRadiusX,
            new int[]{
                withAlpha(accent, 42),
                withAlpha(accent, 8),
                Color.TRANSPARENT
            },
            null, Shader.TileMode.CLAMP
        ));
        paint.setStyle(Paint.Style.FILL);
        canvas.drawCircle(width / 2f, glowCenterY, glowRadiusX, paint);
        canvas.restore();
        paint.setShader(null);

        drawRoute(canvas, leftX + dp(39), rightX - dp(44), centerY, accent);
        drawTelevision(canvas, leftX, centerY, accent);
        drawGlobe(canvas, rightX, centerY, accent);
        drawSymbol(canvas, width / 2f, centerY);
    }

    private void drawTelevision(Canvas canvas, float x, float y, int accent) {
        boolean connected = connection.equals("connected");
        RectF inner = new RectF(x - dp(31), y - dp(23), x + dp(31), y + dp(18));
        paint.setStyle(Paint.Style.FILL);
        paint.setColor(withAlpha(accent, connected ? 95 + (int) (livePulse * 35) : 38));
        if (connected) {
            paint.setShadowLayer(
                dp(10 + livePulse * 6), 0, 0, withAlpha(accent, 80)
            );
        }
        canvas.drawRoundRect(inner, dp(8), dp(8), paint);
        paint.setShadowLayer(0, 0, 0, Color.TRANSPARENT);

        paint.setStyle(Paint.Style.STROKE);
        paint.setStrokeWidth(dp(2));
        paint.setColor(withAlpha(PremiumUi.CHALK, connected ? 235 : 205));
        RectF screen = new RectF(
            x - dp(38), y - dp(30), x + dp(38), y + dp(25)
        );
        canvas.drawRoundRect(screen, dp(12), dp(12), paint);
        paint.setStrokeWidth(dp(1.5f));
        canvas.drawLine(x - dp(13), y + dp(36), x + dp(13), y + dp(36), paint);
        canvas.drawLine(x, y + dp(25), x, y + dp(35), paint);
    }

    private void drawGlobe(Canvas canvas, float x, float y, int accent) {
        float radius = dp(43);
        boolean connected = connection.equals("connected");
        paint.setStyle(Paint.Style.STROKE);
        paint.setStrokeWidth(dp(2));
        paint.setColor(withAlpha(accent, connected ? 245 : 220));
        if (connected) {
            paint.setShadowLayer(dp(10), 0, 0, withAlpha(accent, 75));
        }
        canvas.drawCircle(x, y, radius, paint);
        paint.setShadowLayer(0, 0, 0, Color.TRANSPARENT);

        paint.setStyle(Paint.Style.FILL);
        paint.setColor(withAlpha(accent, connected ? 40 : 28));
        canvas.drawCircle(x, y, radius - dp(3), paint);

        paint.setStyle(Paint.Style.STROKE);
        paint.setStrokeWidth(dp(1));
        paint.setColor(withAlpha(accent, 125));
        canvas.drawOval(
            new RectF(x - radius, y - radius * 0.42f, x + radius, y + radius * 0.42f),
            paint
        );
        canvas.drawLine(x - radius, y, x + radius, y, paint);

        float phase = connected ? globePhase : 0f;
        for (int index = 0; index < 3; index++) {
            float depth = Math.abs((float) Math.cos(phase + index * Math.PI / 3f));
            paint.setColor(withAlpha(accent, 55 + (int) (depth * 135)));
            if (depth < 0.08f) {
                canvas.drawLine(x, y - radius, x, y + radius, paint);
            } else {
                canvas.drawOval(new RectF(
                    x - radius * depth, y - radius,
                    x + radius * depth, y + radius
                ), paint);
            }
        }
    }

    private void drawRoute(
        Canvas canvas, float startX, float endX, float y, int accent
    ) {
        paint.setStyle(Paint.Style.STROKE);
        paint.setStrokeCap(Paint.Cap.ROUND);
        paint.setStrokeWidth(dp(connection.equals("connected") ? 3 : 2));
        paint.setColor(withAlpha(accent, connection.equals("connecting") ? 230 : 155));
        if (connection.equals("connected")) {
            paint.setPathEffect(null);
            paint.setShadowLayer(dp(14), 0, 0, withAlpha(accent, 105));
        } else {
            paint.setShadowLayer(0, 0, 0, Color.TRANSPARENT);
            paint.setPathEffect(new DashPathEffect(new float[]{dp(8), dp(8)}, dashPhase));
        }
        canvas.drawLine(startX, y, endX, y, paint);
        paint.setPathEffect(null);
        paint.setShadowLayer(0, 0, 0, Color.TRANSPARENT);
    }

    private void drawSymbol(Canvas canvas, float x, float y) {
        float radius = dp(25);
        boolean connected = connection.equals("connected");
        boolean connecting = connection.equals("connecting");
        int symbolColor = connected ? PremiumUi.SIGNAL
            : connecting ? PremiumUi.BRASS : PremiumUi.DANGER;

        paint.setStyle(Paint.Style.FILL);
        paint.setColor(withAlpha(PremiumUi.SURFACE, 250));
        canvas.drawCircle(x, y, radius, paint);
        paint.setStyle(Paint.Style.STROKE);
        paint.setStrokeWidth(dp(2.5f));
        paint.setColor(symbolColor);
        canvas.drawCircle(x, y, radius, paint);
        paint.setStrokeWidth(dp(3));
        paint.setStrokeCap(Paint.Cap.ROUND);

        if (connected) {
            paint.setColor(PremiumUi.SIGNAL);
            canvas.drawLine(x - dp(9), y, x - dp(2), y + dp(7), paint);
            canvas.drawLine(x - dp(2), y + dp(7), x + dp(11), y - dp(8), paint);
        } else if (connecting) {
            paint.setColor(PremiumUi.BRASS);
            canvas.drawLine(x - dp(8), y - dp(3), x - dp(2), y - dp(3), paint);
            canvas.drawLine(x + dp(2), y + dp(3), x + dp(8), y + dp(3), paint);
        } else {
            paint.setColor(PremiumUi.DANGER);
            canvas.drawLine(x - dp(8), y - dp(8), x + dp(8), y + dp(8), paint);
            canvas.drawLine(x + dp(8), y - dp(8), x - dp(8), y + dp(8), paint);
        }
    }

    private int accentColor() {
        if (connection.equals("connected")) {
            return PremiumUi.SIGNAL;
        }
        if (connection.equals("error")) {
            return PremiumUi.DANGER;
        }
        return connection.equals("connecting") ? PremiumUi.BRASS : PremiumUi.MIST;
    }

    private void startDashMotion() {
        stopDashMotion();
        dashMotion = ValueAnimator.ofFloat(0f, dp(32));
        dashMotion.setDuration(900);
        dashMotion.setRepeatCount(ValueAnimator.INFINITE);
        dashMotion.setInterpolator(new LinearInterpolator());
        dashMotion.addUpdateListener(new ValueAnimator.AnimatorUpdateListener() {
            @Override
            public void onAnimationUpdate(ValueAnimator animation) {
                dashPhase = (float) animation.getAnimatedValue();
                invalidate();
            }
        });
        dashMotion.start();
    }

    private void stopDashMotion() {
        if (dashMotion != null) {
            dashMotion.cancel();
            dashMotion = null;
        }
    }

    private void startLiveMotion() {
        stopLiveMotion();
        liveMotion = ValueAnimator.ofFloat(0f, (float) (Math.PI * 2));
        liveMotion.setDuration(3600);
        liveMotion.setRepeatCount(ValueAnimator.INFINITE);
        liveMotion.setInterpolator(new LinearInterpolator());
        liveMotion.addUpdateListener(new ValueAnimator.AnimatorUpdateListener() {
            @Override
            public void onAnimationUpdate(ValueAnimator animation) {
                globePhase = (float) animation.getAnimatedValue();
                livePulse = (float) ((Math.sin(globePhase) + 1f) / 2f);
                invalidate();
            }
        });
        liveMotion.start();
    }

    private void stopLiveMotion() {
        if (liveMotion != null) {
            liveMotion.cancel();
            liveMotion = null;
        }
        globePhase = 0f;
        livePulse = 0f;
    }

    private float dp(float value) {
        return value * density;
    }

    private static int withAlpha(int color, int alpha) {
        return Color.argb(alpha, Color.red(color), Color.green(color), Color.blue(color));
    }
}
