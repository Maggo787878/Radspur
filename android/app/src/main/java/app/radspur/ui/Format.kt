package app.radspur.ui

import java.text.SimpleDateFormat
import java.util.*

fun km(m: Double) = "%.2f km".format(Locale.GERMAN, m / 1000)
fun kmh(ms: Double) = "%.1f km/h".format(Locale.GERMAN, ms * 3.6)
fun avgKmh(distanceM: Double, movingS: Long) = if (movingS > 0) kmh(distanceM / movingS) else "–"
fun dateTime(ts: Long): String = SimpleDateFormat("dd.MM.yyyy HH:mm", Locale.GERMAN).format(Date(ts))
