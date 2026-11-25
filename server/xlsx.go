package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Minimaler XLSX-Writer (Office Open XML) – Gegenstück zum Writer in der App.

type xfmt int // Index in cellXfs (siehe xlsxStyles)

const (
	fGeneral xfmt = iota
	fHeader
	fDate
	fDateTime
	fDuration
	fDec2
	fInt
	fDec1
	fPace
)

type xcell struct {
	text  string
	num   float64
	isNum bool
	empty bool
	fmt   xfmt
}

func xtxt(s string) xcell              { return xcell{text: s} }
func xhead(s string) xcell             { return xcell{text: s, fmt: fHeader} }
func xnum(v float64, f xfmt) xcell     { return xcell{num: v, isNum: true, fmt: f} }
func xempty() xcell                    { return xcell{empty: true} }
func xduration(seconds int64) xcell    { return xnum(float64(seconds)/86400, fDuration) }
func xpace(secondsPerKm float64) xcell { return xnum(secondsPerKm/86400, fPace) }
func xdate(t time.Time) xcell          { return xnum(excelSerial(t), fDate) }
func xdatetime(t time.Time) xcell      { return xnum(excelSerial(t), fDateTime) }

// excelSerial: Tage seit 30.12.1899 nach lokaler Uhrzeit von t.
func excelSerial(t time.Time) float64 {
	epoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	wall := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
	return wall.Sub(epoch).Hours() / 24
}

type xsheet struct {
	name   string
	widths []float64
	rows   [][]xcell
}

func (s *xsheet) row(c ...xcell) { s.rows = append(s.rows, c) }

func xcol(i int) string {
	if i < 26 {
		return string(rune('A' + i))
	}
	return xcol(i/26-1) + string(rune('A'+i%26))
}

var xmlRepl = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func xesc(s string) string {
	s = strings.Map(func(r rune) rune { // in XML 1.0 unzulässige Steuerzeichen entfernen
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s)
	return xmlRepl.Replace(s)
}

const xNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
const xREL = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

const xlsxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="` + xNS + `">
<numFmts count="5"><numFmt numFmtId="164" formatCode="dd.mm.yyyy"/><numFmt numFmtId="165" formatCode="dd.mm.yyyy hh:mm"/><numFmt numFmtId="166" formatCode="[h]:mm"/><numFmt numFmtId="167" formatCode="0.0"/><numFmt numFmtId="168" formatCode="[m]:ss"/></numFmts>
<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>
<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>
<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="9">
<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>
<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>
<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="166" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="2" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="1" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="167" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
<xf numFmtId="168" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>
</cellXfs>
<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
</styleSheet>`

func writeXlsx(out io.Writer, sheets []*xsheet) error {
	z := zip.NewWriter(out)
	put := func(name, content string) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, content)
		return err
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	b.WriteString(`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := range sheets {
		fmt.Fprintf(&b, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
	}
	b.WriteString(`</Types>`)
	parts := map[string]string{"[Content_Types].xml": b.String(),
		"_rels/.rels":   `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="` + xREL + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/styles.xml": xlsxStyles}

	b.Reset()
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="` + xNS + `" xmlns:r="` + xREL + `"><sheets>`)
	for i, s := range sheets {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xesc(s.name), i+1, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	parts["xl/workbook.xml"] = b.String()

	b.Reset()
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := range sheets {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="%s/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, xREL, i+1)
	}
	fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="%s/styles" Target="styles.xml"/></Relationships>`, len(sheets)+1, xREL)
	parts["xl/_rels/workbook.xml.rels"] = b.String()

	for i, s := range sheets {
		b.Reset()
		b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="` + xNS + `">`)
		b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
		if len(s.widths) > 0 {
			b.WriteString("<cols>")
			for j, wd := range s.widths {
				fmt.Fprintf(&b, `<col min="%d" max="%d" width="%g" customWidth="1"/>`, j+1, j+1, wd)
			}
			b.WriteString("</cols>")
		}
		b.WriteString("<sheetData>")
		for r, cells := range s.rows {
			fmt.Fprintf(&b, `<row r="%d">`, r+1)
			for c, cell := range cells {
				ref := xcol(c) + strconv.Itoa(r+1)
				switch {
				case cell.empty:
				case cell.isNum:
					fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, cell.fmt, strconv.FormatFloat(cell.num, 'g', -1, 64))
				default:
					st := ""
					if cell.fmt == fHeader {
						st = ` s="1"`
					}
					fmt.Fprintf(&b, `<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, ref, st, xesc(cell.text))
				}
			}
			b.WriteString("</row>")
		}
		b.WriteString("</sheetData></worksheet>")
		parts[fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)] = b.String()
	}
	// feste Reihenfolge: [Content_Types].xml zuerst
	order := []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml"}
	for i := range sheets {
		order = append(order, fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1))
	}
	for _, n := range order {
		if err := put(n, parts[n]); err != nil {
			return err
		}
	}
	return z.Close()
}

