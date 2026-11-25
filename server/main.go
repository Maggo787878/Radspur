// Radspur-Server: empfängt Positionen und Touren mehrerer Benutzer, bietet eine
// Weboberfläche zum Orten/Klingeln, für Touren und für die Benutzerverwaltung.
package main

import (
	"context"
	"crypto/sha256"
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
	"regexp"
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
	AdminUser     string // nur beim ersten Start: Name des Admins
	AdminPassword string // nur beim ersten Start: Passwort des Admins
	DeviceToken   string // optional: altes gemeinsames Geräte-Token (gehört dem Admin)
	PublicURL     string // optional: Basis für Einladungslinks, z. B. https://radspur.example.at
	BrouterURL    string // Routenberechnung (BRouter-Container)
	GeocoderURL   string // Ortssuche (Nominatim-kompatibel)
	RetentionDays int
	// SecureCookie: "auto" (Secure-Flag nur bei HTTPS), "true" oder "false".
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
		AdminUser:     env("ADMIN_USER", "admin"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		DeviceToken:   strings.TrimSpace(os.Getenv("DEVICE_TOKEN")),
		PublicURL:     strings.TrimRight(env("PUBLIC_URL", ""), "/"),
		BrouterURL:    strings.TrimRight(env("BROUTER_URL", "http://brouter:17777"), "/"),
		GeocoderURL:   strings.TrimRight(env("GEOCODER_URL", "https://nominatim.openstreetmap.org"), "/"),
		SecureCookie:  strings.ToLower(env("SECURE_COOKIE", "auto")),
	}
	c.RetentionDays, _ = strconv.Atoi(env("LOCATION_RETENTION_DAYS", "30"))
	if c.DeviceToken != "" && len(c.DeviceToken) < 16 {
		log.Fatal("DEVICE_TOKEN muss mindestens 16 Zeichen haben (oder leer bleiben)")
	}
	return c
}

