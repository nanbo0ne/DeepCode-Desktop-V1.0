package main

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aead.dev/minisign"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/update"
)

// TestSignFiles signs a file with a throwaway key pair (injected via env, exactly
// as CI passes the real key) and verifies the produced .minisig validates under the
// matching public key.
func TestSignFiles(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := minisign.EncryptKey("pw", priv)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINISIGN_PRIVATE_KEY", string(enc))
	t.Setenv("MINISIGN_PASSWORD", "pw")

	dir := t.TempDir()
	artifact := filepath.Join(dir, "O.R.C.A-linux-amd64.deb")
	payload := []byte("pretend this is a release tarball")
	if err := os.WriteFile(artifact, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := signFiles([]string{artifact}); err != nil {
		t.Fatalf("signFiles: %v", err)
	}
	sig, err := os.ReadFile(artifact + ".minisig")
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	if !minisign.Verify(pub, payload, sig) {
		t.Fatal("produced signature does not verify under the signing key")
	}
	parsed, err := minisign.SignatureFromFile(artifact + ".minisig")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Algorithm != minisign.HashEdDSA {
		t.Fatalf("signature algorithm = %#x, want hashed minisign", parsed.Algorithm)
	}
}

// TestGenManifest builds a manifest from a directory of fake artifacts and checks
// every platform is listed with a download URL, a parallel .minisig URL, and a
// non-empty digest. The .minisig and latest.json files must be ignored.
func TestGenManifest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"O.R.C.A-for-Windows-windows-amd64-installer.exe",
		"O.R.C.A-for-Windows-windows-amd64.zip",
		"O.R.C.A-macos-universal.dmg",
		"O.R.C.A-linux-amd64.deb",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := minisign.EncryptKey("pw", priv)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINISIGN_PRIVATE_KEY", string(enc))
	t.Setenv("MINISIGN_PASSWORD", "pw")
	for _, n := range names {
		if err := signFiles([]string{filepath.Join(dir, n)}); err != nil {
			t.Fatalf("sign %s: %v", n, err)
		}
	}
	notes := filepath.Join(t.TempDir(), "desktop-v3.0.3.md")
	if err := os.WriteFile(notes, []byte("# O.R.C.A Desktop v3.0.3\n\nVerified release notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELEASE_NOTES", notes)

	if err := genManifest(dir, "v3.0.3", "desktop-v3.0.3"); err != nil {
		t.Fatalf("genManifest: %v", err)
	}
	if err := signFiles([]string{filepath.Join(dir, "latest.json")}); err != nil {
		t.Fatalf("sign manifest: %v", err)
	}
	if err := validateRelease(dir, "v3.0.3", "desktop-v3.0.3"); err != nil {
		t.Fatalf("validateRelease: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m update.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("latest.json is not valid: %v", err)
	}
	if m.Version != "v3.0.3" {
		t.Fatalf("version = %q, want v3.0.3", m.Version)
	}
	if m.Notes != string(mustRead(t, notes)) {
		t.Fatalf("manifest notes do not match release notes")
	}
	if len(m.Platforms) != 5 {
		t.Fatalf("want 5 platform entries, got %d: %v", len(m.Platforms), m.Platforms)
	}
	win, ok := m.Platforms["windows-amd64"]
	if !ok {
		t.Fatal("windows-amd64 missing")
	}
	wantURL := "https://orca.aichat.diy/releases/desktop-v3.0.3/O.R.C.A-for-Windows-windows-amd64-installer.exe"
	if win.URL != wantURL {
		t.Fatalf("windows url = %q, want %q", win.URL, wantURL)
	}
	if win.Sig != wantURL+".minisig" {
		t.Fatalf("windows sig = %q, want %q.minisig", win.Sig, wantURL)
	}
	wantFallback := "https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.3/O.R.C.A-for-Windows-windows-amd64-installer.exe"
	if win.FallbackURL != wantFallback || win.FallbackSig != wantFallback+".minisig" {
		t.Fatalf("windows fallback = %+v", win)
	}
	if win.SHA256 == "" || win.Size == 0 {
		t.Fatalf("windows asset missing digest/size: %+v", win)
	}
	if got := m.Platforms["windows-amd64-portable"].URL; !strings.HasSuffix(got, "/O.R.C.A-for-Windows-windows-amd64.zip") {
		t.Fatalf("portable URL = %q", got)
	}
	if got := m.Platforms["darwin-amd64"].URL; !strings.HasSuffix(got, "/O.R.C.A-macos-universal.dmg") {
		t.Fatalf("darwin-amd64 URL = %q", got)
	}
	if got := m.Platforms["darwin-arm64"].URL; got != m.Platforms["darwin-amd64"].URL {
		t.Fatalf("universal DMG URLs differ: %q vs %q", got, m.Platforms["darwin-amd64"].URL)
	}
	lin, ok := m.Platforms["linux-amd64"]
	if !ok {
		t.Fatal("linux-amd64 missing")
	}
	if !strings.HasSuffix(lin.URL, "/O.R.C.A-linux-amd64.deb") {
		t.Fatalf("linux-amd64 url = %q, want the .deb", lin.URL)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
