package com.zflip5.modem.ui.more

import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import com.zflip5.modem.COVER
import com.zflip5.modem.FakeDaemonClient
import com.zflip5.modem.data.DaemonException
import com.zflip5.modem.data.SummaryParser
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.fixture
import com.zflip5.modem.setCover
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(qualifiers = COVER)
class HotspotScreenTest {
    @get:Rule val rule = createComposeRule()

    /** Flips the GET /v1/hotspot answer when the dual-band POST succeeds, as the daemon would. */
    private class Fake : FakeDaemonClient() {
        init {
            getResponses["/v1/hotspot"] = JSONObject("""{"active":true,"whitelist":[],"nearby":[],"dual_band":false,"wifi6":true}""")
            getResponses["/v1/presets"] = JSONObject("""{"active":"","auto_switch":false,"presets":[]}""")
            getResponses["/v1/usbtether"] = JSONObject("""{"active":false,"ifaces":[]}""")
        }
        override suspend fun post(path: String, body: JSONObject?, timeoutMs: Int): JSONObject {
            val r = super.post(path, body, timeoutMs)
            if (path == "/v1/hotspot/dual") getResponses["/v1/hotspot"]!!.put("dual_band", body!!.getBoolean("enabled"))
            return r
        }
    }

    private val client = Fake()

    private fun show() = rule.setCover {
        HotspotScreen(SummaryState(SummaryParser.parse(fixture("summary.json")), 100_000, null, false), client, onRefresh = {}, onBack = {}, nowMs = 100_500)
    }

    private fun scrollTo(matcher: androidx.compose.ui.test.SemanticsMatcher) {
        rule.onNode(hasScrollAction()).performScrollToNode(matcher)
    }

    private fun dual() = rule.onNodeWithTag("dual_switch")

    @Test fun dualSwitchDisabledWhileAllowlistSet() {
        client.getResponses["/v1/hotspot"]!!.put("whitelist", org.json.JSONArray(listOf("home")))
        show(); rule.waitForIdle()
        dual().assertIsNotEnabled()
        rule.onNodeWithText("Clear the allowlist first: dual band stops Wi-Fi scanning.").assertExists()
    }

    @Test fun turningOffDualIsNeverBlocked() {
        client.getResponses["/v1/hotspot"]!!.put("dual_band", true).put("whitelist", org.json.JSONArray(listOf("home")))
        show(); rule.waitForIdle()
        dual().assertIsEnabled().assertIsOn()
    }

    @Test fun applyNowSendsApplyNowTrue() {
        client.postResponses["/v1/hotspot/dual"] = JSONObject("""{"dual_band":true,"applied":"now"}""")
        show(); rule.waitForIdle()
        dual().performClick()
        rule.onNodeWithText("Apply now (clients drop about 10 s)").performClick()
        rule.waitForIdle()
        val (path, body) = client.posts.last()
        assertEquals("/v1/hotspot/dual", path)
        assertTrue(body!!.getBoolean("enabled")); assertTrue(body.getBoolean("apply_now"))
        dual().assertIsOn()
    }

    @Test fun atNextStartSendsApplyNowFalse() {
        client.postResponses["/v1/hotspot/dual"] = JSONObject("""{"dual_band":true,"applied":"next_start"}""")
        show(); rule.waitForIdle()
        dual().performClick()
        rule.onNodeWithText("At next start").performClick()
        rule.waitForIdle()
        val (path, body) = client.posts.last()
        assertEquals("/v1/hotspot/dual", path)
        assertTrue(body!!.getBoolean("enabled")); assertEquals(false, body.getBoolean("apply_now"))
    }

    @Test fun cancelLeavesSwitchOff() {
        show(); rule.waitForIdle()
        dual().performClick()
        rule.onNodeWithText("Cancel").performClick()
        rule.waitForIdle()
        dual().assertIsOff()
        assertTrue(client.posts.none { it.first == "/v1/hotspot/dual" })
    }

    @Test fun dual409RollsBackWithSnackbar() {
        val msg = "Turn off preset auto-switch first: dual band stops Wi-Fi scanning."
        client.failures["/v1/hotspot/dual"] = DaemonException(409, msg, "auto_switch")
        show(); rule.waitForIdle()
        dual().performClick()
        rule.onNodeWithText("Apply now (clients drop about 10 s)").performClick()
        rule.waitForIdle()
        rule.onNodeWithText(msg).assertExists()
        dual().assertIsOff()
    }

    @Test fun presetBandChoicesExcludeDual() {
        show(); rule.waitForIdle()
        scrollTo(hasTestTag("usb_switch"))
        rule.onNodeWithText("Add preset").performClick()
        for (t in listOf("2.4 GHz", "5 GHz", "6 GHz")) rule.onNodeWithText(t).assertExists()
        rule.onNodeWithText("Dual").assertDoesNotExist()
    }

    @Test fun usbNotAppliedRollsBack() {
        client.postResponses["/v1/usbtether/toggle?action=start"] = JSONObject("""{"applied":false}""")
        show(); rule.waitForIdle()
        scrollTo(hasTestTag("usb_switch"))
        rule.onNodeWithTag("usb_switch").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("USB tethering did not change").assertExists()
        rule.onNodeWithTag("usb_switch").assertIsOff()
    }
}
