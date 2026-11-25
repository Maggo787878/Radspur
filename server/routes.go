package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---------- Routenplanung: BRouter (Berechnung) + Nominatim (Ortssuche) ----------

// Profile, die in der Weboberfläche angeboten werden (BRouter-Profilname → Anzeige).
var routeProfiles = []struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}{
	{"trekking", "Trekking-/Tourenrad"},
	{"fastbike", "Rennrad"},
	{"gravel", "Gravel"},
	{"mtb", "Mountainbike"},
	{"safety", "Sicher (wenig Verkehr)"},
	{"hiking-mountain", "Zu Fuß / Laufen"},
}

func validProfile(p string) bool {
	for _, x := range routeProfiles {
		if x.ID == p {
			return true
		}
	}
	return false
}

const maxWaypoints = 30
const userAgent = "Radspur/1.0 (+https://github.com/Maggo787878/Radspur)"

var httpClient = &http.Client{Timeout: 60 * time.Second}

type routeResult struct {
	Track     [][3]float64 `json:"track"` // [lat, lon, ele]
	DistanceM float64      `json:"distanceM"`
	AscendM   float64      `json:"ascendM"`
	DescendM  float64      `json:"descendM"`
	TimeS     int64        `json:"timeS"`
}

type routeError struct{ msg string }

func (e routeError) Error() string { return e.msg }

func validWaypoints(wps [][2]float64) error {
	if len(wps) < 2 {
		return routeError{"Mindestens Start und Ziel setzen"}
	}
	if len(wps) > maxWaypoints {
		return routeError{fmt.Sprintf("Höchstens %d Punkte", maxWaypoints)}
	}
	for _, p := range wps {
		if p[0] < -90 || p[0] > 90 || p[1] < -180 || p[1] > 180 {
			return routeError{"Ungültige Koordinate"}
		}
	}
	return nil
}

// calcRoute fragt BRouter. Wegpunkte als [lat, lon].
func (s *server) calcRoute(ctx context.Context, wps [][2]float64, profile string) (*routeResult, error) {
	if err := validWaypoints(wps); err != nil {
		return nil, err
	}
	if !validProfile(profile) {
		return nil, routeError{"Unbekanntes Profil"}
	}
	ll := make([]string, len(wps))
	for i, p := range wps {
		ll[i] = strconv.FormatFloat(p[1], 'f', 6, 64) + "," + strconv.FormatFloat(p[0], 'f', 6, 64)
	}
	q := url.Values{"lonlats": {strings.Join(ll, "|")}, "profile": {profile}, "alternativeidx": {"0"}, "format": {"geojson"}}
	req, _ := http.NewRequestWithContext(ctx, "GET", s.cfg.BrouterURL+"/brouter?"+q.Encode(), nil)
	res, err := httpClient.Do(req)
	if err != nil {
		log.Print("BRouter: ", err)
		return nil, routeError{brouterUnreachable(err)}
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if res.StatusCode != 200 {
		msg := strings.TrimSpace(string(body))
		log.Printf("BRouter %d: %s", res.StatusCode, msg)
		switch {
		case strings.Contains(msg, "not mapped"), strings.Contains(msg, "datafile"):
			return nil, routeError{"Ein Punkt liegt außerhalb der geladenen Kartendaten (BROUTER_SEGMENTS)"}
		case strings.Contains(msg, "no track"), strings.Contains(msg, "not found"):
			return nil, routeError{"Keine Route gefunden – Punkt näher an einen Weg setzen"}
		}
		return nil, routeError{"Routenberechnung fehlgeschlagen"}
	}
	var gj struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Coordinates [][]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &gj); err != nil || len(gj.Features) == 0 {
		return nil, routeError{"Unerwartete Antwort der Routenberechnung"}
	}
	f := gj.Features[0]
	num := func(k string) float64 {
		switch v := f.Properties[k].(type) {
		case string:
			x, _ := strconv.ParseFloat(v, 64)
			return x
		case float64:
			return v
		}
		return 0
	}
	r := &routeResult{DistanceM: num("track-length"), AscendM: num("filtered ascend"), TimeS: int64(num("total-time"))}
	r.Track = make([][3]float64, 0, len(f.Geometry.Coordinates))
	for _, c := range f.Geometry.Coordinates {
		p := [3]float64{c[1], c[0], 0}
		if len(c) > 2 {
			p[2] = math.Round(c[2]*10) / 10
		}
		r.Track = append(r.Track, p)
	}
	r.DescendM = descend(r.Track)
	return r, nil
}

