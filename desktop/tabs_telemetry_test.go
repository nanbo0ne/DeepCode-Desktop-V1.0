package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent/testutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/control"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type usageProvider struct {
	usage *provider.Usage
}

func (p usageProvider) Name() string { return "usage" }

func (p usageProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: p.usage}
	close(ch)
	return ch, nil
}

func TestTelemetryLoadsLegacyReadFileArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl.telemetry.json")
	if err := os.WriteFile(path, []byte(`[{"path":"README.md","turn":2,"time":1000}]`), 0o644); err != nil {
		t.Fatalf("write legacy telemetry: %v", err)
	}

	got := loadTelemetry(path)
	if len(got.ReadFiles) != 1 || got.ReadFiles[0].Path != "README.md" {
		t.Fatalf("legacy read files = %+v", got.ReadFiles)
	}
	if got.Usage.RequestCount != 0 {
		t.Fatalf("legacy usage request count = %d, want 0", got.Usage.RequestCount)
	}
}

func TestRuntimeSwitchTelemetryInterruptsUnfinishedRecords(t *testing.T) {
	records := []RuntimeSwitchRecord{
		{ID: "active", FromMode: promptModeCoding, ToMode: promptModeAssistant, AppliedMode: promptModeCoding, Phase: RuntimeSwitchBuilding, Progress: 35, StartedAt: 1000},
		{ID: "done", FromMode: promptModeAssistant, ToMode: promptModeCoding, AppliedMode: promptModeCoding, Phase: RuntimeSwitchCompleted, Progress: 100, StartedAt: 500, CompletedAt: 900},
	}
	got, changed := interruptRuntimeSwitches(records, 2000)
	if !changed || got[0].Phase != RuntimeSwitchInterrupted || got[0].AppliedMode != promptModeCoding || got[0].CompletedAt != 2000 {
		t.Fatalf("interrupted records = %+v, changed=%v", got, changed)
	}
	if got[1].Phase != RuntimeSwitchCompleted || got[1].CompletedAt != 900 {
		t.Fatalf("completed switch changed during recovery: %+v", got[1])
	}
}

func TestRuntimeSwitchTelemetryRoundTripsCurrentVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl.telemetry.json")
	snapshot := tabTelemetrySnapshot{
		RuntimeSwitches: []RuntimeSwitchRecord{{
			ID: "switch-1", Generation: 4, FromMode: promptModeCoding, ToMode: promptModeAssistant,
			AppliedMode: promptModeAssistant, Phase: RuntimeSwitchCompleted, Progress: 100,
			MessageIndex: 3, StartedAt: 1000, CompletedAt: 1200,
		}},
	}
	if err := saveTelemetry(path, snapshot); err != nil {
		t.Fatal(err)
	}
	got := loadTelemetry(path)
	if got.Version != 7 || len(got.RuntimeSwitches) != 1 || got.RuntimeSwitches[0].ID != "switch-1" {
		t.Fatalf("round-tripped telemetry = %+v", got)
	}
}

func TestRiskReviewTelemetryRoundTripsWithoutArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl.telemetry.json")
	snapshot := tabTelemetrySnapshot{RiskReviews: []event.RiskReviewAudit{{
		Turn: 2, At: 1000, Level: "high", Model: "review/model", DurationMs: 42, Result: "manual_review",
	}}}
	if err := saveTelemetry(path, snapshot); err != nil {
		t.Fatal(err)
	}
	got := loadTelemetry(path)
	if got.Version != 7 || len(got.RiskReviews) != 1 || got.RiskReviews[0].Result != "manual_review" {
		t.Fatalf("risk review telemetry = %+v", got)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(b)
	for _, secretField := range []string{"\"arguments\"", "\"subject\"", "\"reason\""} {
		if strings.Contains(serialized, secretField) {
			t.Fatalf("risk telemetry persisted sensitive field %q: %s", secretField, b)
		}
	}
}

