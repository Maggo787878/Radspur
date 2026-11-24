package app.radspur.tracking

import android.location.Location

/**
 * Incremental tour statistics from GPS fixes. Filters inaccurate fixes,
 * ignores standstill jitter and smooths altitude with a hysteresis.
 */
class TourCalculator(
    var distanceM: Double = 0.0,
    var movingS: Long = 0,
    var maxSpeed: Double = 0.0,
    var elevGain: Double = 0.0,
) {
    private var last: Location? = null
    private var refAlt: Double? = null

    /** Returns true when the fix should be stored as a track point. */
    fun add(loc: Location): Boolean {
        if (!loc.hasAccuracy() || loc.accuracy > MAX_ACCURACY_M) return false
        val prev = last
        if (prev == null) {
            last = loc
            if (loc.hasAltitude()) refAlt = loc.altitude
            return true
        }
        val dt = (loc.time - prev.time) / 1000.0
        if (dt <= 0) return false
        val d = prev.distanceTo(loc).toDouble()
        val speed = if (loc.hasSpeed()) loc.speed.toDouble() else d / dt

        // Standstill: GPS drifts a few metres; don't count it as distance.
        val moving = speed >= MIN_MOVING_SPEED && d >= minOf(loc.accuracy.toDouble(), 8.0) * 0.5
        if (!moving && d < STORE_MIN_DIST_M) return false

        if (moving) {
            distanceM += d
            if (dt <= MAX_GAP_S) movingS += dt.toLong().coerceAtLeast(1)
            if (loc.accuracy <= 20 && speed in 0.0..MAX_PLAUSIBLE_SPEED) maxSpeed = maxOf(maxSpeed, speed)
        }
        if (loc.hasAltitude()) {
            val ref = refAlt
            when {
                ref == null -> refAlt = loc.altitude
                loc.altitude - ref > ELEV_HYSTERESIS_M -> { elevGain += loc.altitude - ref; refAlt = loc.altitude }
                ref - loc.altitude > ELEV_HYSTERESIS_M -> refAlt = loc.altitude
            }
        }
        last = loc
        return true
    }

    /** After a pause the next fix starts a new segment without a distance jump. */
    fun resetSegment() { last = null }

    companion object {
        const val MAX_ACCURACY_M = 30f
        const val MIN_MOVING_SPEED = 0.8      // m/s ≈ 3 km/h
        const val STORE_MIN_DIST_M = 3.0
        const val MAX_GAP_S = 15.0
        const val MAX_PLAUSIBLE_SPEED = 25.0  // m/s = 90 km/h
        const val ELEV_HYSTERESIS_M = 5.0
    }
}
