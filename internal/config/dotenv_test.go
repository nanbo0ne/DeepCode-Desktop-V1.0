package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnvFallsBackToHome(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("KEY_CWD=from_cwd\nKEY_SHARED=cwd_wins\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("KEY_HOME=from_home\nKEY_SHARED=home_loses\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, key := range []string{"KEY_CWD", "KEY_HOME", "KEY_SHARED"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	scope := loadDotEnv()
	if scope["KEY_CWD"] != "from_cwd" || scope["KEY_HOME"] != "from_home" || scope["KEY_SHARED"] != "cwd_wins" {
		t.Fatalf("dotenv scope = %#v", scope)
	}
	if _, ok := os.LookupEnv("KEY_CWD"); ok {
		t.Fatal("project dotenv value leaked into the process environment")
	}
}

func TestLoadDotEnvReadsGlobalCredentials(t *testing.T) {
	cwd := t.TempDir()
	cfgHome := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HOME", cfgHome)
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))
	cred := UserCredentialsPath()
	if cred == "" {
		t.Skip("user config dir unresolved on this platform")
	}
	if err := os.MkdirAll(filepath.Dir(cred), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte("KEY_GLOBAL=from_credentials\nKEY_SHARED=global_loses\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("KEY_SHARED=cwd_wins\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"KEY_GLOBAL", "KEY_SHARED"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	scope := loadDotEnv()
	if scope["KEY_GLOBAL"] != "from_credentials" || scope["KEY_SHARED"] != "cwd_wins" {
		t.Fatalf("dotenv scope = %#v", scope)
	}
}

func TestLoadDotEnvDoesNotOverrideEnv(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("PINNED=from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PINNED", "from_env")

	scope := loadDotEnv()
	if os.Getenv("PINNED") != "from_env" || scope["PINNED"] != "" {
		t.Fatalf("host env priority failed: process=%q scope=%q", os.Getenv("PINNED"), scope["PINNED"])
	}
}

func TestLoadForRootKeepsProjectProviderKeysIsolated(t *testing.T) {
	key := "ORCA_CONFIG_SCOPE_TEST_KEY"
	t.Setenv(key, "")
	os.Unsetenv(key)
	rootA, rootB := t.TempDir(), t.TempDir()
	providerTOML := "[[providers]]\nname = \"scoped\"\nkind = \"openai\"\nbase_url = \"https://example.invalid/v1\"\nmodel = \"test-model\"\napi_key_env = \"" + key + "\"\n"
	for _, root := range []string{rootA, rootB} {
		if err := os.WriteFile(filepath.Join(root, "deepseek-orca.toml"), []byte(providerTOML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(rootA, ".env"), []byte(key+"=project-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, ".env"), []byte(key+"=project-b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgA, err := LoadForRoot(rootA)
	if err != nil {
		t.Fatal(err)
	}
	cfgB, err := LoadForRoot(rootB)
	if err != nil {
		t.Fatal(err)
	}
	pA, okA := cfgA.ResolveModel("scoped/test-model")
	pB, okB := cfgB.ResolveModel("scoped/test-model")
	if !okA || !okB || pA.APIKey() != "project-a" || pB.APIKey() != "project-b" {
		t.Fatalf("scoped provider keys = (%q, %v), (%q, %v)", pA.APIKey(), okA, pB.APIKey(), okB)
	}
	if _, ok := os.LookupEnv(key); ok {
		t.Fatal("project provider key leaked into the process environment")
	}
}

func TestLoadForRootHostProviderKeyWins(t *testing.T) {
	key := "ORCA_CONFIG_HOST_PRIORITY_KEY"
	t.Setenv(key, "host-value")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(key+"=project-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deepseek-orca.toml"), []byte("[[providers]]\nname = \"scoped\"\nkind = \"openai\"\nbase_url = \"https://example.invalid/v1\"\nmodel = \"test-model\"\napi_key_env = \""+key+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.ResolveModel("scoped/test-model")
	if !ok || p.APIKey() != "host-value" {
		t.Fatalf("host provider key = (%q, %v), want host-value", p.APIKey(), ok)
	}
}

func TestConfigEnvironmentMergesHostAndProjectWithoutGlobalMutation(t *testing.T) {
	projectKey := "ORCA_CONFIG_PROJECT_ENV"
	hostKey := "ORCA_CONFIG_HOST_ENV"
	t.Setenv(hostKey, "host-value")
	t.Setenv(projectKey, "")
	os.Unsetenv(projectKey)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(projectKey+"=project-value\n"+hostKey+"=project-wins-not-allowed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deepseek-orca.toml"), []byte("default_model = \"deepseek-flash\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := cfg.Env(projectKey); !ok || got != "project-value" {
		t.Fatalf("project Env = %q, %v", got, ok)
	}
	if got, ok := cfg.Env(hostKey); !ok || got != "host-value" {
		t.Fatalf("host Env = %q, %v", got, ok)
	}
	if _, ok := os.LookupEnv(projectKey); ok {
		t.Fatal("project dotenv value leaked into process environment")
	}
	entries := cfg.Environment()
	if got, ok := entries[projectKey]; !ok || got != "project-value" {
		t.Fatalf("merged project entry = %q, %v", got, ok)
	}
	if got, ok := entries[hostKey]; !ok || got != "host-value" {
		t.Fatalf("merged host entry = %q, %v", got, ok)
	}
	entries[projectKey] = "changed-only-in-copy"
	if got, _ := cfg.Env(projectKey); got != "project-value" {
		t.Fatal("mutating Environment result changed config scope")
	}
}

func TestProviderEntryWithAPIKeyIsInMemoryOnly(t *testing.T) {
	keyEnv := "ORCA_CONFIG_MEMORY_KEY"
	t.Setenv(keyEnv, "host-key")
	e := ProviderEntry{Name: "memory", APIKeyEnv: keyEnv}
	e.WithAPIKey("session-token")
	if got := e.APIKey(); got != "session-token" {
		t.Fatalf("APIKey override = %q, want session-token", got)
	}
	rendered := RenderTOML(&Config{Providers: []ProviderEntry{e}})
	if strings.Contains(rendered, "session-token") {
		t.Fatal("in-memory API key was persisted to TOML")
	}
}
