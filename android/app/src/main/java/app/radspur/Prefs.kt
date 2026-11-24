package app.radspur

import android.content.Context

class Prefs(context: Context) {
    private val sp = context.getSharedPreferences("radspur", Context.MODE_PRIVATE)

    var serverUrl: String
        get() = sp.getString("server_url", "") ?: ""
        set(v) = sp.edit().putString("server_url", v.trim().trimEnd('/')).apply()

    /** Benutzername, mit dem sich das Handy angemeldet hat (leer = altes gemeinsames Token). */
    var username: String
        get() = sp.getString("username", "") ?: ""
        set(v) = sp.edit().putString("username", v).apply()

    var deviceToken: String
        get() = sp.getString("device_token", "") ?: ""
        set(v) = sp.edit().putString("device_token", v.trim()).apply()

    /** Report the phone's position to the server periodically. */
    var findEnabled: Boolean
        get() = sp.getBoolean("find_enabled", true)
        set(v) = sp.edit().putBoolean("find_enabled", v).apply()

    /** Upload finished tours to the server automatically. */
    var autoUpload: Boolean
        get() = sp.getBoolean("auto_upload", true)
        set(v) = sp.edit().putBoolean("auto_upload", v).apply()

    /** Remaining one-minute check-ins after a command (fast "find mode"). */
    var fastChecksLeft: Int
        get() = sp.getInt("fast_checks_left", 0)
        set(v) = sp.edit().putInt("fast_checks_left", v).apply()

    var lastCheckIn: Long
        get() = sp.getLong("last_check_in", 0)
        set(v) = sp.edit().putLong("last_check_in", v).apply()

    var lastError: String
        get() = sp.getString("last_error", "") ?: ""
        set(v) = sp.edit().putString("last_error", v).apply()

    val urlValid get() = serverUrl.startsWith("https://") || serverUrl.startsWith("http://")

    /** Server-URL gesetzt und am Server angemeldet. */
    val configured get() = urlValid && deviceToken.isNotEmpty()
}

val Context.app get() = applicationContext as RadspurApp
