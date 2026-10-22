package com.zflip5.modem.ui.components

import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.hapticfeedback.HapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.isToggleable
import com.zflip5.modem.COVER
import com.zflip5.modem.data.DaemonException
import com.zflip5.modem.setCover
import kotlinx.coroutines.CompletableDeferred
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(qualifiers = COVER)
class OptimisticSwitchTest {
    @get:Rule val rule = createComposeRule()
    private val haptics = mutableListOf<HapticFeedbackType>()
    private val fakeHaptic = object : HapticFeedback {
        override fun performHapticFeedback(hapticFeedbackType: HapticFeedbackType) { haptics += hapticFeedbackType }
    }

    private fun show(enabled: Boolean = true, onWrite: suspend (Boolean) -> Unit) = rule.setCover {
        CompositionLocalProvider(LocalHapticFeedback provides fakeHaptic) {
            OptimisticSwitch(checked = false, enabled = enabled, onWrite = onWrite)
        }
    }

    @Test fun failedWriteRollsBackAndShowsSnackbar() {
        val msg = "clear the allowlist first: dual band stops Wi-Fi scanning"
        show { throw DaemonException(409, msg, "whitelist") }
        rule.onNode(isToggleable()).performClick()
        rule.waitForIdle()
        rule.onNode(isToggleable()).assertIsOff()
        rule.onNodeWithText(msg).assertExists()
        assertEquals(listOf(HapticFeedbackType.ToggleOn, HapticFeedbackType.Reject), haptics)
    }

    @Test fun successShowsTargetImmediately() {
        val gate = CompletableDeferred<Unit>()
        show { gate.await() }
        rule.onNode(isToggleable()).performClick()
        rule.onNode(isToggleable()).assertIsOn()
        gate.complete(Unit)
    }

    @Test fun doubleTapSendsOneWrite() {
        val gate = CompletableDeferred<Unit>()
        var calls = 0
        show { calls++; gate.await() }
        rule.onNode(isToggleable()).performClick()
        rule.onNode(isToggleable()).performClick()
        rule.waitForIdle()
        assertEquals(1, calls)
        gate.complete(Unit)
    }

    @Test fun disabledSwitchIgnoresClicks() {
        var calls = 0
        show(enabled = false) { calls++ }
        rule.onNode(isToggleable()).performClick()
        rule.waitForIdle()
        assertEquals(0, calls)
        rule.onNode(isToggleable()).assertIsNotEnabled()
    }
}
