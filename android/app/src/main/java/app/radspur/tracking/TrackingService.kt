package app.radspur.tracking

import android.annotation.SuppressLint
import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.location.Location
import android.os.Looper
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import app.radspur.R
import app.radspur.RadspurApp
import app.radspur.app
import app.radspur.data.Sport
import app.radspur.data.Tour
import app.radspur.data.TrackPoint
import app.radspur.find.UploadWorker
import app.radspur.ui.MainActivity
import com.google.android.gms.location.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.text.SimpleDateFormat
import java.util.*

enum class TrackStatus { IDLE, RECORDING, PAUSED }

data class LiveTrack(
    val status: TrackStatus = TrackStatus.IDLE,
    val sport: Sport = Sport.BIKE,
    val tourId: Long = 0,
    val startTs: Long = 0,
    val distanceM: Double = 0.0,
    val movingS: Long = 0,
    val maxSpeed: Double = 0.0,
    val elevGain: Double = 0.0,
    val speed: Double = 0.0,
    val accuracy: Float? = null,
    val path: List<Pair<Double, Double>> = emptyList(),
)

class TrackingService : LifecycleService() {

    private lateinit var fused: FusedLocationProviderClient
    private var calc = TourCalculator()
    private var tour: Tour? = null
    private val mutex = Mutex()
    private var lastDbUpdate = 0L

    private val callback = object : LocationCallback() {
        override fun onLocationResult(result: LocationResult) {
            result.locations.forEach { loc -> lifecycleScope.launch { onLocation(loc) } }
        }
    }

    override fun onCreate() {
        super.onCreate()
        fused = LocationServices.getFusedLocationProviderClient(this)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)
        // Must call startForeground promptly on every start.
        ServiceCompat.startForeground(this, NOTIF_ID, notification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
        when (intent?.action) {
            ACTION_PAUSE -> pause()
            ACTION_RESUME -> resume()
            ACTION_STOP -> lifecycleScope.launch { stop() }
            // ACTION_START or restart after process death (intent == null → sport from the DB)
            else -> {
                val sport = Sport.of(intent?.getStringExtra(EXTRA_SPORT))
                lifecycleScope.launch { startOrRestore(sport) }
            }
        }
        return START_STICKY
    }

    private suspend fun startOrRestore(sport: Sport) = mutex.withLock {
        if (tour != null) return@withLock
        val dao = app.db.tours()
        val existing = dao.activeTour()
        val t = if (existing != null) {
            calc = TourCalculator(existing.distanceM, existing.movingS, existing.maxSpeed, existing.elevGain)
            existing
        } else {
            val now = System.currentTimeMillis()
            val prefix = if (sport == Sport.RUN) "Lauf" else "Radtour"
            val name = prefix + " " + SimpleDateFormat("EEE dd.MM.yyyy HH:mm", Locale.GERMAN).format(Date(now))
            val newTour = Tour(clientId = UUID.randomUUID().toString(), name = name, startTs = now, type = sport.id)
            calc = TourCalculator()
            newTour.copy(id = dao.insert(newTour))
        }
        tour = t
        val path = dao.points(t.id).map { it.lat to it.lon }
        _state.value = LiveTrack(
            TrackStatus.RECORDING, t.sport, t.id, t.startTs, calc.distanceM, calc.movingS, calc.maxSpeed, calc.elevGain,
            path = path,
        )
        requestUpdates()
    }

    @SuppressLint("MissingPermission") // checked in the UI before the service is started
    private fun requestUpdates() {
        val req = LocationRequest.Builder(Priority.PRIORITY_HIGH_ACCURACY, 2000L)
            .setMinUpdateIntervalMillis(1000L)
            .build()
        fused.requestLocationUpdates(req, callback, Looper.getMainLooper())
    }

    private fun pause() {
        if (_state.value.status != TrackStatus.RECORDING) return
        fused.removeLocationUpdates(callback)
        calc.resetSegment()
        _state.value = _state.value.copy(status = TrackStatus.PAUSED, speed = 0.0)
        updateNotification()
    }

