package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ---------- Grundbausteine ----------

type user struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (u *user) isAdmin() bool { return u.Role == "admin" }

type ctxKey struct{}

func withUser(r *http.Request, u *user) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, u))
}

// currentUser liefert den angemeldeten Benutzer (von web/device-Middleware gesetzt).
func currentUser(r *http.Request) *user { return r.Context().Value(ctxKey{}).(*user) }

var bcryptCost = 12 // Tests setzen bcrypt.MinCost

const minPasswordLen = 10

func hashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	return string(h), err
}

// dummyHash: Vergleich gegen einen echten Hash auch bei unbekanntem Benutzer,
// damit die Antwortzeit nicht verrät, ob es den Namen gibt.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("radspur-dummy-password"), bcrypt.MinCost)

func newToken(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}

func tokenHash(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func validPassword(pw string) error {
	if len([]rune(pw)) < minPasswordLen {
		return fmt.Errorf("Passwort muss mindestens %d Zeichen haben", minPasswordLen)
	}
	if len(pw) > 72 { // bcrypt-Grenze
		return errors.New("Passwort ist zu lang (max. 72 Byte)")
	}
	return nil
}

// checkLogin prüft Benutzer + Passwort; gesperrte Benutzer schlagen fehl.
func (s *server) checkLogin(username, pw string) (*user, bool) {
	var u user
	var hash string
	var disabled bool
	err := s.db.QueryRow(`SELECT id,username,role,pw_hash,disabled FROM users WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.Role, &hash, &disabled)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(pw))
		return nil, false
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) != nil || disabled {
		return nil, false
	}
	return &u, true
}

// attempt führt einen Login-Versuch mit Sperre pro IP und pro Benutzername aus.
// Rückgabe: Benutzer, Wartezeit (>0 = gesperrt).
func (s *server) attempt(r *http.Request, username, pw string) (*user, time.Duration) {
	ip := "ip:" + clientIP(r)
	acct := "user:" + strings.ToLower(username)
	if wait := max(s.login.retryAfter(ip), s.login.retryAfter(acct)); wait > 0 {
		log.Printf("Login gesperrt für %s / %q (noch %s)", clientIP(r), username, wait.Round(time.Second))
		return nil, wait
	}
	// Versuche serialisieren; ein Fehlversuch hält die Sperre eine Sekunde.
	s.login.serial.Lock()
	u, ok := s.checkLogin(username, pw)
	if !ok {
		time.Sleep(time.Second)
	}
	s.login.serial.Unlock()
	if !ok {
		s.login.fail(ip)
		s.login.fail(acct)
		log.Printf("Login fehlgeschlagen von %s für %q", clientIP(r), username)
		return nil, 0
	}
	s.login.success(ip)
	s.login.success(acct)
	return u, 0
}

// ---------- Sitzungen (Browser) ----------

const cookieName = "radspur_session"
const sessionTTL = 30 * 24 * time.Hour

func (s *server) createSession(w http.ResponseWriter, r *http.Request, u *user) error {
	tok := newToken("")
	exp := time.Now().Add(sessionTTL)
	if _, err := s.db.Exec(`INSERT INTO sessions(token_hash,user_id,created,expires,last_seen) VALUES(?,?,?,?,?)`,
		tokenHash(tok), u.ID, nowMs(), exp.UnixMilli(), nowMs()); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", Expires: exp,
		HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// sessionUser liefert den Benutzer zur Sitzung (nil = keine gültige Sitzung).
func (s *server) sessionUser(r *http.Request) *user {
	c, err := r.Cookie(cookieName)
	if err != nil || len(c.Value) != 64 {
		return nil
	}
	var u user
	var lastSeen int64
	h := tokenHash(c.Value)
	err = s.db.QueryRow(`SELECT u.id,u.username,u.role,s.last_seen FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=? AND s.expires>? AND u.disabled=0`, h, nowMs()).Scan(&u.ID, &u.Username, &u.Role, &lastSeen)
	if err != nil {
		return nil
	}
	if nowMs()-lastSeen > 5*60_000 {
		_, _ = s.db.Exec(`UPDATE sessions SET last_seen=? WHERE token_hash=?`, nowMs(), h)
	}
	return &u
}

func (s *server) web(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := s.sessionUser(r)
		if u == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// CSRF: ändernde Anfragen müssen von der eigenen Seite kommen.
		if r.Method != http.MethodGet && r.Header.Get("X-Radspur") != "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		h(w, withUser(r, u))
	})
}

func (s *server) admin(h http.HandlerFunc) http.Handler {
	return s.web(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).isAdmin() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	})
}

func (s *server) webPage(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.sessionUser(r) == nil {
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

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	username := strings.TrimSpace(r.FormValue("username"))
	u, wait := s.attempt(r, username, r.FormValue("password"))
	if wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		http.Redirect(w, r, "/login?err=locked", http.StatusSeeOther)
		return
	}
	if u == nil {
		http.Redirect(w, r, "/login?err=1", http.StatusSeeOther)
		return
	}
	if err := s.createSession(w, r, u); err != nil {
		httpErr(w, err, 500)
		return
	}
	log.Printf("Login %q von %s (https=%v, secure-cookie=%v)", u.Username, clientIP(r), isHTTPS(r), s.secureCookie(r))
	// Der Marker erkennt Browser, die das Cookie verwerfen.
	http.Redirect(w, r, "/?login=1", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.secureCookie(r), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---------- Geräte (App) ----------

func (s *server) device(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		var u *user
		if ok && tok != "" {
			u = s.deviceUser(tok)
		}
		if u == nil {
			time.Sleep(500 * time.Millisecond)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		h(w, withUser(r, u))
	})
}

func (s *server) deviceUser(tok string) *user {
	// Altes gemeinsames DEVICE_TOKEN aus der Einzelbenutzer-Version → erster Admin.
	if s.cfg.DeviceToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.DeviceToken)) == 1 {
		var u user
		if s.db.QueryRow(`SELECT id,username,role FROM users WHERE role='admin' AND disabled=0 ORDER BY id LIMIT 1`).
			Scan(&u.ID, &u.Username, &u.Role) == nil {
			return &u
		}
		return nil
	}
	var u user
	var id int64
	var lastSeen sql.NullInt64
	h := tokenHash(tok)
	if s.db.QueryRow(`SELECT d.id,d.last_seen,u.id,u.username,u.role FROM devices d JOIN users u ON u.id=d.user_id
		WHERE d.token_hash=? AND u.disabled=0`, h).Scan(&id, &lastSeen, &u.ID, &u.Username, &u.Role) != nil {
		return nil
	}
	if !lastSeen.Valid || nowMs()-lastSeen.Int64 > 5*60_000 {
		_, _ = s.db.Exec(`UPDATE devices SET last_seen=? WHERE id=?`, nowMs(), id)
	}
	return &u
}

// deviceLogin: App meldet sich mit Benutzer + Passwort an und erhält ein eigenes Geräte-Token.
func (s *server) deviceLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in struct{ Username, Password, Device string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	u, wait := s.attempt(r, strings.TrimSpace(in.Username), in.Password)
	if wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		http.Error(w, "Zu viele Fehlversuche, bitte später erneut versuchen", http.StatusTooManyRequests)
		return
	}
	if u == nil {
		http.Error(w, "Benutzername oder Passwort falsch", http.StatusUnauthorized)
		return
	}
	name := strings.TrimSpace(in.Device)
	if name == "" || len(name) > 64 {
		name = "Android"
	}
	tok := newToken("rsd_")
	if _, err := s.db.Exec(`INSERT INTO devices(user_id,token_hash,name,created,last_seen) VALUES(?,?,?,?,?)`,
		u.ID, tokenHash(tok), name, nowMs(), nowMs()); err != nil {
		httpErr(w, err, 500)
		return
	}
	log.Printf("Gerät %q für %q angemeldet", name, u.Username)
	writeJSON(w, map[string]string{"token": tok, "username": u.Username})
}

// ---------- Konto (eigener Benutzer) ----------

func (s *server) me(w http.ResponseWriter, r *http.Request) { writeJSON(w, currentUser(r)) }

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct{ Current, New string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	if _, ok := s.checkLogin(u.Username, in.Current); !ok {
		time.Sleep(time.Second)
		http.Error(w, "Aktuelles Passwort ist falsch", http.StatusForbidden)
		return
	}
	if err := validPassword(in.New); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	hash, err := hashPassword(in.New)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	cur, _ := r.Cookie(cookieName)
	tx, err := s.db.Begin()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer tx.Rollback()
	_, _ = tx.Exec(`UPDATE users SET pw_hash=? WHERE id=?`, hash, u.ID)
	// Alle anderen Browser-Sitzungen abmelden.
	_, _ = tx.Exec(`DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, u.ID, tokenHash(cur.Value))
	if err := tx.Commit(); err != nil {
		httpErr(w, err, 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type deviceInfo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Created  int64  `json:"created"`
	LastSeen *int64 `json:"lastSeen,omitempty"`
}

func (s *server) listDevices(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,name,created,last_seen FROM devices WHERE user_id=? ORDER BY id`, currentUser(r).ID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer rows.Close()
	out := []deviceInfo{}
	for rows.Next() {
		var d deviceInfo
		var ls sql.NullInt64
		if err := rows.Scan(&d.ID, &d.Name, &d.Created, &ls); err != nil {
			httpErr(w, err, 500)
			return
		}
		if ls.Valid {
			d.LastSeen = &ls.Int64
		}
		out = append(out, d)
	}
	writeJSON(w, out)
}

func (s *server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.Exec(`DELETE FROM devices WHERE id=? AND user_id=?`, r.PathValue("id"), currentUser(r).ID)
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

// ---------- Einladungen ----------

const inviteTTL = 7 * 24 * time.Hour

// baseURL für Links: PUBLIC_URL, sonst aus der Anfrage (Host + X-Forwarded-Proto).
func (s *server) baseURL(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// createInvite legt eine Einladung (purpose 'register') oder einen
// Passwort-Link (purpose 'reset') an und liefert das Klartext-Token.
func (s *server) createInvite(purpose string, target *int64, note string, createdBy *int64) (string, time.Time, error) {
	tok := newToken("")
	exp := time.Now().Add(inviteTTL)
	_, err := s.db.Exec(`INSERT INTO invites(token_hash,purpose,target_user,note,created_by,created,expires) VALUES(?,?,?,?,?,?,?)`,
		tokenHash(tok), purpose, target, note, createdBy, nowMs(), exp.UnixMilli())
	return tok, exp, err
}

func (s *server) adminCreateInvite(w http.ResponseWriter, r *http.Request) {
	var in struct{ Note string }
	_ = json.NewDecoder(r.Body).Decode(&in)
	note := strings.TrimSpace(in.Note)
	if len(note) > 100 {
		note = note[:100]
	}
	me := currentUser(r).ID
	tok, exp, err := s.createInvite("register", nil, note, &me)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	log.Printf("Einladung erstellt von %q (%s)", currentUser(r).Username, note)
	writeJSON(w, map[string]any{"url": s.baseURL(r) + "/einladung/" + tok, "expires": exp.UnixMilli()})
}

type inviteInfo struct {
	ID      int64  `json:"id"`
	Purpose string `json:"purpose"`
	Note    string `json:"note"`
	Target  string `json:"target,omitempty"`
	Created int64  `json:"created"`
	Expires int64  `json:"expires"`
}

func (s *server) adminListInvites(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT i.id,i.purpose,i.note,COALESCE(u.username,''),i.created,i.expires FROM invites i
		LEFT JOIN users u ON u.id=i.target_user WHERE i.used IS NULL AND i.expires>? ORDER BY i.id DESC`, nowMs())
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer rows.Close()
	out := []inviteInfo{}
	for rows.Next() {
		var i inviteInfo
		if err := rows.Scan(&i.ID, &i.Purpose, &i.Note, &i.Target, &i.Created, &i.Expires); err != nil {
			httpErr(w, err, 500)
			return
		}
		out = append(out, i)
	}
	writeJSON(w, out)
}

func (s *server) adminDeleteInvite(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.Exec(`DELETE FROM invites WHERE id=? AND used IS NULL`, r.PathValue("id")); err != nil {
		httpErr(w, err, 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type pendingInvite struct {
	id      int64
	purpose string
	target  sql.NullInt64
	user    string
}

func (s *server) lookupInvite(tok string) (*pendingInvite, error) {
	var p pendingInvite
	var uname sql.NullString
	err := s.db.QueryRow(`SELECT i.id,i.purpose,i.target_user,u.username FROM invites i LEFT JOIN users u ON u.id=i.target_user
		WHERE i.token_hash=? AND i.used IS NULL AND i.expires>?`, tokenHash(tok), nowMs()).Scan(&p.id, &p.purpose, &p.target, &uname)
	p.user = uname.String
	return &p, err
}

// inviteInfo für die Einladungsseite (öffentlich, nur mit gültigem Token).
func (s *server) publicInviteInfo(w http.ResponseWriter, r *http.Request) {
	p, err := s.lookupInvite(r.PathValue("token"))
	if err != nil {
		http.Error(w, "Link ungültig oder abgelaufen", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"purpose": p.purpose, "username": p.user})
}

// acceptInvite: Registrierung bzw. neues Passwort setzen, danach angemeldet.
func (s *server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Radspur") != "1" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ip := "ip:" + clientIP(r)
	if s.login.retryAfter(ip) > 0 {
		http.Error(w, "Zu viele Versuche, bitte später erneut", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in struct{ Username, Password string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	p, err := s.lookupInvite(r.PathValue("token"))
	if err != nil {
		s.login.fail(ip)
		http.Error(w, "Link ungültig oder abgelaufen", http.StatusNotFound)
		return
	}
	if err := validPassword(in.Password); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer tx.Rollback()
	// Einladung zuerst verbrauchen (verhindert doppelte Nutzung bei parallelen Anfragen).
	res, err := tx.Exec(`UPDATE invites SET used=? WHERE id=? AND used IS NULL`, nowMs(), p.id)
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		http.Error(w, "Link ungültig oder abgelaufen", http.StatusNotFound)
		return
	}
	var u user
	switch p.purpose {
	case "register":
		name := strings.TrimSpace(in.Username)
		if !validUsername(name) {
			http.Error(w, "Benutzername: 3–32 Zeichen, nur Buchstaben, Ziffern, Punkt, Unterstrich, Bindestrich", http.StatusBadRequest)
			return
		}
		res, err := tx.Exec(`INSERT INTO users(username,pw_hash,role,created) VALUES(?,?,'user',?)`, name, hash, nowMs())
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				http.Error(w, "Benutzername ist bereits vergeben", http.StatusConflict)
				return
			}
			httpErr(w, err, 500)
			return
		}
		u.ID, _ = res.LastInsertId()
		u.Username, u.Role = name, "user"
	case "reset":
		if !p.target.Valid {
			httpErr(w, errors.New("reset ohne Benutzer"), 500)
			return
		}
		if err := tx.QueryRow(`SELECT id,username,role FROM users WHERE id=?`, p.target.Int64).Scan(&u.ID, &u.Username, &u.Role); err != nil {
			http.Error(w, "Benutzer existiert nicht mehr", http.StatusNotFound)
			return
		}
		_, _ = tx.Exec(`UPDATE users SET pw_hash=? WHERE id=?`, hash, u.ID)
		_, _ = tx.Exec(`DELETE FROM sessions WHERE user_id=?`, u.ID)
	default:
		httpErr(w, errors.New("unbekannter Einladungstyp"), 500)
		return
	}
	if err := tx.Commit(); err != nil {
		httpErr(w, err, 500)
		return
	}
	log.Printf("Einladung (%s) eingelöst: %q", p.purpose, u.Username)
	if err := s.createSession(w, r, &u); err != nil {
		httpErr(w, err, 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Benutzerverwaltung (Admin) ----------
// Admins sehen Konten, aber bewusst keine Positionen oder Touren anderer.

type userInfo struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	Disabled   bool   `json:"disabled"`
	Created    int64  `json:"created"`
	LastActive *int64 `json:"lastActive,omitempty"`
	Devices    int    `json:"devices"`
}

func (s *server) adminListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT u.id,u.username,u.role,u.disabled,u.created,
		MAX(COALESCE((SELECT MAX(last_seen) FROM sessions WHERE user_id=u.id),0),
		    COALESCE((SELECT MAX(last_seen) FROM devices WHERE user_id=u.id),0)),
		(SELECT COUNT(*) FROM devices WHERE user_id=u.id)
		FROM users u ORDER BY u.username COLLATE NOCASE`)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer rows.Close()
	out := []userInfo{}
	for rows.Next() {
		var u userInfo
		var last int64
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.Created, &last, &u.Devices); err != nil {
			httpErr(w, err, 500)
			return
		}
		if last > 0 {
			u.LastActive = &last
		}
		out = append(out, u)
	}
	writeJSON(w, out)
}

// targetUser liest {id} und verhindert Aktionen am eigenen Konto.
func (s *server) targetUser(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id == currentUser(r).ID {
		http.Error(w, "Diese Aktion ist am eigenen Konto nicht möglich", http.StatusBadRequest)
		return 0, "", false
	}
	var name string
	if err := s.db.QueryRow(`SELECT username FROM users WHERE id=?`, id).Scan(&name); err != nil {
		http.NotFound(w, r)
		return 0, "", false
	}
	return id, name, true
}

func (s *server) adminSetDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, name, ok := s.targetUser(w, r)
		if !ok {
			return
		}
		_, _ = s.db.Exec(`UPDATE users SET disabled=? WHERE id=?`, disabled, id)
		if disabled {
			// Sofort wirksam: Browser-Sitzungen weg; Geräte werden bei gesperrtem Konto abgewiesen.
			_, _ = s.db.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
		}
		log.Printf("Benutzer %q %s von %q", name, map[bool]string{true: "gesperrt", false: "entsperrt"}[disabled], currentUser(r).Username)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) adminResetLink(w http.ResponseWriter, r *http.Request) {
	id, name, ok := s.targetUser(w, r)
	if !ok {
		return
	}
	me := currentUser(r).ID
	tok, exp, err := s.createInvite("reset", &id, "Passwort für "+name, &me)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	writeJSON(w, map[string]any{"url": s.baseURL(r) + "/einladung/" + tok, "expires": exp.UnixMilli()})
}

func (s *server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, name, ok := s.targetUser(w, r)
	if !ok {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM locations WHERE user_id=?`, `DELETE FROM commands WHERE user_id=?`,
		`DELETE FROM tours WHERE user_id=?`, `DELETE FROM sessions WHERE user_id=?`,
		`DELETE FROM devices WHERE user_id=?`, `DELETE FROM invites WHERE target_user=?`,
		`UPDATE invites SET created_by=NULL WHERE created_by=?`, `DELETE FROM users WHERE id=?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			httpErr(w, err, 500)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpErr(w, err, 500)
		return
	}
	log.Printf("Benutzer %q mit allen Daten gelöscht von %q", name, currentUser(r).Username)
	w.WriteHeader(http.StatusNoContent)
}
