package com.zflip5.modem.widget

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.ActivityInfo
import androidx.compose.ui.test.junit4.v2.createEmptyComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class RotateIpActivityTest {
    @get:Rule val rule = createEmptyComposeRule()

    @Test fun notExportedAndExcludedFromRecents() {
        val ctx = ApplicationProvider.getApplicationContext<Context>()
        val info = ctx.packageManager.getActivityInfo(ComponentName(ctx, RotateIpActivity::class.java), 0)
        assertEquals(false, info.exported)
        assertTrue(info.flags and ActivityInfo.FLAG_EXCLUDE_FROM_RECENTS != 0)
    }

    @Test fun launchShowsConfirm() {
        val ctx = ApplicationProvider.getApplicationContext<Context>()
        ActivityScenario.launch<RotateIpActivity>(Intent(ctx, RotateIpActivity::class.java)).use {
            rule.onNodeWithText("Drops connectivity for about 10 to 30 seconds.").assertExists()
        }
    }
}
