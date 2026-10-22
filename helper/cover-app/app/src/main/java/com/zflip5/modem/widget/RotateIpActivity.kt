package com.zflip5.modem.widget

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.ui.components.LocalSnackbar
import com.zflip5.modem.ui.components.RotateIpFlow
import com.zflip5.modem.ui.theme.FlippostTheme

/** Target for the widget's Rotate IP button: confirms, rotates, refreshes the widget, then closes. */
class RotateIpActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            FlippostTheme {
                CompositionLocalProvider(LocalSnackbar provides remember { SnackbarHostState() }) {
                    RotateIpFlow(DaemonClient()) { WidgetRefresh.refreshAll(this@RotateIpActivity); finish() }
                }
            }
        }
    }
}
