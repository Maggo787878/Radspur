package app.radspur.tracking

import android.content.Context
import android.content.Intent
import androidx.core.content.FileProvider
import app.radspur.data.Tour
import app.radspur.data.TrackPoint
import java.io.File
import java.text.SimpleDateFormat
import java.util.*

object Gpx {
    private fun esc(s: String) = s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace("\"", "&quot;")

    fun write(file: File, tour: Tour, points: List<TrackPoint>) {
        val iso = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss'Z'", Locale.US).apply { timeZone = TimeZone.getTimeZone("UTC") }
        file.bufferedWriter().use { w ->
            w.write("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
            w.write("<gpx version=\"1.1\" creator=\"Radspur\" xmlns=\"http://www.topografix.com/GPX/1/1\">\n")
            w.write("<trk><name>${esc(tour.name)}</name><type>${if (tour.sport == app.radspur.data.Sport.RUN) "running" else "cycling"}</type><trkseg>\n")
            points.forEach { p ->
                w.write("<trkpt lat=\"%.7f\" lon=\"%.7f\">".format(Locale.US, p.lat, p.lon))
                p.alt?.let { w.write("<ele>%.1f</ele>".format(Locale.US, it)) }
                w.write("<time>${iso.format(Date(p.ts))}</time></trkpt>\n")
            }
            w.write("</trkseg></trk></gpx>\n")
        }
    }

    fun share(ctx: Context, tour: Tour, points: List<TrackPoint>) {
        val dir = File(ctx.cacheDir, "gpx").apply { mkdirs() }
        val file = File(dir, "radspur-${SimpleDateFormat("yyyyMMdd-HHmm", Locale.US).format(Date(tour.startTs))}.gpx")
        write(file, tour, points)
        val uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".files", file)
        val send = Intent(Intent.ACTION_SEND)
            .setType("application/gpx+xml")
            .putExtra(Intent.EXTRA_STREAM, uri)
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        ctx.startActivity(Intent.createChooser(send, "GPX teilen").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }
}
