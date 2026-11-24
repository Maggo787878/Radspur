package app.radspur.ui

import java.text.SimpleDateFormat
import java.util.*

fun km(m: Double) = "%.2f km".format(Locale.GERMAN, m / 1000)
fun kmShort(m: Double) = if (m >= 100_000) "%.0f km".format(Locale.GERMAN, m / 1000) else "%.1f km".format(Locale.GERMAN, m / 1000)
fun kmh(ms: Double) = "%.1f km/h".format(Locale.GERMAN, ms * 3.6)
fun avgKmh(distanceM: Double, movingS: Long) = if (movingS > 0) kmh(distanceM / movingS) else "–"

/** Lauftempo aus Geschwindigkeit in m/s, z. B. "5:12 /km". Unter 1 km/h: "–". */
fun pace(ms: Double): String {
    if (ms < 0.28) return "–"
    val secPerKm = (1000 / ms).toInt()
    return "%d:%02d /km".format(secPerKm / 60, secPerKm % 60)
}
fun avgPace(distanceM: Double, movingS: Long) = if (movingS > 0) pace(distanceM / movingS) else "–"

/** Dauer kompakt: "1:23 h" bzw. "45 min". */
fun hours(s: Long) = if (s >= 3600) "%d:%02d h".format(s / 3600, s / 60 % 60) else "${s / 60} min"

fun dateTime(ts: Long): String = SimpleDateFormat("dd.MM.yyyy HH:mm", Locale.GERMAN).format(Date(ts))

/** "Di. 24.11.2026, 15:04–16:10" – bei mehrtägigen Touren mit beiden Daten. */
fun dateRange(start: Long, end: Long): String {
    val day = SimpleDateFormat("EE dd.MM.yyyy", Locale.GERMAN)
    val time = SimpleDateFormat("HH:mm", Locale.GERMAN)
    val a = Date(start); val b = Date(end)
    return if (day.format(a) == day.format(b)) "${day.format(a)}, ${time.format(a)}–${time.format(b)}"
    else "${dateTime(start)} – ${dateTime(end)}"
}
