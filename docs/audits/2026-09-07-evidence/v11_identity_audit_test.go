package audit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
)

func TestProjectDotEnvLeaksAcrossLoadForRootCalls(t *testing.T) {
	key := "ORCA_AUDIT_PROJECT_KEY"
	old, hadOld := os.LookupEnv(key)
	if hadOld {
		t.Fatalf("test key already exists in process environment")
	}
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})

	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, ".env"), []byte(key+"=project-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, ".env"), []byte(key+"=project-b\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := config.LoadForRoot(rootA); err != nil {
		t.Fatal(err)
	}
	first := os.Getenv(key)
	if _, err := config.LoadForRoot(rootB); err != nil {
		t.Fatal(err)
	}
	second := os.Getenv(key)

	if first != "project-a" || second != "project-a" {
		t.Fatalf("project env resolution = first %q, second %q; audit expects the second workspace not to inherit A", first, second)
	}
}

func TestStaleV11MigrationLockSkipsMigration(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("AppData", configRoot)
	t.Setenv("APPDATA", configRoot)
	t.Setenv("HOME", configRoot)
	t.Setenv("USERPROFILE", configRoot)

	legacyRoot := filepath.Join(configRoot, "deepseek-orca")
	if err := os.MkdirAll(filepath.Join(legacyRoot, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyFile := filepath.Join(legacyRoot, "sessions", "legacy.jsonl")
	if err := os.WriteFile(legacyFile, []byte("legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, ".orca-v11-migration.lock"), []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := config.EnsureV11StateMigration(); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(configRoot, "orca", "sessions", "legacy.jsonl")
	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Fatalf("stale migration lock result: new file stat error = %v; expected migration to be skipped", err)
	}
}

func TestWindowsConfigRootIsAppDataOrca(t *testing.T) {
	if os.PathSeparator != '\\' {
		t.Skip("Windows path contract")
	}
	appData := t.TempDir()
	localAppData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
	t.Setenv("LOCALAPPDATA", localAppData)

	want := filepath.Join(appData, "orca", "config.toml")
	got := config.UserConfigPath()
	if got != want {
		t.Fatalf("UserConfigPath() = %q, want %q from the Windows implementation contract", got, want)
	}
}

func TestMigrationDoesNotRetryCredentialsAfterConfigWriteFailure(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("AppData", configRoot)
	t.Setenv("APPDATA", configRoot)
	t.Setenv("HOME", configRoot)
	t.Setenv("USERPROFILE", configRoot)

	legacy := filepath.Join(configRoot, ".deepseek-orca", "config.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"apiKey":"audit-key","lang":"en"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Make the config destination writable while making the credentials target
	// fail as a file write, reproducing a partial migration without touching the
	// real profile.
	if err := os.MkdirAll(filepath.Join(configRoot, "orca"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(configRoot, "orca", "credentials"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := config.MigrateLegacyIfNeeded(); err == nil {
		t.Fatal("migration should report the credentials write failure")
	}
	if _, err := os.Stat(config.UserConfigPath()); err != nil {
		t.Fatalf("partial migration did not leave the config file for retry demonstration: %v", err)
	}
	res, err := config.MigrateLegacyIfNeeded()
	if err != nil || res != nil {
		t.Fatalf("second migration = result %v, error %v; expected the existing config to suppress retry", res, err)
	}
	if _, err := os.Stat(filepath.Join(configRoot, "orca", "credentials", "DEEPSEEK_API_KEY")); !os.IsNotExist(err) {
		t.Fatalf("unexpected credentials artifact: %v", err)
	}
}
