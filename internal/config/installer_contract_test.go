package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsInstallerDeletesCanonicalAppDataRootWhenRequested(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	body, err := os.ReadFile(filepath.Join(repoRoot, "desktop", "build", "windows", "installer", "project.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	start := strings.Index(text, `${If} $DeleteSavedData == ${BST_CHECKED}`)
	end := strings.Index(text[start:], `${EndIf}`)
	if start < 0 || end < 0 {
		t.Fatal("saved-data uninstall block is missing")
	}
	block := text[start : start+end]
	if !strings.Contains(block, `RMDir /r "$AppData\orca"`) {
		t.Fatal("saved-data uninstall block must remove the canonical %APPDATA%\\orca root")
	}
}
