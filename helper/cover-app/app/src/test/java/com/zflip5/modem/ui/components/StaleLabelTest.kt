package com.zflip5.modem.ui.components

import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.data.*
import org.junit.Assert.assertEquals
import org.junit.Test

class StaleLabelTest {
    private val s = Summary(
        1, "", 0, null, UsageInfo(0, 0, 0, null, "", null), SpeedInfo(1000, 0, 0, null, null, null),
        HotspotInfo(false, null, null, null, 0, false, false, null), null, null,
        TempInfo(null, 0.0, "ok", true, 45.0, 48.0, false), null, TailscaleBrief("not_running", null), null,
    )

    @Test fun staleShowsAge() = assertEquals("Updated 7 s ago", staleText(SummaryState(s, 1_000, null, false), 8_000))
    @Test fun downShowsNotRunning() = assertEquals("Service not running", staleText(SummaryState(s, 1_000, null, true), 8_000))
    @Test fun noSummaryConnecting() = assertEquals("Connecting…", staleText(SummaryState(null, 0, null, false), 8_000))
    @Test fun freshIsLive() = assertEquals("LIVE", staleText(SummaryState(s, 1_000, null, false), 3_000))
}
