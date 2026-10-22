package com.zflip5.modem.ui.more

import android.graphics.BitmapFactory
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
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
import com.zflip5.modem.ui.components.PillButton
import com.zflip5.modem.ui.components.QrCode
import com.zflip5.modem.ui.components.ScreenScaffold
import com.zflip5.modem.ui.components.rememberNowMs
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import org.json.JSONObject
import java.io.IOException

@Composable
private fun Note(text: String, modifier: Modifier = Modifier) =
    Text(text, modifier, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)

private enum class Confirm { Restart, Reboot }

@Composable
private fun SwitchRow(label: String, checked: Boolean, enabled: Boolean, tag: String, onWrite: suspend (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().defaultMinSize(minHeight = 48.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(label, Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)
        OptimisticSwitch(checked, enabled, onWrite, Modifier.testTag(tag))
    }
}

@Composable
fun AdvancedScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onBack: () -> Unit, nowMs: Long = rememberNowMs()) {
    val writable = state.writable(nowMs)
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var status by remember { mutableStateOf<JSONObject?>(null) }
    var webUrl by remember { mutableStateOf<String?>(null) }
    var loaded by remember { mutableStateOf(false) }
    var qrPng by remember { mutableStateOf<ImageBitmap?>(null) }
    var confirm by remember { mutableStateOf<Confirm?>(null) }

    val openReads = status?.optBoolean("open_reads") == true
    val openControl = status?.optBoolean("open_control") == true

    suspend fun load() {
        try { status = client.get("/v1/status") } catch (e: CancellationException) { throw e } catch (_: DaemonException) {} catch (_: IOException) {}
    }
    fun say(text: String) { scope.launch { snackbar.showSnackbar(text) } }

    LaunchedEffect(Unit) {
        load()
        try { webUrl = client.tailscale().webUrl } catch (e: CancellationException) { throw e } catch (_: DaemonException) {} catch (_: IOException) {}
        loaded = true
    }
    // With open reads off the QR must carry the read-status token, which only the daemon holds. Fetch its PNG.
    val url = webUrl
    val haveStatus = status != null
    LaunchedEffect(haveStatus, openReads, url) {
        qrPng = null
        if (!haveStatus || openReads || url == null) return@LaunchedEffect
        try {
            val b = client.getBytes("/v1/qr")
            qrPng = BitmapFactory.decodeByteArray(b, 0, b.size)?.asImageBitmap()
        } catch (e: CancellationException) { throw e } catch (_: DaemonException) {} catch (_: IOException) {}
    }

    fun call(path: String, done: String? = null) {
        scope.launch {
            try { client.post(path); done?.let { snackbar.showSnackbar(it) } } catch (e: CancellationException) { throw e } catch (e: Exception) {
                if (e is DaemonException || e is IOException) snackbar.showSnackbar(e.message ?: "write failed") else throw e
            }
            onRefresh()
        }
    }
    suspend fun flag(path: String, on: Boolean) {
        try { client.post(path, JSONObject().put("open", on)) } finally { load() }
    }

    Box(Modifier.fillMaxSize()) {
        ScreenScaffold("Advanced", onBack) {
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    PillButton("Restart daemon", { confirm = Confirm.Restart }, Modifier.fillMaxWidth(), enabled = writable)
                    PillButton("Reboot phone", { confirm = Confirm.Reboot }, Modifier.fillMaxWidth().padding(top = 6.dp), enabled = writable, destructive = true)
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    SwitchRow("Open reads", openReads, writable, "open_reads_switch") { flag("/v1/dashboard/open", it) }
                    SwitchRow("Open control", openControl, writable, "open_control_switch") { flag("/v1/dashboard/control", it) }
                }
            }
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    Text("Browser pairing", color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)
                    if (url == null) {
                        if (loaded) Note("Tailscale serve is not set up, so there is no web address to pair.")
                    } else {
                        val png = qrPng
                        val mod = Modifier.padding(top = 6.dp).size(200.dp).testTag("pair_qr")
                        if (png != null) Image(png, "QR code", mod.aspectRatio(1f))
                        else QrCode(url, mod)
                        Text(url, Modifier.padding(top = 4.dp), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)
                        if (!openReads) Note("Open reads is off. A browser also needs the read-status token from config.json.", Modifier.padding(top = 4.dp))
                    }
                }
            }
        }
        when (confirm) {
            Confirm.Restart -> ConfirmOverlay(
                title = "Restart the daemon?", body = "The app reconnects when it is back.", confirmLabel = "Restart",
                onConfirm = { confirm = null; call("/v1/service/restart", "Daemon restarting") }, onDismiss = { confirm = null },
            )
            Confirm.Reboot -> ConfirmOverlay(
                title = "Reboot the phone?", body = "The hotspot drops for about a minute.", confirmLabel = "Reboot",
                onConfirm = { confirm = null; call("/v1/device/reboot") }, onDismiss = { confirm = null }, destructive = true,
            )
            null -> {}
        }
    }
}
