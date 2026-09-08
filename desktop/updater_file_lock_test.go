//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdatePackageLockedAgainstReplacement(t *testing.T) {
	name := filepath.Join(t.TempDir(), "installer.exe")
	if err := os.WriteFile(name, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openUpdatePackage(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.WriteFile(name, []byte("changed"), 0600); err == nil {
		t.Fatal("verified installer remained writable")
	}
	if err := os.Rename(name, name+".old"); err == nil {
		t.Fatal("verified installer could be replaced")
	}
	if _, err := os.ReadFile(name); err != nil {
		t.Fatalf("installer could not read the locked file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
}
