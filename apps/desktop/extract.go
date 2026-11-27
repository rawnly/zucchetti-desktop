package main

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Balance is a leave account in hours, as the payslip's "Residuo AP /
// Spettante / Goduto / Saldo" columns give it.
type Balance struct {
	Residual  float64 // carried over from last year
	Accrued   float64
	Taken     float64
	Remaining float64
}

// PayslipData is what the app reads from a payslip PDF. Only values that pass
// a consistency check are set; the raw text is never kept.
type PayslipData struct {
	Period   string // as printed, e.g. "Ottobre 2026"
	NetCents int64
	HasNet   bool
	Ferie    *Balance
	ExFs     *Balance
	PAR      *Balance
}

// Net formats the net pay as "2,936.00".
func (d PayslipData) Net() string {
	c := d.NetCents
	whole, frac := c/100, c%100
	s := strconv.FormatInt(whole, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return fmt.Sprintf("%s.%02d", s, frac)
}

// ExtractPayslip reads the text layer of a payslip PDF.
func ExtractPayslip(path string) (PayslipData, error) {
	lines, err := pdfLines(path)
	if err != nil {
		return PayslipData{}, err
	}
	return parsePayslipText(lines), nil
}

func pdfLines(path string) (lines []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("unreadable PDF: %v", r)
		}
	}()
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// The employer's payslip prints on page 1 and repeats its header on the
	// next ones, so reading the first two pages covers the totals.
	for i := 1; i <= r.NumPage() && i <= 2; i++ {
		rows, err := r.Page(i).GetTextByRow()
		if err != nil {
			continue
		}
		for _, row := range rows {
			var words []string
			for _, w := range row.Content {
				words = append(words, w.S)
			}
			lines = append(lines, strings.Join(strings.Fields(strings.Join(words, " ")), " "))
		}
	}
	return lines, nil
}

var (
	periodRe = regexp.MustCompile(`^(Gennaio|Febbraio|Marzo|Aprile|Maggio|Giugno|Luglio|Agosto|Settembre|Ottobre|Novembre|Dicembre) \d{4}$`)
	netRe    = regexp.MustCompile(`^(\d{1,3}(?:\.\d{3})*,\d{2}) €$`)
	numRe    = regexp.MustCompile(`^\d{1,3}(?:\.\d{3})*,\d+$|^\d+,\d+$`)
)

// parseIT reads an Italian number: "1.234,56" is 1234.56.
func parseIT(s string) (float64, bool) {
	if !numRe.MatchString(s) {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", "."), 64)
	return v, err == nil
}

// hours returns the numbers of a line like "32,00000 26,66666 5,33334 ORE".
func hours(line string) []float64 {
	f := strings.Fields(line)
	if len(f) < 2 || f[len(f)-1] != "ORE" {
		return nil
	}
	var out []float64
	for _, t := range f[:len(f)-1] {
		v, ok := parseIT(t)
		if !ok {
			return nil
		}
		out = append(out, v)
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// balance builds a Balance from 3 numbers (accrued, taken, remaining) or 4
// (residual first), and keeps it only when the columns add up.
func balance(n []float64) *Balance {
	switch len(n) {
	case 3:
		if near(n[0]-n[1], n[2]) {
			return &Balance{Accrued: n[0], Taken: n[1], Remaining: n[2]}
		}
	case 4:
		if near(n[0]+n[1]-n[2], n[3]) {
			return &Balance{Residual: n[0], Accrued: n[1], Taken: n[2], Remaining: n[3]}
		}
	}
	return nil
}

// labelled finds the numbers of a leave row whose label sits on its own
// line: the layout puts them on the line before, sometimes after.
func labelled(lines []string, i, want int) *Balance {
	for _, j := range []int{i - 1, i + 1} {
		if j < 0 || j >= len(lines) {
			continue
		}
		if n := hours(lines[j]); len(n) == want {
			if b := balance(n); b != nil {
				return b
			}
		}
	}
	return nil
}

func parsePayslipText(lines []string) PayslipData {
	var d PayslipData
	var nets []int64
	for i, l := range lines {
		switch {
		case d.Period == "" && periodRe.MatchString(l):
			d.Period = l
		case netRe.MatchString(l):
			v, _ := parseIT(netRe.FindStringSubmatch(l)[1])
			nets = append(nets, int64(math.Round(v*100)))
		case l == "Perm.Ex-Fs" && d.ExFs == nil:
			d.ExFs = labelled(lines, i, 3)
		case l == "Perm.P.A.R" && d.PAR == nil:
			d.PAR = labelled(lines, i, 4)
		case strings.HasPrefix(l, "Ferie ") && d.Ferie == nil:
			// "Ferie <accrued> <taken> ORE", with "<residual> <remaining>
			// ORE" on a line nearby.
			ak := hours(strings.TrimPrefix(l, "Ferie "))
			if len(ak) != 2 {
				continue
			}
			for j := max(0, i-3); j < min(len(lines), i+4); j++ {
				if rr := hours(lines[j]); j != i && len(rr) == 2 {
					if b := balance([]float64{rr[0], ak[0], ak[1], rr[1]}); b != nil {
						d.Ferie = b
						break
					}
				}
			}
		}
	}
	// Several candidates means the layout changed: better none than wrong.
	if len(nets) == 1 {
		d.NetCents, d.HasNet = nets[0], true
	}
	return d
}

var italianMonths = map[string]int{
	"Gennaio": 1, "Febbraio": 2, "Marzo": 3, "Aprile": 4, "Maggio": 5, "Giugno": 6,
	"Luglio": 7, "Agosto": 8, "Settembre": 9, "Ottobre": 10, "Novembre": 11, "Dicembre": 12,
}

// PeriodYM returns the year and month of the printed period, if it has one.
func (d PayslipData) PeriodYM() (year, month int, ok bool) {
	name, y, found := strings.Cut(d.Period, " ")
	m := italianMonths[name]
	yy, err := strconv.Atoi(y)
	if !found || m == 0 || err != nil {
		return 0, 0, false
	}
	return yy, m, true
}
