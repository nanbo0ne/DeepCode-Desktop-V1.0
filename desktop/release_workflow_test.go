package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseChecksumsCoverPayloadAndExcludeManifest(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required for the release script")
	}
	dir := t.TempDir()
	want := ""
	for _, name := range []string{"Orca installer.exe", "Orca.zip"} {
		body := []byte("payload for " + name)
		if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
			t.Fatal(err)
		}
		want += fmt.Sprintf("%x  %s\n", sha256.Sum256(body), name)
	}
	for range 2 {
		cmd := exec.Command(bash, "../scripts/checksum-desktop-release.sh", filepath.ToSlash(dir))
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("checksum script: %v\n%s", err, output)
		}
		got, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS.txt"))
		if err != nil || string(got) != want {
			t.Fatalf("manifest = %q, error = %v; want %q", got, err, want)
		}
	}
	cmd := exec.Command(bash, "../scripts/checksum-desktop-release.sh", filepath.ToSlash(t.TempDir()))
	if err := cmd.Run(); err == nil {
		t.Fatal("empty release directory must not produce a successful manifest")
	}
}

func TestReleaseChecksumsAreGeneratedBeforePublication(t *testing.T) {
	workflow := readDesktopReleaseWorkflow(t)
	checksums := strings.Index(workflow, "bash scripts/checksum-desktop-release.sh dist")
	publish := strings.Index(workflow, "- name: Publish GitHub release")
	manifest := strings.Index(workflow, "- name: Generate manifest")
	if checksums <= manifest || publish <= checksums {
		t.Fatal("checksums must include the optional signed manifest and precede publication")
	}
}

func readDesktopReleaseWorkflow(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../.github/workflows/release-desktop.yml")
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func workflowSection(script, start, end string) string {
	from := strings.Index(script, start)
	if from < 0 {
		return ""
	}
	if end == "" {
		return script[from:]
	}
	to := strings.Index(script[from+len(start):], end)
	if to < 0 {
		return script[from:]
	}
	return script[from : from+len(start)+to]
}

func TestReleaseWorkflowPreservesReleaseGatesAndTarget(t *testing.T) {
	workflow := readDesktopReleaseWorkflow(t)
	for _, want := range []string{
		`node-version: "22.23.2"`,
		"name: Test core",
		"name: Test frontend",
		"name: Test native desktop",
		`allow_unsigned_windows`,
		`--target "${{ github.sha }}"`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow is missing retained gate/target %q", want)
		}
	}
}

func TestReleaseWorkflowRepackagesSignedPortablePayload(t *testing.T) {
	workflow := readDesktopReleaseWorkflow(t)
	section := workflowSection(workflow, "- name: Repackage Windows installer with signed app", "- name: Upload unsigned Windows installer for SignPath")
	if section == "" {
		t.Fatal("signed Windows repackage step is missing")
	}
	for _, want := range []string{
		`payload="desktop/build/windows/installer-go/payload"`,
		`cp "$payload/node.exe" "$staging/node.exe"`,
		`cp "$payload/LICENSE.node.txt" "$staging/LICENSE.node.txt"`,
		`cp -R "$payload/codegraph" "$staging/codegraph"`,
		`Compress-Archive -Force -Path '${staging_win}\\*'`,
		`rm -rf -- "$staging"`,
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("signed portable repackage is missing %q", want)
		}
	}
}

func TestReleaseFrontendGateGeneratesNativeBindings(t *testing.T) {
	workflow := readDesktopReleaseWorkflow(t)
	gate := workflowSection(workflow, "  cache-guard:", "  build:")
	generate := strings.Index(gate, "wails generate module -tags webkit2_41")
	tests := strings.Index(gate, "npm run test:all")
	if generate < 0 || tests <= generate {
		t.Fatal("fresh-checkout frontend tests require generated Wails bindings first")
	}
	if !strings.Contains(gate, "libwebkit2gtk-4.1-dev") {
		t.Fatal("binding generation must use the installed Linux WebKit toolchain")
	}
}

func TestReleaseWorkflowValidatesExistingAnnotatedTagBeforePublish(t *testing.T) {
	workflow := readDesktopReleaseWorkflow(t)
	section := workflowSection(workflow, "- name: Validate stable tag target", "# Canary is R2-only")
	if section == "" {
		t.Fatal("stable tag validation step is missing")
	}
	for _, want := range []string{
		`TAG: ${{ steps.ver.outputs.tag }}`,
		`EXPECTED_SHA: ${{ github.sha }}`,
		`ls-remote "$remote" "refs/tags/${TAG}" "refs/tags/${TAG}^{}"`,
		`refs/tags/${TAG}^{}`,
		`tag ${TAG} does not exist; final creation may target ${EXPECTED_SHA}`,
		`refusing release asset clobber`,
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("stable tag validation is missing %q", want)
		}
	}
	if strings.Contains(section, "gh release") {
		t.Fatal("tag validation must happen before any release mutation")
	}
}

func TestReleaseWorkflowSkipsR2WithoutMinisign(t *testing.T) {
	workflow := readDesktopReleaseWorkflow(t)
	if !strings.Contains(workflow, `HAS_MINISIGN: ${{ secrets.MINISIGN_PRIVATE_KEY != '' && secrets.MINISIGN_PASSWORD != '' }}`) {
		t.Fatal("mirror job is missing its Minisign availability guard")
	}
	section := workflowSection(workflow, "name: mirror to R2", "")
	for _, want := range []string{
		`env.HAS_R2 == 'true' && env.HAS_MINISIGN == 'true'`,
		`Skip R2 mirror without Minisign`,
		`env.HAS_R2 == 'true' && env.HAS_MINISIGN != 'true'`,
		`existing R2 latest/ is unchanged`,
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("R2 guard is missing %q", want)
		}
	}
}
