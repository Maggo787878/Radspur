package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var vienna, _ = time.LoadLocation("Europe/Vienna")

func tAt(y int, m time.Month, d, h, min int, km float64, typ string) tour {
	return tour{StartTS: time.Date(y, m, d, h, min, 0, 0, vienna).UnixMilli(), DistanceM: km * 1000, MovingS: int64(km * 150), ElevGain: 10, Type: typ}
}

func TestStatsWeeksAcrossYearAndTimezone(t *testing.T) {
	st := stats{vienna}
	tours := []tour{
		tAt(2025, 12, 29, 8, 0, 10, "bike"), // Mo 29.12.2025 → KW 1/2026
		tAt(2026, 1, 4, 23, 30, 5, "run"),   // So 04.01. 23:30 → KW 1/2026
		tAt(2026, 1, 5, 0, 30, 7, "bike"),   // Mo 05.01. 00:30 Wien (in UTC noch Sonntag!) → KW 2
	}
	ws := st.periods(tours, week, st.day(time.Date(2026, 1, 6, 12, 0, 0, 0, vienna).UnixMilli()), 0)
	if len(ws) != 2 || ws[0].Label != "KW 1/2026" || ws[1].Label != "KW 2/2026" {
		t.Fatalf("Wochen: %+v", ws)
	}
	if ws[0].Totals.DistanceM != 15000 || ws[0].Totals.RunM != 5000 || ws[0].Totals.BikeM != 10000 || ws[1].Totals.DistanceM != 7000 {
		t.Errorf("Summen: %+v / %+v", ws[0].Totals, ws[1].Totals)
	}
	if ws[0].StartS != "2025-12-29" {
		t.Errorf("Wochenbeginn: %s", ws[0].StartS)
	}
}

func TestStatsCurrentAndGapless(t *testing.T) {
	st := stats{vienna}
	today := st.day(time.Date(2026, 11, 24, 12, 0, 0, 0, vienna).UnixMilli()) // Dienstag
	tours := []tour{
		tAt(2025, 11, 24, 9, 0, 99, "bike"),
		tAt(2026, 10, 31, 9, 0, 50, "bike"),
		tAt(2026, 11, 22, 9, 0, 40, "bike"), // So, Vorwoche
		tAt(2026, 11, 23, 7, 0, 12.5, "bike"),
		tAt(2026, 11, 24, 18, 0, 8, "run"),
	}
	if c := st.current(tours, week, today); c.Count != 2 || c.DistanceM != 20500 {
		t.Errorf("Woche: %+v", c)
	}
	if c := st.current(tours, month, today); c.DistanceM != 60500 {
		t.Errorf("Monat: %+v", c)
	}
	if c := st.current(tours, year, today); c.DistanceM != 110500 {
		t.Errorf("Jahr: %+v", c)
	}
	ms := st.periods(tours, month, today, 12)
	if len(ms) != 12 || ms[11].Label != "November 2026" || ms[0].Label != "Dezember 2025" || ms[11].Short != "Nov" {
		t.Errorf("12 Monate: erster %q letzter %q (%d)", ms[0].Label, ms[11].Label, len(ms))
	}
	if ms[3].Short != "Mär" || ms[3].Label != "März 2026" {
		t.Errorf("März: %q / %q", ms[3].Short, ms[3].Label)
	}
	all := st.periods(tours, month, today, 0)
	if len(all) != 13 || all[0].Label != "November 2025" {
		t.Errorf("alle Monate: %d, erster %q", len(all), all[0].Label)
	}
	l, f, _ := st.records(tours)
	if l.DistanceM != 99000 || f == nil {
		t.Errorf("Rekorde: %+v %+v", l, f)
	}
}

func TestStatsEndpointsOnlyOwnTours(t *testing.T) {
	_, ts := newTestServer(t)
	admin := newBrowser(t, ts)
	admin.login("admin", adminPW)
	anna := inviteUser(t, ts, admin, "anna", "anna-pass-123")
	annaPhone, _ := deviceLogin(t, ts, "anna", "anna-pass-123")
	body := tourBody("a1", "Annas Lauf")
	body["type"] = "run"
	body["startTs"] = time.Now().UnixMilli()
	annaPhone.do("/api/device/tours", body)

	var s struct {
		Total   statTotals
		Week    statTotals
		Weeks   []period
		Records map[string]*recordTour
	}
	anna.json("GET", "/api/web/stats?tz=Europe/Vienna", nil, &s)
	if s.Total.Count != 1 || s.Total.RunM != 25000 || s.Week.Count != 1 || len(s.Weeks) != 12 || s.Records["longest"] == nil {
		t.Errorf("anna Statistik: %+v", s)
	}
	anna.json("GET", "/api/web/stats?sport=bike", nil, &s)
	if s.Total.Count != 0 {
		t.Errorf("Filter Rad: %+v", s.Total)
	}
	admin.json("GET", "/api/web/stats", nil, &s)
	if s.Total.Count != 0 || s.Records["longest"] != nil {
		t.Errorf("Admin sieht fremde Statistik: %+v", s.Total)
	}

	// Excel
	req, _ := http.NewRequest("GET", ts.URL+"/api/web/stats.xlsx?tz=Europe/Vienna", nil)
	res, err := anna.c.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("xlsx: %v %d", err, res.StatusCode)
	}
	data, _ := io.ReadAll(res.Body)
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "xl/worksheets/sheet5.xml") {
		t.Errorf("xlsx-Inhalt: %v", names)
	}
	if !strings.Contains(res.Header.Get("Content-Disposition"), "radspur-auswertung-") {
		t.Errorf("Dateiname: %q", res.Header.Get("Content-Disposition"))
	}
	if p := os.Getenv("XLSX_OUT"); p != "" {
		_ = os.WriteFile(p, data, 0o644)
	}
	res2, _ := http.Get(ts.URL + "/api/web/stats")
	expect(t, "Statistik ohne Anmeldung", res2.StatusCode, 401)
	_ = json.Valid
}
