package app.radspur.find

import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.media.AudioAttributes
import android.media.AudioManager
import android.media.MediaPlayer
import android.media.RingtoneManager
import androidx.core.app.NotificationCompat
import app.radspur.R
import app.radspur.RadspurApp

/** Plays the alarm sound at full volume (alarm stream, so it rings in silent mode too). */
object Ringer {
    private const val NOTIF_ID = 2
    private var player: MediaPlayer? = null
    private var previousVolume: Int? = null

    val isRinging get() = player != null

    @Synchronized
    fun start(ctx: Context) {
        if (player != null) return
        val am = ctx.getSystemService(AudioManager::class.java)
        previousVolume = am.getStreamVolume(AudioManager.STREAM_ALARM)
        am.setStreamVolume(AudioManager.STREAM_ALARM, am.getStreamMaxVolume(AudioManager.STREAM_ALARM), 0)

        val uri = RingtoneManager.getDefaultUri(RingtoneManager.TYPE_ALARM)
            ?: RingtoneManager.getDefaultUri(RingtoneManager.TYPE_RINGTONE)
        player = MediaPlayer().apply {
            setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_ALARM)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION)
                    .build()
            )
            setDataSource(ctx, uri)
            isLooping = true
            prepare()
            start()
        }

        val stop = PendingIntent.getBroadcast(
            ctx, 0, Intent(ctx, StopRingReceiver::class.java), PendingIntent.FLAG_IMMUTABLE
        )
        val n = NotificationCompat.Builder(ctx, RadspurApp.CH_FIND)
            .setSmallIcon(R.drawable.ic_stat)
            .setContentTitle("Handy wird gesucht")
            .setContentText("Tippen zum Stoppen")
            .setPriority(NotificationCompat.PRIORITY_MAX)
            .setCategory(NotificationCompat.CATEGORY_ALARM)
            .setContentIntent(stop)
            .addAction(0, "Stopp", stop)
            .setDeleteIntent(stop)
            .setOngoing(true)
            .build()
        ctx.getSystemService(NotificationManager::class.java).notify(NOTIF_ID, n)
    }

    @Synchronized
    fun stop(ctx: Context) {
        player?.run { runCatching { stop() }; release() }
        player = null
        previousVolume?.let {
            ctx.getSystemService(AudioManager::class.java).setStreamVolume(AudioManager.STREAM_ALARM, it, 0)
        }
        previousVolume = null
        ctx.getSystemService(NotificationManager::class.java).cancel(NOTIF_ID)
    }
}

class StopRingReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) = Ringer.stop(context)
}
