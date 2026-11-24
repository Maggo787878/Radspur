package app.radspur.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CloudDone
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.radspur.app
import app.radspur.data.Sport
import app.radspur.data.Tour
import app.radspur.data.TrackPoint
import app.radspur.find.UploadWorker
import app.radspur.tracking.Gpx
import app.radspur.tracking.formatDuration
import kotlinx.coroutines.launch

@Composable
fun ToursScreen(onOpen: (Long) -> Unit) {
    val ctx = LocalContext.current
    val tours by remember { ctx.app.db.tours().finishedTours() }.collectAsStateWithLifecycle(emptyList())
    var filter by rememberSaveable { mutableStateOf<Sport?>(null) }
    TourListContent(tours, filter, { filter = it }, onOpen)
}

/** Filterchips „Alle / Rad / Laufen“. */
@Composable
fun SportFilter(filter: Sport?, onFilter: (Sport?) -> Unit, modifier: Modifier = Modifier) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(filter == null, { onFilter(null) }, label = { Text("Alle") })
        Sport.entries.forEach { sp ->
            FilterChip(
                filter == sp, { onFilter(sp) }, label = { Text(sp.label) },
                leadingIcon = { Icon(sp.icon, null, Modifier.size(18.dp)) },
            )
        }
    }
}

@Composable
fun TourListContent(tours: List<Tour>, filter: Sport?, onFilter: (Sport?) -> Unit, onOpen: (Long) -> Unit) {
    val shown = if (filter == null) tours else tours.filter { it.sport == filter }
    Column(Modifier.fillMaxSize()) {
        SportFilter(filter, onFilter, Modifier.padding(horizontal = 12.dp, vertical = 8.dp))
        if (shown.isEmpty()) {
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text("Noch keine Aufzeichnungen.") }
            return
        }
        LazyColumn(
            contentPadding = PaddingValues(start = 12.dp, end = 12.dp, bottom = 12.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(shown, key = { it.id }) { t -> TourCard(t) { onOpen(t.id) } }
        }
    }
}

@Composable
private fun TourCard(t: Tour, onClick: () -> Unit) {
    val run = t.sport == Sport.RUN
    Card(Modifier.fillMaxWidth().clickable(onClick = onClick)) {
        Row(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(t.sport.icon, t.sport.label, tint = MaterialTheme.colorScheme.primary)
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(t.name, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Text(
                    "${km(t.distanceM)} · ${hours(t.movingS)} · " +
                        (if (run) avgPace(t.distanceM, t.movingS) else "Ø " + avgKmh(t.distanceM, t.movingS)),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2, overflow = TextOverflow.Ellipsis,
                )
            }
            Spacer(Modifier.width(8.dp))
            Icon(
                if (t.uploaded) Icons.Filled.CloudDone else Icons.Filled.CloudOff,
                contentDescription = if (t.uploaded) "hochgeladen" else "nicht hochgeladen",
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(20.dp),
            )
        }
    }
}

@Composable
fun TourDetailScreen(id: Long, onDeleted: () -> Unit) {
    val ctx = LocalContext.current
    val dao = ctx.app.db.tours()
    val scope = rememberCoroutineScope()
    val tour by remember(id) { dao.tourFlow(id) }.collectAsStateWithLifecycle(null)
    var points by remember { mutableStateOf<List<TrackPoint>>(emptyList()) }
    var confirmDelete by remember { mutableStateOf(false) }
    LaunchedEffect(id) { points = dao.points(id) }

    val t: Tour = tour ?: return
    TourDetailContent(
        tour = t,
        pointCount = points.size,
        canUpload = ctx.app.prefs.configured,
        onShare = { Gpx.share(ctx, t, points) },
        onUpload = { scope.launch { dao.update(t.copy(uploaded = false)); UploadWorker.enqueue(ctx) } },
        onDelete = { confirmDelete = true },
        map = { OsmMap(points.map { it.lat to it.lon }, follow = false, modifier = it) },
    )

    if (confirmDelete) AlertDialog(
        onDismissRequest = { confirmDelete = false },
        title = { Text("Aufzeichnung löschen?") },
        text = { Text("Sie wird nur auf dem Handy gelöscht, nicht am Server.") },
        confirmButton = {
            TextButton({ confirmDelete = false; scope.launch { dao.delete(t); onDeleted() } }) { Text("Löschen") }
        },
        dismissButton = { TextButton({ confirmDelete = false }) { Text("Abbrechen") } },
    )
}

@Composable
fun TourDetailContent(
    tour: Tour,
    pointCount: Int,
    canUpload: Boolean,
    onShare: () -> Unit,
    onUpload: () -> Unit,
    onDelete: () -> Unit,
    map: @Composable (Modifier) -> Unit,
) {
    val t = tour
    val run = t.sport == Sport.RUN
    Column(Modifier.fillMaxSize()) {
        map(Modifier.weight(1f).fillMaxWidth())
        // Höchstens die halbe Höhe, damit die Karte sichtbar bleibt; bei Bedarf scrollen.
        Column(
            Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(t.sport.icon, t.sport.label, tint = MaterialTheme.colorScheme.primary)
                Spacer(Modifier.width(8.dp))
                Column(Modifier.weight(1f)) {
                    Text(t.name, style = MaterialTheme.typography.titleMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    Text(
                        dateRange(t.startTs, t.endTs),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                IconButton(onDelete) {
                    Icon(Icons.Filled.Delete, "Löschen", tint = MaterialTheme.colorScheme.error)
                }
            }
            StatGrid(
                listOf(
                    "Distanz" to km(t.distanceM),
                    "Zeit" to formatDuration(t.movingS),
                    (if (run) "Ø Tempo" else "Ø") to (if (run) avgPace(t.distanceM, t.movingS) else avgKmh(t.distanceM, t.movingS)),
                    (if (run) "Top-Tempo" else "max") to (if (run) pace(t.maxSpeed) else kmh(t.maxSpeed)),
                    "Anstieg" to "%.0f m".format(t.elevGain),
                    "Punkte" to pointCount.toString(),
                )
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onShare, Modifier.weight(1f)) {
                    Text("GPX teilen", maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                OutlinedButton(onUpload, Modifier.weight(1f), enabled = canUpload) {
                    Text(if (t.uploaded) "Neu senden" else "Hochladen", maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}
