package app.radspur.stats

import app.radspur.data.Sport
import app.radspur.data.Tour
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneId

class StatsTest {
    private val zone = ZoneId.of("Europe/Vienna")
    private val st = Stats(zone)
    private var n = 0

    private fun tour(y: Int, m: Int, d: Int, h: Int, km: Double, sport: Sport = Sport.BIKE, min: Long = 60) =
        Tour(
            id = (++n).toLong(), clientId = "t$n", name = "t$n",
            startTs = LocalDateTime.of(y, m, d, h, 0).atZone(zone).toInstant().toEpochMilli(),
            endTs = 1, distanceM = km * 1000, movingS = min * 60, elevGain = 10.0, type = sport.id,
        )

    @Test fun isoWeekAcrossYearBoundary() {
        // Mo 29.12.2025 bis So 04.01.2026 ist KW 1/2026
        val tours = listOf(tour(2025, 12, 29, 8, 10.0), tour(2026, 1, 4, 23, 5.0), tour(2026, 1, 5, 0, 7.0))
        val weeks = st.periods(tours, PeriodUnit.WEEK, LocalDate.of(2026, 1, 5))
        assertEquals(2, weeks.size)
        assertEquals("KW 1/2026", weeks[0].label)
        assertEquals(LocalDate.of(2025, 12, 29), weeks[0].start)
        assertEquals(15000.0, weeks[0].totals.distanceM, 0.001)
        assertEquals("KW 2/2026", weeks[1].label)
        assertEquals(7000.0, weeks[1].totals.distanceM, 0.001)
    }

    @Test fun periodsAreGaplessAndLimited() {
        val tours = listOf(tour(2026, 1, 15, 10, 20.0), tour(2026, 4, 2, 10, 30.0))
        val months = st.periods(tours, PeriodUnit.MONTH, LocalDate.of(2026, 5, 10))
        assertEquals(listOf(1, 2, 3, 4, 5), months.map { it.start.monthValue })
        assertEquals(listOf(20.0, 0.0, 0.0, 30.0, 0.0), months.map { it.totals.distanceM / 1000 })
        assertEquals("Mai", months.last().shortLabel)

        val last12 = st.periods(tours, PeriodUnit.WEEK, LocalDate.of(2026, 5, 10), last = 12)
        assertEquals(12, last12.size)
        assertEquals(st.periodStart(LocalDate.of(2026, 5, 10), PeriodUnit.WEEK), last12.last().start)
    }

    @Test fun currentWeekMonthYearAndSportSplit() {
        val today = LocalDate.of(2026, 11, 24) // Dienstag
        val tours = listOf(
            tour(2026, 11, 23, 7, 12.5),              // Mo, diese Woche
            tour(2026, 11, 24, 18, 8.0, Sport.RUN),   // heute, Laufen
            tour(2026, 11, 22, 9, 40.0),              // So, letzte Woche, gleicher Monat
            tour(2026, 10, 31, 9, 50.0),              // Oktober
            tour(2025, 11, 24, 9, 99.0),              // Vorjahr
        )
        val week = st.current(tours, PeriodUnit.WEEK, today)
        assertEquals(2, week.count)
        assertEquals(20500.0, week.distanceM, 0.001)
        assertEquals(12500.0, week.distanceBySport[Sport.BIKE]!!, 0.001)
        assertEquals(8000.0, week.distanceBySport[Sport.RUN]!!, 0.001)
        assertEquals(60500.0, st.current(tours, PeriodUnit.MONTH, today).distanceM, 0.001)
        assertEquals(110500.0, st.current(tours, PeriodUnit.YEAR, today).distanceM, 0.001)
        assertEquals(209500.0, st.totals(tours).distanceM, 0.001)
    }

    @Test fun recordsIgnoreTinyTours() {
        val tours = listOf(tour(2026, 1, 1, 8, 0.3, min = 1), tour(2026, 1, 2, 8, 30.0, min = 60), tour(2026, 1, 3, 8, 50.0, min = 150))
        val r = st.records(tours)
        assertEquals(50000.0, r.longest!!.distanceM, 0.001)
        assertEquals(30000.0, r.fastest!!.distanceM, 0.001) // 30 km/h schlägt 20 km/h; 0,3-km-Tour zählt nicht
    }
}
