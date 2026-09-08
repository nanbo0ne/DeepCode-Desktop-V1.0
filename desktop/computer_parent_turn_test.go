package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/computeruse"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

func TestComputerParentTurnCumulativeBudget(t *testing.T) {
	for _, finishFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "exhausted", true: "success_then_new_goal"}[finishFirst], func(t *testing.T) {
			b := &loopTestBackend{}
			a := &App{computerUse: computeruse.NewService(b, nil)}
			ctx := computerParentTestContext(t)
			calls, complete := 0, finishFirst
			p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
				calls++
				if len(r.Tools) == 0 {
					return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: `{"satisfied":true,"reason":"synthetic"}`}), nil
				}
				if complete && b.actions == 30 {
					return controlCall("computer_complete", `{"summary":"done"}`), nil
				}
				return controlCall("computer_action", `{"type":"wait","timeoutMs":1}`), nil
			}}
			_, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "first"}, p)
			if finishFirst {
				if err != nil || b.actions != 30 {
					t.Fatalf("first: actions=%d err=%v", b.actions, err)
				}
				complete = false
				_, err = a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "second"}, p)
			}
			if !errors.Is(err, computeruse.ErrParentTurnStopped) || b.actions != 40 {
				t.Fatalf("budget: actions=%d err=%v", b.actions, err)
			}
			id, requests := a.computerUse.Current().ID, calls
			for i := 0; i < 3; i++ {
				if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "renamed retry"}, p); !errors.Is(err, computeruse.ErrParentTurnStopped) {
					t.Fatalf("replay admitted: %v", err)
				}
			}
			if b.actions != 40 || calls != requests || a.computerUse.Current().ID != id {
				t.Fatal("retry reset session or reached provider/input")
			}
		})
	}
}

func TestComputerParentTurnExactlyFortiethActionCanComplete(t *testing.T) {
	b := &loopTestBackend{}
	a := &App{computerUse: computeruse.NewService(b, nil)}
	ctx := computerParentTestContext(t)
	decisions, checks := 0, 0
	lastActionImage := ""
	p := &controlTestProvider{stream: func(_ context.Context, r provider.Request) (<-chan provider.Chunk, error) {
		if len(r.Tools) == 0 {
			checks++
			for _, message := range r.Messages {
				for _, img := range message.Images {
					if img.Data == lastActionImage {
						t.Fatal("reused final decision image")
					}
				}
			}
			return controlChunks(provider.Chunk{Type: provider.ChunkText, Text: `{"satisfied":true,"reason":"40th action completed task"}`}), nil
		}
		decisions++
		if b.actions == 40 {
			for _, schema := range r.Tools {
				if schema.Name == "computer_action" {
					t.Fatal("final decision exposes action tool")
				}
			}
			if !strings.Contains(r.Messages[0].Content, "No further computer_action") {
				t.Fatal("final decision lacks action prohibition")
			}
			for _, message := range r.Messages {
				for _, img := range message.Images {
					lastActionImage = img.Data
				}
			}
			return controlCall("computer_complete", `{"summary":"done on action 40"}`), nil
		}
		return controlCall("computer_action", `{"type":"wait","timeoutMs":1}`), nil
	}}
	result, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "exactly 40"}, p)
	if err != nil || result != "done on action 40" || b.actions != 40 || decisions != 41 || checks != 1 || a.computerUse.Current().State != computeruse.StateSucceeded {
		t.Fatalf("result=%q actions=%d decisions=%d checks=%d err=%v", result, b.actions, decisions, checks, err)
	}
	if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "new goal after completion"}, p); !errors.Is(err, computeruse.ErrParentTurnStopped) {
		t.Fatalf("completion reset exhausted budget: %v", err)
	}
}

