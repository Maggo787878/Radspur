package app.radspur.ui

import android.graphics.Color
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import org.osmdroid.tileprovider.tilesource.TileSourceFactory
import org.osmdroid.util.BoundingBox
import org.osmdroid.util.GeoPoint
import org.osmdroid.views.MapView
import org.osmdroid.views.overlay.Polyline

/**
 * OpenStreetMap view with a track line.
 * follow = true keeps the last point centered (live recording),
 * otherwise the map zooms to the whole track once.
 */
@Composable
fun OsmMap(path: List<Pair<Double, Double>>, follow: Boolean, modifier: Modifier = Modifier) {
    val ctx = LocalContext.current
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    var fitted by remember { mutableStateOf(false) }
    val line = remember {
        Polyline().apply {
            outlinePaint.color = Color.rgb(198, 40, 40)
            outlinePaint.strokeWidth = 10f
        }
    }
    val map = remember {
        MapView(ctx).apply {
            setTileSource(TileSourceFactory.MAPNIK)
            setMultiTouchControls(true)
            controller.setZoom(7.0)
            controller.setCenter(GeoPoint(47.5, 13.5))
            overlays.add(line)
        }
    }
    DisposableEffect(lifecycle) {
        val obs = LifecycleEventObserver { _, e ->
            when (e) {
                Lifecycle.Event.ON_RESUME -> map.onResume()
                Lifecycle.Event.ON_PAUSE -> map.onPause()
                else -> {}
            }
        }
        lifecycle.addObserver(obs)
        onDispose {
            lifecycle.removeObserver(obs)
            map.onDetach()
        }
    }
    AndroidView(modifier = modifier, factory = { map }, update = { m ->
        val pts = path.map { GeoPoint(it.first, it.second) }
        line.setPoints(pts)
        if (pts.isNotEmpty()) {
            if (follow) {
                if (m.zoomLevelDouble < 15) m.controller.setZoom(16.0)
                m.controller.setCenter(pts.last())
            } else if (!fitted) {
                fitted = true
                if (pts.size == 1) {
                    m.controller.setZoom(16.0); m.controller.setCenter(pts[0])
                } else {
                    m.post { m.zoomToBoundingBox(BoundingBox.fromGeoPoints(pts).increaseByScale(1.15f), false) }
                }
            }
        }
        m.invalidate()
    })
}
