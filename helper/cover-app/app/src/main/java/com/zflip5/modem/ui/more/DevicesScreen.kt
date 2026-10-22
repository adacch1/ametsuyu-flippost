package com.zflip5.modem.ui.more

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.Format
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.ui.components.GlassCard
import com.zflip5.modem.ui.components.ScreenScaffold

private fun rates(down: Long, up: Long) = "↓${Format.mbps(down)} ↑${Format.mbps(up)} Mb/s"

@Composable
private fun Row2(title: String, detail: String, extra: String? = null) {
    GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
        Column {
            Text(title, color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)
            Text(detail, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)
            if (extra != null) Text(extra, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)
        }
    }
}

@Composable
fun DevicesScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onBack: () -> Unit) {
    val s = state.summary
    ScreenScaffold("Devices", onBack) {
        if (s == null) return@ScreenScaffold
        val clients = s.speed.perClient
        if (clients == null) {
            item { Row2("Per-device speed unavailable", rates(s.speed.wanDownBytesS, s.speed.wanUpBytesS)) }
            item { Row2("${s.hotspot.clients} devices connected", "") }
        } else {
            items(clients.size) { i ->
                val c = clients[i]
                Row2(c.ipv4 ?: c.mac, rates(c.downBytesS, c.upBytesS), c.signalDbm?.let { "$it dBm" })
            }
            val d = s.speed.phoneDownBytesS
            val u = s.speed.phoneUpBytesS
            if (d != null && u != null) item { Row2("This phone (approx.)", rates(d, u)) }
        }
    }
}
