package com.zflip5.modem.ui.components

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import com.zflip5.modem.data.SummaryState
import kotlinx.coroutines.delay

fun staleText(state: SummaryState, nowMs: Long): String = when {
    state.down -> "Service not running"
    state.summary == null -> "Connecting…"
    state.stale(nowMs) -> "Updated ${(nowMs - state.lastOkAtMs) / 1000} s ago"
    else -> "LIVE"
}

@Composable
fun StaleLabel(state: SummaryState, nowMs: Long, modifier: Modifier = Modifier) {
    Text(staleText(state, nowMs), modifier, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelSmall)
}

/** Wall-clock time, refreshed every second. */
@Composable
fun rememberNowMs(): Long = produceState(System.currentTimeMillis()) {
    while (true) { delay(1_000); value = System.currentTimeMillis() }
}.value