func TestWorkspaceTabAggregatesSessionUsageTelemetry(t *testing.T) {
	tab := &WorkspaceTab{}
	start := time.Now().Add(-2 * time.Second).UnixMilli()
	tab.recordTurnStarted(0, start)
	tab.recordUsage(event.Event{
		Usage:       &provider.Usage{PromptTokens: 100, CompletionTokens: 40, TotalTokens: 140, CacheHitTokens: 70, CacheMissTokens: 30, ReasoningTokens: 10},
		SessionHit:  70,
		SessionMiss: 30,
		Pricing:     &provider.Pricing{CacheHit: 1, Input: 2, Output: 3, Currency: "¥"},
	})
	tab.recordTurnDone(start + 1500)

	got := tab.telemetrySnapshot().Usage
	if got.RequestCount != 1 || got.PromptTokens != 100 || got.CompletionTokens != 40 || got.TotalTokens != 140 || got.ReasoningTokens != 10 {
		t.Fatalf("usage tokens = %+v", got)
	}
	if got.CacheHitTokens != 70 || got.CacheMissTokens != 30 {
		t.Fatalf("cache tokens = hit %d miss %d", got.CacheHitTokens, got.CacheMissTokens)
	}
	if got.ElapsedMs != 1500 {
		t.Fatalf("elapsed = %d, want 1500", got.ElapsedMs)
	}
	if got.SessionCost <= 0 || got.SessionCurrency != "¥" {
		t.Fatalf("cost = %f %q, want positive ¥", got.SessionCost, got.SessionCurrency)
	}

	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	if context := app.ContextUsageForTab("tab"); context.SessionTokens != 140 {
		t.Fatalf("context usage session tokens = %d, want 140", context.SessionTokens)
	} else if context.SessionCacheHitTokens != 70 || context.SessionCacheMissTokens != 30 {
		t.Fatalf("context cache tokens = hit %d miss %d, want 70/30", context.SessionCacheHitTokens, context.SessionCacheMissTokens)
	} else if context.SessionCost <= 0 || context.SessionCurrency == "" {
		t.Fatalf("context cost = %f %q, want positive non-empty currency", context.SessionCost, context.SessionCurrency)
	}
	if panel := app.ContextPanel("tab"); panel.TotalTokens != 140 {
		t.Fatalf("context panel total tokens = %d, want 140", panel.TotalTokens)
	}
}

func TestWorkspaceTabAggregatesAuthoritativePerTurnUsageOnce(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-parent"}, 1000, 4)

	flashOffPeak := &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"}
	usage := &provider.Usage{
		PromptTokens:     1_000_000,
		CompletionTokens: 1_000_000,
		TotalTokens:      2_000_000,
		ReasoningTokens:  400_000,
		CacheHitTokens:   500_000,
		CacheMissTokens:  500_000,
	}
	tab.recordUsage(event.Event{Kind: event.Usage, TurnID: "turn-parent", RequestID: "primary-1", ProviderEndpoint: "https://api.deepseek.com", Usage: usage, Pricing: flashOffPeak})
	// A repeated provider receipt must not double count, even if a retry path
	// supplies a different pricing pointer.
	tab.recordUsage(event.Event{Kind: event.Usage, TurnID: "turn-parent", RequestID: "primary-1", ProviderEndpoint: "https://api.deepseek.com", Usage: usage, Pricing: &provider.Pricing{CacheHit: 99, Input: 99, Output: 99, Currency: "$"}})
	// Child and risk requests are attributed to the same parent turn by the
	// explicit parent identity; reasoning remains a subset of completion.
	tab.recordUsage(event.Event{Kind: event.Usage, ParentTurnID: "turn-parent", RequestID: "child-1", ProviderEndpoint: "https://api.deepseek.com", Usage: &provider.Usage{TotalTokens: 10, PromptTokens: 6, CompletionTokens: 4, ReasoningTokens: 3}, Pricing: flashOffPeak})
	tab.recordUsage(event.Event{Kind: event.Usage, ParentTurnID: "turn-parent", RequestID: "risk-1", ProviderEndpoint: "https://api.deepseek.com", Usage: &provider.Usage{TotalTokens: 5, PromptTokens: 4, CompletionTokens: 1}, Pricing: flashOffPeak})
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: "turn-parent", Outcome: event.TurnOutcomeSuccess}, 2000, 4)

	turns := tab.telemetrySnapshot().Turns
	if len(turns) != 1 {
		t.Fatalf("turns = %+v", turns)
	}
	turn := turns[0]
	if turn.Tokens != 2_000_015 {
		t.Fatalf("turn tokens = %d, want 2000015", turn.Tokens)
	}
	if math.Abs(turn.Cost-0.7735055) > 1e-12 || turn.Currency != "$" || !turn.CostAvailable {
		t.Fatalf("turn cost = %v %q available=%v, want 0.7735055 $ available", turn.Cost, turn.Currency, turn.CostAvailable)
	}
	if len(tab.telemetrySnapshot().UsageEvents) != 3 {
		t.Fatalf("usage event count = %d, want 3 after request dedup", len(tab.telemetrySnapshot().UsageEvents))
	}
}

