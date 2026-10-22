package com.zflip5.modem.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

data class SummaryState(val summary: Summary?, val lastOkAtMs: Long, val error: String?, val down: Boolean) {
    /** Stale after 5 s without a success, or when the daemon's collector has stopped for over 30 s. */
    fun stale(nowMs: Long) = nowMs - lastOkAtMs > 5_000 || (summary?.slowAgeS ?: 0) > 30
}

fun SummaryState.writable(nowMs: Long): Boolean = summary != null && !down && !stale(nowMs)

class SummaryRepository(
    private val client: DaemonClient,
    private val scope: CoroutineScope,
    private val now: () -> Long = System::currentTimeMillis,
) {
    private val _state = MutableStateFlow(SummaryState(null, 0L, null, false))
    val state: StateFlow<SummaryState> = _state
    private var job: Job? = null

    fun start() {
        if (job?.isActive == true) return
        job = scope.launch { while (isActive) { fetchOnce(); delay(1_000) } }
    }

    fun stop() { job?.cancel(); job = null }

    fun refreshNow() { scope.launch { fetchOnce() } }

    private suspend fun fetchOnce() {
        try {
            _state.value = SummaryState(client.summary(), now(), null, false)
        } catch (e: kotlinx.coroutines.CancellationException) {
            throw e
        } catch (e: DaemonDownException) {
            _state.value = _state.value.copy(down = true, error = "service not running")
        } catch (e: Exception) {
            _state.value = _state.value.copy(error = e.message, down = false)
        }
    }
}
