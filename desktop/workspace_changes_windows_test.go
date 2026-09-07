//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWorkspaceGitStatusWithShortWindowsPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	pointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	n, err := windows.GetShortPathName(pointer, &buffer[0], uint32(len(buffer)))
	if err != nil || n == 0 || int(n) >= len(buffer) {
		t.Fatalf("GetShortPathName: %v, length %d", err, n)
	}
	short := windows.UTF16ToString(buffer[:n])
	if strings.EqualFold(short, root) {
		t.Skip("the test volume does not generate 8.3 aliases")
	}
	if output, err := workspaceGit("-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"outside.txt", "sub/ leading.txt"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("test\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := workspaceGitStatus(short)
	if err != nil || len(entries) != 2 {
		t.Fatalf("short root status: %v, %+v", err, entries)
	}
	entries, err = workspaceGitStatus(filepath.Join(short, "sub"))
	if err != nil || len(entries) != 1 || entries[0].Path != " leading.txt" {
		t.Fatalf("short subdirectory status: %v, %+v", err, entries)
	}
}
