package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestWindowsInstallerOffersShortcutAndLaunchChoices(t *testing.T) {
	body, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("installer script must use a UTF-8 BOM so makensis decodes custom Chinese text correctly")
	}
	script := string(body)
	for _, want := range []string{
		"Page custom InstallOptionsPage InstallOptionsPageLeave",
		"MUI_FINISHPAGE_RUN",
		"运行 ${INFO_PRODUCTNAME}",
		"选择安装选项",
		"创建桌面快捷方式",
		"CreateDesktopShortcut == ${BST_CHECKED}",
		"Delete \"$DESKTOP\\${INFO_PRODUCTNAME}.lnk\"",
		"IfFileExists \"$DESKTOP\\${INFO_PRODUCTNAME}.lnk\"",
		`File /oname=node.exe "..\installer-go\payload\node.exe"`,
		`File /oname=LICENSE.node.txt "..\installer-go\payload\LICENSE.node.txt"`,
		`File /r "..\installer-go\payload\codegraph\*.*"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("installer is missing %q", want)
		}
	}
}

func TestWindowsPackagingPinsAndVerifiesRuntimePayloads(t *testing.T) {
	body, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		`NODE_VERSION="v22.23.2"`,
		`NODE_RELEASE_BASE="https://nodejs.org/dist/${NODE_VERSION}"`,
		`node_archive_sha="1177b4137ba5adaa56354ae40f1080c7450e8ae09cecb47da459d1c52ac99f97"`,
		`node_binary_sha="0d0f5e39f9f3d9587bc19f73eab3c2c9c4903fd02d6dbf9c853dd81b3d95fad4"`,
		`node_archive_sha="fec025a6da31757e3b6af84c5a1628e9d38442ca99a2161091d78f2fcfa35ef3"`,
		`node_binary_sha="97cce5301a815d2dce07ac5bfd1e6039eae88185ec1d10ae4f8cb712f1732878"`,
		`verify_sha256 "$node_zip" "$node_archive_sha"`,
		`cp "$node_root/LICENSE" "$node_license_dest"`,
		`assert_within_dir "$payload" "$ROOT/desktop/build/windows/installer-go"`,
		`assert_within_dir "$node_extract" "$payload"`,
		`rm -rf -- "$node_extract"`,
		`"\$ErrorActionPreference = 'Stop'; Expand-Archive`,
		`Re-extract from the verified archive on every build`,
		`asset="codegraph-win32-${codegraph_arch}.zip"`,
		`[ "$version" = "$go_version" ] || { echo "CodeGraph version drift`,
		`grep -F "\"$asset\"" "$ROOT/internal/codegraph/checksums.go"`,
		`verify_sha256 "$zip" "$codegraph_sha"`,
		`assert_within_dir "$cg_dest" "$payload"`,
		`assert_within_dir "$codegraph_extract" "$payload"`,
		`rm -rf -- "$cg_dest" "$codegraph_extract"`,
		`mv -- "$extracted" "$cg_dest"`,
		`rm -rf -- "$codegraph_extract"`,
		`https://github.com/colbymchenry/codegraph/releases/download/${version}/${asset}" -o "$zip"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("packaging script is missing %q", want)
		}
	}
	if strings.Contains(script, "command -v node") || strings.Contains(script, "node_src") || strings.Contains(script, "reusing verified") {
		t.Fatal("packaging script must not source the installer Node runtime from PATH")
	}
	guard := strings.Index(script, `assert_within_dir "$payload" "$ROOT/desktop/build/windows/installer-go"`)
	mkdir := strings.Index(script, `mkdir -p "$payload"`)
	if guard < 0 || mkdir < 0 || guard > mkdir {
		t.Fatal("payload must be path-guarded before it is created or cleaned")
	}
}

func TestWindowsPackagingPrepOnlySkipsApplicationBuild(t *testing.T) {
	body, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		`if [ "${ORCA_PACKAGING_PREP_ONLY:-}" = "1" ]; then`,
		`prepare_windows_installer_resources`,
		`echo "==> verified Windows payload staged; prep-only mode complete"`,
		`exit 0`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("prep-only mode is missing %q", want)
		}
	}
	prepStart := strings.Index(script, `if [ "${ORCA_PACKAGING_PREP_ONLY:-}" = "1" ]; then`)
	if prepStart < 0 {
		t.Fatal("prep-only mode guard is missing")
	}
	prepEnd := strings.Index(script[prepStart:], "fi")
	if prepEnd < 0 || strings.Contains(script[prepStart:prepStart+prepEnd], "wails build") {
		t.Fatal("prep-only mode must exit before the Wails build")
	}
}

func TestWindowsPortablePackageIncludesTheVerifiedFullPayload(t *testing.T) {
	body, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		`cp "$payload/node.exe" "$staging/node.exe"`,
		`cp "$payload/LICENSE.node.txt" "$staging/LICENSE.node.txt"`,
		`cp -R "$payload/codegraph" "$staging/codegraph"`,
		`Compress-Archive -Force -Path '${staging_win}`,
		`verify_windows_installer_archive "$ROOT/dist/${ARTIFACT_BASE}-windows-${arch}.zip"`,
		`assert_within_dir "$staging" "$staging_parent"`,
		`rm -rf -- "$staging"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("portable packaging is missing %q", want)
		}
	}
}
