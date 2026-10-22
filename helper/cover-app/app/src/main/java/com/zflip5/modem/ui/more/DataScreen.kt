package com.zflip5.modem.ui.more

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.DaemonException
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.data.writable
import com.zflip5.modem.ui.components.ConfirmOverlay
import com.zflip5.modem.ui.components.GlassCard
import com.zflip5.modem.ui.components.LocalSnackbar
import com.zflip5.modem.ui.components.PillButton
import com.zflip5.modem.ui.components.ScreenScaffold
import com.zflip5.modem.ui.components.rememberNowMs
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import org.json.JSONObject
import java.io.IOException
import java.util.Locale

private const val GB = 1_000_000_000L
private val PERIODS = listOf("Daily", "Weekly", "Monthly", "Manual")
private val TIME_RE = Regex("""([01]\d|2[0-3]):[0-5]\d""")

/** Empty means 0 (no cap); null means the text is not a non-negative number. */
internal fun capBytes(text: String): Long? =
    if (text.isBlank()) 0L else text.trim().toDoubleOrNull()?.takeIf { it >= 0 && it.isFinite() }?.let { Math.round(it * GB) }

@Composable
fun DataScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onBack: () -> Unit, nowMs: Long = rememberNowMs()) {
    val writable = state.writable(nowMs)
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var cap by remember { mutableStateOf("") }
    var period by remember { mutableStateOf("Monthly") }
    var time by remember { mutableStateOf("00:00") }
    var day by remember { mutableStateOf("1") }
    var confirming by remember { mutableStateOf(false) }

    fun act(block: suspend () -> Unit) {
        scope.launch {
            try { block() } catch (e: CancellationException) { throw e } catch (e: Exception) {
                if (e is DaemonException || e is IOException) snackbar.showSnackbar(e.message ?: "write failed") else throw e
            }
            onRefresh()
        }
    }

    LaunchedEffect(Unit) {
        try {
            val u = client.get("/v1/usage")
            val gb = u.optLong("limit_bytes") / GB.toDouble()
            cap = if (gb == 0.0) "" else String.format(Locale.US, "%.3f", gb).trimEnd('0').trimEnd('.')
            period = u.optString("period", "monthly").replaceFirstChar { it.uppercase() }
            time = u.optString("reset_time", "00:00")
            day = u.optInt("reset_day", 1).toString()
        } catch (e: CancellationException) {
            throw e
        } catch (_: DaemonException) {
        } catch (_: IOException) {
        }
    }

    val bytes = capBytes(cap)
    val dayNum = day.toIntOrNull()
    val valid = bytes != null && TIME_RE.matches(time) && (period != "Monthly" || dayNum in 1..28)

    Box(Modifier.fillMaxSize()) {
        ScreenScaffold("Data", onBack) {
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    OutlinedTextField(
                        cap, { cap = it }, Modifier.fillMaxWidth().testTag("cap"), label = { Text("Cap (GB)") }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal), isError = bytes == null,
                    )
                    Text("Period", Modifier.padding(top = 6.dp), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        for (p in PERIODS) PillButton(p, { period = p }, primary = period == p)
                    }
                    OutlinedTextField(
                        time, { time = it }, Modifier.fillMaxWidth().testTag("reset_time"), label = { Text("Reset time") }, singleLine = true,
                        isError = !TIME_RE.matches(time),
                    )
                    if (period == "Monthly") OutlinedTextField(
                        day, { day = it }, Modifier.fillMaxWidth().testTag("reset_day"), label = { Text("Reset day") }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number), isError = dayNum !in 1..28,
                    )
                    PillButton(
                        "Save",
                        {
                            act {
                                client.post(
                                    "/v1/usage/quota",
                                    JSONObject().put("limit_bytes", bytes).put("period", period.lowercase()).put("reset_time", time).put("reset_day", dayNum ?: 1),
                                )
                            }
                        },
                        Modifier.fillMaxWidth().padding(top = 6.dp), primary = true, enabled = writable && valid,
                    )
                }
            }
            item {
                PillButton("Reset data usage", { confirming = true }, Modifier.fillMaxWidth(), enabled = writable, destructive = true)
            }
        }
        if (confirming) ConfirmOverlay(
            title = "Reset data usage?", body = "Sets the used data for this period back to 0.", confirmLabel = "Reset", destructive = true,
            onConfirm = { confirming = false; act { client.post("/v1/usage/reset") } },
            onDismiss = { confirming = false },
        )
    }
}
