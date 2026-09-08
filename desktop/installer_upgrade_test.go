package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readWindowsInstallerSource(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("build", "windows", "installer", "project.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestWindowsInstallerUpgradeAndUninstallContracts(t *testing.T) {
	script := readWindowsInstallerSource(t)

	for _, want := range []string{
		`InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"`,
		`!define ORCA_INSTALLDIR_SENTINEL "$LOCALAPPDATA\Programs\O.R.C.A for Windows.__nsis_default__"`,
		`InstallDir "${ORCA_INSTALLDIR_SENTINEL}"`,
		`StrCmp $INSTDIR "${ORCA_INSTALLDIR_SENTINEL}" use_compat_install_dir install_dir_done`,
		`!define REQUEST_EXECUTION_LEVEL "user"`,
		`ManifestDPIAware true`,
		`Call orca.closeTargetProcesses`,
		`Call un.orca.closeTargetProcesses`,
		`Get-TargetProcesses`,
		`CloseMainWindow`,
		`AddSeconds(5)`,
		`IfSilent close_target_processes_silent_failed close_target_processes_prompt`,
		`WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"`,
		`Delete "$INSTDIR\uninstall.bat"`,
		`RMDir "$INSTDIR"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("installer is missing %q", want)
		}
	}
	userLevel := strings.Index(script, `!define REQUEST_EXECUTION_LEVEL "user"`)
	include := strings.Index(script, `!include "wails_tools.nsh"`)
	if userLevel < 0 || include <= userLevel {
		t.Fatal("the user execution level must override Wails before its generated include")
	}

	if strings.Contains(script, `taskkill.exe" /IM`) {
		t.Fatal("installer must not terminate every Orca.exe by image name")
	}
	if strings.Contains(script, `File /oname=uninstall.bat`) {
		t.Fatal("new installs must not ship the legacy unsafe uninstall batch")
	}
	if strings.Contains(script, `RMDir /r "$INSTDIR"`) {
		t.Fatal("uninstaller must not recursively remove the selected install directory")
	}

	onInit := strings.Index(script, "Function .onInit")
	if onInit < 0 {
		t.Fatal("installer is missing .onInit")
	}
	onInitBody := script[onInit:]
	explicit := strings.Index(onInitBody, `StrCmp $INSTDIR "${ORCA_INSTALLDIR_SENTINEL}" use_compat_install_dir install_dir_done`)
	current := strings.Index(onInitBody, `ReadRegStr $0 HKCU "${UNINST_KEY}" "InstallLocation"`)
	legacy := strings.Index(onInitBody, `ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "InstallLocation"`)
	if explicit < 0 || current < 0 || legacy < 0 || explicit > current || current > legacy {
		t.Fatalf("install directory precedence is not /D, current registry, legacy registry: explicit=%d current=%d legacy=%d", explicit, current, legacy)
	}
	if strings.Contains(onInitBody, `StrCmp $INSTDIR "" 0 done`) {
		t.Fatal("legacy detection is still short-circuited by the InstallDir default")
	}
	if !strings.Contains(onInitBody, "install_dir_default:") {
		t.Fatal("missing final default install path fallback")
	}

	uninstallStart := strings.Index(script, `Section "uninstall"`)
	if uninstallStart < 0 {
		t.Fatal("installer is missing the uninstall section")
	}
	uninstall := script[uninstallStart:]
	deleteData := strings.Index(uninstall, `${If} $DeleteSavedData == ${BST_CHECKED}`)
	webviewData := strings.Index(uninstall, `RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"`)
	if deleteData < 0 || webviewData < deleteData {
		t.Fatal("WebView2 data must be removed only inside the explicit delete-data branch")
	}
}

func TestWindowsInstallerClosesOnlyExactRuntimePaths(t *testing.T) {
	script := readWindowsInstallerSource(t)
	want := `$$targetPaths = @([IO.Path]::Combine($$targetDir, 'Orca.exe'), [IO.Path]::Combine($$targetDir, 'deepseek-orca-desktop.exe'), [IO.Path]::Combine($$targetDir, 'node.exe'), [IO.Path]::Combine($$targetDir, 'codegraph', 'node.exe'))`
	if strings.Count(script, want) != 2 {
		t.Fatalf("installer must use the exact root and codegraph node paths in install and uninstall, count=%d", strings.Count(script, want))
	}
	if !strings.Contains(script, `Get-Process -Name $$names -ErrorAction SilentlyContinue | Where-Object`) {
		t.Fatal("runtime process matching must filter by resolved full path")
	}
	if strings.Contains(script, `taskkill.exe" /IM`) || strings.Contains(script, `taskkill /IM`) {
		t.Fatal("installer must not terminate runtimes by image name alone")
	}
}

func TestWindowsProductNamePreservesUpgradeIdentifiers(t *testing.T) {
	data, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"productName": "O.R.C.A. for Windows"`) {
		t.Fatal("Windows display name must match the canonical product name")
	}
	script := readWindowsInstallerSource(t)
	for _, want := range []string{
		`!define UNINST_KEY_NAME "O.R.C.A for Windows"`,
		`IfFileExists "$DESKTOP\O.R.C.A for Windows.lnk" shortcut_choice_done 0`,
		`Delete "$DESKTOP\O.R.C.A for Windows.lnk"`,
		`StrCmp $0 $1 0 legacy_cleanup_done`,
		`Delete "$INSTDIR\deepseek-orca-desktop.exe"`,
		`DeleteRegKey HKCU "${LEGACY_UNINST_KEY}"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("installer lost identity compatibility: %s", want)
		}
	}
}