func TestWorkspaceTabExposesCurrentCNYDeepSeekCost(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-cny"}, 1000, 1)
	tab.recordUsage(event.Event{
		Kind: event.Usage, TurnID: "turn-cny", RequestID: "req-cny",
		ProviderEndpoint: "https://api.deepseek.com",
		Usage: &provider.Usage{
			PromptTokens: 1_000_000, CompletionTokens: 1_000_000,
			TotalTokens: 2_000_000, CacheMissTokens: 1_000_000,
		},
		Pricing: &provider.Pricing{CacheHit: 0.05, Input: 1.5, Output: 4.5, Currency: "¥"},
	})
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: "turn-cny", Outcome: event.TurnOutcomeSuccess}, 2000, 1)

	turn := tab.telemetrySnapshot().Turns[0]
	if turn.Cost != 6 || turn.Currency != "¥" || !turn.CostAvailable {
		t.Fatalf("current CNY turn = %+v, want 6 ¥ available", turn)
	}
}

func TestWorkspaceTabHidesPartialTurnCostWithoutFakingZero(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-partial"}, 1000, 1)
	tab.recordUsage(event.Event{
		Kind: event.Usage, TurnID: "turn-partial", RequestID: "known",
		Usage:            &provider.Usage{PromptTokens: 1_000_000, CacheMissTokens: 1_000_000, TotalTokens: 1_000_000},
		ProviderEndpoint: "https://api.deepseek.com",
		Pricing:          &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"},
	})
	tab.recordUsage(event.Event{
		Kind: event.Usage, TurnID: "turn-partial", RequestID: "unknown",
		Usage: &provider.Usage{TotalTokens: 50},
	})
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: "turn-partial", Outcome: event.TurnOutcomeSuccess}, 1100, 1)

	turn := tab.telemetrySnapshot().Turns[0]
	if turn.Tokens != 1_000_050 {
		t.Fatalf("partial turn tokens = %d, want 1000050", turn.Tokens)
	}
	if math.Abs(turn.Cost-0.22) > 1e-12 || turn.CostAvailable || turn.Currency != "$" {
		t.Fatalf("partial turn cost = %v %q available=%v, want known partial cost hidden", turn.Cost, turn.Currency, turn.CostAvailable)
	}
	if tab.telemetrySnapshot().Usage.CostAvailable {
		t.Fatal("session header cost must be unavailable when any request is partial")
	}
}

func TestWorkspaceTabHidesCostWhenRequestIDIsMissing(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-no-request-id"}, 1000, 1)
	tab.recordUsage(event.Event{
		Kind: event.Usage, TurnID: "turn-no-request-id",
		Usage:            &provider.Usage{PromptTokens: 1_000_000, CacheMissTokens: 1_000_000, TotalTokens: 1_000_000},
		ProviderEndpoint: "https://api.deepseek.com",
		Pricing:          &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"},
	})
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: "turn-no-request-id", Outcome: event.TurnOutcomeSuccess}, 1100, 1)

	turn := tab.telemetrySnapshot().Turns[0]
	if turn.Tokens != 1_000_000 || turn.Cost != 0 || turn.CostAvailable {
		t.Fatalf("missing request ID turn stats = %+v, want tokens retained and cost hidden", turn)
	}
}

func TestWorkspaceTabHidesOfficialCostWhenUsageBreakdownIsIncomplete(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-incomplete"}, 1000, 1)
	tab.recordUsage(event.Event{
		Kind: event.Usage, TurnID: "turn-incomplete", RequestID: "req-incomplete",
		Usage:            &provider.Usage{PromptTokens: 100, TotalTokens: 150, CacheMissTokens: 100},
		ProviderEndpoint: "https://api.deepseek.com",
		Pricing:          &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"},
	})
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: "turn-incomplete", Outcome: event.TurnOutcomeSuccess}, 1100, 1)

	turn := tab.telemetrySnapshot().Turns[0]
	if turn.Tokens != 150 || turn.Cost != 0 || turn.CostAvailable {
		t.Fatalf("incomplete official usage = %+v, want tokens retained and cost unavailable", turn)
	}
}

func TestWorkspaceTabHidesCostWhenFinishedTurnHasUnreportedFailure(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-failed-request"}, 1000, 1)
	tab.recordUsage(event.Event{
		Kind: event.Usage, TurnID: "turn-failed-request", RequestID: "req-before-failure",
		Usage:            &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CacheMissTokens: 100},
		ProviderEndpoint: "https://api.deepseek.com",
		Pricing:          &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"},
	})
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: "turn-failed-request", Outcome: event.TurnOutcomeFailed, Err: errors.New("provider failed before usage")}, 1100, 1)

	turn := tab.telemetrySnapshot().Turns[0]
	if turn.Tokens != 120 || turn.Cost == 0 || turn.CostAvailable {
		t.Fatalf("failed turn cost = %+v, want known partial cost hidden", turn)
	}
}

