package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Alle Abfragen hier sind auf den angemeldeten Benutzer (currentUser) beschränkt.

// ---------- Positionen & Befehle ----------

type location struct {
	TS       int64    `json:"ts"`
	Lat      float64  `json:"lat"`
	Lon      float64  `json:"lon"`
	Acc      *float64 `json:"acc,omitempty"`
	Alt      *float64 `json:"alt,omitempty"`
	Speed    *float64 `json:"speed,omitempty"`
	Battery  *int     `json:"battery,omitempty"`
	Charging *bool    `json:"charging,omitempty"`
	Source   string   `json:"source,omitempty"`
}

type command struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Created   int64  `json:"created"`
	Delivered *int64 `json:"delivered,omitempty"`
	Acked     *int64 `json:"acked,omitempty"`
}

// postLocations speichert Positionen und antwortet mit offenen Befehlen,
// damit das Handy pro Check-in nur eine Anfrage braucht.
func (s *server) postLocations(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	var locs []location
	if err := json.NewDecoder(r.Body).Decode(&locs); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer tx.Rollback()
	for _, l := range locs {
		if l.Lat < -90 || l.Lat > 90 || l.Lon < -180 || l.Lon > 180 || l.TS <= 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO locations(user_id,ts,lat,lon,acc,alt,speed,battery,charging,source) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			uid, l.TS, l.Lat, l.Lon, l.Acc, l.Alt, l.Speed, l.Battery, l.Charging, l.Source); err != nil {
			httpErr(w, err, 500)
			return
		}
	}
	cmds, err := pendingCommands(tx, uid)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	if _, err := tx.Exec(`UPDATE commands SET delivered=? WHERE user_id=? AND acked IS NULL AND delivered IS NULL`, nowMs(), uid); err != nil {
		httpErr(w, err, 500)
		return
	}
	if err := tx.Commit(); err != nil {
		httpErr(w, err, 500)
		return
	}
	writeJSON(w, map[string]any{"commands": cmds})
}

