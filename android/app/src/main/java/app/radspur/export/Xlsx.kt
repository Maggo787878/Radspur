package app.radspur.export

import java.io.OutputStream
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.temporal.ChronoUnit
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

/** Zahlenformat einer Zelle; [style] ist der Index in cellXfs (styles.xml unten). */
enum class Fmt(val style: Int) { GENERAL(0), HEADER(1), DATE(2), DATETIME(3), DURATION(4), DEC2(5), INT(6), DEC1(7), PACE(8) }

sealed interface Cell
data class Txt(val value: String, val bold: Boolean = false) : Cell
data class Num(val value: Double, val fmt: Fmt = Fmt.GENERAL) : Cell
data object Empty : Cell

private val EXCEL_EPOCH: LocalDate = LocalDate.of(1899, 12, 30)

fun dateCell(d: LocalDate) = Num(ChronoUnit.DAYS.between(EXCEL_EPOCH, d).toDouble(), Fmt.DATE)
fun dateTimeCell(dt: LocalDateTime) = Num(
    ChronoUnit.DAYS.between(EXCEL_EPOCH, dt.toLocalDate()) + dt.toLocalTime().toSecondOfDay() / 86400.0, Fmt.DATETIME
)
/** Dauer als Excel-Zeit (Bruchteil eines Tages), Anzeige [h]:mm. */
fun durationCell(seconds: Long) = Num(seconds / 86400.0, Fmt.DURATION)
/** Tempo in Sekunden pro km als Excel-Zeit, Anzeige [m]:ss (z. B. 5:12). */
fun paceCell(secondsPerKm: Double) = Num(secondsPerKm / 86400.0, Fmt.PACE)

class Sheet(val name: String, val widths: List<Double> = emptyList()) {
    val rows = mutableListOf<List<Cell>>()
    fun row(vararg cells: Cell) { rows += cells.toList() }
    fun header(vararg titles: String) { rows += titles.map { Txt(it, bold = true) } }
}

/**
 * Minimaler XLSX-Writer (Office Open XML, ohne Fremdbibliothek):
 * Inline-Strings, ein Styles-Teil mit Datums-/Zahlenformaten, erste Zeile fixiert.
 */
object Xlsx {
    private const val NS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
    private const val REL = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

    fun write(out: OutputStream, sheets: List<Sheet>) {
        require(sheets.isNotEmpty())
        ZipOutputStream(out).use { zip ->
            fun put(name: String, content: String) {
                zip.putNextEntry(ZipEntry(name))
                zip.write(content.toByteArray(Charsets.UTF_8))
                zip.closeEntry()
            }
            put("[Content_Types].xml", buildString {
                append("""<?xml version="1.0" encoding="UTF-8" standalone="yes"?>""")
                append("""<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">""")
                append("""<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>""")
                append("""<Default Extension="xml" ContentType="application/xml"/>""")
                append("""<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>""")
                append("""<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>""")
                sheets.indices.forEach {
                    append("""<Override PartName="/xl/worksheets/sheet${it + 1}.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>""")
                }
                append("</Types>")
            })
            put("_rels/.rels", """<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="$REL/officeDocument" Target="xl/workbook.xml"/></Relationships>""")
            put("xl/workbook.xml", buildString {
                append("""<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="$NS" xmlns:r="$REL"><sheets>""")
                sheets.forEachIndexed { i, s ->
                    append("""<sheet name="${esc(sheetName(s.name))}" sheetId="${i + 1}" r:id="rId${i + 1}"/>""")
                }
                append("</sheets></workbook>")
            })
            put("xl/_rels/workbook.xml.rels", buildString {
                append("""<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">""")
                sheets.indices.forEach {
                    append("""<Relationship Id="rId${it + 1}" Type="$REL/worksheet" Target="worksheets/sheet${it + 1}.xml"/>""")
                }
                append("""<Relationship Id="rId${sheets.size + 1}" Type="$REL/styles" Target="styles.xml"/>""")
                append("</Relationships>")
            })
            put("xl/styles.xml", STYLES)
            sheets.forEachIndexed { i, s -> put("xl/worksheets/sheet${i + 1}.xml", sheetXml(s)) }
        }
    }

    private fun sheetXml(s: Sheet) = buildString {
        append("""<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="$NS">""")
        append("""<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>""")
        if (s.widths.isNotEmpty()) {
            append("<cols>")
            s.widths.forEachIndexed { i, w -> append("""<col min="${i + 1}" max="${i + 1}" width="$w" customWidth="1"/>""") }
            append("</cols>")
        }
        append("<sheetData>")
        s.rows.forEachIndexed { r, cells ->
            append("""<row r="${r + 1}">""")
            cells.forEachIndexed { c, cell ->
                val ref = col(c) + (r + 1)
                when (cell) {
                    is Txt -> append("""<c r="$ref" t="inlineStr"${if (cell.bold) " s=\"${Fmt.HEADER.style}\"" else ""}><is><t xml:space="preserve">${esc(cell.value)}</t></is></c>""")
                    is Num -> if (cell.value.isFinite()) append("""<c r="$ref" s="${cell.fmt.style}"><v>${cell.value}</v></c>""")
                    Empty -> {}
                }
            }
            append("</row>")
        }
        append("</sheetData></worksheet>")
    }

    /** 0 → A, 25 → Z, 26 → AA */
    fun col(i: Int): String = if (i < 26) ('A' + i).toString() else col(i / 26 - 1) + ('A' + i % 26)

    private fun sheetName(n: String) = n.replace(Regex("""[\[\]:*?/\\]"""), " ").take(31)

    private fun esc(s: String) = buildString {
        for (ch in s) when {
            ch == '&' -> append("&amp;")
            ch == '<' -> append("&lt;")
            ch == '>' -> append("&gt;")
            ch == '"' -> append("&quot;")
            ch < ' ' && ch != '\t' && ch != '\n' && ch != '\r' -> {} // in XML 1.0 nicht erlaubt
            else -> append(ch)
        }
    }

    // cellXfs-Reihenfolge muss zu Fmt passen.
    private val STYLES = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="$NS">
<numFmts count="5"><numFmt numFmtId="164" formatCode="dd.mm.yyyy"/><numFmt numFmtId="165" formatCode="dd.mm.yyyy hh:mm"/><numFmt numFmtId="166" formatCode="[h]:mm"/><numFmt numFmtId="167" formatCode="0.0"/><numFmt numFmtId="168" formatCode="[m]:ss"/></numFmts>
<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>
<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>
<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="9">
<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>
<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="166" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="2" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="1" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="167" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="168" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
</cellXfs>
<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
</styleSheet>"""
}