func TestWorkspaceTabHidesPendingBackgroundChildCostUntilSettled(t *testing.T) {
	tab := &WorkspaceTab{}
	turnID := "turn-pending-child"
	pricing := &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: turnID}, 1000, 1)
	tab.recordUsage(event.Event{
		Kind: event.Usage, TurnID: turnID, RequestID: "root",
		ProviderEndpoint: "https://api.deepseek.com/", Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CacheMissTokens: 100}, Pricing: pricing,
	})
	tab.recordChildState(event.Event{Kind: event.ChildStarted, TurnID: turnID, ParentTurnID: turnID, ChildID: "child-1"})
	if tab.telemetrySnapshot().Usage.CostAvailable {
		t.Fatal("session header exposed cost while background child was pending")
	}

	turnDone := tab.annotateTurnDone(event.Event{Kind: event.TurnDone, TurnID: turnID, Outcome: event.TurnOutcomeSuccess})
	if turnDone.TurnCostAvailable {
		t.Fatal("turn_done exposed cost while background child was pending")
	}
	tab.recordUsage(event.Event{
		Kind: event.Usage, ParentTurnID: turnID, RequestID: "child-1-request",
		ProviderEndpoint: "https://api.deepseek.com/", Usage: &provider.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60, CacheMissTokens: 50}, Pricing: pricing,
	})
	if tab.telemetrySnapshot().Usage.CostAvailable {
		t.Fatal("official child receipt exposed cost before child settled")
	}

	tab.recordChildState(event.Event{Kind: event.ChildDone, TurnID: turnID, ParentTurnID: turnID, ChildID: "child-1", ChildUsageReported: true})
	got := tab.telemetrySnapshot().Turns[0]
	if !got.CostAvailable || got.RequestCount != 2 || got.Tokens != 180 {
		t.Fatalf("settled child aggregate = %+v, want authoritative two-request total", got)
	}
	if !tab.telemetrySnapshot().Usage.CostAvailable {
		t.Fatal("session header did not restore cost after reported child settled")
	}
}

func TestWorkspaceTabKeepsCostUnavailableForSettledChildWithoutUsage(t *testing.T) {
	tab := &WorkspaceTab{}
	turnID := "turn-child-without-usage"
	pricing := &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: turnID}, 1000, 1)
	tab.recordUsage(event.Event{
		Kind: event.Usage, TurnID: turnID, RequestID: "root",
		ProviderEndpoint: "https://api.deepseek.com/", Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}, Pricing: pricing,
	})
	tab.recordChildState(event.Event{Kind: event.ChildStarted, TurnID: turnID, ChildID: "child-unknown"})
	tab.recordChildState(event.Event{Kind: event.ChildDone, TurnID: turnID, ChildID: "child-unknown"})
	turn := tab.telemetrySnapshot().Turns[0]
	if turn.CostAvailable || tab.telemetrySnapshot().Usage.CostAvailable {
		t.Fatalf("unreported settled child exposed cost: turn=%+v usage=%+v", turn, tab.telemetrySnapshot().Usage)
	}
}

func TestWorkspaceTabPersistsTurnCostFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl.telemetry.json")
	snapshot := tabTelemetrySnapshot{Version: 7, Turns: []turnTelemetryRecord{{
		TurnID: "turn-1", Tokens: 12, Cost: 0.000012, Currency: "$", CostAvailable: true,
	}}}
	if err := saveTelemetry(path, snapshot); err != nil {
		t.Fatal(err)
	}
	got := loadTelemetry(path)
	if len(got.Turns) != 1 || got.Turns[0].Tokens != 12 || got.Turns[0].Cost != 0.000012 || got.Turns[0].Currency != "$" || !got.Turns[0].CostAvailable {
		t.Fatalf("persisted turn stats = %+v", got.Turns)
	}
}

