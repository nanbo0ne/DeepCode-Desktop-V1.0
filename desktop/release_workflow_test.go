package main

import (
	"os"
	"strings"
	"testing"
)

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
