package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// recordedRequest captures the parts of a request that tests care about.
type recordedRequest struct {
	mu       sync.Mutex
	method   string
	path     string
	token    string
	rawQuery string
}

func (r *recordedRequest) set(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.method = req.Method
	r.path = req.URL.Path
	r.token = req.Header.Get("x-run-token")
	r.rawQuery = req.URL.RawQuery
}

func (r *recordedRequest) get() (method, path, token, rawQuery string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.method, r.path, r.token, r.rawQuery
}

func TestIsValidBustaKey(t *testing.T) {
	valid := []string{
		"busta-2026-01.pdf",
		"busta-2026-11.pdf",
		"busta-1999-12.pdf",
	}
	for _, key := range valid {
		if !IsValidBustaKey(key) {
			t.Errorf("IsValidBustaKey(%q) = false, want true", key)
		}
	}

	invalid := []string{
		"",
		"busta-2026-13.pdf",
		"busta-2026-00.pdf",
		"busta-26-11.pdf",
		"busta-2026-1.pdf",
		"busta-2026-11.PDF",
		"busta-2026-11.pdfx",
		"busta-2026-11.pdf/",
		"../evil.pdf",
		"busta-2026-11pdf",
	}
	for _, key := range invalid {
		if IsValidBustaKey(key) {
			t.Errorf("IsValidBustaKey(%q) = true, want false", key)
		}
	}
}

func TestPayslipMonthNameAndTitle(t *testing.T) {
	tests := []struct {
		name      string
		p         Payslip
		wantMonth string
		wantTitle string
	}{
		{
			name:      "valid november",
			p:         Payslip{Key: "busta-2026-11.pdf", Year: 2026, Month: 11},
			wantMonth: "November",
			wantTitle: "November 2026",
		},
		{
			name:      "valid january",
			p:         Payslip{Key: "busta-2026-01.pdf", Year: 2026, Month: 1},
			wantMonth: "January",
			wantTitle: "January 2026",
		},
		{
			name:      "zero month falls back to key",
			p:         Payslip{Key: "weird-key", Month: 0},
			wantMonth: "weird-key",
			wantTitle: "weird-key",
		},
		{
			name:      "out of range month falls back to key",
			p:         Payslip{Key: "weird-key", Month: 13},
			wantMonth: "weird-key",
			wantTitle: "weird-key",
		},
		{
			name:      "zero year falls back to key for title",
			p:         Payslip{Key: "busta-2026-05.pdf", Month: 5},
			wantMonth: "May",
			wantTitle: "busta-2026-05.pdf",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.MonthName(); got != tt.wantMonth {
				t.Errorf("MonthName() = %q, want %q", got, tt.wantMonth)
			}
			if got := tt.p.Title(); got != tt.wantTitle {
				t.Errorf("Title() = %q, want %q", got, tt.wantTitle)
			}
		})
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{-1, "0 B"},
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1 KB"},
		{1536, "2 KB"},
		{1024 * 1024, "1.0 MB"},
		{1536 * 1024, "1.5 MB"},
		{5 * 1024 * 1024, "5.0 MB"},
	}
	for _, tt := range tests {
		if got := FormatSize(tt.n); got != tt.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestRunStatusIsTerminal(t *testing.T) {
	for _, status := range []string{"complete", "errored", "terminated"} {
		if !(RunStatus{Status: status}).IsTerminal() {
			t.Errorf("IsTerminal() = false for %q, want true", status)
		}
	}
	for _, status := range []string{"", "queued", "running", "sleeping", "waiting"} {
		if (RunStatus{Status: status}).IsTerminal() {
			t.Errorf("IsTerminal() = true for %q, want false", status)
		}
	}
}

func TestListPayslips(t *testing.T) {
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.set(r)
		if r.URL.Path != "/buste" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items":[
			{"key":"../evil.pdf","year":2020,"month":1},
			{"key":"busta-2025-03.pdf"},
			{"key":"busta-2026-11.pdf","year":2026,"month":11},
			{"key":"busta-2026-13.pdf"},
			{"key":"busta-2024-12.pdf","year":2024,"month":12}
		]}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "secret-token"}
	items, err := c.ListPayslips(context.Background())
	if err != nil {
		t.Fatalf("ListPayslips() error = %v", err)
	}

	method, path, token, _ := rec.get()
	if method != http.MethodGet {
		t.Errorf("request method = %q, want %q", method, http.MethodGet)
	}
	if path != "/buste" {
		t.Errorf("request path = %q, want /buste", path)
	}
	if token != "secret-token" {
		t.Errorf("x-run-token = %q, want secret-token", token)
	}

	if len(items) != 3 {
		t.Fatalf("ListPayslips() returned %d items, want 3: %+v", len(items), items)
	}
	want := []Payslip{
		{Key: "busta-2026-11.pdf", Year: 2026, Month: 11},
		{Key: "busta-2025-03.pdf", Year: 2025, Month: 3},
		{Key: "busta-2024-12.pdf", Year: 2024, Month: 12},
	}
	for i := range want {
		got := items[i]
		if got.Key != want[i].Key || got.Year != want[i].Year || got.Month != want[i].Month {
			t.Errorf("items[%d] = %+v, want %+v", i, got, want[i])
		}
	}
}

