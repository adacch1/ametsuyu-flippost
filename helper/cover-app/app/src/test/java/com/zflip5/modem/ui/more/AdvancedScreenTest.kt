package com.zflip5.modem.ui.more

import android.graphics.Bitmap
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import com.zflip5.modem.COVER
import com.zflip5.modem.FakeDaemonClient
import com.zflip5.modem.assertTargetsAtLeast48dp
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
import java.io.ByteArrayOutputStream

@RunWith(RobolectricTestRunner::class)
@Config(qualifiers = COVER)
class AdvancedScreenTest {
    @get:Rule val rule = createComposeRule()
    private val client = FakeDaemonClient()
    private val url = "https://flip.tail1234.ts.net/dashboard"

    private fun show(openReads: Boolean = true, webUrl: String? = url) {
        client.getResponses["/v1/status"] = JSONObject().put("open_reads", openReads).put("open_control", false)
        client.getResponses["/v1/tailscale"] = JSONObject().put("state", "running").put("web_url", webUrl ?: JSONObject.NULL)
        client.getBytesResponses["/v1/qr"] = ByteArrayOutputStream().also {
            Bitmap.createBitmap(8, 8, Bitmap.Config.ARGB_8888).compress(Bitmap.CompressFormat.PNG, 100, it)
        }.toByteArray()
        rule.setCover {
            AdvancedScreen(SummaryState(SummaryParser.parse(fixture("summary.json")), 100_000, null, false), client, onRefresh = {}, onBack = {}, nowMs = 100_500)
        }
        rule.waitForIdle()
    }

    private fun text(t: String) = rule.onNodeWithText(t).also { rule.onNode(hasScrollAction()).performScrollToNode(hasText(t)) }
    private fun tag(t: String) = rule.onNodeWithTag(t).also { rule.onNode(hasScrollAction()).performScrollToNode(hasTestTag(t)) }

    @Test fun restartNeedsConfirm() {
        show()
        text("Restart daemon").performClick(); rule.waitForIdle()
        assertTrue(client.posts.isEmpty())
        rule.onNodeWithText("Restart the daemon?").assertExists()
        rule.onNodeWithTag("confirm_button").performClick(); rule.waitForIdle()
        assertEquals("/v1/service/restart", client.posts.single().first)
        rule.onNodeWithText("Daemon restarting").assertExists()
    }

    @Test fun rebootNeedsConfirm() {
        show()
        text("Reboot phone").performClick(); rule.waitForIdle()
        assertTrue(client.posts.isEmpty())
        rule.onNodeWithText("Reboot the phone?").assertExists()
        rule.onNodeWithText("The hotspot drops for about a minute.").assertExists()
        rule.onNodeWithTag("confirm_button").performClick(); rule.waitForIdle()
        assertEquals("/v1/device/reboot", client.posts.single().first)
    }

    @Test fun openReadsSwitchPosts() {
        show()
        tag("open_reads_switch").performClick(); rule.waitForIdle()
        val (path, body) = client.posts.single()
        assertEquals("/v1/dashboard/open", path); assertEquals(false, body!!.getBoolean("open"))
    }

    @Test fun openControlSwitchPosts() {
        show()
        tag("open_control_switch").performClick(); rule.waitForIdle()
        val (path, body) = client.posts.single()
        assertEquals("/v1/dashboard/control", path); assertEquals(true, body!!.getBoolean("open"))
    }

    @Test fun pairingQrUsesWebUrl() {
        show()
        tag("pair_qr").assertExists()
        text(url).assertExists()
        assertTrue("${client.gets}", "/v1/qr" !in client.gets)
    }

    @Test fun noWebUrlShowsNote() {
        show(webUrl = null)
        text("Tailscale serve is not set up, so there is no web address to pair.").assertExists()
    }

    @Test fun openReadsOffShowsTokenNote() {
        show(openReads = false)
        text("Open reads is off. A browser also needs the read-status token from config.json.").assertExists()
        tag("pair_qr").assertExists()
        assertTrue("/v1/qr" in client.gets)
    }

    @Test fun everyTargetAtLeast48dp() {
        show()
        rule.assertTargetsAtLeast48dp()
    }
}
