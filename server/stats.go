package main

import (
	"fmt"
	"net/http"
	"time"
	_ "time/tzdata" // Zeitzonen auch im schlanken Container (ohne tzdata-Paket)
)

// ---------- Auswertung (Woche/Monat/Jahr), gleiche Regeln wie in der App ----------

type statTotals struct {
	Count     int     `json:"count"`
	DistanceM float64 `json:"distanceM"`
	MovingS   int64   `json:"movingS"`
	ElevGain  float64 `json:"elevGain"`
	BikeM     float64 `json:"bikeM"`
	RunM      float64 `json:"runM"`
}

func (t *statTotals) add(x tour) {
	t.Count++
	t.DistanceM += x.DistanceM
	t.MovingS += x.MovingS
	t.ElevGain += x.ElevGain
	if x.Type == "run" {
		t.RunM += x.DistanceM
	} else {
		t.BikeM += x.DistanceM
	}
}

func (t statTotals) avgSpeed() float64 {
	if t.MovingS == 0 {
		return 0
	}
	return t.DistanceM / float64(t.MovingS)
}

type unit int

const (
	week unit = iota
	month
	year
)

type period struct {
	Start  time.Time  `json:"-"`
	StartS string     `json:"start"` // JJJJ-MM-TT
	Label  string     `json:"label"` // "KW 47/2026", "November 2026", "2026"
	Short  string     `json:"short"` // "47", "Nov", "2026"
	Totals statTotals `json:"totals"`
}

var monthsDE = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
var monthsShortDE = []string{"Jan", "Feb", "Mär", "Apr", "Mai", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dez"}

type stats struct{ loc *time.Location }

func (s stats) day(ts int64) time.Time {
	t := time.UnixMilli(ts).In(s.loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, s.loc)
}

func (s stats) periodStart(d time.Time, u unit) time.Time {
	switch u {
	case week: // ISO: Woche beginnt am Montag
		return time.Date(d.Year(), d.Month(), d.Day()-(int(d.Weekday())+6)%7, 0, 0, 0, 0, s.loc)
	case month:
		return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, s.loc)
	default:
		return time.Date(d.Year(), 1, 1, 0, 0, 0, 0, s.loc)
	}
}

func next(d time.Time, u unit) time.Time {
	switch u {
	case week:
		return d.AddDate(0, 0, 7)
	case month:
		return d.AddDate(0, 1, 0)
	default:
		return d.AddDate(1, 0, 0)
	}
}

func makePeriod(d time.Time, u unit) period {
	p := period{Start: d, StartS: d.Format("2006-01-02")}
	switch u {
	case week:
		y, w := d.ISOWeek()
		p.Label, p.Short = fmt.Sprintf("KW %d/%d", w, y), fmt.Sprint(w)
	case month:
		p.Label, p.Short = monthsDE[d.Month()-1]+" "+fmt.Sprint(d.Year()), monthsShortDE[d.Month()-1]
	default:
		p.Label, p.Short = fmt.Sprint(d.Year()), fmt.Sprint(d.Year())
	}
	return p
}

func (s stats) total(tours []tour) statTotals {
	var t statTotals
	for _, x := range tours {
		t.add(x)
	}
	return t
}

// current: Summe des Zeitraums, der today enthält.
func (s stats) current(tours []tour, u unit, today time.Time) statTotals {
	start := s.periodStart(today, u)
	var t statTotals
	for _, x := range tours {
		if s.periodStart(s.day(x.StartTS), u).Equal(start) {
			t.add(x)
		}
	}
	return t
}

