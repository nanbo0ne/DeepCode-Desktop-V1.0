package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/config"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/instruction"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/permission"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider/anthropic"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

// TestReproAnthropicCleanEOFWithoutMessageStop records the current behavior:
// a clean connection close after text, without message_stop, is exposed as a
// successful ChunkDone. The expected contract for a framed streaming protocol
// is an interruption/error until the terminal event has been observed.
func TestReproAnthropicCleanEOFWithoutMessageStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n")
	}))
	defer srv.Close()

	p, err := anthropic.New(provider.Config{Name: "audit", BaseURL: srv.URL, Model: "claude-audit"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var text string
	var done, streamError bool
	for chunk := range stream {
		switch chunk.Type {
		case provider.ChunkText:
			text += chunk.Text
		case provider.ChunkDone:
			done = true
		case provider.ChunkError:
			streamError = true
		}
	}
	if text != "partial" || !done || streamError {
		t.Fatalf("repro no longer matches current behavior: text=%q done=%v error=%v", text, done, streamError)
	}
}

type failingReviewer struct{}

func (failingReviewer) Review(context.Context, string, string, json.RawMessage, bool) (bool, error) {
	return false, errors.New("classifier unavailable")
}

// TestReproRiskClassifierFailureFailsOpen records that an unavailable
// AutoReviewer returns allow=true from the fallback policy. A risk classifier
// failure should leave the call at manual review (or deny in a headless mode).
func TestReproRiskClassifierFailureFailsOpen(t *testing.T) {
	g := permission.NewGate(permission.New("allow", nil, nil, nil), nil)
	g.AutoReviewer = failingReviewer{}

	allow, _, err := g.Check(context.Background(), "write_file", json.RawMessage(`{"path":"audit.txt"}`), false)
	if err != nil || !allow {
		t.Fatalf("repro no longer matches current behavior: allow=%v err=%v", allow, err)
	}
}

type auditTool struct {
	name string
}

func (auditTool) Description() string     { return "audit fake tool" }
func (auditTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (auditTool) ReadOnly() bool          { return false }
func (t auditTool) Name() string          { return t.name }
func (t auditTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.name + " ok", nil
}

// TestReproReadinessGenericVerificationSatisfiesEveryProjectCheck records that
// one generic verification command suppresses all distinct project checks.
func TestReproReadinessGenericVerificationSatisfiesEveryProjectCheck(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(auditTool{name: "write_file"})
	reg.Add(auditTool{name: "bash"})

	prov := &scriptedProvider{turns: [][]provider.Chunk{
		{
			toolCall("write-1", "write_file", `{"path":"changed.go","content":"package main"}`),
			toolCall("verify-1", "bash", `{"command":"git diff --check"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "committed despite missing named checks"}, {Type: provider.ChunkDone}},
	}}

	a := agent.New(prov, reg, agent.NewSession(""), agent.Options{
		ProjectChecks: []instruction.VerifyCheck{
			{Command: "go test ./required", SourcePath: "AGENTS.md", Line: 10},
			{Command: "npm test", SourcePath: "AGENTS.md", Line: 11},
		},
	}, event.Discard)
	if err := a.Run(context.Background(), "edit and verify"); err != nil {
		t.Fatalf("repro no longer matches current behavior: Run returned %v", err)
	}
	if prov.calls != 2 {
		t.Fatalf("provider calls=%d, want the current two-call commit path", prov.calls)
	}
}

type scriptedProvider struct {
	turns [][]provider.Chunk
	calls int
}

func (p *scriptedProvider) Name() string { return "audit-provider" }

func (p *scriptedProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	index := p.calls
	if index >= len(p.turns) {
		return nil, errors.New("audit provider exhausted")
	}
	p.calls++
	ch := make(chan provider.Chunk, len(p.turns[index]))
	for _, chunk := range p.turns[index] {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func toolCall(id, name, args string) provider.Chunk {
	return provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: args}}
}

var _ provider.Provider = (*scriptedProvider)(nil)
var _ tool.Tool = auditTool{}

// TestReproStaleQualifiedModelFallsAcrossProviders records that an unresolved
// provider/model reference can be reduced to its bare model and sent to a
// different provider. This is documented fallback behavior, but it is a
// routing hazard when the same model ID exists at multiple endpoints.
func TestReproStaleQualifiedModelFallsAcrossProviders(t *testing.T) {
	c := &config.Config{Providers: []config.ProviderEntry{
		{Name: "relay-a", Kind: "openai", BaseURL: "https://relay-a.invalid/v1", Models: []string{"shared-model"}},
		{Name: "relay-b", Kind: "openai", BaseURL: "https://relay-b.invalid/v1", Models: []string{"shared-model"}},
	}}
	ref, fallback, ok := c.ResolveModelWithFallback("deleted-relay/shared-model")
	if !ok || !fallback || ref != "relay-a/shared-model" {
		t.Fatalf("repro no longer matches current behavior: ref=%q fallback=%v ok=%v", ref, fallback, ok)
	}
}
