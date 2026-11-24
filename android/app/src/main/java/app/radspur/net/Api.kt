package app.radspur.net

import app.radspur.Prefs
import app.radspur.data.Tour
import app.radspur.data.TrackPoint
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

data class Command(val id: Long, val type: String)

data class LocationReport(
    val ts: Long, val lat: Double, val lon: Double,
    val acc: Float?, val alt: Double?, val speed: Float?,
    val battery: Int?, val charging: Boolean?, val source: String,
)

/** Minimal client for the Radspur server (HttpURLConnection + org.json, no extra deps). */
class Api(private val prefs: Prefs) {

    private suspend fun post(path: String, body: String): String = withContext(Dispatchers.IO) {
        if (!prefs.configured) throw IOException("Server nicht konfiguriert")
        val conn = URL(prefs.serverUrl + path).openConnection() as HttpURLConnection
        try {
            conn.requestMethod = "POST"
            conn.connectTimeout = 15_000
            conn.readTimeout = 30_000
            conn.doOutput = true
            conn.setRequestProperty("Authorization", "Bearer " + prefs.deviceToken)
            conn.setRequestProperty("Content-Type", "application/json")
            conn.outputStream.use { it.write(body.toByteArray()) }
            val code = conn.responseCode
            if (code !in 200..299) {
                throw IOException(if (code == 401) "Token abgelehnt (401)" else "HTTP $code")
            }
            if (code == 204) "" else conn.inputStream.bufferedReader().use { it.readText() }
        } finally {
            conn.disconnect()
        }
    }

    /** Sends positions and returns the commands waiting on the server. */
    suspend fun reportLocations(locs: List<LocationReport>): List<Command> {
        val arr = JSONArray()
        locs.forEach { l ->
            arr.put(JSONObject().apply {
                put("ts", l.ts); put("lat", l.lat); put("lon", l.lon)
                l.acc?.let { put("acc", it.toDouble()) }
                l.alt?.let { put("alt", it) }
                l.speed?.let { put("speed", it.toDouble()) }
                l.battery?.let { put("battery", it) }
                l.charging?.let { put("charging", it) }
                put("source", l.source)
            })
        }
        val resp = JSONObject(post("/api/device/locations", arr.toString()))
        val cmds = resp.optJSONArray("commands") ?: return emptyList()
        return (0 until cmds.length()).map {
            val c = cmds.getJSONObject(it)
            Command(c.getLong("id"), c.getString("type"))
        }
    }

    suspend fun ack(id: Long) {
        post("/api/device/commands/$id/ack", "{}")
    }

    suspend fun uploadTour(t: Tour, points: List<TrackPoint>) {
        val pts = JSONArray()
        points.forEach { p ->
            pts.put(JSONObject().apply {
                put("ts", p.ts); put("lat", p.lat); put("lon", p.lon)
                p.alt?.let { put("alt", it) }
                p.speed?.let { put("speed", it.toDouble()) }
            })
        }
        val body = JSONObject().apply {
            put("clientId", t.clientId); put("name", t.name)
            put("startTs", t.startTs); put("endTs", t.endTs)
            put("distanceM", t.distanceM); put("movingS", t.movingS)
            put("maxSpeed", t.maxSpeed); put("elevGain", t.elevGain)
            put("points", pts)
        }
        post("/api/device/tours", body.toString())
    }
}
