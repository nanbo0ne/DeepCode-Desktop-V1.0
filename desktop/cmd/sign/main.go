// Command sign is the CI-side signing and manifest tool for desktop releases. It
// is never shipped in any artifact — the release workflow invokes it via
// `go run ./cmd/sign`. It shares desktop/internal/update with the running updater
// so the sign path and the verify path use one definition of the manifest and one
// minisign implementation.
//
// Subcommands:
//
//	sign <file>...               Write <file>.minisig for each file, signing with the
//	                             encrypted minisign private key in $MINISIGN_PRIVATE_KEY
//	                             (decrypted with $MINISIGN_PASSWORD).
//
//	manifest <dir> <ver> <tag>   Validate the staged release packages and write
//	                             <dir>/latest.json with the primary and fallback URLs.
//
//	validate <dir> <ver> <tag>   Validate the complete staged package, manifest, and
//	                             prehashed minisign signature set.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"aead.dev/minisign"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "sign":
		err = signFiles(os.Args[2:])
	case "manifest":
		if len(os.Args) != 5 {
			usage()
		}
		err = genManifest(os.Args[2], os.Args[3], os.Args[4])
	case "genkey":
		if len(os.Args) != 3 {
			usage()
		}
		err = genKey(os.Args[2])
	case "verify":
		if len(os.Args) != 3 {
			usage()
		}
		err = verifyFile(os.Args[2])
	case "validate":
		if len(os.Args) != 5 {
			usage()
		}
		err = validateRelease(os.Args[2], os.Args[3], os.Args[4])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:\n  sign <file>...\n  manifest <dir> <version> <tag>\n  validate <dir> <version> <tag>\n  genkey <dir>\n  verify <file>")
	os.Exit(2)
}

// verifyFile checks <file> against <file>.minisig using the embedded public key —
// the same check the updater runs before applying. A self-test that the signing
// key matches what's compiled in. Returns an error (nonzero exit) on mismatch.
func verifyFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sig, err := os.ReadFile(path + ".minisig")
	if err != nil {
		return err
	}
	if err := update.VerifyReader(f, sig); err != nil {
		return err
	}
	fmt.Printf("OK: %s verifies against the embedded public key\n", path)
	return nil
}

// genKey generates a fresh minisign key pair, writing the encrypted private key
// (orca.key) and the public key (orca.pub) into dir. The password comes
// from $MINISIGN_PASSWORD. The public key is printed — it's safe to publish; embed
// it in internal/update/verify.go. The private key never leaves dir.
func genKey(dir string) error {
	pw := os.Getenv("MINISIGN_PASSWORD")
	if strings.TrimSpace(pw) == "" {
		return fmt.Errorf("genkey: MINISIGN_PASSWORD is empty")
	}
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	enc, err := minisign.EncryptKey(pw, priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	keyPath := filepath.Join(dir, "orca.key")
	pubPath := filepath.Join(dir, "orca.pub")
	if err := os.WriteFile(keyPath, enc, 0o600); err != nil {
		return err
	}
	pubText, err := pub.MarshalText()
	if err != nil {
		return err
	}
	if err := os.WriteFile(pubPath, pubText, 0o644); err != nil {
		return err
	}
	fmt.Printf("private key -> %s (keep secret; this is the MINISIGN_PRIVATE_KEY value)\n", keyPath)
	fmt.Printf("public key  -> %s\n\n", pubPath)
	fmt.Printf("public key (embed in internal/update/verify.go, key ID %016X):\n%s\n", pub.ID(), pubText)
	return nil
}

// signFiles writes a detached .minisig next to each input file. The private key is
// read only from the environment — it never touches disk or argv.
func signFiles(files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("sign: no files given")
	}
	keyText := os.Getenv("MINISIGN_PRIVATE_KEY")
	if strings.TrimSpace(keyText) == "" {
		return fmt.Errorf("sign: MINISIGN_PRIVATE_KEY is empty")
	}
	password := os.Getenv("MINISIGN_PASSWORD")
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("sign: MINISIGN_PASSWORD is empty")
	}
	priv, err := minisign.DecryptKey(password, []byte(keyText))
	if err != nil {
		return fmt.Errorf("sign: decrypt private key: %w", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, ".minisig") {
			continue
		}
		input, err := os.Open(f)
		if err != nil {
			return err
		}
		reader := minisign.NewReader(input)
		if _, err := io.Copy(io.Discard, reader); err != nil {
			input.Close()
			return err
		}
		sig := reader.SignWithComments(priv,
			"file:"+filepath.Base(f), "O.R.C.A. desktop release")
		if err := input.Close(); err != nil {
			return err
		}
		out := f + ".minisig"
		if err := os.WriteFile(out, sig, 0o644); err != nil {
			return err
		}
		fmt.Printf("signed %s -> %s\n", f, out)
	}
	return nil
}