// periods: lückenlose Zeiträume bis einschließlich today, älteste zuerst.
// last > 0 begrenzt auf die letzten n, sonst ab der ersten Tour.
func (s stats) periods(tours []tour, u unit, today time.Time, last int) []period {
	end := s.periodStart(today, u)
	by := map[time.Time]*statTotals{}
	first := end
	for _, x := range tours {
		k := s.periodStart(s.day(x.StartTS), u)
		if by[k] == nil {
			by[k] = &statTotals{}
		}
		by[k].add(x)
		if k.Before(first) {
			first = k
		}
	}
	if last > 0 {
		first = end
		for i := 1; i < last; i++ {
			switch u {
			case week:
				first = first.AddDate(0, 0, -7)
			case month:
				first = first.AddDate(0, -1, 0)
			default:
				first = first.AddDate(-1, 0, 0)
			}
		}
	}
	var out []period
	for d := first; !d.After(end); d = next(d, u) {
		p := makePeriod(d, u)
		if t := by[d]; t != nil {
			p.Totals = *t
		}
		out = append(out, p)
	}
	return out
}

type recordTour struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	StartTS   int64   `json:"startTs"`
	DistanceM float64 `json:"distanceM"`
	MovingS   int64   `json:"movingS"`
	ElevGain  float64 `json:"elevGain"`
}

func rec(t *tour) *recordTour {
	if t == nil {
		return nil
	}
	return &recordTour{t.ID, t.Name, t.Type, t.StartTS, t.DistanceM, t.MovingS, t.ElevGain}
}

func (s stats) records(tours []tour) (longest, fastest, climb *tour) {
	for i := range tours {
		t := &tours[i]
		if longest == nil || t.DistanceM > longest.DistanceM {
			longest = t
		}
		// Ø-Tempo erst ab 1 km sinnvoll, sonst gewinnen Mini-Touren
		if t.DistanceM >= 1000 && t.MovingS > 0 &&
			(fastest == nil || t.DistanceM/float64(t.MovingS) > fastest.DistanceM/float64(fastest.MovingS)) {
			fastest = t
		}
		if t.ElevGain > 0 && (climb == nil || t.ElevGain > climb.ElevGain) {
			climb = t
		}
	}
	return
}

// ---------- HTTP ----------

// statsLocation: Zeitzone des Browsers (?tz=Europe/Vienna), sonst Europe/Vienna.
func statsLocation(r *http.Request) *time.Location {
	if loc, err := time.LoadLocation(r.URL.Query().Get("tz")); err == nil && r.URL.Query().Get("tz") != "" {
		return loc
	}
	loc, _ := time.LoadLocation("Europe/Vienna")
	return loc
}

// userTours lädt alle Touren des angemeldeten Benutzers (ohne Punkte).
func (s *server) userTours(r *http.Request, sport string) ([]tour, error) {
	q := `SELECT id,client_id,name,start_ts,end_ts,distance_m,moving_s,max_speed,elev_gain,type FROM tours WHERE user_id=?`
	args := []any{currentUser(r).ID}
	if sports[sport] {
		q += ` AND type=?`
		args = append(args, sport)
	}
	rows, err := s.db.Query(q+` ORDER BY start_ts`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tour
	for rows.Next() {
		var t tour
		if err := rows.Scan(&t.ID, &t.ClientID, &t.Name, &t.StartTS, &t.EndTS, &t.DistanceM, &t.MovingS, &t.MaxSpeed, &t.ElevGain, &t.Type); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *server) getStats(w http.ResponseWriter, r *http.Request) {
	tours, err := s.userTours(r, r.URL.Query().Get("sport"))
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	st := stats{statsLocation(r)}
	today := st.day(nowMs())
	total := st.total(tours)
	out := map[string]any{
		"week":   st.current(tours, week, today),
		"month":  st.current(tours, month, today),
		"year":   st.current(tours, year, today),
		"total":  total,
		"weeks":  st.periods(tours, week, today, 12),
		"months": st.periods(tours, month, today, 12),
	}
	if total.Count > 0 {
		days := today.Sub(st.day(tours[0].StartTS)).Hours()/24 + 1
		out["perWeekM"] = total.DistanceM / max(1, days/7)
		out["perTourM"] = total.DistanceM / float64(total.Count)
		out["avgSpeed"] = total.avgSpeed()
	}
	l, f, c := st.records(tours)
	out["records"] = map[string]*recordTour{"longest": rec(l), "fastest": rec(f), "mostClimb": rec(c)}
	writeJSON(w, out)
}
