package com.zflip5.modem.widget

import android.app.AlarmManager
import android.content.Context
import android.content.Intent
import android.view.LayoutInflater
import android.view.View
import android.view.View.MeasureSpec
import android.widget.FrameLayout
import android.widget.RemoteViews
import androidx.test.core.app.ApplicationProvider
import com.zflip5.modem.FakeDaemonClient
import com.zflip5.modem.data.DaemonDownException
import kotlinx.coroutines.Dispatchers
import org.junit.After
import com.zflip5.modem.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import java.net.ConnectException

@RunWith(RobolectricTestRunner::class)
@org.robolectric.annotation.GraphicsMode(org.robolectric.annotation.GraphicsMode.Mode.NATIVE)
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

    @Test fun layoutFitsCompactSize() {
        val d = ctx.resources.displayMetrics.density
        val w = (250 * d).toInt()
        val h = (130 * d).toInt()
        val root = LayoutInflater.from(ctx).inflate(R.layout.widget_flippost, FrameLayout(ctx), false)
        root.findViewById<android.widget.TextView>(R.id.widget_data).text = "38 / 120 GB"
        root.findViewById<android.widget.TextView>(R.id.widget_ip).text = "192.168.43.1"
        root.findViewById<android.widget.TextView>(R.id.widget_temp).text = "41°C"
        root.measure(MeasureSpec.makeMeasureSpec(w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(h, MeasureSpec.EXACTLY))
        root.layout(0, 0, w, h)
        val rotate = root.findViewById<View>(R.id.widget_rotate)
        assertTrue("Rotate IP bottom ${rotate.bottom} exceeds root height $h", rotate.bottom <= h)
        assertTrue("Rotate IP is clipped", rotate.height >= (30 * d).toInt())
        assertTrue("Data row is clipped", root.findViewById<View>(R.id.widget_data).height >= (14 * d).toInt())
        assertEquals(View.VISIBLE, root.findViewById<View>(R.id.widget_switch).visibility)
        assertTrue(root.findViewById<View>(R.id.widget_switch).height >= (48 * d).toInt())
    }

    @Test fun stackIsTightWithFullWidthBar() {
        val d = ctx.resources.displayMetrics.density
        for (hDp in listOf(130, 200)) {
            val w = (250 * d).toInt()
            val h = (hDp * d).toInt()
            val root = LayoutInflater.from(ctx).inflate(R.layout.widget_flippost, FrameLayout(ctx), false)
            root.findViewById<android.widget.TextView>(R.id.widget_data).text = "267 GB / 512 GB"
            root.findViewById<android.widget.TextView>(R.id.widget_ip).text = "192.168.43.1"
            root.findViewById<android.widget.TextView>(R.id.widget_temp).text = "41°C"
            root.measure(MeasureSpec.makeMeasureSpec(w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(h, MeasureSpec.EXACTLY))
            root.layout(0, 0, w, h)
            // Position of a view in root coordinates (all rows sit in the root or in widget_values).
            fun top(v: View) = v.top + ((v.parent as View).takeIf { it !== root }?.top ?: 0)
            fun bottom(v: View) = top(v) + v.height
            val ip = root.findViewById<View>(R.id.widget_ip).parent as View
            val row = root.findViewById<View>(R.id.widget_data_row)
            val bar = root.findViewById<View>(R.id.widget_bar)
            val rotate = root.findViewById<View>(R.id.widget_rotate)
            val content = w - (20 * d).toInt()
            assertTrue("bar width ${bar.width} < 90% of $content at $hDp dp", bar.width >= 0.9 * content)
            assertTrue("bar top ${top(bar)} not below data row bottom ${bottom(row)} at $hDp dp", top(bar) >= bottom(row))
            val max = (10 * d).toInt()
            val rows = listOf(ip, row, bar, rotate)
            for ((a, b) in rows.zipWithNext())
                assertTrue("gap ${top(b) - bottom(a)} > 10 dp at $hDp dp", top(b) - bottom(a) in 0..max)
            assertTrue("Rotate IP outside root at $hDp dp", top(rotate) >= 0 && bottom(rotate) <= h)
        }
    }
}
