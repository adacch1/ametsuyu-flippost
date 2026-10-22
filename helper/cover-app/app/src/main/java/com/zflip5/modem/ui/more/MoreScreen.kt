package com.zflip5.modem.ui.more

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import com.zflip5.modem.ui.components.GlassCard
import com.zflip5.modem.ui.components.ScreenScaffold

private val screens = listOf("Devices", "Hotspot", "Data", "System", "Tailscale", "Alerts", "Advanced")

@Composable
fun MoreScreen(onOpen: (route: String) -> Unit, onBack: () -> Unit) {
    ScreenScaffold("More", onBack) {
        items(screens.size) { i ->
            val label = screens[i]
            GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 48.dp).clickable(role = Role.Button) { onOpen(label.lowercase()) },
                    verticalAlignment = Alignment.CenterVertically,
                ) { Text(label, color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall) }
            }
        }
    }
}