const (
	primaryReleaseBase  = "https://orca.aichat.diy/releases"
	fallbackReleaseBase = "https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download"
)

type releasePackage struct {
	Platform string
	Name     string
}

// requiredPackages is deliberately an exact table. Legacy aliases and substring
// matches are excluded so one human-download artifact cannot shadow another.
var requiredPackages = []releasePackage{
	{Platform: "windows-amd64", Name: "O.R.C.A-for-Windows-windows-amd64-installer.exe"},
	{Platform: "windows-amd64-portable", Name: "O.R.C.A-for-Windows-windows-amd64.zip"},
	{Platform: "darwin-amd64", Name: "O.R.C.A-macos-universal.dmg"},
	{Platform: "darwin-arm64", Name: "O.R.C.A-macos-universal.dmg"},
	{Platform: "linux-amd64", Name: "O.R.C.A-linux-amd64.deb"},
}

// genManifest validates the staged release and writes dir/latest.json.
// version is the semver compared by the updater (e.g. "v1.1.0"); tag is the GitHub
// release tag used in download URLs (e.g. "desktop-v1.1.0").
func genManifest(dir, version, tag string) error {
	if err := validateVersionTag(version, tag); err != nil {
		return err
	}
	notes, err := loadReleaseNotes(dir, version, tag)
	if err != nil {
		return err
	}
	if err := validateStagedPackages(dir); err != nil {
		return err
	}
	m := update.Manifest{
		Version:      version,
		Notes:        notes,
		DownloadPage: fmt.Sprintf("https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/tag/%s", tag),
		Platforms:    map[string]update.Asset{},
	}
	for _, pkg := range requiredPackages {
		size, sum, err := hashFile(filepath.Join(dir, pkg.Name))
		if err != nil {
			return err
		}
		primary := fmt.Sprintf("%s/%s/%s", primaryReleaseBase, tag, pkg.Name)
		fallback := fmt.Sprintf("%s/%s/%s", fallbackReleaseBase, tag, pkg.Name)
		m.Platforms[pkg.Platform] = update.Asset{
			URL:         primary,
			Sig:         primary + ".minisig",
			FallbackURL: fallback,
			FallbackSig: fallback + ".minisig",
			Size:        size,
			SHA256:      sum,
		}
		fmt.Printf("manifest: %s -> %s (%d bytes)\n", pkg.Platform, pkg.Name, size)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "latest.json"), append(b, '\n'), 0o644)
}

