package app.radspur.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.TableChart
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.radspur.app
import app.radspur.data.Sport
import app.radspur.data.Tour
import app.radspur.export.Report
import app.radspur.stats.Period
import app.radspur.stats.PeriodUnit
import app.radspur.stats.Stats
import app.radspur.stats.Totals
import java.time.LocalDate
import java.util.Locale

@Composable
fun StatsScreen() {
    val ctx = LocalContext.current
    val tours by remember { ctx.app.db.tours().finishedTours() }.collectAsStateWithLifecycle(emptyList())
    var filter by rememberSaveable { mutableStateOf<Sport?>(null) }
    var exportError by remember { mutableStateOf<String?>(null) }
    StatsContent(
        tours = tours,
        filter = filter,
        onFilter = { filter = it },
        today = LocalDate.now(),
        onExport = {
            // Export enthält immer alle Sportarten (eigene Spalten je Sportart).
            runCatching { Report.share(ctx, tours) }.onFailure { exportError = it.message ?: it.javaClass.simpleName }
        },
    )
    exportError?.let {
        AlertDialog(
            onDismissRequest = { exportError = null },
            title = { Text("Export fehlgeschlagen") }, text = { Text(it) },
            confirmButton = { TextButton({ exportError = null }) { Text("OK") } },
        )
    }
}

