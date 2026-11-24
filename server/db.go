package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  pw_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'user',
  disabled INTEGER NOT NULL DEFAULT 0,
  created INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  created INTEGER NOT NULL,
  expires INTEGER NOT NULL,
  last_seen INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE TABLE IF NOT EXISTS devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  created INTEGER NOT NULL,
  last_seen INTEGER
);
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);
CREATE TABLE IF NOT EXISTS invites (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  token_hash TEXT NOT NULL UNIQUE,
  purpose TEXT NOT NULL,          -- 'register' | 'reset'
  target_user INTEGER,            -- bei 'reset'
  note TEXT NOT NULL DEFAULT '',
  created_by INTEGER,
  created INTEGER NOT NULL,
  expires INTEGER NOT NULL,
  used INTEGER
);
CREATE TABLE IF NOT EXISTS locations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER,
  ts INTEGER NOT NULL,
  lat REAL NOT NULL, lon REAL NOT NULL,
  acc REAL, alt REAL, speed REAL,
  battery INTEGER, charging INTEGER,
  source TEXT
);
CREATE INDEX IF NOT EXISTS idx_locations_ts ON locations(ts);
CREATE TABLE IF NOT EXISTS commands (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER,
  type TEXT NOT NULL,
  created INTEGER NOT NULL,
  delivered INTEGER,
  acked INTEGER
);
CREATE TABLE IF NOT EXISTS tours (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER,
  client_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  start_ts INTEGER NOT NULL, end_ts INTEGER NOT NULL,
  distance_m REAL NOT NULL, moving_s INTEGER NOT NULL,
  max_speed REAL NOT NULL, elev_gain REAL NOT NULL,
  points TEXT NOT NULL,
  type TEXT NOT NULL DEFAULT 'bike'
);
`

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migration: %w", err)
	}
	return db, nil
}

func hasColumn(db *sql.DB, table, col string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, col).Scan(&n)
	return n > 0, err
}

// migrate ergänzt Spalten, die ältere Datenbanken noch nicht haben.
func migrate(db *sql.DB) error {
	add := []struct{ table, col, ddl string }{
		{"tours", "type", `ALTER TABLE tours ADD COLUMN type TEXT NOT NULL DEFAULT 'bike'`},
		{"tours", "user_id", `ALTER TABLE tours ADD COLUMN user_id INTEGER`},
		{"locations", "user_id", `ALTER TABLE locations ADD COLUMN user_id INTEGER`},
		{"commands", "user_id", `ALTER TABLE commands ADD COLUMN user_id INTEGER`},
	}
	for _, a := range add {
		ok, err := hasColumn(db, a.table, a.col)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := db.Exec(a.ddl); err != nil {
				return err
			}
			log.Printf("Datenbank migriert: Spalte %s.%s ergänzt", a.table, a.col)
		}
	}
	for _, ddl := range []string{
		`CREATE INDEX IF NOT EXISTS idx_locations_user_ts ON locations(user_id, ts)`,
		`CREATE INDEX IF NOT EXISTS idx_commands_user ON commands(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tours_user ON tours(user_id, start_ts)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// bootstrap legt beim ersten Start den Admin an (aus ADMIN_USER/ADMIN_PASSWORD)
// und ordnet ihm alle Daten aus der Zeit vor der Mehrbenutzer-Version zu.
func bootstrap(db *sql.DB, cfg config) error {
	var users int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		return err
	}
	if users == 0 {
		if len(cfg.AdminPassword) < 8 {
			return errors.New("erster Start: ADMIN_PASSWORD muss gesetzt sein (mind. 8 Zeichen)")
		}
		if !validUsername(cfg.AdminUser) {
			return errors.New("ADMIN_USER ungültig (3–32 Zeichen: Buchstaben, Ziffern, . _ -)")
		}
		hash, err := hashPassword(cfg.AdminPassword)
		if err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO users(username,pw_hash,role,created) VALUES(?,?,'admin',?)`,
			cfg.AdminUser, hash, nowMs()); err != nil {
			return err
		}
		log.Printf("Admin-Benutzer %q angelegt", cfg.AdminUser)
	}
	admin, err := firstAdmin(db)
	if err != nil {
		return err
	}
	for _, t := range []string{"locations", "commands", "tours"} {
		res, err := db.Exec(`UPDATE `+t+` SET user_id=? WHERE user_id IS NULL`, admin)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("%d Einträge in %s dem Admin zugeordnet", n, t)
		}
	}
	return nil
}

func firstAdmin(db *sql.DB) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM users WHERE role='admin' ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("kein Admin-Benutzer vorhanden")
	}
	return id, err
}

func validUsername(u string) bool {
	if len(u) < 3 || len(u) > 32 {
		return false
	}
	for _, c := range u {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			return false
		}
	}
	return true
}
