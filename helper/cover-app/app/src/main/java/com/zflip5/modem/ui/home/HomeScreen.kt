package com.zflip5.modem.ui.home

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.sp
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.zflip5.modem.R
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

/** One status item in its own rounded container, as in the mockup's `.chip`. */
@Composable
private fun Chip(content: @Composable RowScope.() -> Unit) {
    val shape = RoundedCornerShape(12.dp)
    Row(
        Modifier.background(Color(0x14D5E7FD), shape).border(1.dp, Color(0x24D5E7FD), shape).padding(horizontal = 9.dp, vertical = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(5.dp),
        content = content,
    )
}

/** Option C home: header, status chips, live usage card and two pills, all on one 352 x 340 dp screen. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun HomeScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onMore: () -> Unit, nowMs: Long = rememberNowMs()) {
    val s = state.summary
    val writable = state.writable(nowMs)
    var rotating by remember { mutableStateOf(false) }
    val text = MaterialTheme.colorScheme.onSurface
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val label = MaterialTheme.typography.labelSmall
    val caption = MaterialTheme.typography.bodySmall
    val chipText = caption.copy(fontWeight = FontWeight.Bold)
    val value = MaterialTheme.typography.bodyMedium.copy(fontSize = 13.sp, fontWeight = FontWeight.ExtraBold)
    val faded = Modifier.alpha(if (writable) 1f else 0.5f)

    Box(Modifier.fillMaxSize().drizzle()) {
        // The design is 352 dp wide; wider windows centre it and the drizzle still fills the window.
        Column(Modifier.align(Alignment.TopCenter).widthIn(max = 352.dp).fillMaxWidth().testTag("home_content").padding(11.dp)) {
            Row(faded.fillMaxWidth().padding(bottom = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Image(
                    painterResource(R.drawable.flippost_logo), contentDescription = null, contentScale = ContentScale.Crop,
                    modifier = Modifier.testTag("home_avatar").size(44.dp).clip(CircleShape).border(3.dp, dim, CircleShape),
                )
                GlassCard(Modifier.weight(1f).testTag("hotspot_card")) {
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Column(Modifier.weight(1f)) {
                            Text("Hotspot · ${s?.hotspot?.ssid ?: NONE}", color = text, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(s?.let { Format.hotspotSubtitle(it.hotspot) } ?: NONE, color = muted, style = caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                        OptimisticSwitch(
                            checked = s?.hotspot?.active == true,
                            enabled = writable,
                            onWrite = { client.tether(it); onRefresh() },
                            modifier = Modifier.testTag("hotspot_switch"),
                        )
                    }
                }
            }

            FlowRow(
                faded.fillMaxWidth().padding(bottom = 8.dp),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Chip {
                    SignalBars(s?.signal?.bars)
                    Text(if (s == null) NONE else s.signal?.display ?: "No signal", color = text, style = chipText, maxLines = 1)
                }
                Chip {
                    if (s != null) {
                        Dot(when (Format.tempLevel(s.temperature)) {
                            TempLevel.OK -> FlippostColors.green
                            TempLevel.WARN -> FlippostColors.amber
                            TempLevel.HOT -> FlippostColors.red
                        })
                    }
                    Text(s?.let { Format.temp(it.temperature.c ?: it.temperature.rawC) } ?: NONE, color = text, style = chipText, maxLines = 1)
                }
                Chip { Text(if (s == null) NONE else s.wanIp ?: "No WAN", color = text, style = chipText, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                Chip {
                    Text("Tailscale", color = text, style = chipText, maxLines = 1)
                    Dot(when (s?.tailscale?.state) { "running" -> FlippostColors.green; "needs_login" -> FlippostColors.amber; else -> dim })
                }
            }

            GlassCard(faded.fillMaxWidth().padding(bottom = 9.dp)) {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
                    StaleLabel(state, nowMs)
                    Text(
                        s?.speed?.let {
                            buildAnnotatedString {
                                withStyle(SpanStyle(fontSize = 17.sp, fontWeight = FontWeight.ExtraBold, color = text)) { append("↓${Format.mbps(it.wanDownBytesS)}") }
                                withStyle(SpanStyle(fontSize = 12.sp, color = muted)) { append(" ↑${Format.mbps(it.wanUpBytesS)} Mb/s") }
                            }
                        } ?: AnnotatedString(NONE),
                        color = text, style = MaterialTheme.typography.titleSmall, maxLines = 1,
                    )
                }
                Row(Modifier.fillMaxWidth().padding(top = 6.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
                    Text("DATA · CYCLE", color = muted, style = label, maxLines = 1)
                    val u = s?.usage
                    Text(
                        when {
                            u == null -> AnnotatedString(NONE)
                            u.limitBytes > 0 -> buildAnnotatedString {
                                append(Format.bytes(u.periodBytes))
                                withStyle(SpanStyle(fontWeight = FontWeight.Normal, color = muted)) { append(" / ${Format.bytes(u.limitBytes)}") }
                            }
                            else -> AnnotatedString(Format.bytes(u.periodBytes))
                        },
                        color = text, style = value, maxLines = 1,
                    )
                }
                val u = s?.usage
                StripedBar(if (u != null && u.limitBytes > 0) u.periodBytes.toFloat() / u.limitBytes else 0f, Modifier.padding(top = 5.dp, bottom = 4.dp))
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    Text(if (u == null) NONE else "Today ${Format.bytes(u.todayBytes)}", color = muted, style = caption, maxLines = 1)
                    Text(if (u == null) NONE else u.leftBytes?.let { "${Format.bytes(it)} left" } ?: "No cap", color = muted, style = caption, maxLines = 1)
                }
            }

            // 165 dp plus the 11 dp screen padding ends at 176 dp, left of the camera cutout.
            Row(Modifier.width(165.dp), horizontalArrangement = Arrangement.spacedBy(5.dp)) {
                PillButton("Rotate IP", onClick = { rotating = true }, Modifier.weight(1f), enabled = writable, compact = true)
                PillButton("More", onClick = onMore, Modifier.weight(1f), compact = true)
            }
        }
        if (rotating) RotateIpFlow(client) { rotating = false; onRefresh() }
    }
}
