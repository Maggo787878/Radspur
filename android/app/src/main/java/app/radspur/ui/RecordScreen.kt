package app.radspur.ui

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.radspur.tracking.TrackStatus
import app.radspur.tracking.TrackingService
import app.radspur.tracking.formatDuration

@Composable
fun RecordScreen() {
    val ctx = LocalContext.current
    val s by TrackingService.state.collectAsStateWithLifecycle()
    var confirmStop by remember { mutableStateOf(false) }
    var permDenied by remember { mutableStateOf(false) }

    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        if (Perms.location(ctx)) TrackingService.send(ctx, TrackingService.ACTION_START) else permDenied = true
    }

    Column(Modifier.fillMaxSize()) {
        OsmMap(s.path, follow = true, modifier = Modifier.weight(1f).fillMaxWidth())

        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(
                if (s.status == TrackStatus.IDLE) "–" else kmh(s.speed),
                fontSize = 44.sp, fontWeight = FontWeight.Bold,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            )
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Stat("Distanz", km(s.distanceM))
                Stat("Fahrzeit", formatDuration(s.movingS))
                Stat("Ø", avgKmh(s.distanceM, s.movingS))
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Stat("max", kmh(s.maxSpeed))
                Stat("Anstieg", "%.0f m".format(s.elevGain))
                Stat("GPS", s.accuracy?.let { "±%.0f m".format(it) } ?: "–")
            }
            if (permDenied) Text("Ohne Standortberechtigung ist keine Aufzeichnung möglich.", color = MaterialTheme.colorScheme.error)

            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                when (s.status) {
                    TrackStatus.IDLE -> Button(
                        onClick = { launcher.launch(Perms.foreground) },
                        modifier = Modifier.weight(1f).height(56.dp),
                    ) { Text("Tour starten") }
                    TrackStatus.RECORDING -> {
                        OutlinedButton({ TrackingService.send(ctx, TrackingService.ACTION_PAUSE) }, Modifier.weight(1f).height(56.dp)) { Text("Pause") }
                        Button({ confirmStop = true }, Modifier.weight(1f).height(56.dp)) { Text("Beenden") }
                    }
                    TrackStatus.PAUSED -> {
                        Button({ TrackingService.send(ctx, TrackingService.ACTION_RESUME) }, Modifier.weight(1f).height(56.dp)) { Text("Weiter") }
                        OutlinedButton({ confirmStop = true }, Modifier.weight(1f).height(56.dp)) { Text("Beenden") }
                    }
                }
            }
        }
    }

    if (confirmStop) AlertDialog(
        onDismissRequest = { confirmStop = false },
        title = { Text("Tour beenden?") },
        text = { Text("Die Tour wird gespeichert und – falls eingestellt – zum Server hochgeladen.") },
        confirmButton = {
            TextButton({ confirmStop = false; TrackingService.send(ctx, TrackingService.ACTION_STOP) }) { Text("Beenden") }
        },
        dismissButton = { TextButton({ confirmStop = false }) { Text("Abbrechen") } },
    )
}

@Composable
fun Stat(label: String, value: String) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, style = MaterialTheme.typography.titleMedium)
    }
}
