package com.zflip5.modem.ui.more

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.delay

/** Runs [block] every [periodMs] while the screen is STARTED, and stops while it is in the background. */
@Composable
internal fun PollWhileStarted(periodMs: Long, block: suspend () -> Unit) {
    val owner = LocalLifecycleOwner.current
    val current by rememberUpdatedState(block)
    LaunchedEffect(owner, periodMs) {
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { while (true) { current(); delay(periodMs) } }
    }
}
