package app.radspur.ui

import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.DirectionsRun
import androidx.compose.material.icons.filled.DirectionsBike
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.radspur.data.Sport

val Sport.icon: ImageVector
    get() = if (this == Sport.RUN) Icons.AutoMirrored.Filled.DirectionsRun else Icons.Filled.DirectionsBike

/**
 * Werte in einem Raster mit gleich breiten Spalten. Jede Zelle bekommt ihre
 * feste Breite; Werte brechen nicht buchstabenweise um.
 */
@Composable
fun StatGrid(items: List<Pair<String, String>>, columns: Int = 3, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        items.chunked(columns).forEach { row ->
            Row(Modifier.fillMaxWidth()) {
                row.forEach { (label, value) -> Stat(label, value, Modifier.weight(1f)) }
                repeat(columns - row.size) { Spacer(Modifier.weight(1f)) }
            }
        }
    }
}

@Composable
fun Stat(label: String, value: String, modifier: Modifier = Modifier) {
    Column(modifier.padding(horizontal = 2.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Text(
            label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center,
        )
        Text(
            value, style = MaterialTheme.typography.titleMedium,
            maxLines = 1, softWrap = false, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center,
        )
    }
}
