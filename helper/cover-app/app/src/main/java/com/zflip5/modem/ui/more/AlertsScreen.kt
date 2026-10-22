package com.zflip5.modem.ui.more

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.ui.components.ScreenScaffold

@Composable
fun AlertsScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onBack: () -> Unit) {
    ScreenScaffold("Alerts", onBack) {
        item { Text("Alerts are set on the device in the ntfy config.", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium) }
    }
}