func validateVersionTag(version, tag string) error {
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?$`).MatchString(version) {
		return fmt.Errorf("manifest: invalid version %q", version)
	}
	if tag == "desktop-canary" {
		if !strings.Contains(version, "-canary.") {
			return fmt.Errorf("manifest: canary tag requires a canary version")
		}
		return nil
	}
	if tag != "desktop-"+version {
		return fmt.Errorf("manifest: tag %q does not match version %q", tag, version)
	}
	return nil
}

func loadReleaseNotes(dir, version, tag string) (string, error) {
	candidates := []string{}
	if configured := strings.TrimSpace(os.Getenv("RELEASE_NOTES")); configured != "" {
		candidates = append(candidates, configured)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	root := filepath.Dir(absDir)
	candidates = append(candidates, filepath.Join(root, "docs", "releases", tag+".md"))
	baseVersion := strings.SplitN(strings.TrimPrefix(version, "v"), "-", 2)[0]
	if baseVersion != "" {
		candidates = append(candidates, filepath.Join(root, "docs", "releases", "desktop-v"+baseVersion+".md"))
	}
	for cwd, i := currentDir(), 0; i < 8 && cwd != ""; cwd, i = filepath.Dir(cwd), i+1 {
		candidates = append(candidates, filepath.Join(cwd, "docs", "releases", tag+".md"))
		if baseVersion != "" {
			candidates = append(candidates, filepath.Join(cwd, "docs", "releases", "desktop-v"+baseVersion+".md"))
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		body, readErr := os.ReadFile(candidate)
		if readErr == nil && strings.TrimSpace(string(body)) != "" {
			return string(body), nil
		}
	}
	return "", fmt.Errorf("manifest: release notes %s.md not found", tag)
}

func currentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func validateStagedPackages(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, ".minisig") || name == "latest.json" || name == "SHA256SUMS.txt" {
			continue
		}
		if err := validateSignature(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("manifest: payload %s signature: %w", name, err)
		}
	}
	for _, pkg := range requiredPackages {
		path := filepath.Join(dir, pkg.Name)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("manifest: required package %s: %w", pkg.Name, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("manifest: required package %s is empty or not regular", pkg.Name)
		}
		if err := validateSignature(path); err != nil {
			return fmt.Errorf("manifest: package %s signature: %w", pkg.Name, err)
		}
	}
	return nil
}

func validateSignature(path string) error {
	sig, err := minisign.SignatureFromFile(path + ".minisig")
	if err != nil {
		return err
	}
	if sig.Algorithm != minisign.HashEdDSA {
		return fmt.Errorf("signature is not prehashed minisign")
	}
	return nil
}

func validateRelease(dir, version, tag string) error {
	if err := validateVersionTag(version, tag); err != nil {
		return err
	}
	if err := validateStagedPackages(dir); err != nil {
		return err
	}
	manifestPath := filepath.Join(dir, "latest.json")
	if err := validateSignature(manifestPath); err != nil {
		return fmt.Errorf("manifest: latest.json signature: %w", err)
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest update.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("manifest: invalid latest.json: %w", err)
	}
	if manifest.Version != version || strings.TrimSpace(manifest.Notes) == "" {
		return fmt.Errorf("manifest: version or release notes do not match staged release")
	}
	if len(manifest.Platforms) != len(requiredPackages) {
		return fmt.Errorf("manifest: got %d platforms, want %d", len(manifest.Platforms), len(requiredPackages))
	}
	for _, pkg := range requiredPackages {
		asset, ok := manifest.Platforms[pkg.Platform]
		if !ok {
			return fmt.Errorf("manifest: missing platform %s", pkg.Platform)
		}
		primary := fmt.Sprintf("%s/%s/%s", primaryReleaseBase, tag, pkg.Name)
		fallback := fmt.Sprintf("%s/%s/%s", fallbackReleaseBase, tag, pkg.Name)
		if asset.URL != primary || asset.Sig != primary+".minisig" || asset.FallbackURL != fallback || asset.FallbackSig != fallback+".minisig" {
			return fmt.Errorf("manifest: URLs for %s are not the staged primary/fallback pair", pkg.Platform)
		}
		size, sum, err := hashFile(filepath.Join(dir, pkg.Name))
		if err != nil {
			return err
		}
		if asset.Size != size || !strings.EqualFold(asset.SHA256, sum) {
			return fmt.Errorf("manifest: metadata for %s does not match staged package", pkg.Platform)
		}
	}
	fmt.Printf("validated release %s (%d packages plus signed manifest)\n", tag, len(requiredPackages))
	return nil
}

// hashFile returns the size and lowercase-hex SHA-256 of a file, streaming it so
// large artifacts don't have to fit in memory.
func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}
