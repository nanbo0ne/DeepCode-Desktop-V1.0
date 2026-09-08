package main

import (
	"math"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestMixedCurrenciesPreserveReceiptsWithoutAddingUnlikeAmounts(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "mixed"}, 1000, 1)
	prices := []*provider.Pricing{
		{CacheHit: .007, Input: .22, Output: .66, Currency: "$"},
		{CacheHit: .05, Input: 1.5, Output: 4.5, Currency: "¥"},
		{CacheHit: .007, Input: .22, Output: .66, Currency: "$"},
	}
	for i, pricing := range prices {
		tab.recordUsage(event.Event{
			Kind: event.Usage, TurnID: "mixed", RequestID: string(rune('a' + i)),
			ProviderEndpoint: "https://api.deepseek.com", Pricing: pricing,
			Usage: &provider.Usage{PromptTokens: 1_000_000, TotalTokens: 1_000_000, CacheMissTokens: 1_000_000},
		})
	}
	snapshot := tab.telemetrySnapshot()
	for _, stats := range []sessionUsageStats{snapshot.Usage, usageStatsFromEvents(snapshot.UsageEvents)} {
		if stats.CostAvailable || stats.SessionCurrency != "$" || math.Abs(stats.SessionCost-.44) > 1e-12 {
			t.Fatalf("mixed aggregate must stay hidden and keep only its currency subtotal: %+v", stats)
		}
	}
	turn := snapshot.Turns[0]
	if turn.CostAvailable || turn.Currency != "$" || math.Abs(turn.Cost-.44) > 1e-12 {
		t.Fatalf("mixed turn added unlike amounts or became available again: %+v", turn)
	}
	if len(snapshot.UsageEvents) != 3 || snapshot.UsageEvents[0].SessionCost != .22 || snapshot.UsageEvents[0].SessionCurrency != "$" || snapshot.UsageEvents[1].SessionCost != 1.5 || snapshot.UsageEvents[1].SessionCurrency != "¥" {
		t.Fatalf("individual historical receipts changed: %+v", snapshot.UsageEvents)
	}
}
