package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// ---------- Touren nachtragen: GPX-Import und manuelle Eingabe ----------

type gpxPoint struct {
	lat, lon, ele float64
	hasEle        bool
	t             time.Time
}

type gpxFile struct {
	name, typ string
	pts       []gpxPoint
}

// parseGPX liest Track- (bzw. ersatzweise Routen-)Punkte mit Höhe und Zeit.
func parseGPX(r io.Reader) (*gpxFile, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	g := &gpxFile{}
	var cur *gpxPoint
	var path []string
	var text strings.Builder
	var rte []gpxPoint
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, routeError{"Datei ist kein gültiges GPX"}
		}
		switch e := tok.(type) {
		case xml.StartElement:
			path = append(path, e.Name.Local)
			text.Reset()
			if e.Name.Local == "trkpt" || e.Name.Local == "rtept" {
				cur = &gpxPoint{}
				for _, a := range e.Attr {
					switch a.Name.Local {
					case "lat":
						fmt.Sscan(a.Value, &cur.lat)
					case "lon":
						fmt.Sscan(a.Value, &cur.lon)
					}
				}
			}
		case xml.CharData:
			text.Write(e)
		case xml.EndElement:
			v := strings.TrimSpace(text.String())
			parent := ""
			if len(path) >= 2 {
				parent = path[len(path)-2]
			}
			switch {
			case cur != nil && e.Name.Local == "ele":
				if _, err := fmt.Sscan(v, &cur.ele); err == nil {
					cur.hasEle = true
				}
			case cur != nil && e.Name.Local == "time":
				cur.t, _ = time.Parse(time.RFC3339, v)
			case e.Name.Local == "trkpt" && cur != nil:
				g.pts = append(g.pts, *cur)
				cur = nil
			case e.Name.Local == "rtept" && cur != nil:
				rte = append(rte, *cur)
				cur = nil
			case e.Name.Local == "name" && (parent == "trk" || (parent == "metadata" && g.name == "")):
				g.name = v
			case e.Name.Local == "type" && parent == "trk":
				g.typ = strings.ToLower(v)
			}
			text.Reset()
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		}
	}
	if len(g.pts) == 0 {
		g.pts = rte
	}
	return g, nil
}

func haversine(a, b gpxPoint) float64 {
	const R = 6371000
	rad := math.Pi / 180
	dLat, dLon := (b.lat-a.lat)*rad, (b.lon-a.lon)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.lat*rad)*math.Cos(b.lat*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * R * math.Asin(math.Sqrt(h))
}

// sportFromGPX: Typangaben von Garmin, Strava, Komoot, Wahoo … auf bike/run abbilden.
func sportFromGPX(typ string) string {
	for _, k := range []string{"run", "lauf", "walk", "hik", "wander", "trail"} {
		if strings.Contains(typ, k) {
			return "run"
		}
	}
	return "bike"
}

// tourFromGPX berechnet die Kennzahlen nach denselben Regeln wie die App:
// Bewegung ab 0,8 m/s, Höhenmeter mit 5 m Hysterese, Höchstgeschwindigkeit
// über mindestens 10 s gemittelt (gegen GPS-Ausreißer).
func tourFromGPX(g *gpxFile) (tour, error) {
	var t tour
	pts := g.pts[:0:0]
	for _, p := range g.pts {
		if p.lat < -90 || p.lat > 90 || p.lon < -180 || p.lon > 180 || (p.lat == 0 && p.lon == 0) {
			continue
		}
		pts = append(pts, p)
	}
	if len(pts) < 2 {
		return t, routeError{"GPX enthält keinen Track"}
	}
	if pts[0].t.IsZero() || pts[len(pts)-1].t.IsZero() {
		return t, routeError{"GPX enthält keine Zeitstempel (geplante Route?) – Tour bitte von Hand eintragen"}
	}
	var dist, moving, maxSpeed, gain float64
	ref, hasRef := 0.0, false
	win := 0 // Anfang des Fensters für die Höchstgeschwindigkeit
	cum := []float64{0}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		d := haversine(a, b)
		dist += d
		cum = append(cum, dist)
		if dt := b.t.Sub(a.t).Seconds(); dt > 0 && dt <= 300 && d/dt >= 0.8 {
			moving += dt
		}
		for win < i && b.t.Sub(pts[win+1].t).Seconds() >= 10 {
			win++
		}
		if dt := b.t.Sub(pts[win].t).Seconds(); dt >= 10 {
			if v := (dist - cum[win]) / dt; v < 30 && v > maxSpeed {
				maxSpeed = v
			}
		}
		if b.hasEle {
			switch {
			case !hasRef:
				ref, hasRef = b.ele, true
			case b.ele-ref > 5:
				gain += b.ele - ref
				ref = b.ele
			case ref-b.ele > 5:
				ref = b.ele
			}
		} else if a.hasEle && !hasRef {
			ref, hasRef = a.ele, true
		}
	}
	t.StartTS = pts[0].t.UnixMilli()
	t.EndTS = pts[len(pts)-1].t.UnixMilli()
	t.DistanceM = math.Round(dist)
	t.MovingS = int64(moving)
	t.MaxSpeed = maxSpeed
	t.ElevGain = math.Round(gain)
	for _, p := range pts {
		tp := tourPoint{TS: p.t.UnixMilli(), Lat: p.lat, Lon: p.lon}
		if p.hasEle {
			e := p.ele
			tp.Alt = &e
		}
		t.Points = append(t.Points, tp)
	}
	// Gleiche Datei erneut hochgeladen → gleiche ID → keine doppelte Tour.
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%.5f|%.5f|%d", t.StartTS, t.EndTS, pts[0].lat, pts[0].lon, len(pts))))
	t.ClientID = "gpx-" + hex.EncodeToString(h[:12])
	return t, nil
}

