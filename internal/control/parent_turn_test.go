package control

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/agent"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/event"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/tool"
)

type parentTurnRunner struct{ contexts []context.Context }

func (r *parentTurnRunner) Run(ctx context.Context, _ string) error {
	r.contexts = append(r.contexts, ctx)
	return nil
}

func TestControllerParentTurnIdentity(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(map[bool]string{false: "ephemeral", true: "saved"}[saved], func(t *testing.T) {
			r := &parentTurnRunner{}
			path := ""
			if saved {
				path = filepath.Join(t.TempDir(), "session.jsonl")
			}
			c := New(Options{Runner: r, SessionPath: path})
			for i := 0; i < 2; i++ {
				if err := c.RunTurn(context.Background(), "test"); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Run(context.Background(), "headless"); err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, ctx := range r.contexts {
				id, lifetime := agent.ParentTurn(ctx)
				if id == "" || seen[id] || lifetime == nil || lifetime.Err() == nil {
					t.Fatal("identity reused or lifetime not ended")
				}
				seen[id] = true
				if got := agent.ParentSession(ctx); got != agent.BranchID(path) || (got != "") != saved {
					t.Fatalf("parent session persistence semantics changed: saved=%t parent=%q", saved, got)
				}
			}
			if len(seen) != 3 {
				t.Fatal("runner not called")
			}
		})
	}
}

type parentTurnProvider struct {
	scriptedTurns
	contexts []context.Context
}

func (p *parentTurnProvider) Stream(ctx context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	p.contexts = append(p.contexts, ctx)
	return p.scriptedTurns.Stream(ctx, request)
}

func TestControllerGoalContinuationKeepsParentTurn(t *testing.T) {
	p := &parentTurnProvider{scriptedTurns: scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Started.\n\n[goal:continue]"), textTurn("Finished.\n\n[goal:complete]"),
	}}}
	ag := agent.New(p, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{Runner: ag, Executor: ag, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone || e.Kind == event.Notice {
			events <- e
		}
	})})
	c.Submit("/goal test parent identity")
	waitForTurnDone(t, events)
	if len(p.contexts) != 2 {
		t.Fatalf("calls=%d", len(p.contexts))
	}
	firstID, firstLifetime := agent.ParentTurn(p.contexts[0])
	nextID, nextLifetime := agent.ParentTurn(p.contexts[1])
	if firstID == "" || nextID != firstID || firstLifetime != nextLifetime || firstLifetime.Err() == nil {
		t.Fatal("automatic continuation reset parent turn")
	}
	if agent.ParentSession(p.contexts[0]) != "" || agent.ParentSession(p.contexts[1]) != "" {
		t.Fatal("ephemeral goal acquired a persistent parent session")
	}
}
