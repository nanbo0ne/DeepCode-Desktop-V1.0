package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/update"
)

func packageFixture(t *testing.T, data []byte) ([]byte, func(io.Reader, []byte) error) {
	t.Helper()
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	r := minisign.NewReader(bytes.NewReader(data))
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatal(err)
	}
	sig := r.Sign(priv)
	return sig, func(input io.Reader, sig []byte) error {
		r := minisign.NewReader(input)
		if _, err := io.Copy(io.Discard, r); err != nil {
			return err
		}
		if !r.Verify(pub, sig) {
			return errors.New("bad signature")
		}
		return nil
	}
}

func fixtureAsset(data []byte, address string) update.Asset {
	sum := sha256.Sum256(data)
	return update.Asset{URL: address, Sig: address + ".minisig", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func TestSignedManifestRejectsHTMLTamperingAndWrongPlatform(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	asset := fixtureAsset([]byte("test"), "https://orca.aichat.diy/releases/desktop-v3.0.3/test.exe")
	valid := update.Manifest{Version: "3.0.3", Platforms: map[string]update.Asset{update.CurrentPlatform(): asset}}
	for _, kind := range []string{"valid", "html", "bad-signature", "wrong-platform", "bad-url", "old-url"} {
		t.Run(kind, func(t *testing.T) {
			m := valid
			m.Platforms = map[string]update.Asset{update.CurrentPlatform(): asset}
			if kind == "wrong-platform" {
				m.Platforms = map[string]update.Asset{"other": asset}
			}
			if kind == "bad-url" {
				a := asset
				a.URL = "https://evil.invalid/test.exe"
				m.Platforms[update.CurrentPlatform()] = a
			}
			if kind == "old-url" {
				a := asset
				a.URL = strings.Replace(a.URL, "3.0.3", "3.0.2", 1)
				a.Sig = a.URL + ".minisig"
				m.Platforms[update.CurrentPlatform()] = a
			}
			body, _ := json.Marshal(m)
			sig := minisign.Sign(priv, body)
			if kind == "bad-signature" {
				body = append(body, ' ')
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".minisig") {
					w.Write(sig)
					return
				}
				if kind == "html" {
					w.Header().Set("Content-Type", "text/html")
					w.Write([]byte("<html>home</html>"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write(body)
			}))
			defer server.Close()
			_, err := fetchSignedManifestAt(context.Background(), server.Client(), server.URL+"/latest.json", func(data, sig []byte) error {
				if !minisign.Verify(pub, data, sig) {
					return errors.New("bad signature")
				}
				return nil
			})
			if (err == nil) != (kind == "valid") {
				t.Fatalf("kind=%s error=%v", kind, err)
			}
		})
	}
}

func TestPackageResumeAndNoRangeRestart(t *testing.T) {
	data := bytes.Repeat([]byte("orca-test-content"), 4096)
	sig, verify := packageFixture(t, data)
	for _, resume := range []bool{true, false} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) {
			var rangeSeen bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".minisig") {
					w.Write(sig)
					return
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				start := 0
				if r.Header.Get("Range") != "" {
					rangeSeen = true
					if resume {
						fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &start)
						w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
						w.WriteHeader(206)
					}
				}
				w.Write(data[start:])
			}))
			defer server.Close()
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "payload.part"), data[:1234], 0o600)
			filename, err := downloadPackage(context.Background(), server.Client(), fixtureAsset(data, server.URL+"/setup.exe"), dir, func(string, int64, int64) {}, verify)
			if err != nil {
				t.Fatal(err)
			}
			if !rangeSeen {
				t.Fatal("no resume request")
			}
			got, err := os.ReadFile(filename)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("corrupted package: %v", err)
			}
		})
	}
}

func TestPackageFallbackAndVerificationFailure(t *testing.T) {
	data := []byte("valid installer fixture")
	sig, verify := packageFixture(t, data)
	for _, kind := range []string{"fallback", "hash", "signature"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/primary/") && kind == "fallback" {
					w.WriteHeader(503)
					return
				}
				if strings.HasSuffix(r.URL.Path, ".minisig") {
					if kind == "signature" {
						w.Write([]byte("invalid"))
					} else {
						w.Write(sig)
					}
					return
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				if kind == "hash" {
					w.Write(bytes.Repeat([]byte("x"), len(data)))
				} else {
					w.Write(data)
				}
			}))
			defer server.Close()
			asset := fixtureAsset(data, server.URL+"/primary/setup.exe")
			asset.FallbackURL = server.URL + "/fallback/setup.exe"
			asset.FallbackSig = asset.FallbackURL + ".minisig"
			dir := t.TempDir()
			filename, err := downloadPackage(context.Background(), server.Client(), asset, dir, func(string, int64, int64) {}, verify)
			if kind == "fallback" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || filename != "" {
				t.Fatal("unverified package promoted")
			}
			if _, err := os.Stat(filepath.Join(dir, "setup.exe")); !os.IsNotExist(err) {
				t.Fatal("invalid installer exists")
			}
		})
	}
}

