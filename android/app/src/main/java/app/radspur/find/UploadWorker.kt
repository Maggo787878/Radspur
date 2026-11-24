package app.radspur.find

import android.content.Context
import androidx.work.*
import app.radspur.app
import app.radspur.net.Api
import java.util.concurrent.TimeUnit

/** Uploads all finished tours that are not on the server yet. */
class UploadWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext.app
        if (!app.prefs.configured) return Result.success()
        val dao = app.db.tours()
        val api = Api(app.prefs)
        return try {
            dao.notUploaded().forEach { t ->
                api.uploadTour(t, dao.points(t.id))
                dao.update(t.copy(uploaded = true))
            }
            Result.success()
        } catch (e: Exception) {
            app.prefs.lastError = "Upload: " + (e.message ?: e.javaClass.simpleName)
            if (runAttemptCount < 5) Result.retry() else Result.failure()
        }
    }

    companion object {
        fun enqueue(ctx: Context) {
            WorkManager.getInstance(ctx).enqueueUniqueWork(
                "upload", ExistingWorkPolicy.APPEND_OR_REPLACE,
                OneTimeWorkRequestBuilder<UploadWorker>()
                    .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                    .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 1, TimeUnit.MINUTES)
                    .build()
            )
        }
    }
}
