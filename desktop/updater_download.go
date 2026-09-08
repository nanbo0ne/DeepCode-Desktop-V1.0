package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/update"
)

const maxUpdatePackageSize int64 = 2 << 30

func fetchLimited(ctx context.Context, c *http.Client, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Orca/"+version)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update HTTP %d", resp.StatusCode)
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return nil, errors.New("update endpoint returned a web page")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("update response is too large")
	}
	return b, nil
}

func fetchSignedManifestAt(ctx context.Context, c *http.Client, address string, verify func([]byte, []byte) error) (*update.Manifest, error) {
	body, err := fetchLimited(ctx, c, address, 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := fetchLimited(ctx, c, address+".minisig", 8192)
	if err != nil {
		return nil, err
	}
	if err = verify(body, sig); err != nil {
		return nil, err
	}
	var manifest update.Manifest
	if err = json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("invalid update manifest: %w", err)
	}
	if _, ok := normalizeVersion(manifest.Version); !ok {
		return nil, errors.New("invalid update version")
	}
	asset, ok := manifest.Asset()
	if !ok {
		return nil, errors.New("no update package for this platform")
	}
	if err = validateUpdateAsset(manifest.Version, asset); err != nil {
		return nil, err
	}
	for _, candidate := range manifest.Platforms {
		if err = validateUpdateAsset(manifest.Version, candidate); err != nil {
			return nil, err
		}
	}
	manifest.Source = "github"
	if strings.HasPrefix(address, macUpdateBase+"/") {
		manifest.Source = "mac"
	}
	return &manifest, nil
}

func validateUpdateAsset(ver string, asset update.Asset) error {
	if asset.Size <= 0 || asset.Size > maxUpdatePackageSize {
		return errors.New("invalid update package size")
	}
	sum, err := hex.DecodeString(asset.SHA256)
	if err != nil || len(sum) != sha256.Size {
		return errors.New("invalid update digest")
	}
	tag := "desktop-v" + strings.TrimPrefix(ver, "v")
	for _, address := range []string{asset.URL, asset.Sig, asset.FallbackURL, asset.FallbackSig} {
		if address == "" {
			continue
		}
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("invalid update URL")
		}
		prefix := "/releases/" + tag + "/"
		if u.Host == "github.com" {
			prefix = "/nanbo0ne/O.R.C.A-for-Windows/releases/download/" + tag + "/"
		} else if u.Host != "orca.aichat.diy" {
			return errors.New("untrusted update host")
		}
		if !strings.HasPrefix(u.Path, prefix) || path.Base(u.Path) == "." || strings.Contains(strings.TrimPrefix(u.Path, prefix), "/") || strings.Contains(u.Path, "..") {
			return errors.New("update URL does not match release version")
		}
	}
	if asset.URL == "" || asset.Sig != asset.URL+".minisig" {
		return errors.New("missing update signature URL")
	}
	if (asset.FallbackURL == "") != (asset.FallbackSig == "") || (asset.FallbackURL != "" && asset.FallbackSig != asset.FallbackURL+".minisig") {
		return errors.New("invalid fallback signature URL")
	}
	if asset.FallbackURL != "" {
		u, _ := url.Parse(asset.URL)
		f, _ := url.Parse(asset.FallbackURL)
		if path.Base(u.Path) != path.Base(f.Path) {
			return errors.New("fallback package differs from primary")
		}
	}
	return nil
}

type packageSource struct {
	name string
	url  string
	sig  string
}

type packageTransferStats struct {
	Received         int64
	Total            int64
	SpeedBPS         int64
	ETASeconds       int64
	SuggestAlternate bool
}

// downloadPackage keeps the original test/helper contract. The updater app uses
// downloadPackageWithSource so it can report which mirror is active and expose
// transfer measurements without adding a separate speed probe.
func downloadPackage(ctx context.Context, c *http.Client, asset update.Asset, dir string, progress func(string, int64, int64), verify func(io.Reader, []byte) error) (string, error) {
	return downloadPackageWithSource(ctx, c, asset, dir, "", func(_ string, phase string, stats packageTransferStats) {
		if progress != nil {
			progress(phase, stats.Received, stats.Total)
		}
	}, verify)
}

func downloadPackageWithSource(ctx context.Context, c *http.Client, asset update.Asset, dir, preferredSource string, progress func(string, string, packageTransferStats), verify func(io.Reader, []byte) error) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	part := filepath.Join(dir, "payload.part")
	var lastErr error
	for _, source := range orderedPackageSources(asset, preferredSource) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		lastErr = transferPackageWithStats(ctx, c, source.url, part, asset.Size, func(stats packageTransferStats) {
			if progress != nil {
				progress(source.name, "downloading", stats)
			}
		})
		if lastErr != nil {
			continue
		}
		if progress != nil {
			progress(source.name, "verifying", packageTransferStats{Received: asset.Size, Total: asset.Size})
		}
		sigCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		sig, err := fetchLimited(sigCtx, c, source.sig, 8192)
		cancel()
		if err == nil {
			err = verifyPackage(part, asset, sig, verify)
		}
		if err != nil {
			lastErr = err
			_ = os.Remove(part)
			continue
		}
		u, err := url.Parse(asset.URL)
		if err != nil {
			return "", err
		}
		name := filepath.Base(u.Path)
		if name == "." || name == "" || strings.ContainsAny(name, `\:`) {
			return "", errors.New("invalid package filename")
		}
		target := filepath.Join(dir, name)
		if err := os.Rename(part, target); err != nil {
			return "", err
		}
		if err := os.WriteFile(target+".minisig", sig, 0o600); err != nil {
			return "", err
		}
		return target, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "", fmt.Errorf("update download failed: %w", lastErr)
}