@Composable
fun StatsContent(
    tours: List<Tour>,
    filter: Sport?,
    onFilter: (Sport?) -> Unit,
    today: LocalDate,
    onExport: () -> Unit,
) {
    val st = remember { Stats() }
    val shown = if (filter == null) tours else tours.filter { it.sport == filter }
    var unit by rememberSaveable { mutableStateOf(PeriodUnit.WEEK) }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        SportFilter(filter, onFilter)

        // Kennzahlen: Woche, Monat, Jahr, Gesamt
        val tiles = listOf(
            "Diese Woche" to st.current(shown, PeriodUnit.WEEK, today),
            "Dieser Monat" to st.current(shown, PeriodUnit.MONTH, today),
            "Dieses Jahr" to st.current(shown, PeriodUnit.YEAR, today),
            "Gesamt" to st.totals(shown),
        )
        tiles.chunked(2).forEach { row ->
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                row.forEach { (label, t) -> TotalsTile(label, t, Modifier.weight(1f)) }
            }
        }

        // Verlauf
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    if (unit == PeriodUnit.WEEK) "Kilometer pro Woche" else "Kilometer pro Monat",
                    style = MaterialTheme.typography.titleSmall,
                )
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                    listOf(PeriodUnit.WEEK to "12 Wochen", PeriodUnit.MONTH to "12 Monate").forEachIndexed { i, (u, l) ->
                        SegmentedButton(
                            selected = unit == u, onClick = { unit = u },
                            shape = SegmentedButtonDefaults.itemShape(i, 2),
                        ) { Text(l) }
                    }
                }
                BarChart(st.periods(shown, unit, today, last = 12))
            }
        }

        // Durchschnitte
        val total = st.totals(shown)
        val days = st.daysSinceFirst(shown, today)
        val weeks = maxOf(1.0, days / 7.0)
        val run = filter == Sport.RUN
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("Durchschnitt", style = MaterialTheme.typography.titleSmall)
                StatGrid(
                    listOf(
                        "pro Woche" to (if (total.count > 0) kmShort(total.distanceM / weeks) else "–"),
                        "pro Tour" to (if (total.count > 0) kmShort(total.distanceM / total.count) else "–"),
                        (if (run) "Ø Tempo" else "Ø") to (if (run) avgPace(total.distanceM, total.movingS) else avgKmh(total.distanceM, total.movingS)),
                    )
                )
            }
        }

        // Rekorde
        val rec = st.records(shown)
        if (rec.longest != null) Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Rekorde", style = MaterialTheme.typography.titleSmall)
                RecordLine("Längste", km(rec.longest.distanceM), rec.longest)
                rec.fastest?.let {
                    RecordLine(
                        "Schnellste (Ø)",
                        if (it.sport == Sport.RUN) avgPace(it.distanceM, it.movingS) else avgKmh(it.distanceM, it.movingS), it,
                    )
                }
                rec.mostClimb?.takeIf { it.elevGain > 0 }?.let { RecordLine("Meiste Höhenmeter", "%.0f m".format(it.elevGain), it) }
            }
        }

        Button(onExport, Modifier.fillMaxWidth().heightIn(min = 48.dp), enabled = tours.isNotEmpty()) {
            Icon(Icons.Filled.TableChart, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp))
            Text("Auswertung als Excel exportieren")
        }
        Text(
            "Die Excel-Datei enthält Übersicht, Wochen, Monate, Jahre und alle Touren – immer für alle Sportarten.",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun TotalsTile(label: String, t: Totals, modifier: Modifier) {
    Card(modifier) {
        Column(Modifier.padding(12.dp)) {
            Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(kmShort(t.distanceM), style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.SemiBold, maxLines = 1)
            Text(
                "${t.count} ${if (t.count == 1) "Tour" else "Touren"} · ${hours(t.movingS)}",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun RecordLine(label: String, value: String, t: Tour) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(t.sport.icon, t.sport.label, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.width(8.dp))
        Column(Modifier.weight(1f)) {
            Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(dateTime(t.startTs), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Text(value, style = MaterialTheme.typography.titleMedium)
    }
}

/**
 * Säulendiagramm für eine Reihe (km je Zeitraum). Eine Farbe, keine Legende
 * (der Titel benennt die Reihe), Beschriftung nur am Maximum; Antippen zeigt
 * den genauen Wert eines Balkens.
 */
@Composable
fun BarChart(periods: List<Period>, modifier: Modifier = Modifier) {
    var selected by remember(periods) { mutableStateOf<Int?>(null) }
    val maxM = periods.maxOfOrNull { it.totals.distanceM } ?: 0.0
    val maxIdx = periods.indexOfFirst { it.totals.distanceM == maxM && maxM > 0 }
    val barColor = MaterialTheme.colorScheme.primary
    val selColor = MaterialTheme.colorScheme.tertiary
    val grid = MaterialTheme.colorScheme.outlineVariant

    Column(modifier.fillMaxWidth()) {
        // Detailzeile (statt Hover-Tooltip)
        val info = selected?.let { periods.getOrNull(it) }
        Text(
            info?.let { "${it.label}: ${km(it.totals.distanceM)} · ${it.totals.count} ${if (it.totals.count == 1) "Tour" else "Touren"}" }
                ?: if (maxM > 0) "Balken antippen für Details" else "Keine Daten im Zeitraum",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(8.dp))
        Row(
            Modifier.fillMaxWidth().height(140.dp),
            verticalAlignment = Alignment.Bottom,
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            periods.forEachIndexed { i, p ->
                val frac = if (maxM > 0) (p.totals.distanceM / maxM).toFloat() else 0f
                Column(
                    Modifier.weight(1f).fillMaxHeight()
                        .clickable { selected = if (selected == i) null else i }
                        .semantics { contentDescription = "${p.label}: ${km(p.totals.distanceM)}" },
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.Bottom,
                ) {
                    if (i == maxIdx) Text(
                        "%.0f".format(Locale.GERMAN, p.totals.distanceM / 1000),
                        style = MaterialTheme.typography.labelSmall, maxLines = 1, softWrap = false,
                    )
                    Box(
                        Modifier.fillMaxWidth(0.7f)
                            .fillMaxHeight(if (frac > 0) maxOf(frac * 0.85f, 0.02f) else 0f)
                            .background(if (selected == i) selColor else barColor, RoundedCornerShape(topStart = 4.dp, topEnd = 4.dp))
                    )
                }
            }
        }
        Box(Modifier.fillMaxWidth().height(1.dp).background(grid))
        Spacer(Modifier.height(4.dp))
        // Achsenbeschriftung: jede zweite, damit nichts kollidiert
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(2.dp)) {
            periods.forEachIndexed { i, p ->
                Text(
                    if ((periods.size - 1 - i) % 2 == 0) p.shortLabel.removePrefix("KW ") else "",
                    Modifier.weight(1f),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center, maxLines = 1, softWrap = false,
                )
            }
        }
        if (periods.firstOrNull()?.unit == PeriodUnit.WEEK) Text(
            "Kalenderwoche",
            Modifier.fillMaxWidth(), style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center,
        )
    }
}
