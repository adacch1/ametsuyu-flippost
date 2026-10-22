package com.zflip5.modem.ui.more

import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.hasScrollAction
import com.zflip5.modem.COVER
import com.zflip5.modem.assertNoTargetInCutout
import com.zflip5.modem.assertTargetsAtLeast48dp
import com.zflip5.modem.setCover
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(qualifiers = COVER)
class MoreScreenTest {
    @get:Rule val rule = createComposeRule()
    private val opened = mutableListOf<String>()

    @Test fun listsAllSevenScreens() {
        rule.setCover { MoreScreen(onOpen = { opened += it }, onBack = {}) }
        for (l in listOf("Devices", "Hotspot", "Data", "System", "Tailscale", "Alerts", "Advanced")) {
            rule.onNode(hasScrollAction()).performScrollToNode(hasText(l))
            rule.onNodeWithText(l).assertExists()
        }
        rule.onNode(hasScrollAction()).performScrollToNode(hasText("Devices"))
        rule.onNodeWithText("Devices").performClick()
        assertEquals(listOf("devices"), opened)
    }

    @Test fun targetsAreAtLeast48dp() {
        rule.setCover { MoreScreen(onOpen = {}, onBack = {}) }
        rule.assertTargetsAtLeast48dp()
    }
}
