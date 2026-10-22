package com.zflip5.modem.ui.components

import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class SignalBarsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun describesBars() {
        rule.setContent { SignalBars(3) }
        rule.onNodeWithContentDescription("Signal 3 of 4 bars").assertExists()
    }

    @Test fun nullBarsUnknown() {
        rule.setContent { SignalBars(null) }
        rule.onNodeWithContentDescription("Signal level unknown").assertExists()
    }
}
