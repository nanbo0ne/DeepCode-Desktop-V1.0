package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
	"golang.org/x/mod/semver"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/update"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/netclient"
)

// updater.go is the transport-free core of the desktop auto-updater: manifest
// fetch, version comparison, signed download, and per-platform apply/relaunch. It
// has no Wails dependency so the logic is unit-tested directly; updater_app.go is
// the thin Wails binding that wires these into App methods and progress events.

// Stable and canary use separate signed pointers. GitHub mirrors stable releases.
const (
	macUpdateBase  = "https://orca.aichat.diy/updates"
	ghReleasesBase = "https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases"
	ghReleasesAPI  = "https://api.github.com/repos/nanbo0ne/O.R.C.A-for-Windows/releases?per_page=20"
	httpTimeout    = 15 * time.Second
)

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func desktopReleaseVersion(tag string) (string, bool) {
	const prefix = "desktop-v"
	if !strings.HasPrefix(tag, prefix) {
		return "", false
	}
	return normalizeVersion(strings.TrimPrefix(tag, "desktop-"))
}

func selectLatestDesktopRelease(releases []githubRelease) (githubRelease, bool) {
	var selected githubRelease
	var selectedVersion string
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		candidate, ok := desktopReleaseVersion(release.TagName)
		if !ok || (selectedVersion != "" && semver.Compare(candidate, selectedVersion) <= 0) {
			continue
		}
		selected = release
		selectedVersion = candidate
	}
	return selected, selectedVersion != ""
}

func fetchLatestDesktopRelease(ctx context.Context, c *http.Client) (githubRelease, error) {
	return fetchLatestDesktopReleaseAt(ctx, c, ghReleasesAPI)
}

func fetchLatestDesktopReleaseAt(ctx context.Context, c *http.Client, url string) (githubRelease, error) {
	body, err := fetchBytes(ctx, c, url)
	if err != nil {
		return githubRelease{}, err
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return githubRelease{}, fmt.Errorf("decode GitHub releases: %w", err)
	}
	release, ok := selectLatestDesktopRelease(releases)
	if !ok {
		return githubRelease{}, fmt.Errorf("no stable desktop release found")
	}
	return release, nil
}

func evaluateGitHubRelease(current string, release githubRelease) UpdateInfo {
	latest, validLatest := desktopReleaseVersion(release.TagName)
	info := UpdateInfo{
		Current:       current,
		Latest:        strings.TrimPrefix(latest, "v"),
		Notes:         release.Body,
		CanSelfUpdate: false,
		DownloadURL:   release.HTMLURL,
	}
	currentVersion, validCurrent := normalizeVersion(current)
	if !validLatest {
		info.Err = "release has no valid desktop version"
		return info
	}
	if validCurrent && semver.Compare(latest, currentVersion) > 0 {
		info.Available = true
	}
	return info
}

// manifestEndpoints returns the primary (Mac) then fallback (GitHub) manifest URLs
// for the running build's channel.
func manifestEndpoints() []string {
	if channel == "canary" {
		// Canary has no public GitHub release, so there is no
		// GitHub fallback for this channel.
		return []string{macUpdateBase + "/canary/latest.json"}
	}
	return []string{
		macUpdateBase + "/stable/latest.json",
		ghReleasesBase + "/latest/download/latest.json",
	}
}

// downloadPage is the human-facing releases page shown when self-update is
// unavailable (macOS) or the manifest omits its own link.
func downloadPage() string {
	if channel == "canary" {
		return ghReleasesBase // lists pre-releases too
	}
	return ghReleasesBase + "/latest"
}

// UpdateInfo is the CheckUpdate result that drives the frontend's update banner.
type UpdateInfo struct {
	CanDownload   bool   `json:"canDownload"`
	Source        string `json:"source,omitempty"`
	Available     bool   `json:"available"`
	Current       string `json:"current"`
	Latest        string `json:"latest"`
	Notes         string `json:"notes"`
	CanSelfUpdate bool   `json:"canSelfUpdate"` // win/linux true; macOS false (unsigned → manual download)
	DownloadURL   string `json:"downloadUrl"`   // human-facing releases page (macOS path / fallback link)
	AssetSize     int64  `json:"assetSize"`     // running platform's artifact size, for the progress bar
	Err           string `json:"err,omitempty"` // set when the check itself failed (both endpoints down)
}

// updateProgress is the payload of the "updater:progress" Wails event emitted
// throughout ApplyUpdate.
type updateProgress struct {
	Version       string `json:"version,omitempty"`
	CanSelfUpdate bool   `json:"canSelfUpdate"`
	Phase         string `json:"phase"` // downloading | verifying | applying | done | error
	Received      int64  `json:"received"`
	Total         int64  `json:"total"`
	Err           string `json:"err,omitempty"`
}

func httpClient() (*http.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{})
}

