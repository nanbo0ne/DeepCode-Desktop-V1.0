package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	liveProbeMacURL             = "https://orca.aichat.diy/releases/desktop-v3.0.3/O.R.C.A-macos-universal.dmg"
	liveProbeGitHubURL          = "https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.3/O.R.C.A-macos-universal.dmg"
	liveProbeExpectedSize int64 = 18_561_854
	liveProbeInitialPart  int64 = 1 << 20
	liveProbeCancelBytes  int64 = 2 << 20
	liveProbeTimeout            = 20 * time.Second
	liveProbeEvidenceEnv        = "ORCA_LIVE_UPDATER_EVIDENCE_DIR"
)

type liveUpdaterProbeReport struct {
	GeneratedAt       string                   `json:"generatedAt"`
	ExpectedAssetSize int64                    `json:"expectedAssetSize"`
	Measurement       string                   `json:"measurement"`
	Verification      string                   `json:"verification"`
	Results           []liveUpdaterProbeResult `json:"results"`
}

type liveUpdaterProbeResult struct {
	Source                 string `json:"source"`
	URL                    string `json:"url"`
	ExpectedBytes          int64  `json:"expectedBytes"`
	InitialPartialBytes    int64  `json:"initialPartialBytes"`
	SyntheticPrefix        string `json:"syntheticPrefix"`
	BytesReceivedThisRun   int64  `json:"bytesReceivedThisRun"`
	BytesOnDisk            int64  `json:"bytesOnDisk"`
	ResponseStatus         int    `json:"responseStatus"`
	ResponseContentLength  int64  `json:"responseContentLength"`
	ContentRange           string `json:"contentRange,omitempty"`
	RequestCount           int    `json:"requestCount"`
	RangeSent              string `json:"rangeSent,omitempty"`
	ResumeHandling         string `json:"resumeHandling"`
	StartedAt              string `json:"startedAt"`
	FirstByteAt            string `json:"firstByteAt,omitempty"`
	FirstByteMs            int64  `json:"firstByteMs,omitempty"`
	EndedAt                string `json:"endedAt"`
	DurationMs             int64  `json:"durationMs"`
	SpeedBPS               int64  `json:"speedBps"`
	CancelReason           string `json:"cancelReason"`
	Error                  string `json:"error,omitempty"`
	TransportProbe         bool   `json:"transportProbe"`
	FullVerification       bool   `json:"fullVerification"`
	ResumeContentsVerified bool   `json:"resumeContentsVerified"`
}

type liveProbeRoundTripper struct {
	base           http.RoundTripper
	mu             sync.Mutex
	requestCount   int
	rangeSent      string
	responseStatus int
	contentLength  int64
	contentRange   string
}

func (r *liveProbeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.requestCount++
	if r.rangeSent == "" {
		r.rangeSent = req.Header.Get("Range")
	}
	r.mu.Unlock()
	resp, err := r.base.RoundTrip(req)
	if resp == nil {
		return nil, err
	}
	r.mu.Lock()
	r.responseStatus = resp.StatusCode
	r.contentLength = resp.ContentLength
	r.contentRange = resp.Header.Get("Content-Range")
	r.mu.Unlock()
	return resp, err
}

func (r *liveProbeRoundTripper) snapshot() (int, string, int, int64, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requestCount, r.rangeSent, r.responseStatus, r.contentLength, r.contentRange
}

func TestLiveUpdaterTransferProbe(t *testing.T) {
	if os.Getenv("ORCA_RUN_LIVE_UPDATER_PROBE") != "1" {
		t.Skip("set ORCA_RUN_LIVE_UPDATER_PROBE=1 to run the bounded live updater probe")
	}

	client, err := httpClient()
	if err != nil {
		t.Fatalf("real desktop httpClient: %v", err)
	}
	results := []liveUpdaterProbeResult{
		runLiveUpdaterTransferProbe(t, client, "mac", liveProbeMacURL),
		runLiveUpdaterTransferProbe(t, client, "github", liveProbeGitHubURL),
	}
	report := liveUpdaterProbeReport{
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		ExpectedAssetSize: liveProbeExpectedSize,
		Measurement:       "bytesReceivedThisRun is measured network bytes; the first 1 MiB on disk is a synthetic zero-filled prefix, so the 2 MiB cancellation bound is file size, not network bytes",
		Verification:      "not_run: Range/transport probe only; synthetic resume contents were not verified; no digest/signature or installer verification",
		Results:           results,
	}
	saveLiveUpdaterProbeEvidence(t, report)
	for _, result := range results {
		t.Logf("live updater %s: status=%d requests=%d range=%q bytes=%d disk=%d firstByteMs=%d speedBPS=%d cancel=%s error=%q resume=%s verification=%v",
			result.Source, result.ResponseStatus, result.RequestCount, result.RangeSent,
			result.BytesReceivedThisRun, result.BytesOnDisk, result.FirstByteMs,
			result.SpeedBPS, result.CancelReason, result.Error, result.ResumeHandling, result.FullVerification)
	}
}

