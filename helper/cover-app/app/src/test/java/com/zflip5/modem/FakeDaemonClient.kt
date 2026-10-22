package com.zflip5.modem

import com.zflip5.modem.data.DaemonClient
import org.json.JSONObject

fun fixture(name: String): JSONObject =
    JSONObject(object {}.javaClass.classLoader!!.getResourceAsStream(name)!!.use { it.readBytes().toString(Charsets.UTF_8) })

open class FakeDaemonClient : DaemonClient("http://fake") {
    val gets = mutableListOf<String>()
    val posts = mutableListOf<Pair<String, JSONObject?>>()
    val getResponses = mutableMapOf<String, JSONObject>()
    val postResponses = mutableMapOf<String, JSONObject>()
    /** Keyed by the full path including the query. */
    val failures = mutableMapOf<String, Exception>()

    fun fixture(name: String): JSONObject = com.zflip5.modem.fixture(name)

    override suspend fun get(path: String, timeoutMs: Int): JSONObject {
        gets += path
        failures[path]?.let { throw it }
        return getResponses[path] ?: JSONObject()
    }

    override suspend fun post(path: String, body: JSONObject?, timeoutMs: Int): JSONObject {
        posts += path to body
        failures[path]?.let { throw it }
        return postResponses[path] ?: JSONObject()
    }
}