func TestPackageSourceSelectionAndCrossSourceResume(t *testing.T) {
	data := bytes.Repeat([]byte("cross-source-resume"), 512)
	sig, verify := packageFixture(t, data)

	t.Run("preferred fallback", func(t *testing.T) {
		var requests []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests = append(requests, r.URL.Path)
			if strings.HasSuffix(r.URL.Path, ".minisig") {
				_, _ = w.Write(sig)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(data)
		}))
		defer server.Close()

		asset := fixtureAsset(data, server.URL+"/primary/setup.exe")
		asset.FallbackURL = server.URL + "/fallback/setup.exe"
		asset.FallbackSig = asset.FallbackURL + ".minisig"
		if _, err := downloadPackageWithSource(context.Background(), server.Client(), asset, t.TempDir(), updateSourceGitHub, nil, verify); err != nil {
			t.Fatal(err)
		}
		if len(requests) == 0 || requests[0] != "/fallback/setup.exe" {
			t.Fatalf("requests = %v, want fallback source first", requests)
		}
	})

	t.Run("resume after primary failure", func(t *testing.T) {
		const firstChunk = 777
		var requests []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests = append(requests, r.URL.Path+" "+r.Header.Get("Range"))
			if strings.HasSuffix(r.URL.Path, ".minisig") {
				_, _ = w.Write(sig)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			if r.URL.Path == "/primary/setup.exe" {
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				_, _ = w.Write(data[:firstChunk])
				return
			}
			var start int
			if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &start); err != nil || start != firstChunk {
				http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[start:])
		}))
		defer server.Close()

		asset := fixtureAsset(data, server.URL+"/primary/setup.exe")
		asset.FallbackURL = server.URL + "/fallback/setup.exe"
		asset.FallbackSig = asset.FallbackURL + ".minisig"
		filename, err := downloadPackageWithSource(context.Background(), server.Client(), asset, t.TempDir(), updateSourceMac, nil, verify)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filename)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("cross-source payload mismatch: %v", err)
		}
		if len(requests) < 2 || !strings.Contains(requests[1], "/fallback/setup.exe bytes=777-") {
			t.Fatalf("requests = %v, want fallback resume", requests)
		}
	})
}

func TestTransferStatsReportETAAndSustainedLowThroughput(t *testing.T) {
	now := time.Now()
	tracker := transferSpeedTracker{
		startedAt: now.Add(-40 * time.Second),
		initial:   0,
		lowSince:  now.Add(-31 * time.Second),
	}
	stats := tracker.stats(1024, 10<<10)
	if stats.SpeedBPS <= 0 || stats.ETASeconds <= 0 {
		t.Fatalf("stats = %+v, want speed and ETA", stats)
	}
	if !stats.SuggestAlternate {
		t.Fatalf("stats = %+v, want sustained-low-throughput suggestion", stats)
	}
}

func TestTransferStatsPreserveProgressOnNetworkFailure(t *testing.T) {
	data := bytes.Repeat([]byte("partial"), 256)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data[:len(data)/2])
	}))
	defer server.Close()

	var last packageTransferStats
	err := transferPackageWithStats(context.Background(), server.Client(), server.URL, filepath.Join(t.TempDir(), "payload.part"), int64(len(data)), func(stats packageTransferStats) {
		last = stats
	})
	if err == nil {
		t.Fatal("truncated response should fail")
	}
	if last.Received == 0 || last.Total != int64(len(data)) {
		t.Fatalf("last stats = %+v, want received bytes and total after failure", last)
	}
}

func TestSetUpdateSourceChangesRuntimeSelectionOnly(t *testing.T) {
	asset := update.Asset{
		URL:         "https://orca.aichat.diy/releases/desktop-v3.0.4/setup.exe",
		FallbackURL: "https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.4/setup.exe",
	}
	a := &App{updatePending: &pendingDesktopUpdate{manifest: update.Manifest{
		Platforms: map[string]update.Asset{
			update.CurrentPlatform():               asset,
			update.CurrentPlatform() + "-portable": asset,
		},
	}}}
	if err := a.SetUpdateSource(updateSourceGitHub); err != nil {
		t.Fatal(err)
	}
	if a.updatePending.source != updateSourceGitHub || a.GetUpdateStatus().Source != updateSourceGitHub {
		t.Fatalf("source selection = pending=%q status=%q", a.updatePending.source, a.GetUpdateStatus().Source)
	}
}

func TestUpdateSourcesUseHostNotManifestPosition(t *testing.T) {
	asset := update.Asset{URL: "https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.4/setup.exe"}
	sources := updateAssetSources(asset)
	if len(sources) != 1 || sources[0] != updateSourceGitHub {
		t.Fatalf("GitHub-only manifest mislabeled: %v", sources)
	}
	asset.FallbackURL = "https://orca.aichat.diy/releases/desktop-v3.0.4/setup.exe"
	ordered := orderedPackageSources(asset, updateSourceMac)
	if len(ordered) != 2 || ordered[0].url != asset.FallbackURL || ordered[0].name != updateSourceMac {
		t.Fatalf("mirror selection ignored actual hosts: %+v", ordered)
	}
}

func TestPackageCancellationAndSizeChange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := transferPackage(ctx, http.DefaultClient, "https://unused.invalid", filepath.Join(t.TempDir(), "part"), 123, func(int64) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("wrong size")) }))
	defer server.Close()
	if err := transferPackage(context.Background(), server.Client(), server.URL, filepath.Join(t.TempDir(), "part"), 123, func(int64) {}); err == nil {
		t.Fatal("size mismatch accepted")
	}
}

func TestUpdateAPIsCannotInstallWithoutVerifiedDownload(t *testing.T) {
	a := &App{}
	if got := a.GetUpdateStatus(); got.Phase != "idle" {
		t.Fatalf("status=%+v", got)
	}
	if err := a.DownloadUpdate(); err == nil {
		t.Fatal("download without manifest allowed")
	}
	if _, err := a.verifiedUpdate(); err == nil {
		t.Fatal("unverified update allowed")
	}
	if err := a.ApplyUpdate(); err == nil {
		t.Fatal("unverified install allowed")
	}
	if err := a.OpenDownloadedUpdate(); err == nil {
		t.Fatal("unverified package opened")
	}
}