// brouterUnreachable übersetzt Verbindungsfehler in einen Hinweis, was zu tun ist.
func brouterUnreachable(err error) string {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr):
		return "Routenberechnung: BRouter-Container nicht gefunden – docker-compose.yml aktuell (git pull) und mit „docker compose up -d“ gestartet?"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "Routenberechnung startet noch – beim ersten Start lädt BRouter die Kartendaten (docker logs radspur-brouter)"
	case errors.Is(err, context.DeadlineExceeded), os.IsTimeout(err):
		return "Routenberechnung antwortet nicht rechtzeitig – Route kürzer planen oder später erneut versuchen"
	}
	return "Routenberechnung nicht erreichbar – läuft der BRouter-Container? (docker logs radspur-brouter)"
}

// descend: Abstieg mit derselben Glättung (5 m Hysterese) wie die App.
func descend(t [][3]float64) float64 {
	if len(t) == 0 {
		return 0
	}
	ref, sum := t[0][2], 0.0
	for _, p := range t[1:] {
		switch {
		case ref-p[2] > 5:
			sum += ref - p[2]
			ref = p[2]
		case p[2]-ref > 5:
			ref = p[2]
		}
	}
	return math.Round(sum)
}

// ---------- Ortssuche (Nominatim) ----------
// Nominatim-Nutzungsregeln: höchstens 1 Anfrage/Sekunde, eindeutiger User-Agent, Ergebnisse cachen.

type geoHit struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

type geocoder struct {
	mu    sync.Mutex
	last  time.Time
	cache map[string][]geoHit
}

var geo = &geocoder{cache: map[string][]geoHit{}}

func (s *server) geocode(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 || len(q) > 200 {
		writeJSON(w, []geoHit{})
		return
	}
	key := strings.ToLower(q)
	geo.mu.Lock()
	defer geo.mu.Unlock()
	if hits, ok := geo.cache[key]; ok {
		writeJSON(w, hits)
		return
	}
	if wait := time.Second - time.Since(geo.last); wait > 0 {
		time.Sleep(wait)
	}
	geo.last = time.Now()
	u := s.cfg.GeocoderURL + "/search?" + url.Values{
		"q": {q}, "format": {"jsonv2"}, "limit": {"6"}, "accept-language": {"de"},
	}.Encode()
	req, _ := http.NewRequestWithContext(r.Context(), "GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	res, err := httpClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		if err == nil {
			res.Body.Close()
			err = fmt.Errorf("HTTP %d", res.StatusCode)
		}
		log.Print("Ortssuche: ", err)
		http.Error(w, "Ortssuche nicht erreichbar", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	var raw []struct {
		DisplayName string `json:"display_name"`
		Lat, Lon    string
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&raw); err != nil {
		http.Error(w, "Ortssuche: unerwartete Antwort", http.StatusBadGateway)
		return
	}
	hits := []geoHit{}
	for _, x := range raw {
		lat, _ := strconv.ParseFloat(x.Lat, 64)
		lon, _ := strconv.ParseFloat(x.Lon, 64)
		hits = append(hits, geoHit{x.DisplayName, lat, lon})
	}
	if len(geo.cache) > 2000 {
		geo.cache = map[string][]geoHit{}
	}
	geo.cache[key] = hits
	writeJSON(w, hits)
}

// ---------- API: berechnen, speichern, laden, GPX ----------

func (s *server) listProfiles(w http.ResponseWriter, r *http.Request) { writeJSON(w, routeProfiles) }

type routeIn struct {
	Name      string       `json:"name"`
	Profile   string       `json:"profile"`
	Waypoints [][2]float64 `json:"waypoints"`
}

func (s *server) routeCalc(w http.ResponseWriter, r *http.Request) {
	var in routeIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	res, err := s.calcRoute(r.Context(), in.Waypoints, in.Profile)
	if err != nil {
		routeErr(w, err)
		return
	}
	writeJSON(w, res)
}

func routeErr(w http.ResponseWriter, err error) {
	var re routeError
	if errors.As(err, &re) {
		http.Error(w, re.msg, http.StatusUnprocessableEntity)
		return
	}
	httpErr(w, err, 500)
}

type savedRoute struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	Profile   string       `json:"profile"`
	Waypoints [][2]float64 `json:"waypoints,omitempty"`
	Track     [][3]float64 `json:"track,omitempty"`
	DistanceM float64      `json:"distanceM"`
	AscendM   float64      `json:"ascendM"`
	DescendM  float64      `json:"descendM"`
	TimeS     int64        `json:"timeS"`
	Created   int64        `json:"created"`
	Updated   int64        `json:"updated"`
}