func TestWindowsInstallerCompilesWithMakensis(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("makensis contract compilation is Windows-only")
	}

	makensis := os.Getenv("MAKENSIS")
	if makensis == "" {
		for _, candidate := range []string{
			"C:\\Program Files (x86)\\NSIS\\makensis.exe",
			"C:\\Program Files\\NSIS\\makensis.exe",
		} {
			if _, err := os.Stat(candidate); err == nil {
				makensis = candidate
				break
			}
		}
	}
	if makensis == "" {
		makensis, _ = exec.LookPath("makensis.exe")
	}
	if makensis == "" {
		t.Skip("makensis is not installed")
	}

	const resultName = "orca-nsis-parameters-probe.txt"
	resultPath := filepath.Join(os.TempDir(), resultName)
	_ = os.Remove(resultPath)
	t.Cleanup(func() {
		_ = os.Remove(resultPath)
	})

	script := "Unicode true\n" +
		"SilentInstall silent\n" +
		"AutoCloseWindow true\n" +
		"RequestExecutionLevel user\n" +
		"InstallDir \"$TEMP\\\\orca-nsis-sentinel\"\n" +
		"OutFile \"probe.exe\"\n" +
		"!include \"FileFunc.nsh\"\n\n" +
		"Function .onInit\n" +
		"    ${GetParameters} $0\n" +
		"    FileOpen $1 \"$TEMP\\" + resultName + "\" w\n" +
		"    FileWrite $1 \"CMDLINE=$CMDLINE$\\r$\\n\"\n" +
		"    FileWrite $1 \"PARAMS=$0$\\r$\\n\"\n" +
		"    FileWrite $1 \"INSTDIR=$INSTDIR$\\r$\\n\"\n" +
		"    FileClose $1\n" +
		"    Abort\n" +
		"FunctionEnd\n\n" +
		"Section\n" +
		"SectionEnd\n"
	scriptPath := filepath.Join(t.TempDir(), "probe.nsi")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	scriptDir := filepath.Dir(scriptPath)
	compile := exec.Command(makensis, "/V1", scriptPath)
	compile.Dir = scriptDir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("synthetic makensis probe failed: %v\n%s", err, out)
	}

	explicit := filepath.Join(os.TempDir(), "orca-nsis-explicit-target")
	_ = exec.Command(filepath.Join(scriptDir, "probe.exe"), "/D="+explicit).Run()
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("NSIS probe did not write %s: %v", resultPath, err)
	}
	got := string(raw)
	if strings.Contains(got, "/D=") {
		t.Fatalf("NSIS exposed /D in $CMDLINE or GetParameters: %q", got)
	}
	if !strings.Contains(got, "INSTDIR="+explicit) {
		t.Fatalf("NSIS did not preserve explicit /D path: %q", got)
	}
	if strings.Contains(got, "PARAMS=/D=") {
		t.Fatalf("GetParameters unexpectedly included /D: %q", got)
	}
}
