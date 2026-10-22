package com.zflip5.modem.widget

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.graphics.Typeface
import android.text.SpannableString
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.text.style.StyleSpan
import android.view.View
import android.widget.RemoteViews
import com.zflip5.modem.CoverKioskActivity
import com.zflip5.modem.R
import com.zflip5.modem.data.Format
import com.zflip5.modem.data.Summary
import com.zflip5.modem.data.TempLevel
import kotlin.math.roundToInt

data class WidgetModel(
    val down: Boolean, val active: Boolean, val devices: String, val wanIp: String,
    val temp: String, val tempLevel: TempLevel, val data: String, val dataPct: Int,
)

object WidgetRenderer {
    private const val OK = 0xFF9EE6C3.toInt()
    private const val WARN = 0xFFFFD49A.toInt()
    private const val HOT = 0xFFFF9AA6.toInt()

    fun model(s: Summary?, down: Boolean): WidgetModel {
        if (down || s == null) return WidgetModel(true, false, "", "", "", TempLevel.OK, "", 0)
        val active = s.hotspot.active
        val cap = s.usage.limitBytes
        val pct = if (cap > 0) (100.0 * s.usage.periodBytes / cap).roundToInt().coerceIn(0, 100) else 0
        return WidgetModel(
            down = false, active = active,
            devices = if (active) Format.devices(s.hotspot.clients) else "Off",
            wanIp = s.wanIp ?: "No WAN",
            temp = Format.temp(s.temperature.c ?: s.temperature.rawC),
            tempLevel = Format.tempLevel(s.temperature),
            data = Format.usage(s.usage), dataPct = pct,
        )
    }

    /** Bold bright amount, dim remainder: "266 GB" stays bold in "266 GB / 512 GB". */
    private fun dataText(d: String): CharSequence {
        val cut = d.indexOf(" / ").let { if (it < 0) d.length else it }
        return SpannableString(d).apply {
            setSpan(StyleSpan(Typeface.BOLD), 0, cut, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
            setSpan(ForegroundColorSpan(0xFFFEF7EE.toInt()), 0, cut, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        }
    }

    fun render(ctx: Context, s: Summary?, down: Boolean): RemoteViews {
        val m = model(s, down)
        val rv = RemoteViews(ctx.packageName, R.layout.widget_flippost)
        rv.setViewVisibility(R.id.widget_status, if (m.down) View.VISIBLE else View.GONE)
        rv.setViewVisibility(R.id.widget_values, if (m.down) View.GONE else View.VISIBLE)
        if (m.down) rv.setTextViewText(R.id.widget_status, "service not running")
        else {
            rv.setTextViewText(R.id.widget_devices, m.devices)
            rv.setTextViewText(R.id.widget_ip, m.wanIp)
            rv.setTextViewText(R.id.widget_temp, m.temp)
            rv.setTextColor(R.id.widget_temp, when (m.tempLevel) { TempLevel.OK -> OK; TempLevel.WARN -> WARN; TempLevel.HOT -> HOT })
            rv.setTextViewText(R.id.widget_data, dataText(m.data))
            val dot = when (m.tempLevel) { TempLevel.OK -> R.drawable.widget_dot_ok; TempLevel.WARN -> R.drawable.widget_dot_warn; TempLevel.HOT -> R.drawable.widget_dot_hot }
            rv.setTextViewCompoundDrawablesRelative(R.id.widget_temp, dot, 0, 0, 0)
            rv.setProgressBar(R.id.widget_bar, 100, m.dataPct, false)
        }
        rv.setImageViewResource(R.id.widget_switch, if (m.active) R.drawable.widget_switch_on else R.drawable.widget_switch_off)
        rv.setContentDescription(R.id.widget_switch, if (m.active) "Hotspot on" else "Hotspot off")
        // The click carries the state to switch to; the intent stays explicit and the PendingIntent immutable.
        val toggle = PendingIntent.getBroadcast(ctx, 1,
            Intent(FlippostWidget.ACTION_TOGGLE).setClass(ctx, WidgetActionReceiver::class.java)
                .putExtra(RemoteViews.EXTRA_CHECKED, !m.active),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        rv.setOnClickPendingIntent(R.id.widget_switch, toggle)
        rv.setOnClickPendingIntent(R.id.widget_rotate, PendingIntent.getActivity(ctx, 2,
            Intent(ctx, RotateIpActivity::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE))
        rv.setOnClickPendingIntent(R.id.widget_root, PendingIntent.getActivity(ctx, 3,
            Intent(ctx, CoverKioskActivity::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE))
        return rv
    }
}
