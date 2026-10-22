package com.zflip5.modem.ui.home

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.zflip5.modem.COVER
import com.zflip5.modem.FakeDaemonClient
import com.zflip5.modem.assertFitsWithoutScroll
import com.zflip5.modem.assertNoTargetInCutout
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

@RunWith(RobolectricTestRunner::class)
@Config(qualifiers = COVER)
class HomeScreenTest {
    @get:Rule val rule = createComposeRule()
    private val client = FakeDaemonClient()
    private var refreshes = 0

    private fun state(json: JSONObject = fixture("summary.json")) = SummaryState(SummaryParser.parse(json), 100_000, null, false)

    private fun show(s: SummaryState = state(), nowMs: Long = 100_500) = rule.setCover {
        HomeScreen(s, client, onRefresh = { refreshes++ }, onMore = {}, nowMs = nowMs)
    }

    private fun assertLayout() {
        rule.assertFitsWithoutScroll()
        rule.assertNoTargetInCutout()
        rule.assertTargetsAtLeast48dp()
    }

    @Test fun homeFitsWithoutScrolling() { show(); rule.assertFitsWithoutScroll() }
    @Test fun noTapTargetInCameraCutout() { show(); rule.assertNoTargetInCutout() }
    @Test fun everyTapTargetIsAtLeast48dp() { show(); rule.assertTargetsAtLeast48dp() }

    @Test fun pillsStayInLeft176dp() {
        show()
        for (label in listOf("Rotate IP", "More")) {
            val n = rule.onNodeWithText(label).fetchSemanticsNode()
            val limit = 176 * n.layoutInfo.density.density
            assertTrue("$label right edge ${n.boundsInRoot.right} passes $limit", n.boundsInRoot.right <= limit + 0.5f)
        }
    }

    @Test fun rendersOptionCContent() {
        show()
        for (t in listOf("Hotspot · Flippost", "Wi-Fi 6 · 5 GHz · 1 device", "5G", "41.2°", "100.64.12.34", "Tailscale",
            "↓20.0 ↑2.4 Mb/s", "12 GB / 100 GB", "Today 1.2 GB", "88 GB left", "Live")) rule.onNodeWithText(t).assertExists()
        rule.onNode(hasContentDescription("Signal 3 of 4 bars"), useUnmergedTree = true).assertExists()
    }

    @Test fun nullsRenderPlaceholders() {
        show(state(fixture("summary_nulls.json")))
        for (t in listOf("No signal", "No WAN", "No cap")) rule.onNodeWithText(t).assertExists()
        assertLayout()
    }

    private fun assertRendersDashes(f: String) {
        show(state(fixture(f)))
        rule.onNodeWithText("Hotspot · —").assertExists()
        assertLayout()
    }

    @Test fun nullFieldsRender() = assertRendersDashes("summary_allnull.json")
    @Test fun absentFieldsRender() = assertRendersDashes("summary_absent.json")

    @Test fun avatarIsLeftOfHotspotCard() {
        show()
        val a = rule.onNodeWithTag("home_avatar").assertExists().fetchSemanticsNode().boundsInRoot
        val c = rule.onNodeWithText("Hotspot · Flippost").fetchSemanticsNode().boundsInRoot
        assertTrue(a.right <= c.left)
    }

    @Test fun longValuesStillFit() {
        val j = fixture("summary.json")
        j.getJSONObject("hotspot").put("ssid", "Flippost-super-long-ssid-32-char")
        j.put("wan_ip", "255.255.255.255")
        j.getJSONObject("signal").put("operator", "Viettel Telecom Long Name")
        show(state(j))
        assertLayout()
    }

    @Test fun staleStateGreysOutAndDisablesWrites() {
        show(nowMs = 107_000)
        rule.onNodeWithText("Updated 7 s ago").assertExists()
        rule.onNodeWithTag("hotspot_switch").assertIsNotEnabled()
        rule.onNodeWithText("Rotate IP").assertIsNotEnabled()
        rule.onNodeWithText("More").assertIsEnabled()
    }

    @Test fun downShowsServiceNotRunning() {
        show(state().copy(down = true))
        rule.onNodeWithText("Service not running").assertExists()
        rule.onNodeWithTag("hotspot_switch").assertIsNotEnabled()
        assertLayout()
    }

    @Test fun switchTogglesHotspotOptimistically() {
        client.postResponses["/v1/tether?action=stop"] = JSONObject("""{"applied":true,"action":"stop","active":false}""")
        val on = state()
        val off = on.copy(summary = on.summary!!.copy(hotspot = on.summary.hotspot.copy(active = false)))
        var current by mutableStateOf(on)
        rule.setCover { HomeScreen(current, client, onRefresh = { refreshes++; current = off }, onMore = {}, nowMs = 100_500) }
        rule.onNodeWithTag("hotspot_switch").performClick()
        rule.waitForIdle()
        rule.onNodeWithTag("hotspot_switch").assertIsOff()
        assertEquals("/v1/tether?action=stop", client.posts.single().first)
        assertEquals(1, refreshes)
    }

    @Test fun cutoutClearWithOverlayAndSnackbar() {
        show()
        rule.onNodeWithText("Rotate IP").performClick()
        rule.onNodeWithText("Rotate IP?").assertExists()
        rule.assertNoTargetInCutout()
        rule.assertTargetsAtLeast48dp()
    }

    @Test fun switchSitsInsideHotspotCard() {
        show()
        val card = rule.onNodeWithTag("hotspot_card").fetchSemanticsNode().boundsInRoot
        val sw = rule.onNodeWithTag("hotspot_switch").fetchSemanticsNode().boundsInRoot
        assertTrue("switch $sw outside card $card", sw.left >= card.left && sw.right <= card.right && sw.top >= card.top && sw.bottom <= card.bottom)
        assertEquals("switch is vertically centred", card.center.y, sw.center.y, 1f)
    }

    @Test fun pillsAreBalancedAndSingleLine() {
        show()
        val rotate = rule.onNodeWithText("Rotate IP").fetchSemanticsNode()
        val more = rule.onNodeWithText("More").fetchSemanticsNode()
        for (n in listOf(rotate, more)) {
            val results = mutableListOf<TextLayoutResult>()
            n.config.getOrNull(SemanticsActions.GetTextLayoutResult)!!.action!!(results)
            assertEquals(1, results.single().lineCount)
        }
        val d = rotate.layoutInfo.density.density
        assertEquals(rotate.boundsInRoot.width, more.boundsInRoot.width, d)
        assertEquals(rotate.boundsInRoot.height, more.boundsInRoot.height, d)
    }

    @Test @Config(qualifiers = "w1200dp-h700dp-land-xhdpi")
    fun wideWindowKeepsDesignedWidthCentred() {
        show()
        val root = rule.onRoot().fetchSemanticsNode()
        val d = root.layoutInfo.density.density
        val c = rule.onNodeWithTag("home_content").fetchSemanticsNode().boundsInRoot
        assertTrue("content ${c.width} px is wider than 352 dp", c.width <= 352 * d + 1)
        assertEquals(root.size.width / 2f, c.center.x, 2 * d)
    }
}