func TestWorkspaceTabPreservesStoredUSDAmountAndLabel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl.telemetry.json")
	snapshot := tabTelemetrySnapshot{
		Version: 7,
		Usage: sessionUsageStats{
			SessionCost: 0.22, SessionCostUsd: 0.22,
			SessionCurrency: "$", CostAvailable: true,
		},
		UsageEvents: []usageTelemetryEvent{{
			SessionCost: 0.22, SessionCostUsd: 0.22,
			SessionCurrency: "$", CostAvailable: true,
		}},
	}
	if err := saveTelemetry(path, snapshot); err != nil {
		t.Fatal(err)
	}
	got := loadTelemetry(path)
	if got.Usage.SessionCost != 0.22 || got.Usage.SessionCurrency != "$" || !got.Usage.CostAvailable {
		t.Fatalf("stored USD session was relabeled or recalculated: %+v", got.Usage)
	}
	if len(got.UsageEvents) != 1 || got.UsageEvents[0].SessionCost != 0.22 || got.UsageEvents[0].SessionCurrency != "$" {
		t.Fatalf("stored USD receipt changed: %+v", got.UsageEvents)
	}
}

func TestWorkspaceTabDeduplicatesPersistedRequestReceiptAfterRestart(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-restart"}, 1000, 1)
	pricing := &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"}
	usage := &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
	tab.recordUsage(event.Event{Kind: event.Usage, TurnID: "turn-restart", RequestID: "req-replayed", ProviderEndpoint: "https://api.deepseek.com", Usage: usage, Pricing: pricing})
	snapshot := tab.telemetrySnapshot()

	restored := &WorkspaceTab{
		usageTelemetry:         snapshot.Usage,
		usageTelemetryEvents:   snapshot.UsageEvents,
		turnTelemetry:          snapshot.Turns,
		currentTelemetryTurnID: "turn-restart",
	}
	restored.recordUsage(event.Event{Kind: event.Usage, TurnID: "turn-restart", RequestID: "req-replayed", ProviderEndpoint: "https://api.deepseek.com", Usage: usage, Pricing: pricing})
	turn := restored.telemetrySnapshot().Turns[0]
	if turn.RequestCount != 1 || turn.Tokens != 120 || len(restored.telemetrySnapshot().UsageEvents) != 1 {
		t.Fatalf("replayed request changed restored telemetry: turn=%+v events=%+v", turn, restored.telemetrySnapshot().UsageEvents)
	}
}

func TestLateChildUsageRefreshesPersistedFinishedTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	ctrl := control.New(control.Options{SessionPath: path})
	tab := &WorkspaceTab{Ctrl: ctrl}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	sink := &tabEventSink{app: app, tabID: "tab"}
	turnID := "turn-late-child"
	pricing := &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: turnID}, 1000, 1)
	tab.recordUsage(event.Event{Kind: event.Usage, TurnID: turnID, RequestID: "root", ProviderEndpoint: "https://api.deepseek.com", Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CacheMissTokens: 100}, Pricing: pricing})
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: turnID, Outcome: event.TurnOutcomeSuccess}, 1100, 1)
	sink.recordTurnDone()
	before := loadTelemetry(path + ".telemetry.json")
	if len(before.Turns) != 1 || before.Turns[0].Tokens != 120 || !before.Turns[0].CostAvailable || before.Turns[0].Outcome != event.TurnOutcomeSuccess {
		t.Fatalf("persisted completed turn before child = %+v", before.Turns)
	}

	// Background child work can arrive after the host TurnDone. It must refresh
	// the durable record, while retaining the completed outcome and known totals.
	sink.recordUsageTelemetry(event.Event{
		Kind: event.Usage, ParentTurnID: turnID, RequestID: "child-late",
		ProviderEndpoint: "https://api.deepseek.com", Usage: &provider.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60, CacheMissTokens: 50}, Pricing: pricing,
	})
	after := loadTelemetry(path + ".telemetry.json")
	if len(after.Turns) != 1 || after.Turns[0].Outcome != event.TurnOutcomeSuccess || after.Turns[0].State != event.TurnStateFinished {
		t.Fatalf("late child changed finished outcome = %+v", after.Turns)
	}
	if after.Turns[0].Tokens != 180 || after.Turns[0].RequestCount != 2 || !after.Turns[0].CostAvailable {
		t.Fatalf("late child did not refresh persisted totals = %+v", after.Turns)
	}

	sink.recordUsageTelemetry(event.Event{
		Kind: event.Usage, ParentTurnID: turnID, RequestID: "child-unknown",
		Usage: &provider.Usage{TotalTokens: 40},
	})
	unknown := loadTelemetry(path + ".telemetry.json")
	if len(unknown.Turns) != 1 || unknown.Turns[0].Outcome != event.TurnOutcomeSuccess || unknown.Turns[0].State != event.TurnStateFinished || unknown.Turns[0].Tokens != 220 || unknown.Turns[0].CostAvailable {
		t.Fatalf("unknown late child should hide cost without reopening turn = %+v", unknown.Turns)
	}
}

