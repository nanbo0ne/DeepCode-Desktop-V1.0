package billing

import (
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestOfficialDeepSeekPricing(t *testing.T) {
	tests := []struct {
		model             string
		cacheHit, input   float64
		output            float64
		peakCache, peakIn float64
		peakOutput        float64
	}{
		{"deepseek-v4-flash", 0.05, 1.5, 4.5, 0.10, 3, 9},
		{"deepseek-v4-flash-vision-exp", 0.05, 1.5, 4.5, 0.10, 3, 9},
		{"deepseek-v4-pro", 0.15, 4.5, 13.5, 0.30, 9, 27},
	}
	beijing := time.FixedZone("test-beijing", 8*60*60)
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := OfficialDeepSeekPricing("deepseek", "https://api.deepseek.com", tt.model)
			if p == nil || p.Currency != "¥" || p.CacheHit != tt.cacheHit || p.Input != tt.input || p.Output != tt.output {
				t.Fatalf("off-peak pricing = %+v, want ¥ %.4f/%.4f/%.4f", p, tt.cacheHit, tt.input, tt.output)
			}
			peak := p.SnapshotAt(time.Date(2026, time.August, 17, 15, 0, 0, 0, beijing))
			if peak.CacheHit != tt.peakCache || peak.Input != tt.peakIn || peak.Output != tt.peakOutput {
				t.Fatalf("peak pricing = %+v, want ¥ %.4f/%.4f/%.4f", peak, tt.peakCache, tt.peakIn, tt.peakOutput)
			}
		})
	}
}

func TestOfficialDeepSeekPricingBoundariesAndScope(t *testing.T) {
	beijing := time.FixedZone("test-beijing", 8*60*60)
	p := OfficialDeepSeekPricing("deepseek-flash", "https://api.deepseek.com/v1", "deepseek-v4-flash")
	if p == nil {
		t.Fatal("official pricing is nil")
	}
	for _, tt := range []struct {
		name string
		at   time.Time
		want float64
	}{
		{"morning start", time.Date(2026, time.August, 17, 9, 0, 0, 0, beijing), 3},
		{"morning end", time.Date(2026, time.August, 17, 12, 0, 0, 0, beijing), 1.5},
		{"afternoon start", time.Date(2026, time.August, 17, 14, 0, 0, 0, beijing), 3},
		{"afternoon end", time.Date(2026, time.August, 17, 18, 0, 0, 0, beijing), 1.5},
		{"weekend", time.Date(2026, time.August, 22, 10, 0, 0, 0, beijing), 1.5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.SnapshotAt(tt.at).Input; got != tt.want {
				t.Errorf("input rate = %v, want %v", got, tt.want)
			}
		})
	}
	if got := OfficialDeepSeekPricing("other", "https://relay.example/v1", "deepseek-v4-flash"); got != nil {
		t.Fatalf("other provider received official pricing: %+v", got)
	}
	if got := OfficialDeepSeekPricing("relay", "https://relay.example/v1", "deepseek-v4-flash"); got != nil {
		t.Fatalf("custom gateway received official pricing: %+v", got)
	}
	if got := OfficialDeepSeekPricing("other", "https://api.deepseek.com", "deepseek-v4-flash"); got != nil {
		t.Fatalf("other provider received official pricing: %+v", got)
	}
	if got := OfficialDeepSeekPricing("deepseek", "http://api.deepseek.com", "deepseek-v4-flash"); got != nil {
		t.Fatalf("insecure endpoint received official pricing: %+v", got)
	}
}

func TestIsOfficialDeepSeekPricingKeepsLegacyUSDRecognizedWithoutRelabeling(t *testing.T) {
	legacy := &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"}
	if !IsOfficialDeepSeekPricing(legacy, "https://api.deepseek.com") {
		t.Fatal("legacy USD pricing was rejected")
	}
	if legacy.Symbol() != "$" || legacy.Cost(&provider.Usage{CacheMissTokens: 1_000_000}) != 0.22 {
		t.Fatalf("legacy USD pricing was changed: %+v", legacy)
	}
	current := &provider.Pricing{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "¥"}
	if !IsOfficialDeepSeekPricing(current, "https://api.deepseek.com") {
		t.Fatal("current CNY pricing was rejected")
	}
}
