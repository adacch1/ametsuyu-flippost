package com.zflip5.modem.ui.components

import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.zflip5.modem.COVER
import com.zflip5.modem.FakeDaemonClient
import com.zflip5.modem.assertNoTargetInCutout
import com.zflip5.modem.assertTargetsAtLeast48dp
import com.zflip5.modem.data.DaemonException
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
class RotateIpFlowTest {
    @get:Rule val rule = createComposeRule()
    private val client = FakeDaemonClient()
    private var finished = 0

    private fun show() = rule.setCover { RotateIpFlow(client) { finished++ } }
    private fun rotate() { rule.onNodeWithTag("confirm_button").performClick(); rule.waitForIdle() }

    @Test fun confirmsBeforeRotating() {
        show()
        rule.onNodeWithText("Rotate IP?").assertExists()
        rule.onNodeWithText("Drops connectivity for about 10 to 30 seconds.").assertExists()
        assertTrue(client.posts.isEmpty())
        rotate()
        assertEquals("/v1/airplane", client.posts.single().first)
        assertEquals("""{"mode":"cycle"}""", client.posts.single().second.toString())
    }

    @Test fun showsNewIp() {
        client.postResponses["/v1/airplane"] = JSONObject("""{"changed":true,"old_ip":"10.0.0.1","new_ip":"10.0.0.2"}""")
        show(); rotate()
        rule.onNodeWithText("New IP 10.0.0.2").assertExists()
        rule.onNodeWithText("Done").performClick()
        assertEquals(1, finished)
    }

    @Test fun unchangedIp() {
        client.postResponses["/v1/airplane"] = JSONObject("""{"changed":false,"old_ip":"10.0.0.1","new_ip":"10.0.0.1"}""")
        show(); rotate()
        rule.onNodeWithText("IP unchanged (10.0.0.1)").assertExists()
    }

    @Test fun busyShowsDaemonError() {
        client.failures["/v1/airplane"] = DaemonException(409, "airplane busy", "busy")
        show(); rotate()
        rule.onNodeWithText("airplane busy").assertExists()
        rule.onNodeWithText("Done").assertExists()
    }

    @Test fun overlayClearsCutout() {
        show()
        rule.assertNoTargetInCutout()
        rule.assertTargetsAtLeast48dp()
    }
}
