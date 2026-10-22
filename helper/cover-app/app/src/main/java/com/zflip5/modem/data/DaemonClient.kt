package com.zflip5.modem.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONException
import org.json.JSONObject
import java.net.ConnectException
import java.net.HttpURLConnection
import java.net.URL

open class DaemonClient(private val base: String = "http://127.0.0.1:18080") {
    open suspend fun get(path: String, timeoutMs: Int = 3_000): JSONObject = call("GET", path, null, timeoutMs)
    open suspend fun post(path: String, body: JSONObject? = null, timeoutMs: Int = 10_000): JSONObject = call("POST", path, body, timeoutMs)

    suspend fun summary(): Summary = SummaryParser.parse(get("/v1/summary", 3_000))
    suspend fun tailscale(): TailscaleStatus = SummaryParser.tailscale(get("/v1/tailscale", 3_000))
    suspend fun tailscaleLogin(): LoginResult = post("/v1/tailscale/login", null, 30_000).let {
        LoginResult(it.getString("state"), if (it.isNull("login_url")) null else it.getString("login_url"))
    }
    suspend fun tailscaleLogout() { post("/v1/tailscale/logout", null, 10_000) }
    suspend fun setDualBand(enabled: Boolean, applyNow: Boolean): DualResult =
        post("/v1/hotspot/dual", JSONObject().put("enabled", enabled).put("apply_now", applyNow), 40_000)
            .let { DualResult(it.getBoolean("dual_band"), it.getString("applied")) }

    suspend fun tether(start: Boolean): JSONObject {
        val action = if (start) "start" else "stop"
        val r = post("/v1/tether?action=$action", null, 25_000)
        if (r.has("applied") && !r.optBoolean("applied")) throw DaemonException(200, "hotspot did not $action", "not_applied")
        return r
    }

    suspend fun rotateIp(): JSONObject = post("/v1/airplane", JSONObject().put("mode", "cycle"), 60_000)

    private suspend fun call(method: String, path: String, body: JSONObject?, timeoutMs: Int): JSONObject = withContext(Dispatchers.IO) {
        val c = URL(base + path).openConnection() as HttpURLConnection
        try {
            c.requestMethod = method
            c.connectTimeout = timeoutMs
            c.readTimeout = timeoutMs
            c.setRequestProperty("Content-Type", "application/json")
            if (method == "POST") {
                c.doOutput = true
                c.outputStream.use { it.write((body?.toString() ?: "").toByteArray()) }
            }
            val code = c.responseCode
            val ok = code in 200..299
            val text = (if (ok) c.inputStream else c.errorStream)?.use { it.readBytes().toString(Charsets.UTF_8) }.orEmpty()
            if (ok) return@withContext if (text.isBlank()) JSONObject() else JSONObject(text)
            val err = try { JSONObject(text) } catch (_: JSONException) { null }
            val msg = err?.optString("error").orEmpty().ifBlank { "HTTP $code" }
            throw DaemonException(code, msg, err?.takeUnless { it.isNull("reason") }?.optString("reason"))
        } catch (e: ConnectException) {
            throw DaemonDownException(e)
        } finally {
            c.disconnect()
        }
    }
}
