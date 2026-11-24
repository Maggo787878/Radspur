package app.radspur.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import app.radspur.data.Sport
import app.radspur.data.Tour
import app.radspur.tracking.LiveTrack
import app.radspur.tracking.TrackStatus
import org.junit.Rule
import org.junit.Test
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneId

/** Rendert die Bildschirme ohne Gerät; kleines Gerät mit 130 % Schrift deckt Platzprobleme auf. */
abstract class ScreensTestBase(device: DeviceConfig) {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = device, showSystemUi = false)

    private val mapStub: @Composable (Modifier) -> Unit = { Box(it.background(Color(0xFFD7E3D0))) }
    private fun shot(content: @Composable () -> Unit) =
        paparazzi.snapshot { MaterialTheme { Surface { content() } } }

    private val zone = ZoneId.systemDefault()
    private fun ts(d: LocalDateTime) = d.atZone(zone).toInstant().toEpochMilli()
    private val bike = Tour(
        1, "a", "Radtour Di. 24.11.2026 15:04", ts(LocalDateTime.of(2026, 11, 24, 15, 4)), ts(LocalDateTime.of(2026, 11, 24, 17, 41)),
        distanceM = 48_730.0, movingS = 8_160, maxSpeed = 13.2, elevGain = 412.0, uploaded = true,
    )
    private val run = Tour(
        2, "b", "Lauf So. 22.11.2026 08:12", ts(LocalDateTime.of(2026, 11, 22, 8, 12)), ts(LocalDateTime.of(2026, 11, 22, 9, 1)),
        distanceM = 9_850.0, movingS = 2_890, maxSpeed = 4.4, elevGain = 64.0, type = Sport.RUN.id,
    )
    private val tours = buildList {
        add(bike); add(run)
        var i = 3L
        for (w in 1..20) {
            val d = LocalDateTime.of(2026, 11, 20, 9, 0).minusWeeks(w.toLong())
            add(bike.copy(id = i++, clientId = "w$w", startTs = ts(d), endTs = ts(d.plusHours(2)), distanceM = 15_000.0 + (w * 7919 % 50) * 1000))
        }
    }

    @Test fun recordIdle() = shot {
        RecordContent(LiveTrack(), Sport.BIKE, {}, false, {}, {}, {}, {}, mapStub)
    }

    @Test fun recordBike() = shot {
        RecordContent(
            LiveTrack(TrackStatus.RECORDING, Sport.BIKE, 1, 0, 23_480.0, 3_725, 12.9, 214.0, speed = 7.8, accuracy = 4f),
            Sport.BIKE, {}, false, {}, {}, {}, {}, mapStub,
        )
    }

    @Test fun recordRun() = shot {
        RecordContent(
            LiveTrack(TrackStatus.PAUSED, Sport.RUN, 1, 0, 7_310.0, 2_290, 4.6, 51.0, speed = 3.1, accuracy = 6f),
            Sport.RUN, {}, false, {}, {}, {}, {}, mapStub,
        )
    }

    @Test fun tourList() = shot { TourListContent(tours, null, {}, {}) }

    @Test fun tourDetail() = shot { TourDetailContent(bike, 2_874, true, {}, {}, {}, mapStub) }

    @Test fun tourDetailRun() = shot { TourDetailContent(run, 1_120, true, {}, {}, {}, mapStub) }

    @Test fun stats() = shot { StatsContent(tours, null, {}, LocalDate.of(2026, 11, 24), {}) }
}

class ScreensPixel5Test : ScreensTestBase(DeviceConfig.PIXEL_5)
class ScreensSmallLargeFontTest : ScreensTestBase(DeviceConfig.NEXUS_5.copy(fontScale = 1.3f))
