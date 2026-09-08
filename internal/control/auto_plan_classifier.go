package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/nilutil"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

const autoPlanClassifierPrompt = `You classify whether a coding-agent user request should first enter read-only planning mode.
Return ONLY JSON: {"needs_plan":true|false,"reason":"short reason"}.
Use true for multi-step implementation, refactors, migrations, unclear cross-file work, PRD/spec/issue work, or tasks needing investigation before edits.
Use false for explanations, simple questions, single obvious edits, direct commands, or requests that should be answered without changing files.`

type ProviderAutoPlanClassifier struct {
	prov             provider.Provider
	usageSink        event.Sink
	pricing          *provider.Pricing
	providerEndpoint string
}

func NewProviderAutoPlanClassifier(prov provider.Provider) *ProviderAutoPlanClassifier {
	if nilutil.IsNil(prov) {
		return nil
	}
	return &ProviderAutoPlanClassifier{prov: prov}
}

func (c *ProviderAutoPlanClassifier) NeedsPlan(ctx context.Context, input string, score int) (bool, string, error) {
	return c.NeedsPlanWithParentTurn(ctx, input, score, "")
}

// WithTelemetry enables request-scoped classifier usage receipts. The
// classifier remains usable without a sink for hosts that only need the
// planning decision.
func (c *ProviderAutoPlanClassifier) WithTelemetry(sink event.Sink, pricing *provider.Pricing, endpoint string) *ProviderAutoPlanClassifier {
	if c == nil {
		return c
	}
	c.usageSink = sink
	c.pricing = pricing
	c.providerEndpoint = strings.TrimSpace(endpoint)
	return c
}

// NeedsPlanWithParentTurn attributes this auxiliary request to the host turn
// that asked for the classification. A model name or numeric pricing table is
// not treated as proof of an official cost source by downstream sinks.
func (c *ProviderAutoPlanClassifier) NeedsPlanWithParentTurn(ctx context.Context, input string, score int, parentTurnID string) (bool, string, error) {
	if c == nil || nilutil.IsNil(c.prov) {
		return false, "", fmt.Errorf("auto plan classifier is not initialized")
	}
	requestID := event.NewRequestID()
	requestPricing := c.pricing.SnapshotAt(time.Now())
	var usage *provider.Usage
	ch, err := c.prov.Stream(ctx, provider.Request{
		RequestID: requestID,
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: autoPlanClassifierPrompt},
			{Role: provider.RoleUser, Content: fmt.Sprintf("heuristic_score=%d\n\nUSER_REQUEST:\n%s", score, input)},
		},
		Temperature: 0,
		MaxTokens:   80,
	})
	if err != nil {
		return false, "", err
	}
	defer func() {
		if usage != nil && c.usageSink != nil {
			c.usageSink.Emit(event.Event{Kind: event.Usage, ParentTurnID: strings.TrimSpace(parentTurnID), RequestID: requestID, ProviderEndpoint: c.providerEndpoint, Usage: usage, Pricing: requestPricing})
		}
	}()

	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkError:
			return false, "", chunk.Err
		}
	}

	var out struct {
		NeedsPlan *bool  `json:"needs_plan"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(text.String())), &out); err != nil {
		return false, "", fmt.Errorf("decode classifier response: %w", err)
	}
	if out.NeedsPlan == nil {
		return false, "", fmt.Errorf("decode classifier response: missing needs_plan")
	}
	return *out.NeedsPlan, strings.TrimSpace(out.Reason), nil
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end >= start {
		return s[start : end+1]
	}
	return s
}
