package com.zflip5.modem.ui.home

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.Format
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.data.TempLevel
import com.zflip5.modem.data.writable
import com.zflip5.modem.ui.components.GlassCard
import com.zflip5.modem.ui.components.OptimisticSwitch
import com.zflip5.modem.ui.components.PillButton
import com.zflip5.modem.ui.components.RotateIpFlow
import com.zflip5.modem.ui.components.SignalBars
import com.zflip5.modem.ui.components.StaleLabel
import com.zflip5.modem.ui.components.StripedBar
import com.zflip5.modem.ui.components.rememberNowMs
import com.zflip5.modem.ui.theme.FlippostColors
import com.zflip5.modem.ui.theme.drizzle

private val dim = Color(0x40D5E7FD)
private const val NONE = "—"

@Composable
private fun Dot(color: Color) = Box(Modifier.size(7.dp).background(color, CircleShape))

/** Option C home: header, status chips, live usage card and two pills, all on one 352 x 340 dp screen. */
@Composable
fun HomeScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onMore: () -> Unit, nowMs: Long = rememberNowMs()) {
    val s = state.summary
    val writable = state.writable(nowMs)
    var rotating by remember { mutableStateOf(false) }
    val text = MaterialTheme.colorScheme.onSurface
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val small = MaterialTheme.typography.labelSmall
    val faded = Modifier.alpha(if (writable) 1f else 0.5f)

    Box(Modifier.fillMaxSize().drizzle()) {
        Column(Modifier.fillMaxSize().padding(11.dp)) {
            Row(faded.fillMaxWidth().padding(bottom = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Box(Modifier.size(44.dp).background(MaterialTheme.colorScheme.surface, CircleShape).border(3.dp, dim, CircleShape))
                GlassCard(Modifier.weight(1f)) {
                    Text("Hotspot · ${s?.hotspot?.ssid ?: NONE}", color = text, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(s?.let { Format.hotspotSubtitle(it.hotspot) } ?: NONE, color = muted, style = small, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                OptimisticSwitch(
                    checked = s?.hotspot?.active == true,
                    enabled = writable,
                    onWrite = { client.tether(it); onRefresh() },
                    modifier = Modifier.testTag("hotspot_switch"),
                )
            }

            Row(faded.fillMaxWidth().padding(bottom = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                SignalBars(s?.signal?.bars)
                Text(if (s == null) NONE else s.signal?.display ?: "No signal", color = text, style = small, maxLines = 1)
                if (s != null) {
                    Dot(when (Format.tempLevel(s.temperature)) {
                        TempLevel.OK -> FlippostColors.green
                        TempLevel.WARN -> FlippostColors.amber
                        TempLevel.HOT -> FlippostColors.red
                    })
                }
                Text(s?.let { Format.temp(it.temperature.c ?: it.temperature.rawC) } ?: NONE, color = text, style = small, maxLines = 1)
                Text(if (s == null) NONE else s.wanIp ?: "No WAN", Modifier.weight(1f, fill = false), color = text, style = small, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text("Tailscale", color = text, style = small, maxLines = 1)
                Dot(when (s?.tailscale?.state) { "running" -> FlippostColors.green; "needs_login" -> FlippostColors.amber; else -> dim })
            }

            GlassCard(faded.fillMaxWidth().padding(bottom = 9.dp)) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    StaleLabel(state, nowMs)
                    Text(s?.speed?.let { "↓${Format.mbps(it.wanDownBytesS)} ↑${Format.mbps(it.wanUpBytesS)} Mb/s" } ?: NONE, color = text, style = small, maxLines = 1)
                }
                Spacer(Modifier.size(4.dp))
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    Text("DATA · CYCLE", color = muted, style = small, maxLines = 1)
                    Text(s?.let { Format.usage(it.usage) } ?: NONE, color = text, style = small, maxLines = 1)
                }
                val u = s?.usage
                StripedBar(if (u != null && u.limitBytes > 0) u.periodBytes.toFloat() / u.limitBytes else 0f, Modifier.padding(vertical = 4.dp))
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    Text(if (u == null) NONE else "Today ${Format.bytes(u.todayBytes)}", color = muted, style = small, maxLines = 1)
                    Text(if (u == null) NONE else u.leftBytes?.let { "${Format.bytes(it)} left" } ?: "No cap", color = muted, style = small, maxLines = 1)
                }
            }

            // 165 dp plus the 11 dp screen padding ends at 176 dp, left of the camera cutout.
            Row(Modifier.width(165.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                PillButton("Rotate IP", onClick = { rotating = true }, enabled = writable)
                PillButton("More", onClick = onMore)
            }
        }
        if (rotating) RotateIpFlow(client) { rotating = false; onRefresh() }
    }
}
