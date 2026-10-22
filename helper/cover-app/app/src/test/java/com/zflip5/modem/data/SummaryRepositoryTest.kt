package com.zflip5.modem.data

import com.zflip5.modem.FakeDaemonClient
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.io.IOException
import java.net.ConnectException

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class SummaryRepositoryTest {
    private fun fake() = FakeDaemonClient().also { it.getResponses["/v1/summary"] = it.fixture("summary.json") }
    private fun FakeDaemonClient.calls() = gets.count { it == "/v1/summary" }
    private fun sample() = SummaryParser.parse(com.zflip5.modem.fixture("summary.json"))

    @Test fun startPollsOncePerSecond() = runTest {
        val f = fake(); val r = SummaryRepository(f, backgroundScope, now = { testScheduler.currentTime })
        r.start(); advanceTimeBy(3_500)
        assertEquals(4, f.calls())
    }

    @Test fun startTwiceDoesNotDoublePoll() = runTest {
        val f = fake(); val r = SummaryRepository(f, backgroundScope, now = { testScheduler.currentTime })
        r.start(); r.start(); advanceTimeBy(2_500)
        assertEquals(3, f.calls())
    }

    @Test fun stopHaltsPolling() = runTest {
        val f = fake(); val r = SummaryRepository(f, backgroundScope, now = { testScheduler.currentTime })
        r.start(); advanceTimeBy(1_500); r.stop()
        val n = f.calls(); advanceTimeBy(5_000)
        assertEquals(n, f.calls())
    }

    @Test fun downOnRefusedThenRecovers() = runTest {
        val f = fake(); f.failures["/v1/summary"] = DaemonDownException(ConnectException())
        val r = SummaryRepository(f, backgroundScope, now = { testScheduler.currentTime })
        r.start(); runCurrent()
        assertTrue(r.state.value.down); assertNull(r.state.value.summary)
        f.failures.clear(); advanceTimeBy(1_000); runCurrent()
        assertFalse(r.state.value.down); assertNotNull(r.state.value.summary); assertEquals(1_000L, r.state.value.lastOkAtMs)
    }

    @Test fun errorKeepsLastSummary() = runTest {
        val f = fake(); val r = SummaryRepository(f, backgroundScope, now = { testScheduler.currentTime })
        r.start(); runCurrent()
        f.failures["/v1/summary"] = IOException("timeout"); advanceTimeBy(1_000); runCurrent()
        assertNotNull(r.state.value.summary); assertEquals("timeout", r.state.value.error); assertFalse(r.state.value.down)
    }

    @Test fun staleAfterFiveSeconds() {
        val st = SummaryState(sample(), 1_000, null, false)
        assertFalse(st.stale(6_000)); assertTrue(st.stale(6_001))
        assertFalse(st.writable(6_001)); assertTrue(st.writable(2_000))
    }

    @Test fun staleWhenCollectorStuck() {
        val s = sample()
        val stuck = SummaryState(s.copy(slowAgeS = 31), 1_000, null, false)
        assertTrue(stuck.stale(1_500)); assertFalse(stuck.writable(1_500))
        assertFalse(SummaryState(s.copy(slowAgeS = 30), 1_000, null, false).stale(1_500))
    }

    @Test fun refreshNowFetchesImmediately() = runTest {
        val f = fake(); val r = SummaryRepository(f, backgroundScope, now = { testScheduler.currentTime })
        r.refreshNow(); runCurrent()
        assertEquals(1, f.calls())
    }
}