func TestWorkspaceTabRootUsageDeduplicatesAndNormalizesTotals(t *testing.T) {
	tab := &WorkspaceTab{}
	pricing := &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"}
	tab.recordUsage(event.Event{RequestID: "root-1", ProviderEndpoint: "https://api.deepseek.com", Usage: &provider.Usage{PromptTokens: 9, CompletionTokens: 3}, Pricing: pricing})
	tab.recordUsage(event.Event{RequestID: "root-1", ProviderEndpoint: "https://api.deepseek.com", Usage: &provider.Usage{TotalTokens: 999}, Pricing: pricing})
	got := tab.telemetrySnapshot().Usage
	if got.RequestCount != 1 || got.TotalTokens != 12 || got.PromptTokens != 9 || got.CompletionTokens != 3 {
		t.Fatalf("root usage = %+v, want one deduplicated normalized receipt", got)
	}
}

type telemetryFixtureTool struct{}

func (telemetryFixtureTool) Name() string            { return "fixture_read" }
func (telemetryFixtureTool) Description() string     { return "fixture read" }
func (telemetryFixtureTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (telemetryFixtureTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "fixture result", nil
}
func (telemetryFixtureTool) ReadOnly() bool { return true }

func TestRealAgentMultiRequestTelemetryOfficialEndpointAndRelay(t *testing.T) {
	run := func(endpoint string) (*WorkspaceTab, []event.Event, []provider.Request) {
		fixture := testutil.NewMock("deepseek-v4", testutil.Turn{
			ToolCalls: []provider.ToolCall{{ID: "fixture-call", Name: "fixture_read", Arguments: `{}`}},
			Usage:     &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, ReasoningTokens: 10, CacheMissTokens: 100},
		}, testutil.Turn{
			Text:  "finished",
			Usage: &provider.Usage{PromptTokens: 150, CompletionTokens: 30, TotalTokens: 180, ReasoningTokens: 20, CacheHitTokens: 50, CacheMissTokens: 100},
		})
		reg := tool.NewRegistry()
		reg.Add(telemetryFixtureTool{})
		var events []event.Event
		sink := event.Lifecycle(event.FuncSink(func(e event.Event) { events = append(events, e) }))
		a := agent.New(fixture, reg, agent.NewSession("system"), agent.Options{
			Pricing:          &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"},
			ProviderEndpoint: endpoint,
		}, sink)
		if err := a.Run(context.Background(), "inspect"); err != nil {
			t.Fatalf("fixture agent run: %v", err)
		}
		var turnID string
		for _, e := range events {
			if e.Kind == event.TurnStarted {
				turnID = e.TurnID
				break
			}
		}
		if turnID == "" {
			t.Fatal("fixture emitted no turn start")
		}
		tab := &WorkspaceTab{}
		for _, e := range events {
			tab.recordLifecycle(e, 1000, 1)
			if e.Kind == event.Usage {
				tab.recordUsage(e)
			}
		}
		tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: turnID, Outcome: event.TurnOutcomeSuccess}, 2000, 1)
		return tab, events, fixture.Requests()
	}

	official, officialEvents, officialRequests := run("https://api.deepseek.com")
	turn := official.telemetrySnapshot().Turns[0]
	if len(officialRequests) != 2 || len(official.telemetrySnapshot().UsageEvents) != 2 {
		t.Fatalf("official requests/events = %d/%d, want two provider requests and receipts", len(officialRequests), len(official.telemetrySnapshot().UsageEvents))
	}
	if officialRequests[0].RequestID == "" || officialRequests[0].RequestID == officialRequests[1].RequestID {
		t.Fatalf("provider request IDs = %q, %q, want distinct non-empty IDs", officialRequests[0].RequestID, officialRequests[1].RequestID)
	}
	if turn.Tokens != 300 || turn.Cost <= 0 || !turn.CostAvailable || turn.Currency != "$" {
		t.Fatalf("official turn = %+v, want authoritative cost", turn)
	}
	var firstUsage event.Event
	for _, e := range officialEvents {
		if e.Kind == event.Usage {
			firstUsage = e
			break
		}
	}
	official.recordUsage(firstUsage)
	if got := official.telemetrySnapshot().Turns[0]; got.RequestCount != 2 || got.Tokens != 300 {
		t.Fatalf("duplicate receipt changed official turn = %+v", got)
	}

	relay, _, _ := run("https://relay.example/v1")
	relayTurn := relay.telemetrySnapshot().Turns[0]
	if relayTurn.Tokens != 300 || relayTurn.Cost != 0 || relayTurn.CostAvailable {
		t.Fatalf("relay turn = %+v, want tokens retained and cost unavailable", relayTurn)
	}
}

