package control

import (
	"context"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type classifierProvider struct {
	text  string
	err   error
	req   provider.Request
	usage *provider.Usage
}

func (p *classifierProvider) Name() string { return "classifier" }

func (p *classifierProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.req = req
	if p.err != nil {
		return nil, p.err
	}
	chunks := 2
	if p.usage != nil {
		chunks++
	}
	ch := make(chan provider.Chunk, chunks)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.text}
	if p.usage != nil {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: p.usage}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestProviderAutoPlanClassifierEmitsAttributedUsageReceipt(t *testing.T) {
	p := &classifierProvider{
		text:  `{"needs_plan":true,"reason":"multi-file"}`,
		usage: &provider.Usage{PromptTokens: 12, CompletionTokens: 4, TotalTokens: 16},
	}
	var got event.Event
	sink := event.FuncSink(func(e event.Event) { got = e })
	c := NewProviderAutoPlanClassifier(p).WithTelemetry(sink, &provider.Pricing{CacheHit: 0.007, Input: 0.22, Output: 0.66, Currency: "$"}, "https://api.deepseek.com")
	if _, _, err := c.NeedsPlanWithParentTurn(context.Background(), "implement feature", 1, "turn-parent"); err != nil {
		t.Fatal(err)
	}
	if got.Kind != event.Usage || got.RequestID == "" || got.ParentTurnID != "turn-parent" || got.ProviderEndpoint != "https://api.deepseek.com" || got.Usage == nil || got.Usage.TotalTokens != 16 {
		t.Fatalf("auto-plan usage receipt = %+v", got)
	}
}

func TestProviderAutoPlanClassifierParsesJSON(t *testing.T) {
	p := &classifierProvider{text: "```json\n{\"needs_plan\":true,\"reason\":\"multi-file\"}\n```"}
	c := NewProviderAutoPlanClassifier(p)

	needsPlan, reason, err := c.NeedsPlan(context.Background(), "implement feature", 1)
	if err != nil {
		t.Fatalf("NeedsPlan error: %v", err)
	}
	if !needsPlan || reason != "multi-file" {
		t.Fatalf("NeedsPlan = (%v,%q), want (true,multi-file)", needsPlan, reason)
	}
	if len(p.req.Messages) != 2 || p.req.Messages[0].Role != provider.RoleSystem {
		t.Fatalf("request messages = %+v", p.req.Messages)
	}
	if p.req.MaxTokens != 80 || p.req.Temperature != 0 {
		t.Fatalf("request limits = max %d temp %v, want 80/0", p.req.MaxTokens, p.req.Temperature)
	}
	if !strings.Contains(p.req.Messages[1].Content, "heuristic_score=1") {
		t.Fatalf("user message missing score: %q", p.req.Messages[1].Content)
	}
}

func TestProviderAutoPlanClassifierRejectsBadJSON(t *testing.T) {
	p := &classifierProvider{text: "needs plan"}
	c := NewProviderAutoPlanClassifier(p)

	if _, _, err := c.NeedsPlan(context.Background(), "x", 1); err == nil {
		t.Fatal("NeedsPlan should reject non-JSON response")
	}
}

func TestProviderAutoPlanClassifierRequiresNeedsPlan(t *testing.T) {
	p := &classifierProvider{text: `{"reason":"missing decision"}`}
	c := NewProviderAutoPlanClassifier(p)

	if _, _, err := c.NeedsPlan(context.Background(), "x", 1); err == nil {
		t.Fatal("NeedsPlan should reject JSON without needs_plan")
	}
}

func TestProviderAutoPlanClassifierNilReceiverReturnsError(t *testing.T) {
	var c *ProviderAutoPlanClassifier

	if _, _, err := c.NeedsPlan(context.Background(), "x", 1); err == nil {
		t.Fatal("NeedsPlan should reject nil classifier")
	}
}
