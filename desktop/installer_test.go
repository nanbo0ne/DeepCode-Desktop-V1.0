package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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

func TestWindowsPackagingCreatesFreshPayloadParent(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	body, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(script, "resolve_absolute_path()")
	end := strings.Index(script, "\tlocal node_arch node_archive")
	if start < 0 || end <= start {
		t.Fatal("cannot locate packaging directory preparation")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "desktop", "build", "windows", "installer"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "--noprofile", "--norc", "-s")
	cmd.Env = append(os.Environ(), "ROOT="+filepath.ToSlash(root), "os=windows")
	cmd.Stdin = strings.NewReader("set -eu\n" + script[start:end] + "}\nprepare_windows_installer_resources\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fresh packaging preparation: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, "desktop", "build", "windows", "installer-go", "payload")); err != nil {
		t.Fatal(err)
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

func TestWindowsArchiveVerificationDoesNotRunNSISCheckOnZIP(t *testing.T) {
	body, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	start := strings.Index(script, "verify_windows_installer_archive() {")
	if start < 0 {
		t.Fatal("Windows archive verifier is missing")
	}
	relEnd := strings.Index(script[start:], "\n}\n\n# Bounded")
	if relEnd < 0 {
		t.Fatal("could not isolate Windows archive verifier")
	}
	verifier := script[start : start+relEnd]
	if !strings.Contains(verifier, `*.exe) go -C "$ROOT/desktop" run ./cmd/nsischeck "$installer" ;;`) {
		t.Fatal("NSIS checker must be restricted to .exe inputs")
	}
	if strings.Contains(verifier, `nsischeck "$ROOT/dist/${ARTIFACT_BASE}-windows-${arch}.zip"`) {
		t.Fatal("ZIP must not be passed to the NSIS checker")
	}
	if !strings.Contains(verifier, `"$seven_zip" t "$installer"`) {
		t.Fatal("Windows archive verification must retain the 7-Zip check")
	}
	if !strings.Contains(script, `verify_windows_installer_archive "$packaged_installer"`) ||
		!strings.Contains(script, `verify_windows_installer_archive "$ROOT/dist/${ARTIFACT_BASE}-windows-${arch}.zip"`) {
		t.Fatal("Windows installer and ZIP callers must both use the archive verifier")
	}
}

func readWindowsInstallerAcceptance(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../scripts/test-desktop-installer.ps1")
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(body), "\r\n", "\n")
}

func TestWindowsInstallerAcceptanceSourceContracts(t *testing.T) {
	script := readWindowsInstallerAcceptance(t)
	contracts := map[string][]string{
		"runner_and_paths": {
			`$env:GITHUB_ACTIONS -cne 'true'`,
			`$env:RUNNER_ENVIRONMENT -cne 'github-hosted'`,
			`$env:RUNNER_OS -cne 'Windows'`,
			`$env:RUNNER_ARCH -cne 'X64'`,
			`-not $IsWindows -or -not [Environment]::Is64BitProcess`,
			`$runnerTemp = Get-PlainPath $env:RUNNER_TEMP`,
			`$runnerTemp.Length -le 3`,
			`[IO.Path]::GetFullPath($Path)`,
			`[IO.FileAttributes]::ReparsePoint`,
			`$full.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase)`,
			`$ownedRoot = Assert-ChildPath (Join-Path $runnerTemp ('orca installer acceptance ' + [guid]::NewGuid().ToString('N'))) $runnerTemp`,
			`if (Test-Path -LiteralPath $ownedRoot) { throw`,
			`$repo -ine $workspace`,
			`@('CurrentUser', 'LocalMachine')`,
			`@('Registry32', 'Registry64')`,
			`Runner is not clean: existing`,
			`Preinstalled WebView2 is required`,
		},
		"official_baseline": {
			`https://api.github.com/repos/nanbo0ne/O.R.C.A-for-Windows/releases/tags/desktop-v3.0.2`,
			`https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/`,
			`$release.tag_name -cne 'desktop-v3.0.2' -or $release.draft -or $release.prerelease`,
			`$assetName = 'O.R.C.A-for-Windows-windows-amd64-installer.exe'`,
			`@($assetName, 'SHA256SUMS.txt')`,
			`$assets.Count -ne 1`,
			`$checksumRows.Count -ne 1`,
			`[regex]::Escape($assetName)`,
			`$oldHash -ine $checksumRows[0].Groups[1].Value -or $oldHash -cne $pinnedOldHash`,
			`5bf27fd4d958fc389a2ef320e401d05875d379b4d96c4ac64a8e8288ea48894d`,
			`Invoke-WebRequest -Uri $direct -OutFile $destination -TimeoutSec 120`,
		},
		"bounded_silent_processes": {
			`$allowedExecutables.Contains($exe)`,
			`$info.Arguments = $Arguments`,
			`$info.UseShellExecute = $false`,
			`$info.Environment.Remove('GH_TOKEN')`,
			`$process.WaitForExit(120000)`,
			`$process.Kill($true)`,
			`120000 - $timer.ElapsedMilliseconds`,
			`$streams.Wait($remaining)`,
			`$process.ExitCode -ne 0`,
			`Owned-Path 'upgrade target with spaces'`,
			`Owned-Path 'fresh target with spaces'`,
			`Invoke-BoundedProcess $oldInstaller "/S /D=$upgradeDir" 'install-302'`,
			`Invoke-BoundedProcess $newInstaller '/S' 'upgrade-current'`,
			`Invoke-BoundedProcess $newInstaller "/S /D=$freshDir" 'install-fresh'`,
			`Invoke-BoundedProcess $uninstaller "/S _?=$target" $Label`,
			`Assert-NoApplication`,
			`$evidence.status = 'failed'`,
			"$evidence['error'] = $_.Exception.Message\n    throw",
		},
		"isolated_data_and_restore": {
			`'AppData' = Owned-Path 'profile\AppData\Roaming'`,
			`'Local AppData' = Owned-Path 'profile\AppData\Local'`,
			`@('User Shell Folders', 'Shell Folders')`,
			`[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames`,
			`$folderTargets[$saved.Name], $saved.Kind`,
			`@('System32', 'SysWOW64')`,
			`[Environment]::GetFolderPath('ApplicationData')`,
			`[Environment]::GetFolderPath('LocalApplicationData')`,
			`NSIS shell folder isolation failed`,
			`sessions\synthetic-session.json`,
			`models\synthetic-tiny.gguf`,
			`$markerHashes[$safe] = Get-SHA256 $safe`,
			`(Get-SHA256 $path) -cne $markerHashes[$path]`,
			`Assert-Markers 'installed-302'`,
			`Assert-Markers 'upgraded-current'`,
			`Assert-Markers $Label`,
			`Invoke-DefaultUninstall $freshDir 'uninstall-fresh'`,
			`Invoke-DefaultUninstall $upgradeDir 'uninstall-upgraded'`,
			`$key.SetValue($saved.Name, $saved.Value, $saved.Kind)`,
			`throw 'Failed to restore runner shell folder mappings.'`,
		},
		"payload_versions_and_evidence": {
			`desktop\build\bin\Orca.exe`,
			`desktop\build\windows\installer-go\payload`,
			`'node.exe', 'LICENSE.node.txt', 'codegraph\node.exe', 'codegraph\bin\codegraph.cmd'`,
			`Get-ChildItem -Force -Recurse -LiteralPath $codegraph`,
			`Get-FileHash -LiteralPath $safe -Algorithm SHA256`,
			`(Get-SHA256 $installed) -cne $expectedHashes[$relative]`,
			`$actualFiles.Count -ne $expectedCount`,
			`$key.GetValue('InstallLocation')`,
			`$key.GetValue('DisplayVersion')`,
			`GetVersionInfo($installedApp).ProductVersion`,
			`$location -ine $target`,
			`Assert-Installation $upgradeDir '3.0.2' 'installed-302'`,
			`Assert-Installation $upgradeDir $productVersion 'upgraded-current'`,
			`Assert-CurrentPayload $freshDir 'fresh-directory'`,
			`Assert-CurrentPayload $upgradeDir 'upgraded-current'`,
			`Assert-CurrentPayload $upgradeDir 'original-directory-unchanged'`,
			`'codegraph', 'uninstall.exe'`,
			`Owned-Path 'evidence\result.json'`,
		},
	}
	for name, required := range contracts {
		t.Run(name, func(t *testing.T) {
			for _, want := range required {
				if !strings.Contains(script, want) {
					t.Errorf("installer acceptance is missing %q", want)
				}
			}
		})
	}
	for _, forbidden := range []string{
		"/NCRC", "Remove-Item", "Directory]::Delete", "File]::Delete", "Remove-ItemProperty",
		"DeleteSubKey", "Start-Process", "Invoke-Expression", "$info.ArgumentList",
		"$allowedExecutables.Add($app)", "$allowedExecutables.Add($candidate)",
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("installer acceptance must not contain %q", forbidden)
		}
	}
	guard := strings.Index(script, "$env:GITHUB_ACTIONS -cne 'true'")
	functions := strings.Index(script, "function Get-PlainPath")
	firstWrite := strings.Index(script, "[void][IO.Directory]::CreateDirectory($ownedRoot)")
	checksum := strings.Index(script, "throw 'Official 3.0.2 installer SHA256 mismatch.'")
	install := strings.Index(script, `Invoke-BoundedProcess $oldInstaller "/S /D=$upgradeDir"`)
	if guard < 0 || functions <= guard || firstWrite <= functions || checksum < 0 || install <= checksum {
		t.Fatal("runner guards must precede side effects; baseline verification must precede installation")
	}
	restore := strings.LastIndex(script, "} finally {")
	if restore < 0 || !strings.Contains(script[restore:], "ConvertTo-Json -Depth 10") {
		t.Fatal("acceptance must write evidence even on failure")
	}
}

