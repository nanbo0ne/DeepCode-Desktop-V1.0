package config

import (
	"os"
	"strings"
	"testing"
)

func TestExpandVars(t *testing.T) {
	t.Setenv("DEEPSEEK_ORCA_TEST_TOKEN", "sk-123")
	t.Setenv("DEEPSEEK_ORCA_TEST_EMPTY", "")

	cases := []struct{ in, want string }{
		{"Bearer ${DEEPSEEK_ORCA_TEST_TOKEN}", "Bearer sk-123"},
		{"${DEEPSEEK_ORCA_TEST_MISSING}", ""},                                        // unset, no default → empty
		{"${DEEPSEEK_ORCA_TEST_MISSING:-fallback}", "fallback"},                      // unset → default
		{"${DEEPSEEK_ORCA_TEST_EMPTY:-fallback}", "fallback"},                        // set-but-empty → default
		{"${DEEPSEEK_ORCA_TEST_TOKEN:-fallback}", "sk-123"},                          // set → value, default ignored
		{"no vars here", "no vars here"},                                             // untouched
		{"a${DEEPSEEK_ORCA_TEST_TOKEN}b${DEEPSEEK_ORCA_TEST_MISSING}c", "ask-123bc"}, // multiple refs
	}
	for _, c := range cases {
		if got := ExpandVars(c.in); got != c.want {
			t.Errorf("ExpandVars(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExpandedPlugin(t *testing.T) {
	t.Setenv("DEEPSEEK_ORCA_TEST_KEY", "secret")
	e := PluginEntry{
		Name:    "x",
		Type:    "http",
		URL:     "https://api/${DEEPSEEK_ORCA_TEST_MISSING:-v1}",
		Args:    []string{"--token", "${DEEPSEEK_ORCA_TEST_KEY}"},
		Env:     map[string]string{"K": "${DEEPSEEK_ORCA_TEST_KEY}"},
		Headers: map[string]string{"Authorization": "Bearer ${DEEPSEEK_ORCA_TEST_KEY}"},
	}
	out := e.ExpandedPlugin()
	if out.URL != "https://api/v1" {
		t.Errorf("URL = %q", out.URL)
	}
	if out.Args[1] != "secret" {
		t.Errorf("Args = %v", out.Args)
	}
	if out.Env["K"] != "secret" || out.Headers["Authorization"] != "Bearer secret" {
		t.Errorf("env/headers not expanded: %v %v", out.Env, out.Headers)
	}
	// The original entry must be untouched (we returned a copy).
	if e.Headers["Authorization"] != "Bearer ${DEEPSEEK_ORCA_TEST_KEY}" {
		t.Error("ExpandedPlugin mutated the original entry")
	}
}

func TestConfigExpandPluginUsesScopedEnvironment(t *testing.T) {
	key := "ORCA_CONFIG_SCOPED_PLUGIN"
	t.Setenv(key, "host-value")
	projectKey := "ORCA_CONFIG_PROJECT_PLUGIN"
	t.Setenv(projectKey, "")
	os.Unsetenv(projectKey)
	cfg := &Config{env: map[string]string{projectKey: "project-value"}}
	e := PluginEntry{
		Type:    "http",
		URL:     "https://${ORCA_CONFIG_PROJECT_PLUGIN}",
		Headers: map[string]string{"Authorization": "Bearer ${ORCA_CONFIG_SCOPED_PLUGIN}"},
		Env:     map[string]string{"PROJECT": "${ORCA_CONFIG_PROJECT_PLUGIN}"},
	}
	out := cfg.ExpandPlugin(e)
	if out.URL != "https://project-value" || out.Env["PROJECT"] != "project-value" || out.Headers["Authorization"] != "Bearer host-value" {
		t.Fatalf("scoped plugin expansion = %+v", out)
	}
	if _, ok := os.LookupEnv(projectKey); ok {
		t.Fatal("scoped plugin expansion leaked project env into process")
	}
}

func TestLoadedPluginExpandedPluginUsesConfigEnvironment(t *testing.T) {
	projectKey := "ORCA_CONFIG_LOADED_PLUGIN"
	t.Setenv(projectKey, "")
	os.Unsetenv(projectKey)
	cfg := &Config{
		env: map[string]string{projectKey: "loaded-project-value"},
		Plugins: []PluginEntry{{
			Name: "loaded",
			Type: "http",
			URL:  "https://${ORCA_CONFIG_LOADED_PLUGIN}",
			Env:  map[string]string{"PROJECT": "${ORCA_CONFIG_LOADED_PLUGIN}"},
		}},
	}
	bindProviderEnv(cfg)
	out := cfg.Plugins[0].ExpandedPlugin()
	if out.URL != "https://loaded-project-value" || out.Env["PROJECT"] != "loaded-project-value" {
		t.Fatalf("loaded plugin expansion = %+v", out)
	}
	explicit := cfg.ExpandPlugin(cfg.Plugins[0])
	if explicit.URL != "https://loaded-project-value" || explicit.Env["PROJECT"] != "loaded-project-value" {
		t.Fatalf("explicit loaded plugin expansion = %+v", explicit)
	}
	if cfg.Plugins[0].URL != "https://${ORCA_CONFIG_LOADED_PLUGIN}" {
		t.Fatal("ExpandedPlugin mutated the bound config entry")
	}
	if rendered := RenderTOML(cfg); strings.Contains(rendered, "loaded-project-value") {
		t.Fatal("private plugin environment scope was serialized")
	}
}
