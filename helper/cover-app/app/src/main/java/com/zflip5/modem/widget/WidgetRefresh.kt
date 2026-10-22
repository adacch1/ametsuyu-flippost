package com.zflip5.modem.widget

import android.app.AlarmManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.SystemClock

object WidgetRefresh {
    private fun intent(ctx: Context) = Intent(FlippostWidget.ACTION_REFRESH).setClass(ctx, WidgetActionReceiver::class.java)

    private fun pi(ctx: Context) = PendingIntent.getBroadcast(ctx, 0, intent(ctx), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)

    fun schedule(ctx: Context) {
        val every = AlarmManager.INTERVAL_FIFTEEN_MINUTES
        ctx.getSystemService(AlarmManager::class.java)
            .setInexactRepeating(AlarmManager.ELAPSED_REALTIME, SystemClock.elapsedRealtime() + every, every, pi(ctx))
    }

    fun cancel(ctx: Context) { ctx.getSystemService(AlarmManager::class.java).cancel(pi(ctx)) }

    fun refreshAll(ctx: Context) { ctx.sendBroadcast(intent(ctx)) }
}
