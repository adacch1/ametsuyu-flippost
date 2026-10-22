package com.zflip5.modem.ui.more

import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
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
class DualBandDialogTest {
    @get:Rule val rule = createComposeRule()

    @Test fun dialogClearsCutoutAndTargets() {
        rule.setCover { DualBandDialog(enable = true, onChoose = {}, onDismiss = {}) }
        rule.assertNoTargetInCutout()
        rule.assertTargetsAtLeast48dp()
    }

    @Test fun textsAndChoices() {
        val chosen = mutableListOf<Boolean>()
        var dismissed = 0
        rule.setCover { DualBandDialog(enable = false, onChoose = { chosen += it }, onDismiss = { dismissed++ }) }
        rule.onNodeWithText("Turn off dual band?").assertExists()
        rule.onNodeWithText("The hotspot goes back to 5 GHz only.").assertExists()
        rule.onNodeWithText("Apply now (clients drop about 10 s)").performClick()
        rule.onNodeWithText("At next start").performClick()
        rule.onNodeWithText("Cancel").performClick()
        assertEquals(listOf(true, false), chosen)
        assertEquals(1, dismissed)
    }
}
