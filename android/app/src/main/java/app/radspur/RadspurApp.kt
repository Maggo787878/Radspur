package app.radspur

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import app.radspur.data.AppDb
import app.radspur.find.FindWorker
import org.osmdroid.config.Configuration

class RadspurApp : Application() {
    val db by lazy { AppDb.create(this) }
    val prefs by lazy { Prefs(this) }

    override fun onCreate() {
        super.onCreate()
        Configuration.getInstance().apply {
            userAgentValue = "Radspur/${BuildConfig.VERSION_NAME}"
            osmdroidBasePath = cacheDir.resolve("osmdroid")
            osmdroidTileCache = cacheDir.resolve("osmdroid/tiles")
        }
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CH_TRACKING, "Tour-Aufzeichnung", NotificationManager.IMPORTANCE_LOW)
        )
        nm.createNotificationChannel(
            NotificationChannel(CH_FIND, "Handy finden", NotificationManager.IMPORTANCE_HIGH).apply {
                setBypassDnd(true)
            }
        )
        FindWorker.schedule(this)
    }

    companion object {
        const val CH_TRACKING = "tracking"
        const val CH_FIND = "find"
    }
}
