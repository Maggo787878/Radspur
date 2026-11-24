package app.radspur.ui

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.DirectionsBike
import androidx.compose.material.icons.filled.List
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.navigation.NavType
import androidx.navigation.compose.*
import androidx.navigation.navArgument
import android.os.Build

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent { RadspurTheme { AppNav() } }
    }
}

@Composable
fun RadspurTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val ctx = LocalContext.current
    val scheme = when {
        Build.VERSION.SDK_INT >= 31 -> if (dark) dynamicDarkColorScheme(ctx) else dynamicLightColorScheme(ctx)
        dark -> darkColorScheme()
        else -> lightColorScheme()
    }
    MaterialTheme(colorScheme = scheme, content = content)
}

private data class Dest(val route: String, val label: String, val icon: androidx.compose.ui.graphics.vector.ImageVector)

private val dests = listOf(
    Dest("record", "Aufzeichnen", Icons.Filled.DirectionsBike),
    Dest("tours", "Touren", Icons.Filled.List),
    Dest("settings", "Einstellungen", Icons.Filled.Settings),
)

@Composable
fun AppNav() {
    val nav = rememberNavController()
    val entry by nav.currentBackStackEntryAsState()
    val current = entry?.destination?.route
    Scaffold(
        bottomBar = {
            NavigationBar {
                dests.forEach { d ->
                    NavigationBarItem(
                        selected = current == d.route || (d.route == "tours" && current?.startsWith("tour/") == true),
                        onClick = {
                            nav.navigate(d.route) {
                                popUpTo(nav.graph.startDestinationId) { saveState = true }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = { Icon(d.icon, null) },
                        label = { Text(d.label) },
                    )
                }
            }
        }
    ) { pad ->
        NavHost(nav, startDestination = "record", modifier = Modifier.padding(pad)) {
            composable("record") { RecordScreen() }
            composable("tours") { ToursScreen(onOpen = { nav.navigate("tour/$it") }) }
            composable("tour/{id}", arguments = listOf(navArgument("id") { type = NavType.LongType })) {
                TourDetailScreen(it.arguments!!.getLong("id"), onDeleted = { nav.popBackStack() })
            }
            composable("settings") { SettingsScreen() }
        }
    }
}
