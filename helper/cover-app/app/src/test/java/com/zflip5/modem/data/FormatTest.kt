package com.zflip5.modem.data

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import java.util.Locale

class FormatTest {
    private lateinit var saved: Locale
    @Before fun setUp() { saved = Locale.getDefault(); Locale.setDefault(Locale("vi", "VN")) }
    @After fun tearDown() = Locale.setDefault(saved)

    private fun temp(state: String, safe: Boolean) = TempInfo(40.0, 40.0, state, safe, 44.0, 46.0, false)
    private fun hs(active: Boolean = true, wifi6: Boolean?, band: String?, n: Int) =
        HotspotInfo(active, "Flippost", band, wifi6, n, false, false, null)

    @Test fun bytes() {
        assertEquals("0 B", Format.bytes(0)); assertEquals("1.2 GB", Format.bytes(1_234_567_890))
        assertEquals("12 GB", Format.bytes(12_345_678_901)); assertEquals("88 GB", Format.bytes(87_654_321_099))
        assertEquals("512 MB", Format.bytes(512_000_000))
    }
    @Test fun usage() {
        fun u(limit: Long) = UsageInfo(12_345_678_901, limit, 0, null, "monthly", null)
        assertEquals("12 GB / 100 GB", Format.usage(u(100_000_000_000))); assertEquals("12 GB", Format.usage(u(0)))
    }
    @Test fun mbps() {
        assertEquals("20.0", Format.mbps(2_500_000)); assertEquals("2.4", Format.mbps(300_000))
        assertEquals("19.9", Format.mbps(2_488_000)); assertEquals("0.0", Format.mbps(0))
    }
    @Test fun temp() { assertEquals("41.2°", Format.temp(41.2)); assertEquals("—", Format.temp(null)) }
    @Test fun tempLevel() {
        assertEquals(TempLevel.OK, Format.tempLevel(temp("SAFE", true)))
        assertEquals(TempLevel.WARN, Format.tempLevel(temp("WARM", true)))
        assertEquals(TempLevel.WARN, Format.tempLevel(temp("RECOVERY", true)))
        assertEquals(TempLevel.HOT, Format.tempLevel(temp("HOT", true)))
        assertEquals(TempLevel.HOT, Format.tempLevel(temp("COOLDOWN", true)))
        assertEquals(TempLevel.HOT, Format.tempLevel(temp("SAFE", false)))
    }
    @Test fun hotspotSubtitle() {
        assertEquals("Wi-Fi 6 · 5 GHz · 1 device", Format.hotspotSubtitle(hs(wifi6 = true, band = "5", n = 1)))
        assertEquals("Wi-Fi 6 unavailable · 2.4 + 5 GHz · 5 devices", Format.hotspotSubtitle(hs(wifi6 = false, band = "dual", n = 5)))
        assertEquals("0 devices", Format.hotspotSubtitle(hs(wifi6 = null, band = null, n = 0)))
        assertEquals("Off", Format.hotspotSubtitle(hs(active = false, wifi6 = true, band = "5", n = 2)))
    }
    @Test fun bandLabel() {
        assertEquals("2.4 GHz", Format.bandLabel("2")); assertEquals("5 GHz", Format.bandLabel("5"))
        assertEquals("6 GHz", Format.bandLabel("6")); assertEquals("2.4 + 5 GHz", Format.bandLabel("dual"))
        assertEquals(null, Format.bandLabel(null))
    }
}
