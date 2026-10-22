package com.zflip5.modem.data

import com.zflip5.modem.fixture
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class) class SummaryParserTest {
  @Test fun parsesFullSample() { val s = SummaryParser.parse(fixture("summary.json"))
    assertEquals(3, s.signal!!.bars); assertEquals("5G", s.signal!!.display)
    assertEquals(12345678901L, s.usage.periodBytes); assertEquals(87654321099L, s.usage.leftBytes)
    assertEquals(2500000L, s.speed.wanDownBytesS); assertEquals("192.168.1.23", s.speed.perClient!![0].ipv4)
    assertEquals(-48, s.speed.perClient!![0].signalDbm); assertEquals(true, s.hotspot.wifi6)
    assertNull(s.hotspot.dualBandBlock); assertEquals(41.2, s.temperature.c!!, 0.0)
    assertEquals("running", s.tailscale.state); assertEquals(false, s.airplane) }
  @Test fun perClientUnavailableAndNullsParse() { val s = SummaryParser.parse(fixture("summary_nulls.json"))
    assertNull(s.speed.perClient); assertNull(s.speed.phoneDownBytesS); assertNull(s.signal)
    assertNull(s.usage.leftBytes); assertNull(s.hotspot.wifi6); assertNull(s.wanIp)
    assertNull(s.temperature.c); assertNull(s.battery); assertNull(s.airplane); assertNull(s.tailscale.ip) }
  @Test fun parsesTailscaleStatus() { val t = SummaryParser.tailscale(fixture("tailscale_running.json"))
    assertEquals("zflip5.tailXXXX.ts.net", t.dnsName); assertEquals(listOf("100.110.36.27","fd7a:115c:a1e0::1"), t.ips)
    assertTrue(t.serveOk); assertNull(t.error) }
}