func TestContextUsageRestoresLastUsageEventAfterRestart(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordUsage(event.Event{
		Usage: &provider.Usage{
			PromptTokens:     100,
			CompletionTokens: 40,
			TotalTokens:      140,
			CacheHitTokens:   70,
			CacheMissTokens:  30,
			ReasoningTokens:  10,
		},
		SessionHit:  70,
		SessionMiss: 30,
		Pricing:     &provider.Pricing{CacheHit: 1, Input: 2, Output: 3, Currency: "¥"},
	})
	snapshot := tab.telemetrySnapshot()

	restored := &WorkspaceTab{
		ID:                   "tab",
		readTelemetry:        snapshot.ReadFiles,
		usageTelemetry:       snapshot.Usage,
		usageTelemetryEvents: snapshot.UsageEvents,
	}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": restored}}
	context := app.ContextUsageForTab("tab")
	if context.TotalTokens != 140 || context.PromptTokens != 100 || context.CompletionTokens != 40 || context.ReasoningTokens != 10 {
		t.Fatalf("restored last usage = %+v, want latest event tokens", context)
	}
	if context.CacheHitTokens != 70 || context.CacheMissTokens != 30 || context.SessionCacheHitTokens != 70 || context.SessionCacheMissTokens != 30 {
		t.Fatalf("restored cache = %+v, want latest and session cache", context)
	}
	if context.SessionCost <= 0 || context.SessionCurrency == "" {
		t.Fatalf("restored cost = %f %q, want persisted cost with currency", context.SessionCost, context.SessionCurrency)
	}
}

func TestTelemetryPersistsMixedPeakAndOffPeakCostsWithoutRepricing(t *testing.T) {
	tab := &WorkspaceTab{}
	usage := &provider.Usage{PromptTokens: 1_000_000, CacheMissTokens: 1_000_000, TotalTokens: 1_000_000}
	tab.recordUsage(event.Event{Usage: usage, Pricing: &provider.Pricing{Input: 1.5, Currency: "¥"}})
	tab.recordUsage(event.Event{Usage: usage, Pricing: &provider.Pricing{Input: 3, Currency: "¥"}})

	path := filepath.Join(t.TempDir(), "session.jsonl.telemetry.json")
	if err := saveTelemetry(path, tab.telemetrySnapshot()); err != nil {
		t.Fatal(err)
	}
	got := loadTelemetry(path)
	if got.Usage.SessionCost != 4.5 || got.Usage.SessionCurrency != "¥" {
		t.Fatalf("restored mixed-rate cost = %v %q, want 4.5 ¥", got.Usage.SessionCost, got.Usage.SessionCurrency)
	}
	if len(got.UsageEvents) != 2 || got.UsageEvents[0].SessionCost != 1.5 || got.UsageEvents[1].SessionCost != 3 {
		t.Fatalf("persisted request costs were repriced: %+v", got.UsageEvents)
	}
}

func TestBlankConversationContextUsageDisplaysZero(t *testing.T) {
	ag := agent.New(
		usageProvider{usage: &provider.Usage{PromptTokens: 99, CompletionTokens: 1, TotalTokens: 100}},
		tool.NewRegistry(),
		agent.NewSession("system prompt and tools are loaded"),
		agent.Options{ContextWindow: 200000},
		event.Discard,
	)
	tab := &WorkspaceTab{
		ID:    "tab",
		Ctrl:  control.New(control.Options{Executor: ag, Sink: event.Discard}),
		Scope: "global",
		Ready: true,
	}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}

	context := app.ContextUsageForTab("tab")
	if context.Used != 0 {
		t.Fatalf("blank context used = %d, want 0", context.Used)
	}
	if context.Window == 0 {
		t.Fatalf("blank context window = 0, want model window retained")
	}
	panel := app.ContextPanel("tab")
	if panel.UsedTokens != 0 {
		t.Fatalf("blank panel used = %d, want 0", panel.UsedTokens)
	}
	if panel.WindowTokens == 0 {
		t.Fatalf("blank panel window = 0, want model window retained")
	}
}