func cleanName(n string) string {
	n = strings.TrimSpace(n)
	if len([]rune(n)) > 80 {
		n = string([]rune(n)[:80])
	}
	if n == "" {
		n = "Route " + time.Now().Format("02.01.2006 15:04")
	}
	return n
}

// saveRoute legt an (id == 0) oder aktualisiert; die Strecke wird immer am
// Server aus Wegpunkten und Profil berechnet.
func (s *server) saveRoute(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	var id int64
	if v := r.PathValue("id"); v != "" {
		id, _ = strconv.ParseInt(v, 10, 64)
	}
	var in routeIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	res, err := s.calcRoute(r.Context(), in.Waypoints, in.Profile)
	if err != nil {
		routeErr(w, err)
		return
	}
	wp, _ := json.Marshal(in.Waypoints)
	tr, _ := json.Marshal(res.Track)
	name, now := cleanName(in.Name), nowMs()
	if id == 0 {
		x, err := s.db.Exec(`INSERT INTO routes(user_id,name,profile,waypoints,track,distance_m,ascend_m,descend_m,time_s,created,updated)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`, uid, name, in.Profile, string(wp), string(tr), res.DistanceM, res.AscendM, res.DescendM, res.TimeS, now, now)
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		id, _ = x.LastInsertId()
	} else {
		x, err := s.db.Exec(`UPDATE routes SET name=?,profile=?,waypoints=?,track=?,distance_m=?,ascend_m=?,descend_m=?,time_s=?,updated=?
			WHERE id=? AND user_id=?`, name, in.Profile, string(wp), string(tr), res.DistanceM, res.AscendM, res.DescendM, res.TimeS, now, id, uid)
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		if n, _ := x.RowsAffected(); n == 0 {
			http.NotFound(w, r)
			return
		}
	}
	writeJSON(w, map[string]int64{"id": id})
}

