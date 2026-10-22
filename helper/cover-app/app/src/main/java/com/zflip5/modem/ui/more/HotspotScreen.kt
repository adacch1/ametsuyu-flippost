package com.zflip5.modem.ui.more

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
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
import androidx.compose.ui.unit.dp
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.DaemonException
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.data.writable
import com.zflip5.modem.ui.components.ConfirmOverlay
import com.zflip5.modem.ui.components.GlassCard
import com.zflip5.modem.ui.components.LocalSnackbar
import com.zflip5.modem.ui.components.OptimisticSwitch
import com.zflip5.modem.ui.components.OverlayCard
import com.zflip5.modem.ui.components.PillButton
import com.zflip5.modem.ui.components.ScreenScaffold
import com.zflip5.modem.ui.components.rememberNowMs
import com.zflip5.modem.ui.components.rememberOptimistic
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException

private const val MAX_SSIDS = 16

private data class Preset(val id: String, val name: String, val ssid: String)
private data class Nearby(val ssid: String, val rssi: Int, val allowlisted: Boolean)
private data class HotspotData(
    val dualBand: Boolean, val whitelist: List<String>, val nearby: List<Nearby>, val overrideLeftS: Int,
    val autoSwitch: Boolean, val presets: List<Preset>, val usbActive: Boolean,
)

private fun JSONArray?.strings() = this?.let { a -> List(a.length()) { a.getString(it) } }.orEmpty()

@Composable
private fun Label(text: String, modifier: Modifier = Modifier) =
    Text(text, modifier, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)

@Composable
private fun Heading(text: String) =
    Text(text, Modifier.padding(top = 8.dp, bottom = 4.dp), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)

@Composable
fun HotspotScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onBack: () -> Unit, nowMs: Long = rememberNowMs()) {
    val writable = state.writable(nowMs)
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var hotspot by remember { mutableStateOf(JSONObject()) }
    var presets by remember { mutableStateOf(JSONObject()) }
    var usb by remember { mutableStateOf(JSONObject()) }
    var dualTarget by remember { mutableStateOf<Boolean?>(null) }
    var deleting by remember { mutableStateOf<Preset?>(null) }
    var adding by remember { mutableStateOf(false) }
    var ssidDraft by remember { mutableStateOf("") }

    suspend fun <T> quiet(block: suspend () -> T): T? = try { block() } catch (e: CancellationException) { throw e } catch (_: DaemonException) { null } catch (_: IOException) { null }
    suspend fun loadHotspot() { quiet { client.get("/v1/hotspot") }?.let { hotspot = it } }
    suspend fun reload() {
        loadHotspot()
        quiet { client.get("/v1/presets") }?.let { presets = it }
        quiet { client.get("/v1/usbtether") }?.let { usb = it }
        onRefresh()
    }
    /** Runs a write, shows a failure in the snackbar, then reloads. */
    fun act(block: suspend () -> Unit) {
        scope.launch {
            try { block() } catch (e: CancellationException) { throw e } catch (e: Exception) {
                if (e is DaemonException || e is IOException) snackbar.showSnackbar(e.message ?: "write failed") else throw e
            }
            reload()
        }
    }

    LaunchedEffect(Unit) { reload() }
    PollWhileStarted(5_000) { loadHotspot() }

    val d = HotspotData(
        dualBand = hotspot.optBoolean("dual_band"),
        whitelist = hotspot.optJSONArray("whitelist").strings(),
        nearby = hotspot.optJSONArray("nearby")?.let { a -> List(a.length()) { a.getJSONObject(it).let { n -> Nearby(n.optString("ssid"), n.optInt("rssi"), n.optBoolean("whitelisted")) } } }.orEmpty(),
        overrideLeftS = hotspot.optInt("override_left_s"),
        autoSwitch = presets.optBoolean("auto_switch"),
        presets = presets.optJSONArray("presets")?.let { a -> List(a.length()) { a.getJSONObject(it).let { p -> Preset(p.optString("id"), p.optString("name"), p.optString("ssid")) } } }.orEmpty(),
        usbActive = usb.optBoolean("active"),
    )
    val block = when {
        d.whitelist.isNotEmpty() -> "whitelist"
        d.autoSwitch -> "auto_switch"
        else -> null
    }
    val opt = rememberOptimistic(server = d.dualBand)

    Box(Modifier.fillMaxSize()) {
        ScreenScaffold("Hotspot", onBack) {
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("Dual band", Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)
                        Switch(
                            checked = opt.value, onCheckedChange = { dualTarget = it },
                            modifier = Modifier.testTag("dual_switch"), enabled = writable && !opt.busy && (d.dualBand || block == null),
                        )
                    }
                    when (block) {
                        "whitelist" -> Label("Clear the allowlist first: dual band stops Wi-Fi scanning.")
                        "auto_switch" -> Label("Turn off preset auto-switch first: dual band stops Wi-Fi scanning.")
                    }
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Heading("Allowlist")
                    if (d.dualBand) Label("Turn off dual band to use the allowlist.")
                    val on = writable && !d.dualBand
                    fun save(list: List<String>) = act { client.post("/v1/hotspot/whitelist", JSONObject().put("ssids", JSONArray(list))) }
                    for (s in d.whitelist) Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(s, Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.bodyMedium)
                        PillButton("Remove", { save(d.whitelist - s) }, enabled = on)
                    }
                    OutlinedTextField(ssidDraft, { ssidDraft = it }, Modifier.fillMaxWidth(), label = { Text("SSID") }, singleLine = true, enabled = on)
                    PillButton(
                        "Add", { save(d.whitelist + ssidDraft.trim()); ssidDraft = "" }, Modifier.fillMaxWidth(),
                        enabled = on && ssidDraft.isNotBlank() && ssidDraft.trim() !in d.whitelist && d.whitelist.size < MAX_SSIDS,
                    )
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Heading("Force on")
                    if (d.overrideLeftS > 0) Label("On for ${(d.overrideLeftS + 59) / 60} min")
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        for ((label, hours) in listOf("1 h" to 1, "4 h" to 4, "Off" to 0))
                            PillButton(label, { act { client.post("/v1/hotspot/override", JSONObject().put("hours", hours)) } }, Modifier.weight(1f), enabled = writable)
                    }
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Heading("Nearby networks")
                    PillButton("Scan now", { act { client.post("/v1/hotspot/scan") } }, Modifier.fillMaxWidth(), enabled = writable)
                    for (n in d.nearby) Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("${n.ssid}  ${n.rssi} dBm", Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.bodyMedium)
                        if (n.allowlisted) Label("allowlisted")
                    }
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Heading("Presets")
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("Auto-switch", Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.bodyMedium)
                        OptimisticSwitch(
                            d.autoSwitch, writable && !d.dualBand,
                            { on -> try { client.post("/v1/presets/auto", JSONObject().put("on", on)) } finally { reload() } },
                            Modifier.testTag("auto_switch"),
                        )
                    }
                    if (d.dualBand) Label("Turn off dual band to use auto-switch.")
                    for (p in d.presets) Column(Modifier.padding(top = 6.dp)) {
                        Text(p.name, color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.bodyMedium)
                        Label(p.ssid)
                        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            PillButton("Apply", { act { client.post("/v1/presets/apply", JSONObject().put("id", p.id)) } }, enabled = writable)
                            PillButton("Delete", { deleting = p }, enabled = writable, destructive = true)
                        }
                    }
                    PillButton("Add preset", { adding = true }, Modifier.fillMaxWidth().padding(top = 6.dp), enabled = writable)
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("USB tethering", Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)
                        OptimisticSwitch(
                            d.usbActive, writable,
                            { on ->
                                try {
                                    val r = client.post("/v1/usbtether/toggle?action=${if (on) "start" else "stop"}")
                                    if (r.has("applied") && !r.optBoolean("applied")) throw DaemonException(200, "USB tethering did not change", "not_applied")
                                } finally { reload() }
                            },
                            Modifier.testTag("usb_switch"),
                        )
                    }
                }
            }
        }

        dualTarget?.let { target ->
            DualBandDialog(
                enable = target,
                onChoose = { applyNow ->
                    dualTarget = null
                    scope.launch { opt.write(target) { client.setDualBand(target, applyNow); reload() } }
                },
                onDismiss = { dualTarget = null },
            )
        }
        deleting?.let { p ->
            ConfirmOverlay(
                title = "Delete preset?", body = "Delete \"${p.name}\" from the saved presets.", confirmLabel = "Delete", destructive = true,
                onConfirm = { deleting = null; act { client.post("/v1/presets/delete", JSONObject().put("id", p.id)) } },
                onDismiss = { deleting = null },
            )
        }
        if (adding) PresetForm(
            onSave = { body -> adding = false; act { client.post("/v1/presets", body) } },
            onDismiss = { adding = false },
        )
    }
}

