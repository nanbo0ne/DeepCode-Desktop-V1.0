package config

import "testing"

func configureTestMimo(t *testing.T, c *Config) {
	t.Helper()
	if err := c.UpsertProvider(ProviderEntry{
		Name:          "mimo-pro",
		Kind:          "openai",
		BaseURL:       "https://token-plan-cn.xiaomimimo.com/v1",
		Models:        []string{"mimo-v2.5", "mimo-v2.5-pro"},
		Default:       "mimo-v2.5-pro",
		APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
		ContextWindow: 1_048_576,
		NoProxy:       true,
	}); err != nil {
		t.Fatalf("configure test MiMo: %v", err)
	}
}
