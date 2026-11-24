// Radspur server: receives phone locations and bike tours, offers a web UI
// to locate the phone, ring it and browse recorded tours.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed web
var webFS embed.FS

type config struct {
	Listen        string
	DBPath        string
	AdminPassword string
	DeviceToken   string
	SessionSecret []byte
	RetentionDays int
	// SecureCookie: "auto" (Secure flag only for HTTPS requests), "true" or "false".
	SecureCookie string
}

type server struct {
	cfg   config
	db    *sql.DB
	login *loginLimiter
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func loadConfig() config {
	c := config{
		Listen:        env("LISTEN", ":8080"),
		DBPath:        env("DB_PATH", "/data/radspur.db"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		DeviceToken:   os.Getenv("DEVICE_TOKEN"),
		SecureCookie:  strings.ToLower(env("SECURE_COOKIE", "auto")),
	}
	c.RetentionDays, _ = strconv.Atoi(env("LOCATION_RETENTION_DAYS", "30"))
	if len(c.AdminPassword) < 8 {
		log.Fatal("ADMIN_PASSWORD muss gesetzt sein (mind. 8 Zeichen)")
	}
	if len(c.DeviceToken) < 16 {
		log.Fatal("DEVICE_TOKEN muss gesetzt sein (mind. 16 Zeichen)")
	}
	if s := os.Getenv("SESSION_SECRET"); s != "" {
		c.SessionSecret = []byte(s)
	} else {
		// Derived from the admin password: sessions survive restarts and are
		// invalidated when the password changes.
		h := sha256.Sum256([]byte("radspur-session|" + c.AdminPassword))
		c.SessionSecret = h[:]
	}
	return c
}

const schema = `
CREATE TABLE IF NOT EXISTS locations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  lat REAL NOT NULL, lon REAL NOT NULL,
  acc REAL, alt REAL, speed REAL,
  battery INTEGER, charging INTEGER,
  source TEXT
);
CREATE INDEX IF NOT EXISTS idx_locations_ts ON locations(ts);
CREATE TABLE IF NOT EXISTS commands (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  created INTEGER NOT NULL,
  delivered INTEGER,
  acked INTEGER
);
CREATE TABLE IF NOT EXISTS tours (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  client_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  start_ts INTEGER NOT NULL, end_ts INTEGER NOT NULL,
  distance_m REAL NOT NULL, moving_s INTEGER NOT NULL,
  max_speed REAL NOT NULL, elev_gain REAL NOT NULL,
  points TEXT NOT NULL
);
`

func main() {
	cfg := loadConfig()
	db, err := sql.Open("sqlite", cfg.DBPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("schema: ", err)
	}
	if err := migrate(db); err != nil {
		log.Fatal("migration: ", err)
	}
	s := &server{cfg: cfg, db: db, login: newLoginLimiter(5, 15*time.Minute)}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go s.cleanupLoop(ctx)

	srv := &http.Server{Addr: cfg.Listen, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("Radspur-Server lauscht auf %s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	_ = db.Close()
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Device API (Bearer token)
	mux.Handle("POST /api/device/locations", s.device(s.postLocations))
	mux.Handle("POST /api/device/commands/{id}/ack", s.device(s.ackCommand))
	mux.Handle("POST /api/device/tours", s.device(s.postTour))

	// Web login
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "login.html")
	})
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.logout)

	// Web API (session cookie)
	mux.Handle("GET /api/web/status", s.web(s.status))
	mux.Handle("GET /api/web/locations", s.web(s.listLocations))
	mux.Handle("POST /api/web/commands", s.web(s.createCommand))
	mux.Handle("GET /api/web/tours", s.web(s.listTours))
	mux.Handle("GET /api/web/tours/{id}", s.web(s.getTour))
	mux.Handle("DELETE /api/web/tours/{id}", s.web(s.deleteTour))
	mux.Handle("GET /tours/{id}/gpx", s.web(s.tourGPX))

	mux.Handle("GET /static/", noDirListing(http.StripPrefix("/static/", http.FileServerFS(static))))
	mux.Handle("GET /{$}", s.webPage(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "index.html")
	}))
	return securityHeaders(mux)
}

// Content-Security-Policy: only own scripts/styles; map tiles from OSM.
const csp = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data: https://tile.openstreetmap.org; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"