func pendingCommands(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, uid int64) ([]command, error) {
	// Befehle älter als eine Stunde sind hinfällig.
	rows, err := q.Query(`SELECT id,type,created,delivered,acked FROM commands
		WHERE user_id=? AND acked IS NULL AND created > ? ORDER BY id`, uid, nowMs()-3600_000)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []command{}
	for rows.Next() {
		var c command
		if err := rows.Scan(&c.ID, &c.Type, &c.Created, &c.Delivered, &c.Acked); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *server) ackCommand(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if _, err := s.db.Exec(`UPDATE commands SET acked=? WHERE id=? AND user_id=? AND acked IS NULL`,
		nowMs(), id, currentUser(r).ID); err != nil {
		httpErr(w, err, 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var allowedCommands = map[string]bool{"ring": true, "stop_ring": true, "locate": true}

func (s *server) createCommand(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	var in struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !allowedCommands[in.Type] {
		httpErr(w, errors.New("ungültiger Befehl"), http.StatusBadRequest)
		return
	}
	if in.Type == "stop_ring" {
		// Ein noch nicht zugestelltes Klingeln wird einfach storniert.
		_, _ = s.db.Exec(`UPDATE commands SET acked=? WHERE user_id=? AND type='ring' AND delivered IS NULL AND acked IS NULL`, nowMs(), uid)
	}
	res, err := s.db.Exec(`INSERT INTO commands(user_id,type,created) VALUES(?,?,?)`, uid, in.Type, nowMs())
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, map[string]any{"id": id})
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	out := map[string]any{"last": nil}
	var l location
	var acc, alt, speed sql.NullFloat64
	var bat sql.NullInt64
	var chg sql.NullBool
	var src sql.NullString
	err := s.db.QueryRow(`SELECT ts,lat,lon,acc,alt,speed,battery,charging,source FROM locations
		WHERE user_id=? ORDER BY ts DESC LIMIT 1`, uid).
		Scan(&l.TS, &l.Lat, &l.Lon, &acc, &alt, &speed, &bat, &chg, &src)
	if err == nil {
		if acc.Valid {
			l.Acc = &acc.Float64
		}
		if alt.Valid {
			l.Alt = &alt.Float64
		}
		if speed.Valid {
			l.Speed = &speed.Float64
		}
		if bat.Valid {
			b := int(bat.Int64)
			l.Battery = &b
		}
		if chg.Valid {
			l.Charging = &chg.Bool
		}
		l.Source = src.String
		out["last"] = l
	} else if !errors.Is(err, sql.ErrNoRows) {
		httpErr(w, err, 500)
		return
	}
	cmds, err := pendingCommands(s.db, uid)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	out["pending"] = cmds
	writeJSON(w, out)
}

func (s *server) listLocations(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if since == 0 {
		since = nowMs() - 24*3600_000
	}
	rows, err := s.db.Query(`SELECT ts,lat,lon,acc FROM locations WHERE user_id=? AND ts >= ? ORDER BY ts LIMIT 5000`,
		currentUser(r).ID, since)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer rows.Close()
	out := []location{}
	for rows.Next() {
		var l location
		var acc sql.NullFloat64
		if err := rows.Scan(&l.TS, &l.Lat, &l.Lon, &acc); err != nil {
			httpErr(w, err, 500)
			return
		}
		if acc.Valid {
			l.Acc = &acc.Float64
		}
		out = append(out, l)
	}
	writeJSON(w, out)
}

// ---------- Touren ----------

type tourPoint struct {
	TS    int64    `json:"ts"`
	Lat   float64  `json:"lat"`
	Lon   float64  `json:"lon"`
	Alt   *float64 `json:"alt,omitempty"`
	Speed *float64 `json:"speed,omitempty"`
}

type tour struct {
	ID        int64       `json:"id"`
	ClientID  string      `json:"clientId"`
	Name      string      `json:"name"`
	StartTS   int64       `json:"startTs"`
	EndTS     int64       `json:"endTs"`
	DistanceM float64     `json:"distanceM"`
	MovingS   int64       `json:"movingS"`
	MaxSpeed  float64     `json:"maxSpeed"`
	ElevGain  float64     `json:"elevGain"`
	Type      string      `json:"type"`
	Points    []tourPoint `json:"points,omitempty"`
}

// Sportarten, wie sie die App sendet.
var sports = map[string]bool{"bike": true, "run": true}

func (s *server) postTour(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	var t tour
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	if t.ClientID == "" || len(t.Points) == 0 {
		httpErr(w, errors.New("clientId und points erforderlich"), http.StatusBadRequest)
		return
	}
	if !sports[t.Type] {
		t.Type = "bike" // ältere App-Versionen senden keinen Typ
	}
	if t.Name == "" {
		t.Name = time.UnixMilli(t.StartTS).Format("02.01.2006 15:04")
	}
	pts, _ := json.Marshal(t.Points)
	// Upsert nur auf eigene Touren: gehört die client_id einem anderen
	// Benutzer, greift das WHERE nicht und es wird nichts geändert.
	res, err := s.db.Exec(`INSERT INTO tours(user_id,client_id,name,start_ts,end_ts,distance_m,moving_s,max_speed,elev_gain,points,type)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(client_id) DO UPDATE SET name=excluded.name,start_ts=excluded.start_ts,end_ts=excluded.end_ts,
		distance_m=excluded.distance_m,moving_s=excluded.moving_s,max_speed=excluded.max_speed,elev_gain=excluded.elev_gain,
		points=excluded.points,type=excluded.type
		WHERE tours.user_id=excluded.user_id`,
		uid, t.ClientID, t.Name, t.StartTS, t.EndTS, t.DistanceM, t.MovingS, t.MaxSpeed, t.ElevGain, string(pts), t.Type)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "clientId gehört einem anderen Benutzer", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listTours(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,client_id,name,start_ts,end_ts,distance_m,moving_s,max_speed,elev_gain,type
		FROM tours WHERE user_id=? ORDER BY start_ts DESC`, currentUser(r).ID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer rows.Close()
	out := []tour{}
	for rows.Next() {
		var t tour
		if err := rows.Scan(&t.ID, &t.ClientID, &t.Name, &t.StartTS, &t.EndTS, &t.DistanceM, &t.MovingS, &t.MaxSpeed, &t.ElevGain, &t.Type); err != nil {
			httpErr(w, err, 500)
			return
		}
		out = append(out, t)
	}
	writeJSON(w, out)
}

func (s *server) loadTour(r *http.Request) (*tour, error) {
	var t tour
	var pts string
	err := s.db.QueryRow(`SELECT id,client_id,name,start_ts,end_ts,distance_m,moving_s,max_speed,elev_gain,type,points
		FROM tours WHERE id=? AND user_id=?`, r.PathValue("id"), currentUser(r).ID).
		Scan(&t.ID, &t.ClientID, &t.Name, &t.StartTS, &t.EndTS, &t.DistanceM, &t.MovingS, &t.MaxSpeed, &t.ElevGain, &t.Type, &pts)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(pts), &t.Points); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *server) getTour(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTour(r)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		httpErr(w, err, 500)
		return
	}
	writeJSON(w, t)
}

func (s *server) deleteTour(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.Exec(`DELETE FROM tours WHERE id=? AND user_id=?`, r.PathValue("id"), currentUser(r).ID)
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

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func (s *server) tourGPX(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTour(r)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		httpErr(w, err, 500)
		return
	}
	w.Header().Set("Content-Type", "application/gpx+xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="radspur-%d.gpx"`, t.ID))
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<gpx version="1.1" creator="Radspur" xmlns="http://www.topografix.com/GPX/1/1">` + "\n")
	gpxType := "cycling"
	if t.Type == "run" {
		gpxType = "running"
	}
	fmt.Fprintf(&b, "<trk><name>%s</name><type>%s</type><trkseg>\n", xmlEscape(t.Name), gpxType)
	for _, p := range t.Points {
		fmt.Fprintf(&b, `<trkpt lat="%.7f" lon="%.7f">`, p.Lat, p.Lon)
		if p.Alt != nil {
			fmt.Fprintf(&b, "<ele>%.1f</ele>", *p.Alt)
		}
		fmt.Fprintf(&b, "<time>%s</time></trkpt>\n", time.UnixMilli(p.TS).UTC().Format(time.RFC3339))
	}
	b.WriteString("</trkseg></trk></gpx>\n")
	w.Write([]byte(b.String()))
}