func newServer(cfg config) (*server, error) {
	db, err := openDB(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if err := bootstrap(db, cfg); err != nil {
		return nil, err
	}
	return &server{cfg: cfg, db: db, login: newLoginLimiter(5, 15*time.Minute)}, nil
}

func main() {
	cfg := loadConfig()
	if len(os.Args) > 1 {
		os.Exit(cli(cfg, os.Args[1:]))
	}
	s, err := newServer(cfg)
	if err != nil {
		log.Fatal(err)
	}

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
	_ = s.db.Close()
}

// cli: Verwaltungsbefehle, z. B. docker exec radspur radspur-server reset-link admin
func cli(cfg config, args []string) int {
	switch args[0] {
	case "reset-link":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Aufruf: radspur-server reset-link <benutzername>")
			return 2
		}
		s, err := newServer(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		var id int64
		if err := s.db.QueryRow(`SELECT id FROM users WHERE username=?`, args[1]).Scan(&id); err != nil {
			fmt.Fprintf(os.Stderr, "Benutzer %q nicht gefunden\n", args[1])
			return 1
		}
		_, _ = s.db.Exec(`UPDATE users SET disabled=0 WHERE id=?`, id)
		tok, exp, err := s.createInvite("reset", &id, "Passwort für "+args[1]+" (Kommandozeile)", nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		base := cfg.PublicURL
		if base == "" {
			base = "https://<deine-adresse>"
		}
		fmt.Printf("Link zum Setzen eines neuen Passworts für %q (gültig bis %s):\n%s/einladung/%s\n",
			args[1], exp.Format("02.01.2006 15:04"), base, tok)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "Unbekannter Befehl. Verfügbar: reset-link <benutzername>")
		return 2
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	page := htmlPages(static)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// App (Geräte-Token)
	mux.HandleFunc("POST /api/device/login", s.deviceLogin)
	mux.Handle("POST /api/device/locations", s.device(s.postLocations))
	mux.Handle("POST /api/device/commands/{id}/ack", s.device(s.ackCommand))
	mux.Handle("POST /api/device/tours", s.device(s.postTour))

	// Anmeldung, Einladung
	mux.HandleFunc("GET /login", page("login.html"))
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /einladung/{token}", page("invite.html"))
	mux.HandleFunc("GET /api/invite/{token}", s.publicInviteInfo)
	mux.HandleFunc("POST /api/invite/{token}", s.acceptInvite)

	// Eigene Daten (Sitzung)
	mux.Handle("GET /api/web/me", s.web(s.me))
	mux.Handle("POST /api/web/me/password", s.web(s.changePassword))
	mux.Handle("GET /api/web/me/devices", s.web(s.listDevices))
	mux.Handle("DELETE /api/web/me/devices/{id}", s.web(s.deleteDevice))
	mux.Handle("GET /api/web/status", s.web(s.status))
	mux.Handle("GET /api/web/locations", s.web(s.listLocations))
	mux.Handle("POST /api/web/commands", s.web(s.createCommand))
	mux.Handle("GET /api/web/tours", s.web(s.listTours))
	mux.Handle("GET /api/web/tours/{id}", s.web(s.getTour))
	mux.Handle("DELETE /api/web/tours/{id}", s.web(s.deleteTour))
	mux.Handle("GET /tours/{id}/gpx", s.web(s.tourGPX))
	mux.Handle("GET /api/web/stats", s.web(s.getStats))
	mux.Handle("GET /api/web/geocode", s.web(s.geocode))
	mux.Handle("GET /api/web/route-profiles", s.web(s.listProfiles))
	mux.Handle("POST /api/web/route-calc", s.web(s.routeCalc))
	mux.Handle("GET /api/web/routes", s.web(s.listRoutes))
	mux.Handle("POST /api/web/routes", s.web(s.saveRoute))
	mux.Handle("GET /api/web/routes/{id}", s.web(s.getRoute))
	mux.Handle("PUT /api/web/routes/{id}", s.web(s.saveRoute))
	mux.Handle("DELETE /api/web/routes/{id}", s.web(s.deleteRoute))
	mux.Handle("GET /routes/{id}/gpx", s.web(s.routeGPX))
	mux.Handle("GET /api/web/stats.xlsx", s.web(s.statsXlsx))

	// Benutzerverwaltung (nur Admin)
	mux.Handle("GET /api/web/admin/users", s.admin(s.adminListUsers))
	mux.Handle("POST /api/web/admin/users/{id}/disable", s.admin(s.adminSetDisabled(true)))
	mux.Handle("POST /api/web/admin/users/{id}/enable", s.admin(s.adminSetDisabled(false)))
	mux.Handle("POST /api/web/admin/users/{id}/reset", s.admin(s.adminResetLink))
	mux.Handle("DELETE /api/web/admin/users/{id}", s.admin(s.adminDeleteUser))
	mux.Handle("GET /api/web/admin/invites", s.admin(s.adminListInvites))
	mux.Handle("POST /api/web/admin/invites", s.admin(s.adminCreateInvite))
	mux.Handle("DELETE /api/web/admin/invites/{id}", s.admin(s.adminDeleteInvite))

	mux.Handle("GET /static/", noDirListing(http.StripPrefix("/static/", http.FileServerFS(static))))
	mux.Handle("GET /{$}", s.webPage(page("index.html")))
	return securityHeaders(mux)
}

// assetRef findet Verweise auf eigene Skripte/Stylesheets in den HTML-Seiten.
var assetRef = regexp.MustCompile(`(/static/[^"?]+\.(?:js|css))"`)

// htmlPages liefert die HTML-Seiten mit Versionskennung an allen eigenen
// Skripten/Stylesheets (?v=<Hash über alle Web-Dateien>). So lädt der Browser
// nach einem Update die neuen Dateien, auch wenn ein Proxy (z. B. „Cache
// Assets“ im Nginx Proxy Manager) sie stundenlang zwischenspeichern lässt.
func htmlPages(static fs.FS) func(name string) http.HandlerFunc {
	h := sha256.New()
	_ = fs.WalkDir(static, ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(static, path)
			h.Write([]byte(path))
			h.Write(b)
		}
		return nil
	})
	version := hex.EncodeToString(h.Sum(nil))[:12]
	return func(name string) http.HandlerFunc {
		raw, err := fs.ReadFile(static, name)
		if err != nil {
			panic(err)
		}
		body := assetRef.ReplaceAll(raw, []byte(`$1?v=`+version+`"`))
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(body)
		}
	}
}

// Content-Security-Policy: nur eigene Skripte/Styles; Kartenkacheln von OSM.
const csp = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data: https://tile.openstreetmap.org; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"

// noDirListing beantwortet Verzeichnis-Anfragen (z. B. /static/) mit 404.
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
		// OSM-Kachelserver verlangen einen Referer; nur die Herkunft senden.
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Einladungslinks nicht an fremde Server (Kacheln) weitergeben.
		if strings.HasPrefix(r.URL.Path, "/einladung/") {
			w.Header().Set("Referrer-Policy", "no-referrer")
		}
		h.ServeHTTP(w, r)
	})
}

// isHTTPS: spricht der Browser HTTPS mit uns (direkt oder über den Reverse Proxy)?
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

// clientIP bevorzugt die Adresse, die nginx in X-Real-IP mitgibt.
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

// ---------- Hilfsfunktionen ----------

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

// ---------- Aufräumen ----------

func (s *server) cleanupLoop(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		s.cleanup()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *server) cleanup() {
	now := nowMs()
	if s.cfg.RetentionDays > 0 {
		if _, err := s.db.Exec(`DELETE FROM locations WHERE ts < ?`, now-int64(s.cfg.RetentionDays)*24*3600_000); err != nil {
			log.Print("cleanup: ", err)
		}
	}
	_, _ = s.db.Exec(`DELETE FROM commands WHERE created < ?`, now-7*24*3600_000)
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE expires < ?`, now)
	_, _ = s.db.Exec(`DELETE FROM invites WHERE expires < ?`, now-30*24*3600_000)
}
