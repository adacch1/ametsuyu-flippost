package com.zflip5.modem.ui.more

import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performTextClearance
import androidx.compose.ui.test.performTextInput
import com.zflip5.modem.COVER
import com.zflip5.modem.FakeDaemonClient
import com.zflip5.modem.data.SummaryParser
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.fixture
import com.zflip5.modem.setCover
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(qualifiers = COVER)
class SystemScreenTest {
    @get:Rule val rule = createComposeRule()
    private val client = FakeDaemonClient().also {
        it.getResponses["/v1/cpu"] = JSONObject("""{"requested":"auto","reason":"cool"}""")
    }

    private fun show(lastOkAtMs: Long = 100_000) = rule.setCover {
        SystemScreen(SummaryState(SummaryParser.parse(fixture("summary.json")), lastOkAtMs, null, false), client, onRefresh = {}, onBack = {}, nowMs = 100_500)
    }

    private fun text(t: String) = rule.onNodeWithText(t).also { rule.onNode(hasScrollAction()).performScrollToNode(hasText(t)) }
    private fun tag(t: String) = rule.onNodeWithTag(t).also { rule.onNode(hasScrollAction()).performScrollToNode(hasTestTag(t)) }

    @Test fun cpuModePosts() {
        show(); rule.waitForIdle()
        text("Running: auto (cool)").assertExists()
        text("Eco").performClick(); rule.waitForIdle()
        val (path, body) = client.posts.single()
        assertEquals("/v1/cpu/mode", path); assertEquals("eco", body!!.getString("mode"))
    }

    @Test fun thermalLimitsShowClamp() {
        client.postResponses["/v1/thermal/limits"] = JSONObject("""{"warn_c":44,"gate_c":48,"clamped":true}""")
        show(); rule.waitForIdle()
        tag("gate_c").performTextClearance()
        rule.onNodeWithTag("gate_c").performTextInput("60")
        text("Apply").performClick(); rule.waitForIdle()
        assertEquals("/v1/thermal/limits", client.posts.single().first)
        assertEquals(60.0, client.posts.single().second!!.getDouble("gate_c"), 0.0)
        rule.onNodeWithText("Clamped to warn 44°, gate 48°").assertExists()
    }

    @Test fun benchSwitchPosts() {
        show(); rule.waitForIdle()
        tag("bench_switch").performClick(); rule.waitForIdle()
        val (path, body) = client.posts.single()
        assertEquals("/v1/thermal/bench", path); assertEquals(true, body!!.getBoolean("enabled"))
    }

    @Test fun airplaneSwitchPosts() {
        show(); rule.waitForIdle()
        tag("airplane_switch").performClick(); rule.waitForIdle()
        val (path, body) = client.posts.single()
        assertEquals("/v1/airplane", path); assertEquals("on", body!!.getString("mode"))
    }

    @Test fun cooldownShowsNote() {
        client.postResponses["/v1/cooldown"] = JSONObject("""{"note":"CPU forced to eco for 5 minutes."}""")
        show(); rule.waitForIdle()
        text("Cooldown").performClick(); rule.waitForIdle()
        rule.onNodeWithText("CPU forced to eco for 5 minutes.").assertExists()
    }

    @Test fun batteryCapIsReadOnly() {
        show(); rule.waitForIdle()
        text("Battery stops charging at 80%. The module sets this at boot.").assertExists()
        assertFalse(client.posts.any { it.first.contains("battery") })
    }

    @Test fun writesDisabledWhenStale() {
        show(lastOkAtMs = 0); rule.waitForIdle()
        text("Eco").assertIsNotEnabled()
        text("Apply").assertIsNotEnabled()
        text("Rotate IP").assertIsNotEnabled()
        tag("bench_switch").assertIsNotEnabled()
        tag("airplane_switch").assertIsNotEnabled()
    }
}
