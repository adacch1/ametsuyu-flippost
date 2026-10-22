package com.zflip5.modem.data

import java.util.Locale

enum class TempLevel { OK, WARN, HOT }

object Format {
    private val units = arrayOf("B", "kB", "MB", "GB", "TB")

    fun bytes(b: Long): String {
        var v = b.toDouble()
        var i = 0
        while (v >= 1000 && i < units.lastIndex) { v /= 1000; i++ }
        return when {
            i == 0 -> "$b B"
            v < 10 -> String.format(Locale.US, "%.1f %s", v, units[i])
            else -> String.format(Locale.US, "%.0f %s", v, units[i])
        }
    }

    fun usage(u: UsageInfo): String =
        if (u.limitBytes > 0) "${bytes(u.periodBytes)} / ${bytes(u.limitBytes)}" else bytes(u.periodBytes)

    fun mbps(bytesS: Long): String = String.format(Locale.US, "%.1f", bytesS * 8 / 1_000_000.0)

    fun temp(c: Double?): String = if (c == null) "—" else String.format(Locale.US, "%.1f°", c)

    fun tempLevel(t: TempInfo): TempLevel = when {
        !t.safe || t.state == "HOT" || t.state == "COOLDOWN" -> TempLevel.HOT
        t.state == "WARM" || t.state == "RECOVERY" -> TempLevel.WARN
        else -> TempLevel.OK
    }

    fun bandLabel(band: String?): String? = when (band) {
        "2" -> "2.4 GHz"
        "5" -> "5 GHz"
        "6" -> "6 GHz"
        "dual" -> "2.4 + 5 GHz"
        else -> null
    }

    fun hotspotSubtitle(h: HotspotInfo): String {
        if (!h.active) return "Off"
        val wifi = when (h.wifi6) { true -> "Wi-Fi 6"; false -> "Wi-Fi 6 unavailable"; null -> null }
        return listOfNotNull(wifi, bandLabel(h.band), devices(h.clients)).joinToString(" · ")
    }

    fun devices(n: Int): String = if (n == 1) "1 device" else "$n devices"
}
