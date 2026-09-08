package config

import "testing"

func hasModel(c *Config, model string) *ProviderEntry {
	for i := range c.Providers {
		for _, m := range c.Providers[i].ModelList() {
			if m == model {
				return &c.Providers[i]
			}
		}
	}
	return nil
}

func TestExactMimoReferenceNeverFallsBackAcrossProviderIdentity(t *testing.T) {
	t.Setenv("MIMO_API_KEY", "api-key")
	t.Setenv("MIMO_TOKEN_PLAN_API_KEY", "plan-key")
	c := Default()
	c.Desktop.ProviderAccess = []string{"mimo-api", "mimo-token-plan"}
	normalizeDesktopOfficialProviderAccess(c)
	api, _ := c.Provider("mimo-api")
	api.Models = []string{"mimo-v2.5"}
	api.Default = "mimo-v2.5"
	if resolved, fallback, ok := c.ResolveModelWithFallback("mimo-api/mimo-v2.5-pro"); ok || fallback || resolved != "" {
		t.Fatalf("missing exact API ref crossed provider identity: resolved=%q fallback=%v ok=%v", resolved, fallback, ok)
	}
	if got, ok := c.ResolveModel("mimo-token-plan/mimo-v2.5-pro"); !ok || got.Name != "mimo-token-plan" {
		t.Fatalf("exact Token Plan ref = %+v, %v", got, ok)
	}
}

func TestBackfillDeepSeekProRestoresPro(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY"},
	}}
	backfillDeepSeekPro(c)
	pro := hasModel(c, "deepseek-v4-pro")
	if pro == nil {
		t.Fatal("deepseek-v4-pro not restored")
	} else if pro.Price == nil || pro.Price.Output != 13.5 || pro.Price.Currency != "¥" || pro.Price.Schedule == nil {
		t.Errorf("pro price not the preset: %+v", pro.Price)
	}
}

func TestBackfillDeepSeekProInheritsKeyEnv(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "MY_DS_KEY"},
	}}
	backfillDeepSeekPro(c)
	if pro := hasModel(c, "deepseek-v4-pro"); pro == nil || pro.APIKeyEnv != "MY_DS_KEY" {
		t.Errorf("pro should inherit the flash key env, got %+v", pro)
	}
}

func TestBackfillDeepSeekProNoopWhenProPresent(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "deepseek-flash", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"},
		{Name: "deepseek-pro", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"},
	}}
	backfillDeepSeekPro(c)
	if n := len(c.Providers); n != 2 {
		t.Errorf("providers grew to %d; should be a no-op when pro is present", n)
	}
}

func TestBackfillDeepSeekProSkipsCustomEndpoint(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "myproxy", BaseURL: "https://proxy.example.com/v1", Model: "deepseek-v4-flash"},
	}}
	backfillDeepSeekPro(c)
	if hasModel(c, "deepseek-v4-pro") != nil {
		t.Error("must not add pro for a non-official endpoint that may not serve it")
	}
}

func TestBackfillDeepSeekProSkipsNonDeepSeek(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "mimo-flash", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5"},
	}}
	backfillDeepSeekPro(c)
	if len(c.Providers) != 1 {
		t.Error("unrelated config must be untouched")
	}
}

func TestNormalizeLegacyProviderModelsRepairsOfficialProvider(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:      "deepseek-flash",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}}}
	normalizeLegacyProviderModels(c)
	if got := c.Providers[0].Model; got != "deepseek-v4-flash" {
		t.Fatalf("deepseek-flash model = %q, want deepseek-v4-flash", got)
	}
}

func TestNormalizeLegacyProviderModelsLeavesCustomProviderUntouched(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:    "custom",
		Kind:    "openai",
		BaseURL: "https://proxy.example.com/v1",
	}}}
	normalizeLegacyProviderModels(c)
	if got := c.Providers[0].Model; got != "" {
		t.Fatalf("custom provider model = %q, want empty", got)
	}
}

