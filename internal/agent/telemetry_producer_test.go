package agent

import (
	"context"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent/testutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

func TestAgentUsageReceiptCarriesRequestAndParentTurn(t *testing.T) {
	providerFixture := testutil.NewMock("fixture", testutil.Turn{
		Text:  "done",
		Usage: &provider.Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10, ReasoningTokens: 2},
	})
	var events []event.Event
	sink := event.Lifecycle(event.FuncSink(func(e event.Event) { events = append(events, e) }))
	a := New(providerFixture, tool.NewRegistry(), NewSession("system"), Options{
		Pricing:          &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"},
		ProviderEndpoint: "https://api.deepseek.com",
	}, sink)

	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	var started, usage event.Event
	for _, e := range events {
		switch e.Kind {
		case event.TurnStarted:
			started = e
		case event.Usage:
			usage = e
		}
	}
	if started.TurnID == "" || usage.TurnID != started.TurnID {
		t.Fatalf("turn attribution = start %q usage %q", started.TurnID, usage.TurnID)
	}
	if usage.RequestID == "" || providerFixture.LastRequest().RequestID != usage.RequestID {
		t.Fatalf("request attribution = provider %q usage %q", providerFixture.LastRequest().RequestID, usage.RequestID)
	}
	if usage.ProviderEndpoint != "https://api.deepseek.com" {
		t.Fatalf("provider endpoint = %q", usage.ProviderEndpoint)
	}
}

func TestSubagentUsageForwardsParentTurnAndRequestID(t *testing.T) {
	ctx, end := WithParentTurn(context.Background())
	defer end()
	parentTurn, _ := ParentTurn(ctx)
	var got event.Event
	parent := event.FuncSink(func(e event.Event) { got = e })
	nested := subSink(withCallContext(ctx, "task-1", parent, nil))
	nested.Emit(event.Event{Kind: event.Usage, RequestID: "child-request", Usage: &provider.Usage{TotalTokens: 4}})
	if got.Kind != event.Usage || got.RequestID != "child-request" || got.ParentTurnID != parentTurn {
		t.Fatalf("forwarded usage = %+v, want parent turn %q", got, parentTurn)
	}
}