// noDirListing answers 404 for directory requests (e.g. /static/), which
// http.FileServer would otherwise answer with a listing or index.html.
func noDirListing(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		if isHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		// OSM tile servers require a Referer (tile usage policy); send the origin only.
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.ServeHTTP(w, r)
	})
}

// ---------- auth ----------

func (s *server) device(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.DeviceToken)) != 1 {
			time.Sleep(500 * time.Millisecond)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		h(w, r)
	})
}

const cookieName = "radspur_session"

func (s *server) sign(payload string) string {
	m := hmac.New(sha256.New, s.cfg.SessionSecret)
	m.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(m.Sum(nil))
}

func (s *server) validSession(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	i := strings.LastIndex(c.Value, ".")
	if i < 0 {
		return false
	}
	payload := c.Value[:i]
	if !hmac.Equal([]byte(s.sign(payload)), []byte(c.Value)) {
		return false
	}
	parts := strings.Split(payload, "|")
	if len(parts) != 3 {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && time.Now().Unix() < exp
}

func (s *server) web(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.validSession(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// CSRF: state-changing requests must come from our own page.
		if r.Method != http.MethodGet && r.Header.Get("X-Radspur") != "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		h(w, r)
	})
}

func (s *server) webPage(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.validSession(r) {
			target := "/login"
			if r.URL.Query().Get("login") == "1" {
				log.Printf("Browser von %s hat das Session-Cookie nicht gespeichert (https=%v, SECURE_COOKIE=%s)",
					clientIP(r), isHTTPS(r), s.cfg.SecureCookie)
				target = "/login?err=cookie"
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		h(w, r)
	})
}

// isHTTPS reports whether the browser talks HTTPS to us, directly or via a
// reverse proxy that sets X-Forwarded-Proto.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

// clientIP prefers the address nginx passes in X-Real-IP (logging only).
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

