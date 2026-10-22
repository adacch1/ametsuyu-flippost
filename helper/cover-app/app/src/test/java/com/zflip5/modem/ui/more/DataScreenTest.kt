package com.zflip5.modem.ui.more

import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
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
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(qualifiers = COVER)
class DataScreenTest {
    @get:Rule val rule = createComposeRule()
    private val client = FakeDaemonClient().also {
        it.getResponses["/v1/usage"] = JSONObject("""{"limit_bytes":100000000000,"period":"weekly","reset_time":"03:30","reset_day":5}""")
    }

    private fun show(lastOkAtMs: Long = 100_000) = rule.setCover {
        DataScreen(SummaryState(SummaryParser.parse(fixture("summary.json")), lastOkAtMs, null, false), client, onRefresh = {}, onBack = {}, nowMs = 100_500)
    }

    private fun type(tag: String, text: String) {
        rule.onNodeWithTag(tag).performScrollTo().performTextClearance()
        if (text.isNotEmpty()) rule.onNodeWithTag(tag).performTextInput(text)
    }

    private fun save(): JSONObject {
        rule.onNodeWithText("Save").performScrollTo().performClick(); rule.waitForIdle()
        val (path, body) = client.posts.last()
        assertEquals("/v1/usage/quota", path)
        return body!!
    }

    @Test fun saveQuotaSendsBytes() {
        show(); rule.waitForIdle()
        type("cap", "120")
        rule.onNodeWithText("Monthly").performScrollTo().performClick()
        type("reset_day", "1"); type("reset_time", "00:00")
        val b = save()
        assertEquals(120_000_000_000L, b.getLong("limit_bytes"))
        assertEquals("monthly", b.getString("period")); assertEquals("00:00", b.getString("reset_time")); assertEquals(1, b.getInt("reset_day"))
    }

    @Test fun prefillsFromUsageAndHidesDayUnlessMonthly() {
        show(); rule.waitForIdle()
        rule.onNodeWithText("100").assertExists()
        rule.onNodeWithTag("reset_day").assertDoesNotExist()
        assertEquals("weekly", save().getString("period"))
    }

    @Test fun emptyCapMeansNoCap() {
        show(); rule.waitForIdle()
        type("cap", "")
        assertEquals(0L, save().getLong("limit_bytes"))
    }

    @Test fun resetNeedsConfirm() {
        show(); rule.waitForIdle()
        rule.onNodeWithText("Reset data usage").performScrollTo().performClick()
        rule.onNodeWithText("Reset data usage?").assertExists()
        assertTrue(client.posts.isEmpty())
        rule.onNodeWithTag("confirm_button").performClick(); rule.waitForIdle()
        assertEquals("/v1/usage/reset", client.posts.single().first)
    }

    @Test fun writesDisabledWhenStale() {
        show(lastOkAtMs = 0); rule.waitForIdle()
        rule.onNodeWithText("Save").performScrollTo().assertIsNotEnabled()
        rule.onNodeWithText("Reset data usage").performScrollTo().assertIsNotEnabled()
    }
}
