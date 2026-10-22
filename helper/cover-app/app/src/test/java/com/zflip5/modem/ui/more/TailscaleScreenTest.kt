package com.zflip5.modem.ui.more

import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.zflip5.modem.COVER
import com.zflip5.modem.FakeDaemonClient
import com.zflip5.modem.assertNoTargetInCutout
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
class TailscaleScreenTest {
    @get:Rule val rule = createComposeRule()
    private val client = FakeDaemonClient()
    private val login = "https://login.tailscale.com/a/abc"

    private fun running() = client.fixture("tailscale_running.json")
    private fun needsLogin() = JSONObject("""{"state":"needs_login","serve_ok":false,"online":false,"expired":false,"ips":[],"health":[]}""")

    private fun show() {
        rule.setCover {
            TailscaleScreen(SummaryState(SummaryParser.parse(fixture("summary.json")), 100_000, null, false), client, onRefresh = {}, onBack = {}, nowMs = 100_500)
        }
        if (rule.mainClock.autoAdvance) rule.waitForIdle() else rule.mainClock.advanceTimeBy(100)
    }

    @Test fun statusRows() {
        client.getResponses["/v1/tailscale"] = running().put("expired", true).put("serve_ok", false).put("health", org.json.JSONArray(listOf("DNS is slow")))
        show()
        for (t in listOf("State", "running", "IP", "100.110.36.27", "MagicDNS", "zflip5.tailXXXX.ts.net", "Key expiry", "2027-04-01 (expired)",
            "Web UI", "https://zflip5.tailXXXX.ts.net/", "serve missing", "DNS is slow")) rule.onNodeWithText(t).assertExists()
    }

    @Test fun neverExpiringKey() {
        client.getResponses["/v1/tailscale"] = running().put("key_expiry", JSONObject.NULL)
        show()
        rule.onNodeWithText("Never").assertExists()
    }

    @Test fun loginShowsQrThenClosesWhenRunning() {
        rule.mainClock.autoAdvance = false
        client.getResponses["/v1/tailscale"] = needsLogin()
        client.postResponses["/v1/tailscale/login"] = JSONObject("""{"state":"needs_login","login_url":"$login"}""")
        show()
        rule.onNodeWithText("Log in").performClick()
        rule.mainClock.advanceTimeBy(100)
        rule.onNodeWithTag("login_qr").assertExists()
        rule.onNodeWithText("Scan to sign in").assertExists()
        client.getResponses["/v1/tailscale"] = running()
        rule.mainClock.advanceTimeBy(2_100)
        rule.onNodeWithTag("login_qr").assertDoesNotExist()
        rule.onNodeWithText("Tailscale connected").assertExists()
    }

    @Test fun loginTimesOut() {
        rule.mainClock.autoAdvance = false
        client.getResponses["/v1/tailscale"] = needsLogin()
        client.postResponses["/v1/tailscale/login"] = JSONObject("""{"state":"needs_login","login_url":"$login"}""")
        show()
        rule.onNodeWithText("Log in").performClick()
        rule.mainClock.advanceTimeBy(100)
        rule.onNodeWithTag("login_qr").assertExists()
        rule.mainClock.advanceTimeBy(181_000)
        rule.onNodeWithTag("login_qr").assertDoesNotExist()
        rule.onNodeWithText("Sign-in timed out").assertExists()
    }

    @Test fun machineAuth409ShowsApprovalMessage() {
        client.getResponses["/v1/tailscale"] = needsLogin()
        client.failures["/v1/tailscale/login"] = DaemonException(409, "approve this device", "needs_machine_auth")
        show()
        rule.onNodeWithText("Log in").performClick(); rule.waitForIdle()
        rule.onNodeWithText("Approve this device in the Tailscale admin console.").assertExists()
    }

    @Test fun logoutNeedsConfirm() {
        client.getResponses["/v1/tailscale"] = running()
        show()
        rule.onNodeWithText("Log out").performClick(); rule.waitForIdle()
        assertTrue(client.posts.isEmpty())
        rule.onNodeWithText("Log out of Tailscale?").assertExists()
        rule.onNodeWithTag("confirm_button").performClick(); rule.waitForIdle()
        assertEquals("/v1/tailscale/logout", client.posts.single().first)
    }

    @Test fun notRunningShowsHint() {
        client.getResponses["/v1/tailscale"] = JSONObject("""{"state":"not_running","serve_ok":false,"online":false,"expired":false,"ips":[],"health":[]}""")
        show()
        rule.onNodeWithText("Tailscale is not running").assertExists()
    }

    @Test fun qrOverlayClearsCutout() {
        client.getResponses["/v1/tailscale"] = needsLogin()
        client.postResponses["/v1/tailscale/login"] = JSONObject("""{"state":"needs_login","login_url":"$login"}""")
        show()
        rule.onNodeWithText("Log in").performClick(); rule.waitForIdle()
        rule.onNodeWithTag("login_qr").assertExists()
        rule.assertNoTargetInCutout()
    }
}
