package app.radspur.export

import android.content.Context
import android.content.Intent
import androidx.core.content.FileProvider
import app.radspur.data.Sport
import app.radspur.data.Tour
import app.radspur.stats.Period
import app.radspur.stats.PeriodUnit
import app.radspur.stats.Stats
import app.radspur.stats.Totals
import java.io.File
import java.time.Instant
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** Baut die Excel-Auswertung: Übersicht, Wochen, Monate, Jahre und alle Touren. */
object Report {

    private fun totalsCells(t: Totals): Array<Cell> = arrayOf(
        Num(t.count.toDouble(), Fmt.INT),
        Num(t.distanceM / 1000, Fmt.DEC2),
        Num((t.distanceBySport[Sport.BIKE] ?: 0.0) / 1000, Fmt.DEC2),
        Num((t.distanceBySport[Sport.RUN] ?: 0.0) / 1000, Fmt.DEC2),
        durationCell(t.movingS),
        Num(t.elevGain, Fmt.INT),
    )

    private val TOTALS_HEADER = arrayOf("Touren", "km gesamt", "km Rad", "km Laufen", "Zeit in Bewegung", "Höhenmeter")

    private fun periodSheet(name: String, first: String, periods: List<Period>) = Sheet(
        name, listOf(16.0, 12.0, 8.0, 11.0, 10.0, 11.0, 17.0, 11.0)
    ).apply {
        header(first, "Beginn", *TOTALS_HEADER)
        periods.forEach { p -> row(Txt(p.label), dateCell(p.start), *totalsCells(p.totals)) }
    }

    fun build(tours: List<Tour>, today: LocalDate, zone: ZoneId = ZoneId.systemDefault()): List<Sheet> {
        val st = Stats(zone)
        val overview = Sheet("Übersicht", listOf(16.0, 8.0, 11.0, 10.0, 11.0, 17.0, 11.0, 10.0)).apply {
            header("Zeitraum", *TOTALS_HEADER, "Ø km/h")
            fun line(label: String, t: Totals) = row(Txt(label), *totalsCells(t), Num(t.avgSpeed * 3.6, Fmt.DEC1))
            line("Diese Woche", st.current(tours, PeriodUnit.WEEK, today))
            line("Dieser Monat", st.current(tours, PeriodUnit.MONTH, today))
            line("Dieses Jahr", st.current(tours, PeriodUnit.YEAR, today))
            line("Gesamt", st.totals(tours))
            row()
            row(Txt("Erstellt"), dateTimeCell(LocalDateTime.now(zone)))
        }
        val toursSheet = Sheet("Touren", listOf(17.0, 9.0, 34.0, 9.0, 17.0, 9.0, 11.0, 9.0, 11.0)).apply {
            header("Start", "Sportart", "Name", "km", "Zeit in Bewegung", "Ø km/h", "Ø min/km", "max km/h", "Höhenmeter")
            tours.sortedBy { it.startTs }.forEach { t ->
                val km = t.distanceM / 1000
                row(
                    dateTimeCell(LocalDateTime.ofInstant(Instant.ofEpochMilli(t.startTs), zone)),
                    Txt(t.sport.label), Txt(t.name),
                    Num(km, Fmt.DEC2), durationCell(t.movingS),
                    if (t.movingS > 0) Num(t.distanceM / t.movingS * 3.6, Fmt.DEC1) else Empty,
                    if (km >= 0.1) paceCell(t.movingS / km) else Empty,
                    Num(t.maxSpeed * 3.6, Fmt.DEC1),
                    Num(t.elevGain, Fmt.INT),
                )
            }
        }
        return listOf(
            overview,
            periodSheet("Wochen", "Woche", st.periods(tours, PeriodUnit.WEEK, today)),
            periodSheet("Monate", "Monat", st.periods(tours, PeriodUnit.MONTH, today)),
            periodSheet("Jahre", "Jahr", st.periods(tours, PeriodUnit.YEAR, today)),
            toursSheet,
        )
    }

    /** Schreibt die Auswertung in den Cache und öffnet den Teilen-Dialog. */
    fun share(ctx: Context, tours: List<Tour>) {
        val today = LocalDate.now()
        val dir = File(ctx.cacheDir, "export").apply { mkdirs() }
        val file = File(dir, "radspur-auswertung-${today.format(DateTimeFormatter.ISO_LOCAL_DATE)}.xlsx")
        file.outputStream().use { Xlsx.write(it, build(tours, today)) }
        val uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".files", file)
        val send = Intent(Intent.ACTION_SEND)
            .setType("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
            .putExtra(Intent.EXTRA_STREAM, uri)
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        ctx.startActivity(Intent.createChooser(send, "Auswertung teilen").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }
}