func (s *server) listRoutes(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,name,profile,distance_m,ascend_m,descend_m,time_s,created,updated
		FROM routes WHERE user_id=? ORDER BY updated DESC`, currentUser(r).ID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer rows.Close()
	out := []savedRoute{}
	for rows.Next() {
		var x savedRoute
		if err := rows.Scan(&x.ID, &x.Name, &x.Profile, &x.DistanceM, &x.AscendM, &x.DescendM, &x.TimeS, &x.Created, &x.Updated); err != nil {
			httpErr(w, err, 500)
			return
		}
		out = append(out, x)
	}
	writeJSON(w, out)
}

func (s *server) loadRoute(r *http.Request) (*savedRoute, error) {
	var x savedRoute
	var wp, tr string
	err := s.db.QueryRow(`SELECT id,name,profile,waypoints,track,distance_m,ascend_m,descend_m,time_s,created,updated
		FROM routes WHERE id=? AND user_id=?`, r.PathValue("id"), currentUser(r).ID).
		Scan(&x.ID, &x.Name, &x.Profile, &wp, &tr, &x.DistanceM, &x.AscendM, &x.DescendM, &x.TimeS, &x.Created, &x.Updated)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(wp), &x.Waypoints)
	_ = json.Unmarshal([]byte(tr), &x.Track)
	return &x, nil
}

func (s *server) getRoute(w http.ResponseWriter, r *http.Request) {
	x, err := s.loadRoute(r)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		httpErr(w, err, 500)
		return
	}
	writeJSON(w, x)
}

func (s *server) deleteRoute(w http.ResponseWriter, r *http.Request) {
	x, err := s.db.Exec(`DELETE FROM routes WHERE id=? AND user_id=?`, r.PathValue("id"), currentUser(r).ID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	if n, _ := x.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxGPXPoints: ältere Garmin-Geräte kürzen Strecken mit mehr Punkten.
const maxGPXPoints = 9000

// simplify reduziert Punkte (Douglas-Peucker), Toleranz steigt bis die Grenze passt.
func simplify(t [][3]float64, max int) [][3]float64 {
	if len(t) <= max {
		return t
	}
	for tol := 2.0; ; tol *= 1.5 {
		keep := make([]bool, len(t))
		keep[0], keep[len(t)-1] = true, true
		dp(t, 0, len(t)-1, tol, keep)
		out := make([][3]float64, 0, max)
		for i, k := range keep {
			if k {
				out = append(out, t[i])
			}
		}
		if len(out) <= max {
			return out
		}
	}
}

// Abstand Punkt–Strecke in Metern (lokal flach genähert).
func segDist(p, a, b [3]float64) float64 {
	ky := 111320.0
	kx := ky * math.Cos(a[0]*math.Pi/180)
	px, py := (p[1]-a[1])*kx, (p[0]-a[0])*ky
	bx, by := (b[1]-a[1])*kx, (b[0]-a[0])*ky
	l2 := bx*bx + by*by
	if l2 == 0 {
		return math.Hypot(px, py)
	}
	u := math.Max(0, math.Min(1, (px*bx+py*by)/l2))
	return math.Hypot(px-u*bx, py-u*by)
}

func dp(t [][3]float64, i, j int, tol float64, keep []bool) {
	if j <= i+1 {
		return
	}
	best, idx := 0.0, -1
	for k := i + 1; k < j; k++ {
		if d := segDist(t[k], t[i], t[j]); d > best {
			best, idx = d, k
		}
	}
	if best > tol {
		keep[idx] = true
		dp(t, i, idx, tol, keep)
		dp(t, idx, j, tol, keep)
	}
}

func fileName(n string) string {
	var b strings.Builder
	for _, r := range n {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('_')
		case strings.ContainsRune("äöüÄÖÜß", r):
			b.WriteString(map[rune]string{'ä': "ae", 'ö': "oe", 'ü': "ue", 'Ä': "Ae", 'Ö': "Oe", 'Ü': "Ue", 'ß': "ss"}[r])
		}
	}
	if b.Len() == 0 {
		return "route"
	}
	return b.String()
}

// routeGPX: GPX 1.1 als Track mit Höhen – von Garmin, Wahoo, Sigma, Hammerhead … als Strecke importierbar.
func (s *server) routeGPX(w http.ResponseWriter, r *http.Request) {
	x, err := s.loadRoute(r)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		httpErr(w, err, 500)
		return
	}
	pts := simplify(x.Track, maxGPXPoints)
	typ := "cycling"
	if x.Profile == "hiking-mountain" {
		typ = "hiking"
	}
	w.Header().Set("Content-Type", "application/gpx+xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.gpx"`, fileName(x.Name)))
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<gpx version="1.1" creator="Radspur" xmlns="http://www.topografix.com/GPX/1/1">` + "\n")
	fmt.Fprintf(&b, "<metadata><name>%s</name><time>%s</time></metadata>\n", xmlEscape(x.Name), time.UnixMilli(x.Updated).UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "<trk><name>%s</name><type>%s</type><trkseg>\n", xmlEscape(x.Name), typ)
	for _, p := range pts {
		fmt.Fprintf(&b, `<trkpt lat="%.6f" lon="%.6f"><ele>%.1f</ele></trkpt>`+"\n", p[0], p[1], p[2])
	}
	b.WriteString("</trkseg></trk></gpx>\n")
	w.Write([]byte(b.String()))
}
