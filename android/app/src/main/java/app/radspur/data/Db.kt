package app.radspur.data

import android.content.Context
import androidx.room.*
import kotlinx.coroutines.flow.Flow

@Entity(tableName = "tours", indices = [Index("clientId", unique = true)])
data class Tour(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    val clientId: String,
    val name: String,
    val startTs: Long,
    /** 0 while the tour is being recorded. */
    val endTs: Long = 0,
    val distanceM: Double = 0.0,
    val movingS: Long = 0,
    val maxSpeed: Double = 0.0,
    val elevGain: Double = 0.0,
    val uploaded: Boolean = false,
)

@Entity(
    tableName = "points",
    foreignKeys = [ForeignKey(Tour::class, ["id"], ["tourId"], onDelete = ForeignKey.CASCADE)],
    indices = [Index("tourId")]
)
data class TrackPoint(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    val tourId: Long,
    val ts: Long,
    val lat: Double,
    val lon: Double,
    val alt: Double?,
    val speed: Float?,
    val acc: Float?,
)

@Dao
interface TourDao {
    @Insert suspend fun insert(t: Tour): Long
    @Update suspend fun update(t: Tour)
    @Delete suspend fun delete(t: Tour)
    @Insert suspend fun insertPoint(p: TrackPoint)

    @Query("SELECT * FROM tours WHERE endTs > 0 ORDER BY startTs DESC")
    fun finishedTours(): Flow<List<Tour>>

    @Query("SELECT * FROM tours WHERE id = :id")
    fun tourFlow(id: Long): Flow<Tour?>

    @Query("SELECT * FROM tours WHERE id = :id")
    suspend fun tour(id: Long): Tour?

    @Query("SELECT * FROM tours WHERE endTs = 0 LIMIT 1")
    suspend fun activeTour(): Tour?

    @Query("SELECT * FROM tours WHERE endTs > 0 AND uploaded = 0")
    suspend fun notUploaded(): List<Tour>

    @Query("SELECT * FROM points WHERE tourId = :tourId ORDER BY ts")
    suspend fun points(tourId: Long): List<TrackPoint>
}

@Database(entities = [Tour::class, TrackPoint::class], version = 1, exportSchema = false)
abstract class AppDb : RoomDatabase() {
    abstract fun tours(): TourDao

    companion object {
        fun create(ctx: Context) = Room.databaseBuilder(ctx, AppDb::class.java, "radspur.db").build()
    }
}