func runLiveUpdaterTransferProbe(t *testing.T, baseClient *http.Client, source, address string) liveUpdaterProbeResult {
	t.Helper()
	started := time.Now()
	result := liveUpdaterProbeResult{
		Source: source, URL: address, ExpectedBytes: liveProbeExpectedSize,
		InitialPartialBytes: liveProbeInitialPart,
		SyntheticPrefix:     "first 1 MiB are synthetic zero bytes, not network data",
		StartedAt:           started.UTC().Format(time.RFC3339Nano), TransportProbe: true,
		FullVerification: false, ResumeContentsVerified: false,
	}
	dir := t.TempDir()
	if !filepath.IsAbs(dir) {
		t.Fatalf("temporary probe directory is not absolute: %q", dir)
	}
	part := filepath.Join(dir, "payload.part")
	f, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("create partial payload: %v", err)
	}
	if err := f.Truncate(liveProbeInitialPart); err != nil {
		_ = f.Close()
		t.Fatalf("seed partial payload: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close partial payload: %v", err)
	}

	transport := baseClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	recorder := &liveProbeRoundTripper{base: transport}
	probeClient := *baseClient
	probeClient.Transport = recorder
	ctx, cancel := context.WithTimeout(context.Background(), liveProbeTimeout)
	defer cancel()
	var last packageTransferStats
	var firstByte time.Time
	cancelReason := ""
	err = transferPackageWithStats(ctx, &probeClient, address, part, liveProbeExpectedSize, func(stats packageTransferStats) {
		last = stats
		if firstByte.IsZero() && stats.Received > liveProbeInitialPart {
			firstByte = time.Now()
		}
		if cancelReason == "" && stats.Received >= liveProbeCancelBytes {
			cancelReason = "2MiB file size (1MiB synthetic prefix)"
			cancel()
		}
	})
	ended := time.Now()
	result.EndedAt = ended.UTC().Format(time.RFC3339Nano)
	result.DurationMs = ended.Sub(started).Milliseconds()
	result.BytesReceivedThisRun = maxInt64(0, last.Received-liveProbeInitialPart)
	result.SpeedBPS = last.SpeedBPS
	if !firstByte.IsZero() {
		result.FirstByteAt = firstByte.UTC().Format(time.RFC3339Nano)
		result.FirstByteMs = firstByte.Sub(started).Milliseconds()
	}
	if stat, statErr := os.Stat(part); statErr == nil {
		result.BytesOnDisk = stat.Size()
	} else {
		result.Error = "partial file stat: " + statErr.Error()
	}
	if cancelReason == "" && result.BytesOnDisk >= liveProbeCancelBytes {
		cancelReason = "2MiB file size (1MiB synthetic prefix)"
	}
	result.RequestCount, result.RangeSent, result.ResponseStatus, result.ResponseContentLength, result.ContentRange = recorder.snapshot()
	if result.RangeSent == "" {
		result.ResumeHandling = "no-range-request"
	} else if result.ResponseStatus == http.StatusPartialContent {
		result.ResumeHandling = "range-request-accepted"
	} else if result.ResponseStatus == http.StatusOK {
		result.ResumeHandling = "range-request-sent-but-server-restarted-from-zero"
	} else {
		result.ResumeHandling = "range-request-sent-response-not-complete"
	}
	if cancelReason == "" {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			cancelReason = "20s deadline"
		case errors.Is(ctx.Err(), context.Canceled):
			cancelReason = "context canceled"
		default:
			cancelReason = "transport returned before bound"
		}
	}
	result.CancelReason = cancelReason
	if err != nil {
		result.Error = sanitizeLiveProbeError(err)
	}
	return result
}

func saveLiveUpdaterProbeEvidence(t *testing.T, report liveUpdaterProbeReport) {
	t.Helper()
	evidenceDir := strings.TrimSpace(os.Getenv(liveProbeEvidenceEnv))
	if evidenceDir == "" {
		evidenceDir = t.TempDir()
	} else if !filepath.IsAbs(evidenceDir) {
		t.Fatalf("%s must be absolute when provided: %q", liveProbeEvidenceEnv, evidenceDir)
	}
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatalf("create evidence directory: %v", err)
	}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal probe report: %v", err)
	}
	jsonPath := filepath.Join(evidenceDir, "live-updater-probe.json")
	if !filepath.IsAbs(jsonPath) {
		t.Fatalf("JSON evidence path is not absolute: %q", jsonPath)
	}
	if err := os.WriteFile(jsonPath, append(body, '\n'), 0o600); err != nil {
		t.Fatalf("write JSON evidence: %v", err)
	}
	var reportText strings.Builder
	fmt.Fprintf(&reportText, "# Bounded Live Updater Probe\n\nGenerated: `%s`\n\nMeasurement: %s\n\nVerification: %s\n\n", report.GeneratedAt, report.Measurement, report.Verification)
	for _, result := range report.Results {
		fmt.Fprintf(&reportText, "## %s\n\n- URL: `%s`\n- Expected bytes: `%d`\n- Seeded prefix: %s\n- Bytes received from network this run: `%d`\n- Bytes on disk (synthetic prefix + network bytes): `%d`\n- Response: `%d`\n- First byte: `%d ms`\n- Duration: `%d ms`\n- Speed: `%d B/s`\n- Cancel: `%s`\n- Resume transport: %s\n- Resume contents verified: `%t`\n- Error: `%s`\n- Full verification: `%t`\n\n", result.Source, result.URL, result.ExpectedBytes, result.SyntheticPrefix, result.BytesReceivedThisRun, result.BytesOnDisk, result.ResponseStatus, result.FirstByteMs, result.DurationMs, result.SpeedBPS, result.CancelReason, result.ResumeHandling, result.ResumeContentsVerified, result.Error, result.FullVerification)
	}
	mdPath := filepath.Join(evidenceDir, "live-updater-probe.md")
	if !filepath.IsAbs(mdPath) {
		t.Fatalf("Markdown evidence path is not absolute: %q", mdPath)
	}
	if err := os.WriteFile(mdPath, []byte(reportText.String()), 0o600); err != nil {
		t.Fatalf("write Markdown evidence: %v", err)
	}
}

func sanitizeLiveProbeError(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
