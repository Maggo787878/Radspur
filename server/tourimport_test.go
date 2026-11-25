package main

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

// makeGPX: 1-Sekunden-Punkte nach Norden mit speed m/s, optional Pause und GPS-Ausreißer.
func makeGPX(typ string, start time.Time, seconds int, speed float64, pauseAt, pauseLen int, spikeAt int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><gpx version="1.1" creator="Garmin Edge 840" xmlns="http://www.topografix.com/GPX/1/1">`)
	fmt.Fprintf(&b, `<trk><name>Testfahrt &amp; Co</name><type>%s</type><trkseg>`, typ)
	lat, t := 47.80, start
	for i := 0; i <= seconds; i++ {
		ele := 400 + 60*math.Sin(float64(i)/300) // bis zu +60/-60 m
		la := lat
		if i == spikeAt {
			la += 0.00012 // ~13 m Sprung für einen Punkt (kurz ~73 km/h)
		}
		fmt.Fprintf(&b, `<trkpt lat="%.7f" lon="13.0447"><ele>%.1f</ele><time>%s</time></trkpt>`, la, ele, t.UTC().Format(time.RFC3339))
		if i == pauseAt {
			t = t.Add(time.Duration(pauseLen) * time.Second) // Stillstand ohne Punkte
		}
		t = t.Add(time.Second)
		lat += speed / 111320
	}
	b.WriteString(`</trkseg></trk></gpx>`)
	return b.String()
}

func upload(t *testing.T, b *browser, gpx, query string) (int, string) {
	req, _ := http.NewRequest("POST", b.base+"/api/web/tours/import-gpx?"+query, strings.NewReader(gpx))
	req.Header.Set("X-Radspur", "1")
	req.Header.Set("Content-Type", "application/gpx+xml")
	res, err := b.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(data)
}

func TestImportGPX(t *testing.T) {
	_, ts := newTestServer(t)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	anna := inviteUser(t, ts, admin, "anna", "anna-pass-123")
	start := time.Date(2026, 6, 14, 7, 30, 0, 0, vienna)

	// 1 h Radfahrt mit 7 m/s, 10 min Pause nach 30 min, ein GPS-Ausreißer
	gpx := makeGPX("cycling", start, 3600, 7, 1800, 600, 2500)
	code, body := upload(t, anna, gpx, "sport=auto&tz=Europe/Vienna")
	if code != 200 {
		t.Fatalf("Import: %d %s", code, body)
	}
	var tours []tour
	anna.json("GET", "/api/web/tours", nil, &tours)
	if len(tours) != 1 {
		t.Fatalf("Touren: %d", len(tours))
	}
	tr := tours[0]
	if tr.Name != "Testfahrt & Co" || tr.Type != "bike" || tr.Source != "gpx" {
		t.Errorf("Name/Typ/Herkunft: %q %q %q", tr.Name, tr.Type, tr.Source)
	}
	if math.Abs(tr.DistanceM-25200) > 100 { // 3600 s × 7 m/s + Ausreißer hin/zurück
		t.Errorf("Distanz: %.0f", tr.DistanceM)
	}
	if tr.MovingS < 3550 || tr.MovingS > 3601 { // Pause zählt nicht
		t.Errorf("Zeit in Bewegung: %d s", tr.MovingS)
	}
	if v := tr.MaxSpeed * 3.6; v > 35 || v < 24 { // echte 25,2 km/h; Ausreißer darf nicht zählen
		t.Errorf("Höchstgeschwindigkeit: %.1f km/h", tr.MaxSpeed*3.6)
	}
	if tr.ElevGain < 100 || tr.ElevGain > 260 {
		t.Errorf("Höhenmeter: %.0f", tr.ElevGain)
	}
	if tr.StartTS != start.UnixMilli() {
		t.Errorf("Start: %v", time.UnixMilli(tr.StartTS))
	}

	// Gleiche Datei nochmal → keine Doppelung
	_, body = upload(t, anna, gpx, "sport=auto")
	anna.json("GET", "/api/web/tours", nil, &tours)
	if len(tours) != 1 || !strings.Contains(body, `"existed":true`) {
		t.Errorf("Doppelimport: %d Touren, %s", len(tours), body)
	}
	// Gleiche Datei bei anderem Benutzer → eigene Tour, kein Fehler
	code, _ = upload(t, admin, gpx, "sport=auto")
	expect(t, "gleiche Datei anderer Benutzer", code, 200)
	anna.json("GET", "/api/web/tours", nil, &tours)
	if len(tours) != 1 {
		t.Errorf("anna sieht fremden Import")
	}

	// Lauf wird aus <type> erkannt; Sportart lässt sich überschreiben
	run := makeGPX("running", start.Add(48*time.Hour), 1800, 3, -1, 0, -1)
	upload(t, anna, run, "sport=auto")
	anna.json("GET", "/api/web/tours", nil, &tours)
	if tours[0].Type != "run" {
		t.Errorf("Lauf nicht erkannt: %q", tours[0].Type)
	}
	upload(t, anna, makeGPX("cycling", start.Add(96*time.Hour), 600, 6, -1, 0, -1), "sport=run&name=Manuell+benannt")
	anna.json("GET", "/api/web/tours", nil, &tours)
	if tours[0].Type != "run" || tours[0].Name != "Manuell benannt" {
		t.Errorf("Sportart/Name überschreiben: %q %q", tours[0].Type, tours[0].Name)
	}

	// Geplante Route ohne Zeitstempel / Unsinn
	code, body = upload(t, anna, `<gpx><trk><trkseg><trkpt lat="47.8" lon="13.0"><ele>400</ele></trkpt><trkpt lat="47.81" lon="13.0"/></trkseg></trk></gpx>`, "")
	if code != 422 || !strings.Contains(body, "keine Zeitstempel") {
		t.Errorf("ohne Zeit: %d %q", code, body)
	}
	code, _ = upload(t, anna, "das ist kein xml <<<", "")
	expect(t, "kein GPX", code, 422)

	// Große Datei (> 1 MB) wird angenommen
	big := makeGPX("cycling", start.Add(200*time.Hour), 20000, 5, -1, 0, -1)
	if len(big) < 1<<20 {
		t.Fatalf("Testdatei zu klein: %d", len(big))
	}
	code, body = upload(t, anna, big, "")
	if code != 200 {
		t.Errorf("große Datei (%d KB): %d %s", len(big)>>10, code, body)
	}

	// Detail mit Punkten, GPX-Export
	var detail tour
	anna.json("GET", fmt.Sprintf("/api/web/tours/%d", tours[len(tours)-1].ID), nil, &detail)
	if len(detail.Points) != 3601 || detail.Points[0].Alt == nil {
		t.Errorf("Punkte: %d", len(detail.Points))
	}
}

func TestManualTourAndPatch(t *testing.T) {
	_, ts := newTestServer(t)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	start := time.Now().Add(-48 * time.Hour).UnixMilli()
	ok := map[string]any{"name": "Zur Arbeit", "type": "bike", "startTs": start, "distanceM": 18500, "movingS": 2700, "elevGain": 120}
	var res struct{ ID int64 }
	if c := admin.json("POST", "/api/web/tours/manual", ok, &res); c != 200 {
		t.Fatalf("manuell: %d", c)
	}
	for name, bad := range map[string]map[string]any{
		"Zukunft":      {"type": "bike", "startTs": time.Now().Add(72 * time.Hour).UnixMilli(), "distanceM": 1000, "movingS": 600},
		"zu schnell":   {"type": "bike", "startTs": start, "distanceM": 100000, "movingS": 600},
		"0 km":         {"type": "bike", "startTs": start, "distanceM": 0, "movingS": 600},
		"Sportart":     {"type": "ski", "startTs": start, "distanceM": 1000, "movingS": 600},
		"Höhe negativ": {"type": "run", "startTs": start, "distanceM": 1000, "movingS": 600, "elevGain": -5},
	} {
		c, _ := admin.do("POST", "/api/web/tours/manual", bad)
		expect(t, "manuell "+name, c, 400)
	}
	var tours []tour
	admin.json("GET", "/api/web/tours", nil, &tours)
	if len(tours) != 1 || tours[0].Source != "manual" || tours[0].DistanceM != 18500 || tours[0].EndTS != start+2700_000 {
		t.Fatalf("manuelle Tour: %+v", tours)
	}
	// zählt in der Statistik
	var st struct{ Total statTotals }
	admin.json("GET", "/api/web/stats", nil, &st)
	if st.Total.Count != 1 || st.Total.BikeM != 18500 {
		t.Errorf("Statistik: %+v", st.Total)
	}
	// umbenennen / Sportart ändern; fremde Tour nicht
	id := fmt.Sprint(tours[0].ID)
	c, _ := admin.do("PATCH", "/api/web/tours/"+id, map[string]string{"name": "Pendeln", "type": "run"})
	expect(t, "ändern", c, 204)
	admin.json("GET", "/api/web/tours", nil, &tours)
	if tours[0].Name != "Pendeln" || tours[0].Type != "run" {
		t.Errorf("nach Ändern: %q %q", tours[0].Name, tours[0].Type)
	}
	anna := inviteUser(t, ts, admin, "anna", "anna-pass-123")
	c, _ = anna.do("PATCH", "/api/web/tours/"+id, map[string]string{"name": "GEKAPERT", "type": "bike"})
	expect(t, "fremde Tour ändern", c, 404)
}