func (s *server) storeTour(uid int64, t tour, source string) (id int64, existed bool, err error) {
	pts, _ := json.Marshal(t.Points)
	if t.Points == nil {
		pts = []byte("[]")
	}
	err = s.db.QueryRow(`SELECT id FROM tours WHERE client_id=? AND user_id=?`, t.ClientID, uid).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	res, err := s.db.Exec(`INSERT INTO tours(user_id,client_id,name,start_ts,end_ts,distance_m,moving_s,max_speed,elev_gain,points,type,source)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, uid, t.ClientID, t.Name, t.StartTS, t.EndTS, t.DistanceM, t.MovingS, t.MaxSpeed, t.ElevGain, string(pts), t.Type, source)
	if err != nil {
		return 0, false, err
	}
	id, _ = res.LastInsertId()
	return id, false, nil
}

func defaultTourName(typ string, ts int64, loc *time.Location) string {
	prefix := "Radtour"
	if typ == "run" {
		prefix = "Lauf"
	}
	return prefix + " " + time.UnixMilli(ts).In(loc).Format("02.01.2006 15:04")
}

// importGPX: Rohdaten einer GPX-Datei im Body; ?sport=auto|bike|run&name=…&tz=…
func (s *server) importGPX(w http.ResponseWriter, r *http.Request) {
	g, err := parseGPX(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "Datei zu groß (max. 25 MB)", http.StatusRequestEntityTooLarge)
			return
		}
		routeErr(w, err)
		return
	}
	t, err := tourFromGPX(g)
	if err != nil {
		routeErr(w, err)
		return
	}
	switch sp := r.URL.Query().Get("sport"); sp {
	case "bike", "run":
		t.Type = sp
	default:
		t.Type = sportFromGPX(g.typ)
	}
	t.Name = cleanTourName(r.URL.Query().Get("name"))
	if t.Name == "" {
		t.Name = cleanTourName(g.name)
	}
	if t.Name == "" {
		t.Name = defaultTourName(t.Type, t.StartTS, statsLocation(r))
	}
	uid := currentUser(r).ID
	t.ClientID = fmt.Sprintf("%s-%d", t.ClientID, uid) // client_id ist global eindeutig
	id, existed, err := s.storeTour(uid, t, "gpx")
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	writeJSON(w, map[string]any{"id": id, "existed": existed, "name": t.Name, "distanceM": t.DistanceM, "startTs": t.StartTS})
}

func cleanTourName(n string) string {
	n = strings.TrimSpace(n)
	if r := []rune(n); len(r) > 100 {
		n = string(r[:100])
	}
	return n
}

// manualTour: Tour ohne Streckenverlauf von Hand eintragen.
func (s *server) manualTour(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string  `json:"name"`
		Type      string  `json:"type"`
		StartTS   int64   `json:"startTs"`
		DistanceM float64 `json:"distanceM"`
		MovingS   int64   `json:"movingS"`
		ElevGain  float64 `json:"elevGain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	switch {
	case !sports[in.Type]:
		http.Error(w, "Sportart ungültig", 400)
	case in.StartTS < time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli() || in.StartTS > nowMs()+24*3600_000:
		http.Error(w, "Datum ungültig (nicht in der Zukunft)", 400)
	case in.DistanceM < 100 || in.DistanceM > 1_000_000:
		http.Error(w, "Distanz muss zwischen 0,1 und 1000 km liegen", 400)
	case in.MovingS < 60 || in.MovingS > 48*3600:
		http.Error(w, "Zeit muss zwischen 1 Minute und 48 Stunden liegen", 400)
	case in.DistanceM/float64(in.MovingS) > 30:
		http.Error(w, "Distanz und Zeit ergeben über 108 km/h – bitte prüfen", 400)
	case in.ElevGain < 0 || in.ElevGain > 20000:
		http.Error(w, "Höhenmeter müssen zwischen 0 und 20 000 liegen", 400)
	default:
		t := tour{ClientID: "manual-" + newToken("")[:24], Type: in.Type, StartTS: in.StartTS,
			EndTS: in.StartTS + in.MovingS*1000, DistanceM: in.DistanceM, MovingS: in.MovingS, ElevGain: in.ElevGain}
		t.Name = cleanTourName(in.Name)
		if t.Name == "" {
			t.Name = defaultTourName(t.Type, t.StartTS, statsLocation(r))
		}
		id, _, err := s.storeTour(currentUser(r).ID, t, "manual")
		if err != nil {
			httpErr(w, err, 500)
			return
		}
		writeJSON(w, map[string]int64{"id": id})
	}
}

// patchTour: Name und Sportart einer eigenen Tour ändern.
func (s *server) patchTour(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Type string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	name := cleanTourName(in.Name)
	if name == "" || !sports[in.Type] {
		http.Error(w, "Name und Sportart erforderlich", 400)
		return
	}
	res, err := s.db.Exec(`UPDATE tours SET name=?, type=? WHERE id=? AND user_id=?`, name, in.Type, r.PathValue("id"), currentUser(r).ID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