func orderedPackageSources(asset update.Asset, preferred string) []packageSource {
	all := []packageSource{
		{name: updateSourceName(asset.URL, updateSourceMac), url: asset.URL, sig: asset.Sig},
		{name: updateSourceName(asset.FallbackURL, updateSourceGitHub), url: asset.FallbackURL, sig: asset.FallbackSig},
	}
	ordered := make([]packageSource, 0, len(all))
	for _, source := range all {
		if source.url != "" && source.name == preferred {
			ordered = append(ordered, source)
		}
	}
	for _, source := range all {
		if source.url == "" || source.name == preferred {
			continue
		}
		ordered = append(ordered, source)
	}
	return ordered
}

func updateSourceName(address, fallback string) string {
	u, err := url.Parse(address)
	if err == nil {
		switch u.Hostname() {
		case "github.com":
			return updateSourceGitHub
		case "orca.aichat.diy":
			return updateSourceMac
		}
	}
	// Local test transports use the manifest slot; production URLs are validated.
	return fallback
}

func transferPackage(parent context.Context, c *http.Client, address, filename string, size int64, progress func(int64)) error {
	return transferPackageWithStats(parent, c, address, filename, size, func(stats packageTransferStats) {
		if progress != nil {
			progress(stats.Received)
		}
	})
}

const (
	lowThroughputThreshold = int64(128 << 10)
	lowThroughputDuration  = 30 * time.Second
)

type transferSpeedTracker struct {
	startedAt        time.Time
	initial          int64
	lowSince         time.Time
	suggestAlternate bool
}

func (t *transferSpeedTracker) stats(received, total int64) packageTransferStats {
	stats := packageTransferStats{Received: received, Total: total}
	elapsed := time.Since(t.startedAt)
	transferred := received - t.initial
	if elapsed > 0 && transferred > 0 {
		speed := float64(transferred) / elapsed.Seconds()
		stats.SpeedBPS = int64(speed)
		remaining := total - received
		if remaining > 0 && speed > 0 {
			stats.ETASeconds = int64(float64(remaining) / speed)
			if float64(stats.ETASeconds)*speed < float64(remaining) {
				stats.ETASeconds++
			}
		}
		if speed < float64(lowThroughputThreshold) {
			if t.lowSince.IsZero() {
				t.lowSince = time.Now()
			}
			if time.Since(t.lowSince) >= lowThroughputDuration {
				t.suggestAlternate = true
			}
		} else {
			t.lowSince = time.Time{}
			t.suggestAlternate = false
		}
	}
	stats.SuggestAlternate = t.suggestAlternate
	return stats
}

func transferPackageWithStats(parent context.Context, c *http.Client, address, filename string, size int64, progress func(packageTransferStats)) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// Reset on every received chunk, rather than imposing a short limit on a large download.
	watchdog := time.AfterFunc(20*time.Second, cancel)
	defer watchdog.Stop()
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	offset := stat.Size()
	if offset > size {
		if err := f.Truncate(0); err != nil {
			return err
		}
		offset = 0
	}
	if offset == size {
		return nil
	}
	tracker := transferSpeedTracker{startedAt: time.Now(), initial: offset}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		if err := f.Truncate(0); err != nil {
			return err
		}
		offset = 0
	case http.StatusPartialContent:
		var start, end, total int64
		if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil || start != offset || total != size || end != size-1 {
			return errors.New("invalid resume range")
		}
	default:
		return fmt.Errorf("update download HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != size-offset {
		return errors.New("update package size changed")
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	if progress != nil {
		progress(tracker.stats(offset, size))
	}
	buf := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(20 * time.Second)
			if offset+int64(n) > size {
				return errors.New("update package exceeds declared size")
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			offset += int64(n)
			if progress != nil {
				progress(tracker.stats(offset, size))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if offset != size {
		return io.ErrUnexpectedEOF
	}
	return f.Sync()
}

func verifyPackage(filename string, asset update.Asset, sig []byte, verify func(io.Reader, []byte) error) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return verifyPackageContents(f, asset, sig, verify)
}

func verifyPackageContents(f io.ReadSeeker, asset update.Asset, sig []byte, verify func(io.Reader, []byte) error) error {
	hash := sha256.New()
	n, err := io.Copy(hash, f)
	if err != nil {
		return err
	}
	if n != asset.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), asset.SHA256) {
		return errors.New("update package digest or size mismatch")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return verify(f, sig)
}
