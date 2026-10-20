package net.aegisvpn

import org.json.JSONArray
import org.json.JSONObject

/**
 * One exit node from the coordination server registry
 * (docs/PROTOCOL.md section 4: GET /v1/nodes).
 */
data class Node(
    val id: String,
    val country: String,
    val city: String,
    val host: String,
    val port: Int,
    val pubkey: String,
    val type: String,
    val load: Double,
    val latencyMs: Int,
) {
    /** Human badge label for special node types, null for plain "standard" nodes. */
    val badgeLabel: String?
        get() = when (type) {
            "p2p" -> "P2P"
            "double" -> "Double VPN"
            "onion" -> "Onion"
            "obfuscated" -> "Obfuscated"
            "dedicated" -> "Dedicated IP"
            else -> null
        }

    val endpoint: String get() = "$host:$port"

    fun toJson(): String = JSONObject().apply {
        put("id", id)
        put("country", country)
        put("city", city)
        put("host", host)
        put("port", port)
        put("pubkey", pubkey)
        put("type", type)
        put("load", load)
        put("latencyMs", latencyMs)
    }.toString()

    companion object {
        fun fromJson(o: JSONObject): Node = Node(
            id = o.optString("id"),
            country = o.optString("country"),
            city = o.optString("city"),
            host = o.optString("host"),
            port = o.optInt("port", 51820),
            pubkey = o.optString("pubkey"),
            type = o.optString("type", "standard"),
            load = o.optDouble("load", 0.0),
            latencyMs = o.optInt("latencyMs", 0),
        )
    }
}

/** Wrapper matching the registry response envelope: {"nodes": [...]}. */
data class NodesResponse(val nodes: List<Node>) {
    companion object {
        fun fromJson(text: String): NodesResponse {
            val root = JSONObject(text)
            val arr: JSONArray = root.optJSONArray("nodes") ?: JSONArray()
            val list = ArrayList<Node>(arr.length())
            for (i in 0 until arr.length()) {
                val item = arr.optJSONObject(i) ?: continue
                if (item.optString("id").isNotBlank()) list.add(Node.fromJson(item))
            }
            return NodesResponse(list)
        }
    }
}
