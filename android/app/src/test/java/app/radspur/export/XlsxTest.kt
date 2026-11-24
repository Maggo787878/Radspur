package app.radspur.export

import app.radspur.data.Sport
import app.radspur.data.Tour
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneId
import java.util.zip.ZipFile

class XlsxTest {
    @Test fun columnNames() {
        assertEquals(listOf("A", "Z", "AA", "AZ", "BA"), listOf(0, 25, 26, 51, 52).map { Xlsx.col(it) })
    }

    @Test fun excelDateSerial() {
        assertEquals(45658.0, dateCell(LocalDate.of(2025, 1, 1)).value, 0.0)
        assertEquals(45658.5, dateTimeCell(LocalDateTime.of(2025, 1, 1, 12, 0)).value, 1e-9)
    }

    /** Schreibt eine Beispiel-Auswertung nach build/ (wird zusätzlich mit openpyxl geprüft). */
    @Test fun writesReport() {
        val zone = ZoneId.of("Europe/Vienna")
        fun t(i: Int, d: LocalDateTime, km: Double, sport: Sport) = Tour(
            id = i.toLong(), clientId = "c$i", name = "Tour <$i> & \"Test\"", startTs = d.atZone(zone).toInstant().toEpochMilli(),
            endTs = 1, distanceM = km * 1000, movingS = (km * 150).toLong(), maxSpeed = 9.0, elevGain = 120.0, type = sport.id,
        )
        val tours = listOf(
            t(1, LocalDateTime.of(2026, 10, 5, 9, 30), 42.2, Sport.BIKE),
            t(2, LocalDateTime.of(2026, 11, 20, 17, 0), 8.4, Sport.RUN),
            t(3, LocalDateTime.of(2026, 11, 23, 7, 15), 25.0, Sport.BIKE),
        )
        val f = File("build/test-export/radspur-auswertung.xlsx").apply { parentFile.mkdirs() }
        f.outputStream().use { Xlsx.write(it, Report.build(tours, LocalDate.of(2026, 11, 24), zone)) }
        ZipFile(f).use { z ->
            val names = z.entries().toList().map { it.name }
            assertTrue(names.containsAll(listOf("[Content_Types].xml", "xl/workbook.xml", "xl/styles.xml", "xl/worksheets/sheet5.xml")))
        }
    }
}
