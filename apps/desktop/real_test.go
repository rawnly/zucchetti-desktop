package main

import (
	"os"
	"testing"
)

// TestRealPayslip runs against a payslip on this machine: PAYSLIP_PDF=path go test -run Real -v
func TestRealPayslip(t *testing.T) {
	p := os.Getenv("PAYSLIP_PDF")
	if p == "" {
		t.Skip("PAYSLIP_PDF not set")
	}
	d, err := ExtractPayslip(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("period=%q hasNet=%v net=%s", d.Period, d.HasNet, d.Net())
	t.Logf("ferie=%+v exfs=%+v par=%+v", d.Ferie, d.ExFs, d.PAR)
}
