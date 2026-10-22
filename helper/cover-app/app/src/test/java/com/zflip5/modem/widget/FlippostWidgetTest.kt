package com.zflip5.modem.widget

import android.app.AlarmManager
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews
import androidx.test.core.app.ApplicationProvider
import com.zflip5.modem.FakeDaemonClient
import com.zflip5.modem.data.DaemonDownException
import kotlinx.coroutines.Dispatchers
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import java.net.ConnectException

@RunWith(RobolectricTestRunner::class)
class FlippostWidgetTest {
    private val ctx = ApplicationProvider.getApplicationContext<Context>()
    private val fake = FakeDaemonClient().also { it.getResponses["/v1/summary"] = it.fixture("summary.json") }

    @Before fun setUp() { FlippostWidget.client = { fake }; FlippostWidget.dispatcher = Dispatchers.Unconfined }
    @After fun tearDown() { FlippostWidget.client = { com.zflip5.modem.data.DaemonClient() }; FlippostWidget.dispatcher = Dispatchers.IO }

    private fun toggle(checked: Boolean) = WidgetActionReceiver().onReceive(
        ctx, Intent(FlippostWidget.ACTION_TOGGLE).putExtra(RemoteViews.EXTRA_CHECKED, checked))

    @Test fun toggleCallsTetherWithCheckedExtra() {
        toggle(false)
        assertEquals("/v1/tether?action=stop", fake.posts.single().first)
        assertEquals(listOf("/v1/summary"), fake.gets)
        toggle(true)
        assertEquals("/v1/tether?action=start", fake.posts.last().first)
    }

    @Test fun toggleWhenDaemonDownStillFinishes() {
        fake.failures["/v1/tether?action=stop"] = DaemonDownException(ConnectException())
        fake.failures["/v1/summary"] = DaemonDownException(ConnectException())
        toggle(false)
        assertEquals(listOf("/v1/summary"), fake.gets)
    }

    @Suppress("DEPRECATION")
    @Test fun scheduleRepeatsEvery15Minutes() {
        WidgetRefresh.schedule(ctx)
        val a = shadowOf(ctx.getSystemService(AlarmManager::class.java)).peekNextScheduledAlarm()!!
        assertEquals(AlarmManager.INTERVAL_FIFTEEN_MINUTES, a.interval)
        assertEquals(AlarmManager.ELAPSED_REALTIME, a.type)
        WidgetRefresh.cancel(ctx)
    }
}
