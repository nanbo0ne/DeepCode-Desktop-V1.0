package main

import (
	"context"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/computeruse"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type completionProvider struct {
	request provider.Request
	text    string
	done    bool
}

func (*completionProvider) Name() string { return "completion-test" }
func (p *completionProvider) Stream(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.request = r
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.text}
	if p.done {
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}
	close(ch)
	return ch, nil
}

func TestComputerCompletionRequiresIndependentEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		done, ok   bool
	}{
		{"verified", `{"satisfied":true,"reason":"result is visible"}`, true, true},
		{"missing", `{"satisfied":false,"reason":"result missing"}`, true, false},
		{"no evidence", `{"satisfied":true,"reason":""}`, true, false},
		{"invalid", "done", true, false},
		{"unknown field", `{"satisfied":true,"reason":"visible","override":true}`, true, false},
		{"truncated", `{"satisfied":true,"reason":"visible"}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &completionProvider{text: tc.text, done: tc.done}
			err := verifyComputerCompletion(context.Background(), p, computeruse.StartRequest{Goal: "test goal"}, computeruse.Observation{Generation: 1, Screenshot: "synthetic", ScreenshotMIME: "image/png"})
			if (err == nil) != tc.ok {
				t.Fatalf("verification err=%v", err)
			}
			if len(p.request.Messages) != 2 || len(p.request.Tools) != 0 || p.request.Temperature != 0 || !p.request.DisableThinking {
				t.Fatalf("verifier request not isolated: %+v", p.request)
			}
		})
	}
}

func TestComputerControlRejectsBatchedActionAndCompletion(t *testing.T) {
	if err := validateComputerCalls([]provider.ToolCall{{Name: "computer_action"}, {Name: "computer_complete"}}); err == nil {
		t.Fatal("mixed batch accepted")
	}
	if err := validateComputerCalls([]provider.ToolCall{{Name: "computer_action"}}); err != nil {
		t.Fatal(err)
	}
}
