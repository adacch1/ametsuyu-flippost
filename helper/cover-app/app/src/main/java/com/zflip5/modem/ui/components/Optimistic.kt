package com.zflip5.modem.ui.components

import androidx.compose.material3.Switch
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.ProvidableCompositionLocal
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.hapticfeedback.HapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import com.zflip5.modem.data.DaemonException
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import java.io.IOException

val LocalSnackbar: ProvidableCompositionLocal<SnackbarHostState> = staticCompositionLocalOf { error("LocalSnackbar not provided") }

/** Shows [pending] at once, then falls back to the server value on failure or after 3 s. */
class OptimisticState internal constructor(
    private val server: State<Boolean>,
    private val haptic: HapticFeedback,
    private val snackbar: SnackbarHostState,
) {
    private var pending by mutableStateOf<Boolean?>(null)
    var busy by mutableStateOf(false)
        private set
    val value: Boolean get() = pending ?: server.value

    suspend fun write(target: Boolean, call: suspend () -> Unit) {
        if (busy) return
        pending = target
        busy = true
        haptic.performHapticFeedback(if (target) HapticFeedbackType.ToggleOn else HapticFeedbackType.ToggleOff)
        var failure: Exception? = null
        try {
            call()
            withTimeoutOrNull(3_000) { snapshotFlow { server.value }.first { it == target } }
        } catch (e: DaemonException) {
            failure = e
        } catch (e: IOException) {
            failure = e
        } finally {
            pending = null
            busy = false
        }
        if (failure != null) {
            haptic.performHapticFeedback(HapticFeedbackType.Reject)
            snackbar.showSnackbar(failure.message ?: "write failed")
        }
    }
}

@Composable
fun rememberOptimistic(server: Boolean): OptimisticState {
    val serverState = rememberUpdatedState(server)
    val haptic = LocalHapticFeedback.current
    val snackbar = LocalSnackbar.current
    return remember(haptic, snackbar) { OptimisticState(serverState, haptic, snackbar) }
}

@Composable
fun OptimisticSwitch(
    checked: Boolean,
    enabled: Boolean,
    onWrite: suspend (Boolean) -> Unit,
    modifier: Modifier = Modifier,
) {
    val state = rememberOptimistic(checked)
    val scope = rememberCoroutineScope()
    Switch(
        checked = state.value,
        onCheckedChange = { target -> scope.launch { state.write(target) { onWrite(target) } } },
        modifier = modifier,
        enabled = enabled,
    )
}
