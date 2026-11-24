package app.radspur.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CloudDone
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.radspur.app
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
    if (tours.isEmpty()) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { Text("Noch keine Touren aufgezeichnet.") }
        return
    }
    LazyColumn(contentPadding = PaddingValues(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        items(tours, key = { it.id }) { t ->
            Card(Modifier.fillMaxWidth().clickable { onOpen(t.id) }) {
                Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(t.name, style = MaterialTheme.typography.titleMedium)
                        Text(
                            "${km(t.distanceM)} · ${formatDuration(t.movingS)} · Ø ${avgKmh(t.distanceM, t.movingS)}",
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                    Icon(
                        if (t.uploaded) Icons.Filled.CloudDone else Icons.Filled.CloudOff,
                        contentDescription = if (t.uploaded) "hochgeladen" else "nicht hochgeladen",
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
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
    Column(Modifier.fillMaxSize()) {
        OsmMap(points.map { it.lat to it.lon }, follow = false, modifier = Modifier.weight(1f).fillMaxWidth())
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(t.name, style = MaterialTheme.typography.titleLarge)
            Text("${dateTime(t.startTs)} – ${dateTime(t.endTs)}", style = MaterialTheme.typography.bodyMedium)
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Stat("Distanz", km(t.distanceM))
                Stat("Fahrzeit", formatDuration(t.movingS))
                Stat("Ø", avgKmh(t.distanceM, t.movingS))
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Stat("max", kmh(t.maxSpeed))
                Stat("Anstieg", "%.0f m".format(t.elevGain))
                Stat("Punkte", points.size.toString())
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button({ Gpx.share(ctx, t, points) }) { Text("GPX teilen") }
                OutlinedButton(
                    onClick = { scope.launch { dao.update(t.copy(uploaded = false)); UploadWorker.enqueue(ctx) } },
                    enabled = ctx.app.prefs.configured,
                ) { Text(if (t.uploaded) "Erneut senden" else "Hochladen") }
                TextButton({ confirmDelete = true }) { Text("Löschen") }
            }
        }
    }

    if (confirmDelete) AlertDialog(
        onDismissRequest = { confirmDelete = false },
        title = { Text("Tour löschen?") },
        text = { Text("Die Tour wird nur auf dem Handy gelöscht, nicht am Server.") },
        confirmButton = {
            TextButton({ confirmDelete = false; scope.launch { dao.delete(t); onDeleted() } }) { Text("Löschen") }
        },
        dismissButton = { TextButton({ confirmDelete = false }) { Text("Abbrechen") } },
    )
}
