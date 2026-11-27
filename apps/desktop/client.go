package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

var bustaKeyPattern = regexp.MustCompile(`^busta-(\d{4})-(\d{2})\.pdf$`)

// Payslip represents one archived payslip PDF stored in buste-paga-cf's R2 bucket.
type Payslip struct {
	Key      string    `json:"key"`
	Year     int       `json:"year"`
	Month    int       `json:"month"`
	Size     int64     `json:"size"`
	Uploaded time.Time `json:"uploaded"`
	ETag     string    `json:"etag"`
}

// MonthName returns the English month label, e.g. "November".
func (p Payslip) MonthName() string {
	if p.Month >= 1 && p.Month <= 12 {
		return time.Month(p.Month).String()
	}
	return p.Key
}

// Title returns e.g. "November 2026".
func (p Payslip) Title() string {
	if p.Year > 0 && p.Month >= 1 && p.Month <= 12 {
		return fmt.Sprintf("%s %d", time.Month(p.Month).String(), p.Year)
	}
	return p.Key
}

// FormatSize returns a compact human-readable byte size (e.g. "142 KB").
func FormatSize(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	const kb = 1024
	const mb = 1024 * kb
	switch {
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%d KB", (n+kb/2)/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// LocalFileName is the name a payslip is saved under, YYYY-MM.pdf, from its
// worker key busta-YYYY-MM.pdf. It reports false for any other key.
func LocalFileName(key string) (string, bool) {
	m := bustaKeyPattern.FindStringSubmatch(key)
	if m == nil || !IsValidBustaKey(key) {
		return "", false
	}
	return m[1] + "-" + m[2] + ".pdf", true
}

// IsValidBustaKey checks that key strictly matches busta-YYYY-MM.pdf.
func IsValidBustaKey(key string) bool {
	m := bustaKeyPattern.FindStringSubmatch(key)
	if m == nil {
		return false
	}
	month, err := strconv.Atoi(m[2])
	return err == nil && month >= 1 && month <= 12
}

// RunStatus represents the response of GET /status/<instanceId>.
type RunStatus struct {
	Status string          `json:"status"`
	Error  json.RawMessage `json:"error,omitempty"`
	Output json.RawMessage `json:"output,omitempty"`
}

// IsTerminal reports whether the Cloudflare Workflow has reached a final state.
func (s RunStatus) IsTerminal() bool {
	switch s.Status {
	case "complete", "errored", "terminated":
		return true
	default:
		return false
	}
}

// Client talks to the buste-paga-cf Cloudflare Worker using x-run-token.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) newRequest(ctx context.Context, method, path string) (*http.Request, error) {
	base := normalizeWorkerURL(c.BaseURL)
	if base == "" {
		return nil, errors.New("worker URL is not configured")
	}
	if c.Token == "" {
		return nil, errors.New("access token is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-run-token", c.Token)
	req.Header.Set("User-Agent", "Zucchetti-Desktop/0.1")
	return req, nil
}

// ListPayslips fetches GET /buste and returns payslips ordered newest first.
func (c *Client) ListPayslips(ctx context.Context) ([]Payslip, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/buste")
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach worker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("unauthorized or worker missing /buste endpoint (check token & deploy)")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("worker returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var payload struct {
		Items []Payslip `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("invalid response from worker: %w", err)
	}

	items := make([]Payslip, 0, len(payload.Items))
	for _, it := range payload.Items {
		if !IsValidBustaKey(it.Key) {
			continue
		}
		if it.Year == 0 || it.Month == 0 {
			m := bustaKeyPattern.FindStringSubmatch(it.Key)
			it.Year, _ = strconv.Atoi(m[1])
			it.Month, _ = strconv.Atoi(m[2])
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Year != items[j].Year {
			return items[i].Year > items[j].Year
		}
		if items[i].Month != items[j].Month {
			return items[i].Month > items[j].Month
		}
		return items[i].Key > items[j].Key
	})
	return items, nil
}

// DownloadPayslip fetches GET /buste/<key>, verifies PDF magic bytes, and writes
// atomically to destDir/YYYY-MM.pdf.
func (c *Client) DownloadPayslip(ctx context.Context, key, destDir string) (string, error) {
	if !IsValidBustaKey(key) {
		return "", fmt.Errorf("invalid payslip key %q", key)
	}
	if destDir == "" {
		return "", errors.New("download folder is not configured")
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/buste/"+url.PathEscape(key))
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("payslip %s not found on server", key)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("download returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	// Cap at 32 MB for safety; payslips are typically 50–300 KB.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", fmt.Errorf("reading PDF body: %w", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", errors.New("server response is not a valid PDF")
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create download folder: %w", err)
	}
	name, _ := LocalFileName(key) // key was validated above
	target := filepath.Join(destDir, name)
	tmp, err := os.CreateTemp(destDir, ".busta-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("write PDF: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("close PDF: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("save PDF: %w", err)
	}
	return target, nil
}

// TriggerRun calls POST /run (optionally with ?date=YYYY-MM-DD) and returns the workflow instance ID.
func (c *Client) TriggerRun(ctx context.Context, date string) (string, error) {
	path := "/run"
	if date != "" {
		path += "?date=" + url.QueryEscape(date)
	}
	return c.postRun(ctx, path)
}

// TriggerRefetch asks the worker to scrape one month again and overwrite its
// R2 object, without the worker emailing the PDF.
func (c *Client) TriggerRefetch(ctx context.Context, year, month int) (string, error) {
	if year < 2000 || month < 1 || month > 12 {
		return "", fmt.Errorf("invalid month %d-%02d", year, month)
	}
	// The 15th keeps the UTC month stable in every time zone.
	return c.postRun(ctx, fmt.Sprintf("/run?date=%04d-%02d-15&notify=0", year, month))
}

// WaitForRun polls GET /status/<id> until the workflow reaches a final state
// or ctx ends. A failed poll is retried on the next tick. onStatus, if set,
// hears every status seen.
func (c *Client) WaitForRun(ctx context.Context, id string, every time.Duration, onStatus func(string)) (RunStatus, error) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return RunStatus{}, ctx.Err()
		case <-ticker.C:
			st, err := c.GetRunStatus(ctx, id)
			if err != nil {
				continue
			}
			if onStatus != nil {
				onStatus(st.Status)
			}
			if st.IsTerminal() {
				return st, nil
			}
		}
	}
}

func (c *Client) postRun(ctx context.Context, path string) (string, error) {
	req, err := c.newRequest(ctx, http.MethodPost, path)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("trigger failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("trigger returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var res struct {
		InstanceID string `json:"instanceId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("invalid trigger response: %w", err)
	}
	if res.InstanceID == "" {
		return "", errors.New("worker returned empty instanceId")
	}
	return res.InstanceID, nil
}

// GetRunStatus calls GET /status/<instanceId>.
func (c *Client) GetRunStatus(ctx context.Context, instanceID string) (RunStatus, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/status/"+url.PathEscape(instanceID))
	if err != nil {
		return RunStatus{}, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return RunStatus{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return RunStatus{}, fmt.Errorf("status returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var st RunStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return RunStatus{}, err
	}
	return st, nil
}