// Only a registered Windows installation is eligible for an in-place update.
func canSelfUpdate() bool { return isInstalledDesktop() }

// normalizeVersion canonicalizes a version to semver "vX.Y.Z". It reports ok=false
// for the un-injected "dev" build (and anything not valid semver), so a dev build
// never prompts to update.
func normalizeVersion(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" {
		return "", false
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", false
	}
	return semver.Canonical(v), true
}

// fetchManifest pulls latest.json from the primary endpoint, then the fallback,
// and decodes it.
func fetchManifest(ctx context.Context, c *http.Client) (*update.Manifest, error) {
	var lastErr error
	for _, url := range manifestEndpoints() {
		attempt, cancel := context.WithTimeout(ctx, 12*time.Second)
		m, err := fetchSignedManifestAt(attempt, c, url, update.Verify)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		return m, nil
	}
	return nil, fmt.Errorf("update: fetch manifest: %w", lastErr)
}

// evaluate compares the running version against the manifest and builds the
// frontend-facing result. Pure (no I/O) so the comparison is unit-tested.
func evaluate(current string, m *update.Manifest) UpdateInfo {
	page := m.DownloadPage
	if page == "" {
		page = downloadPage()
	}
	info := UpdateInfo{
		Current:       current,
		Latest:        m.Version,
		Notes:         m.Notes,
		CanSelfUpdate: canSelfUpdate(),
		DownloadURL:   page,
	}
	cur, okCur := normalizeVersion(current)
	latest, okLatest := normalizeVersion(m.Version)
	if !okLatest {
		info.Err = "manifest has no valid version"
		return info
	}
	// A dev/invalid running version never auto-prompts.
	if okCur && semver.Compare(latest, cur) > 0 {
		info.Available = true
	}
	if a, ok := selectedUpdateAsset(m); ok {
		info.AssetSize = a.Size
		info.CanDownload = info.Available && a.Size > 0
	}
	return info
}

func selectedUpdateAsset(m *update.Manifest) (update.Asset, bool) {
	key := update.CurrentPlatform()
	if runtime.GOOS == "windows" && !isInstalledDesktop() {
		key += "-portable"
	}
	asset, ok := m.Platforms[key]
	return asset, ok
}

// fetchBytes GETs a URL fully into memory.
func fetchBytes(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// download fetches url into memory, invoking onProgress as bytes arrive. total is
// the expected size for the progress denominator (overridden by Content-Length).
func download(ctx context.Context, c *http.Client, url string, total int64, onProgress func(received, total int64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if resp.ContentLength > 0 {
		total = resp.ContentLength
	}
	var buf bytes.Buffer
	pr := &progressReader{r: resp.Body, total: total, onProgress: onProgress}
	if _, err := io.Copy(&buf, pr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// progressReader reports cumulative bytes read, throttled so the event channel
// isn't flooded.
type progressReader struct {
	r          io.Reader
	received   int64
	total      int64
	lastEmit   int64
	onProgress func(received, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.received += int64(n)
	// Emit roughly every 256 KiB, and always on the final read (io.EOF).
	if p.onProgress != nil && (p.received-p.lastEmit >= 256<<10 || err == io.EOF) {
		p.lastEmit = p.received
		p.onProgress(p.received, p.total)
	}
	return n, err
}

// checkSHA256 verifies data's digest matches the lowercase-hex want.
func checkSHA256(data []byte, want string) error {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, want) {
		return fmt.Errorf("update: sha256 mismatch: got %s want %s", got, want)
	}
	return nil
}

// extractBinary pulls a single named regular file out of a .tar.gz blob.
func extractBinary(targz []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(targz))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && (h.Name == name || strings.HasSuffix(h.Name, "/"+name)) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("update: %q not found in archive", name)
}

// applyLinux replaces the running binary with the one inside the downloaded
// tar.gz; the caller relaunches afterwards.
func applyLinux(targz []byte) error {
	bin, err := extractBinary(targz, "Orca")
	if err != nil {
		return err
	}
	return selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{})
}

// applyWindows writes the downloaded NSIS installer to a temp file and launches it.
// The per-user installer needs no admin rights and its finish page relaunches the
// app; the caller then exits so the installer can replace the running exe. The
// installer targets the running app's own directory (issue #3217) so an update
// overwrites in place instead of landing a second copy at the per-user default —
// this also covers upgrades from builds that predate the registry InstallLocation.
func applyWindows(installer []byte) error {
	f, err := os.CreateTemp("", "orca-update-*.exe")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err := f.Write(installer); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return installerCommand(name, currentInstallDir()).Start()
}

// currentInstallDir is the directory of the running executable — the location a
// Windows update must overwrite. Empty when it can't be resolved, in which case
// the installer falls back to its own InstallDir logic.
func currentInstallDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// relaunch starts a fresh copy of the (just-replaced) executable.
func relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Start()
}
