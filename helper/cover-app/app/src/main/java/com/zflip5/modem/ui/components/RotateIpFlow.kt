package com.zflip5.modem.ui.components

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.zflip5.modem.data.DaemonClient
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

private sealed interface Rotate {
    data object Confirm : Rotate
    data object Running : Rotate
    data class Finished(val message: String) : Rotate
}

/** Confirm, run `/v1/airplane` cycle, then show the new IP or the error. */
@Composable
fun RotateIpFlow(client: DaemonClient, onFinish: () -> Unit) {
    var state by remember { mutableStateOf<Rotate>(Rotate.Confirm) }
    val scope = rememberCoroutineScope()
    when (val s = state) {
        Rotate.Confirm -> ConfirmOverlay(
            title = "Rotate IP?",
            body = "Drops connectivity for about 10 to 30 seconds.",
            confirmLabel = "Rotate",
            onConfirm = {
                state = Rotate.Running
                scope.launch {
                    state = Rotate.Finished(
                        try {
                            val r = client.rotateIp()
                            if (r.optBoolean("changed")) "New IP ${r.optString("new_ip")}" else "IP unchanged (${r.optString("old_ip")})"
                        } catch (e: CancellationException) {
                            throw e
                        } catch (e: Exception) {
                            e.message ?: "Rotate failed"
                        },
                    )
                }
            },
            onDismiss = onFinish,
        )
        Rotate.Running -> OverlayCard(onDismiss = {}) {
            CircularProgressIndicator(Modifier.align(Alignment.CenterHorizontally))
            Text("Rotating IP…", Modifier.padding(top = 8.dp).align(Alignment.CenterHorizontally), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleMedium)
        }
        is Rotate.Finished -> OverlayCard(onDismiss = onFinish) {
            Text(s.message, Modifier.padding(bottom = 8.dp), color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleMedium)
            PillButton("Done", onFinish, Modifier.fillMaxWidth(), primary = true)
        }
    }
}
