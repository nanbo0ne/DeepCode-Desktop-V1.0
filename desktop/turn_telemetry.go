package main

import (
	"strings"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/billing"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// usageTurnID resolves the canonical host turn for a usage receipt. Child and
// auxiliary producers must set ParentTurnID; ordinary lifecycle events use TurnID.
func usageTurnID(e event.Event, fallback string) string {
	if id := strings.TrimSpace(e.ParentTurnID); id != "" {
		return id
	}
	if id := strings.TrimSpace(e.TurnID); id != "" {
		return id
	}
	return strings.TrimSpace(fallback)
}

func usageTotalTokens(u *provider.Usage) int {
	if u == nil {
		return 0
	}
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

// usageCostBreakdownAvailable requires the fields needed to price one request
// without guessing at omitted tokens or cache state. Reasoning is deliberately
// excluded because it is a subset of completion tokens.
func usageCostBreakdownAvailable(u *provider.Usage) bool {
	if u == nil || u.PromptTokens < 0 || u.CompletionTokens < 0 || u.TotalTokens < 0 || u.CacheHitTokens < 0 || u.CacheMissTokens < 0 {
		return false
	}
	if u.TotalTokens != 0 && u.TotalTokens != u.PromptTokens+u.CompletionTokens {
		return false
	}
	// Cache diagnostics are optional on compatible providers; Pricing.Cost
	// conservatively treats an absent split as uncached prompt input.
	return true
}

// officialDeepSeekPricing recognizes current CNY and legacy USD snapshots
// after SnapshotAt has frozen them into plain per-request rates.
func officialDeepSeekPricing(p *provider.Pricing, endpoint string) bool {
	return billing.IsOfficialDeepSeekPricing(p, endpoint)
}

func (t *WorkspaceTab) annotateTurnDone(e event.Event) event.Event {
	if e.Kind != event.TurnDone {
		return e
	}
	t.telemMu.Lock()
	defer t.telemMu.Unlock()
	turnID := usageTurnID(e, t.currentTelemetryTurnID)
	for index := range t.turnTelemetry {
		turn := t.turnTelemetry[index]
		if turn.TurnID != turnID {
			continue
		}
		e.TurnTokens = turn.Tokens
		e.TurnCost = turn.Cost
		e.TurnCurrency = turn.Currency
		e.TurnCostAvailable = turn.CostAvailable
		return e
	}
	return e
}
