package com.zflip5.modem.ui.more

import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import com.zflip5.modem.COVER
import com.zflip5.modem.FakeDaemonClient
import com.zflip5.modem.data.SummaryParser
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.fixture
import com.zflip5.modem.setCover
import org.json.JSONObject
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(qualifiers = COVER)
class DevicesScreenTest {
    @get:Rule val rule = createComposeRule()

    private fun show(json: JSONObject = fixture("summary.json")) = rule.setCover {
        DevicesScreen(SummaryState(SummaryParser.parse(json), 100_000, null, false), FakeDaemonClient(), onRefresh = {}, onBack = {})
    }

    @Test fun rowsShowClientsAndPhone() {
        show()
        for (t in listOf("192.168.1.23", "↓19.9 ↑2.4 Mb/s", "-48 dBm", "This phone (approx.)", "↓0.1 ↑0.0 Mb/s")) rule.onNodeWithText(t).assertExists()
    }

    @Test fun perClientUnavailable() {
        show(fixture("summary.json").apply { getJSONObject("speed").put("per_client", JSONObject.NULL) })
        rule.onNodeWithText("Per-device speed unavailable").assertExists()
        rule.onNodeWithText("1 devices connected").assertExists()
        rule.onNodeWithText("This phone (approx.)").assertDoesNotExist()
    }
}
