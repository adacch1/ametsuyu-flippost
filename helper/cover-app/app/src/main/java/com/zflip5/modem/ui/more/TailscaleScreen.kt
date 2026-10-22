package com.zflip5.modem.ui.more

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import com.zflip5.modem.data.DaemonException
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.SummaryState
import com.zflip5.modem.data.TailscaleStatus
import com.zflip5.modem.data.writable
import com.zflip5.modem.ui.components.ConfirmOverlay
import com.zflip5.modem.ui.components.GlassCard
import com.zflip5.modem.ui.components.LocalSnackbar
import com.zflip5.modem.ui.components.PillButton
import com.zflip5.modem.ui.components.QrCode
import com.zflip5.modem.ui.components.ScreenScaffold
import com.zflip5.modem.ui.components.rememberNowMs
import com.zflip5.modem.ui.theme.FlippostColors
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.io.IOException

private const val LOGIN_TIMEOUT_MS = 180_000L

private fun loginError(e: DaemonException): String = when {
    e.code == 409 && e.reason == "needs_machine_auth" -> "Approve this device in the Tailscale admin console."
    e.code == 409 && e.reason == "busy" -> "A sign-in is already in progress. Try again in a moment."
    e.code == 504 && e.reason == "no_login_url" -> "Tailscale gave no sign-in link. Try again."
    else -> e.message
}

@Composable
private fun StatusRow(label: String, value: String, note: String? = null) {
    Row(Modifier.fillMaxWidth().padding(vertical = 3.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(label, Modifier.width(84.dp), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)
        Column(Modifier.weight(1f)) {
            Text(value, color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.bodyMedium)
            note?.let { Text(it, color = FlippostColors.red, style = MaterialTheme.typography.labelSmall) }
        }
    }
}

@Composable
fun TailscaleScreen(state: SummaryState, client: DaemonClient, onRefresh: () -> Unit, onBack: () -> Unit, nowMs: Long = rememberNowMs()) {
    val writable = state.writable(nowMs)
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var status by remember { mutableStateOf<TailscaleStatus?>(null) }
    var pollError by remember { mutableStateOf<String?>(null) }
    var qrUrl by remember { mutableStateOf<String?>(null) }
    var confirmLogout by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }

    suspend fun load() {
        try { status = client.tailscale(); pollError = null } catch (e: CancellationException) { throw e } catch (e: DaemonException) { pollError = e.message } catch (e: IOException) { pollError = e.message }
    }
    fun say(text: String) { scope.launch { snackbar.showSnackbar(text) } }

    PollWhileStarted(2_000) { load() }

    val running = status?.state == "running"
    LaunchedEffect(running) {
        if (running && qrUrl != null) { qrUrl = null; say("Tailscale connected") }
    }
    LaunchedEffect(qrUrl) {
        if (qrUrl == null) return@LaunchedEffect
        delay(LOGIN_TIMEOUT_MS)
        qrUrl = null
        say("Sign-in timed out")
    }

    fun login() {
        busy = true
        scope.launch {
            try {
                val r = client.tailscaleLogin()
                when {
                    r.state == "running" -> load()
                    r.loginUrl != null -> qrUrl = r.loginUrl
                    else -> say("Tailscale gave no sign-in link. Try again.")
                }
            } catch (e: CancellationException) { throw e } catch (e: DaemonException) { say(loginError(e)) } catch (e: IOException) { say(e.message ?: "Sign-in failed") }
            busy = false
        }
    }
    fun logout() {
        busy = true
        scope.launch {
            try { client.tailscaleLogout() } catch (e: CancellationException) { throw e } catch (e: DaemonException) { say(e.message) } catch (e: IOException) { say(e.message ?: "Log out failed") }
            load(); onRefresh()
            busy = false
        }
    }

    Box(Modifier.fillMaxSize()) {
        ScreenScaffold("Tailscale", onBack) {
            item {
                GlassCard(Modifier.fillMaxWidth().padding(bottom = 6.dp)) {
                    val s = status
                    if (s == null) {
                        Text(pollError ?: "Loading…", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
                        return@GlassCard
                    }
                    if (s.state == "not_running") Text("Tailscale is not running", color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleSmall)
                    s.error?.let { Text(it, color = FlippostColors.red, style = MaterialTheme.typography.bodyMedium) }
                    StatusRow("State", s.state)
                    StatusRow("IP", s.ip ?: "—")
                    StatusRow("MagicDNS", s.dnsName ?: "—")
                    StatusRow("Key expiry", (s.keyExpiry?.substringBefore('T') ?: "Never") + if (s.expired) " (expired)" else "")
                    StatusRow("Web UI", s.webUrl ?: "—", if (!s.serveOk) "serve missing" else null)
                    for (h in s.health) Text(h, Modifier.padding(top = 2.dp), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)
                    when (s.state) {
                        "running" -> PillButton("Log out", { confirmLogout = true }, Modifier.fillMaxWidth().padding(top = 6.dp), enabled = writable && !busy, destructive = true)
                        "not_running" -> {}
                        else -> PillButton("Log in", { login() }, Modifier.fillMaxWidth().padding(top = 6.dp), enabled = writable && !busy, primary = true)
                    }
                }
            }
        }
        if (confirmLogout) ConfirmOverlay(
            title = "Log out of Tailscale?",
            body = "The web UI stops being reachable over the tailnet until you sign in again.",
            confirmLabel = "Log out",
            onConfirm = { confirmLogout = false; logout() },
            onDismiss = { confirmLogout = false },
            destructive = true,
        )
        qrUrl?.let { url -> LoginQr(url) { qrUrl = null } }
    }
}

/** Full-screen sign-in code. Cancel stays in the left 176 dp so it clears the camera cutout. */
@Composable
private fun LoginQr(url: String, onCancel: () -> Unit) {
    Box(Modifier.fillMaxSize().background(Color(0xF2141B29)).pointerInput(Unit) { detectTapGestures { } }.testTag("login_qr")) {
        Column(Modifier.align(Alignment.TopCenter).padding(top = 4.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            QrCode(url, Modifier.size(260.dp))
            Text("Scan to sign in", color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.labelSmall)
        }
        PillButton("Cancel", onCancel, Modifier.align(Alignment.BottomStart).padding(start = 6.dp, bottom = 2.dp).width(164.dp))
    }
}
