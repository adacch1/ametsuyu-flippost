package com.zflip5.modem.widget

import android.content.Context
import android.view.View
import android.widget.FrameLayout
import android.widget.ProgressBar
import android.widget.TextView
import androidx.test.core.app.ApplicationProvider
import com.zflip5.modem.R
import com.zflip5.modem.data.SummaryParser
import com.zflip5.modem.data.TempLevel
import com.zflip5.modem.fixture
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class WidgetModelTest {
    private fun summary(edit: JSONObject.() -> Unit = {}) = SummaryParser.parse(fixture("summary.json").apply(edit))

    @Test fun downShowsServiceNotRunning() {
        val ctx = ApplicationProvider.getApplicationContext<Context>()
        val v = WidgetRenderer.render(ctx, null, true).apply(ctx, FrameLayout(ctx))
        val status = v.findViewById<TextView>(R.id.widget_status)
        assertEquals(View.VISIBLE, status.visibility)
        assertEquals("service not running", status.text.toString())
        assertEquals(View.GONE, v.findViewById<View>(R.id.widget_values).visibility)
    }

    @Test fun fixtureValues() {
        val m = WidgetRenderer.model(summary(), false)
        assertEquals("1 device", m.devices)
        assertEquals("100.64.12.34", m.wanIp)
        assertEquals("41.2°", m.temp)
        assertEquals(TempLevel.OK, m.tempLevel)
        assertEquals("12 GB / 100 GB", m.data)
        assertEquals(12, m.dataPct)
        assertTrue(m.active)
        assertFalse(m.down)
    }

    @Test fun renderAppliesValues() {
        val ctx = ApplicationProvider.getApplicationContext<Context>()
        val v = WidgetRenderer.render(ctx, summary(), false).apply(ctx, FrameLayout(ctx))
        assertEquals("100.64.12.34", v.findViewById<TextView>(R.id.widget_ip).text.toString())
        assertEquals(12, v.findViewById<ProgressBar>(R.id.widget_bar).progress)
        assertEquals(View.GONE, v.findViewById<View>(R.id.widget_status).visibility)
        assertEquals(0xFF9EE6C3.toInt(), v.findViewById<TextView>(R.id.widget_temp).currentTextColor)
    }

    @Test fun noCapShowsUsedOnly() {
        val m = WidgetRenderer.model(summary { getJSONObject("usage").put("limit_bytes", 0) }, false)
        assertEquals("12 GB", m.data)
        assertEquals(0, m.dataPct)
    }

    @Test fun hotspotOffShowsOff() {
        val m = WidgetRenderer.model(summary { getJSONObject("hotspot").put("active", false) }, false)
        assertEquals("Off", m.devices)
        assertFalse(m.active)
    }

    @Test fun nullWanIp() {
        assertEquals("No WAN", WidgetRenderer.model(summary { put("wan_ip", JSONObject.NULL) }, false).wanIp)
    }
}
