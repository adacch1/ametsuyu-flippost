package com.zflip5.modem.data

import com.sun.net.httpserver.HttpServer
import kotlinx.coroutines.runBlocking
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.net.InetSocketAddress

@RunWith(RobolectricTestRunner::class) class DaemonClientTest {
    private lateinit var server: HttpServer
    private var status = 200
    private var reply = "{}"
    private var method: String? = null
    private var path: String? = null
    private var body: String? = null
    private var auth: String? = "unset"

    private val client get() = DaemonClient("http://127.0.0.1:${server.address.port}")

    @Before fun start() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/") { ex ->
            method = ex.requestMethod
            path = ex.requestURI.toString()
            body = ex.requestBody.readBytes().toString(Charsets.UTF_8)
            auth = ex.requestHeaders.getFirst("Authorization")
            val bytes = reply.toByteArray()
            ex.sendResponseHeaders(status, if (bytes.isEmpty()) -1 else bytes.size.toLong())
            if (bytes.isNotEmpty()) ex.responseBody.use { it.write(bytes) }
        }
        server.start()
    }

    @After fun stop() = server.stop(0)

    @Test fun postNon2xxThrowsDaemonExceptionWithReason() {
        status = 409
        reply = """{"error":"clear the allowlist first: dual band stops Wi-Fi scanning","code":409,"reason":"whitelist"}"""
        val e = assertThrows(DaemonException::class.java) { runBlocking { client.setDualBand(true, true) } }
        assertEquals(409, e.code); assertEquals("whitelist", e.reason)
        assertEquals("clear the allowlist first: dual band stops Wi-Fi scanning", e.message)
    }

    @Test fun setDualBandSendsBodyAndNoAuthHeader() {
        reply = """{"dual_band":true,"applied":"now"}"""
        val r = runBlocking { client.setDualBand(true, true) }
        assertEquals("POST", method); assertEquals("/v1/hotspot/dual", path)
        val b = JSONObject(body!!)
        assertTrue(b.getBoolean("enabled")); assertTrue(b.getBoolean("apply_now"))
        assertNull(auth)
        assertEquals(DualResult(true, "now"), r)
    }

    @Test fun refusedConnectionThrowsDaemonDown() {
        val e = assertThrows(DaemonDownException::class.java) { runBlocking { DaemonClient("http://127.0.0.1:1").summary() } }
        assertEquals("service not running", e.message)
    }

    @Test fun nonJsonErrorBodyUsesHttpCode() {
        status = 502; reply = "boom"
        val e = assertThrows(DaemonException::class.java) { runBlocking { client.get("/v1/summary") } }
        assertEquals("HTTP 502", e.message); assertNull(e.reason)
    }

    @Test fun tetherNotAppliedThrows() {
        reply = """{"applied":false,"action":"start","active":false}"""
        val e = assertThrows(DaemonException::class.java) { runBlocking { client.tether(true) } }
        assertEquals(200, e.code); assertEquals("hotspot did not start", e.message); assertEquals("not_applied", e.reason)
        assertEquals("/v1/tether?action=start", path)
    }

    @Test fun rotateIpPostsCycle() {
        runBlocking { client.rotateIp() }
        assertEquals("/v1/airplane", path); assertEquals("""{"mode":"cycle"}""", body)
    }

    @Test fun emptySuccessBodyIsEmptyObject() {
        reply = ""
        assertEquals(0, runBlocking { client.get("/x") }.length())
    }
}
