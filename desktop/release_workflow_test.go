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
		body := []byte("payload for " + name + "\x00\r\n\xff")
		if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
			t.Fatal(err)
		}
		want += fmt.Sprintf("%x *%s\n", sha256.Sum256(body), name)
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
	publish := strings.Index(workflow, "- name: Publish draft GitHub release")
	manifest := strings.Index(workflow, "- name: Generate manifest")
	if checksums <= manifest || publish <= checksums {
		t.Fatal("checksums must include the optional signed manifest and precede publication")
	}
}

func TestReleaseChecksumsSupportNativeMacTool(t *testing.T) {
	body, err := os.ReadFile("../scripts/checksum-desktop-release.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"command -v sha256sum", "command -v shasum", "hash=(shasum -a 256)", `"${hash[@]}" --check SHA256SUMS.txt`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("portable checksum generation is missing %q", want)
		}
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

func TestStableManualDispatchIsMainOnlyBeforeBuild(t *testing.T) {
	workflow := readDesktopReleaseWorkflow(t)
	guard := strings.Index(workflow, "- name: Require stable manual dispatch on main")
	build := strings.Index(workflow, "  build:")
	if guard < 0 || build < 0 || guard > build {
		t.Fatal("stable manual dispatch must be restricted to main before platform builds")
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
		`[ -f "THIRD-PARTY-NOTICES.txt" ]`,
		`cp "THIRD-PARTY-NOTICES.txt" "$staging/THIRD-PARTY-NOTICES.txt"`,
		`Compress-Archive -Force -Path '${staging_win}\\*'`,
		`rm -rf -- "$staging"`,
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("signed portable repackage is missing %q", want)
		}
	}
}

func TestInstallerAcceptanceUsesPublished303Baseline(t *testing.T) {
	body, err := os.ReadFile("../scripts/test-desktop-installer.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		"$ExpectedVersion = '3.0.4'",
		"releases/tags/desktop-v3.0.3",
		"Assert-Installation $upgradeDir '3.0.3' 'installed-303'",
		"'SHA256SUMS.txt'",
		"7437055c8680e564311c3455f5d6d1ddea06e9a1b69ee2e56d3e52960b9cc75b",
		"$oldHash -ine $checksumRows[0].Groups[1].Value -or $oldHash -cne $pinnedOldHash",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("installer acceptance is missing published-baseline check %q", want)
		}
	}
	if strings.Contains(script, "3.0.2") {
		t.Fatal("installer acceptance must not retain the 3.0.2 upgrade baseline")
	}
	if strings.Contains(script, "5bf27fd4d958fc389a2ef320e401d05875d379b4d96c4ac64a8e8288ea48894d") {
		t.Fatal("installer acceptance must not retain the 3.0.2 baseline hash")
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
	section := workflowSection(workflow, "- name: Validate stable tag target", "# Canary never appears")
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

func TestReleaseWorkflowUsesDraftOnlyAndDoesNotRewriteSignedManifest(t *testing.T) {
	workflow := readDesktopReleaseWorkflow(t)
	for _, forbidden := range []string{"R2", "Rewrite latest.json", "--clobber", "gh release edit"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("obsolete public mirror/mutation path remains: %q", forbidden)
		}
	}
	for _, want := range []string{
		"- name: Sign manifest (minisign)",
		"- name: Validate packages, manifest, and signatures",
		"RELEASE_NOTES: ../docs/releases/${{ steps.ver.outputs.tag }}.md",
		"- name: Publish draft GitHub release",
		"--draft",
		"--notes-file \"docs/releases/${{ steps.ver.outputs.tag }}.md\"",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("draft signed release workflow is missing %q", want)
		}
	}
}

func TestNextDesktopReleaseDoesNotGenerateLegacyArtifactAliases(t *testing.T) {
	workflow := readDesktopReleaseWorkflow(t)
	if strings.Contains(workflow, "DeepSeek-Orca-windows-") {
		t.Fatal("next desktop release workflow must not publish DeepSeek-Orca artifact aliases")
	}
	body, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "DeepSeek-Orca-windows-") {
		t.Fatal("desktop build must not generate DeepSeek-Orca artifact aliases")
	}
}
