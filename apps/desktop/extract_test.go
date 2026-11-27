package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// layout mimics the order the real payslip's text comes out in, with
// invented numbers.
var sampleLines = []string{
	"ACME SPA",
	"Novembre 2026",
	"Residuo AP Spettante Goduto Saldo",
	"10,00000 10,00000 ORE",
	"FER.ILL.",
	"Ferie 120,00000 100,00000 ORE",
	"16,00000 4,00000 12,00000 ORE",
	"Perm.Ex-Fs",
	"1.234,50 €",
	"1,50000 40,00000 30,00000 11,50000 ORE",
	"Perm.P.A.R",
}

func TestParsePayslipText(t *testing.T) {
	d := parsePayslipText(sampleLines)
	if d.Period != "Novembre 2026" {
		t.Errorf("period %q", d.Period)
	}
	if !d.HasNet || d.NetCents != 123450 || d.Net() != "1,234.50" {
		t.Errorf("net %v %d %s", d.HasNet, d.NetCents, d.Net())
	}
	if d.Ferie == nil || *d.Ferie != (Balance{10, 120, 100, 30}) {
		// 10 + 120 - 100 = 30, but the sample says 10: must be rejected
		if d.Ferie != nil {
			t.Errorf("inconsistent ferie accepted: %+v", d.Ferie)
		}
	}
	if d.ExFs == nil || *d.ExFs != (Balance{Accrued: 16, Taken: 4, Remaining: 12}) {
		t.Errorf("ex-fs %+v", d.ExFs)
	}
	if d.PAR == nil || *d.PAR != (Balance{1.5, 40, 30, 11.5}) {
		t.Errorf("par %+v", d.PAR)
	}
}

func TestParseAcceptsConsistentFerie(t *testing.T) {
	d := parsePayslipText([]string{"20,00000 30,00000 ORE", "Ferie 120,00000 110,00000 ORE"})
	if d.Ferie == nil || *d.Ferie != (Balance{20, 120, 110, 30}) {
		t.Errorf("ferie %+v", d.Ferie)
	}
}

func TestParseRefusesAmbiguousNet(t *testing.T) {
	d := parsePayslipText([]string{"1.000,00 €", "2.000,00 €"})
	if d.HasNet {
		t.Error("two net candidates must give none")
	}
}

func TestParseRejectsBrokenArithmetic(t *testing.T) {
	d := parsePayslipText([]string{"16,00000 4,00000 99,00000 ORE", "Perm.Ex-Fs"})
	if d.ExFs != nil {
		t.Errorf("accepted %+v", d.ExFs)
	}
}

func TestParseIT(t *testing.T) {
	for in, want := range map[string]float64{"1.234,56": 1234.56, "0,08333": 0.08333, "58,50000": 58.5, "12.345.678,9": 12345678.9} {
		if got, ok := parseIT(in); !ok || got != want {
			t.Errorf("parseIT(%q) = %v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "12", "1,2,3", "a,b", "1.23,4"} {
		if _, ok := parseIT(bad); ok {
			t.Errorf("parseIT(%q) accepted", bad)
		}
	}
}

// writePDF builds a one-page PDF with one text line per entry, top first.
func writePDF(t *testing.T, path string, lines []string) {
	t.Helper()
	var content strings.Builder
	content.WriteString("BT /F1 10 Tf\n")
	y := 780
	for _, l := range lines {
		fmt.Fprintf(&content, "1 0 0 1 40 %d Tm (%s) Tj\n", y, strings.ReplaceAll(l, "€", `\200`)) // 0x80 is the euro in WinAnsi
		y -= 14
	}
	content.WriteString("ET")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	var offs []int
	for i, o := range objs {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExtractPayslipFromPDF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.pdf")
	writePDF(t, path, sampleLines)
	d, err := ExtractPayslip(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Period != "Novembre 2026" || !d.HasNet || d.NetCents != 123450 || d.ExFs == nil || d.PAR == nil {
		t.Errorf("%+v", d)
	}
}

func TestExtractPayslipErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := ExtractPayslip(filepath.Join(dir, "missing.pdf")); err == nil {
		t.Error("missing file accepted")
	}
	junk := filepath.Join(dir, "junk.pdf")
	os.WriteFile(junk, []byte("%PDF-1.4 not really"), 0o600)
	if _, err := ExtractPayslip(junk); err == nil {
		t.Error("junk accepted")
	}
}

func TestPeriodYM(t *testing.T) {
	if y, m, ok := (PayslipData{Period: "Ottobre 2026"}).PeriodYM(); !ok || y != 2026 || m != 10 {
		t.Errorf("%d %d %v", y, m, ok)
	}
	for _, bad := range []string{"", "Ottobre", "Foo 2026", "Ottobre x"} {
		if _, _, ok := (PayslipData{Period: bad}).PeriodYM(); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}
