package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/update"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"dev", "", false},
		{"", "", false},
		{"  ", "", false},
		{"1.2.3", "v1.2.3", true},
		{"v1.2.3", "v1.2.3", true},
		{"v1.2", "v1.2.0", true}, // semver.Canonical fills the patch
		{"garbage", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeVersion(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("normalizeVersion(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSelectLatestDesktopRelease(t *testing.T) {
	releases := []githubRelease{
		{TagName: "v9.0.0", HTMLURL: "https://example.invalid/cli"},
		{TagName: "npm-v8.0.0", HTMLURL: "https://example.invalid/npm"},
		{TagName: "desktop-v2.0.25", HTMLURL: "https://example.invalid/25"},
		{TagName: "desktop-v2.0.27", HTMLURL: "https://example.invalid/draft", Draft: true},
		{TagName: "desktop-v2.0.28", HTMLURL: "https://example.invalid/pre", Prerelease: true},
		{TagName: "desktop-v2.0.26", HTMLURL: "https://example.invalid/26"},
	}
	got, ok := selectLatestDesktopRelease(releases)
	if !ok || got.TagName != "desktop-v2.0.26" {
		t.Fatalf("selectLatestDesktopRelease = (%+v,%v), want desktop-v2.0.26", got, ok)
	}
}

func TestEvaluateGitHubReleaseIsNotificationOnly(t *testing.T) {
	info := evaluateGitHubRelease("2.0.25", githubRelease{
		TagName: "desktop-v2.0.26",
		HTMLURL: "https://github.com/nanbo0ne/DeepSeek-Orca/releases/tag/desktop-v2.0.26",
		Body:    "notes",
	})
	if !info.Available || info.Latest != "2.0.26" {
		t.Fatalf("evaluateGitHubRelease = %+v", info)
	}
	if info.CanSelfUpdate {
		t.Fatal("desktop update notification must never enable in-app installation")
	}
	if !strings.Contains(info.DownloadURL, "desktop-v2.0.26") {
		t.Fatalf("DownloadURL = %q, want the matching official release", info.DownloadURL)
	}
}

func TestFetchLatestDesktopReleaseAtHandlesHTTPResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tag_name":"v9.0.0"},{"tag_name":"desktop-v2.0.26","html_url":"https://example.invalid/26"}]`))
	}))
	defer server.Close()

	release, err := fetchLatestDesktopReleaseAt(context.Background(), server.Client(), server.URL)
	if err != nil || release.TagName != "desktop-v2.0.26" {
		t.Fatalf("fetchLatestDesktopReleaseAt = (%+v,%v)", release, err)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	failingURL := failing.URL
	failing.Close()
	if _, err := fetchLatestDesktopReleaseAt(context.Background(), failing.Client(), failingURL); err == nil {
		t.Fatal("network failure should be reported")
	}
}

func TestEvaluate(t *testing.T) {
	mk := func(version string) *update.Manifest {
		return &update.Manifest{
			Version:   version,
			Notes:     "notes",
			Platforms: map[string]update.Asset{update.CurrentPlatform(): {Size: 999}, "windows-amd64-portable": {Size: 999}},
		}
	}

	if got := evaluate("v1.0.0", mk("v1.1.0")); !got.Available {
		t.Error("v1.0.0 -> v1.1.0 should be available")
	}
	if got := evaluate("v1.1.0", mk("v1.1.0")); got.Available {
		t.Error("same version should not be available")
	}
	if got := evaluate("v1.2.0", mk("v1.1.0")); got.Available {
		t.Error("newer-than-manifest should not be available")
	}
	// A dev build must never auto-prompt, even against a real release.
	if got := evaluate("dev", mk("v1.1.0")); got.Available {
		t.Error("dev build should not prompt to update")
	}
	// An invalid manifest version is a check error, not an update.
	got := evaluate("v1.0.0", mk("not-a-version"))
	if got.Available || got.Err == "" {
		t.Errorf("invalid manifest version: got %+v", got)
	}
	// Metadata carries through.
	full := evaluate("v1.0.0", mk("v1.1.0"))
	if full.Latest != "v1.1.0" || full.Notes != "notes" || full.AssetSize != 999 {
		t.Errorf("metadata not carried: %+v", full)
	}
	if full.CanSelfUpdate != isInstalledDesktop() {
		t.Errorf("CanSelfUpdate = %v, want registered installation status", full.CanSelfUpdate)
	}
}

func TestChannelSelectsDistinctPointers(t *testing.T) {
	orig := channel
	t.Cleanup(func() { channel = orig })

	channel = "stable"
	stable := manifestEndpoints()
	channel = "canary"
	canary := manifestEndpoints()

	for _, u := range stable {
		if strings.Contains(u, "canary") {
			t.Errorf("stable endpoint leaks into canary: %q", u)
		}
	}
	if stable[0] != "https://orca.aichat.diy/updates/stable/latest.json" {
		t.Errorf("stable primary = %q, want the latest/ pointer", stable[0])
	}
	for _, u := range canary {
		if strings.Contains(u, "/latest/") {
			t.Errorf("canary endpoint hits the stable latest/ pointer: %q", u)
		}
	}
	if !strings.Contains(canary[0], "/canary/latest.json") {
		t.Errorf("canary primary = %q, want the canary/ pointer", canary[0])
	}
	if downloadPage() == (ghReleasesBase + "/latest") {
		t.Error("canary download page should not be the stable latest releases page")
	}
}

func TestCheckSHA256(t *testing.T) {
	data := []byte("hello world")
	// echo -n "hello world" | shasum -a 256
	const sum = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if err := checkSHA256(data, sum); err != nil {
		t.Errorf("matching digest should pass: %v", err)
	}
	if err := checkSHA256(data, "deadbeef"); err == nil {
		t.Error("mismatched digest should fail")
	}
	// Case-insensitive hex.
	if err := checkSHA256(data, "B94D27B9934D3E08A52E52D7DA7DABFAC484EFE37A5380EE9088F7ACE2EFCDE9"); err != nil {
		t.Errorf("uppercase digest should pass: %v", err)
	}
}

func TestExtractBinary(t *testing.T) {
	want := []byte("#!/bin/sh\necho deepseek-orca\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string][]byte{"README": []byte("ignore me"), "deepseek-orca-desktop": want}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()

	got, err := extractBinary(buf.Bytes(), "deepseek-orca-desktop")
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted %q, want %q", got, want)
	}
	if _, err := extractBinary(buf.Bytes(), "missing"); err == nil {
		t.Error("missing entry should error")
	}
}
