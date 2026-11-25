package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

const adminPW = "admin-pass-123"
const legacyToken = "legacy-device-token-0123456789"

func testConfig(dbPath string) config {
	return config{DBPath: dbPath, AdminUser: "admin", AdminPassword: adminPW, DeviceToken: legacyToken,
		RetentionDays: 30, SecureCookie: "false"}
}

func startServer(t *testing.T, cfg config) (*server, *httptest.Server) {
	t.Helper()
	bcryptCost = bcrypt.MinCost
	s, err := newServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(func() { ts.Close(); s.db.Close() })
	return s, ts
}

func newTestServer(t *testing.T) (*server, *httptest.Server) {
	return startServer(t, testConfig(filepath.Join(t.TempDir(), "t.db")))
}

// ---- Clients ----

type browser struct {
	t    *testing.T
	base string
	c    *http.Client
	ip   string
}

func newBrowser(t *testing.T, ts *httptest.Server) *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{t: t, base: ts.URL, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
}

func (b *browser) login(user, pw string) string {
	req, _ := http.NewRequest("POST", b.base+"/login", strings.NewReader(url.Values{"username": {user}, "password": {pw}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if b.ip != "" {
		req.Header.Set("X-Real-IP", b.ip)
	}
	res, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	res.Body.Close()
	return res.Header.Get("Location")
}

func (b *browser) do(method, path string, body any) (int, string) {
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = strings.NewReader(string(j))
	}
	req, _ := http.NewRequest(method, b.base+path, rd)
	req.Header.Set("X-Radspur", "1")
	req.Header.Set("Content-Type", "application/json")
	if b.ip != "" {
		req.Header.Set("X-Real-IP", b.ip)
	}
	res, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(data)
}

func (b *browser) json(method, path string, body any, out any) int {
	code, data := b.do(method, path, body)
	if out != nil && code == 200 {
		if err := json.Unmarshal([]byte(data), out); err != nil {
			b.t.Fatalf("%s %s: %v (%s)", method, path, err, data)
		}
	}
	return code
}

type phone struct {
	t     *testing.T
	base  string
	token string
}

func (p *phone) do(path string, body any) (int, string) {
	j, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", p.base+path, strings.NewReader(string(j)))
	req.Header.Set("Authorization", "Bearer "+p.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(data)
}

func (p *phone) commands() []command {
	code, data := p.do("/api/device/locations", []location{})
	if code != 200 {
		p.t.Fatalf("locations: %d %s", code, data)
	}
	var out struct{ Commands []command }
	_ = json.Unmarshal([]byte(data), &out)
	return out.Commands
}

func deviceLogin(t *testing.T, ts *httptest.Server, user, pw string) (*phone, int) {
	j, _ := json.Marshal(map[string]string{"username": user, "password": pw, "device": "Testhandy"})
	res, err := http.Post(ts.URL+"/api/device/login", "application/json", strings.NewReader(string(j)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct{ Token string }
	_ = json.NewDecoder(res.Body).Decode(&out)
	return &phone{t: t, base: ts.URL, token: out.Token}, res.StatusCode
}

func tokenFrom(t *testing.T, link string) string {
	i := strings.LastIndex(link, "/einladung/")
	if i < 0 {
		t.Fatalf("kein Einladungslink: %q", link)
	}
	return link[i+len("/einladung/"):]
}

// inviteUser: Admin lädt ein, Gast registriert sich und ist danach angemeldet.
func inviteUser(t *testing.T, ts *httptest.Server, admin *browser, name, pw string) *browser {
	var inv struct{ URL string }
	if code := admin.json("POST", "/api/web/admin/invites", map[string]string{"note": name}, &inv); code != 200 {
		t.Fatalf("Einladung: %d", code)
	}
	b := newBrowser(t, ts)
	if code, body := b.do("POST", "/api/invite/"+tokenFrom(t, inv.URL), map[string]string{"username": name, "password": pw}); code != 204 {
		t.Fatalf("Registrierung %s: %d %s", name, code, body)
	}
	return b
}

func tourBody(clientID, name string) map[string]any {
	return map[string]any{"clientId": clientID, "name": name, "type": "bike", "startTs": 1795000000000, "endTs": 1795003600000,
		"distanceM": 25000, "movingS": 3500, "maxSpeed": 9, "elevGain": 120,
		"points": []map[string]any{{"ts": 1795000000000, "lat": 48.2, "lon": 16.4}, {"ts": 1795000060000, "lat": 48.21, "lon": 16.41}}}
}

func expect(t *testing.T, what string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s: Status %d, erwartet %d", what, got, want)
	}
}

// ---- Tests ----

// Eine Datenbank der Einzelbenutzer-Version wird übernommen und dem Admin zugeordnet.
func TestMigrationFromSingleUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE locations (id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, lat REAL NOT NULL, lon REAL NOT NULL, acc REAL, alt REAL, speed REAL, battery INTEGER, charging INTEGER, source TEXT)`,
		`CREATE TABLE commands (id INTEGER PRIMARY KEY AUTOINCREMENT, type TEXT NOT NULL, created INTEGER NOT NULL, delivered INTEGER, acked INTEGER)`,
		`CREATE TABLE tours (id INTEGER PRIMARY KEY AUTOINCREMENT, client_id TEXT NOT NULL UNIQUE, name TEXT NOT NULL, start_ts INTEGER NOT NULL, end_ts INTEGER NOT NULL, distance_m REAL NOT NULL, moving_s INTEGER NOT NULL, max_speed REAL NOT NULL, elev_gain REAL NOT NULL, points TEXT NOT NULL)`,
		fmt.Sprintf(`INSERT INTO locations(ts,lat,lon) VALUES(%d,48.1,16.1)`, nowMs()),
		`INSERT INTO tours(client_id,name,start_ts,end_ts,distance_m,moving_s,max_speed,elev_gain,points) VALUES('alt','Alte Tour',1,2,1000,60,5,3,'[]')`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()

	_, ts := startServer(t, testConfig(path))
	admin := newBrowser(t, ts)
	if loc := admin.login("admin", adminPW); loc != "/?login=1" {
		t.Fatalf("Admin-Login: %q", loc)
	}
	var tours []tour
	admin.json("GET", "/api/web/tours", nil, &tours)
	if len(tours) != 1 || tours[0].Name != "Alte Tour" || tours[0].Type != "bike" {
		t.Fatalf("alte Tour nicht übernommen: %+v", tours)
	}
	var st struct{ Last *location }
	admin.json("GET", "/api/web/status", nil, &st)
	if st.Last == nil || st.Last.Lat != 48.1 {
		t.Fatalf("alte Position nicht übernommen: %+v", st.Last)
	}
	// Altes gemeinsames Token funktioniert weiter und gehört dem Admin.
	legacy := &phone{t: t, base: ts.URL, token: legacyToken}
	code, _ := legacy.do("/api/device/tours", tourBody("vom-alten-token", "Neu über altes Token"))
	expect(t, "Upload mit altem DEVICE_TOKEN", code, 204)
	admin.json("GET", "/api/web/tours", nil, &tours)
	if len(tours) != 2 {
		t.Fatalf("Tour über altes Token fehlt beim Admin: %d", len(tours))
	}
}

// Kernprüfung: Benutzer sehen und steuern nur ihre eigenen Daten.
func TestIsolationBetweenUsers(t *testing.T) {
	_, ts := newTestServer(t)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	anna := inviteUser(t, ts, admin, "anna", "anna-pass-123")
	bert := inviteUser(t, ts, admin, "bert", "bert-pass-123")

	annaPhone, code := deviceLogin(t, ts, "anna", "anna-pass-123")
	expect(t, "Geräte-Login anna", code, 200)
	bertPhone, _ := deviceLogin(t, ts, "bert", "bert-pass-123")

	// Daten anlegen
	c, _ := annaPhone.do("/api/device/locations", []map[string]any{{"ts": nowMs(), "lat": 47.07, "lon": 15.44}})
	expect(t, "Position anna", c, 200)
	c, _ = annaPhone.do("/api/device/tours", tourBody("anna-1", "Annas Tour"))
	expect(t, "Tour anna", c, 204)
	c, _ = bertPhone.do("/api/device/locations", []map[string]any{{"ts": nowMs(), "lat": 48.30, "lon": 14.29}})
	expect(t, "Position bert", c, 200)

	var annaTours []tour
	anna.json("GET", "/api/web/tours", nil, &annaTours)
	if len(annaTours) != 1 {
		t.Fatalf("anna sieht %d Touren", len(annaTours))
	}
	annaTourID := fmt.Sprint(annaTours[0].ID)

	// bert sieht nichts von anna
	var bertTours []tour
	bert.json("GET", "/api/web/tours", nil, &bertTours)
	if len(bertTours) != 0 {
		t.Errorf("bert sieht fremde Touren: %+v", bertTours)
	}
	var st struct{ Last *location }
	bert.json("GET", "/api/web/status", nil, &st)
	if st.Last == nil || st.Last.Lat != 48.30 {
		t.Errorf("bert sieht falsche Position: %+v", st.Last)
	}
	var locs []location
	bert.json("GET", "/api/web/locations", nil, &locs)
	for _, l := range locs {
		if l.Lat == 47.07 {
			t.Error("bert sieht annas Positionsverlauf")
		}
	}
	c, _ = bert.do("GET", "/api/web/tours/"+annaTourID, nil)
	expect(t, "bert liest annas Tour", c, 404)
	c, _ = bert.do("GET", "/tours/"+annaTourID+"/gpx", nil)
	expect(t, "bert lädt annas GPX", c, 404)
	c, _ = bert.do("DELETE", "/api/web/tours/"+annaTourID, nil)
	expect(t, "bert löscht annas Tour", c, 404)

	// bert versucht, annas Tour über deren clientId zu überschreiben
	c, _ = bertPhone.do("/api/device/tours", tourBody("anna-1", "GEKAPERT"))
	expect(t, "bert überschreibt annas Tour", c, 409)
	anna.json("GET", "/api/web/tours", nil, &annaTours)
	if annaTours[0].Name != "Annas Tour" {
		t.Errorf("annas Tour wurde verändert: %q", annaTours[0].Name)
	}

	// Admin sieht keine fremden Daten
	var adminTours []tour
	admin.json("GET", "/api/web/tours", nil, &adminTours)
	if len(adminTours) != 0 {
		t.Errorf("Admin sieht fremde Touren: %d", len(adminTours))
	}
	admin.json("GET", "/api/web/status", nil, &st)
	if st.Last != nil {
		t.Errorf("Admin sieht fremde Position: %+v", st.Last)
	}

	// Befehle: annas Klingeln erreicht nur annas Handy
	var cmd struct{ ID int64 }
	if anna.json("POST", "/api/web/commands", map[string]string{"type": "ring"}, &cmd) != 200 {
		t.Fatal("anna: Befehl fehlgeschlagen")
	}
	if got := bertPhone.commands(); len(got) != 0 {
		t.Errorf("berts Handy bekommt annas Befehl: %+v", got)
	}
	// bert versucht annas Befehl zu quittieren
	bertPhone.do(fmt.Sprintf("/api/device/commands/%d/ack", cmd.ID), nil)
	if got := annaPhone.commands(); len(got) != 1 || got[0].Type != "ring" {
		t.Errorf("annas Handy: %+v", got)
	}

	// Admin-Funktionen für normale Benutzer gesperrt
	for _, p := range []struct{ m, path string }{
		{"GET", "/api/web/admin/users"}, {"POST", "/api/web/admin/invites"}, {"GET", "/api/web/admin/invites"},
		{"POST", "/api/web/admin/users/1/disable"}, {"POST", "/api/web/admin/users/1/reset"}, {"DELETE", "/api/web/admin/users/1"},
	} {
		c, _ := anna.do(p.m, p.path, map[string]string{})
		expect(t, "anna "+p.m+" "+p.path, c, 403)
	}
	// Ohne Anmeldung und ohne CSRF-Header
	c, _ = newBrowser(t, ts).do("GET", "/api/web/admin/users", nil)
	expect(t, "anonym Benutzerliste", c, 401)
	req, _ := http.NewRequest("POST", ts.URL+"/api/web/commands", strings.NewReader(`{"type":"ring"}`))
	for _, ck := range anna.c.Jar.Cookies(req.URL) {
		req.AddCookie(ck)
	}
	res, _ := http.DefaultClient.Do(req)
	expect(t, "Befehl ohne CSRF-Header", res.StatusCode, 403)
}

func TestInviteRules(t *testing.T) {
	s, ts := newTestServer(t)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	inviteUser(t, ts, admin, "carla", "carla-pass-123")

	newInvite := func() string {
		var inv struct{ URL string }
		admin.json("POST", "/api/web/admin/invites", map[string]string{}, &inv)
		return tokenFrom(t, inv.URL)
	}
	guest := newBrowser(t, ts)
	tok := newInvite()
	c, _ := guest.do("POST", "/api/invite/"+tok, map[string]string{"username": "Carla", "password": "irgendwas-123"})
	expect(t, "Benutzername doppelt (Groß/klein)", c, 409)
	c, _ = guest.do("POST", "/api/invite/"+tok, map[string]string{"username": "dora", "password": "kurz"})
	expect(t, "Passwort zu kurz", c, 400)
	c, _ = guest.do("POST", "/api/invite/"+tok, map[string]string{"username": "<script>", "password": "lang-genug-123"})
	expect(t, "ungültiger Benutzername", c, 400)
	// Fehlversuche verbrauchen die Einladung nicht
	c, _ = guest.do("POST", "/api/invite/"+tok, map[string]string{"username": "dora", "password": "dora-pass-123"})
	expect(t, "gültige Registrierung", c, 204)
	c, _ = newBrowser(t, ts).do("POST", "/api/invite/"+tok, map[string]string{"username": "emil", "password": "emil-pass-123"})
	expect(t, "Einladung zweimal benutzt", c, 404)

	// Abgelaufene Einladung
	tok2 := newInvite()
	_, _ = s.db.Exec(`UPDATE invites SET expires=? WHERE token_hash=?`, nowMs()-1, tokenHash(tok2))
	c, _ = newBrowser(t, ts).do("GET", "/api/invite/"+tok2, nil)
	expect(t, "abgelaufene Einladung", c, 404)

	// Ohne Einladung keine Registrierung
	c, _ = newBrowser(t, ts).do("POST", "/api/invite/"+strings.Repeat("0", 64), map[string]string{"username": "fritz", "password": "fritz-pass-123"})
	expect(t, "erfundenes Token", c, 404)
	if _, code := deviceLogin(t, ts, "fritz", "fritz-pass-123"); code != 401 {
		t.Errorf("fritz konnte sich ohne Einladung anmelden")
	}
}

func TestDisableDeleteAndReset(t *testing.T) {
	_, ts := newTestServer(t)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	anna := inviteUser(t, ts, admin, "anna", "anna-pass-123")
	annaPhone, _ := deviceLogin(t, ts, "anna", "anna-pass-123")
	annaPhone.do("/api/device/tours", tourBody("a1", "Tour"))

	var users []userInfo
	admin.json("GET", "/api/web/admin/users", nil, &users)
	var annaID, adminID int64
	for _, u := range users {
		if u.Username == "anna" {
			annaID = u.ID
		} else {
			adminID = u.ID
		}
	}
	id := fmt.Sprint(annaID)

	c, _ := admin.do("POST", fmt.Sprintf("/api/web/admin/users/%d/disable", adminID), nil)
	expect(t, "Admin sperrt sich selbst", c, 400)

	// Sperren wirkt sofort
	c, _ = admin.do("POST", "/api/web/admin/users/"+id+"/disable", nil)
	expect(t, "sperren", c, 204)
	c, _ = anna.do("GET", "/api/web/status", nil)
	expect(t, "gesperrt: Sitzung", c, 401)
	c, _ = annaPhone.do("/api/device/locations", []location{})
	expect(t, "gesperrt: Handy", c, 401)
	if loc := newBrowser(t, ts).login("anna", "anna-pass-123"); loc != "/login?err=1" {
		t.Errorf("gesperrt: Login möglich (%q)", loc)
	}
	c, _ = admin.do("POST", "/api/web/admin/users/"+id+"/enable", nil)
	expect(t, "entsperren", c, 204)
	c, _ = annaPhone.do("/api/device/locations", []location{})
	expect(t, "entsperrt: Handy", c, 200)

	// Passwort zurücksetzen per Link
	var rl struct{ URL string }
	admin.json("POST", "/api/web/admin/users/"+id+"/reset", nil, &rl)
	var info map[string]string
	newBrowser(t, ts).json("GET", "/api/invite/"+tokenFrom(t, rl.URL), nil, &info)
	if info["purpose"] != "reset" || info["username"] != "anna" {
		t.Errorf("Reset-Info: %+v", info)
	}
	anna2 := newBrowser(t, ts)
	c, _ = anna2.do("POST", "/api/invite/"+tokenFrom(t, rl.URL), map[string]string{"password": "anna-neu-12345"})
	expect(t, "Reset einlösen", c, 204)
	c, _ = anna2.do("GET", "/api/web/me", nil)
	expect(t, "nach Reset angemeldet", c, 200)
	if loc := newBrowser(t, ts).login("anna", "anna-neu-12345"); loc != "/?login=1" {
		t.Errorf("neues Passwort geht nicht: %q", loc)
	}

	// Eigenes Passwort ändern: andere Sitzungen fliegen raus, aktuelle bleibt
	other := newBrowser(t, ts)
	other.login("anna", "anna-neu-12345")
	c, _ = anna2.do("POST", "/api/web/me/password", map[string]string{"current": "falsch-falsch", "new": "anna-x-123456"})
	expect(t, "Passwort ändern mit falschem aktuellen", c, 403)
	c, _ = anna2.do("POST", "/api/web/me/password", map[string]string{"current": "anna-neu-12345", "new": "anna-x-123456"})
	expect(t, "Passwort ändern", c, 204)
	c, _ = anna2.do("GET", "/api/web/me", nil)
	expect(t, "aktuelle Sitzung bleibt", c, 200)
	c, _ = other.do("GET", "/api/web/me", nil)
	expect(t, "andere Sitzung abgemeldet", c, 401)

	// Abmelden beendet die Sitzung serverseitig
	cookies := anna2.c.Jar.Cookies(mustURL(ts.URL))
	anna2.do("POST", "/logout", nil)
	stolen := newBrowser(t, ts)
	stolen.c.Jar.SetCookies(mustURL(ts.URL), cookies)
	c, _ = stolen.do("GET", "/api/web/me", nil)
	expect(t, "Cookie nach Abmelden", c, 401)

	// Gerät entfernen
	other.login("anna", "anna-x-123456")
	var devs []deviceInfo
	other.json("GET", "/api/web/me/devices", nil, &devs)
	if len(devs) != 1 {
		t.Fatalf("Geräte: %+v", devs)
	}
	c, _ = admin.do("DELETE", fmt.Sprintf("/api/web/me/devices/%d", devs[0].ID), nil)
	expect(t, "Admin entfernt fremdes Gerät über /me", c, 404)
	c, _ = other.do("DELETE", fmt.Sprintf("/api/web/me/devices/%d", devs[0].ID), nil)
	expect(t, "eigenes Gerät entfernen", c, 204)
	c, _ = annaPhone.do("/api/device/locations", []location{})
	expect(t, "entferntes Gerät", c, 401)

	// Löschen entfernt Konto und Daten
	c, _ = admin.do("DELETE", "/api/web/admin/users/"+id, nil)
	expect(t, "löschen", c, 204)
	c, _ = other.do("GET", "/api/web/me", nil)
	expect(t, "gelöscht: Sitzung", c, 401)
	if _, code := deviceLogin(t, ts, "anna", "anna-x-123456"); code != 401 {
		t.Errorf("gelöschter Benutzer kann sich anmelden")
	}
}

func TestLoginLockoutPerAccount(t *testing.T) {
	_, ts := newTestServer(t)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	inviteUser(t, ts, admin, "anna", "anna-pass-123")
	// 5 Fehlversuche von wechselnden IPs (über nginx: X-Real-IP)
	for i := 0; i < 5; i++ {
		b := newBrowser(t, ts)
		b.ip = fmt.Sprintf("203.0.113.%d", i+1)
		if loc := b.login("anna", "falsch"); loc != "/login?err=1" {
			t.Fatalf("Fehlversuch %d: %q", i, loc)
		}
	}
	b := newBrowser(t, ts)
	b.ip = "198.51.100.7"
	if loc := b.login("anna", "anna-pass-123"); loc != "/login?err=locked" {
		t.Errorf("Konto nicht gesperrt nach 5 Fehlversuchen: %q", loc)
	}
	if _, code := deviceLogin(t, ts, "anna", "anna-pass-123"); code != 429 {
		t.Errorf("App-Login nicht gesperrt: %d", code)
	}
	// Andere Konten sind nicht betroffen
	if loc := newBrowser(t, ts).login("admin", adminPW); loc != "/?login=1" {
		t.Errorf("Admin von annas Sperre betroffen: %q", loc)
	}
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }

// HTML-Seiten verweisen auf versionierte Skripte/Stylesheets (gegen Proxy-Caches).
func TestAssetVersioning(t *testing.T) {
	_, ts := newTestServer(t)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	for _, path := range []string{"/", "/login", "/einladung/abc"} {
		code, body := admin.do("GET", path, nil)
		if code != 200 {
			t.Fatalf("%s: %d", path, code)
		}
		for _, asset := range []string{"/static/style.css?v=", ".js?v="} {
			if !strings.Contains(body, asset) {
				t.Errorf("%s: %q fehlt", path, asset)
			}
		}
		if strings.Contains(body, `.js"`) || strings.Contains(body, `.css"`) {
			t.Errorf("%s: unversionierter Verweis vorhanden", path)
		}
	}
	// Versionierte Datei wird ausgeliefert
	res, err := http.Get(ts.URL + "/static/app.js?v=123")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("versioniertes Skript: %v %d", err, res.StatusCode)
	}
}