func TestWorkspaceTabRewindTelemetryPrunesUsage(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-0"}, 1000, 0)
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: "turn-0", Outcome: event.TurnOutcomeSuccess}, 1100, 0)
	tab.recordTurnStarted(0, 1000)
	tab.recordUsage(event.Event{Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}})
	tab.recordTurnDone(1100)
	tab.recordTurnStarted(1, 1200)
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-1"}, 1200, 1)
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: "turn-1", Outcome: event.TurnOutcomeFailed}, 1300, 1)
	tab.recordUsage(event.Event{Usage: &provider.Usage{PromptTokens: 20, CompletionTokens: 6, TotalTokens: 26}})
	tab.recordTurnDone(1300)

	tab.rewindTelemetryBefore(1)

	got := tab.telemetrySnapshot().Usage
	if got.RequestCount != 1 || got.TotalTokens != 15 || got.PromptTokens != 10 || got.CompletionTokens != 5 {
		t.Fatalf("rewound usage = %+v, want only first turn", got)
	}
	turns := tab.telemetrySnapshot().Turns
	if len(turns) != 1 || turns[0].TurnID != "turn-0" {
		t.Fatalf("rewound turns = %+v, want only turn-0", turns)
	}
}

func TestWorkspaceTabLifecycleTelemetryRecordsCommittedFinal(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordLifecycle(event.Event{Kind: event.TurnStarted, TurnID: "turn-1"}, 1000, 7)
	tab.recordLifecycle(event.Event{Kind: event.ItemCompleted, TurnID: "turn-1", ItemID: "item-progress", MessageID: "message-progress", ItemType: event.ItemAgentMessage, ItemStatus: event.ItemStatusCompleted}, 1010, 7)
	tab.recordLifecycle(event.Event{Kind: event.ItemCompleted, TurnID: "turn-1", ItemID: "item-final", MessageID: "message-final", ItemType: event.ItemAgentMessage, ItemStatus: event.ItemStatusCompleted}, 1020, 7)
	tab.recordLifecycle(event.Event{Kind: event.AnswerCommitted, TurnID: "turn-1", FinalItemID: "item-final", FinalMessageID: "message-final"}, 1030, 7)
	tab.recordLifecycle(event.Event{Kind: event.TurnDone, TurnID: "turn-1", Outcome: event.TurnOutcomeSuccess, FinalItemID: "item-final", FinalMessageID: "message-final"}, 1100, 7)

	turns := tab.telemetrySnapshot().Turns
	if len(turns) != 1 {
		t.Fatalf("turn count = %d, want 1", len(turns))
	}
	turn := turns[0]
	if turn.CheckpointTurn != 7 || turn.Outcome != event.TurnOutcomeSuccess || turn.FinalMessageID != "message-final" || turn.ElapsedMs != 100 {
		t.Fatalf("turn telemetry = %+v", turn)
	}
	if len(turn.Items) != 2 || turn.Items[1].MessageOrdinal != 1 {
		t.Fatalf("turn items = %+v", turn.Items)
	}
}

func TestContextPanelUsesLastUsageBreakdownWithTelemetryTotal(t *testing.T) {
	lastUsage := &provider.Usage{
		PromptTokens:     10,
		CompletionTokens: 4,
		TotalTokens:      14,
		CacheHitTokens:   7,
		CacheMissTokens:  3,
		ReasoningTokens:  2,
	}
	ag := agent.New(
		usageProvider{usage: lastUsage},
		tool.NewRegistry(),
		agent.NewSession("system"),
		agent.Options{ContextWindow: 200},
		event.Discard,
	)
	if err := ag.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{
		ID:    "tab",
		Ctrl:  control.New(control.Options{Executor: ag, Sink: event.Discard}),
		Scope: "global",
		Ready: true,
	}
	tab.recordUsage(event.Event{
		Usage: &provider.Usage{
			PromptTokens:     100,
			CompletionTokens: 40,
			TotalTokens:      140,
			CacheHitTokens:   70,
			CacheMissTokens:  30,
			ReasoningTokens:  10,
		},
	})
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}

	panel := app.ContextPanel("tab")
	if panel.TotalTokens != 140 {
		t.Fatalf("context panel total tokens = %d, want telemetry total 140", panel.TotalTokens)
	}
	if panel.PromptTokens != 10 || panel.CompletionTokens != 4 || panel.ReasoningTokens != 2 {
		t.Fatalf("context panel breakdown = prompt:%d completion:%d reasoning:%d, want last usage 10/4/2",
			panel.PromptTokens, panel.CompletionTokens, panel.ReasoningTokens)
	}
	if panel.CacheHitTokens != 7 || panel.CacheMissTokens != 3 {
		t.Fatalf("context panel cache breakdown = hit:%d miss:%d, want last usage 7/3",
			panel.CacheHitTokens, panel.CacheMissTokens)
	}
}