func TestWindowsInstallerAcceptanceWorkflowGate(t *testing.T) {
	body, err := os.ReadFile("../.github/workflows/release-desktop.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	integrity := strings.Index(workflow, "- name: Verify Windows package integrity")
	acceptance := strings.Index(workflow, "- name: Test Windows installer upgrade and data retention")
	signing := strings.Index(workflow, "- name: Sign artifacts (minisign)")
	if integrity < 0 || acceptance <= integrity || signing <= acceptance {
		t.Fatal("Windows real installer acceptance must run after integrity and before minisign")
	}
	step := workflow[acceptance:signing]
	for _, want := range []string{
		"if: runner.os == 'Windows'\n", "shell: pwsh", "timeout-minutes: 15",
		"GH_TOKEN: ${{ github.token }}", "ORCA_INSTALLER_EXPECTED_VERSION: ${{ steps.ver.outputs.version }}",
		"run: ./scripts/test-desktop-installer.ps1 -ExpectedVersion $env:ORCA_INSTALLER_EXPECTED_VERSION",
	} {
		if !strings.Contains(step, want) {
			t.Errorf("installer acceptance workflow step is missing %q", want)
		}
	}
	for _, forbidden := range []string{"continue-on-error", "allow_unsigned_windows", "upload-artifact", "canary", "MINISIGN_PRIVATE_KEY"} {
		if strings.Contains(step, forbidden) {
			t.Errorf("installer acceptance must be a mandatory isolated gate, found %q", forbidden)
		}
	}
}
