package com.zflip5.modem.data

data class Summary(val v: Int, val updatedAt: String, val slowAgeS: Int, val signal: SignalInfo?, val usage: UsageInfo, val speed: SpeedInfo,
    val hotspot: HotspotInfo, val wanIp: String?, val wanIface: String?, val temperature: TempInfo, val battery: BatteryInfo?, val tailscale: TailscaleBrief, val airplane: Boolean?)
data class SignalInfo(val display: String, val tech: String, val operator: String, val bars: Int?)
data class UsageInfo(val periodBytes: Long, val limitBytes: Long, val todayBytes: Long, val leftBytes: Long?, val period: String, val nextReset: String?)
data class SpeedInfo(val intervalMs: Int, val wanDownBytesS: Long, val wanUpBytesS: Long, val phoneDownBytesS: Long?, val phoneUpBytesS: Long?, val perClient: List<ClientSpeed>?) // null means "unavailable"
data class ClientSpeed(val mac: String, val ipv4: String?, val downBytesS: Long, val upBytesS: Long, val signalDbm: Int?)
data class HotspotInfo(val active: Boolean, val ssid: String?, val band: String?, val wifi6: Boolean?, val clients: Int, val dualBand: Boolean, val dualActive: Boolean, val dualBandBlock: String?)
data class TempInfo(val c: Double?, val rawC: Double, val state: String, val safe: Boolean, val warnC: Double, val gateC: Double, val bench: Boolean)
data class BatteryInfo(val level: Int, val plugged: String, val tempC: Double)
data class TailscaleBrief(val state: String, val ip: String?)
data class TailscaleStatus(val state: String, val backendState: String?, val ip: String?, val ips: List<String>, val dnsName: String?, val hostname: String?, val online: Boolean,
    val keyExpiry: String?, val expired: Boolean, val tailnet: String?, val serveOk: Boolean, val webUrl: String?, val version: String?, val health: List<String>, val error: String?)
data class LoginResult(val state: String, val loginUrl: String?)
data class DualResult(val dualBand: Boolean, val applied: String)

class DaemonException(val code: Int, override val message: String, val reason: String?) : Exception(message)
class DaemonDownException(cause: Throwable) : java.io.IOException("service not running", cause)
