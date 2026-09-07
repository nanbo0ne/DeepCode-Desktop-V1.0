package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func isolateStateMigrationProfile(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("AppData", root)
	t.Setenv("APPDATA", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	return root
}

func TestAcquireMigrationLockSerializesTwoAcquisitionsAndRelease(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".orca-v11-migration.lock")
	release, acquired, err := acquireMigrationLock(lockPath)
	if err != nil || !acquired {
		t.Fatalf("first acquisition = release=%v acquired=%v err=%v", release != nil, acquired, err)
	}
	secondRelease, secondAcquired, secondErr := acquireMigrationLock(lockPath)
	var busy *MigrationBusyError
	if secondRelease != nil || secondAcquired || !errors.As(secondErr, &busy) || !errors.Is(secondErr, ErrMigrationBusy) {
		t.Fatalf("second acquisition = release=%v acquired=%v err=%v, want typed busy", secondRelease != nil, secondAcquired, secondErr)
	}
	release()
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file must remain after release: %v", err)
	}
	thirdRelease, thirdAcquired, thirdErr := acquireMigrationLock(lockPath)
	if thirdErr != nil || !thirdAcquired || thirdRelease == nil {
		t.Fatalf("acquisition after release = acquired=%v err=%v", thirdAcquired, thirdErr)
	}
	thirdRelease()
}

func TestEnsureV11StateMigrationLeavesPartialTargetAndNoMarker(t *testing.T) {
	isolateStateMigrationProfile(t)
	configRoot, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(configRoot, "deepseek-orca", "sessions", "partial.jsonl")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("complete legacy file"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(configRoot, "orca", "sessions", "partial.jsonl")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = EnsureV11StateMigration()
	var conflict *MigrationConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("partial destination error = %v, want conflict", err)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || string(got) != "truncated" {
		t.Fatalf("partial destination was overwritten: %q, err=%v", got, readErr)
	}
	if migrationMarkerValid(filepath.Join(configRoot, "orca", v11MigrationMarker)) {
		t.Fatal("unresolved partial destination must not receive a success marker")
	}
}

func TestCopyTreeMissingPreservesConflictingNewFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "legacy")
	target := filepath.Join(root, "new")
	path := filepath.Join(source, "sessions", "same.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(target, "sessions", "same.jsonl")
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := copyTreeMissing(source, target)
	var conflict *MigrationConflictError
	if !errors.As(err, &conflict) || len(conflict.Paths) != 1 {
		t.Fatalf("copy conflict error = %v, want one conflict", err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "new" {
		t.Fatalf("existing new file = %q, err=%v", got, err)
	}
}

func TestCopyTreeMissingSkipsIdenticalExistingFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "legacy")
	target := filepath.Join(root, "new")
	sourceFile := filepath.Join(source, "same.jsonl")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceFile, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "same.jsonl"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyTreeMissing(source, target); err != nil {
		t.Fatalf("identical existing file should be complete: %v", err)
	}
}

func TestCopyTreeMissingRecoversOrphanedCopyTemp(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "legacy")
	target := filepath.Join(root, "new")
	sourceFile := filepath.Join(source, "sessions", "resume.jsonl")
	if err := os.MkdirAll(filepath.Dir(sourceFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceFile, []byte("complete legacy state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sessions", ".migration-v11-copy-orphan.tmp"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyTreeMissing(source, target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "sessions", "resume.jsonl"))
	if err != nil || string(got) != "complete legacy state" {
		t.Fatalf("recovered copy = %q, err=%v", got, err)
	}
}