func (s *server) secureCookie(r *http.Request) bool {
	switch s.cfg.SecureCookie {
	case "true":
		return true
	case "false":
		return false
	default:
		return isHTTPS(r)
	}
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if wait := s.login.retryAfter(ip); wait > 0 {
		log.Printf("Login gesperrt für %s (noch %s)", ip, wait.Round(time.Second))
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		http.Redirect(w, r, "/login?err=locked", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	pw := r.FormValue("password")

	// Serialize attempts; a failure holds the lock for a second (see loginLimiter).
	s.login.serial.Lock()
	ok := subtle.ConstantTimeCompare([]byte(pw), []byte(s.cfg.AdminPassword)) == 1
	if !ok {
		time.Sleep(time.Second)
	}
	s.login.serial.Unlock()

	if !ok {
		s.login.fail(ip)
		log.Printf("Login fehlgeschlagen von %s", ip)
		http.Redirect(w, r, "/login?err=1", http.StatusSeeOther)
		return
	}
	s.login.success(ip)
	secure := s.secureCookie(r)
	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	exp := time.Now().Add(30 * 24 * time.Hour)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    s.sign(fmt.Sprintf("admin|%d|%x", exp.Unix(), nonce)),
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	log.Printf("Login erfolgreich von %s (https=%v, secure-cookie=%v)", clientIP(r), isHTTPS(r), secure)
	// The marker lets the start page detect a browser that dropped the cookie.
	http.Redirect(w, r, "/?login=1", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error, code int) {
	if code >= 500 {
		log.Print(err)
		http.Error(w, "internal error", code)
		return
	}
	http.Error(w, err.Error(), code)
}

func nowMs() int64 { return time.Now().UnixMilli() }

// ---------- locations & commands ----------

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

// postLocations stores reported positions and answers with pending commands,
// so the phone needs only one request per check-in.
func (s *server) postLocations(w http.ResponseWriter, r *http.Request) {
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
		if _, err := tx.Exec(`INSERT INTO locations(ts,lat,lon,acc,alt,speed,battery,charging,source) VALUES(?,?,?,?,?,?,?,?,?)`,
			l.TS, l.Lat, l.Lon, l.Acc, l.Alt, l.Speed, l.Battery, l.Charging, l.Source); err != nil {
			httpErr(w, err, 500)
			return
		}
	}
	cmds, err := pendingCommands(tx)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	if _, err := tx.Exec(`UPDATE commands SET delivered=? WHERE acked IS NULL AND delivered IS NULL`, nowMs()); err != nil {
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
}) ([]command, error) {
	// Commands older than one hour are stale (nobody is waiting for the ring anymore).
	rows, err := q.Query(`SELECT id,type,created,delivered,acked FROM commands WHERE acked IS NULL AND created > ? ORDER BY id`, nowMs()-3600_000)
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
	_, err := s.db.Exec(`UPDATE commands SET acked=? WHERE id=? AND acked IS NULL`, nowMs(), id)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var allowedCommands = map[string]bool{"ring": true, "stop_ring": true, "locate": true}

func (s *server) createCommand(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !allowedCommands[in.Type] {
		httpErr(w, errors.New("ungültiger Befehl"), http.StatusBadRequest)
		return
	}
	if in.Type == "stop_ring" {
		// A pending ring that was never delivered can simply be cancelled.
		_, _ = s.db.Exec(`UPDATE commands SET acked=? WHERE type='ring' AND delivered IS NULL AND acked IS NULL`, nowMs())
	}
	res, err := s.db.Exec(`INSERT INTO commands(type,created) VALUES(?,?)`, in.Type, nowMs())
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, map[string]any{"id": id})
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"last": nil}
	var l location
	var acc, alt, speed sql.NullFloat64
	var bat sql.NullInt64
	var chg sql.NullBool
	var src sql.NullString
	err := s.db.QueryRow(`SELECT ts,lat,lon,acc,alt,speed,battery,charging,source FROM locations ORDER BY ts DESC LIMIT 1`).
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
	cmds, err := pendingCommands(s.db)
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
	rows, err := s.db.Query(`SELECT ts,lat,lon,acc FROM locations WHERE ts >= ? ORDER BY ts LIMIT 5000`, since)
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

// ---------- tours ----------

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

// migrate ergänzt Spalten, die ältere Datenbanken noch nicht haben.
func migrate(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('tours') WHERE name='type'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE tours ADD COLUMN type TEXT NOT NULL DEFAULT 'bike'`); err != nil {
			return err
		}
		log.Print("Datenbank migriert: Spalte tours.type ergänzt")
	}
	return nil
}

func (s *server) postTour(w http.ResponseWriter, r *http.Request) {
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
	_, err := s.db.Exec(`INSERT INTO tours(client_id,name,start_ts,end_ts,distance_m,moving_s,max_speed,elev_gain,points,type)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(client_id) DO UPDATE SET name=excluded.name,start_ts=excluded.start_ts,end_ts=excluded.end_ts,
		distance_m=excluded.distance_m,moving_s=excluded.moving_s,max_speed=excluded.max_speed,elev_gain=excluded.elev_gain,points=excluded.points,type=excluded.type`,
		t.ClientID, t.Name, t.StartTS, t.EndTS, t.DistanceM, t.MovingS, t.MaxSpeed, t.ElevGain, string(pts), t.Type)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listTours(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,client_id,name,start_ts,end_ts,distance_m,moving_s,max_speed,elev_gain,type FROM tours ORDER BY start_ts DESC`)
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

func (s *server) loadTour(id string) (*tour, error) {
	var t tour
	var pts string
	err := s.db.QueryRow(`SELECT id,client_id,name,start_ts,end_ts,distance_m,moving_s,max_speed,elev_gain,type,points FROM tours WHERE id=?`, id).
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
	t, err := s.loadTour(r.PathValue("id"))
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
	if _, err := s.db.Exec(`DELETE FROM tours WHERE id=?`, r.PathValue("id")); err != nil {
		httpErr(w, err, 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func (s *server) tourGPX(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTour(r.PathValue("id"))
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

// ---------- housekeeping ----------

func (s *server) cleanupLoop(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if s.cfg.RetentionDays > 0 {
			cut := nowMs() - int64(s.cfg.RetentionDays)*24*3600_000
			if _, err := s.db.Exec(`DELETE FROM locations WHERE ts < ?`, cut); err != nil {
				log.Print("cleanup: ", err)
			}
		}
		_, _ = s.db.Exec(`DELETE FROM commands WHERE created < ?`, nowMs()-7*24*3600_000)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
