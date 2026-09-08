package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestAgentSubagentModelConfigDecodesFromTOML(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`
[agent]
subagent_model = "deepseek-pro"
subagent_models = { explore = "deepseek-pro", "security-review" = "mimo-pro" }
`, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if cfg.Agent.SubagentModel != "deepseek-pro" {
		t.Fatalf("subagent_model = %q, want deepseek-pro", cfg.Agent.SubagentModel)
	}
	if cfg.Agent.SubagentModels["explore"] != "deepseek-pro" {
		t.Fatalf("explore model = %q", cfg.Agent.SubagentModels["explore"])
	}
	if cfg.Agent.SubagentModels["security-review"] != "mimo-pro" {
		t.Fatalf("security-review model = %q", cfg.Agent.SubagentModels["security-review"])
	}
	if cfg.Agent.SubagentEffort != "" {
		t.Fatalf("subagent_effort should be empty by default, got %q", cfg.Agent.SubagentEffort)
	}
	if len(cfg.Agent.SubagentEfforts) != 0 {
		t.Fatalf("subagent_efforts should be empty by default, got %v", cfg.Agent.SubagentEfforts)
	}
}

func TestResolveVisionModelRefUsesExplicitRole(t *testing.T) {
	cfg := &Config{
		Agent: AgentConfig{
			SubagentModel:  "qwen/general",
			SubagentModels: map[string]string{VisionSubagentRole: "qwen/qwen-vl"},
		},
		Providers: []ProviderEntry{{Name: "qwen", Kind: "openai", BaseURL: "https://qwen.example/v1", Models: []string{"qwen-general", "qwen-vl"}}},
	}

	if got := cfg.ResolveVisionModelRef(); got != "qwen/qwen-vl" {
		t.Fatalf("vision model ref = %q, want explicit role", got)
	}
	if cfg.Agent.SubagentModel != "qwen/general" {
		t.Fatalf("general subagent model was changed to %q", cfg.Agent.SubagentModel)
	}
}

func TestSetVisionModelOnlyChangesVisionRole(t *testing.T) {
	cfg := &Config{
		Agent:     AgentConfig{SubagentModel: "qwen/general"},
		Providers: []ProviderEntry{{Name: "qwen", Kind: "openai", BaseURL: "https://qwen.example/v1", Models: []string{"qwen-general", "qwen-vl"}}},
	}
	if err := cfg.SetVisionModel("qwen/qwen-vl"); err != nil {
		t.Fatalf("SetVisionModel: %v", err)
	}
	if got := cfg.Agent.SubagentModels[VisionSubagentRole]; got != "qwen/qwen-vl" {
		t.Fatalf("vision role = %q, want qwen/qwen-vl", got)
	}
	if cfg.Agent.SubagentModel != "qwen/general" {
		t.Fatalf("general subagent model changed to %q", cfg.Agent.SubagentModel)
	}
	if err := cfg.SetVisionModel(""); err != nil {
		t.Fatalf("clear vision model: %v", err)
	}
	if got := cfg.ResolveVisionModelRef(); got != "" {
		t.Fatalf("cleared vision role resolved to %q", got)
	}
}

func TestResolveVisionModelRefUsesOfficialDeepSeekFallback(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderEntry{
			{Name: "qwen", Kind: "openai", BaseURL: "https://qwen.example/v1", Models: []string{"vision"}},
			{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com", Models: []string{OfficialDeepSeekVisionModel}},
		},
	}

	if got := cfg.ResolveVisionModelRef(); got != "deepseek/"+OfficialDeepSeekVisionModel {
		t.Fatalf("vision model ref = %q, want official DeepSeek fallback", got)
	}
}

func TestResolveVisionModelRefRejectsNonOfficialOrUnrelatedProviders(t *testing.T) {
	cases := []struct {
		name      string
		providers []ProviderEntry
		access    []string
	}{
		{
			name:      "no deepseek",
			providers: []ProviderEntry{{Name: "qwen", Kind: "openai", BaseURL: "https://qwen.example/v1", Models: []string{OfficialDeepSeekVisionModel}}},
		},
		{
			name:      "custom endpoint",
			providers: []ProviderEntry{{Name: "deepseek", Kind: "openai", BaseURL: "https://relay.example/deepseek", Models: []string{OfficialDeepSeekVisionModel}}},
		},
		{
			name:      "provider isolation",
			providers: []ProviderEntry{{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com", Models: []string{OfficialDeepSeekVisionModel}}},
			access:    []string{"qwen"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Providers: tc.providers, Desktop: DesktopConfig{ProviderAccess: tc.access}}
			if got := cfg.ResolveVisionModelRef(); got != "" {
				t.Fatalf("vision model ref = %q, want no fallback", got)
			}
		})
	}
}

func TestAgentSubagentEffortConfigDecodesFromTOML(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`
	[agent]
	subagent_effort = "max"
	subagent_efforts = { review = "max", task = "high" }
	`, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if cfg.Agent.SubagentEffort != "max" {
		t.Fatalf("subagent_effort = %q, want max", cfg.Agent.SubagentEffort)
	}
	if cfg.Agent.SubagentEfforts["review"] != "max" {
		t.Fatalf("review effort = %q", cfg.Agent.SubagentEfforts["review"])
	}
	if cfg.Agent.SubagentEfforts["task"] != "high" {
		t.Fatalf("task effort = %q", cfg.Agent.SubagentEfforts["task"])
	}
}