@Composable
private fun PresetForm(onSave: (JSONObject) -> Unit, onDismiss: () -> Unit) {
    var name by remember { mutableStateOf("") }
    var ssid by remember { mutableStateOf("") }
    var pass by remember { mutableStateOf("") }
    var triggers by remember { mutableStateOf("") }
    var security by remember { mutableStateOf("wpa2") }
    var band by remember { mutableStateOf("5") }
    OverlayCard(onDismiss) {
        Text("Add preset", color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleMedium)
        OutlinedTextField(name, { name = it }, Modifier.fillMaxWidth().testTag("preset_name"), label = { Text("Name") }, singleLine = true)
        OutlinedTextField(ssid, { ssid = it }, Modifier.fillMaxWidth().testTag("preset_ssid"), label = { Text("SSID") }, singleLine = true)
        OutlinedTextField(pass, { pass = it }, Modifier.fillMaxWidth().testTag("preset_pass"), label = { Text("Passphrase") }, singleLine = true)
        OutlinedTextField(triggers, { triggers = it }, Modifier.fillMaxWidth().testTag("preset_triggers"), label = { Text("Triggers (comma-separated)") }, singleLine = true)
        Row(Modifier.padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            for (s in listOf("open", "wpa2", "wpa3")) PillButton(s, { security = s }, primary = security == s)
        }
        Row(Modifier.padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            for ((label, v) in listOf("2.4 GHz" to "2", "5 GHz" to "5", "6 GHz" to "6")) PillButton(label, { band = v }, primary = band == v)
        }
        PillButton(
            "Save",
            {
                onSave(
                    JSONObject().put("name", name.trim()).put("ssid", ssid.trim()).put("passphrase", pass).put("security", security).put("band", band)
                        .put("triggers", JSONArray(triggers.split(',').map { it.trim() }.filter { it.isNotEmpty() })),
                )
            },
            Modifier.fillMaxWidth().padding(top = 6.dp), primary = true, enabled = name.isNotBlank() && ssid.isNotBlank(),
        )
        PillButton("Cancel", onDismiss, Modifier.fillMaxWidth().padding(top = 6.dp))
    }
}
