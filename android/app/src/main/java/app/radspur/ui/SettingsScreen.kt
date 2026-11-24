package app.radspur.ui

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import app.radspur.app
import app.radspur.find.FindWorker
import app.radspur.find.UploadWorker
import app.radspur.net.Api
import kotlinx.coroutines.launch
import kotlinx.coroutines.delay

@SuppressLint("BatteryLife")
@Composable
fun SettingsScreen() {
    val ctx = LocalContext.current
    val prefs = ctx.app.prefs
    var url by remember { mutableStateOf(prefs.serverUrl) }
    var username by remember { mutableStateOf(prefs.username) }
    var password by remember { mutableStateOf("") }
    var loggedIn by remember { mutableStateOf(prefs.configured) }
    var busy by remember { mutableStateOf(false) }
    var loginError by remember { mutableStateOf<String?>(null) }
    var confirmLogout by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    var findEnabled by remember { mutableStateOf(prefs.findEnabled) }
    var autoUpload by remember { mutableStateOf(prefs.autoUpload) }
    // Refreshed on resume and every few seconds (status of the last check-in).
    var tick by remember { mutableIntStateOf(0) }
    LifecycleResumeEffect(Unit) { tick++; onPauseOrDispose { } }
    LaunchedEffect(Unit) { while (true) { delay(3000); tick++ } }

    val fgLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { tick++ }
    val bgLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { tick++ }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Server", style = MaterialTheme.typography.titleMedium)
        if (loggedIn) {
            Text(
                if (prefs.username.isNotEmpty()) "Angemeldet als ${prefs.username}" else "Angemeldet (gemeinsames Geräte-Token)",
                style = MaterialTheme.typography.bodyLarge,
            )
            Text(prefs.serverUrl, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton({ FindWorker.runNow(ctx) }) { Text("Jetzt melden") }
                TextButton({ confirmLogout = true }) { Text("Abmelden") }
            }
        } else {
            OutlinedTextField(
                url, { url = it; loginError = null }, label = { Text("Server-URL") },
                placeholder = { Text("https://radspur.example.at") }, singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                username, { username = it; loginError = null }, label = { Text("Benutzername") }, singleLine = true,
                keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Ascii),
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                password, { password = it; loginError = null }, label = { Text("Passwort") }, singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password), modifier = Modifier.fillMaxWidth(),
            )
            if (url.trim().startsWith("http://")) Text(
                "Unverschlüsselt: Passwort und Positionen gehen im Klartext über das Netz. " +
                    "Nur im eigenen LAN/VPN verwenden.",
                color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall,
            )
            loginError?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            Button(
                onClick = {
                    val u = url.trim().trimEnd('/')
                    if (!u.startsWith("http://") && !u.startsWith("https://")) {
                        loginError = "Server-URL muss mit https:// oder http:// beginnen"; return@Button
                    }
                    busy = true
                    scope.launch {
                        try {
                            val tok = Api.login(u, username.trim(), password, Build.MANUFACTURER + " " + Build.MODEL)
                            prefs.serverUrl = u; prefs.username = username.trim(); prefs.deviceToken = tok
                            prefs.lastError = ""
                            password = ""; loggedIn = true
                            FindWorker.schedule(ctx); FindWorker.runNow(ctx)
                            if (prefs.autoUpload) UploadWorker.enqueue(ctx)
                        } catch (e: Exception) {
                            loginError = "Anmeldung fehlgeschlagen: " + (e.message ?: e.javaClass.simpleName)
                        } finally {
                            busy = false
                        }
                    }
                },
                enabled = !busy && url.isNotBlank() && username.isNotBlank() && password.isNotEmpty(),
            ) { Text(if (busy) "Anmelden …" else "Anmelden") }
            Text(
                "Noch kein Konto? Konten werden nur per Einladung vom Administrator angelegt.",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        key(tick) {
            val last = prefs.lastCheckIn
            Text(
                if (last > 0) "Letzte Meldung an Server: ${dateTime(last)}" else "Noch keine Meldung an den Server.",
                style = MaterialTheme.typography.bodyMedium,
            )
            if (prefs.lastError.isNotEmpty()) Text("Fehler: ${prefs.lastError}", color = MaterialTheme.colorScheme.error)
        }

        HorizontalDivider()
        SwitchRow("Handy-Ortung (alle 15 min an Server melden)", findEnabled) {
            findEnabled = it; prefs.findEnabled = it; FindWorker.schedule(ctx)
        }
        SwitchRow("Touren automatisch hochladen", autoUpload) { autoUpload = it; prefs.autoUpload = it }

        HorizontalDivider()
        Text("Berechtigungen", style = MaterialTheme.typography.titleMedium)
        key(tick) {
            PermRow("Standort", Perms.location(ctx)) { fgLauncher.launch(Perms.foreground) }
            PermRow("Benachrichtigungen", Perms.notifications(ctx)) { fgLauncher.launch(Perms.foreground) }
            PermRow("Standort im Hintergrund („Immer zulassen“)", Perms.background(ctx)) {
                if (!Perms.location(ctx) || Build.VERSION.SDK_INT < 29) fgLauncher.launch(Perms.foreground)
                else bgLauncher.launch(Manifest.permission.ACCESS_BACKGROUND_LOCATION)
            }
            PermRow("Akku-Optimierung deaktiviert", Perms.batteryUnrestricted(ctx)) {
                ctx.startActivity(
                    Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:" + ctx.packageName))
                )
            }
        }
        Text(
            "Für „Handy finden“ werden Standort im Hintergrund und eine deaktivierte Akku-Optimierung benötigt, " +
                "sonst meldet sich das Handy nur unregelmäßig. Nach einem Befehl aus dem Web (Klingeln, Position) " +
                "meldet sich das Handy 15 Minuten lang jede Minute.",
            style = MaterialTheme.typography.bodySmall,
        )
        if (Build.VERSION.SDK_INT >= 31) {
            TextButton({
                ctx.startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:" + ctx.packageName)))
            }) { Text("App-Einstellungen öffnen") }
        }
    }

    if (confirmLogout) AlertDialog(
        onDismissRequest = { confirmLogout = false },
        title = { Text("Abmelden?") },
        text = { Text("Das Handy meldet dann keine Positionen mehr und lädt keine Touren hoch. Aufzeichnungen am Handy bleiben erhalten.") },
        confirmButton = {
            TextButton({
                confirmLogout = false
                prefs.deviceToken = ""; prefs.username = ""; prefs.lastCheckIn = 0; prefs.lastError = ""
                loggedIn = false
                FindWorker.schedule(ctx)
            }) { Text("Abmelden") }
        },
        dismissButton = { TextButton({ confirmLogout = false }) { Text("Abbrechen") } },
    )
}

@Composable
private fun SwitchRow(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(label, Modifier.weight(1f))
        Switch(checked, onChange)
    }
}

@Composable
private fun PermRow(label: String, granted: Boolean, onRequest: () -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text((if (granted) "✓ " else "✗ ") + label, Modifier.weight(1f))
        if (!granted) TextButton(onRequest) { Text("Erlauben") }
    }
}