func TestComputerParentTurnCancellationAndConcurrentAdmission(t *testing.T) {
	b := &loopTestBackend{}
	a := &App{computerUse: computeruse.NewService(b, nil)}
	parent := computerParentTestContext(t)
	callCtx, cancel := context.WithCancel(parent)
	defer cancel()
	entered := make(chan struct{})
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		close(entered)
		return make(chan provider.Chunk), nil
	}}
	done := make(chan error, 1)
	go func() {
		_, err := a.runComputerTaskWithProvider(callCtx, computeruse.StartRequest{TabID: "a", Goal: "first"}, p)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("provider not entered")
	}
	if _, err := a.runComputerTaskWithProvider(parent, computeruse.StartRequest{TabID: "a", Goal: "parallel"}, p); !errors.Is(err, computeruse.ErrOccupied) {
		t.Fatalf("concurrent: %v", err)
	}
	if _, err := a.runComputerTaskWithProvider(parent, computeruse.StartRequest{TabID: "b", Goal: "other tab"}, p); !errors.Is(err, computeruse.ErrWrongOwner) {
		t.Fatalf("tab: %v", err)
	}
	otherSession := agent.WithParentSession(parent, "other-parent")
	if _, err := a.runComputerTaskWithProvider(otherSession, computeruse.StartRequest{TabID: "a", Goal: "other session"}, p); !errors.Is(err, computeruse.ErrWrongOwner) {
		t.Fatalf("session: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, computeruse.ErrParentTurnStopped) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel blocked")
	}
	if _, err := a.runComputerTaskWithProvider(parent, computeruse.StartRequest{TabID: "a", Goal: "retry after call cancellation"}, p); !errors.Is(err, computeruse.ErrParentTurnStopped) {
		t.Fatalf("call cancellation erased budget: %v", err)
	}
	if b.actions != 0 {
		t.Fatal("unexpected input")
	}
}

func TestComputerParentTurnResumePreservesBudget(t *testing.T) {
	for _, newTurn := range []bool{false, true} {
		t.Run(map[bool]string{false: "same_turn", true: "new_user_turn"}[newTurn], func(t *testing.T) {
			b := &loopTestBackend{}
			a := &App{computerUse: computeruse.NewService(b, nil)}
			ctx := computerParentTestContext(t)
			pause := true
			p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
				if pause && b.actions == 30 {
					return controlCall("computer_escalate", `{"reason":"need decision"}`), nil
				}
				return controlCall("computer_action", `{"type":"wait","timeoutMs":1}`), nil
			}}
			if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "test"}, p); err != nil {
				t.Fatal(err)
			}
			id := a.computerUse.Current().ID
			if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "reset"}, p); !errors.Is(err, computeruse.ErrParentTurnStopped) {
				t.Fatalf("paused replacement: %v", err)
			}
			if newTurn {
				ctx = computerParentTestContext(t)
			}
			pause = false
			if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", ResumeSessionID: id, Guidance: "continue safely"}, p); !errors.Is(err, computeruse.ErrParentTurnStopped) {
				t.Fatalf("resume: %v", err)
			}
			if b.actions != 40 || a.computerUse.Current().ID != id {
				t.Fatalf("resume reset budget/session: actions=%d", b.actions)
			}
		})
	}
}

func TestComputerParentTurnRejectsMissingOrForgedIdentity(t *testing.T) {
	a := &App{computerUse: computeruse.NewService(&loopTestBackend{}, nil)}
	p := &controlTestProvider{stream: func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		t.Fatal("provider reached")
		return nil, nil
	}}
	for _, ctx := range []context.Context{context.Background(), agent.WithParentSession(context.Background(), "session")} {
		if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "test"}, p); err == nil {
			t.Fatal("missing identity admitted")
		}
	}
	ctx := computerParentTestContext(t)
	if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "test", ParentTurnID: "forged"}, p); !errors.Is(err, computeruse.ErrWrongOwner) {
		t.Fatal(err)
	}
	if _, err := a.runComputerTaskWithProvider(ctx, computeruse.StartRequest{TabID: "a", Goal: "test", ParentSessionID: "forged"}, p); !errors.Is(err, computeruse.ErrWrongOwner) {
		t.Fatal(err)
	}
}

func TestComputerScrollSchemaMatchesWindowsWheelSemantics(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(computerControlSchemas[0].Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	for field, words := range map[string][]string{"deltaY": {"positive", "UP", "negative", "DOWN", "120"}, "deltaX": {"positive", "RIGHT", "120"}} {
		for _, word := range words {
			if !strings.Contains(schema.Properties[field].Description, word) {
				t.Fatalf("%s missing %s", field, word)
			}
		}
	}
	prompt := computerControlSystemPrompt(computeruse.StartRequest{})
	for _, phrase := range []string{"Windows wheel units", "deltaY positive scrolls UP", "negative scrolls DOWN", "deltaX positive scrolls RIGHT", "120"} {
		if !strings.Contains(prompt, phrase) {
			t.Fatalf("prompt missing %q", phrase)
		}
	}
}