// ---------- Auswertung als Excel ----------

var totalsHeader = []string{"Touren", "km gesamt", "km Rad", "km Laufen", "Zeit in Bewegung", "Höhenmeter"}

func totalsCells(t statTotals) []xcell {
	return []xcell{xnum(float64(t.Count), fInt), xnum(t.DistanceM/1000, fDec2), xnum(t.BikeM/1000, fDec2),
		xnum(t.RunM/1000, fDec2), xduration(t.MovingS), xnum(t.ElevGain, fInt)}
}

func heads(h ...string) []xcell {
	out := make([]xcell, len(h))
	for i, s := range h {
		out[i] = xhead(s)
	}
	return out
}

func buildReport(tours []tour, st stats, now time.Time) []*xsheet {
	today := st.day(now.UnixMilli())
	ov := &xsheet{name: "Übersicht", widths: []float64{16, 8, 11, 10, 11, 17, 11, 10}}
	ov.row(heads(append([]string{"Zeitraum"}, append(totalsHeader, "Ø km/h")...)...)...)
	line := func(label string, t statTotals) {
		ov.row(append(append([]xcell{xtxt(label)}, totalsCells(t)...), xnum(t.avgSpeed()*3.6, fDec1))...)
	}
	line("Diese Woche", st.current(tours, week, today))
	line("Dieser Monat", st.current(tours, month, today))
	line("Dieses Jahr", st.current(tours, year, today))
	line("Gesamt", st.total(tours))
	ov.row()
	ov.row(xtxt("Erstellt"), xdatetime(now.In(st.loc)))

	periodSheet := func(name, first string, ps []period) *xsheet {
		s := &xsheet{name: name, widths: []float64{16, 12, 8, 11, 10, 11, 17, 11}}
		s.row(heads(append([]string{first, "Beginn"}, totalsHeader...)...)...)
		for _, p := range ps {
			s.row(append([]xcell{xtxt(p.Label), xdate(p.Start)}, totalsCells(p.Totals)...)...)
		}
		return s
	}
	ts := &xsheet{name: "Touren", widths: []float64{17, 9, 34, 9, 17, 9, 11, 9, 11}}
	ts.row(heads("Start", "Sportart", "Name", "km", "Zeit in Bewegung", "Ø km/h", "Ø min/km", "max km/h", "Höhenmeter")...)
	for _, t := range tours {
		km := t.DistanceM / 1000
		sport := "Rad"
		if t.Type == "run" {
			sport = "Laufen"
		}
		avg, pace := xempty(), xempty()
		if t.MovingS > 0 {
			avg = xnum(t.DistanceM/float64(t.MovingS)*3.6, fDec1)
		}
		if km >= 0.1 {
			pace = xpace(float64(t.MovingS) / km)
		}
		ts.row(xdatetime(time.UnixMilli(t.StartTS).In(st.loc)), xtxt(sport), xtxt(t.Name), xnum(km, fDec2),
			xduration(t.MovingS), avg, pace, xnum(t.MaxSpeed*3.6, fDec1), xnum(t.ElevGain, fInt))
	}
	return []*xsheet{ov,
		periodSheet("Wochen", "Woche", st.periods(tours, week, today, 0)),
		periodSheet("Monate", "Monat", st.periods(tours, month, today, 0)),
		periodSheet("Jahre", "Jahr", st.periods(tours, year, today, 0)),
		ts}
}

func (s *server) statsXlsx(w http.ResponseWriter, r *http.Request) {
	tours, err := s.userTours(r, "") // Export enthält immer alle Sportarten
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	st := stats{statsLocation(r)}
	now := time.Now()
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="radspur-auswertung-%s.xlsx"`, now.In(st.loc).Format("2006-01-02")))
	if err := writeXlsx(w, buildReport(tours, st, now)); err != nil {
		httpErr(w, err, 500)
	}
}
