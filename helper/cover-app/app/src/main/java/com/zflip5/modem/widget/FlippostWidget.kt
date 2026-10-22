package com.zflip5.modem.widget

import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.BroadcastReceiver
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.Summary
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch

/**
 * Exported provider: the system sends APPWIDGET_UPDATE here. It only forwards to the shared
 * refresh path. TOGGLE and REFRESH go to the non-exported [WidgetActionReceiver], so other apps
 * cannot start or stop the hotspot through the widget.
 */
class FlippostWidget : AppWidgetProvider() {
    override fun onEnabled(context: Context) = WidgetRefresh.schedule(context)
    override fun onDisabled(context: Context) = WidgetRefresh.cancel(context)

    override fun onUpdate(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
        WidgetRefresh.schedule(context)
        refresh(context, goAsync(), null)
    }

    companion object {
        const val ACTION_TOGGLE = "com.zflip5.modem.widget.TOGGLE"
        const val ACTION_REFRESH = "com.zflip5.modem.widget.REFRESH"
        internal var client: () -> DaemonClient = { DaemonClient() }
        internal var dispatcher: CoroutineDispatcher = Dispatchers.IO

        /** Optionally runs a tether toggle, then fetches the summary and redraws every widget instance. */
        internal fun refresh(ctx: Context, pending: BroadcastReceiver.PendingResult?, tether: Boolean?) {
            CoroutineScope(dispatcher).launch {
                try {
                    val c = client()
                    if (tether != null) runCatching { c.tether(tether) }
                    var s: Summary? = null
                    var down = false
                    try { s = c.summary() } catch (e: Exception) { down = true }
                    val rv = WidgetRenderer.render(ctx, s, down)
                    AppWidgetManager.getInstance(ctx).updateAppWidget(ComponentName(ctx, FlippostWidget::class.java), rv)
                } finally {
                    pending?.finish()
                }
            }
        }
    }
}

class WidgetActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            FlippostWidget.ACTION_TOGGLE ->
                FlippostWidget.refresh(context, goAsync(), intent.getBooleanExtra(RemoteViews.EXTRA_CHECKED, false))
            FlippostWidget.ACTION_REFRESH -> FlippostWidget.refresh(context, goAsync(), null)
        }
    }
}
