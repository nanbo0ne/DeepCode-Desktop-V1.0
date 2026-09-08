package config

import "testing"

func TestSelectableProviderPresetCatalogIsDeepSeekOnly(t *testing.T) {
	catalog := SelectableProviderPresetCatalog()
	if len(catalog) != 1 || catalog[0].ID != "deepseek" {
		t.Fatalf("new provider catalog = %#v, want only DeepSeek", catalog)
	}
	if catalog[0].Entry.DefaultModel() != "deepseek-v4-flash" {
		t.Fatalf("DeepSeek default model = %q", catalog[0].Entry.DefaultModel())
	}
}

func TestProviderPresetCatalogPutsDeepSeekFirstForExistingViews(t *testing.T) {
	catalog := ProviderPresetCatalog()
	if len(catalog) == 0 || catalog[0].ID != "deepseek" {
		t.Fatalf("provider catalog first entry = %#v, want DeepSeek", catalog)
	}
}

func TestHistoricalPresetIDsRemainResolvable(t *testing.T) {
	for _, id := range []string{"openai", "anthropic", "openrouter", "mimo-api", "mimo-token-plan", "stepfun", "step-plan"} {
		preset, ok := ProviderPresetByID(id)
		if !ok || preset.ID != id {
			t.Fatalf("historical preset %q = %#v, %v", id, preset, ok)
		}
	}
}

func TestHistoricalDeepSeekPresetKeepsPricingMetadata(t *testing.T) {
	preset, ok := ProviderPresetByID("deepseek")
	if !ok {
		t.Fatal("DeepSeek preset missing")
	}
	if preset.Entry.BalanceURL == "" || preset.Entry.ContextWindow != 1_000_000 {
		t.Fatalf("DeepSeek metadata = %+v", preset.Entry)
	}
}
