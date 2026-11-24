package app.radspur.find

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.location.Location
import android.os.BatteryManager
import androidx.core.content.ContextCompat
import androidx.work.*
import app.radspur.app
import app.radspur.net.Api
import app.radspur.net.Command
import app.radspur.net.LocationReport
import com.google.android.gms.location.CurrentLocationRequest
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import kotlinx.coroutines.delay
import kotlinx.coroutines.tasks.await
import kotlinx.coroutines.withTimeoutOrNull
import java.util.concurrent.TimeUnit

/**
 * Periodic check-in: reports the current position and executes commands
 * from the server (ring, stop_ring, locate). After a command the worker
 * switches to one-minute check-ins for a while ("find mode").
 */
class FindWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {

    private val prefs = ctx.app.prefs
    private val api = Api(prefs)

    override suspend fun doWork(): Result {
        if (!prefs.findEnabled || !prefs.configured) return Result.success()
        val fast = prefs.fastChecksLeft > 0
        try {
            val loc = currentLocation(if (fast) Priority.PRIORITY_HIGH_ACCURACY else Priority.PRIORITY_BALANCED_POWER_ACCURACY)
            val cmds = api.reportLocations(listOfNotNull(loc?.let { report(it, if (fast) "find" else "periodic") }))
            prefs.lastCheckIn = System.currentTimeMillis()
            prefs.lastError = ""
            if (cmds.isNotEmpty()) {
                prefs.fastChecksLeft = FAST_CHECKS
                handle(cmds)
            }
        } catch (e: Exception) {
            prefs.lastError = e.message ?: e.javaClass.simpleName
        } finally {
            if (prefs.fastChecksLeft > 0) {
                prefs.fastChecksLeft = prefs.fastChecksLeft - 1
                scheduleFast(applicationContext)
            }
        }
        return Result.success()
    }

    private suspend fun handle(cmds: List<Command>) {
        for (c in cmds) {
            when (c.type) {
                "locate" -> {
                    api.ack(c.id)
                    currentLocation(Priority.PRIORITY_HIGH_ACCURACY)?.let { api.reportLocations(listOf(report(it, "locate"))) }
                }
                "stop_ring" -> { api.ack(c.id); Ringer.stop(applicationContext) }
                "ring" -> { api.ack(c.id); ring() }
                else -> api.ack(c.id)
            }
        }
    }

    /** Rings for up to RING_SECONDS, polling the server for stop_ring meanwhile. */
    private suspend fun ring() {
        Ringer.start(applicationContext)
        val until = System.currentTimeMillis() + RING_SECONDS * 1000
        while (Ringer.isRinging && System.currentTimeMillis() < until) {
            delay(10_000)
            val more = runCatching { api.reportLocations(emptyList()) }.getOrDefault(emptyList())
            more.forEach { c ->
                api.ack(c.id)
                if (c.type == "stop_ring") Ringer.stop(applicationContext)
            }
        }
        Ringer.stop(applicationContext)
    }

    @SuppressLint("MissingPermission")
    private suspend fun currentLocation(priority: Int): Location? {
        val granted = ContextCompat.checkSelfPermission(applicationContext, Manifest.permission.ACCESS_FINE_LOCATION) ==
            PackageManager.PERMISSION_GRANTED
        if (!granted) throw SecurityException("Standortberechtigung fehlt")
        val fused = LocationServices.getFusedLocationProviderClient(applicationContext)
        val req = CurrentLocationRequest.Builder()
            .setPriority(priority)
            .setMaxUpdateAgeMillis(60_000)
            .setDurationMillis(30_000)
            .build()
        return withTimeoutOrNull(35_000) { fused.getCurrentLocation(req, null).await() }
            ?: fused.lastLocation.await()
    }

    private fun report(l: Location, source: String): LocationReport {
        val bat = applicationContext.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val level = bat?.let {
            val lvl = it.getIntExtra(BatteryManager.EXTRA_LEVEL, -1)
            val scale = it.getIntExtra(BatteryManager.EXTRA_SCALE, -1)
            if (lvl >= 0 && scale > 0) lvl * 100 / scale else null
        }
        val plugged = bat?.getIntExtra(BatteryManager.EXTRA_PLUGGED, 0)?.let { it != 0 }
        return LocationReport(
            ts = l.time, lat = l.latitude, lon = l.longitude,
            acc = if (l.hasAccuracy()) l.accuracy else null,
            alt = if (l.hasAltitude()) l.altitude else null,
            speed = if (l.hasSpeed()) l.speed else null,
            battery = level, charging = plugged, source = source,
        )
    }

    companion object {
        private const val PERIODIC = "find-periodic"
        private const val FAST = "find-fast"
        private const val FAST_CHECKS = 15
        private const val RING_SECONDS = 120L

        private val network = Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()

        fun schedule(ctx: Context) {
            val wm = WorkManager.getInstance(ctx)
            if (!ctx.app.prefs.findEnabled) {
                wm.cancelUniqueWork(PERIODIC)
                return
            }
            val req = PeriodicWorkRequestBuilder<FindWorker>(15, TimeUnit.MINUTES)
                .setConstraints(network)
                .build()
            wm.enqueueUniquePeriodicWork(PERIODIC, ExistingPeriodicWorkPolicy.UPDATE, req)
        }

        fun runNow(ctx: Context) {
            WorkManager.getInstance(ctx).enqueueUniqueWork(
                "find-now", ExistingWorkPolicy.REPLACE,
                OneTimeWorkRequestBuilder<FindWorker>().setConstraints(network).build()
            )
        }

        private fun scheduleFast(ctx: Context) {
            WorkManager.getInstance(ctx).enqueueUniqueWork(
                FAST, ExistingWorkPolicy.REPLACE,
                OneTimeWorkRequestBuilder<FindWorker>()
                    .setInitialDelay(1, TimeUnit.MINUTES)
                    .setConstraints(network)
                    .build()
            )
        }
    }
}