    private fun resume() {
        if (_state.value.status != TrackStatus.PAUSED) return
        _state.value = _state.value.copy(status = TrackStatus.RECORDING)
        requestUpdates()
        updateNotification()
    }

    private suspend fun onLocation(loc: Location) = mutex.withLock {
        val t = tour ?: return@withLock
        if (_state.value.status != TrackStatus.RECORDING) return@withLock
        val store = calc.add(loc)
        val dao = app.db.tours()
        var path = _state.value.path
        if (store) {
            dao.insertPoint(
                TrackPoint(
                    tourId = t.id, ts = loc.time, lat = loc.latitude, lon = loc.longitude,
                    alt = if (loc.hasAltitude()) loc.altitude else null,
                    speed = if (loc.hasSpeed()) loc.speed else null,
                    acc = if (loc.hasAccuracy()) loc.accuracy else null,
                )
            )
            path = path + (loc.latitude to loc.longitude)
        }
        _state.value = _state.value.copy(
            distanceM = calc.distanceM, movingS = calc.movingS, maxSpeed = calc.maxSpeed,
            elevGain = calc.elevGain, speed = if (loc.hasSpeed()) loc.speed.toDouble() else 0.0,
            accuracy = if (loc.hasAccuracy()) loc.accuracy else null, path = path,
        )
        val now = System.currentTimeMillis()
        if (now - lastDbUpdate > 10_000) {
            lastDbUpdate = now
            tour = t.copy(distanceM = calc.distanceM, movingS = calc.movingS, maxSpeed = calc.maxSpeed, elevGain = calc.elevGain)
            dao.update(tour!!)
            updateNotification()
        }
    }

    private suspend fun stop() {
        fused.removeLocationUpdates(callback)
        mutex.withLock {
            val t = tour
            if (t != null) {
                val dao = app.db.tours()
                if (dao.points(t.id).size < 2) {
                    dao.delete(t) // nothing useful recorded
                } else {
                    dao.update(
                        t.copy(
                            endTs = System.currentTimeMillis(), distanceM = calc.distanceM, movingS = calc.movingS,
                            maxSpeed = calc.maxSpeed, elevGain = calc.elevGain,
                        )
                    )
                    if (app.prefs.autoUpload && app.prefs.configured) UploadWorker.enqueue(this@TrackingService)
                }
            }
            tour = null
            _state.value = LiveTrack()
        }
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private fun notification(): Notification {
        val s = _state.value
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE
        )
        val text = "%.2f km · %s".format(s.distanceM / 1000, formatDuration(s.movingS)) +
            if (s.status == TrackStatus.PAUSED) " · pausiert" else ""
        return NotificationCompat.Builder(this, RadspurApp.CH_TRACKING)
            .setSmallIcon(R.drawable.ic_stat)
            .setContentTitle(if (s.sport == Sport.RUN) "Lauf wird aufgezeichnet" else "Radtour wird aufgezeichnet")
            .setContentText(text)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(open)
            .build()
    }

    private fun updateNotification() {
        // NotificationManager.notify is a no-op when notifications are not permitted.
        getSystemService(android.app.NotificationManager::class.java).notify(NOTIF_ID, notification())
    }

    companion object {
        const val NOTIF_ID = 1
        const val ACTION_START = "start"
        const val ACTION_PAUSE = "pause"
        const val ACTION_RESUME = "resume"
        const val ACTION_STOP = "stop"
        const val EXTRA_SPORT = "sport"

        private val _state = MutableStateFlow(LiveTrack())
        val state: StateFlow<LiveTrack> = _state

        fun send(ctx: Context, action: String, sport: Sport? = null) {
            val i = Intent(ctx, TrackingService::class.java).setAction(action)
            sport?.let { i.putExtra(EXTRA_SPORT, it.id) }
            ContextCompat.startForegroundService(ctx, i)
        }
    }
}

fun formatDuration(s: Long) = "%d:%02d:%02d".format(s / 3600, s / 60 % 60, s % 60)