func TestListPayslipsErrors(t *testing.T) {
	t.Run("missing configuration", func(t *testing.T) {
		_, err := (&Client{}).ListPayslips(context.Background())
		if err == nil || !strings.Contains(err.Error(), "worker URL is not configured") {
			t.Errorf("empty BaseURL error = %v, want worker URL not configured", err)
		}

		_, err = (&Client{BaseURL: "https://example.com"}).ListPayslips(context.Background())
		if err == nil || !strings.Contains(err.Error(), "access token is not configured") {
			t.Errorf("empty Token error = %v, want access token not configured", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		_, err := (&Client{BaseURL: srv.URL, Token: "t"}).ListPayslips(context.Background())
		if err == nil || !strings.Contains(err.Error(), "unauthorized or worker missing /buste endpoint") {
			t.Errorf("404 error = %v, want unauthorized/worker missing message", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := (&Client{BaseURL: srv.URL, Token: "t"}).ListPayslips(context.Background())
		if err == nil || !strings.Contains(err.Error(), "worker returned 500: boom") {
			t.Errorf("500 error = %v, want worker returned 500: boom", err)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "not-json")
		}))
		defer srv.Close()

		_, err := (&Client{BaseURL: srv.URL, Token: "t"}).ListPayslips(context.Background())
		if err == nil || !strings.Contains(err.Error(), "invalid response from worker") {
			t.Errorf("bad JSON error = %v, want invalid response from worker", err)
		}
	})
}

func TestDownloadPayslip(t *testing.T) {
	pdf := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.set(r)
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(pdf)
	}))
	defer srv.Close()

	dest := t.TempDir()
	c := &Client{BaseURL: srv.URL, Token: "tok"}
	gotPath, err := c.DownloadPayslip(context.Background(), "busta-2026-11.pdf", dest)
	if err != nil {
		t.Fatalf("DownloadPayslip() error = %v", err)
	}

	wantPath := filepath.Join(dest, "busta-2026-11.pdf")
	if gotPath != wantPath {
		t.Errorf("DownloadPayslip() path = %q, want %q", gotPath, wantPath)
	}

	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Errorf("downloaded bytes = %q, want %q", got, pdf)
	}

	method, path, token, _ := rec.get()
	if method != http.MethodGet {
		t.Errorf("request method = %q, want %q", method, http.MethodGet)
	}
	if path != "/buste/busta-2026-11.pdf" {
		t.Errorf("request path = %q, want /buste/busta-2026-11.pdf", path)
	}
	if token != "tok" {
		t.Errorf("x-run-token = %q, want tok", token)
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("reading dest dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("dest dir has %d entries, want only the downloaded file: %v", len(entries), entries)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".busta-") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file in dest dir: %s", e.Name())
		}
	}
}

func TestDownloadPayslipRejectsNonPDF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "definitely not a pdf")
	}))
	defer srv.Close()

	dest := t.TempDir()
	_, err := (&Client{BaseURL: srv.URL, Token: "t"}).DownloadPayslip(context.Background(), "busta-2026-11.pdf", dest)
	if err == nil || !strings.Contains(err.Error(), "server response is not a valid PDF") {
		t.Errorf("non-PDF error = %v, want server response is not a valid PDF", err)
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("reading dest dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dest dir has %d entries after non-PDF response, want 0: %v", len(entries), entries)
	}
	if _, err := os.Stat(filepath.Join(dest, "busta-2026-11.pdf")); !os.IsNotExist(err) {
		t.Errorf("target file exists after non-PDF response (stat err = %v)", err)
	}
}

func TestDownloadPayslipInvalidKeys(t *testing.T) {
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.set(r)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "t"}
	for _, key := range []string{"../evil.pdf", "busta-2026-13.pdf", "busta-2026-00.pdf", ""} {
		dest := t.TempDir()
		_, err := c.DownloadPayslip(context.Background(), key, dest)
		if err == nil {
			t.Errorf("DownloadPayslip(%q) error = nil, want invalid key error", key)
		}
		entries, readErr := os.ReadDir(dest)
		if readErr != nil {
			t.Fatalf("reading dest dir for %q: %v", key, readErr)
		}
		if len(entries) != 0 {
			t.Errorf("dest dir for %q has %d entries, want 0", key, len(entries))
		}
	}

	if method, _, _, _ := rec.get(); method != "" {
		t.Errorf("server was hit for invalid key (method %q), want no request", method)
	}
}

func TestDownloadPayslipNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := (&Client{BaseURL: srv.URL, Token: "t"}).DownloadPayslip(context.Background(), "busta-2026-11.pdf", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "payslip busta-2026-11.pdf not found on server") {
		t.Errorf("404 error = %v, want payslip not found message", err)
	}
}

func TestDownloadPayslipEmptyDestDir(t *testing.T) {
	_, err := (&Client{}).DownloadPayslip(context.Background(), "busta-2026-11.pdf", "")
	if err == nil || !strings.Contains(err.Error(), "download folder is not configured") {
		t.Errorf("empty destDir error = %v, want download folder is not configured", err)
	}
}

func TestTriggerRun(t *testing.T) {
	t.Run("without date", func(t *testing.T) {
		rec := &recordedRequest{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.set(r)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"instanceId":"inst-1"}`)
		}))
		defer srv.Close()

		id, err := (&Client{BaseURL: srv.URL, Token: "tok"}).TriggerRun(context.Background(), "")
		if err != nil {
			t.Fatalf("TriggerRun() error = %v", err)
		}
		if id != "inst-1" {
			t.Errorf("TriggerRun() = %q, want inst-1", id)
		}

		method, path, token, rawQuery := rec.get()
		if method != http.MethodPost {
			t.Errorf("request method = %q, want %q", method, http.MethodPost)
		}
		if path != "/run" {
			t.Errorf("request path = %q, want /run", path)
		}
		if token != "tok" {
			t.Errorf("x-run-token = %q, want tok", token)
		}
		if rawQuery != "" {
			t.Errorf("raw query = %q, want empty", rawQuery)
		}
	})

	t.Run("with date", func(t *testing.T) {
		rec := &recordedRequest{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.set(r)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"instanceId":"inst-2"}`)
		}))
		defer srv.Close()

		id, err := (&Client{BaseURL: srv.URL, Token: "tok"}).TriggerRun(context.Background(), "2026-11-01")
		if err != nil {
			t.Fatalf("TriggerRun() error = %v", err)
		}
		if id != "inst-2" {
			t.Errorf("TriggerRun() = %q, want inst-2", id)
		}

		_, _, _, rawQuery := rec.get()
		if rawQuery != "date=2026-11-01" {
			t.Errorf("raw query = %q, want date=2026-11-01", rawQuery)
		}
	})

	t.Run("empty instance id", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"instanceId":""}`)
		}))
		defer srv.Close()

		_, err := (&Client{BaseURL: srv.URL, Token: "t"}).TriggerRun(context.Background(), "")
		if err == nil || !strings.Contains(err.Error(), "worker returned empty instanceId") {
			t.Errorf("empty instanceId error = %v, want worker returned empty instanceId", err)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad date", http.StatusBadRequest)
		}))
		defer srv.Close()

		_, err := (&Client{BaseURL: srv.URL, Token: "t"}).TriggerRun(context.Background(), "2026-11-01")
		if err == nil || !strings.Contains(err.Error(), "trigger returned 400: bad date") {
			t.Errorf("non-200 error = %v, want trigger returned 400: bad date", err)
		}
	})
}

func TestGetRunStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		rec := &recordedRequest{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.set(r)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"complete","error":null,"output":{"ok":true}}`)
		}))
		defer srv.Close()

		st, err := (&Client{BaseURL: srv.URL, Token: "tok"}).GetRunStatus(context.Background(), "instance-1")
		if err != nil {
			t.Fatalf("GetRunStatus() error = %v", err)
		}
		if st.Status != "complete" {
			t.Errorf("GetRunStatus().Status = %q, want complete", st.Status)
		}
		if !st.IsTerminal() {
			t.Errorf("GetRunStatus().IsTerminal() = false, want true")
		}

		method, path, token, _ := rec.get()
		if method != http.MethodGet {
			t.Errorf("request method = %q, want %q", method, http.MethodGet)
		}
		if path != "/status/instance-1" {
			t.Errorf("request path = %q, want /status/instance-1", path)
		}
		if token != "tok" {
			t.Errorf("x-run-token = %q, want tok", token)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "gone", http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := (&Client{BaseURL: srv.URL, Token: "t"}).GetRunStatus(context.Background(), "instance-1")
		if err == nil || !strings.Contains(err.Error(), "status returned 404: gone") {
			t.Errorf("non-200 error = %v, want status returned 404: gone", err)
		}
	})
}