func TestNormalizeDesktopOfficialProviderAccessCanonicalizesLegacyIDs(t *testing.T) {
	c := Default()
	c.DefaultModel = "deepseek-flash/deepseek-v4-pro"
	c.Desktop.ProviderAccess = []string{"deepseek-flash", "mimo-pro"}
	normalizeDesktopOfficialProviderAccess(c)
	if len(c.Desktop.ProviderAccess) != 2 || c.Desktop.ProviderAccess[0] != "deepseek" || c.Desktop.ProviderAccess[1] != "mimo-token-plan" {
		t.Fatalf("provider_access = %+v, want canonical official ids", c.Desktop.ProviderAccess)
	}
	if c.DefaultModel != "deepseek/deepseek-v4-pro" {
		t.Fatalf("default_model = %q, want deepseek/deepseek-v4-pro", c.DefaultModel)
	}
	if _, ok := c.Provider("deepseek"); !ok {
		t.Fatal("canonical deepseek provider missing")
	}
	if _, ok := c.Provider("mimo-token-plan"); !ok {
		t.Fatal("canonical mimo-token-plan provider missing")
	}
}

func TestNormalizeDesktopOfficialProviderAccessEnsuresMimoAPI(t *testing.T) {
	c := Default()
	c.DefaultModel = "mimo-api/mimo-v2.5-pro"
	c.Bot.Model = "mimo-flash/mimo-v2.5"
	c.Desktop.ProviderAccess = []string{"mimo-api"}
	normalizeDesktopOfficialProviderAccess(c)
	if _, ok := c.Provider("mimo-api"); !ok {
		t.Fatal("mimo-api paid provider missing")
	}
	if got := c.Desktop.ProviderAccess; len(got) != 1 || got[0] != "mimo-api" {
		t.Fatalf("provider_access = %+v, want mimo-api", got)
	}
	if got := c.Bot.Model; got != "mimo-api/mimo-v2.5" {
		t.Fatalf("bot.model = %q, want migrated Mimo API reference", got)
	}
}

func TestResolveModelBareNameHonorsMimoProviderAccess(t *testing.T) {
	t.Setenv("MIMO_API_KEY", "test-key")
	c := &Config{
		DefaultModel: "mimo-api/mimo-v2.5-pro",
		Desktop:      DesktopConfig{ProviderAccess: []string{"mimo-api"}},
		Providers: []ProviderEntry{
			{Name: "mimo-flash", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5", APIKeyEnv: "MIMO_API_KEY"},
			{Name: "mimo-pro", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5-pro", APIKeyEnv: "MIMO_API_KEY"},
			{Name: "mimo-api", Kind: "openai", BaseURL: "https://api.xiaomimimo.com/v1", Models: []string{"mimo-v2.5", "mimo-v2.5-pro"}, Default: "mimo-v2.5-pro", APIKeyEnv: "MIMO_API_KEY"},
		},
	}

	entry, ok := c.ResolveModel("mimo-v2.5")
	if !ok || entry.Name != "mimo-api" || entry.BaseURL != "https://api.xiaomimimo.com/v1" {
		t.Fatalf("bare MiMo model resolved to %+v, want mimo-api", entry)
	}
	ref, fallback, ok := c.ResolveModelWithFallback("mimo-flash/mimo-v2.5")
	if !ok || fallback || ref != "mimo-api/mimo-v2.5" {
		t.Fatalf("legacy MiMo ref resolved to %q fallback=%v ok=%v", ref, fallback, ok)
	}
	if migrated, ok := c.ResolveModel("mimo-pro/mimo-v2.5-pro"); !ok || migrated.Name != "mimo-api" {
		t.Fatalf("legacy Mimo PRO alias resolved to %+v, want enabled Mimo API", migrated)
	}
}

func TestResolveModelProviderAccessKeepsCustomProviderVisible(t *testing.T) {
	c := &Config{
		DefaultModel: "deepseek/deepseek-v4-flash",
		Desktop:      DesktopConfig{ProviderAccess: []string{"deepseek"}},
		Providers: []ProviderEntry{
			{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com", Models: []string{"deepseek-v4-flash"}},
			{Name: "private-relay", Kind: "openai", BaseURL: "https://relay.example.test/v1", Models: []string{"test-model"}},
			{Name: "openai", Kind: "openai", BaseURL: "https://api.openai.com/v1", Models: []string{"gpt-test"}},
		},
	}

	if got, ok := c.ResolveModel("private-relay/test-model"); !ok || got.Name != "private-relay" {
		t.Fatalf("custom provider was hidden by provider_access: %+v, %v", got, ok)
	}
	if got, ok := c.ResolveModel("openai/gpt-test"); ok || got != nil {
		t.Fatalf("curated provider not in provider_access resolved as %+v, %v", got, ok)
	}
}
