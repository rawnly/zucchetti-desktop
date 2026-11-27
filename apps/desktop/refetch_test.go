package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestTriggerRefetchQuery(t *testing.T) {
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.set(r)
		fmt.Fprint(w, `{"instanceId":"i1"}`)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Token: "t"}
	if _, err := c.TriggerRefetch(context.Background(), 2026, 10); err != nil {
		t.Fatal(err)
	}
	method, path, _, q := rec.get()
	if method != http.MethodPost || path != "/run" || q != "date=2026-10-15&notify=0" {
		t.Errorf("got %s %s ?%s", method, path, q)
	}
	if _, err := c.TriggerRefetch(context.Background(), 2026, 13); err == nil {
		t.Error("month 13 accepted")
	}
}

func TestWaitForRun(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n.Add(1) {
		case 1:
			http.Error(w, "boom", 500) // a failed poll is retried
		case 2:
			fmt.Fprint(w, `{"status":"running"}`)
		default:
			fmt.Fprint(w, `{"status":"complete"}`)
		}
	}))
	defer srv.Close()
	var seen []string
	st, err := (&Client{BaseURL: srv.URL, Token: "t"}).WaitForRun(context.Background(), "i1", time.Millisecond, func(s string) { seen = append(seen, s) })
	if err != nil || st.Status != "complete" {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if len(seen) != 2 || seen[0] != "running" || seen[1] != "complete" {
		t.Errorf("statuses %v", seen)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	stuck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"running"}`) }))
	defer stuck.Close()
	if _, err := (&Client{BaseURL: stuck.URL, Token: "t"}).WaitForRun(ctx, "i1", time.Millisecond, nil); err == nil {
		t.Error("expected timeout")
	}
}

func TestRefetchRewritesFile(t *testing.T) {
	var mu sync.Mutex
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		switch {
		case r.URL.Path == "/run":
			fmt.Fprint(w, `{"instanceId":"i1"}`)
		case r.URL.Path == "/status/i1":
			fmt.Fprint(w, `{"status":"complete"}`)
		case r.URL.Path == "/buste/busta-2026-10.pdf":
			fmt.Fprint(w, "%PDF-1.4 fresh")
		default:
			fmt.Fprint(w, `{"items":[{"key":"busta-2026-10.pdf","year":2026,"month":10,"size":14}]}`)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	old := filepath.Join(dir, "busta-2026-10.pdf")
	os.WriteFile(old, []byte("%PDF-1.4 stale"), 0o600)

	app := newTestApp(t)
	var main sync.Mutex // stands in for the main thread
	app.runOnMain = func(fn func()) { main.Lock(); defer main.Unlock(); fn() }
	app.cfg = Config{WorkerURL: srv.URL, DownloadDir: dir}
	app.token = "t"
	p := Payslip{Key: "busta-2026-10.pdf", Year: 2026, Month: 10}
	app.payslips = []Payslip{p}
	app.Refetch(p)
	if !app.refetching[p.Key] {
		t.Fatal("expected refetching flag")
	}
	app.Refetch(p) // ignored while one runs
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		main.Lock()
		done := len(app.refetching) == 0
		main.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // the follow-up Refresh lands
	b, _ := os.ReadFile(old)
	if string(b) != "%PDF-1.4 fresh" {
		t.Fatalf("file not rewritten: %q", b)
	}
	main.Lock()
	defer main.Unlock()
	if !app.downloaded[p.Key] || app.noticeIsError {
		t.Errorf("downloaded=%v notice=%q err=%v", app.downloaded[p.Key], app.noticeMsg, app.noticeIsError)
	}
}

func TestQuickLookAndRefetchButtons(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "busta-2026-11.pdf"), []byte("%PDF-1.4"), 0o600)
	app := newTestApp(t)
	app.cfg = Config{WorkerURL: "https://example.com", DownloadDir: dir}
	app.token = "t"
	app.hasLoaded = true
	app.payslips = []Payslip{{Key: "busta-2026-11.pdf", Year: 2026, Month: 11, Size: 1000, Uploaded: time.Now()}}
	app.ScanLocalFiles()
	var refetched string
	app.onRefetch = func(p Payslip) { refetched = p.Key }

	tt := ui.NewTester(app.view, 368, 488)
	tt.Frame()
	if err := tt.Click("Quick Look November 2026"); err != nil {
		t.Fatal(err)
	}
	fp := app.platform.(*fakePlatform)
	if len(fp.previewed) == 0 || fp.previewed[0] != filepath.Join(dir, "busta-2026-11.pdf") {
		t.Errorf("previewed %v", fp.previewed)
	}
	if err := tt.Click("Re-fetch November 2026"); err != nil {
		t.Fatal(err)
	}
	if refetched != "busta-2026-11.pdf" {
		t.Errorf("refetched %q", refetched)
	}
}

func TestDetailsShowInRowAndPTO(t *testing.T) {
	dir := t.TempDir()
	writePDF(t, filepath.Join(dir, "busta-2026-11.pdf"), sampleLines)
	app := newTestApp(t)
	var main sync.Mutex
	app.runOnMain = func(fn func()) { main.Lock(); defer main.Unlock(); fn() }
	app.cfg = Config{WorkerURL: "https://example.com", DownloadDir: dir}
	app.token = "t"
	app.hasLoaded = true
	app.payslips = []Payslip{{Key: "busta-2026-11.pdf", Year: 2026, Month: 11, Size: 1000, Uploaded: time.Now()}}
	app.ScanLocalFiles()
	app.loadDetails()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		main.Lock()
		n := len(app.details)
		main.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	main.Lock()
	defer main.Unlock()
	tt := ui.NewTester(app.view, 368, 488)
	tt.Frame()
	if !tt.HasText("€1,234.50 net · Novembre 2026") {
		t.Errorf("row text missing: %q", tt.Texts())
	}
	tt.Click("PTO")
	tt.Frame()
	for _, want := range []string{"Leave left", "Ex-holidays", "12.0 h · 1.5 d", "PAR leave", "11.5 h · 1.4 d"} {
		if !tt.HasText(want) {
			t.Errorf("PTO missing %q: %q", want, tt.Texts())
		}
	}
}

func TestPeriodMismatchWarning(t *testing.T) {
	app := newTestApp(t)
	app.cfg = Config{WorkerURL: "https://example.com", DownloadDir: t.TempDir()}
	app.token = "t"
	app.hasLoaded = true
	p := Payslip{Key: "busta-2026-11.pdf", Year: 2026, Month: 11, Size: 1000, Uploaded: time.Now()}
	app.payslips = []Payslip{p}
	app.downloaded[p.Key] = true
	app.details[p.Key] = PayslipData{Period: "Ottobre 2026", HasNet: true, NetCents: 293600}
	if !app.periodMismatch(p) {
		t.Fatal("expected a mismatch")
	}
	tt := ui.NewTester(app.view, 368, 488)
	tt.Frame()
	if !tt.HasText("Contains Ottobre 2026, not November 2026") {
		t.Errorf("warning missing: %q", tt.Texts())
	}
	app.details[p.Key] = PayslipData{Period: "Novembre 2026", HasNet: true, NetCents: 301700}
	if app.periodMismatch(p) {
		t.Error("matching period flagged")
	}
}
