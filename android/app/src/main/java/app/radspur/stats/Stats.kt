package app.radspur.stats

import app.radspur.data.Sport
import app.radspur.data.Tour
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.TextStyle
import java.time.temporal.ChronoUnit
import java.time.temporal.IsoFields
import java.time.temporal.TemporalAdjusters
import java.util.Locale

enum class PeriodUnit { WEEK, MONTH, YEAR }

/** Summen über eine Menge von Touren. */
data class Totals(
    val count: Int = 0,
    val distanceM: Double = 0.0,
    val movingS: Long = 0,
    val elevGain: Double = 0.0,
    val distanceBySport: Map<Sport, Double> = emptyMap(),
) {
    /** Ø-Geschwindigkeit in m/s (0, wenn keine Fahrzeit). */
    val avgSpeed get() = if (movingS > 0) distanceM / movingS else 0.0

    operator fun plus(t: Tour) = Totals(
        count + 1, distanceM + t.distanceM, movingS + t.movingS, elevGain + t.elevGain,
        distanceBySport + (t.sport to (distanceBySport[t.sport] ?: 0.0) + t.distanceM),
    )
}

/** Ein Zeitraum (Woche, Monat oder Jahr) mit seinen Summen. */
data class Period(val unit: PeriodUnit, val start: LocalDate, val totals: Totals) {
    /** Kurzbezeichnung für Diagramme: "KW 47", "Nov", "2026". */
    val shortLabel: String
        get() = when (unit) {
            PeriodUnit.WEEK -> "KW ${start.get(IsoFields.WEEK_OF_WEEK_BASED_YEAR)}"
            PeriodUnit.MONTH -> start.month.getDisplayName(TextStyle.SHORT_STANDALONE, Locale.GERMAN).trimEnd('.')
            PeriodUnit.YEAR -> start.year.toString()
        }

    /** Ausführliche Bezeichnung: "KW 47/2026", "November 2026", "2026". */
    val label: String
        get() = when (unit) {
            PeriodUnit.WEEK -> "KW %d/%d".format(
                start.get(IsoFields.WEEK_OF_WEEK_BASED_YEAR), start.get(IsoFields.WEEK_BASED_YEAR)
            )
            PeriodUnit.MONTH -> start.month.getDisplayName(TextStyle.FULL_STANDALONE, Locale.GERMAN) + " " + start.year
            PeriodUnit.YEAR -> start.year.toString()
        }
}

data class Records(val longest: Tour?, val fastest: Tour?, val mostClimb: Tour?)

class Stats(private val zone: ZoneId = ZoneId.systemDefault()) {

    fun date(ts: Long): LocalDate = Instant.ofEpochMilli(ts).atZone(zone).toLocalDate()

    fun periodStart(d: LocalDate, unit: PeriodUnit): LocalDate = when (unit) {
        PeriodUnit.WEEK -> d.with(TemporalAdjusters.previousOrSame(java.time.DayOfWeek.MONDAY))
        PeriodUnit.MONTH -> d.withDayOfMonth(1)
        PeriodUnit.YEAR -> d.withDayOfYear(1)
    }

    private fun next(d: LocalDate, unit: PeriodUnit): LocalDate = when (unit) {
        PeriodUnit.WEEK -> d.plusWeeks(1)
        PeriodUnit.MONTH -> d.plusMonths(1)
        PeriodUnit.YEAR -> d.plusYears(1)
    }

    fun totals(tours: List<Tour>) = tours.fold(Totals()) { acc, t -> acc + t }

    /** Summe der Touren im Zeitraum, der [today] enthält (diese Woche / dieser Monat / dieses Jahr). */
    fun current(tours: List<Tour>, unit: PeriodUnit, today: LocalDate): Totals {
        val start = periodStart(today, unit)
        return totals(tours.filter { periodStart(date(it.startTs), unit) == start })
    }

    /**
     * Lückenlose Zeiträume bis einschließlich [today], älteste zuerst.
     * [last] begrenzt auf die letzten n Zeiträume; null = ab der ersten Tour.
     */
    fun periods(tours: List<Tour>, unit: PeriodUnit, today: LocalDate, last: Int? = null): List<Period> {
        val end = periodStart(today, unit)
        val byStart = tours.groupBy { periodStart(date(it.startTs), unit) }
        val first = when {
            last != null -> when (unit) {
                PeriodUnit.WEEK -> end.minusWeeks(last - 1L)
                PeriodUnit.MONTH -> end.minusMonths(last - 1L)
                PeriodUnit.YEAR -> end.minusYears(last - 1L)
            }
            byStart.isEmpty() -> end
            else -> minOf(byStart.keys.min(), end)
        }
        val out = ArrayList<Period>()
        var d = first
        while (!d.isAfter(end)) {
            out += Period(unit, d, totals(byStart[d].orEmpty()))
            d = next(d, unit)
        }
        return out
    }

    fun records(tours: List<Tour>) = Records(
        longest = tours.maxByOrNull { it.distanceM },
        // Ø-Tempo erst ab 1 km sinnvoll, sonst gewinnen Mini-Touren
        fastest = tours.filter { it.distanceM >= 1000 && it.movingS > 0 }.maxByOrNull { it.distanceM / it.movingS },
        mostClimb = tours.maxByOrNull { it.elevGain },
    )

    /** Anzahl Tage seit der ersten Tour (für Durchschnitt pro Woche). */
    fun daysSinceFirst(tours: List<Tour>, today: LocalDate): Long =
        tours.minOfOrNull { date(it.startTs) }?.let { ChronoUnit.DAYS.between(it, today) + 1 } ?: 0
}
