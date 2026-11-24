package app.radspur.ui

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.radspur.data.Sport
import app.radspur.tracking.LiveTrack
import app.radspur.tracking.TrackStatus
import app.radspur.tracking.TrackingService
import app.radspur.tracking.formatDuration

@Composable
fun RecordScreen() {
    val ctx = LocalContext.current
    val s by TrackingService.state.collectAsStateWithLifecycle()
    var sport by rememberSaveable { mutableStateOf(Sport.BIKE) }
    var confirmStop by remember { mutableStateOf(false) }
    var permDenied by remember { mutableStateOf(false) }

    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        if (Perms.location(ctx)) TrackingService.send(ctx, TrackingService.ACTION_START, sport) else permDenied = true
    }

    RecordContent(
        state = s,
        selectedSport = sport,
        onSelectSport = { sport = it },
        permDenied = permDenied,
        onStart = { launcher.launch(Perms.foreground) },
        onPause = { TrackingService.send(ctx, TrackingService.ACTION_PAUSE) },
        onResume = { TrackingService.send(ctx, TrackingService.ACTION_RESUME) },
        onStop = { confirmStop = true },
        map = { OsmMap(s.path, follow = true, modifier = it) },
    )

    if (confirmStop) AlertDialog(
        onDismissRequest = { confirmStop = false },
        title = { Text(if (s.sport == Sport.RUN) "Lauf beenden?" else "Tour beenden?") },
        text = { Text("Die Aufzeichnung wird gespeichert und – falls eingestellt – zum Server hochgeladen.") },
        confirmButton = {
            TextButton({ confirmStop = false; TrackingService.send(ctx, TrackingService.ACTION_STOP) }) { Text("Beenden") }
        },
        dismissButton = { TextButton({ confirmStop = false }) { Text("Abbrechen") } },
    )
}

/** Reine Darstellung (ohne Service/Berechtigungen), damit sie auch in Tests gerendert werden kann. */
@Composable
fun RecordContent(
    state: LiveTrack,
    selectedSport: Sport,
    onSelectSport: (Sport) -> Unit,
    permDenied: Boolean,
    onStart: () -> Unit,
    onPause: () -> Unit,
    onResume: () -> Unit,
    onStop: () -> Unit,
    map: @Composable (Modifier) -> Unit,
) {
    val idle = state.status == TrackStatus.IDLE
    val sport = if (idle) selectedSport else state.sport
    val run = sport == Sport.RUN

    Column(Modifier.fillMaxSize()) {
        map(Modifier.weight(1f).fillMaxWidth())

        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (idle) {
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                    Sport.entries.forEachIndexed { i, sp ->
                        SegmentedButton(
                            selected = sp == selectedSport,
                            onClick = { onSelectSport(sp) },
                            shape = SegmentedButtonDefaults.itemShape(i, Sport.entries.size),
                            icon = { Icon(sp.icon, null) },
                        ) { Text(sp.label) }
                    }
                }
            }

            // displayMedium bringt eine zur Schriftgröße passende Zeilenhöhe mit;
            // eine bloße fontSize erbt die Zeilenhöhe des Fließtexts und wird oben abgeschnitten.
            Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
                Text(
                    when {
                        idle -> "–"
                        run -> pace(state.speed)
                        else -> kmh(state.speed)
                    },
                    style = MaterialTheme.typography.displayMedium,
                    fontWeight = FontWeight.Bold,
                    textAlign = TextAlign.Center,
                    maxLines = 1,
                    softWrap = false,
                )
                Text(
                    if (run) "aktuelles Tempo" else "aktuelle Geschwindigkeit",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            StatGrid(
                listOf(
                    "Distanz" to km(state.distanceM),
                    "Zeit" to formatDuration(state.movingS),
                    (if (run) "Ø Tempo" else "Ø") to (if (run) avgPace(state.distanceM, state.movingS) else avgKmh(state.distanceM, state.movingS)),
                    (if (run) "Top-Tempo" else "max") to (if (run) pace(state.maxSpeed) else kmh(state.maxSpeed)),
                    "Anstieg" to "%.0f m".format(state.elevGain),
                    "GPS" to (state.accuracy?.let { "±%.0f m".format(it) } ?: "–"),
                )
            )

            if (permDenied) Text(
                "Ohne Standortberechtigung ist keine Aufzeichnung möglich.",
                color = MaterialTheme.colorScheme.error,
            )

            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                val btn = Modifier.weight(1f).heightIn(min = 56.dp)
                when (state.status) {
                    TrackStatus.IDLE -> Button(onStart, btn) { Text(if (run) "Lauf starten" else "Radtour starten") }
                    TrackStatus.RECORDING -> {
                        OutlinedButton(onPause, btn) { Text("Pause") }
                        Button(onStop, btn) { Text("Beenden") }
                    }
                    TrackStatus.PAUSED -> {
                        Button(onResume, btn) { Text("Weiter") }
                        OutlinedButton(onStop, btn) { Text("Beenden") }
                    }
                }
            }
        }
    }
}
