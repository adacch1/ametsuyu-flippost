package com.zflip5.modem.data

import org.json.JSONArray
import org.json.JSONObject

object SummaryParser {
    private fun JSONObject.str(k: String): String? = if (isNull(k)) null else getString(k)
    private fun JSONObject.int(k: String): Int? = if (isNull(k)) null else getInt(k)
    private fun JSONObject.long(k: String): Long? = if (isNull(k)) null else getLong(k)
    private fun JSONObject.bool(k: String): Boolean? = if (isNull(k)) null else getBoolean(k)
    private fun JSONObject.dbl(k: String): Double? = if (isNull(k)) null else getDouble(k)
    private fun JSONArray.strings(): List<String> = List(length()) { getString(it) }

    fun parse(o: JSONObject): Summary {
        val u = o.getJSONObject("usage")
        val sp = o.getJSONObject("speed")
        val h = o.getJSONObject("hotspot")
        val t = o.getJSONObject("temperature")
        val ts = o.optJSONObject("tailscale") ?: JSONObject()
        return Summary(
            v = o.getInt("v"), updatedAt = o.getString("updated_at"), slowAgeS = o.getInt("slow_age_s"),
            signal = o.optJSONObject("signal")?.let {
                SignalInfo(it.str("display"), it.str("tech"), it.str("operator"), it.int("bars"))
            },
            usage = UsageInfo(u.getLong("period_bytes"), u.getLong("limit_bytes"), u.getLong("today_bytes"),
                u.long("left_bytes"), u.getString("period"), u.str("next_reset")),
            speed = SpeedInfo(sp.getInt("interval_ms"), sp.getLong("wan_down_bytes_s"), sp.getLong("wan_up_bytes_s"),
                sp.long("phone_down_bytes_s"), sp.long("phone_up_bytes_s"),
                (sp.opt("per_client") as? JSONArray)?.let { a ->
                    List(a.length()) {
                        val c = a.getJSONObject(it)
                        ClientSpeed(c.getString("mac"), c.str("ipv4"), c.getLong("down_bytes_s"), c.getLong("up_bytes_s"), c.int("signal_dbm"))
                    }
                }),
            hotspot = HotspotInfo(h.getBoolean("active"), h.str("ssid"), h.str("band"), h.bool("wifi6"), h.getInt("clients"),
                h.getBoolean("dual_band"), h.getBoolean("dual_active"), h.str("dual_band_block")),
            wanIp = o.str("wan_ip"), wanIface = o.str("wan_iface"),
            temperature = TempInfo(t.dbl("c"), t.getDouble("raw_c"), t.getString("state"), t.getBoolean("safe"),
                t.getDouble("warn_c"), t.getDouble("gate_c"), t.getBoolean("bench")),
            battery = o.optJSONObject("battery")?.let {
                BatteryInfo(it.int("level"), it.str("plugged"), it.dbl("temp_c"))
            },
            tailscale = TailscaleBrief(ts.str("state") ?: "unknown", ts.str("ip")),
            airplane = o.bool("airplane"),
        )
    }

    fun tailscale(o: JSONObject) = TailscaleStatus(
        state = o.getString("state"), backendState = o.str("backend_state"), ip = o.str("ip"),
        ips = o.optJSONArray("ips")?.strings().orEmpty(), dnsName = o.str("dns_name"), hostname = o.str("hostname"),
        online = o.optBoolean("online"), keyExpiry = o.str("key_expiry"), expired = o.optBoolean("expired"),
        tailnet = o.str("tailnet"), serveOk = o.optBoolean("serve_ok"), webUrl = o.str("web_url"), version = o.str("version"),
        health = o.optJSONArray("health")?.strings().orEmpty(), error = o.str("error"),
    )
}
