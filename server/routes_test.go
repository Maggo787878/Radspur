package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeBrouter liefert eine gerade Linie zwischen den Wegpunkten mit n Zwischenpunkten.
func fakeBrouter(t *testing.T, n int) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ll := strings.Split(r.URL.Query().Get("lonlats"), "|")
		if strings.HasPrefix(ll[0], "0.000000") {
			http.Error(w, "position not mapped in existing datafile", 500)
			return
		}
		var coords [][]float64
		var a, b [2]float64
		fmt.Sscanf(ll[0], "%f,%f", &a[0], &a[1])
		fmt.Sscanf(ll[len(ll)-1], "%f,%f", &b[0], &b[1])
		for i := 0; i <= n; i++ {
			f := float64(i) / float64(n)
			// leichte Zickzack-Höhe → Anstieg/Abstieg
			// kurvige Strecke wie eine echte Route, Höhe auf und ab
			coords = append(coords, []float64{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f + 0.01*math.Sin(float64(i)/40), 400 + 80*math.Sin(float64(i)/25)})
		}
		json.NewEncoder(w).Encode(map[string]any{"type": "FeatureCollection", "features": []any{map[string]any{
			"type": "Feature", "properties": map[string]string{"track-length": "12345", "filtered ascend": "210", "total-time": "2700"},
			"geometry": map[string]any{"type": "LineString", "coordinates": coords}}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func routeServer(t *testing.T, points int) *httptest.Server {
	cfg := testConfig(filepath.Join(t.TempDir(), "r.db"))
	cfg.BrouterURL = fakeBrouter(t, points).URL
	_, ts := startServer(t, cfg)
	return ts
}

var salzburgGmunden = [][2]float64{{47.8095, 13.0447}, {47.9187, 13.7999}}

func TestRouteCalcSaveAndIsolation(t *testing.T) {
	ts := routeServer(t, 300)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	anna := inviteUser(t, ts, admin, "anna", "anna-pass-123")

	var profiles []map[string]string
	anna.json("GET", "/api/web/route-profiles", nil, &profiles)
	if len(profiles) == 0 || profiles[0]["id"] != "trekking" || profiles[0]["label"] == "" {
		t.Errorf("Profile: %+v", profiles)
	}
	var calc routeResult
	if c := anna.json("POST", "/api/web/route-calc", routeIn{Profile: "trekking", Waypoints: salzburgGmunden}, &calc); c != 200 {
		t.Fatalf("Berechnung: %d", c)
	}
	if calc.DistanceM != 12345 || calc.AscendM != 210 || calc.TimeS != 2700 || len(calc.Track) != 301 || calc.DescendM <= 0 {
		t.Errorf("Ergebnis: dist=%v asc=%v desc=%v time=%v pts=%d", calc.DistanceM, calc.AscendM, calc.DescendM, calc.TimeS, len(calc.Track))
	}
	if calc.Track[0][0] != 47.8095 || calc.Track[0][1] != 13.0447 {
		t.Errorf("Koordinatenreihenfolge (lat,lon) falsch: %v", calc.Track[0])
	}

	// Fehlerfälle
	c, body := anna.do("POST", "/api/web/route-calc", routeIn{Profile: "car-fast", Waypoints: salzburgGmunden})
	expect(t, "Autoprofil", c, 422)
	c, body = anna.do("POST", "/api/web/route-calc", routeIn{Profile: "trekking", Waypoints: salzburgGmunden[:1]})
	expect(t, "nur ein Punkt", c, 422)
	c, body = anna.do("POST", "/api/web/route-calc", routeIn{Profile: "trekking", Waypoints: [][2]float64{{0, 0}, {1, 1}}})
	if c != 422 || !strings.Contains(body, "außerhalb der geladenen Kartendaten") {
		t.Errorf("Punkt außerhalb: %d %q", c, body)
	}
	many := make([][2]float64, 31)
	for i := range many {
		many[i] = [2]float64{47 + float64(i)*0.01, 13}
	}
	c, _ = anna.do("POST", "/api/web/route-calc", routeIn{Profile: "trekking", Waypoints: many})
	expect(t, "31 Punkte", c, 422)

	// Speichern, Liste, Laden, Ändern
	var created struct{ ID int64 }
	anna.json("POST", "/api/web/routes", routeIn{Name: "Salzkammergut <Runde>", Profile: "trekking", Waypoints: salzburgGmunden}, &created)
	id := fmt.Sprint(created.ID)
	var list []savedRoute
	anna.json("GET", "/api/web/routes", nil, &list)
	if len(list) != 1 || list[0].Name != "Salzkammergut <Runde>" || list[0].Track != nil {
		t.Fatalf("Liste: %+v", list)
	}
	var got savedRoute
	anna.json("GET", "/api/web/routes/"+id, nil, &got)
	if len(got.Waypoints) != 2 || len(got.Track) != 301 {
		t.Errorf("Laden: %d Wegpunkte, %d Trackpunkte", len(got.Waypoints), len(got.Track))
	}
	c, _ = anna.do("PUT", "/api/web/routes/"+id, routeIn{Name: "Umbenannt", Profile: "fastbike", Waypoints: append(salzburgGmunden, [2]float64{47.8, 13.05})})
	expect(t, "Ändern", c, 200)
	anna.json("GET", "/api/web/routes/"+id, nil, &got)
	if got.Name != "Umbenannt" || got.Profile != "fastbike" || len(got.Waypoints) != 3 {
		t.Errorf("nach Ändern: %+v", got.Name)
	}

	// GPX
	code, gpx := anna.do("GET", "/routes/"+id+"/gpx", nil)
	if code != 200 || !strings.Contains(gpx, "<name>Umbenannt</name>") || strings.Count(gpx, "<trkpt") != 301 || !strings.Contains(gpx, "<ele>") {
		t.Errorf("GPX: %d, %d Punkte", code, strings.Count(gpx, "<trkpt"))
	}

	// Andere Benutzer: kein Zugriff
	for _, p := range []struct{ m, path string }{{"GET", "/api/web/routes/" + id}, {"DELETE", "/api/web/routes/" + id}, {"GET", "/routes/" + id + "/gpx"}} {
		c, _ := admin.do(p.m, p.path, nil)
		expect(t, "Admin "+p.m+" "+p.path, c, 404)
	}
	c, _ = admin.do("PUT", "/api/web/routes/"+id, routeIn{Name: "GEKAPERT", Profile: "trekking", Waypoints: salzburgGmunden})
	expect(t, "Admin überschreibt fremde Route", c, 404)
	admin.json("GET", "/api/web/routes", nil, &list)
	if len(list) != 0 {
		t.Errorf("Admin sieht fremde Routen: %d", len(list))
	}
	anna.json("GET", "/api/web/routes/"+id, nil, &got)
	if got.Name != "Umbenannt" {
		t.Errorf("Route wurde von Fremdem geändert: %q", got.Name)
	}
	c, _ = newBrowser(t, ts).do("GET", "/routes/"+id+"/gpx", nil)
	expect(t, "GPX ohne Anmeldung", c, 401)

	c, _ = anna.do("DELETE", "/api/web/routes/"+id, nil)
	expect(t, "Löschen", c, 204)
	c, _ = anna.do("GET", "/api/web/routes/"+id, nil)
	expect(t, "nach Löschen", c, 404)
}

func TestLongRouteSimplifiedForGPX(t *testing.T) {
	ts := routeServer(t, 20000)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	var created struct{ ID int64 }
	admin.json("POST", "/api/web/routes", routeIn{Name: "Lang", Profile: "trekking", Waypoints: salzburgGmunden}, &created)
	_, gpx := admin.do("GET", fmt.Sprintf("/routes/%d/gpx", created.ID), nil)
	n := strings.Count(gpx, "<trkpt")
	if n > maxGPXPoints || n < 100 {
		t.Errorf("GPX-Punkte nach Vereinfachung: %d", n)
	}
}

func TestSimplifyKeepsShape(t *testing.T) {
	// Gerade mit einem deutlichen Knick: Endpunkte und Knick müssen bleiben.
	var tr [][3]float64
	for i := 0; i <= 1000; i++ {
		tr = append(tr, [3]float64{47 + float64(i)*0.00001, 13, 0})
	}
	for i := 1; i <= 1000; i++ {
		tr = append(tr, [3]float64{47.01, 13 + float64(i)*0.00001, 0})
	}
	s := simplify(tr, 100)
	if len(s) != 3 || s[1] != tr[1000] {
		t.Errorf("vereinfacht: %d Punkte, Knick %v", len(s), s)
	}
}

func TestGeocodeCached(t *testing.T) {
	var hits int32
	nom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Radspur/") {
			t.Errorf("User-Agent: %q", r.Header.Get("User-Agent"))
		}
		w.Write([]byte(`[{"display_name":"Gmunden, Oberösterreich, Österreich","lat":"47.918","lon":"13.799"}]`))
	}))
	defer nom.Close()
	cfg := testConfig(filepath.Join(t.TempDir(), "g.db"))
	cfg.GeocoderURL = nom.URL
	geo.cache = map[string][]geoHit{}
	_, ts := startServer(t, cfg)
	b := newBrowser(t, ts)
	b.login("admin", adminPW)
	var res []geoHit
	b.json("GET", "/api/web/geocode?q=Gmunden", nil, &res)
	b.json("GET", "/api/web/geocode?q=gmunden", nil, &res)
	if len(res) != 1 || res[0].Lat != 47.918 || hits != 1 {
		t.Errorf("Treffer %+v, Anfragen an Nominatim: %d", res, hits)
	}
	c, _ := newBrowser(t, ts).do("GET", "/api/web/geocode?q=Wien", nil)
	expect(t, "Ortssuche ohne Anmeldung", c, 401)
}

func TestBrouterUnreachableMessages(t *testing.T) {
	for _, c := range []struct{ url, want string }{
		{"http://brouter-gibt-es-nicht.invalid:17777", "nicht gefunden"},
		{"http://127.0.0.1:1", "startet noch"},
	} {
		cfg := testConfig(filepath.Join(t.TempDir(), "u.db"))
		cfg.BrouterURL = c.url
		_, ts := startServer(t, cfg)
		b := newBrowser(t, ts)
		b.login("admin", adminPW)
		code, body := b.do("POST", "/api/web/route-calc", routeIn{Profile: "trekking", Waypoints: salzburgGmunden})
		if code != 422 || !strings.Contains(body, c.want) {
			t.Errorf("%s: %d %q", c.url, code, body)
		}
	}
}
