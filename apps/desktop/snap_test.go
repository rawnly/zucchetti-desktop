package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestWriteSnapshots(t *testing.T) {
	out := os.Getenv("SNAP_DIR")
	if out == "" {
		t.Skip("SNAP_DIR not set")
	}
	app := newTestApp(t)
	app.cfg.WorkerURL = "https://example.com"
	app.cfg.DownloadDir = t.TempDir()
	app.token = "x"
	app.hasLoaded = true
	for i, m := range []int{11, 10, 9, 8, 7, 6} {
		app.payslips = append(app.payslips, Payslip{Key: fmt.Sprintf("busta-2026-%02d.pdf", m), Year: 2026, Month: m, Size: int64(140000 - i*1000), Uploaded: time.Date(2026, time.Month(m), 27, 0, 0, 0, 0, time.UTC)})
	}
	for _, k := range []string{"busta-2026-10.pdf", "busta-2026-11.pdf"} {
		name, _ := LocalFileName(k)
		writePDF(t, filepath.Join(app.cfg.DownloadDir, name), sampleLines)
	}
	app.runOnMain = func(fn func()) { fn() }
	app.ScanLocalFiles()
	app.loadDetails()
	time.Sleep(300 * time.Millisecond)
	shots := map[string]func(){
		"payslips": func() {},
		"settings": func() { app.OpenSettings() },
		"pto":      func() { app.CloseSettings(); app.tab = TabPTO },
	}
	for _, name := range []string{"payslips", "settings", "pto"} {
		shots[name]()
		tt := ui.NewTester(app.view, 368, 488)
		tt.Frame()
		f, _ := os.Create(out + "/" + name + ".png")
		png.Encode(f, tt.Image())
		f.Close()
	}
}
