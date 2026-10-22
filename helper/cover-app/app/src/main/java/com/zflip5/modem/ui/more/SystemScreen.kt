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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.DaemonException
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.data.writable
import com.zflip5.modem.ui.components.GlassCard
import com.zflip5.modem.ui.components.LocalSnackbar
import com.zflip5.modem.ui.components.OptimisticSwitch
import com.zflip5.modem.ui.components.PillButton
import com.zflip5.modem.ui.components.RotateIpFlow
import com.zflip5.modem.ui.components.ScreenScaffold
import com.zflip5.modem.ui.components.rememberNowMs
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import org.json.JSONObject
import java.io.IOException

private val CPU_MODES = listOf("Auto", "Performance", "Balanced", "Eco", "Off")

private fun degrees(v: Double?) = v?.let { if (it % 1.0 == 0.0) it.toInt().toString() else it.toString() }.orEmpty()

@Composable
private fun Heading(text: String) =
    Text(text, Modifier.padding(top = 8.dp, bottom = 4.dp), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)

@Composable
private fun Note(text: String, modifier: Modifier = Modifier) =
    Text(text, modifier, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)

@Composable
fun SystemScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onBack: () -> Unit, nowMs: Long = rememberNowMs()) {
    val writable = state.writable(nowMs)
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val s = state.summary
    var warn by remember { mutableStateOf(degrees(s?.temperature?.warnC)) }
    var gate by remember { mutableStateOf(degrees(s?.temperature?.gateC)) }
    var clamp by remember { mutableStateOf<String?>(null) }
    var cpu by remember { mutableStateOf(JSONObject()) }
    var cooldownNote by remember { mutableStateOf<String?>(null) }
    var rotating by remember { mutableStateOf(false) }

    suspend fun loadCpu() {
        try { cpu = client.get("/v1/cpu") } catch (e: CancellationException) { throw e } catch (_: DaemonException) {} catch (_: IOException) {}
    }
    fun act(block: suspend () -> Unit) {
        scope.launch {
            try { block() } catch (e: CancellationException) { throw e } catch (e: Exception) {
                if (e is DaemonException || e is IOException) snackbar.showSnackbar(e.message ?: "write failed") else throw e
            }
            loadCpu()
            onRefresh()
        }
    }

    LaunchedEffect(Unit) { loadCpu() }
    PollWhileStarted(5_000) { loadCpu() }
    // The summary can arrive after the screen opens; fill the empty fields once.
    LaunchedEffect(s?.temperature?.warnC == null) {
        if (warn.isBlank()) warn = degrees(s?.temperature?.warnC)
        if (gate.isBlank()) gate = degrees(s?.temperature?.gateC)
    }

    val warnC = warn.toDoubleOrNull()
    val gateC = gate.toDoubleOrNull()
    val requested = cpu.optString("requested")

    Box(Modifier.fillMaxSize()) {
        ScreenScaffold("System", onBack) {
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    s?.battery?.let { Note("Battery ${it.level}%") }
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("Bench mode", Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)
                        OptimisticSwitch(
                            s?.temperature?.bench == true, writable,
                            { on -> try { client.post("/v1/thermal/bench", JSONObject().put("enabled", on)) } finally { onRefresh() } },
                            Modifier.testTag("bench_switch"),
                        )
                    }
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("Airplane mode", Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)
                        OptimisticSwitch(
                            s?.airplane == true, writable && s?.airplane != null,
                            { on -> try { client.post("/v1/airplane", JSONObject().put("mode", if (on) "on" else "off"), 60_000) } finally { onRefresh() } },
                            Modifier.testTag("airplane_switch"),
                        )
                    }
                    PillButton("Rotate IP", { rotating = true }, Modifier.fillMaxWidth().padding(top = 6.dp), enabled = writable)
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Heading("Thermal limits")
                    OutlinedTextField(
                        warn, { warn = it }, Modifier.fillMaxWidth().testTag("warn_c"), label = { Text("Warn °C") }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal), enabled = writable,
                    )
                    OutlinedTextField(
                        gate, { gate = it }, Modifier.fillMaxWidth().testTag("gate_c"), label = { Text("Gate °C") }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal), enabled = writable,
                    )
                    clamp?.let { Note(it) }
                    PillButton(
                        "Apply",
                        {
                            act {
                                val r = client.post("/v1/thermal/limits", JSONObject().put("warn_c", warnC).put("gate_c", gateC))
                                clamp = if (r.optBoolean("clamped")) "Clamped to warn ${degrees(r.optDouble("warn_c"))}°, gate ${degrees(r.optDouble("gate_c"))}°" else null
                                r.optDouble("warn_c").takeUnless { it.isNaN() }?.let { warn = degrees(it) }
                                r.optDouble("gate_c").takeUnless { it.isNaN() }?.let { gate = degrees(it) }
                            }
                        },
                        Modifier.fillMaxWidth().padding(top = 6.dp), primary = true, enabled = writable && warnC != null && gateC != null,
                    )
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Heading("CPU mode")
                    if (requested.isNotEmpty()) Note("Running: $requested (${cpu.optString("reason")})")
                    Row(Modifier.fillMaxWidth().padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        for (m in CPU_MODES.take(3)) PillButton(m, { act { client.post("/v1/cpu/mode", JSONObject().put("mode", m.lowercase())) } }, primary = requested == m.lowercase(), enabled = writable)
                    }
                    Row(Modifier.fillMaxWidth().padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        for (m in CPU_MODES.drop(3)) PillButton(m, { act { client.post("/v1/cpu/mode", JSONObject().put("mode", m.lowercase())) } }, primary = requested == m.lowercase(), enabled = writable)
                    }
                    PillButton(
                        "Cooldown",
                        { act { cooldownNote = client.post("/v1/cooldown").optString("note").ifBlank { null } } },
                        Modifier.fillMaxWidth().padding(top = 6.dp), enabled = writable,
                    )
                    cooldownNote?.let { Note(it, Modifier.padding(top = 4.dp)) }
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Heading("Battery cap")
                    Note("Battery stops charging at 80%. The module sets this at boot.")
                }
            }
        }
        if (rotating) RotateIpFlow(client) { rotating = false; onRefresh() }
    }
}
